package main

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type promotionInput struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Code         string   `json:"code"`
	DiscountType string   `json:"discount_type"`
	Value        int64    `json:"value"` // percentage 使用万分比：2000 表示减免 20%。
	Currency     string   `json:"currency"`
	MinTotal     int64    `json:"min_total"`
	MinQuantity  int      `json:"min_quantity"`
	Scope        string   `json:"scope"`
	ProductIDs   []string `json:"product_ids"`
	Stackable    bool     `json:"stackable"`
	Enabled      bool     `json:"enabled"`
	StartsAt     int64    `json:"starts_at"`
	EndsAt       int64    `json:"ends_at"`
	UsageLimit   int      `json:"usage_limit"`
}

type Promotion struct {
	promotionInput
	ID   string `json:"id"`
	Used int    `json:"used"`
}

type OrderDiscount struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Code   string `json:"code"`
	Amount int64  `json:"amount"`
}

type OrderQuote struct {
	OriginalTotal int64           `json:"original_total"`
	DiscountTotal int64           `json:"discount_total"`
	Total         int64           `json:"total"`
	Currency      string          `json:"currency"`
	Discounts     []OrderDiscount `json:"discounts"`
}

type promotionQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

const promotionSelect = `SELECT p.id,p.name,p.kind,p.code,p.discount_type,p.value,p.currency,p.min_total,p.min_quantity,p.scope,p.stackable,p.enabled,p.starts_at,p.ends_at,p.usage_limit,
 (SELECT COUNT(*) FROM order_promotions op JOIN orders o ON o.id=op.order_id WHERE op.promotion_id=p.id AND (o.payment_status NOT IN ('closed','invalid') OR o.paid_at IS NOT NULL)) FROM promotions p`

func scanPromotion(row scanner) (Promotion, error) {
	p := Promotion{promotionInput: promotionInput{ProductIDs: []string{}}}
	err := row.Scan(&p.ID, &p.Name, &p.Kind, &p.Code, &p.DiscountType, &p.Value, &p.Currency, &p.MinTotal, &p.MinQuantity, &p.Scope, &p.Stackable, &p.Enabled, &p.StartsAt, &p.EndsAt, &p.UsageLimit, &p.Used)
	return p, err
}

var couponPattern = regexp.MustCompile(`^[A-Z0-9_-]{3,64}$`)

func (a *App) adminPromotions(w http.ResponseWriter, r *http.Request) error {
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(r.Context(), promotionSelect+" WHERE p.deleted_at IS NULL ORDER BY p.created_at DESC,p.id")
	if err != nil {
		return err
	}
	items := []Promotion{}
	for rows.Next() {
		p, err := scanPromotion(rows)
		if err != nil {
			rows.Close()
			return err
		}
		items = append(items, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// 单连接 SQLite：关闭规则游标后再读取目标，避免嵌套查询等待自己。
	for i := range items {
		rows, err := tx.QueryContext(r.Context(), "SELECT product_id FROM promotion_products WHERE promotion_id=? ORDER BY product_id", items[i].ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			items[i].ProductIDs = append(items[i].ProductIDs, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	reply(w, 200, items)
	return nil
}

func (a *App) savePromotion(w http.ResponseWriter, r *http.Request) error {
	var input promotionInput
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Code = strings.ToUpper(strings.TrimSpace(input.Code))
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	if input.Kind == "automatic" {
		input.Code = ""
	}
	if input.Name == "" || !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) > 120 || (input.Kind != "automatic" && input.Kind != "code") ||
		(input.Kind == "code" && !couponPattern.MatchString(input.Code)) ||
		(input.DiscountType != "percentage" && input.DiscountType != "fixed") || input.Value <= 0 || input.Value > 9_000_000_000_000 ||
		(input.DiscountType == "percentage" && input.Value > 9999) || input.MinTotal < 0 || input.MinTotal > 9_000_000_000_000 ||
		input.MinQuantity < 1 || input.MinQuantity > 100 || input.UsageLimit < 0 || input.UsageLimit > 1_000_000_000 ||
		(input.Scope != "all" && input.Scope != "selected") || len(input.ProductIDs) > 1000 ||
		input.StartsAt < 0 || input.EndsAt < 0 || (input.EndsAt != 0 && input.EndsAt <= input.StartsAt) {
		return bad("优惠设置无效，请检查名称、优惠值、门槛和时间")
	}
	if input.Currency == "" {
		if input.DiscountType == "fixed" || input.MinTotal != 0 {
			return bad("固定减免或金额门槛必须指定币种")
		}
	} else if _, err := currencyScale(input.Currency); err != nil {
		return bad(err.Error())
	}
	if input.Scope == "selected" && len(input.ProductIDs) == 0 {
		return bad("请选择至少一个适用商品")
	}
	if input.Scope == "all" {
		input.ProductIDs = nil
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	if id == "" {
		id = newID()
	} else {
		var exists int
		if err = tx.QueryRowContext(r.Context(), "SELECT 1 FROM promotions WHERE id=? AND deleted_at IS NULL", id).Scan(&exists); err == sql.ErrNoRows {
			return &apiError{404, "优惠不存在"}
		} else if err != nil {
			return err
		}
	}
	if input.Kind == "code" {
		var duplicate int
		if err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM promotions WHERE code=? AND id<>? AND deleted_at IS NULL", input.Code, id).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate > 0 {
			return bad("优惠码已存在")
		}
	}
	for _, productID := range input.ProductIDs {
		var exists int
		if err = tx.QueryRowContext(r.Context(), "SELECT 1 FROM products WHERE id=? AND deleted_at IS NULL", productID).Scan(&exists); err == sql.ErrNoRows {
			return bad("适用商品不存在或已删除")
		} else if err != nil {
			return err
		}
	}
	now := unixNow()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO promotions(id,name,kind,code,discount_type,value,currency,min_total,min_quantity,scope,stackable,enabled,starts_at,ends_at,usage_limit,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,kind=excluded.kind,code=excluded.code,discount_type=excluded.discount_type,value=excluded.value,currency=excluded.currency,min_total=excluded.min_total,min_quantity=excluded.min_quantity,scope=excluded.scope,stackable=excluded.stackable,enabled=excluded.enabled,starts_at=excluded.starts_at,ends_at=excluded.ends_at,usage_limit=excluded.usage_limit,updated_at=excluded.updated_at`,
		id, input.Name, input.Kind, input.Code, input.DiscountType, input.Value, input.Currency, input.MinTotal, input.MinQuantity, input.Scope, input.Stackable, input.Enabled, input.StartsAt, input.EndsAt, input.UsageLimit, now, now)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM promotion_products WHERE promotion_id=?", id); err != nil {
		return err
	}
	for _, productID := range input.ProductIDs {
		if _, err = tx.ExecContext(r.Context(), "INSERT OR IGNORE INTO promotion_products(promotion_id,product_id) VALUES(?,?)", id, productID); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	audit("promotion_saved", id)
	reply(w, 200, map[string]string{"id": id})
	return nil
}

func (a *App) deletePromotion(w http.ResponseWriter, r *http.Request) error {
	result, err := a.db.ExecContext(r.Context(), "UPDATE promotions SET enabled=0,deleted_at=COALESCE(deleted_at,?),updated_at=? WHERE id=?", unixNow(), unixNow(), r.PathValue("id"))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return &apiError{404, "优惠不存在"}
	}
	audit("promotion_deleted", r.PathValue("id"))
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}

// 减免总额向下取整，且始终保留至少一个最小货币单位，避免绕过 HashPay 产生零元订单。
func promotionAmount(p Promotion, base int64) int64 {
	amount := p.Value
	if p.DiscountType == "percentage" {
		amount = base * p.Value / 10000
	}
	if amount >= base {
		amount = base - 1
	}
	return amount
}

func quoteOrder(ctx context.Context, db promotionQuerier, p Product, quantity int, code string, now int64) (OrderQuote, error) {
	quote := OrderQuote{Currency: p.Currency, Discounts: []OrderDiscount{}}
	if quantity < 1 || quantity > 100 || p.Price > 9_000_000_000_000/int64(quantity) {
		return quote, bad("数量或总金额超限")
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if code != "" && !couponPattern.MatchString(code) {
		return quote, bad("优惠码不可用、未生效或不适用于当前订单")
	}
	quote.OriginalTotal = p.Price * int64(quantity)
	quote.Total = quote.OriginalTotal
	rows, err := db.QueryContext(ctx, promotionSelect+` WHERE p.deleted_at IS NULL AND p.enabled=1
 AND (p.kind='automatic' OR (p.kind='code' AND p.code=?))
 AND (p.currency='' OR p.currency=?) AND p.min_total<=? AND p.min_quantity<=?
 AND p.starts_at<=? AND (p.ends_at=0 OR p.ends_at>?)
 AND (p.scope='all' OR EXISTS(SELECT 1 FROM promotion_products pp WHERE pp.promotion_id=p.id AND pp.product_id=?))
 ORDER BY p.id`, code, p.Currency, quote.OriginalTotal, quantity, now, now, p.ID)
	if err != nil {
		return quote, err
	}
	activities := []Promotion{}
	var coupon *Promotion
	for rows.Next() {
		rule, err := scanPromotion(rows)
		if err != nil {
			rows.Close()
			return quote, err
		}
		if rule.UsageLimit > 0 && rule.Used >= rule.UsageLimit {
			continue
		}
		if rule.Kind == "code" {
			coupon = &rule
		} else {
			activities = append(activities, rule)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return quote, err
	}
	if code != "" && coupon == nil {
		return quote, bad("优惠码不可用、未生效或不适用于当前订单")
	}
	// 每单最多一个活动和一个优惠码，双方都允许才叠加，先活动后优惠码。
	// 比较所有可用组合，不能先选最大活动而错过更划算的可叠加组合。
	consider := func(rules ...Promotion) {
		total := quote.OriginalTotal
		discounts := []OrderDiscount{}
		for _, rule := range rules {
			amount := promotionAmount(rule, total)
			if amount > 0 {
				total -= amount
				discounts = append(discounts, OrderDiscount{rule.ID, rule.Name, rule.Code, amount})
			}
		}
		if total < quote.Total {
			quote.Total, quote.Discounts = total, discounts
		}
	}
	if coupon != nil {
		consider(*coupon)
	}
	for _, activity := range activities {
		consider(activity)
		if coupon != nil && activity.Stackable && coupon.Stackable {
			consider(activity, *coupon)
		}
	}
	quote.DiscountTotal = quote.OriginalTotal - quote.Total
	return quote, nil
}

func (a *App) quoteOrder(w http.ResponseWriter, r *http.Request) error {
	if !a.limits.allow("quote:"+clientIP(r), 90, time.Minute) {
		return &apiError{429, "优惠查询过于频繁，请稍后再试"}
	}
	var input orderInput
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	// 同一事务保证商品价格、规则和占用次数来自同一个快照。
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := scanProduct(tx.QueryRowContext(r.Context(), productSelect+" WHERE p.id=? AND p.active=1 AND p.deleted_at IS NULL", input.ProductID))
	if err == sql.ErrNoRows {
		return &apiError{404, "商品不存在或已下架"}
	}
	if err != nil {
		return err
	}
	quote, err := quoteOrder(r.Context(), tx, p, input.Quantity, input.CouponCode, unixNow())
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	reply(w, 200, quote)
	return nil
}
