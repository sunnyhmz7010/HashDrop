package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductDetailsPersistAndDoNotBloatCatalog(t *testing.T) {
	a, _ := newTestApp(t)
	request := adminSessionRequest(t, a)
	details := "## 商品介绍\n\n**加粗内容**\n\n![图片](https://example.com/image.png)\n\n![视频](https://example.com/video.webm)"
	input := map[string]any{"name": "详情商品", "description": "卡片简介 <保留原文>", "details": details, "price": 1234, "currency": "CNY", "kind": "card", "active": true}
	body, _ := json.Marshal(input)
	created := request("POST", "/api/admin/products", string(body))
	if created.Code != http.StatusOK {
		t.Fatalf("详情商品保存失败: %s", created.Body)
	}
	var product Product
	if err := json.Unmarshal(created.Body.Bytes(), &product); err != nil || product.Details != details {
		t.Fatal("详情未原样保存")
	}
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
		t.Fatal("未填写新字段的旧客户端清空了已有详情")
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
