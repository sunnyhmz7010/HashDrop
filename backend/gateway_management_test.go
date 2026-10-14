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

func adminSessionRequest(t *testing.T, a *App) func(string, string, ...string) *httptest.ResponseRecorder {
	t.Helper()
	login := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(`{"username":"admin","password":"test-password-only"}`))
	login.Header.Set("Origin", a.config.PublicURL)
	login.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	a.routes().ServeHTTP(response, login)
	if response.Code != 200 {
		t.Fatalf("登录失败: %s", response.Body)
	}
	var session struct {
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	cookie := response.Result().Cookies()[0]
	return func(method, path string, payload ...string) *httptest.ResponseRecorder {
		body := ""
		if len(payload) != 0 {
			body = payload[0]
		}
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.AddCookie(cookie)
		request.Header.Set("Origin", a.config.PublicURL)
		request.Header.Set("X-CSRF-Token", session.CSRF)
		request.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		a.routes().ServeHTTP(result, request)
		return result
	}
}

func TestDeleteOrderPreservesLatePaymentAndClaim(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	order, token := reserve(t, a, product, 1)
	if err := a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	request := adminSessionRequest(t, a)
	unauthorized := httptest.NewRecorder()
	a.routes().ServeHTTP(unauthorized, httptest.NewRequest("DELETE", "/api/admin/orders/"+order.ID, nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatal("未登录可删除订单")
	}
	for i := 0; i < 2; i++ {
		if result := request("DELETE", "/api/admin/orders/"+order.ID); result.Code != 200 {
			t.Fatalf("删除不可重试: %s", result.Body)
		}
	}
	result := request("GET", "/api/admin/orders")
	var list struct {
		Items []Order `json:"items"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &list); err != nil || len(list.Items) != 0 {
		t.Fatal("已删除订单仍在列表")
	}
	if result := request("GET", "/api/admin/orders/"+order.ID); result.Code != 404 {
		t.Fatal("已删除订单仍能从后台详情打开")
	}
	var reservation string
	if err := a.db.QueryRow("SELECT status FROM stock_items WHERE product_id=?", product).Scan(&reservation); err != nil || reservation != "reserved" {
		t.Fatal("删除提前释放了库存")
	}
	if result := request("PUT", "/api/admin/settings", `{"title":"TinyFaka","hashpay_url":"https://other.example","merchant_id":"other"}`); result.Code != 400 || !strings.Contains(result.Body.String(), "存在历史订单") {
		t.Fatal("删除后仍待付款的订单未保护原支付商户映射")
	}
	gateway.set(order.ID, "paid", "admin")
	// 删除不会取消补查；由原任务核验网关付款并完成原交付。
	if _, err := a.db.Exec("UPDATE orders SET next_check=0 WHERE id=?", order.ID); err != nil {
		t.Fatal(err)
	}
	a.runPending(context.Background())
	claim := httptest.NewRequest("GET", "/api/orders/"+order.ID, nil)
	claim.Header.Set("Authorization", "Bearer "+token)
	receipt := httptest.NewRecorder()
	a.routes().ServeHTTP(receipt, claim)
	if receipt.Code != 200 || !strings.Contains(receipt.Body.String(), "卡密-") {
		t.Fatalf("删除破坏了迟到付款领取: %s", receipt.Body)
	}
	if result := request("POST", "/api/admin/orders/"+order.ID+"/release"); result.Code != 404 && result.Code != 405 {
		t.Fatalf("已移除的手动关闭接口未拒绝请求: %d %s", result.Code, result.Body)
	}
}

func TestGatewayPaidDoesNotRequireConfirmationMetadata(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "static", 0)
	order, _ := reserve(t, a, product, 1)
	if err := a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	gateway.set(order.ID, "paid", "")
	if err := a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	paid, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || paid.PaymentStatus != "paid" || paid.DeliveryStatus != "done" {
		t.Fatal("网关已付款被本地确认来源策略拦截")
	}
	// 已付款订单的手动刷新也必须查询网关，不能直接返回本地缓存。
	gateway.mu.Lock()
	gateway.corrupt = "amount"
	gateway.mu.Unlock()
	if err = a.syncOrder(context.Background(), order.ID); err == nil {
		t.Fatal("已付款刷新绕过了网关查询和金额核验")
	}
}

func TestVersionOneMigrationRetainsOrdersStockAndClaims(t *testing.T) {
	a, gateway := newTestApp(t)
	path := filepath.Join(t.TempDir(), "version-one.db")
	old, err := sql.Open("sqlite", databaseURI(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	old.SetMaxOpenConns(1)
	schema, err := tinyfaka.Assets.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	a.db.Close()
	a.db = old
	product := insertProduct(t, a, "card", 3)
	paid, _ := reserve(t, a, product, 1)
	syncPaid(t, a, gateway, paid)
	pending, _ := reserve(t, a, product, 1)
	before, err := a.deliveryResults(context.Background(), paid.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	a.db, err = openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.db.Close() })
	after, err := a.deliveryResults(context.Background(), paid.ID)
	if err != nil || len(after) != 1 || after[0].Content != before[0].Content {
		t.Fatal("升级破坏了已交付密文或领取结果")
	}
	remaining, err := a.loadOrder(context.Background(), pending.ID)
	if err != nil || remaining.ReservationStatus != "reserved" {
		t.Fatal("升级破坏了未完成订单")
	}
	var version, foreignKeys, stock int
	if err = a.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal("数据库版本未升级")
	}
	if err = a.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatal("升级后未恢复外键约束")
	}
	if err = a.db.QueryRow("SELECT COUNT(*) FROM stock_items WHERE product_id=?", product).Scan(&stock); err != nil || stock != 3 {
		t.Fatal("升级丢失库存")
	}
	if _, err = a.db.Exec("DELETE FROM orders WHERE id=?", paid.ID); err == nil {
		t.Fatal("升级损坏了订单与库存/交付外键")
	}
}
