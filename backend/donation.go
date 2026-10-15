package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type DonationSettings struct {
	Enabled     bool    `json:"enabled"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Currency    string  `json:"currency"`
	Amounts     []int64 `json:"amounts"`
	MinAmount   int64   `json:"min_amount"`
	MaxAmount   int64   `json:"max_amount"`
}

type Donation struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Amount            int64  `json:"amount"`
	Currency          string `json:"currency"`
	GatewayID         string `json:"gateway_id"`
	CheckoutURL       string `json:"checkout_url"`
	PaymentStatus     string `json:"payment_status"`
	ExpiresAt         int64  `json:"expires_at"`
	PaidAt            int64  `json:"paid_at"`
	CreatedAt         int64  `json:"created_at"`
	LastError         string `json:"last_error"`
	TokenHash         string `json:"-"`
	GatewayURL        string `json:"-"`
	GatewayMerchantID string `json:"-"`
}

const donationSelect = `SELECT id,title,amount,currency,COALESCE(gateway_id,''),checkout_url,payment_status,expires_at,COALESCE(paid_at,0),created_at,last_error,token_hash,gateway_url,gateway_merchant_id FROM donations`

func scanDonation(row scanner) (Donation, error) {
	var d Donation
	err := row.Scan(&d.ID, &d.Title, &d.Amount, &d.Currency, &d.GatewayID, &d.CheckoutURL, &d.PaymentStatus, &d.ExpiresAt, &d.PaidAt, &d.CreatedAt, &d.LastError, &d.TokenHash, &d.GatewayURL, &d.GatewayMerchantID)
	return d, err
}

func (a *App) loadDonation(ctx context.Context, id string) (Donation, error) {
	return scanDonation(a.db.QueryRowContext(ctx, donationSelect+" WHERE id=?", id))
}

func readDonationSettings(row scanner) (DonationSettings, error) {
	var settings DonationSettings
	var document string
	if err := row.Scan(&document); err != nil {
		return settings, err
	}
	err := json.Unmarshal([]byte(document), &settings)
	return settings, err
}

func (a *App) getDonationSettings(w http.ResponseWriter, r *http.Request) error {
	s, err := readDonationSettings(a.db.QueryRowContext(r.Context(), "SELECT document FROM donation_settings WHERE id=1"))
	if err != nil {
		return err
	}
	reply(w, 200, s)
	return nil
}

func (a *App) publicDonationSettings(w http.ResponseWriter, r *http.Request) error {
	s, err := readDonationSettings(a.db.QueryRowContext(r.Context(), "SELECT document FROM donation_settings WHERE id=1"))
	if err != nil {
		return err
	}
	if !s.Enabled {
		reply(w, 200, map[string]bool{"enabled": false})
	} else {
		reply(w, 200, s)
	}
	return nil
}

func (a *App) updateDonationSettings(w http.ResponseWriter, r *http.Request) error {
	var s DonationSettings
	if err := readJSON(w, r, &s); err != nil {
		return err
	}
	s.Title = strings.TrimSpace(s.Title)
	if s.Title == "" || utf8.RuneCountInString(s.Title) > 120 || utf8.RuneCountInString(s.Description) > 65536 {
		return bad("赞赏标题不能为空或超过 120 字符，说明不能超过 65536 字符")
	}
	if _, err := currencyScale(s.Currency); err != nil {
		return bad(err.Error())
	}
	if s.MinAmount <= 0 || s.MaxAmount < s.MinAmount || s.MaxAmount > 9_000_000_000_000 || len(s.Amounts) > 8 {
		return bad("赞赏金额范围无效，预设金额最多 8 个")
	}
	seen := map[int64]bool{}
	for _, amount := range s.Amounts {
		if amount < s.MinAmount || amount > s.MaxAmount || seen[amount] {
			return bad("预设赞赏金额须在范围内且不能重复")
		}
		seen[amount] = true
	}
	if s.Amounts == nil {
		s.Amounts = []int64{}
	}
	if s.Enabled {
		if _, err := newHashPay(a.settings().HashPay); err != nil {
			return &apiError{503, "支付服务尚未配置，请联系店主"}
		}
	}
	document, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if _, err = a.db.ExecContext(r.Context(), "UPDATE donation_settings SET document=? WHERE id=1", string(document)); err != nil {
		return err
	}
	audit("donation_settings_saved", "")
	reply(w, 200, s)
	return nil
}

func (a *App) createDonation(w http.ResponseWriter, r *http.Request) error {
	if !a.limits.allow("donation-create:"+clientIP(r), 10, time.Minute) {
		return &apiError{429, "下单过于频繁，请稍后再试"}
	}
	var input struct {
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	// 与网关配置切换串行，保存原商户归属后才允许调度创建远端订单。
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	c := a.settings()
	if _, err := newHashPay(c.HashPay); err != nil {
		return &apiError{503, "支付服务尚未配置，请联系店主"}
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s, err := readDonationSettings(tx.QueryRowContext(r.Context(), "SELECT document FROM donation_settings WHERE id=1"))
	if err != nil {
		return err
	}
	if !s.Enabled {
		return &apiError{404, "赞赏页尚未开启"}
	}
	if input.Currency != s.Currency {
		return &apiError{409, "赞赏设置已变化，请刷新后重新选择金额"}
	}
	if input.Amount < s.MinAmount || input.Amount > s.MaxAmount {
		return bad("赞赏金额不在允许范围内")
	}
	token := randomToken()
	d := Donation{ID: "don_" + newID(), Title: s.Title, Amount: input.Amount, Currency: s.Currency, TokenHash: tokenHash(token), GatewayURL: strings.TrimRight(c.HashPay.BaseURL, "/"), GatewayMerchantID: c.HashPay.MerchantID, PaymentStatus: "creating", CreatedAt: unixNow()}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO donations(id,title,amount,currency,token_hash,gateway_url,gateway_merchant_id,payment_status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, d.ID, d.Title, d.Amount, d.Currency, d.TokenHash, d.GatewayURL, d.GatewayMerchantID, d.PaymentStatus, d.CreatedAt, d.CreatedAt); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
	audit("donation_created", d.ID)
	reply(w, 201, map[string]any{"donation": d, "token": token})
	return nil
}

func (a *App) donationDetail(w http.ResponseWriter, r *http.Request) error {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(token) != 43 || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return &apiError{401, "请输入有效的查询凭证"}
	}
	d, err := a.loadDonation(r.Context(), r.PathValue("id"))
	if err == sql.ErrNoRows || (err == nil && !constantEqual(d.TokenHash, tokenHash(token))) {
		return &apiError{404, "赞赏记录或凭证无效"}
	}
	if err != nil {
		return err
	}
	// 创建摘要尚不足以核验归属，只有查单通过的待付款记录才开放支付地址。
	if d.PaymentStatus != "pending" {
		d.CheckoutURL = ""
	}
	reply(w, 200, d)
	return nil
}

func (a *App) syncDonation(ctx context.Context, id string) error {
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	err := a.syncDonationLocked(ctx, id)
	if err != nil {
		if _, updateErr := a.db.ExecContext(context.Background(), "UPDATE donations SET attempts=attempts+1,next_check=?,last_error=?,updated_at=? WHERE id=?", unixNow()+120, safeGatewayError(err), unixNow(), id); updateErr != nil {
			return updateErr
		}
	}
	return err
}

func (a *App) syncDonationLocked(ctx context.Context, id string) error {
	d, err := a.loadDonation(ctx, id)
	if err != nil {
		return err
	}
	// 已核验的付款不回退；重复回调也不会重复计入按记录汇总的统计。
	if d.PaymentStatus == "paid" || d.PaymentStatus == "closed" || d.PaymentStatus == "invalid" {
		return nil
	}
	c := a.settings()
	if d.GatewayURL != strings.TrimRight(c.HashPay.BaseURL, "/") || d.GatewayMerchantID != c.HashPay.MerchantID {
		return errors.New("HashPay 配置与赞赏原网关或商户不一致，需核查")
	}
	client, err := newHashPay(c.HashPay)
	if err != nil {
		return err
	}
	if d.GatewayID == "" {
		remote, checkout, err := client.createPayment(ctx, paymentRequest{d.ID, d.Title, d.Currency, d.Amount}, strings.TrimRight(c.PublicURL, "/")+"/donate/orders/"+d.ID)
		if err != nil {
			return err
		}
		expires := remote.ExpiresAt
		if expires == 0 {
			expires = remote.ExpireAt
		}
		if expires <= 0 {
			return errors.New("HashPay 有效期缺失")
		}
		if _, err = a.db.ExecContext(ctx, "UPDATE donations SET gateway_id=?,checkout_url=?,expires_at=?,next_check=?,last_error='',updated_at=? WHERE id=? AND gateway_id IS NULL", remote.ID, checkout, expires, unixNow()+30, unixNow(), id); err != nil {
			return err
		}
		d.GatewayID = remote.ID
	}
	remote, err := client.query(ctx, d.GatewayID)
	if err != nil {
		var failure *hashPayHTTPError
		if errors.As(err, &failure) && failure.Status == http.StatusNotFound && failure.Key == "errors.order_not_found" {
			_, err = a.db.ExecContext(ctx, "UPDATE donations SET payment_status='closed',last_error='',updated_at=? WHERE id=?", unixNow(), id)
			return err
		}
		return err
	}
	if !client.matchesPayment(remote, d.ID, d.GatewayID, d.Currency, d.Amount) {
		if _, err = a.db.ExecContext(ctx, "UPDATE donations SET payment_status='review',updated_at=? WHERE id=?", unixNow(), id); err != nil {
			return err
		}
		return errors.New("HashPay 查询结果不匹配")
	}
	if remote.Status != "paid" && remote.Status != "pending" && remote.Status != "expired" && remote.Status != "invalid" {
		return errors.New("HashPay 订单状态未知")
	}
	next := int64(30)
	if remote.Status == "expired" {
		next = 900 // 沿用网关语义：过期仍可能到账，继续补查。
	}
	_, err = a.db.ExecContext(ctx, "UPDATE donations SET payment_status=?,paid_at=CASE WHEN ?='paid' THEN COALESCE(paid_at,?) ELSE paid_at END,next_check=?,last_error='',updated_at=? WHERE id=?", remote.Status, remote.Status, unixNow(), unixNow()+next, unixNow(), id)
	if err == nil && remote.Status == "paid" {
		audit("donation_paid", id)
	}
	return err
}

func (a *App) runPendingDonations(ctx context.Context) {
	rows, err := a.db.QueryContext(ctx, "SELECT id FROM donations WHERE next_check<=? AND payment_status IN ('creating','pending','expired','review') ORDER BY next_check,created_at LIMIT 20", unixNow())
	if err != nil {
		slog.Error("赞赏补查读取失败", "error", err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			slog.Error("赞赏补查读取失败", "error", err)
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		slog.Error("赞赏补查读取失败", "error", err)
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err := a.syncDonation(ctx, id); err != nil {
			slog.Warn("赞赏补查暂未完成", "donation_id", id, "reason", safeGatewayError(err))
		}
	}
}

type DonationTotal struct {
	Currency string `json:"currency"`
	Count    int64  `json:"count"`
	Amount   int64  `json:"amount"`
}

func (a *App) adminDonations(w http.ResponseWriter, r *http.Request) error {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		return bad("页码超限")
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(r.Context(), donationSelect+" ORDER BY created_at DESC,id DESC LIMIT 21 OFFSET ?", (page-1)*20)
	if err != nil {
		return err
	}
	items := []Donation{}
	for rows.Next() {
		d, err := scanDonation(rows)
		if err != nil {
			rows.Close()
			return err
		}
		items = append(items, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(r.Context(), "SELECT currency,COUNT(*),SUM(amount) FROM donations WHERE payment_status='paid' GROUP BY currency ORDER BY currency")
	if err != nil {
		return err
	}
	totals := []DonationTotal{}
	for rows.Next() {
		var total DonationTotal
		if err := rows.Scan(&total.Currency, &total.Count, &total.Amount); err != nil {
			rows.Close()
			return err
		}
		totals = append(totals, total)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	more := len(items) > 20
	if more {
		items = items[:20]
	}
	reply(w, 200, map[string]any{"items": items, "page": page, "has_more": more, "totals": totals})
	return nil
}

func (a *App) recheckDonation(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, err := a.loadDonation(r.Context(), id); err == sql.ErrNoRows {
		return &apiError{404, "赞赏记录或凭证无效"}
	} else if err != nil {
		return err
	}
	if err := a.syncDonation(r.Context(), id); err != nil {
		return &apiError{502, safeGatewayError(err)}
	}
	d, err := a.loadDonation(r.Context(), id)
	if err != nil {
		return err
	}
	reply(w, 200, d)
	return nil
}
