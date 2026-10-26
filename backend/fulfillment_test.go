package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDeliveryUsesPersistedPaymentAndExplicitClaim(t *testing.T) {
	a, _ := newTestApp(t)
	order, key := reserve(t, a, insertProduct(t, a, "card", 1), 1)
	order.PaymentStatus = "paid"
	if err := a.deliverOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	entries, err := a.deliveryMetadata(context.Background(), order.ID)
	if err != nil || entries[0].Status != "pending" {
		t.Fatal("未付款交付被伪造调用放行", entries, err)
	}
	if response := claimDeliveryForTest(t, a, order.ID, entries[0].ID, key); response.Code != 409 {
		t.Fatal("未付款内容可领取", response.Body)
	}
	if err = a.markPaid(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	if err = a.deliverOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	lookup := claimRequest(t, a, "/api/orders/lookup", map[string]string{"key": key})
	if lookup.Code != 200 || strings.Contains(lookup.Body.String(), "卡密-") || strings.Contains(lookup.Body.String(), `"content"`) {
		t.Fatal("查询泄露正文", lookup.Body)
	}
	claimed := claimDeliveryForTest(t, a, order.ID, entries[0].ID, key)
	if claimed.Code != 200 || !strings.Contains(claimed.Body.String(), "卡密-0") {
		t.Fatal("主动领取失败", claimed.Body)
	}
	if response := claimDeliveryForTest(t, a, order.ID, entries[0].ID, randomToken()); response.Code != 404 {
		t.Fatal("错误凭证可领取", response.Body)
	}
}

func TestPartialFailureRetryDoesNotReallocateOrQueryGateway(t *testing.T) {
	a, g := newTestApp(t)
	order, _ := reserve(t, a, insertProduct(t, a, "card", 2), 2)
	entries, err := a.deliveryMetadata(context.Background(), order.ID)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot string
	if err = a.db.QueryRow("SELECT content FROM deliveries WHERE id=?", entries[0].ID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec("UPDATE deliveries SET content='broken' WHERE id=?", entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = a.markPaid(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	if err = a.deliverOrder(context.Background(), order); err == nil {
		t.Fatal("损坏快照未报错")
	}
	got, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || got.DeliveryStatus != "partial" {
		t.Fatal("未记录部分完成", got, err)
	}
	before, err := a.deliveryResults(context.Background(), order.ID)
	if err != nil || before[0].Status != "failed" || before[1].Status != "done" {
		t.Fatal(before, err)
	}
	if _, err = a.db.Exec("UPDATE deliveries SET content=? WHERE id=?", snapshot, entries[0].ID); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	g.queryStatus = http.StatusServiceUnavailable
	g.mu.Unlock()
	admin := adminSessionRequest(t, a)
	for range 2 {
		if response := admin("POST", "/api/admin/orders/"+order.ID+"/retry", "{}"); response.Code != 200 {
			t.Fatal(response.Body)
		}
	}
	after, err := a.deliveryResults(context.Background(), order.ID)
	if err != nil || after[0].Status != "done" || after[1].Status != "done" || deliveryText(after[1].Content) != deliveryText(before[1].Content) {
		t.Fatal("重试更改已完成结果", after, err)
	}
	var sold, allocations int
	if err = a.db.QueryRow("SELECT COUNT(*) FROM stock_items WHERE status='sold'").Scan(&sold); err != nil {
		t.Fatal(err)
	}
	if err = a.db.QueryRow("SELECT COUNT(*) FROM delivery_allocations").Scan(&allocations); err != nil {
		t.Fatal(err)
	}
	if sold != 2 || allocations != 2 {
		t.Fatal("重试重复分配库存", sold, allocations)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.queries != 0 {
		t.Fatal("交付重试依赖支付网关")
	}
}

func TestManualRecoveryIsAtomicIdempotentAndScoped(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "card", 3)
	order, _ := reserve(t, a, product, 2)
	other, _ := reserve(t, a, product, 1)
	entries, err := a.deliveryMetadata(context.Background(), order.ID)
	if err != nil {
		t.Fatal(err)
	}
	others, err := a.deliveryMetadata(context.Background(), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	admin := adminSessionRequest(t, a)
	fulfill := func(id, text string) int {
		body, e := json.Marshal(map[string]any{"delivery_id": id, "content": testDocument(text)})
		if e != nil {
			t.Fatal(e)
		}
		return admin("POST", "/api/admin/orders/"+order.ID+"/fulfill", string(body)).Code
	}
	if fulfill(entries[0].ID, "补交") != 400 {
		t.Fatal("未付款订单允许补交")
	}
	if err = a.markPaid(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	if fulfill(others[0].ID, "越权") != 404 {
		t.Fatal("可补交其他订单")
	}
	if fulfill(entries[0].ID, "第一份") != 200 || fulfill(entries[0].ID, "第一份") != 200 || fulfill(entries[0].ID, "覆盖") != 409 {
		t.Fatal("补交未保持幂等及不可覆盖")
	}
	got, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || got.DeliveryStatus != "partial" {
		t.Fatal(got, err)
	}
	if fulfill(entries[1].ID, "第二份") != 200 {
		t.Fatal("剩余内容不能补交")
	}
	var available, reserved int
	if err = a.db.QueryRow("SELECT SUM(status='available'),SUM(status='reserved') FROM stock_items").Scan(&available, &reserved); err != nil || available != 2 || reserved != 1 {
		t.Fatal("补交未归还预留或影响了其他订单", available, reserved, err)
	}
	got, err = a.loadOrder(context.Background(), order.ID)
	if err != nil || got.DeliveryStatus != "done" || got.ReservationStatus != "consumed" {
		t.Fatal(got, err)
	}
}

func TestVerifiedPaymentCannotRegressBeforeDelivery(t *testing.T) {
	a, g := newTestApp(t)
	order, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
	if err := a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.markPaid(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	g.set(order.ID, "invalid", "")
	if err := a.syncOrder(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	got, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || got.PaymentStatus != "paid" || got.DeliveryStatus != "done" {
		t.Fatal("已核验付款回退", got, err)
	}
}

func TestPaidSchedulerDoesNotDependOnGateway(t *testing.T) {
	a, g := newTestApp(t)
	order, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
	if err := a.markPaid(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	g.queryStatus = 503
	g.mu.Unlock()
	a.runPending(context.Background())
	got, err := a.loadOrder(context.Background(), order.ID)
	if err != nil || got.DeliveryStatus != "done" {
		t.Fatal("网关故障阻塞本地交付", got, err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.queries != 0 {
		t.Fatal("已付款调度仍访问网关")
	}
}
