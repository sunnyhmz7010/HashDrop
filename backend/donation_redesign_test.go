package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func createNamedDonation(t *testing.T, a *App, input map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return donationRequest(a, "POST", "/api/donations", string(body), "")
}

func createdDonation(t *testing.T, response *httptest.ResponseRecorder) Donation {
	t.Helper()
	var result struct {
		Donation Donation `json:"donation"`
	}
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Donation.ID == "" {
		t.Fatalf("创建赞赏失败: %d %s", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), `"token":`) || strings.Contains(response.Body.String(), `"request_id":`) {
		t.Fatal("昵称赞赏仍返回额外凭证或内部请求标识")
	}
	return result.Donation
}

func TestDonationRequestRetryPreservesOriginalRecord(t *testing.T) {
	a, _ := newTestApp(t)
	admin := enableDonations(t, a)
	input := map[string]any{"request_id": newID(), "nickname": " 幂等赞赏者🔑 ", "show_public": true, "amount": 1234, "currency": "CNY"}
	first := createdDonation(t, createNamedDonation(t, a, input))
	if first.Nickname != "幂等赞赏者🔑" || !first.ShowPublic || first.Amount != 1234 {
		t.Fatalf("昵称标准化或公开选择未保存: %+v", first)
	}
	if replay := createdDonation(t, createNamedDonation(t, a, input)); replay.ID != first.ID {
		t.Fatal("同一次请求重试创建了第二笔赞赏")
	}
	for _, change := range []struct {
		field string
		value any
	}{{"nickname", "被篡改的昵称"}, {"show_public", false}, {"amount", 2000}, {"currency", "USD"}} {
		t.Run(change.field, func(t *testing.T) {
			original := input[change.field]
			input[change.field] = change.value
			defer func() { input[change.field] = original }()
			if response := createNamedDonation(t, a, input); response.Code != 409 {
				t.Fatalf("重复请求允许篡改 %s: %d %s", change.field, response.Code, response.Body)
			}
		})
	}
	var settings DonationSettings
	if err := json.Unmarshal(admin("GET", "/api/admin/donation").Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	settings.Enabled = false
	settings.Currencies[0].MinAmount = 5000
	settings.Currencies[0].Amounts = []int64{5000}
	body, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if response := admin("PUT", "/api/admin/donation", string(body)); response.Code != 200 {
		t.Fatal(response.Body)
	}
	if replay := createdDonation(t, createNamedDonation(t, a, input)); replay.ID != first.ID || replay.Amount != first.Amount {
		t.Fatal("停用或修改金额设置阻止了原请求恢复")
	}
	// 持续丢包后的幂等重放不应消耗新建额度或变成另一笔赞赏。
	for i := 0; i < 12; i++ {
		if replay := createdDonation(t, createNamedDonation(t, a, input)); replay.ID != first.ID {
			t.Fatal("多次重放无法恢复同一笔赞赏")
		}
	}
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM donations").Scan(&count); err != nil || count != 1 {
		t.Fatal("重试或被拒绝的篡改留下了额外记录", count, err)
	}
}

func TestDonationNicknameConflictAndCharacterLimit(t *testing.T) {
	a, _ := newTestApp(t)
	enableDonations(t, a)
	nickname := strings.Repeat("赞", 127) + "🎁"
	input := map[string]any{"request_id": newID(), "nickname": nickname, "show_public": false, "amount": 500, "currency": "CNY"}
	createdDonation(t, createNamedDonation(t, a, input))
	input["request_id"], input["nickname"] = newID(), " "+nickname+" "
	response := createNamedDonation(t, a, input)
	var conflict struct {
		Code        string   `json:"code"`
		Suggestions []string `json:"suggestions"`
	}
	if response.Code != 409 || json.Unmarshal(response.Body.Bytes(), &conflict) != nil || conflict.Code != "donation_repeat_confirmation_required" || len(conflict.Suggestions) != 3 {
		t.Fatalf("重复昵称未要求确认或未提供派生建议: %d %s", response.Code, response.Body)
	}
	seen := map[string]bool{nickname: true}
	for _, suggestion := range conflict.Suggestions {
		if !utf8.ValidString(suggestion) || utf8.RuneCountInString(suggestion) > 128 || strings.TrimSpace(suggestion) == "" || seen[suggestion] {
			t.Fatalf("昵称建议重复、为空或超长: %q", suggestion)
		}
		seen[suggestion] = true
	}
	input["request_id"], input["nickname"] = newID(), conflict.Suggestions[0]
	if accepted := createdDonation(t, createNamedDonation(t, a, input)); accepted.Nickname != conflict.Suggestions[0] {
		t.Fatal("派生昵称不能直接使用")
	}
	for _, invalid := range []string{"", " \t\n ", nickname + "多"} {
		input["request_id"], input["nickname"] = newID(), invalid
		if response := createNamedDonation(t, a, input); response.Code != 400 {
			t.Fatalf("无效昵称未被拒绝: %d %s", response.Code, response.Body)
		}
	}
	delete(input, "nickname")
	input["request_id"] = newID()
	if response := createNamedDonation(t, a, input); response.Code != 400 {
		t.Fatal("省略昵称仍可创建匿名新赞赏")
	}
}

func TestDonationMultipleCurrenciesKeepIndependentRanges(t *testing.T) {
	a, _ := newTestApp(t)
	admin := enableDonations(t, a)
	settings := DonationSettings{Enabled: true, Title: "多币种赞赏", Currencies: []DonationCurrencySettings{
		{Currency: "CNY", Amounts: []int64{500}, MinAmount: 100, MaxAmount: 1000},
		{Currency: "USD", Amounts: []int64{50}, MinAmount: 50, MaxAmount: 200},
		{Currency: "JPY", Amounts: []int64{1}, MinAmount: 1, MaxAmount: 10},
	}}
	body, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if response := admin("PUT", "/api/admin/donation", string(body)); response.Code != 200 {
		t.Fatal(response.Body)
	}
	var public DonationSettings
	response := donationRequest(a, "GET", "/api/donation", "", "")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &public) != nil || len(public.Currencies) != 3 {
		t.Fatalf("公开赞赏页未获得全部币种设置: %s", response.Body)
	}
	for _, sample := range []struct {
		currency string
		amount   int64
		status   int
	}{{"CNY", 100, 201}, {"USD", 50, 201}, {"JPY", 1, 201}, {"USD", 201, 400}, {"CNY", 50, 400}, {"EUR", 100, 409}} {
		input := map[string]any{"request_id": newID(), "nickname": "币种测试-" + newID(), "show_public": false, "amount": sample.amount, "currency": sample.currency}
		response := createNamedDonation(t, a, input)
		if response.Code != sample.status {
			t.Fatalf("%s %d 未按本币种范围校验: %d %s", sample.currency, sample.amount, response.Code, response.Body)
		}
		if sample.status == 201 {
			d := createdDonation(t, response)
			if d.Currency != sample.currency || d.Amount != sample.amount {
				t.Fatalf("赞赏金额或币种被错误转换: %+v", d)
			}
		}
	}
}

func TestDonationNicknameLookupExposesOnlyBuyerFields(t *testing.T) {
	a, _ := newTestApp(t)
	enableDonations(t, a)
	d, nickname := newDonation(t, a, 500, "CNY")
	for _, status := range []string{"creating", "pending", "paid", "expired", "invalid", "review", "closed"} {
		if _, err := a.db.Exec("UPDATE donations SET payment_status=?,gateway_id='internal-gateway',checkout_url='https://pay.example/pay/test',last_error='internal-error' WHERE id=?", status, d.ID); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(map[string]string{"nickname": " " + nickname + " "})
		if err != nil {
			t.Fatal(err)
		}
		for _, response := range []*httptest.ResponseRecorder{
			donationRequest(a, "POST", "/api/donations/lookup", string(body), ""),
			donationRequest(a, "GET", "/api/donations/"+d.ID, "", nickname),
		} {
			var value map[string]json.RawMessage
			var found Donation
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &value) != nil || json.Unmarshal(response.Body.Bytes(), &found) != nil || found.ID != d.ID || found.Nickname != nickname || found.PaymentStatus != status {
				t.Fatalf("昵称查询未恢复对应记录: %d %s", response.Code, response.Body)
			}
			for _, forbidden := range []string{"token_hash", "request_id", "gateway_id", "gateway_url", "gateway_merchant_id", "last_error", "attempts", "next_check"} {
				if _, present := value[forbidden]; present {
					t.Fatalf("公开查询泄露内部字段 %s: %s", forbidden, response.Body)
				}
			}
			if (status == "pending") != (found.CheckoutURL != "") {
				t.Fatalf("%s 状态的支付地址开放错误: %s", status, response.Body)
			}
		}
	}
	if response := donationRequest(a, "POST", "/api/donations/lookup", `{"nickname":"不存在的昵称"}`, ""); response.Code != 404 {
		t.Fatal("不存在的昵称未返回404")
	}
}

func TestDonationPublicListRequiresPaidAndExplicitConsent(t *testing.T) {
	a, _ := newTestApp(t)
	enableDonations(t, a)
	for i := 0; i < 26; i++ {
		nickname, status, shown := fmt.Sprintf("公开赞赏者-%02d", i), "paid", true
		if i == 23 {
			shown = false
		} else if i == 24 {
			status = "pending"
		} else if i == 25 {
			nickname = "" // 旧版本匿名记录不能因为迁移而被公开。
		}
		currency := "CNY"
		if i%2 == 0 {
			currency = "USD"
		}
		if _, err := a.db.Exec(`INSERT INTO donations(id,title,amount,currency,token_hash,gateway_url,gateway_merchant_id,payment_status,created_at,updated_at,paid_at,nickname,show_public)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, fmt.Sprintf("public_%02d", i), "公开记录测试", i+100, currency, randomToken(), "https://pay.example", "merchant-test", status, i, i, 1000+i, nickname, shown); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for page, want := range []int{20, 3} {
		response := donationRequest(a, "GET", fmt.Sprintf("/api/donations/public?page=%d", page+1), "", "")
		var result struct {
			Items []map[string]json.RawMessage `json:"items"`
			Page  int                          `json:"page"`
			More  bool                         `json:"has_more"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Items) != want || result.Page != page+1 || result.More != (page == 0) {
			t.Fatalf("公开赞赏分页错误: %d %s", response.Code, response.Body)
		}
		for _, item := range result.Items {
			if len(item) != 4 || item["nickname"] == nil || item["amount"] == nil || item["currency"] == nil || item["paid_at"] == nil {
				t.Fatalf("公开列表超出允许字段或缺少金额信息: %+v", item)
			}
			var nickname string
			if err := json.Unmarshal(item["nickname"], &nickname); err != nil || nickname == "" || seen[nickname] || nickname == "公开赞赏者-23" || nickname == "公开赞赏者-24" {
				t.Fatalf("公开了匿名、未付款、未同意的赞赏或重复记录: %q %v", nickname, err)
			}
			seen[nickname] = true
		}
	}
	if len(seen) != 23 {
		t.Fatal("公开赞赏分页遗漏记录")
	}
}

func TestLegacyDonationBearerCredentialStillWorks(t *testing.T) {
	a, _ := newTestApp(t)
	token := randomToken()
	if _, err := a.db.Exec(`INSERT INTO donations(id,title,amount,currency,token_hash,gateway_url,gateway_merchant_id,payment_status,created_at,updated_at)
 VALUES('legacy-bearer','旧匿名赞赏',500,'CNY',?,'https://pay.example','merchant-test','paid',1,1)`, tokenHash(token)); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/donations/legacy-bearer", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	a.routes().ServeHTTP(response, request)
	var found Donation
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &found) != nil || found.ID != "legacy-bearer" || found.PaymentStatus != "paid" || found.Nickname != "" {
		t.Fatalf("升级后旧 Bearer 领取链接不可用: %d %s", response.Code, response.Body)
	}
}
