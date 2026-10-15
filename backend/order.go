package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type Order struct {
	ID                string          `json:"id"`
	ProductID         string          `json:"product_id"`
	ProductName       string          `json:"product_name"`
	Kind              string          `json:"kind"`
	UnitPrice         int64           `json:"unit_price"`
	Currency          string          `json:"currency"`
	Quantity          int             `json:"quantity"`
	Total             int64           `json:"total"`
	OriginalTotal     int64           `json:"original_total"`
	DiscountTotal     int64           `json:"discount_total"`
	Discounts         []OrderDiscount `json:"discounts"`
	GatewayID         string          `json:"gateway_id"`
	GatewayURL        string          `json:"-"`
	GatewayMerchantID string          `json:"-"`
	CheckoutURL       string          `json:"checkout_url"`
	PaymentStatus     string          `json:"payment_status"`
	DeliveryStatus    string          `json:"delivery_status"`
	ReservationStatus string          `json:"reservation_status"`
	ExpiresAt         int64           `json:"expires_at"`
	CreatedAt         int64           `json:"created_at"`
	Content           string          `json:"-"`
	TokenHash         string          `json:"-"`
	Attempts          int             `json:"attempts"`
	LastError         string          `json:"last_error"`
}

const orderSelect = `SELECT id,product_id,product_name,kind,unit_price,currency,quantity,total,COALESCE(gateway_id,''),gateway_url,gateway_merchant_id,checkout_url,payment_status,delivery_status,reservation_status,expires_at,created_at,content,token_hash,attempts,last_error,discounts FROM orders`

func scanOrder(row scanner) (Order, error) {
	var o Order
	var discounts string
	err := row.Scan(&o.ID, &o.ProductID, &o.ProductName, &o.Kind, &o.UnitPrice, &o.Currency, &o.Quantity, &o.Total, &o.GatewayID, &o.GatewayURL, &o.GatewayMerchantID, &o.CheckoutURL, &o.PaymentStatus, &o.DeliveryStatus, &o.ReservationStatus, &o.ExpiresAt, &o.CreatedAt, &o.Content, &o.TokenHash, &o.Attempts, &o.LastError, &discounts)
	if err == nil {
		err = json.Unmarshal([]byte(discounts), &o.Discounts)
	}
	o.OriginalTotal = o.UnitPrice * int64(o.Quantity)
	o.DiscountTotal = o.OriginalTotal - o.Total
	return o, err
}

type orderInput struct {
	ProductID      string `json:"product_id"`
	Quantity       int    `json:"quantity"`
	CouponCode     string `json:"coupon_code"`
	QuotedTotal    *int64 `json:"quoted_total"`
	QuotedCurrency string `json:"quoted_currency"`
}

func (a *App) loadOrder(ctx context.Context, id string) (Order, error) {
	return scanOrder(a.db.QueryRowContext(ctx, orderSelect+" WHERE id=?", id))
}
func (a *App) createOrder(w http.ResponseWriter, r *http.Request) error {
	if !a.limits.allow("create:"+clientIP(r), 10, time.Minute) {
		return &apiError{429, "下单过于频繁，请稍后再试"}
	}
	// 未配置网关时不接受下单，避免积累无法付款的预留。
	if _, err := newHashPay(a.settings().HashPay); err != nil {
		return &apiError{503, "支付服务尚未配置，请联系店主"}
	}
	var input orderInput
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	if input.Quantity < 1 || input.Quantity > 100 {
		return bad("数量须为 1–100")
	}
	o, token, err := a.reservePricedOrder(r.Context(), input)
	if err != nil {
		return err
	}
	// 持久化后立即返回凭证，网关延迟或排队不能阻塞领取入口的交付。
	// 只唤醒单个内置调度器，不为每笔订单启动额外 goroutine。
	select {
	case a.wake <- struct{}{}:
	default:
	}
	reply(w, 201, map[string]any{"order": o, "token": token})
	return nil
}
func (a *App) reserveOrder(ctx context.Context, productID string, quantity int) (Order, string, error) {
	return a.reservePricedOrder(ctx, orderInput{ProductID: productID, Quantity: quantity})
}
func (a *App) reservePricedOrder(ctx context.Context, input orderInput) (Order, string, error) {
	productID, quantity := input.ProductID, input.Quantity
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, "", err
	}
	defer tx.Rollback()
	p, err := scanProduct(tx.QueryRowContext(ctx, productSelect+" WHERE p.id=? AND p.active=1 AND p.deleted_at IS NULL", productID))
	if err == sql.ErrNoRows {
		return Order{}, "", bad("商品不存在或已下架")
	}
	if err != nil {
		return Order{}, "", err
	}
	quote, err := quoteOrder(ctx, tx, p, quantity, input.CouponCode, unixNow())
	if err != nil {
		return Order{}, "", err
	}
	if input.QuotedTotal != nil && (*input.QuotedTotal != quote.Total || input.QuotedCurrency != quote.Currency) {
		return Order{}, "", &apiError{409, "优惠或价格已变化，请重新确认金额"}
	}
	token, id := randomToken(), newID()
	content := ""
	if p.Kind == "static" {
		plain, e := a.vault.decrypt(p.Content, "product:"+p.ID)
		if e != nil {
			return Order{}, "", e
		}
		content, e = a.vault.encrypt(plain, "order:"+id)
		if e != nil {
			return Order{}, "", e
		}
	}
	o := Order{ID: id, ProductID: p.ID, ProductName: p.Name, Kind: p.Kind, UnitPrice: p.Price, Currency: p.Currency, Quantity: quantity, Total: p.Price * int64(quantity), TokenHash: tokenHash(token), Content: content, PaymentStatus: "creating", DeliveryStatus: "pending", ReservationStatus: "reserved", CreatedAt: unixNow()}
	o.Total, o.OriginalTotal, o.DiscountTotal, o.Discounts = quote.Total, quote.OriginalTotal, quote.DiscountTotal, quote.Discounts
	snapshot, err := json.Marshal(o.Discounts)
	if err != nil {
		return Order{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO orders(id,product_id,product_name,kind,unit_price,currency,quantity,total,content,token_hash,payment_status,created_at,updated_at,discounts) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, o.ID, o.ProductID, o.ProductName, o.Kind, o.UnitPrice, o.Currency, o.Quantity, o.Total, o.Content, o.TokenHash, o.PaymentStatus, o.CreatedAt, o.CreatedAt, string(snapshot)); err != nil {
		return Order{}, "", err
	}
	// 与库存预留同一写事务占用优惠次数；只有可靠终止且未付款的订单才归还名额。
	for _, discount := range o.Discounts {
		if _, err = tx.ExecContext(ctx, "INSERT INTO order_promotions(order_id,promotion_id) VALUES(?,?)", id, discount.ID); err != nil {
			return Order{}, "", err
		}
	}
	stockIDs := []string{}
	if p.Kind == "card" || p.Kind == "account" {
		rows, e := tx.QueryContext(ctx, "SELECT id FROM stock_items WHERE product_id=? AND status='available' ORDER BY created_at,id LIMIT ?", p.ID, quantity)
		if e != nil {
			return Order{}, "", e
		}
		for rows.Next() {
			var stock string
			if e = rows.Scan(&stock); e != nil {
				rows.Close()
				return Order{}, "", e
			}
			stockIDs = append(stockIDs, stock)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return Order{}, "", e
		}
		if len(stockIDs) != quantity {
			return Order{}, "", &apiError{409, "库存不足，请调整数量"}
		}
		for _, stock := range stockIDs {
			result, e := tx.ExecContext(ctx, "UPDATE stock_items SET status='reserved',order_id=? WHERE id=? AND status='available'", id, stock)
			if e != nil {
				return Order{}, "", e
			}
			n, e := result.RowsAffected()
			if e != nil || n != 1 {
				return Order{}, "", &apiError{409, "库存发生变化，请重试"}
			}
		}
	}
	for i := 1; i <= quantity; i++ {
		var stock any
		if len(stockIDs) > 0 {
			stock = stockIDs[i-1]
		}
		deliveryID := newID()
		if _, err = tx.ExecContext(ctx, "INSERT INTO deliveries(id,order_id,ordinal,stock_id,updated_at) VALUES(?,?,?,?,?)", deliveryID, id, i, stock, unixNow()); err != nil {
			return Order{}, "", err
		}
	}
	if err = tx.Commit(); err != nil {
		return Order{}, "", err
	}
	audit("order_created", id)
	return o, token, nil
}
func (a *App) orderDetail(w http.ResponseWriter, r *http.Request) error {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(token) != 43 || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return &apiError{401, "请输入有效的领取凭证"}
	}
	o, err := a.loadOrder(r.Context(), r.PathValue("id"))
	if err == sql.ErrNoRows {
		return &apiError{404, "订单或凭证无效"}
	}
	if err != nil {
		return err
	}
	if !constantEqual(o.TokenHash, tokenHash(token)) {
		return &apiError{404, "订单或凭证无效"}
	}
	results, err := a.deliveryResults(r.Context(), o.ID)
	if err != nil {
		return err
	}
	o.LastError = ""
	reply(w, 200, map[string]any{"order": o, "deliveries": results})
	return nil
}
func (a *App) syncOrder(ctx context.Context, id string) error {
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	err := a.syncOrderLocked(ctx, id)
	if err != nil {
		_, dbErr := a.db.ExecContext(context.Background(), "UPDATE orders SET attempts=attempts+1,next_check=?,last_error=?,updated_at=? WHERE id=?", unixNow()+120, safeGatewayError(err), unixNow(), id)
		if dbErr != nil {
			return dbErr
		}
	}
	return err
}
func safeGatewayError(err error) string {
	if strings.HasPrefix(err.Error(), "HashPay ") {
		return err.Error()
	}
	return "付款查询或交付暂时失败"
}
func (a *App) syncOrderLocked(ctx context.Context, id string) error {
	o, err := a.loadOrder(ctx, id)
	if err != nil {
		return err
	}
	config := a.settings()
	if (o.GatewayURL != "" || o.GatewayMerchantID != "") &&
		(o.GatewayURL != strings.TrimRight(config.HashPay.BaseURL, "/") || o.GatewayMerchantID != config.HashPay.MerchantID) {
		return errors.New("HashPay 配置与订单原网关或商户不一致，需核查")
	}
	client, err := newHashPay(config.HashPay)
	if err != nil {
		return err
	}
	if o.GatewayID == "" {
		remote, checkout, err := client.create(ctx, o, strings.TrimRight(config.PublicURL, "/")+"/orders/"+id)
		if err != nil {
			return err
		}
		expire := remote.ExpiresAt
		if expire == 0 {
			expire = remote.ExpireAt
		}
		if expire <= 0 {
			return errors.New("HashPay 有效期缺失")
		}
		if _, err = a.db.ExecContext(ctx, "UPDATE orders SET gateway_id=?,checkout_url=?,expires_at=?,payment_status='pending',next_check=?,last_error='',updated_at=? WHERE id=? AND gateway_id IS NULL", remote.ID, checkout, expire, unixNow()+30, unixNow(), id); err != nil {
			return err
		}
		o.GatewayID = remote.ID
	}
	remote, err := client.query(ctx, o.GatewayID)
	if err != nil {
		var failure *hashPayHTTPError
		if errors.As(err, &failure) && failure.Status == http.StatusNotFound && failure.Key == "errors.order_not_found" {
			// 已核验付款不会因网关删除而丢失，未完成交付继续使用原预留。
			if o.PaymentStatus == "paid" {
				return a.deliverOrder(ctx, o)
			}
			if o.GatewayURL == "" || o.GatewayMerchantID == "" {
				return errors.New("HashPay 订单不存在，但尚未核验原商户归属，保留库存待核查")
			}
			return a.terminateOrder(ctx, id, "closed", "HashPay 网关订单已不存在")
		}
		return err
	}
	amount, err := decimalMinor(remote.Amount.String(), o.Currency)
	if err != nil || remote.ID != o.GatewayID || remote.MerchantID != config.HashPay.MerchantID || remote.MerchantNo != o.ID || remote.Currency != o.Currency || amount != o.Total {
		if _, e := a.db.ExecContext(ctx, "UPDATE orders SET payment_status='review',last_error='支付订单身份、金额或币种不匹配',next_check=?,updated_at=? WHERE id=? AND payment_status<>'paid'", unixNow()+900, unixNow(), id); e != nil {
			return e
		}
		return errors.New("HashPay 查询结果不匹配")
	}
	// 仅在签名查询完整核验订单归属、金额和币种后保存映射；旧订单不盲目回填当前配置。
	if o.GatewayURL == "" && o.GatewayMerchantID == "" {
		if _, err = a.db.ExecContext(ctx, "UPDATE orders SET gateway_url=?,gateway_merchant_id=? WHERE id=?", strings.TrimRight(config.HashPay.BaseURL, "/"), config.HashPay.MerchantID, id); err != nil {
			return err
		}
	}
	if remote.Status == "paid" {
		// 已完成签名查询和身份/金额核验，付款状态由 HashPay 决定。
		if err = a.markPaid(ctx, id); err != nil {
			return err
		}
		o, err = a.loadOrder(ctx, id)
		if err != nil {
			return err
		}
		return a.deliverOrder(ctx, o)
	}
	if remote.Status == "invalid" {
		return a.terminateOrder(ctx, id, "invalid", "HashPay 订单已失效")
	}
	if remote.Status != "pending" && remote.Status != "expired" {
		return errors.New("HashPay 订单状态未知")
	}
	next := int64(30)
	if remote.Status == "expired" {
		next = 900 // 过期仍可能迟到付款，继续保留库存并补查。
	}
	_, err = a.db.ExecContext(ctx, "UPDATE orders SET payment_status=?,next_check=?,last_error='',updated_at=? WHERE id=?", remote.Status, unixNow()+next, unixNow(), id)
	return err
}

func (a *App) terminateOrder(ctx context.Context, id, status, reason string) error {
	// 核验 invalid 或原商户的订单不存在响应后，只释放尚未交付的预留。
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE stock_items SET status='available',order_id=NULL WHERE order_id=? AND status='reserved'", id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE deliveries SET stock_id=NULL,status='manual',last_error=?,updated_at=? WHERE order_id=? AND status<>'done'", reason, unixNow(), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE orders SET payment_status=?,reservation_status=CASE WHEN reservation_status='reserved' THEN 'released' ELSE reservation_status END,delivery_status=CASE WHEN delivery_status='done' THEN 'done' ELSE 'cancelled' END,last_error=?,updated_at=? WHERE id=?", status, reason, unixNow(), id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("gateway_order_"+status, id)
	return nil
}
func (a *App) markPaid(ctx context.Context, id string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var reservation string
	if err = tx.QueryRowContext(ctx, "SELECT reservation_status FROM orders WHERE id=?", id).Scan(&reservation); err != nil {
		return err
	}
	delivery := "pending"
	if reservation == "released" {
		delivery = "manual"
	}
	if _, err = tx.ExecContext(ctx, "UPDATE orders SET payment_status='paid',paid_at=COALESCE(paid_at,?),delivery_status=CASE WHEN payment_status='paid' THEN delivery_status ELSE ? END,last_error='',updated_at=? WHERE id=?", unixNow(), delivery, unixNow(), id); err != nil {
		return err
	}
	if reservation == "released" {
		if _, err = tx.ExecContext(ctx, "UPDATE deliveries SET status='manual',last_error='库存已释放，需人工补货',updated_at=? WHERE order_id=? AND status<>'done'", unixNow(), id); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("payment_verified", id)
	return nil
}
