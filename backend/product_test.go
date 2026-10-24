package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductDetailsPersistAndDoNotBloatCatalog(t *testing.T) {
	a, _ := newTestApp(t)
	request := adminSessionRequest(t, a)
	inventory := createInventoryViaAPI(t, a, "card", "")
	details := "## 商品介绍\n\n**加粗内容**\n\n![图片](https://example.com/image.png)\n\n![视频](https://example.com/video.webm)"
	input := map[string]any{"name": "详情商品", "description": "卡片简介 <保留原文>", "details": details, "price": 1234, "currency": "CNY", "active": true}
	body, _ := json.Marshal(input)
	created := request("POST", "/api/admin/products", string(body))
	if created.Code != http.StatusOK {
		t.Fatalf("详情商品保存失败: %s", created.Body)
	}
	var product Product
	if err := json.Unmarshal(created.Body.Bytes(), &product); err != nil || product.Details != details {
		t.Fatal("详情未原样保存")
	}
	bindInventoryProducts(t, a, inventory.ID, product.ID)
	read := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		a.routes().ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		return response
	}
	public := read("/api/products/" + product.ID)
	var fetched Product
	if err := json.Unmarshal(public.Body.Bytes(), &fetched); err != nil || fetched.Details != details || fetched.Description != input["description"] {
		t.Fatal("公开详情读取不一致")
	}
	var catalog []Product
	if err := json.Unmarshal(read("/api/products").Body.Bytes(), &catalog); err != nil || len(catalog) != 1 || catalog[0].Details != "" || catalog[0].Description != input["description"] {
		t.Fatal("商城列表泄入完整长详情或丢失简介")
	}
	delete(input, "details")
	input["description"] = "修改后的简介"
	body, _ = json.Marshal(input)
	updated := request("PUT", "/api/admin/products/"+product.ID, string(body))
	if updated.Code != 200 {
		t.Fatal(updated.Body)
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &fetched); err != nil || fetched.Details != details {
		t.Fatal("仅修改介绍时清空了已有详情")
	}
	input["details"] = strings.Repeat("详", 65536)
	body, _ = json.Marshal(input)
	if accepted := request("PUT", "/api/admin/products/"+product.ID, string(body)); accepted.Code != 200 {
		t.Fatal("按字符限制的中文详情未被接受")
	}
	input["details"] = strings.Repeat("详", 65537)
	body, _ = json.Marshal(input)
	if rejected := request("PUT", "/api/admin/products/"+product.ID, string(body)); rejected.Code != 400 {
		t.Fatal("过长详情未被拒绝")
	}
	input["details"] = ""
	body, _ = json.Marshal(input)
	cleared := request("PUT", "/api/admin/products/"+product.ID, string(body))
	if cleared.Code != 200 {
		t.Fatal(cleared.Body)
	}
	if err := json.Unmarshal(cleared.Body.Bytes(), &fetched); err != nil || fetched.Details != "" {
		t.Fatal("主动清空详情未生效")
	}
}

func TestHiddenProductCanBePurchasedWithoutAppearingInCatalog(t *testing.T) {
	a, _ := newTestApp(t)
	admin := adminSessionRequest(t, a)
	hiddenID := insertProduct(t, a, "card", 3)
	visibleID := insertProduct(t, a, "static", 0)
	input := map[string]any{"name": "链接商品", "price": 1234, "currency": "CNY", "active": true, "hidden": true}
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
		t.Fatal("仅修改名称时清除了隐藏设置")
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
					request.Header.Set("Authorization", claimAuthorization(token))
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
			stale := `{"name":"旧编辑窗口","price":1234,"currency":"CNY","active":true}`
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
