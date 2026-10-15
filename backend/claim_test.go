package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func claimRequest(t *testing.T, a *App, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	return w
}

func TestClaimKeyLookupAndPayment(t *testing.T) {
	a, g := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	key := " 我的领取密钥🔑/abc?x=1&z=2 "
	id := newID()
	created := claimRequest(t, a, "/api/orders", map[string]any{"product_id": product, "quantity": 1, "key": key, "request_id": id, "quoted_total": 1234, "quoted_currency": "CNY"})
	if created.Code != 201 || strings.Contains(created.Body.String(), key) {
		t.Fatalf("创建订单: %d %s", created.Code, created.Body)
	}
	o, err := a.loadOrder(context.Background(), id)
	if err != nil || o.TokenHash != tokenHash(key) {
		t.Fatalf("凭证哈希未保存: %v", err)
	}
	lookup := func(key string) *httptest.ResponseRecorder {
		return claimRequest(t, a, "/api/orders/lookup", map[string]string{"key": key})
	}
	if response := lookup(strings.TrimSpace(key)); response.Code != 404 {
		t.Fatalf("空格被静默修改: %d", response.Code)
	}
	if response := lookup(key); response.Code != 200 || strings.Contains(response.Body.String(), "卡密-0") {
		t.Fatal("付款前泄漏交付内容或查询失败")
	}
	syncPaid(t, a, g, o)
	paid := lookup(key)
	if paid.Code != 200 || !strings.Contains(paid.Body.String(), "卡密-0") || paid.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("密钥领取未返回已核验交付")
	}
	if response := lookup("其他密钥"); response.Code != 404 || strings.Contains(response.Body.String(), "卡密-0") {
		t.Fatal("错误密钥可读取交付内容")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, returnURL := range g.returnURLs {
		if returnURL != a.config.PublicURL+"/claim" {
			t.Fatalf("支付返回地址包含额外凭证: %q", returnURL)
		}
	}
}

func TestClaimKeyConflictIsAtomic(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "card", 5)
	key := strings.Repeat("钥", 128)
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes, conflicts := 0, 0
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.reserveOrder(context.Background(), product, 1, key, newID())
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
			} else if errors.Is(err, errClaimKeyUsed) {
				conflicts++
			} else {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if successes != 1 || conflicts != 11 {
		t.Fatalf("并发下单: success=%d conflict=%d", successes, conflicts)
	}
	var reserved int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM stock_items WHERE status='reserved'").Scan(&reserved); err != nil || reserved != 1 {
		t.Fatal("重复密钥额外占用了库存")
	}
	response := claimRequest(t, a, "/api/orders", map[string]any{"product_id": product, "quantity": 1, "key": key, "quoted_total": 1234, "quoted_currency": "CNY"})
	var result struct {
		Code        string   `json:"code"`
		Suggestions []string `json:"suggestions"`
	}
	if response.Code != 409 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Code != "key_conflict" || len(result.Suggestions) != 3 {
		t.Fatalf("重复密钥未返回候选: %s", response.Body)
	}
	seen := map[string]bool{}
	for _, candidate := range result.Suggestions {
		if utf8.RuneCountInString(candidate) > 128 || candidate == key || seen[candidate] {
			t.Fatal("派生密钥超长或重复")
		}
		seen[candidate] = true
		if _, err := a.reserveOrder(context.Background(), product, 1, candidate, newID()); err != nil {
			t.Fatalf("派生密钥不可用: %v", err)
		}
	}
	var id string
	if err := a.db.QueryRow("SELECT id FROM orders WHERE token_hash=?", tokenHash(key)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := a.terminateOrder(context.Background(), id, "closed", "测试终止"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.reserveOrder(context.Background(), product, 1, key, newID()); !errors.Is(err, errClaimKeyUsed) {
		t.Fatal("关闭订单的密钥被重复使用")
	}
}

func TestPurchaseRetryReusesReservationAndGatewayOrder(t *testing.T) {
	a, g := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	id, key := newID(), "a"
	input := map[string]any{"product_id": product, "quantity": 1, "key": key, "request_id": id, "quoted_total": 1234, "quoted_currency": "CNY"}
	if response := claimRequest(t, a, "/api/orders", input); response.Code != 201 {
		t.Fatal(response.Body)
	}
	g.failCreateOnce = true
	if err := a.syncOrder(context.Background(), id); err == nil {
		t.Fatal("未模拟网关结果不明")
	}
	if _, err := a.db.Exec("UPDATE products SET active=0 WHERE id=?", product); err != nil {
		t.Fatal(err)
	}
	if response := claimRequest(t, a, "/api/orders", input); response.Code != 201 {
		t.Fatalf("重试错误重新校验已预留库存: %s", response.Body)
	}
	var next int64
	if err := a.db.QueryRow("SELECT next_check FROM orders WHERE id=?", id).Scan(&next); err != nil || next != 0 {
		t.Fatal("重试没有唤醒支付单创建")
	}
	a.runPending(context.Background())
	o, err := a.loadOrder(context.Background(), id)
	if err != nil || o.CheckoutURL == "" || o.PaymentStatus != "pending" || g.creates != 1 {
		t.Fatalf("网关重试产生额外订单: %v", err)
	}
	var orders, reserved, deliveries int
	if err := a.db.QueryRow("SELECT (SELECT COUNT(*) FROM orders),(SELECT COUNT(*) FROM stock_items WHERE status='reserved'),(SELECT COUNT(*) FROM deliveries)").Scan(&orders, &reserved, &deliveries); err != nil || orders != 1 || reserved != 1 || deliveries != 1 {
		t.Fatal("重试重复预留或交付")
	}
	input["key"] = "wrong"
	if response := claimRequest(t, a, "/api/orders", input); response.Code != 409 || strings.Contains(response.Body.String(), o.CheckoutURL) {
		t.Fatal("相同请求标识可绕过密钥认证")
	}
}

func TestClaimKeyValidationAndFailureLimit(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "static", 0)
	id, key := newID(), "Short"
	if _, err := a.reserveOrder(context.Background(), product, 1, key, id); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", " \t\n", strings.Repeat("🔑", 129)} {
		if err := validateClaimKey(invalid); err == nil {
			t.Fatal("无效密钥通过校验")
		}
	}
	if response := claimRequest(t, a, "/api/orders/lookup", map[string]string{"key": "short"}); response.Code != 404 {
		t.Fatal("密钥未区分大小写")
	}
	for range 9 {
		if response := claimRequest(t, a, "/api/orders/lookup", map[string]string{"key": "wrong"}); response.Code != 404 {
			t.Fatalf("意外限流: %d", response.Code)
		}
	}
	if response := claimRequest(t, a, "/api/orders/lookup", map[string]string{"key": key}); response.Code != http.StatusTooManyRequests {
		t.Fatal("失败限流未覆盖查询接口")
	}
	r := httptest.NewRequest("GET", "/api/orders/"+id, nil)
	r.Header.Set("Authorization", "Bearer "+randomToken())
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != 429 {
		t.Fatal("旧查询接口可绕过失败限流")
	}
	a.limits.mu.Lock()
	for entry, value := range a.limits.entries {
		if strings.HasPrefix(entry, "claim-failed:") {
			value.until = time.Now().Add(-time.Second)
			a.limits.entries[entry] = value
		}
	}
	a.limits.mu.Unlock()
	if response := claimRequest(t, a, "/api/orders/lookup", map[string]string{"key": key}); response.Code != 200 {
		t.Fatal("失败限流过期后未恢复")
	}
}

func TestConcurrentClaimFailureLimit(t *testing.T) {
	a, _ := newTestApp(t)
	var wg sync.WaitGroup
	statuses := make(chan int, 30)
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- claimRequest(t, a, "/api/orders/lookup", map[string]string{"key": "wrong"}).Code
		}()
	}
	wg.Wait()
	close(statuses)
	failed, limited := 0, 0
	for status := range statuses {
		switch status {
		case 404:
			failed++
		case 429:
			limited++
		default:
			t.Fatalf("意外状态: %d", status)
		}
	}
	if failed != 10 || limited != 20 {
		t.Fatalf("并发突破失败预算: failed=%d limited=%d", failed, limited)
	}
}

func TestPricedCheckoutRetryPreservesDiscountSnapshot(t *testing.T) {
	a, g := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	rule := promotionRule()
	rule.Kind, rule.Code, rule.UsageLimit = "code", "ONCE", 1
	promotion := saveTestPromotion(t, a, rule, "")
	quote, err := testQuote(t, a, product, 1, "ONCE", unixNow())
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"product_id": product, "quantity": 1, "key": "discount-retry", "request_id": newID(), "coupon_code": "ONCE", "quoted_total": quote.Total, "quoted_currency": quote.Currency}
	first := claimRequest(t, a, "/api/orders", input)
	if first.Code != 201 {
		t.Fatal(first.Body)
	}
	var created struct {
		Order Order `json:"order"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE promotions SET enabled=0,name='后来修改的优惠' WHERE id=?", promotion); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE products SET price=50000 WHERE id=?", product); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		response := claimRequest(t, a, "/api/orders", input)
		if response.Code != 201 {
			t.Fatalf("重试重新消耗优惠或重新报价: %s", response.Body)
		}
	}
	var uses int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM order_promotions WHERE promotion_id=?", promotion).Scan(&uses); err != nil || uses != 1 {
		t.Fatal("同单重试重复占用优惠次数")
	}
	syncPaid(t, a, g, created.Order)
	response := claimRequest(t, a, "/api/orders/lookup", map[string]string{"key": "discount-retry"})
	var result struct {
		Order      Order      `json:"order"`
		Deliveries []Delivery `json:"deliveries"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Order.Total != quote.Total || len(result.Order.Discounts) != 1 || result.Order.Discounts[0].Name != rule.Name || len(result.Deliveries) != 1 || result.Deliveries[0].Content != "卡密-0" {
		t.Fatal("密钥查询丢失原优惠快照或交付")
	}
}

func TestCheckoutRequiresQuoteAndRejectsExpiredCoupon(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	input := map[string]any{"product_id": product, "quantity": 1, "key": "quote-required", "request_id": newID()}
	if response := claimRequest(t, a, "/api/orders", input); response.Code != 400 {
		t.Fatal("公开下单可省略确认报价")
	}
	rule := promotionRule()
	rule.Kind, rule.Code = "code", "EXPIRE"
	id := saveTestPromotion(t, a, rule, "")
	quote, err := testQuote(t, a, product, 1, rule.Code, unixNow())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE promotions SET enabled=0 WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	input["quoted_total"], input["quoted_currency"], input["coupon_code"] = quote.Total, quote.Currency, rule.Code
	if response := claimRequest(t, a, "/api/orders", input); response.Code != 409 {
		t.Fatalf("失效优惠未要求重新确认: %d %s", response.Code, response.Body)
	}
	var orders, reserved, uses int
	if err := a.db.QueryRow("SELECT (SELECT COUNT(*) FROM orders),(SELECT COUNT(*) FROM stock_items WHERE status='reserved'),(SELECT COUNT(*) FROM order_promotions)").Scan(&orders, &reserved, &uses); err != nil || orders != 0 || reserved != 0 || uses != 0 {
		t.Fatal("无效报价占用了订单、库存或优惠")
	}
}
