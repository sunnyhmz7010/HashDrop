package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewaySettingsAllowCompletedAndTerminatedOrders(t *testing.T) {
	for _, sample := range []struct {
		status  string
		deleted bool
	}{
		{"paid", false}, {"paid", true}, {"closed", true}, {"invalid", true},
	} {
		t.Run(fmt.Sprintf("%s/deleted-%t", sample.status, sample.deleted), func(t *testing.T) {
			a, gateway := newTestApp(t)
			a.configPath = filepath.Join(t.TempDir(), "config.json")
			order, token := reserve(t, a, insertProduct(t, a, "card", 1), 1)
			if err := a.syncOrder(context.Background(), order.ID); err != nil {
				t.Fatal(err)
			}
			if sample.status == "closed" {
				gateway.mu.Lock()
				delete(gateway.orders, order.ID)
				gateway.mu.Unlock()
			} else {
				gateway.set(order.ID, sample.status, "system")
			}
			if err := a.syncOrder(context.Background(), order.ID); err != nil {
				t.Fatal(err)
			}
			before, err := a.loadOrder(context.Background(), order.ID)
			if err != nil || before.PaymentStatus != sample.status {
				t.Fatalf("未建立原网关核验的历史订单: %+v %v", before, err)
			}
			request := adminSessionRequest(t, a)
			if sample.deleted {
				if result := request("DELETE", "/api/admin/orders/"+order.ID); result.Code != 200 {
					t.Fatalf("删除订单失败: %s", result.Body)
				}
			}
			if result := request("PUT", "/api/admin/settings", `{"title":"TinyFaka","hashpay_url":"https://other.example/","merchant_id":"other"}`); result.Code != 200 {
				t.Fatalf("完成或终止的历史订单仍阻止切换: %s", result.Body)
			}
			var saved Config
			data, err := os.ReadFile(a.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.HashPay.BaseURL != "https://other.example" || saved.HashPay.MerchantID != "other" || saved.HashPay != a.settings().HashPay {
				t.Fatal("新网关配置未持久化或内存配置未更新")
			}
			// 原商户归属不能被新配置覆盖，也不能将历史订单发送给新商户查询。
			queries := gatewayQueryCount(gateway)
			a.runPending(context.Background())
			if err = a.syncOrder(context.Background(), order.ID); err == nil || !strings.Contains(err.Error(), "原网关或商户") {
				t.Fatalf("原订单错误使用了新网关: %v", err)
			}
			if gatewayQueryCount(gateway) != queries {
				t.Fatal("已完成或终止的订单仍被自动补查")
			}
			after, err := a.loadOrder(context.Background(), order.ID)
			if err != nil || after.PaymentStatus != before.PaymentStatus || after.DeliveryStatus != before.DeliveryStatus || after.ReservationStatus != before.ReservationStatus || after.GatewayURL != before.GatewayURL || after.GatewayMerchantID != before.GatewayMerchantID {
				t.Fatal("更换配置改变了历史订单状态或商户归属")
			}
			claim := httptest.NewRequest("GET", "/api/orders/"+order.ID, nil)
			claim.Header.Set("Authorization", "Bearer "+token)
			receipt := httptest.NewRecorder()
			a.routes().ServeHTTP(receipt, claim)
			if receipt.Code != 200 || !strings.Contains(receipt.Body.String(), `"payment_status":"`+sample.status+`"`) {
				t.Fatalf("更换配置破坏了历史领取链接: %s", receipt.Body)
			}
			if sample.status == "paid" && !strings.Contains(receipt.Body.String(), "卡密-0") {
				t.Fatal("更换配置破坏了已完成交付的卡密")
			}
		})
	}
}

func TestGatewaySettingsProtectUnfinishedOrders(t *testing.T) {
	for _, state := range []struct{ payment, delivery string }{
		{"creating", "pending"}, {"pending", "pending"}, {"expired", "pending"}, {"review", "pending"},
		{"paid", "pending"}, {"paid", "processing"}, {"paid", "manual"},
	} {
		t.Run(state.payment+"/"+state.delivery, func(t *testing.T) {
			a, _ := newTestApp(t)
			a.configPath = filepath.Join(t.TempDir(), "config.json")
			a.config.HashPay.BaseURL = "https://pay.example"
			if err := writeConfig(a.configPath, a.config); err != nil {
				t.Fatal(err)
			}
			original := a.settings()
			order, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
			// 隔离付款与交付阶段，覆盖进程恢复和人工处理中的订单。
			if _, err := a.db.Exec("UPDATE orders SET payment_status=?,delivery_status=? WHERE id=?", state.payment, state.delivery, order.ID); err != nil {
				t.Fatal(err)
			}
			request := adminSessionRequest(t, a)
			if result := request("DELETE", "/api/admin/orders/"+order.ID); result.Code != 200 {
				t.Fatal(result.Body)
			}
			for _, change := range []struct{ url, merchant string }{
				{"https://other.example", original.HashPay.MerchantID},
				{original.HashPay.BaseURL, "other"},
			} {
				payload := fmt.Sprintf(`{"title":"不能保存","hashpay_url":%q,"merchant_id":%q}`, change.url, change.merchant)
				result := request("PUT", "/api/admin/settings", payload)
				if result.Code != 400 || !strings.Contains(result.Body.String(), "待核验付款或未完成交付") {
					t.Fatalf("删除的未完成订单未保护原网关: %d %s", result.Code, result.Body)
				}
			}
			data, err := os.ReadFile(a.configPath)
			if err != nil {
				t.Fatal(err)
			}
			var saved Config
			if err = json.Unmarshal(data, &saved); err != nil || saved.HashPay != original.HashPay || saved.Title != original.Title || a.settings().HashPay != original.HashPay || a.settings().Title != original.Title {
				t.Fatal("被拒绝的切换修改了内存或磁盘配置")
			}
			// 只保存站点设置或带空白的相同商户，不应被当作更换网关。
			payload := fmt.Sprintf(`{"title":"新店铺名称","hashpay_url":" https://pay.example/ ","merchant_id":%q}`, " "+original.HashPay.MerchantID+" ")
			if result := request("PUT", "/api/admin/settings", payload); result.Code != 200 {
				t.Fatalf("未更换网关仍被阻止保存: %s", result.Body)
			}
			if a.settings().HashPay != original.HashPay || a.settings().Title != "新店铺名称" {
				t.Fatal("相同网关的设置保存结果不正确")
			}
		})
	}
}
