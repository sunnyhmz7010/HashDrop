package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err := a.db.QueryRow("SELECT status FROM stock_items WHERE inventory_id=?", product).Scan(&reservation); err != nil || reservation != "reserved" {
		t.Fatal("删除提前释放了库存")
	}
	if result := request("PUT", "/api/admin/settings", `{"title":"HashDrop","hashpay_url":"https://other.example","merchant_id":"other"}`); result.Code != 400 || !strings.Contains(result.Body.String(), "待核验付款或未完成交付") {
		t.Fatal("删除后仍待付款的订单未保护原支付商户映射")
	}
	gateway.set(order.ID, "paid", "admin")
	// 删除不会取消补查；由原任务核验网关付款并完成原交付。
	if _, err := a.db.Exec("UPDATE orders SET next_check=0 WHERE id=?", order.ID); err != nil {
		t.Fatal(err)
	}
	a.runPending(context.Background())
	claim := httptest.NewRequest("GET", "/api/orders/"+order.ID, nil)
	claim.Header.Set("Authorization", claimAuthorization(token))
	receipt := httptest.NewRecorder()
	a.routes().ServeHTTP(receipt, claim)
	if receipt.Code != 200 || strings.Contains(receipt.Body.String(), "卡密-") {
		t.Fatalf("删除后查单失败或泄漏正文: %s", receipt.Body)
	}
	deliveries, err := a.deliveryMetadata(context.Background(), order.ID)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("交付状态: %v", err)
	}
	if claimed := claimDeliveryForTest(t, a, order.ID, deliveries[0].ID, token); claimed.Code != 200 || !strings.Contains(claimed.Body.String(), "卡密-") {
		t.Fatalf("删除破坏了迟到付款领取: %s", claimed.Body)
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

func gatewayQueryCount(g *fakeGateway) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.queries
}

func TestMissingGatewayOrderReleasesOnlyItsReservation(t *testing.T) {
	a, gateway := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	order, token := reserve(t, a, product, 1)
	if err := a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	gateway.set(order.ID, "expired", "")
	if err := a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	gateway.mu.Lock()
	original := gateway.orders[order.ID]
	delete(gateway.orders, order.ID)
	gateway.mu.Unlock()
	if _, err := a.db.Exec("UPDATE orders SET next_check=0 WHERE id=?", order.ID); err != nil {
		t.Fatal(err)
	}
	a.runPending(context.Background())
	closed, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || closed.PaymentStatus != "closed" || closed.ReservationStatus != "released" || closed.DeliveryStatus != "cancelled" {
		t.Fatalf("删除后未安全终止订单: %+v %v", closed, err)
	}
	claim := httptest.NewRequest("GET", "/api/orders/"+order.ID, nil)
	claim.Header.Set("Authorization", claimAuthorization(token))
	response := httptest.NewRecorder()
	a.routes().ServeHTTP(response, claim)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"payment_status":"closed"`) {
		t.Fatal("删除后领取链接不能查询终止状态")
	}
	queries := gatewayQueryCount(gateway)
	a.runPending(context.Background())
	if gatewayQueryCount(gateway) != queries {
		t.Fatal("已关闭订单仍被自动补查")
	}
	next, _ := reserve(t, a, product, 1)
	if err = a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	// 删除响应可重复处理；网关后来恢复付款也不能抢占下一笔订单的库存。
	original.Status = "paid"
	gateway.mu.Lock()
	gateway.orders[order.ID] = original
	gateway.mu.Unlock()
	if err = a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	late, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || late.PaymentStatus != "paid" || late.DeliveryStatus != "failed" {
		t.Fatal("释放后的迟到付款没有转人工")
	}
	var assigned, state string
	if err = a.db.QueryRow("SELECT order_id,status FROM stock_items WHERE inventory_id=?", product).Scan(&assigned, &state); err != nil || assigned != next.ID || state != "reserved" {
		t.Fatal("删除或迟到付款破坏了后续订单库存")
	}
}

func TestMissingGatewayOrderKeepsVerifiedPayment(t *testing.T) {
	for _, completed := range []bool{false, true} {
		name := "pending-delivery"
		if completed {
			name = "delivered"
		}
		t.Run(name, func(t *testing.T) {
			a, gateway := newTestApp(t)
			order, _ := reserve(t, a, insertProduct(t, a, "account", 1), 1)
			if err := a.syncOrder(context.Background(), order.ID); err != nil {
				t.Fatal(err)
			}
			gateway.set(order.ID, "paid", "system")
			if completed {
				if err := a.syncOrder(context.Background(), order.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				// 模拟已核验付款、尚未进入交付事务时进程退出。
				if err := a.markPaid(context.Background(), order.ID); err != nil {
					t.Fatal(err)
				}
			}
			gateway.mu.Lock()
			delete(gateway.orders, order.ID)
			gateway.mu.Unlock()
			for i := 0; i < 2; i++ {
				if err := a.syncOrder(context.Background(), order.ID); err != nil {
					t.Fatal(err)
				}
			}
			paid, err := a.loadOrder(context.Background(), order.ID)
			if err != nil || paid.PaymentStatus != "paid" || paid.DeliveryStatus != "done" || paid.ReservationStatus != "consumed" {
				t.Fatalf("网关删除破坏了已付款交付: %+v %v", paid, err)
			}
			results, err := a.deliveryResults(context.Background(), order.ID)
			if err != nil || len(results) != 1 || deliveryText(results[0].Content) != "卡密-0" {
				t.Fatal("网关删除破坏了原领取内容")
			}
		})
	}
}

func TestUncertainGatewayErrorsKeepStockReserved(t *testing.T) {
	for _, sample := range []struct {
		name              string
		status            int
		contentType, body string
	}{
		{"html-404", 404, "text/html", `{"error":{"key":"errors.order_not_found"}}`},
		{"other-404", 404, "application/json", `{"error":{"key":"errors.route_not_found"}}`},
		{"malformed-404", 404, "application/json", `{`},
		{"trailing-404", 404, "application/json", `{"error":{"key":"errors.order_not_found"}} {}`},
		{"authentication", 401, "application/json", `{"error":{"key":"errors.order_not_found"}}`},
		{"unavailable", 503, "application/json", `{"error":{"key":"errors.order_not_found"}}`},
		{"oversized", 404, "application/json", strings.Repeat("x", 1024*1024+1)},
	} {
		t.Run(sample.name, func(t *testing.T) {
			a, gateway := newTestApp(t)
			order, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
			if err := a.syncOrder(context.Background(), order.ID); err != nil {
				t.Fatal(err)
			}
			gateway.mu.Lock()
			gateway.queryStatus, gateway.queryType, gateway.queryBody = sample.status, sample.contentType, sample.body
			gateway.mu.Unlock()
			if err := a.syncOrder(context.Background(), order.ID); err == nil {
				t.Fatal("不确定响应被当作订单删除")
			}
			pending, err := a.loadOrder(context.Background(), order.ID)
			if err != nil || pending.PaymentStatus != "pending" || pending.ReservationStatus != "reserved" {
				t.Fatal("不确定响应改变了付款或库存状态")
			}
			var next int64
			if err = a.db.QueryRow("SELECT next_check FROM orders WHERE id=?", order.ID).Scan(&next); err != nil || next < unixNow()+115 {
				t.Fatal("查询错误没有安排补查")
			}
		})
	}
}

func TestUnverifiedGatewayOrderKeepsReservationUntilOwnershipConfirmed(t *testing.T) {
	a, gateway := newTestApp(t)
	order, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
	gateway.queryStatus = http.StatusServiceUnavailable
	if err := a.syncOrder(context.Background(), order.ID); err == nil {
		t.Fatal("未复现创建网关订单后首次查询失败")
	}
	gateway.queryStatus, gateway.queryType, gateway.queryBody = http.StatusNotFound, "application/json", `{"error":{"key":"errors.order_not_found"}}`
	if err := a.syncOrder(context.Background(), order.ID); err == nil || !strings.Contains(err.Error(), "尚未核验") {
		t.Fatal("尚未完整核验归属的 404 没有保留待核查状态")
	}
	pending, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || pending.GatewayID == "" || pending.GatewayURL != "" || pending.GatewayMerchantID != "" || pending.ReservationStatus != "reserved" {
		t.Fatalf("首次核验失败后释放了库存或猜测了商户归属: %+v %v", pending, err)
	}
	gateway.queryStatus, gateway.queryType, gateway.queryBody = 0, "", ""
	if err = a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	gateway.queryStatus, gateway.queryType, gateway.queryBody = http.StatusNotFound, "application/json", `{"error":{"key":"errors.order_not_found"}}`
	if err = a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	closed, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || closed.PaymentStatus != "closed" || closed.ReservationStatus != "released" || closed.GatewayURL != gateway.server.URL || closed.GatewayMerchantID != "merchant-test" {
		t.Fatalf("完整核验后未正确处理原网关订单不存在: %+v %v", closed, err)
	}
}

func TestGatewayIdentityChangeCannotReleaseStock(t *testing.T) {
	for _, field := range []string{"merchant", "gateway"} {
		t.Run(field, func(t *testing.T) {
			a, gateway := newTestApp(t)
			order, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
			if err := a.syncOrder(context.Background(), order.ID); err != nil {
				t.Fatal(err)
			}
			if field == "merchant" {
				a.config.HashPay.MerchantID = "other"
			} else {
				a.config.HashPay.BaseURL = "https://other.example"
			}
			queries := gatewayQueryCount(gateway)
			if err := a.syncOrder(context.Background(), order.ID); err == nil || !strings.Contains(err.Error(), "原网关或商户") {
				t.Fatal("配置变更未被识别")
			}
			pending, err := a.loadOrder(context.Background(), order.ID)
			if err != nil || pending.ReservationStatus != "reserved" || gatewayQueryCount(gateway) != queries {
				t.Fatal("配置变更后仍查询或释放了旧订单")
			}
		})
	}
}

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
			if result := request("PUT", "/api/admin/settings", `{"title":"HashDrop","hashpay_url":"https://other.example/","merchant_id":"other"}`); result.Code != 200 {
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
			claim.Header.Set("Authorization", claimAuthorization(token))
			receipt := httptest.NewRecorder()
			a.routes().ServeHTTP(receipt, claim)
			if receipt.Code != 200 || !strings.Contains(receipt.Body.String(), `"payment_status":"`+sample.status+`"`) {
				t.Fatalf("更换配置破坏了历史领取链接: %s", receipt.Body)
			}
			if strings.Contains(receipt.Body.String(), "卡密-0") {
				t.Fatal("状态查询泄露卡密")
			}
			if sample.status == "paid" {
				entries, err := a.deliveryMetadata(context.Background(), order.ID)
				if err != nil {
					t.Fatal(err)
				}
				claimed := claimDeliveryForTest(t, a, order.ID, entries[0].ID, token)
				if claimed.Code != 200 || !strings.Contains(claimed.Body.String(), "卡密-0") {
					t.Fatal("更换配置破坏历史领取", claimed.Body)
				}
			}
		})
	}
}

func TestGatewaySettingsProtectUnfinishedOrders(t *testing.T) {
	for _, state := range []struct{ payment, delivery string }{
		{"creating", "pending"}, {"pending", "pending"}, {"expired", "pending"}, {"review", "pending"},
		{"paid", "pending"}, {"paid", "partial"}, {"paid", "failed"},
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
