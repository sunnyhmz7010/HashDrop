package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestProductChineseTextLimits(t *testing.T) {
	a, _ := newTestApp(t)
	admin := adminSessionRequest(t, a)
	name := strings.Repeat("商", 119) + "📦"
	description := strings.Repeat("介", 4000)
	input := map[string]any{"name": name, "description": description, "price": 100, "currency": "CNY", "active": false}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	response := admin("POST", "/api/admin/products", string(body))
	var product Product
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &product) != nil || product.Name != name || product.Description != description {
		t.Fatalf("字符上限内的中文商品未原样保存: %d %s", response.Code, response.Body)
	}
	for _, field := range []string{"name", "description"} {
		t.Run(field, func(t *testing.T) {
			original := input[field]
			input[field] = original.(string) + "多"
			defer func() { input[field] = original }()
			body, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if rejected := admin("PUT", "/api/admin/products/"+product.ID, string(body)); rejected.Code != http.StatusBadRequest {
				t.Fatalf("超出字符上限的 %s 未被拒绝: %d %s", field, rejected.Code, rejected.Body)
			}
		})
	}
	stored, err := scanProduct(a.db.QueryRow(productSelect+" WHERE p.id=?", product.ID))
	if err != nil || stored.Name != name || stored.Description != description {
		t.Fatal("被拒绝的修改改变了原商品文本", err)
	}
}

func TestInventoryChineseTextLimits(t *testing.T) {
	a, _ := newTestApp(t)
	admin := adminSessionRequest(t, a)
	name := strings.Repeat("库", 119) + "📦"
	content := strings.Repeat("文", 65536)
	input := map[string]string{"name": name, "kind": "static", "static_content": content}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	response := admin("POST", "/api/admin/inventories", string(body))
	var pool Inventory
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &pool) != nil || pool.Name != name || !pool.HasContent {
		t.Fatalf("字符上限内的中文库存池未保存: %d %s", response.Code, response.Body)
	}
	for _, field := range []string{"name", "static_content"} {
		t.Run(field, func(t *testing.T) {
			original := input[field]
			input[field] = original + "多"
			defer func() { input[field] = original }()
			body, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if rejected := admin("PUT", "/api/admin/inventories/"+pool.ID, string(body)); rejected.Code != http.StatusBadRequest {
				t.Fatalf("超出字符上限的 %s 未被拒绝: %d %s", field, rejected.Code, rejected.Body)
			}
		})
	}
	var storedName, cipher string
	if err := a.db.QueryRow("SELECT name,content FROM inventories WHERE id=?", pool.ID).Scan(&storedName, &cipher); err != nil {
		t.Fatal(err)
	}
	plain, err := a.vault.decrypt(cipher, "inventory:"+pool.ID)
	if err != nil || storedName != name || plain != content {
		t.Fatal("固定文本未原样保存或被拒绝的修改改变了库存池", err)
	}
}

func TestPromotionChineseNameLimit(t *testing.T) {
	a, _ := newTestApp(t)
	admin := adminSessionRequest(t, a)
	rule := promotionRule()
	rule.Name = strings.Repeat("惠", 119) + "🎁"
	id := saveTestPromotion(t, a, rule, "")
	name := rule.Name
	rule.Name += "多"
	body, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	if rejected := admin("PUT", "/api/admin/promotions/"+id, string(body)); rejected.Code != http.StatusBadRequest {
		t.Fatalf("超过 120 字符的优惠名称未被拒绝: %d %s", rejected.Code, rejected.Body)
	}
	var stored string
	if err := a.db.QueryRow("SELECT name FROM promotions WHERE id=?", id).Scan(&stored); err != nil || stored != name {
		t.Fatal("中文优惠名称未原样保存或被拒绝的修改覆盖", err)
	}
}
