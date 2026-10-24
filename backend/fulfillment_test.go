package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestUnknownDeliveryKindDoesNotChangeOrder(t *testing.T) {
	a, _ := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	order, _ := reserve(t, a, product, 1)
	order.PaymentStatus, order.Kind = "paid", "unsupported"
	if err := a.deliverOrder(context.Background(), order); err == nil {
		t.Fatal("接受了无效交付类型")
	}
	var orderStatus, deliveryStatus string
	if err := a.db.QueryRow("SELECT delivery_status FROM orders WHERE id=?", order.ID).Scan(&orderStatus); err != nil || orderStatus != "pending" {
		t.Fatal("无效类型改变了订单交付状态", orderStatus, err)
	}
	if err := a.db.QueryRow("SELECT status FROM deliveries WHERE order_id=?", order.ID).Scan(&deliveryStatus); err != nil || deliveryStatus != "pending" {
		t.Fatal("无效类型改变了逐份交付状态", deliveryStatus, err)
	}
}

func TestManualPurchaseAndPartialDelivery(t *testing.T) {
	a, gateway := newTestApp(t)
	admin := adminSessionRequest(t, a)
	created := admin("POST", "/api/admin/products", `{"name":"人工充值","price":100,"currency":"CNY","active":true,"delivery_mode":"manual","delivery_note":"两小时内完成，请保留密钥","buyer_note_label":"充值账号","buyer_note_required":true,"min_quantity":2,"max_quantity":3}`)
	var p Product
	if created.Code != 200 || json.Unmarshal(created.Body.Bytes(), &p) != nil || p.Kind != "manual" || p.InventoryCount != 0 {
		t.Fatal("人工商品不能独立上架", created.Body)
	}
	input := orderInput{ProductID: p.ID, Quantity: 2, Key: "manual-order-key", RequestID: newID(), BuyerNote: "账号@example.com"}
	for _, invalid := range []orderInput{
		{ProductID: p.ID, Quantity: 1, Key: "invalid-quantity"},
		{ProductID: p.ID, Quantity: 4, Key: "invalid-quantity"},
		{ProductID: p.ID, Quantity: 2, Key: "missing-buyer"},
	} {
		if _, _, err := a.reservePricedOrder(context.Background(), invalid); err == nil {
			t.Fatal("非法数量或缺失必填信息仍可下单")
		}
	}
	o, _, err := a.reservePricedOrder(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = a.db.QueryRow("SELECT buyer_note FROM orders WHERE id=?", o.ID).Scan(&stored); err != nil || strings.Contains(stored, input.BuyerNote) {
		t.Fatal("买家信息没有加密", err)
	}
	if _, _, err = a.reservePricedOrder(context.Background(), input); err != nil {
		t.Fatal("下单重试无法恢复原订单", err)
	}
	changed := input
	changed.BuyerNote = "另一个账号"
	if _, _, err = a.reservePricedOrder(context.Background(), changed); err == nil {
		t.Fatal("同一请求标识可改写买家信息")
	}
	lookup := func() *Order {
		response := claimRequest(t, a, "/api/orders/lookup", map[string]string{"key": input.Key})
		var detail struct {
			Order Order `json:"order"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &detail) != nil {
			t.Fatal(response.Body)
		}
		return &detail.Order
	}
	var deliveryID string
	if err = a.db.QueryRow("SELECT id FROM deliveries WHERE order_id=? AND ordinal=1", o.ID).Scan(&deliveryID); err != nil {
		t.Fatal(err)
	}
	fulfill := func(id, content string) int {
		body, err := json.Marshal(map[string]string{"delivery_id": id, "content": content})
		if err != nil {
			t.Fatal(err)
		}
		return admin("POST", "/api/admin/orders/"+o.ID+"/fulfill", string(body)).Code
	}
	if fulfill(deliveryID, "未付款发货") != 400 {
		t.Fatal("未付款订单允许发货")
	}
	syncPaid(t, a, gateway, o)
	if got := lookup(); got.DeliveryStatus != "manual" || got.BuyerNote != input.BuyerNote || got.DeliveryNote != o.DeliveryNote {
		t.Fatal("人工订单状态或快照无效", got)
	}
	if response := admin("PUT", "/api/admin/products/"+p.ID, `{"name":"改名","price":200,"currency":"CNY","active":false,"delivery_note":"新说明"}`); response.Code != 200 {
		t.Fatal(response.Body)
	}
	if fulfill(deliveryID, "首份已完成\n凭证-001") != 200 || fulfill(deliveryID, "首份已完成\n凭证-001") != 200 || fulfill(deliveryID, "覆盖内容") != 409 {
		t.Fatal("人工交付未保持幂等与不可覆盖")
	}
	if got := lookup(); got.DeliveryStatus != "manual" || got.ProductName != "人工充值" || got.DeliveryNote != o.DeliveryNote {
		t.Fatal("部分交付或商品快照被改变", got)
	}
	if err = a.db.QueryRow("SELECT id FROM deliveries WHERE order_id=? AND ordinal=2", o.ID).Scan(&deliveryID); err != nil {
		t.Fatal(err)
	}
	if fulfill(deliveryID, "第二份已完成") != 200 {
		t.Fatal("无法完成剩余交付")
	}
	if got := lookup(); got.DeliveryStatus != "done" || got.ReservationStatus != "consumed" {
		t.Fatal("完成状态未收敛", got)
	}
	results, err := a.deliveryResults(context.Background(), o.ID)
	if err != nil || len(results) != 2 || results[0].Content != "首份已完成\n凭证-001" || results[1].Content != "第二份已完成" {
		t.Fatal("领取内容不正确", results, err)
	}
	var stocks int
	if err = a.db.QueryRow("SELECT COUNT(*) FROM stock_items").Scan(&stocks); err != nil || stocks != 0 {
		t.Fatal("人工订单错误占用了库存", stocks, err)
	}
}

func TestVerifiedPaymentCannotRegressBeforeDelivery(t *testing.T) {
	a, g := newTestApp(t)
	o, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
	if err := a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.markPaid(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	g.set(o.ID, "invalid", "")
	if err := a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal("已核验的订单因网关状态回退而无法交付", err)
	}
	got, err := a.loadOrder(context.Background(), o.ID)
	if err != nil || got.PaymentStatus != "paid" || got.DeliveryStatus != "done" {
		t.Fatal("已核验付款回退或未完成交付", got, err)
	}
}

func TestManualRecoveryReleasesReservationAndRejectsOtherOrderItems(t *testing.T) {
	a, _ := newTestApp(t)
	p := insertProduct(t, a, "card", 2)
	o, _ := reserve(t, a, p, 1)
	other, _ := reserve(t, a, p, 1)
	if err := a.markPaid(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	admin := adminSessionRequest(t, a)
	var id string
	if err := a.db.QueryRow("SELECT id FROM deliveries WHERE order_id=?", other.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	response := admin("POST", "/api/admin/orders/"+o.ID+"/fulfill", `{"delivery_id":"`+id+`","content":"补交内容"}`)
	if response.Code != http.StatusNotFound {
		t.Fatal("可以修改其他订单的交付", response.Body)
	}
	if err := a.db.QueryRow("SELECT id FROM deliveries WHERE order_id=?", o.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	response = admin("POST", "/api/admin/orders/"+o.ID+"/fulfill", `{"delivery_id":"`+id+`","content":"补交内容"}`)
	if response.Code != 200 {
		t.Fatal(response.Body)
	}
	var available, reserved int
	if err := a.db.QueryRow("SELECT SUM(status='available'),SUM(status='reserved') FROM stock_items").Scan(&available, &reserved); err != nil || available != 1 || reserved != 1 {
		t.Fatal("补交没有归还原预留或影响了其他订单", available, reserved, err)
	}
}
