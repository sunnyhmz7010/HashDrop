package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"tinyfaka"
)

func TestHiddenProductCanBePurchasedWithoutAppearingInCatalog(t *testing.T) {
	a, _ := newTestApp(t)
	admin := adminSessionRequest(t, a)
	hiddenID := insertProduct(t, a, "card", 3)
	visibleID := insertProduct(t, a, "static", 0)
	input := map[string]any{"name": "链接商品", "price": 1234, "currency": "CNY", "kind": "card", "active": true, "hidden": true}
	save := func() Product {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		response := admin("PUT", "/api/admin/products/"+hiddenID, string(body))
		var product Product
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &product) != nil {
			t.Fatalf("保存商品失败: %s", response.Body)
		}
		return product
	}
	if product := save(); !product.Hidden || !product.Active {
		t.Fatal("隐藏设置与上架状态未分别保存")
	}
	public := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Origin", a.config.PublicURL)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		a.routes().ServeHTTP(response, request)
		return response
	}
	var products []Product
	response := public("GET", "/api/products", "")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &products) != nil || len(products) != 1 || products[0].ID != visibleID {
		t.Fatal("公开列表未排除隐藏商品")
	}
	response = admin("GET", "/api/admin/products")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &products) != nil || len(products) != 2 {
		t.Fatal("隐藏商品从管理员列表中丢失")
	}
	var product Product
	response = public("GET", "/api/products/"+hiddenID, "")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &product) != nil || !product.Hidden || product.Available != 3 {
		t.Fatal("隐藏商品无法通过链接读取")
	}
	delete(input, "hidden")
	input["name"] = "修改后的链接商品"
	if product := save(); !product.Hidden {
		t.Fatal("旧客户端省略隐藏字段时清除了隐藏设置")
	}
	response = public("POST", "/api/orders", `{"product_id":"`+hiddenID+`","quantity":2,"key":"hidden-purchase","quoted_total":2468,"quoted_currency":"CNY"}`)
	var created struct {
		Order Order `json:"order"`
	}
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &created) != nil || created.Order.ProductID != hiddenID || created.Order.Quantity != 2 || created.Order.Total != 2468 {
		t.Fatalf("隐藏商品匿名下单失败: %s", response.Body)
	}
	product, err := scanProduct(a.db.QueryRow(productSelect+" WHERE p.id=?", hiddenID))
	if err != nil || product.Available != 1 || product.Reserved != 2 {
		t.Fatal("隐藏商品下单未正确预留库存")
	}
	input["hidden"] = false
	if product := save(); product.Hidden {
		t.Fatal("无法取消隐藏")
	}
	response = public("GET", "/api/products", "")
	if json.Unmarshal(response.Body.Bytes(), &products) != nil || len(products) != 2 {
		t.Fatal("取消隐藏后未恢复商城展示")
	}
	input["hidden"] = true
	input["active"] = false
	save()
	if response := public("GET", "/api/products/"+hiddenID, ""); response.Code != 404 {
		t.Fatal("下架的隐藏商品仍能访问")
	}
	if response := public("POST", "/api/orders", `{"product_id":"`+hiddenID+`","quantity":1}`); response.Code != 400 {
		t.Fatal("下架的隐藏商品仍能下单")
	}
}

func TestHiddenProductMigrationKeepsExistingProductsVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version-five.db")
	old, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	for _, file := range []string{"migrations/001_init.sql", "migrations/002_gateway_orders.sql", "migrations/003_gateway_identity.sql", "migrations/004_remove_service_products.sql", "migrations/005_product_details.sql"} {
		schema, err := tinyfaka.Assets.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = old.Exec(string(schema)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = old.Exec(`INSERT INTO products(id,name,description,details,price,currency,kind,active,created_at,updated_at) VALUES('legacy','原有商品','原有介绍','## 原有详情',1234,'CNY','card',1,0,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(`INSERT INTO stock_items(id,product_id,content,fingerprint,created_at) VALUES('legacy-stock','legacy','原有密文','fingerprint',0)`); err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	product, err := scanProduct(db.QueryRow(productSelect + " WHERE p.id='legacy'"))
	if err != nil || product.Hidden || !product.Active || product.Details != "## 原有详情" || product.Description != "原有介绍" || product.Available != 1 {
		t.Fatal("升级隐藏字段改变了已有商品或库存")
	}
	var content string
	if err = db.QueryRow("SELECT content FROM stock_items WHERE id='legacy-stock'").Scan(&content); err != nil || content != "原有密文" {
		t.Fatal("升级隐藏字段破坏了库存密文")
	}
}
