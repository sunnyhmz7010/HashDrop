package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func promotionRule() promotionInput {
	return promotionInput{Name: "常驻八折", Kind: "automatic", DiscountType: "percentage", Value: 2000, MinQuantity: 1, Scope: "all", Enabled: true}
}

func saveTestPromotion(t *testing.T, a *App, input promotionInput, id string) string {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	method, path := "POST", "/api/admin/promotions"
	if id != "" {
		method, path = "PUT", path+"/"+id
	}
	response := adminSessionRequest(t, a)(method, path, string(body))
	var saved struct {
		ID string `json:"id"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &saved) != nil || saved.ID == "" {
		t.Fatalf("保存优惠失败: %d %s", response.Code, response.Body)
	}
	return saved.ID
}

func testQuote(t *testing.T, a *App, product string, quantity int, code string, now int64) (OrderQuote, error) {
	t.Helper()
	p, err := scanProduct(a.db.QueryRow(productSelect+" WHERE p.id=?", product))
	if err != nil {
		t.Fatal(err)
	}
	return quoteOrder(context.Background(), a.db, p, quantity, code, now)
}

func TestPromotionRequiresBothStackingPermissions(t *testing.T) {
	for _, activityStack := range []bool{false, true} {
		for _, couponStack := range []bool{false, true} {
			t.Run(fmt.Sprintf("activity-%t/coupon-%t", activityStack, couponStack), func(t *testing.T) {
				a, _ := newTestApp(t)
				product := insertProduct(t, a, "static", 0)
				if _, err := a.db.Exec("UPDATE products SET price=10000 WHERE id=?", product); err != nil {
					t.Fatal(err)
				}
				rule := promotionRule()
				rule.Stackable = activityStack
				saveTestPromotion(t, a, rule, "")
				rule.Name, rule.Kind, rule.Code, rule.Value, rule.Stackable = "九折码", "code", "SAVE10", 1000, couponStack
				saveTestPromotion(t, a, rule, "")
				quote, err := testQuote(t, a, product, 1, " save10 ", unixNow())
				want, count := int64(8000), 1
				if activityStack && couponStack {
					want, count = 7200, 2
				}
				if err != nil || quote.Total != want || len(quote.Discounts) != count || quote.DiscountTotal != 10000-want {
					t.Fatalf("叠加许可未正确执行: %+v %v", quote, err)
				}
			})
		}
	}
}

func TestPromotionChoosesBestEligibleCombination(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "static", 0)
	if _, err := a.db.Exec("UPDATE products SET price=10000 WHERE id=?", product); err != nil {
		t.Fatal(err)
	}
	rule := promotionRule()
	rule.Stackable = true
	saveTestPromotion(t, a, rule, "")
	rule.Name, rule.Kind, rule.Code = "可叠加八折码", "code", "SAVE20"
	saveTestPromotion(t, a, rule, "")
	rule.Name, rule.Kind, rule.Code, rule.Value, rule.Stackable = "独占七折", "automatic", "", 3000, false
	saveTestPromotion(t, a, rule, "")
	quote, err := testQuote(t, a, product, 1, "SAVE20", unixNow())
	if err != nil || quote.Total != 6400 || len(quote.Discounts) != 2 {
		t.Fatalf("没有比较较小活动与优惠码的组合: %+v %v", quote, err)
	}
	quote, err = testQuote(t, a, product, 1, "", unixNow())
	if err != nil || quote.Total != 7000 || len(quote.Discounts) != 1 {
		t.Fatal("未输入优惠码时自动优惠不正确")
	}
	public := httptest.NewRecorder()
	a.routes().ServeHTTP(public, httptest.NewRequest("GET", "/api/products", nil))
	if public.Code != 200 || !strings.Contains(public.Body.String(), `"sale_price":7000`) || strings.Contains(public.Body.String(), "SAVE20") {
		t.Fatal("商城活动价不正确或公开了优惠码")
	}
}

func TestPromotionScopeScheduleThresholdAndCurrency(t *testing.T) {
	a, _ := newTestApp(t)
	first := insertProduct(t, a, "static", 0)
	second := insertProduct(t, a, "static", 0)
	third := insertProduct(t, a, "static", 0)
	rule := promotionRule()
	rule.Kind, rule.Code, rule.Scope, rule.ProductIDs = "code", "TARGET", "selected", []string{first, second}
	rule.DiscountType, rule.Currency, rule.Value, rule.MinTotal, rule.MinQuantity = "fixed", "CNY", 100, 2000, 2
	rule.StartsAt, rule.EndsAt = 1000, 2000
	id := saveTestPromotion(t, a, rule, "")
	for _, sample := range []struct {
		product  string
		quantity int
		now      int64
		valid    bool
	}{
		{first, 2, 1000, true}, {second, 2, 1999, true}, {third, 2, 1500, false},
		{first, 1, 1500, false}, {first, 2, 999, false}, {first, 2, 2000, false},
	} {
		quote, err := testQuote(t, a, sample.product, sample.quantity, "target", sample.now)
		if (err == nil) != sample.valid || (sample.valid && (quote.Total != 2368 || quote.DiscountTotal != 100)) {
			t.Fatalf("适用范围/边界错误: %+v %+v %v", sample, quote, err)
		}
	}
	rule.Scope, rule.ProductIDs, rule.MinTotal, rule.MinQuantity = "all", nil, 0, 1
	saveTestPromotion(t, a, rule, id)
	if _, err := testQuote(t, a, third, 1, "TARGET", 1500); err != nil {
		t.Fatal("全局优惠未覆盖其他商品")
	}
	if _, err := a.db.Exec("UPDATE products SET currency='USD' WHERE id=?", third); err != nil {
		t.Fatal(err)
	}
	if _, err := testQuote(t, a, third, 1, "TARGET", 1500); err == nil {
		t.Fatal("固定金额跨币种使用")
	}
	rule.Enabled = false
	saveTestPromotion(t, a, rule, id)
	if _, err := testQuote(t, a, first, 2, "TARGET", 1500); err == nil {
		t.Fatal("停用优惠仍被使用")
	}
	if promotionAmount(Promotion{promotionInput: promotionInput{DiscountType: "percentage", Value: 3333}}, 101) != 33 || promotionAmount(Promotion{promotionInput: promotionInput{DiscountType: "fixed", Value: 1000}}, 100) != 99 {
		t.Fatal("金额舍入或最低实付错误")
	}
}

func TestPromotionUseLimitIsAtomicAndSurvivesDeletionAndExpiration(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "card", 8)
	rule := promotionRule()
	rule.Kind, rule.Code, rule.UsageLimit = "code", "ONCE", 1
	saveTestPromotion(t, a, rule, "")
	if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 9, CouponCode: "ONCE"}); err == nil {
		t.Fatal("超出库存仍创建了优惠订单")
	}
	if _, err := testQuote(t, a, product, 1, "ONCE", unixNow()); err != nil {
		t.Fatal("库存预留失败没有回滚优惠名额", err)
	}
	var count atomic.Int32
	var lock sync.Mutex
	var winner Order
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			order, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 1, CouponCode: "ONCE"})
			if err == nil {
				count.Add(1)
				lock.Lock()
				winner = order
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("一次优惠被并发使用 %d 次", count.Load())
	}
	var orders, reserved int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT COUNT(*) FROM stock_items WHERE status='reserved'").Scan(&reserved); err != nil || orders != 1 || reserved != 1 {
		t.Fatal("失败的用码尝试占用了订单或库存")
	}
	if err := a.syncOrder(context.Background(), winner.ID); err != nil {
		t.Fatal(err)
	}
	admin := adminSessionRequest(t, a)
	if response := admin("DELETE", "/api/admin/orders/"+winner.ID); response.Code != 200 {
		t.Fatal(response.Body)
	}
	gateway.set(winner.ID, "expired", "")
	if err := a.syncOrder(context.Background(), winner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testQuote(t, a, product, 1, "ONCE", unixNow()); err == nil {
		t.Fatal("删除或过期归还了仍可能付款的优惠名额")
	}
	gateway.mu.Lock()
	delete(gateway.orders, winner.ID)
	gateway.mu.Unlock()
	if err := a.syncOrder(context.Background(), winner.ID); err != nil {
		t.Fatal(err)
	}
	next, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 1, CouponCode: "ONCE"})
	if err != nil {
		t.Fatal("可靠关闭后未归还优惠名额", err)
	}
	syncPaid(t, a, gateway, next)
	if response := admin("DELETE", "/api/admin/orders/"+next.ID); response.Code != 200 {
		t.Fatal(response.Body)
	}
	if _, err := testQuote(t, a, product, 1, "ONCE", unixNow()); err == nil {
		t.Fatal("删除已付款订单错误归还优惠名额")
	}
	// 曾经核验过付款的优惠保持消费，即使网关后来异常变为失效。
	gateway.set(next.ID, "invalid", "")
	if err := a.syncOrder(context.Background(), next.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testQuote(t, a, product, 1, "ONCE", unixNow()); err == nil {
		t.Fatal("已付款订单的后续失效错误归还了已消费优惠")
	}
}

func TestPromotionPriceSnapshotAndGatewayVerification(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	if _, err := a.db.Exec("UPDATE products SET price=10000 WHERE id=?", product); err != nil {
		t.Fatal(err)
	}
	rule := promotionRule()
	rule.Stackable = true
	activity := saveTestPromotion(t, a, rule, "")
	rule.Name, rule.Kind, rule.Code, rule.Value = "下单九折码", "code", "SAVE10", 1000
	coupon := saveTestPromotion(t, a, rule, "")
	wrong := int64(1)
	if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 1, CouponCode: "SAVE10", QuotedTotal: &wrong, QuotedCurrency: "CNY"}); err == nil {
		t.Fatal("接受了伪造实付金额")
	}
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&count); err != nil || count != 0 {
		t.Fatal("金额变化仍创建了订单")
	}
	total := int64(7200)
	if _, _, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 1, CouponCode: "SAVE10", QuotedTotal: &total, QuotedCurrency: "USD"}); err == nil {
		t.Fatal("报价币种变化却未要求重新确认")
	}
	order, token, err := a.reservePricedOrder(context.Background(), orderInput{ProductID: product, Quantity: 1, CouponCode: "SAVE10", QuotedTotal: &total, QuotedCurrency: "CNY"})
	if err != nil {
		t.Fatal(err)
	}
	rule.Kind, rule.Code, rule.Name, rule.Value = "automatic", "", "后来修改的五折", 5000
	saveTestPromotion(t, a, rule, activity)
	admin := adminSessionRequest(t, a)
	if response := admin("DELETE", "/api/admin/promotions/"+coupon); response.Code != 200 {
		t.Fatal(response.Body)
	}
	if err = a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	gateway.mu.Lock()
	remote := gateway.orders[order.ID]
	gateway.corrupt = "amount"
	gateway.mu.Unlock()
	if remote.Amount.String() != "72.00" {
		t.Fatalf("向 HashPay 提交了错误的优惠后金额: %s", remote.Amount)
	}
	if err = a.syncOrder(context.Background(), order.ID); err == nil {
		t.Fatal("优惠订单绕过了支付金额核验")
	}
	gateway.mu.Lock()
	gateway.corrupt = ""
	gateway.mu.Unlock()
	gateway.set(order.ID, "paid", "system")
	if err = a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || saved.Total != 7200 || saved.OriginalTotal != 10000 || saved.DiscountTotal != 2800 || len(saved.Discounts) != 2 || saved.Discounts[0].Name != "常驻八折" || saved.Discounts[1].Name != "下单九折码" {
		t.Fatalf("优惠变更破坏了订单快照: %+v %v", saved, err)
	}
	claim := httptest.NewRequest("GET", "/api/orders/"+order.ID, nil)
	claim.Header.Set("Authorization", claimAuthorization(token))
	response := httptest.NewRecorder()
	a.routes().ServeHTTP(response, claim)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "卡密-0") {
		t.Fatal("优惠订单无法领取原库存")
	}
}

func TestPromotionAdminValidationAndDeletion(t *testing.T) {
	a, _ := newTestApp(t)
	rule := promotionRule()
	rule.Kind, rule.Code = "code", "SAVE20"
	response := httptest.NewRecorder()
	a.routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/admin/promotions", nil))
	if response.Code != 401 {
		t.Fatal("未登录可管理优惠")
	}
	id := saveTestPromotion(t, a, rule, "")
	admin := adminSessionRequest(t, a)
	for _, sample := range []struct {
		name   string
		change func(*promotionInput)
	}{
		{"duplicate", func(p *promotionInput) { p.Code = " save20 " }},
		{"empty-selection", func(p *promotionInput) { p.Code = "EMPTY"; p.Scope = "selected" }},
		{"currency-required", func(p *promotionInput) { p.Code = "FIXED"; p.DiscountType = "fixed" }},
		{"percentage", func(p *promotionInput) { p.Code = "ZERO"; p.Value = 10000 }},
		{"date-range", func(p *promotionInput) { p.Code = "DATE"; p.StartsAt = 2000; p.EndsAt = 1000 }},
	} {
		t.Run(sample.name, func(t *testing.T) {
			input := rule
			sample.change(&input)
			body, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if result := admin("POST", "/api/admin/promotions", string(body)); result.Code != 400 {
				t.Fatalf("接受了无效优惠: %d %s", result.Code, result.Body)
			}
		})
	}
	for i := 0; i < 2; i++ {
		if result := admin("DELETE", "/api/admin/promotions/"+id); result.Code != 200 {
			t.Fatal("优惠删除不可重试", result.Body)
		}
	}
	body, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	if result := admin("PUT", "/api/admin/promotions/"+id, string(body)); result.Code != 404 {
		t.Fatal("旧编辑器恢复了已删除优惠")
	}
	if next := saveTestPromotion(t, a, rule, ""); next == id {
		t.Fatal("重新派发的优惠复用了历史规则 ID")
	}
}
