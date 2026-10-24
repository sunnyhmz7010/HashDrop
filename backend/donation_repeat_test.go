package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestDonationRepeatRequiresConfirmationAndKeepsRequestIdentity(t *testing.T) {
	a, _ := newTestApp(t)
	enableDonations(t, a)
	input := map[string]any{"request_id": newID(), "nickname": "再次支持", "show_public": false, "amount": 500, "currency": "CNY"}
	firstRequestID := input["request_id"].(string)
	first := createdDonation(t, createNamedDonation(t, a, input))
	input["request_id"] = newID()
	if response := createNamedDonation(t, a, input); response.Code != 409 || !strings.Contains(response.Body.String(), "donation_repeat_confirmation_required") {
		t.Fatalf("重复昵称未要求确认: %d %s", response.Code, response.Body)
	}
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM donations").Scan(&count); err != nil || count != 1 {
		t.Fatal("确认前已创建额外赞赏", count, err)
	}
	input["confirm_repeat"] = true
	second := createdDonation(t, createNamedDonation(t, a, input))
	if first.ID == second.ID || first.Nickname != second.Nickname {
		t.Fatal("再次赞赏没有生成同昵称的独立记录")
	}
	// 同一请求的确认重试、确认标志丢失均只恢复原单。
	delete(input, "confirm_repeat")
	if replay := createdDonation(t, createNamedDonation(t, a, input)); replay.ID != second.ID {
		t.Fatal("确认后重放创建了第三笔赞赏")
	}
	for _, id := range []string{first.ID, second.ID} {
		response := donationRequest(a, "GET", "/api/donations/"+id, "", first.Nickname)
		if response.Code != 200 || !strings.Contains(response.Body.String(), id) {
			t.Fatalf("昵称不能查询本人的历次赞赏: %d %s", response.Code, response.Body)
		}
		if response := donationRequest(a, "GET", "/api/donations/"+id, "", "其他昵称"); response.Code != 404 {
			t.Fatal("其他昵称可以查询赞赏")
		}
	}
	body, _ := json.Marshal(map[string]any{"nickname": first.Nickname, "page": 1, "request_id": input["request_id"]})
	response := donationRequest(a, "POST", "/api/donations/lookup", string(body), "")
	var result struct {
		Items   []Donation `json:"items"`
		Matched string     `json:"matched_request_id"`
		More    bool       `json:"has_more"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Items) != 2 || result.More || result.Matched != input["request_id"] {
		t.Fatalf("昵称查询没有恢复全部历史或未识别本次请求: %d %s", response.Code, response.Body)
	}
	unknown := newID()
	body, _ = json.Marshal(map[string]any{"nickname": first.Nickname, "page": 1, "request_id": unknown})
	response = donationRequest(a, "POST", "/api/donations/lookup", string(body), "")
	if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Matched != "" {
		t.Fatal("同昵称旧记录被误认为尚未创建的新请求")
	}
	if strings.Contains(response.Body.String(), firstRequestID) || strings.Contains(response.Body.String(), `"token_hash"`) || strings.Contains(response.Body.String(), `"gateway_id"`) {
		t.Fatal("历史查询泄露未提供的请求标识或内部付款字段")
	}
}

func TestDonationGroupedPaginationKeepsCompleteHistoryAndAnonymousIdentity(t *testing.T) {
	a, _ := newTestApp(t)
	admin := enableDonations(t, a)
	for i := 0; i < 25; i++ {
		nickname, currency, status := fmt.Sprintf("支持者-%02d", i), "CNY", "paid"
		if i < 3 {
			nickname = "多次支持"
		}
		if i == 0 {
			currency = "USD"
		} else if i == 1 {
			status = "pending"
		}
		if _, err := a.db.Exec(`INSERT INTO donations(id,title,nickname,amount,currency,gateway_url,gateway_merchant_id,payment_status,created_at,updated_at)
VALUES(?,'历史赞赏',?,100,?,'https://pay.example','merchant-test',?,?,?)`, fmt.Sprintf("history_%02d", i), nickname, currency, status, 100-i, 100-i); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for page, want := range []int{22, 3} {
		response := admin("GET", fmt.Sprintf("/api/admin/donations?group_by=nickname&page=%d", page+1))
		var result struct {
			Items  []Donation      `json:"items"`
			Totals []DonationTotal `json:"totals"`
			More   bool            `json:"has_more"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Items) != want || result.More != (page == 0) {
			t.Fatalf("按昵称分页错误: %d %s", response.Code, response.Body)
		}
		repeat := 0
		for _, item := range result.Items {
			if seen[item.ID] {
				t.Fatal("分组分页重复记录", item.ID)
			}
			seen[item.ID] = true
			if item.Nickname == "多次支持" {
				repeat++
			}
		}
		if page == 0 && repeat != 3 || page == 1 && repeat != 0 {
			t.Fatal("同昵称的历史被拆分到不同页")
		}
		if len(result.Totals) != 2 || result.Totals[0].Amount != 2300 || result.Totals[1].Amount != 100 {
			t.Fatal("汇总混入待付款或混合了币种", result.Totals)
		}
	}
	if len(seen) != 25 {
		t.Fatal("分组分页遗漏历史")
	}
	if _, err := a.db.Exec(`INSERT INTO donations(id,title,nickname,amount,currency,gateway_url,gateway_merchant_id,payment_status,created_at,updated_at)
VALUES('search-literal','搜索测试','Sunny_100%',500,'USD','https://pay.example','merchant-test','paid',101,101)`); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"nickname", ""} {
		for _, sample := range []struct {
			query string
			want  int
		}{
			{" 多次支持 ", 3},
			{"支持者-22", 1}, // 原列表第二页的昵称也必须能够搜到。
			{"sunny", 1},
			{"%", 1},
			{"_", 1},
			{"' OR 1=1 --", 0},
			{"不存在的昵称", 0},
		} {
			params := url.Values{"group_by": {group}, "q": {sample.query}}
			response := admin("GET", "/api/admin/donations?"+params.Encode())
			var result struct {
				Items []Donation `json:"items"`
				More  bool       `json:"has_more"`
			}
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Items) != sample.want || result.More {
				t.Fatalf("昵称搜索未保持完整历史或按字面匹配，分组=%q 搜索=%q: %d %s", group, sample.query, response.Code, response.Body)
			}
		}
	}
}

func TestDonationNicknameHistoryPaginatesAndMatchesOnlyProvidedRequest(t *testing.T) {
	a, _ := newTestApp(t)
	enableDonations(t, a)
	for i := 0; i < 23; i++ {
		if _, err := a.db.Exec(`INSERT INTO donations(id,title,nickname,request_id,amount,currency,gateway_url,gateway_merchant_id,payment_status,created_at,updated_at)
VALUES(?,'多页历史','长期支持者',?,100,'CNY','https://pay.example','merchant-test','paid',?,?)`, fmt.Sprintf("history_%02d", i), fmt.Sprintf("request_%02d", i), i, i); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for page, want := range []int{20, 3, 0} {
		body := fmt.Sprintf(`{"nickname":"长期支持者","page":%d,"request_id":"request_00"}`, page+1)
		response := donationRequest(a, "POST", "/api/donations/lookup", body, "")
		var result struct {
			Items   []Donation `json:"items"`
			More    bool       `json:"has_more"`
			Matched string     `json:"matched_request_id"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Items) != want || result.More != (page == 0) || result.Matched != "request_00" {
			t.Fatalf("多页查询遗漏历史或不能匹配后页请求: %d %s", response.Code, response.Body)
		}
		for _, item := range result.Items {
			if seen[item.ID] {
				t.Fatal("昵称查询跨页重复记录", item.ID)
			}
			seen[item.ID] = true
		}
		if strings.Contains(response.Body.String(), "request_22") || strings.Contains(response.Body.String(), `"last_error"`) {
			t.Fatal("多页查询泄露未提供的请求标识或诊断信息")
		}
	}
	if len(seen) != 23 {
		t.Fatal("昵称查询丢失历史")
	}
}
