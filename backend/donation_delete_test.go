package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDonationDeleteHidesAdminHistoryAndPreservesPaymentIdentity(t *testing.T) {
	a, gateway := newTestApp(t)
	admin := enableDonations(t, a)
	input := map[string]any{"request_id": newID(), "nickname": "可删除的赞赏", "amount": 500, "currency": "CNY", "show_public": false}
	first := createdDonation(t, createNamedDonation(t, a, input))
	firstInput := map[string]any{}
	for key, value := range input {
		firstInput[key] = value
	}
	input["request_id"], input["confirm_repeat"] = newID(), true
	second := createdDonation(t, createNamedDonation(t, a, input))
	for _, donation := range []Donation{first, second} {
		if err := a.syncDonation(context.Background(), donation.ID); err != nil {
			t.Fatal(err)
		}
	}
	gateway.set(first.ID, "paid", "system")
	if err := a.syncDonation(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if response := donationRequest(a, "DELETE", "/api/admin/donations/"+first.ID, "", ""); response.Code != 401 {
		t.Fatal("赞赏删除未要求管理员登录", response.Code)
	}
	if response := admin("DELETE", "/api/admin/donations/"+first.ID); response.Code != 200 {
		t.Fatal("不能删除已付款的赞赏", response.Code, response.Body)
	}
	var list struct {
		Items  []Donation      `json:"items"`
		Totals []DonationTotal `json:"totals"`
	}
	for _, query := range []string{"", "?group_by=nickname&q=可删除"} {
		response := admin("GET", "/api/admin/donations"+query)
		if json.Unmarshal(response.Body.Bytes(), &list) != nil || len(list.Items) != 1 || list.Items[0].ID != second.ID || len(list.Totals) != 0 {
			t.Fatal("后台列表或同昵称分组仍包含已删除的记录", response.Body)
		}
	}
	if response := admin("POST", "/api/admin/donations/"+first.ID+"/recheck", "{}"); response.Code != 404 {
		t.Fatal("后台仍能操作已删除的记录", response.Code)
	}
	if replay := createdDonation(t, createNamedDonation(t, a, firstInput)); replay.ID != first.ID {
		t.Fatal("删除后重试原请求创建了另一笔赞赏")
	}
	if response := donationRequest(a, "GET", "/api/donations/"+first.ID, "", first.Nickname); response.Code != 200 {
		t.Fatal("后台删除破坏了原查询凭证", response.Code, response.Body)
	}
	if response := admin("DELETE", "/api/admin/donations/"+second.ID); response.Code != 200 {
		t.Fatal("不能删除待付款赞赏", response.Code)
	}
	gateway.set(second.ID, "paid", "system")
	if err := a.syncDonation(context.Background(), second.ID); err != nil {
		t.Fatal("后台删除破坏了后续付款核验", err)
	}
	response := admin("GET", "/api/admin/donations?group_by=nickname")
	if json.Unmarshal(response.Body.Bytes(), &list) != nil || len(list.Items) != 0 || len(list.Totals) != 0 {
		t.Fatal("删除最后一笔后仍返回该昵称卡片", response.Body)
	}
	if response := admin("DELETE", "/api/admin/donations/"+first.ID); response.Code != 200 {
		t.Fatal("重复删除未保持幂等", response.Code)
	}
	if response := admin("DELETE", "/api/admin/donations/missing"); response.Code != 404 {
		t.Fatal("不存在的记录被报告为删除成功", response.Code)
	}
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM donations WHERE deleted_at IS NOT NULL AND payment_status='paid'").Scan(&count); err != nil || count != 2 {
		t.Fatal("删除丢失了付款记录或重复创建了记录", count, err)
	}
}
