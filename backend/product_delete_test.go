package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"tinyfaka"
)

func TestDeleteProductPreservesOrdersAndClaims(t *testing.T) {
	for _, kind := range []string{"card", "static"} {
		t.Run(kind, func(t *testing.T) {
			a, gateway := newTestApp(t)
			stock := 0
			if kind == "card" {
				stock = 4
			}
			product := insertProduct(t, a, kind, stock)
			other := insertProduct(t, a, "static", 0)
			paid, paidToken := reserve(t, a, product, 1)
			syncPaid(t, a, gateway, paid)
			before, err := a.deliveryResults(context.Background(), paid.ID)
			if err != nil || len(before) != 1 || before[0].Content == "" {
				t.Fatal("已付款订单未准备好领取内容")
			}
			pending, pendingToken := reserve(t, a, product, 1)
			if err = a.syncOrder(context.Background(), pending.ID); err != nil {
				t.Fatal(err)
			}
			admin := adminSessionRequest(t, a)
			unauthorized := httptest.NewRecorder()
			a.routes().ServeHTTP(unauthorized, httptest.NewRequest("DELETE", "/api/admin/products/"+product, nil))
			if unauthorized.Code != http.StatusUnauthorized {
				t.Fatal("未登录可以删除商品")
			}
			for i := 0; i < 2; i++ {
				if response := admin("DELETE", "/api/admin/products/"+product); response.Code != 200 {
					t.Fatalf("商品删除或重试失败: %s", response.Body)
				}
			}
			var products []Product
			if response := admin("GET", "/api/admin/products"); json.Unmarshal(response.Body.Bytes(), &products) != nil || len(products) != 1 || products[0].ID != other {
				t.Fatal("商品删除范围不正确")
			}
			public := func(path, token string) *httptest.ResponseRecorder {
				request := httptest.NewRequest("GET", path, nil)
				if token != "" {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				response := httptest.NewRecorder()
				a.routes().ServeHTTP(response, request)
				return response
			}
			if response := public("/api/products", ""); json.Unmarshal(response.Body.Bytes(), &products) != nil || len(products) != 1 || products[0].ID != other {
				t.Fatal("商城列表仍返回已删除商品或丢失其他商品")
			}
			if response := public("/api/products/"+product, ""); response.Code != 404 {
				t.Fatal("已删除商品仍可通过旧链接访问")
			}
			stale := `{"name":"旧编辑窗口","price":1234,"currency":"CNY","inventory_id":"` + product + `","active":true}`
			if response := admin("PUT", "/api/admin/products/"+product, stale); response.Code != 404 {
				t.Fatal("旧编辑窗口可以重新保存已删除商品")
			}
			if response := admin("GET", "/api/admin/inventories/"+product+"/stock"); response.Code != 200 {
				t.Fatal("删除商品破坏了独立库存管理")
			}
			if response := admin("POST", "/api/orders", `{"product_id":"`+product+`","quantity":1}`); response.Code != 400 {
				t.Fatal("已删除商品仍能创建新订单")
			}
			var overview map[string]int
			expectedStock := 0
			if kind == "card" {
				expectedStock = 2
			}
			response := admin("GET", "/api/admin/overview")
			if json.Unmarshal(response.Body.Bytes(), &overview) != nil || overview["products"] != 1 || overview["stock"] != expectedStock || overview["orders"] != 2 || overview["paid"] != 1 {
				t.Fatal("删除后的商品、库存或历史订单统计不正确")
			}
			remaining, err := a.loadOrder(context.Background(), pending.ID)
			if err != nil || remaining.PaymentStatus != "pending" || remaining.ReservationStatus != "reserved" {
				t.Fatal("删除商品改变了原订单付款或预留状态")
			}
			gateway.set(pending.ID, "paid", "system")
			if _, err = a.db.Exec("UPDATE orders SET next_check=0 WHERE id=?", pending.ID); err != nil {
				t.Fatal(err)
			}
			a.runPending(context.Background())
			for _, claim := range []struct{ id, token string }{{paid.ID, paidToken}, {pending.ID, pendingToken}} {
				var receipt struct {
					Order      Order      `json:"order"`
					Deliveries []Delivery `json:"deliveries"`
				}
				response := public("/api/orders/"+claim.id, claim.token)
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &receipt) != nil || receipt.Order.PaymentStatus != "paid" || receipt.Order.DeliveryStatus != "done" || len(receipt.Deliveries) != 1 || receipt.Deliveries[0].Content == "" {
					t.Fatalf("删除商品破坏了原订单或迟到付款的领取: %s", response.Body)
				}
				if claim.id == paid.ID && receipt.Deliveries[0].Content != before[0].Content {
					t.Fatal("删除商品改变了已交付内容")
				}
				if response := admin("GET", "/api/admin/orders/"+claim.id); response.Code != 200 {
					t.Fatal("对应订单的后台详情丢失")
				}
			}
			var orders struct {
				Items []Order `json:"items"`
			}
			response = admin("GET", "/api/admin/orders")
			if json.Unmarshal(response.Body.Bytes(), &orders) != nil || len(orders.Items) != 2 {
				t.Fatal("对应订单从后台列表丢失")
			}
		})
	}
}

func TestDeletedProductMigrationRetainsVersionSixProducts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version-six.db")
	old, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	for _, file := range []string{"migrations/001_init.sql", "migrations/002_gateway_orders.sql", "migrations/003_gateway_identity.sql", "migrations/004_remove_service_products.sql", "migrations/005_product_details.sql", "migrations/006_hidden_products.sql"} {
		schema, err := tinyfaka.Assets.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = old.Exec(string(schema)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = old.Exec(`INSERT INTO products(id,name,details,price,currency,kind,active,hidden,content,created_at,updated_at) VALUES('legacy','旧商品','## 旧详情',1234,'CNY','static',1,1,'原有密文',0,0)`); err != nil {
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
	a := &App{db: db}
	products, err := a.products(false)
	if err != nil || len(products) != 1 || !products[0].Active || !products[0].Hidden || products[0].Content != "原有密文" || !strings.Contains(products[0].Details, "旧详情") {
		t.Fatal("v6 升级改变了已有商品、隐藏状态或内容")
	}
}
