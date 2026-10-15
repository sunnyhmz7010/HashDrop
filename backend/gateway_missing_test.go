package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	claim.Header.Set("Authorization", "Bearer "+token)
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
	if err != nil || late.PaymentStatus != "paid" || late.DeliveryStatus != "manual" {
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
			if err != nil || len(results) != 1 || results[0].Content != "卡密-0" {
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

func TestLegacyGatewayOwnershipMustBeVerifiedBeforeRelease(t *testing.T) {
	a, gateway := newTestApp(t)
	order, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
	if err := a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE orders SET gateway_url='',gateway_merchant_id='' WHERE id=?", order.ID); err != nil {
		t.Fatal(err)
	}
	gateway.mu.Lock()
	original := gateway.orders[order.ID]
	delete(gateway.orders, order.ID)
	gateway.mu.Unlock()
	if err := a.syncOrder(context.Background(), order.ID); err == nil || !strings.Contains(err.Error(), "尚未核验") {
		t.Fatal("旧订单未保留归属核查提示")
	}
	unknown, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || unknown.ReservationStatus != "reserved" || unknown.GatewayMerchantID != "" {
		t.Fatal("旧订单首次 404 被盲目释放或绑定")
	}
	gateway.mu.Lock()
	gateway.orders[order.ID], gateway.corrupt = original, "merchant"
	gateway.mu.Unlock()
	if err = a.syncOrder(context.Background(), order.ID); err == nil {
		t.Fatal("未拒绝错误商户")
	}
	unverified, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || unverified.GatewayMerchantID != "" {
		t.Fatal("错误商户响应被用作归属证据")
	}
	gateway.mu.Lock()
	gateway.corrupt = ""
	gateway.mu.Unlock()
	if err = a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	verified, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || verified.GatewayMerchantID != "merchant-test" || verified.GatewayURL != gateway.server.URL {
		t.Fatal("旧订单成功核验后未绑定原商户")
	}
	gateway.mu.Lock()
	delete(gateway.orders, order.ID)
	gateway.mu.Unlock()
	if err = a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	closed, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || closed.PaymentStatus != "closed" || closed.ReservationStatus != "released" {
		t.Fatal("已核验的旧订单删除后未释放库存")
	}
}
