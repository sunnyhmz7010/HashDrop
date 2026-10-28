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
	TokenHash         string          `json:"-"`
	Attempts          int             `json:"attempts"`
	LastError         string          `json:"last_error"`
	DeliveryNote      string          `json:"delivery_note"`
}

const orderSelect = `SELECT id,product_id,product_name,unit_price,currency,quantity,total,COALESCE(gateway_id,''),gateway_url,gateway_merchant_id,checkout_url,payment_status,delivery_status,reservation_status,expires_at,created_at,token_hash,attempts,last_error,discounts,delivery_note FROM orders`

func scanOrder(row scanner) (Order, error) {
	var o Order
	var discounts string
	err := row.Scan(&o.ID, &o.ProductID, &o.ProductName, &o.UnitPrice, &o.Currency, &o.Quantity, &o.Total, &o.GatewayID, &o.GatewayURL, &o.GatewayMerchantID, &o.CheckoutURL, &o.PaymentStatus, &o.DeliveryStatus, &o.ReservationStatus, &o.ExpiresAt, &o.CreatedAt, &o.TokenHash, &o.Attempts, &o.LastError, &discounts, &o.DeliveryNote)
	if err == nil {
		err = json.Unmarshal([]byte(discounts), &o.Discounts)
	}
	o.OriginalTotal = o.UnitPrice * int64(o.Quantity)
	o.DiscountTotal = o.OriginalTotal - o.Total
	return o, err
}

type orderInput struct {
	Key            string `json:"key"`
	RequestID      string `json:"request_id"`
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
	if err := validateClaimKey(input.Key); err != nil {
		return err
	}
	if input.RequestID == "" {
		input.RequestID = newID()
	} else if !validRequestID(input.RequestID) {
		return bad("下单请求标识无效，请重新打开下单弹窗")
	}
	if input.QuotedTotal == nil || input.QuotedCurrency == "" {
		return bad("请先获取并确认订单报价")
	}
	o, _, err := a.reservePricedOrder(r.Context(), input)
	if errors.Is(err, errClaimKeyUsed) {
		suggestions, e := a.suggestClaimKeys(r.Context(), input.Key)
		if e != nil {
			return e
		}
		reply(w, 409, map[string]any{"error": "领取密钥已被使用，请更换或选择派生密钥", "code": "key_conflict", "suggestions": suggestions})
		return nil
	}
	if err != nil {
		return err
	}
	// 同一次下单的重试沿用订单快照、优惠占用及 merchantNo。
	if _, err = a.db.ExecContext(r.Context(), "UPDATE orders SET next_check=0 WHERE id=? AND payment_status='creating'", o.ID); err != nil {
		return err
	}
	// 持久化后立即返回订单，弹窗继续等待支付链接。
	// 只唤醒单个内置调度器，不为每笔订单启动额外 goroutine。
	select {
	case a.wake <- struct{}{}:
	default:
	}
	reply(w, 201, map[string]any{"order": o})
	return nil
}
func (a *App) reserveOrder(ctx context.Context, productID string, quantity int, token, id string) (Order, error) {
	o, _, err := a.reservePricedOrder(ctx, orderInput{ProductID: productID, Quantity: quantity, Key: token, RequestID: id})
	return o, err
}
func (a *App) reservePricedOrder(ctx context.Context, input orderInput) (Order, string, error) {
	productID, quantity := input.ProductID, input.Quantity
	token, id := input.Key, input.RequestID
	// 内部调用沿用随机凭证；公开下单入口必须先验证买家设置的密钥。
	if token == "" {
		token = randomToken()
	}
	if id == "" {
		id = newID()
	}
	if err := validateClaimKey(token); err != nil {
		return Order{}, "", err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, "", err
	}
	defer tx.Rollback()
	// 在重新报价与库存校验前恢复同一次请求，避免重复占用优惠次数。
	existing, err := scanOrder(tx.QueryRowContext(ctx, orderSelect+" WHERE id=?", id))
	if err == nil {
		if existing.ProductID != productID || existing.Quantity != quantity || !constantEqual(existing.TokenHash, tokenHash(token)) {
			return Order{}, "", &apiError{409, "下单请求标识无效，请重新打开下单弹窗"}
		}
		return existing, token, nil
	}
	if err != sql.ErrNoRows {
		return Order{}, "", err
	}
	var used bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM orders WHERE token_hash=?)", tokenHash(token)).Scan(&used); err != nil {
		return Order{}, "", err
	}
	if used {
		return Order{}, "", errClaimKeyUsed
	}
	p, err := scanProduct(tx.QueryRowContext(ctx, productSelect+" WHERE p.id=? AND p.active=1 AND p.deleted_at IS NULL", productID))
	if err == sql.ErrNoRows {
		return Order{}, "", bad("商品不存在或已下架")
	}
	if err != nil {
		return Order{}, "", err
	}
	if err = validatePurchaseQuantity(p, quantity); err != nil {
		return Order{}, "", err
	}
	if err = a.hydrateProduct(ctx, tx, &p); err != nil {
		return Order{}, "", err
	}
	quote, err := quoteOrder(ctx, tx, p, quantity, input.CouponCode, unixNow())
	if err != nil {
		var invalid *apiError
		if input.QuotedTotal != nil && errors.As(err, &invalid) && invalid.Status == 400 {
			return Order{}, "", &apiError{409, "优惠或价格已变化，请重新确认金额"}
		}
		return Order{}, "", err
	}
	if input.QuotedTotal != nil && (*input.QuotedTotal != quote.Total || input.QuotedCurrency != quote.Currency) {
		return Order{}, "", &apiError{409, "优惠或价格已变化，请重新确认金额"}
	}
	o := Order{ID: id, ProductID: p.ID, ProductName: p.Name, UnitPrice: p.Price, Currency: p.Currency, Quantity: quantity, Total: p.Price * int64(quantity), TokenHash: tokenHash(token), PaymentStatus: "creating", DeliveryStatus: "pending", ReservationStatus: "reserved", CreatedAt: unixNow()}
	o.Total, o.OriginalTotal, o.DiscountTotal, o.Discounts = quote.Total, quote.OriginalTotal, quote.DiscountTotal, quote.Discounts
	o.DeliveryNote = p.DeliveryNote
	snapshot, err := json.Marshal(o.Discounts)
	if err != nil {
		return Order{}, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO orders(id,product_id,product_name,unit_price,currency,quantity,total,token_hash,payment_status,created_at,updated_at,discounts,delivery_note) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, o.ID, o.ProductID, o.ProductName, o.UnitPrice, o.Currency, o.Quantity, o.Total, o.TokenHash, o.PaymentStatus, o.CreatedAt, o.CreatedAt, string(snapshot), o.DeliveryNote); err != nil {
		return Order{}, "", err
	}
	// 所有必需取货项和订单在同一事务内预留，任一资源缺货会整体回滚。
	allocations, err := a.allocateInventory(ctx, tx, p, quantity, id)
	if err != nil {
		return Order{}, "", err
	}
	// 与库存预留同一写事务占用优惠次数；只有可靠终止且未付款的订单才归还名额。
	for _, discount := range o.Discounts {
		if _, err = tx.ExecContext(ctx, "INSERT INTO order_promotions(order_id,promotion_id) VALUES(?,?)", id, discount.ID); err != nil {
			return Order{}, "", err
		}
	}
	for ordinal, item := range allocations {
		deliveryID := newID()
		plain, err := encodeContent(item.content)
		if err != nil {
			return Order{}, "", err
		}
		content, err := a.vault.encrypt(plain, "delivery-content:"+deliveryID)
		if err != nil {
			return Order{}, "", err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO deliveries(id,order_id,ordinal,content,updated_at) VALUES(?,?,?,?,?)", deliveryID, id, ordinal+1, content, unixNow()); err != nil {
			return Order{}, "", err
		}
		for index, resource := range item.resources {
			if _, err = tx.ExecContext(ctx, `INSERT INTO delivery_allocations(id,delivery_id,stock_id,inventory_id,slot_id,ordinal,consumes,status) VALUES(?,?,?,?,?,?,?,'reserved')`, newID(), deliveryID, resource.stock, resource.inventory, resource.slot, index+1, resource.consumes); err != nil {
				return Order{}, "", err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return Order{}, "", err
	}
	audit("order_created", id)
	return o, token, nil
}
func (a *App) orderDetail(w http.ResponseWriter, r *http.Request) error {
	o, err := a.authorizeOrderAccess(r)
	if err != nil {
		return err
	}
	return a.replyOrder(w, r, o)
}

// 身份凭证校验与领取动作分离，状态轮询不会隐式领取或消耗访问次数。
func (a *App) authorizeOrderAccess(r *http.Request) (Order, error) {
	refund, allowed := a.beginClaim(r)
	if !allowed {
		return Order{}, &apiError{429, "密钥尝试过于频繁，请稍后再试"}
	}
	token, valid := claimKeyFromAuthorization(r)
	if !valid {
		return Order{}, &apiError{401, "请输入有效的领取凭证"}
	}
	o, err := a.loadOrder(r.Context(), r.PathValue("id"))
	if err == sql.ErrNoRows {
		return Order{}, &apiError{404, "订单或凭证无效"}
	}
	if err != nil {
		refund()
		return Order{}, err
	}
	if !constantEqual(o.TokenHash, tokenHash(token)) {
		return Order{}, &apiError{404, "订单或凭证无效"}
	}
	refund()
	return o, nil
}
func (a *App) replyOrder(w http.ResponseWriter, r *http.Request, o Order) error {
	results, err := a.deliveryMetadata(r.Context(), o.ID)
	if err != nil {
		return err
	}
	o.LastError = ""
	reply(w, 200, map[string]any{"order": o, "deliveries": results})
	return nil
}

func validatePurchaseQuantity(p Product, quantity int) error {
	if quantity < p.MinQuantity || quantity > p.MaxQuantity {
		return bad("购买数量不在商品允许的范围内")
	}
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
		remote, checkout, err := client.create(ctx, o, strings.TrimRight(config.PublicURL, "/")+"/center/orders/"+o.ID)
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
	if !client.matchesPayment(remote, o.ID, o.GatewayID, o.Currency, o.Total) {
		if _, e := a.db.ExecContext(ctx, "UPDATE orders SET payment_status='review',last_error='支付订单身份、金额或币种不匹配',next_check=?,updated_at=? WHERE id=? AND payment_status<>'paid'", unixNow()+900, unixNow(), id); e != nil {
			return e
		}
		return errors.New("HashPay 查询结果不匹配")
	}
	// 仅在签名查询完整核验订单归属、金额和币种后保存映射。
	if o.GatewayURL == "" && o.GatewayMerchantID == "" {
		if _, err = a.db.ExecContext(ctx, "UPDATE orders SET gateway_url=?,gateway_merchant_id=? WHERE id=?", strings.TrimRight(config.HashPay.BaseURL, "/"), config.HashPay.MerchantID, id); err != nil {
			return err
		}
	}
	// 刷新仍核对原网关和金额，但已经核实的付款不能因后续状态回退而释放库存。
	if o.PaymentStatus == "paid" {
		return a.deliverOrder(ctx, o)
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
	var payment string
	if err = tx.QueryRowContext(ctx, "SELECT payment_status FROM orders WHERE id=?", id).Scan(&payment); err != nil {
		return err
	}
	if payment == "paid" {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM deliveries WHERE order_id=? AND status<>'done'", id)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var deliveryID string
		if err = rows.Scan(&deliveryID); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, deliveryID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, deliveryID := range ids {
		if err = releaseDeliveryAllocations(ctx, tx, id, deliveryID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE deliveries SET status='cancelled',last_error=?,updated_at=? WHERE order_id=? AND status<>'done'", reason, unixNow(), id); err != nil {
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
		delivery = "failed"
	}
	if _, err = tx.ExecContext(ctx, "UPDATE orders SET payment_status='paid',paid_at=COALESCE(paid_at,?),delivery_status=CASE WHEN payment_status='paid' THEN delivery_status ELSE ? END,last_error='',updated_at=? WHERE id=?", unixNow(), delivery, unixNow(), id); err != nil {
		return err
	}
	if reservation == "released" {
		if _, err = tx.ExecContext(ctx, "UPDATE deliveries SET status='failed',last_error='原库存已释放，需要人工补交',updated_at=? WHERE order_id=? AND status<>'done'", unixNow(), id); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("payment_verified", id)
	return nil
}
