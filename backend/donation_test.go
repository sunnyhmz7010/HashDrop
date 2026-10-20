package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func donationRequest(a *App, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "ClaimKey "+base64.RawURLEncoding.EncodeToString([]byte(token)))
	}
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	return w
}

func enableDonations(t *testing.T, a *App) func(string, string, ...string) *httptest.ResponseRecorder {
	t.Helper()
	request := adminSessionRequest(t, a)
	result := request("PUT", "/api/admin/donation", `{"enabled":true,"title":"支持开发","description":"**感谢支持**","currencies":[{"currency":"CNY","amounts":[500,1000,2000],"min_amount":100,"max_amount":100000}]}`)
	if result.Code != 200 {
		t.Fatalf("启用赞赏失败: %s", result.Body)
	}
	return request
}

func newDonation(t *testing.T, a *App, amount int64, currency string) (Donation, string) {
	t.Helper()
	w := donationRequest(a, "POST", "/api/donations", fmt.Sprintf(`{"request_id":%q,"nickname":%q,"show_public":false,"amount":%d,"currency":%q}`, newID(), "赞赏者-"+newID(), amount, currency), "")
	if w.Code != 201 {
		t.Fatalf("创建赞赏失败: %d %s", w.Code, w.Body)
	}
	var result struct {
		Donation Donation `json:"donation"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Donation, result.Donation.Nickname
}

func TestDonationSettingsAmountsAndPrivateQuery(t *testing.T) {
	a, _ := newTestApp(t)
	if w := donationRequest(a, "GET", "/api/admin/donation", "", ""); w.Code != 401 {
		t.Fatal("赞赏管理未要求管理员身份")
	}
	if w := donationRequest(a, "GET", "/api/donation", "", ""); w.Code != 200 || strings.Contains(w.Body.String(), "description") || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("默认关闭时暴露了未发布的设置: %s", w.Body)
	}
	admin := enableDonations(t, a)
	for _, sample := range []struct {
		body   string
		status int
	}{
		{`{"amount":0,"currency":"CNY"}`, 400},
		{`{"amount":-1,"currency":"CNY"}`, 400},
		{`{"amount":99,"currency":"CNY"}`, 400},
		{`{"amount":100001,"currency":"CNY"}`, 400},
		{`{"amount":100.5,"currency":"CNY"}`, 400},
		{`{"amount":500,"currency":"USD"}`, 409},
		{`{"amount":500,"currency":"CNY","coupon_code":"FREE"}`, 400},
	} {
		body := strings.Replace(sample.body, "{", fmt.Sprintf(`{"request_id":%q,"nickname":%q,"show_public":false,`, newID(), "金额校验-"+newID()), 1)
		if w := donationRequest(a, "POST", "/api/donations", body, ""); w.Code != sample.status {
			t.Fatalf("无效金额或请求未拒绝: %s: %d %s", sample.body, w.Code, w.Body)
		}
	}
	d, token := newDonation(t, a, 1234, "CNY")
	for _, sample := range []struct {
		token  string
		status int
	}{{"", 401}, {randomToken(), 404}, {token, 200}} {
		w := donationRequest(a, "GET", "/api/donations/"+d.ID, "", sample.token)
		if w.Code != sample.status || strings.Contains(w.Body.String(), "token_hash") || strings.Contains(w.Body.String(), "gateway_merchant_id") {
			t.Fatalf("赞赏查询凭证或信息边界错误: %d %s", w.Code, w.Body)
		}
	}
	var settings DonationSettings
	if err := json.Unmarshal(admin("GET", "/api/admin/donation").Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	settings.Enabled = false
	payload, _ := json.Marshal(settings)
	if w := admin("PUT", "/api/admin/donation", string(payload)); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := donationRequest(a, "POST", "/api/donations", fmt.Sprintf(`{"request_id":%q,"nickname":"停用后新增","show_public":false,"amount":500,"currency":"CNY"}`, newID()), ""); w.Code != 404 {
		t.Fatal("停用后仍可创建赞赏")
	}
	if w := donationRequest(a, "GET", "/api/donations/"+d.ID, "", token); w.Code != 200 {
		t.Fatal("停用破坏了历史查询")
	}
	for _, table := range []string{"products", "orders", "stock_items", "deliveries", "order_promotions"} {
		var count int
		if err := a.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("赞赏影响了商品或优惠数据 %s: %d %v", table, count, err)
		}
	}
}

func TestDonationSettingsRejectInvalidValues(t *testing.T) {
	a, _ := newTestApp(t)
	admin := enableDonations(t, a)
	original := admin("GET", "/api/admin/donation").Body.String()
	for _, change := range []func(*DonationSettings){
		func(s *DonationSettings) { s.Title = " " },
		func(s *DonationSettings) { s.Currencies[0].MinAmount = 0 },
		func(s *DonationSettings) { s.Currencies[0].MinAmount = s.Currencies[0].MaxAmount + 1 },
		func(s *DonationSettings) { s.Currencies[0].MaxAmount = 9_000_000_000_001 },
		func(s *DonationSettings) { s.Currencies[0].Amounts = []int64{500, 500} },
		func(s *DonationSettings) { s.Currencies[0].Amounts = []int64{1} },
		func(s *DonationSettings) { s.Currencies[0].Currency = "INVALID" },
		func(s *DonationSettings) { s.Currencies[0].Amounts = make([]int64, 9) },
		func(s *DonationSettings) { s.Currencies = nil },
		func(s *DonationSettings) { s.Currencies = append(s.Currencies, s.Currencies[0]) },
	} {
		var s DonationSettings
		if err := json.Unmarshal([]byte(original), &s); err != nil {
			t.Fatal(err)
		}
		change(&s)
		payload, _ := json.Marshal(s)
		if w := admin("PUT", "/api/admin/donation", string(payload)); w.Code != 400 {
			t.Fatalf("接受了无效赞赏设置: %s", w.Body)
		}
		if saved := admin("GET", "/api/admin/donation").Body.String(); saved != original {
			t.Fatal("被拒绝的配置覆盖了已保存的设置")
		}
	}
}

func TestDonationCallbackRequiresVerificationAndIsIdempotent(t *testing.T) {
	a, gateway := newTestApp(t)
	admin := enableDonations(t, a)
	d, _ := newDonation(t, a, 1234, "CNY")
	if err := a.syncDonation(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	d, _ = a.loadDonation(context.Background(), d.ID)
	notice := envelope(t, &gateway.key.PublicKey, d.GatewayID, d.ID)
	if w := sendWebhook(a, notice); w.Code != 200 {
		t.Fatal(w.Body)
	}
	current, _ := a.loadDonation(context.Background(), d.ID)
	if current.PaymentStatus == "paid" {
		t.Fatal("仅凭加密通知确认了赞赏到账")
	}
	if w := sendWebhook(a, envelope(t, &gateway.key.PublicKey, "wrong-order", d.ID)); w.Code != 400 {
		t.Fatal("回调未核验网关映射")
	}
	gateway.set(d.ID, "paid", "system")
	for i := 0; i < 3; i++ {
		if w := sendWebhook(a, notice); w.Code != 200 {
			t.Fatal(w.Body)
		}
	}
	var result struct {
		Totals []DonationTotal `json:"totals"`
	}
	if err := json.Unmarshal(admin("GET", "/api/admin/donations").Body.Bytes(), &result); err != nil || len(result.Totals) != 1 || result.Totals[0].Amount != 1234 || result.Totals[0].Count != 1 {
		t.Fatalf("重复回调造成统计错误: %+v %v", result, err)
	}
}

func TestDonationRejectsPaymentIdentityMismatch(t *testing.T) {
	for _, corrupt := range []string{"id", "number", "merchant", "currency", "amount"} {
		t.Run(corrupt, func(t *testing.T) {
			a, gateway := newTestApp(t)
			enableDonations(t, a)
			d, _ := newDonation(t, a, 500, "CNY")
			if err := a.syncDonation(context.Background(), d.ID); err != nil {
				t.Fatal(err)
			}
			gateway.set(d.ID, "paid", "system")
			gateway.corrupt = corrupt
			if err := a.syncDonation(context.Background(), d.ID); err == nil {
				t.Fatal("未拒绝付款身份或金额不匹配")
			}
			d, _ = a.loadDonation(context.Background(), d.ID)
			if d.PaymentStatus != "review" || d.PaidAt != 0 {
				t.Fatalf("不匹配的赞赏被计为付款: %+v", d)
			}
		})
	}
}

func TestDonationRejectsUnverifiedCheckout(t *testing.T) {
	for _, corrupt := range []string{"merchant", "number"} {
		t.Run(corrupt, func(t *testing.T) {
			a, gateway := newTestApp(t)
			enableDonations(t, a)
			d, _ := newDonation(t, a, 500, "CNY")
			gateway.corruptCreate = corrupt
			if err := a.syncDonation(context.Background(), d.ID); err == nil {
				t.Fatal("未拒绝创建响应中的错误商户或商户订单号")
			}
			d, err := a.loadDonation(context.Background(), d.ID)
			if err != nil || d.CheckoutURL != "" || d.GatewayID != "" || d.PaymentStatus != "creating" {
				t.Fatalf("未核验身份的付款链接被公开: %+v %v", d, err)
			}
		})
	}
}

func TestDonationCheckoutWaitsForVerifiedQuery(t *testing.T) {
	a, gateway := newTestApp(t)
	enableDonations(t, a)
	d, token := newDonation(t, a, 500, "CNY")
	gateway.queryStatus = 503
	if err := a.syncDonation(context.Background(), d.ID); err == nil {
		t.Fatal("未复现创建成功后的查单失败")
	}
	var public Donation
	w := donationRequest(a, "GET", "/api/donations/"+d.ID, "", token)
	if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil || w.Code != 200 || public.PaymentStatus != "creating" || public.CheckoutURL != "" {
		t.Fatalf("创建摘要尚未核验归属就开放了付款链接: %s %v", w.Body, err)
	}
	gateway.queryStatus = 0
	if err := a.syncDonation(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	w = donationRequest(a, "GET", "/api/donations/"+d.ID, "", token)
	if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil || public.PaymentStatus != "pending" || public.CheckoutURL == "" || gateway.creates != 1 {
		t.Fatalf("查单恢复后没有开放原订单的付款链接: %s %v", w.Body, err)
	}
}

func TestDonationPrefixDoesNotMisrouteProductCallback(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "card", 0)
	const id = "don_product_callback"
	if _, err := a.db.Exec(`INSERT INTO orders(id,product_id,product_name,kind,unit_price,currency,quantity,total,token_hash,gateway_id,payment_status,created_at,updated_at) VALUES(?,?,'前缀相同的商品','card',1234,'CNY',1,1234,?,'prefix-gateway','pending',?,?)`, id, product, randomToken(), unixNow(), unixNow()); err != nil {
		t.Fatal(err)
	}
	gateway.orders[id] = GatewayOrder{ID: "prefix-gateway", MerchantID: "merchant-test", MerchantNo: id, Currency: "CNY", Amount: json.Number("12.34"), Status: "pending"}
	w := sendWebhook(a, envelope(t, &gateway.key.PublicKey, "prefix-gateway", id))
	if w.Code != 200 {
		t.Fatalf("赞赏前缀阻断了前缀相同的商品的付款通知: %s", w.Body)
	}
}

func TestDonationCreateRetryAndDisabledLatePayment(t *testing.T) {
	a, gateway := newTestApp(t)
	admin := enableDonations(t, a)
	d, token := newDonation(t, a, 2345, "CNY")
	gateway.failCreateOnce = true
	if err := a.syncDonation(context.Background(), d.ID); err == nil {
		t.Fatal("没有复现创建结果未知的网关故障")
	}
	if err := a.syncDonation(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if gateway.creates != 1 || len(gateway.returnURLs) != 2 {
		t.Fatal("创建重试未复用同一商户订单号")
	}
	for _, returnURL := range gateway.returnURLs {
		if strings.Contains(returnURL, token) || !strings.HasSuffix(returnURL, "/donate/orders/"+d.ID) {
			t.Fatal("赞赏支付回跳地址错误或泄露了凭证")
		}
	}
	gateway.set(d.ID, "expired", "")
	if err := a.syncDonation(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	var settings DonationSettings
	json.Unmarshal(admin("GET", "/api/admin/donation").Body.Bytes(), &settings)
	settings.Enabled = false
	payload, _ := json.Marshal(settings)
	if w := admin("PUT", "/api/admin/donation", string(payload)); w.Code != 200 {
		t.Fatal(w.Body)
	}
	gateway.set(d.ID, "paid", "system")
	if _, err := a.db.Exec("UPDATE donations SET next_check=0 WHERE id=?", d.ID); err != nil {
		t.Fatal(err)
	}
	a.runPending(context.Background())
	d, err := a.loadDonation(context.Background(), d.ID)
	if err != nil || d.PaymentStatus != "paid" || d.PaidAt == 0 {
		t.Fatalf("停用赞赏页后未补查过期记录的迟到付款: %+v %v", d, err)
	}
	queries := gatewayQueryCount(gateway)
	if _, err := a.db.Exec("UPDATE donations SET next_check=0 WHERE id=?", d.ID); err != nil {
		t.Fatal(err)
	}
	a.runPending(context.Background())
	if gatewayQueryCount(gateway) != queries {
		t.Fatal("已完成赞赏仍被自动补查")
	}
}

func TestDonationProtectsGatewayUntilFinished(t *testing.T) {
	for _, status := range []string{"creating", "pending", "expired", "review", "paid", "closed", "invalid"} {
		t.Run(status, func(t *testing.T) {
			a, _ := newTestApp(t)
			a.configPath = filepath.Join(t.TempDir(), "config.json")
			admin := enableDonations(t, a)
			d, token := newDonation(t, a, 500, "CNY")
			if _, err := a.db.Exec("UPDATE donations SET payment_status=? WHERE id=?", status, d.ID); err != nil {
				t.Fatal(err)
			}
			w := admin("PUT", "/api/admin/settings", `{"title":"HashDrop","hashpay_url":"https://other.example","merchant_id":"other"}`)
			finished := status == "paid" || status == "closed" || status == "invalid"
			if (finished && w.Code != 200) || (!finished && (w.Code != 400 || !strings.Contains(w.Body.String(), "待核验付款的赞赏"))) {
				t.Fatalf("网关切换保护错误: %s: %d %s", status, w.Code, w.Body)
			}
			if w := donationRequest(a, "GET", "/api/donations/"+d.ID, "", token); w.Code != 200 || !strings.Contains(w.Body.String(), status) {
				t.Fatal("网关配置变更影响了历史赞赏查询")
			}
		})
	}
}

func TestDonationSummarySeparatesCurrenciesAndPagination(t *testing.T) {
	a, _ := newTestApp(t)
	admin := enableDonations(t, a)
	for i := 0; i < 23; i++ {
		currency, status := "CNY", "paid"
		if i == 0 {
			currency = "USD"
		}
		if i == 1 {
			status = "pending"
		}
		if _, err := a.db.Exec(`INSERT INTO donations(id,title,amount,currency,token_hash,gateway_url,gateway_merchant_id,payment_status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, fmt.Sprintf("don_%02d", i), "历史赞赏", 100, currency, randomToken(), "https://pay.example", "merchant-test", status, i, i); err != nil {
			t.Fatal(err)
		}
	}
	var first, second struct {
		Items  []Donation      `json:"items"`
		Totals []DonationTotal `json:"totals"`
		More   bool            `json:"has_more"`
		Page   int             `json:"page"`
	}
	if err := json.Unmarshal(admin("GET", "/api/admin/donations?page=1").Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(admin("GET", "/api/admin/donations?page=2").Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 20 || !first.More || len(second.Items) != 3 || second.More || second.Page != 2 || first.Items[19].ID == second.Items[0].ID {
		t.Fatal("赞赏分页重复或遗漏记录")
	}
	if len(first.Totals) != 2 || first.Totals[0].Currency != "CNY" || first.Totals[0].Amount != 2100 || first.Totals[1].Currency != "USD" || first.Totals[1].Amount != 100 {
		t.Fatalf("未按币种汇总已付款赞赏: %+v", first.Totals)
	}
}
