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
	Enabled     bool                       `json:"enabled"`
	Title       string                     `json:"title"`
	Description string                     `json:"description"`
	Currencies  []DonationCurrencySettings `json:"currencies"`
}

type DonationCurrencySettings struct {
	Currency  string  `json:"currency"`
	Amounts   []int64 `json:"amounts"`
	MinAmount int64   `json:"min_amount"`
	MaxAmount int64   `json:"max_amount"`
}

type Donation struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Nickname          string `json:"nickname"`
	ShowPublic        bool   `json:"show_public"`
	RequestID         string `json:"-"`
	Amount            int64  `json:"amount"`
	Currency          string `json:"currency"`
	GatewayID         string `json:"gateway_id"`
	CheckoutURL       string `json:"checkout_url"`
	PaymentStatus     string `json:"payment_status"`
	ExpiresAt         int64  `json:"expires_at"`
	PaidAt            int64  `json:"paid_at"`
	CreatedAt         int64  `json:"created_at"`
	LastError         string `json:"last_error"`
	GatewayURL        string `json:"-"`
	GatewayMerchantID string `json:"-"`
}

const donationSelect = `SELECT id,title,amount,currency,COALESCE(gateway_id,''),checkout_url,payment_status,expires_at,COALESCE(paid_at,0),created_at,last_error,gateway_url,gateway_merchant_id,nickname,show_public,request_id FROM donations`

func scanDonation(row scanner) (Donation, error) {
	var d Donation
	err := row.Scan(&d.ID, &d.Title, &d.Amount, &d.Currency, &d.GatewayID, &d.CheckoutURL, &d.PaymentStatus, &d.ExpiresAt, &d.PaidAt, &d.CreatedAt, &d.LastError, &d.GatewayURL, &d.GatewayMerchantID, &d.Nickname, &d.ShowPublic, &d.RequestID)
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
	if err := json.Unmarshal([]byte(document), &settings); err != nil {
		return settings, err
	}
	if len(settings.Currencies) == 0 {
		return settings, errors.New("赞赏设置缺少币种配置")
	}
	return settings, nil
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
	if len(s.Currencies) < 1 || len(s.Currencies) > 8 {
		return bad("请设置 1–8 个不重复的赞赏币种")
	}
	seenCurrencies := map[string]bool{}
	for i := range s.Currencies {
		group := &s.Currencies[i]
		if _, err := currencyScale(group.Currency); err != nil {
			return bad(err.Error())
		}
		if seenCurrencies[group.Currency] {
			return bad("赞赏币种不能重复")
		}
		seenCurrencies[group.Currency] = true
		if group.MinAmount <= 0 || group.MaxAmount < group.MinAmount || group.MaxAmount > 9_000_000_000_000 || len(group.Amounts) > 8 {
			return bad("赞赏金额范围无效，预设金额最多 8 个")
		}
		seen := map[int64]bool{}
		for _, amount := range group.Amounts {
			if amount < group.MinAmount || amount > group.MaxAmount || seen[amount] {
				return bad("预设赞赏金额须在范围内且不能重复")
			}
			seen[amount] = true
		}
		if group.Amounts == nil {
			group.Amounts = []int64{}
		}
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
	var input struct {
		RequestID     string `json:"request_id"`
		Nickname      string `json:"nickname"`
		ShowPublic    bool   `json:"show_public"`
		Amount        int64  `json:"amount"`
		Currency      string `json:"currency"`
		ConfirmRepeat bool   `json:"confirm_repeat"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	input.Nickname = strings.TrimSpace(input.Nickname)
	if !validRequestID(input.RequestID) {
		return bad("赞赏请求标识无效，请重新打开赞赏页面")
	}
	if err := validateDonationNickname(input.Nickname); err != nil {
		return err
	}
	// 与网关配置切换串行，保存原商户归属后才允许调度创建远端订单。
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	// 优先恢复已持久化的请求；停用页面、变更金额或网关不能让未知结果变成第二笔。
	existing, err := scanDonation(a.db.QueryRowContext(r.Context(), donationSelect+" WHERE request_id=?", input.RequestID))
	if err == nil {
		if existing.Nickname != input.Nickname || existing.Amount != input.Amount || existing.Currency != input.Currency || existing.ShowPublic != input.ShowPublic {
			return &apiError{409, "赞赏请求内容已变化，请使用新的请求标识"}
		}
		reply(w, 201, map[string]any{"donation": donationResponse(existing)})
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	// 恢复同一请求不消耗新建额度，避免连续丢包后无法取回已存在的记录。
	if !a.limits.allow("donation-create:"+clientIP(r), 10, time.Minute) {
		return &apiError{429, "下单过于频繁，请稍后再试"}
	}
	var used bool
	if err := a.db.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM donations WHERE nickname=?)", input.Nickname).Scan(&used); err != nil {
		return err
	}
	if used && !input.ConfirmRepeat {
		suggestions, err := a.suggestDonationNicknames(r.Context(), input.Nickname)
		if err != nil {
			return err
		}
		reply(w, 409, map[string]any{"error": "昵称已有赞赏记录，请确认是否为再次赞赏", "code": "donation_repeat_confirmation_required", "suggestions": suggestions})
		return nil
	}
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
	var group *DonationCurrencySettings
	for i := range s.Currencies {
		if s.Currencies[i].Currency == input.Currency {
			group = &s.Currencies[i]
			break
		}
	}
	if group == nil {
		return &apiError{409, "赞赏设置已变化，请刷新后重新选择金额"}
	}
	if input.Amount < group.MinAmount || input.Amount > group.MaxAmount {
		return bad("赞赏金额不在允许范围内")
	}
	d := Donation{ID: "don_" + newID(), Title: s.Title, Nickname: input.Nickname, ShowPublic: input.ShowPublic, RequestID: input.RequestID, Amount: input.Amount, Currency: input.Currency, GatewayURL: strings.TrimRight(c.HashPay.BaseURL, "/"), GatewayMerchantID: c.HashPay.MerchantID, PaymentStatus: "creating", CreatedAt: unixNow()}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO donations(id,title,nickname,show_public,request_id,amount,currency,gateway_url,gateway_merchant_id,payment_status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, d.ID, d.Title, d.Nickname, d.ShowPublic, d.RequestID, d.Amount, d.Currency, d.GatewayURL, d.GatewayMerchantID, d.PaymentStatus, d.CreatedAt, d.CreatedAt); err != nil {
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
	reply(w, 201, map[string]any{"donation": donationResponse(d)})
	return nil
}

func (a *App) donationDetail(w http.ResponseWriter, r *http.Request) error {
	token, valid := claimKeyFromAuthorization(r)
	if !valid {
		return &apiError{401, "请输入有效的查询凭证"}
	}
	d, err := a.loadDonation(r.Context(), r.PathValue("id"))
	if err == sql.ErrNoRows || (err == nil && !constantEqual(tokenHash(d.Nickname), tokenHash(token))) {
		return &apiError{404, "赞赏记录或凭证无效"}
	}
	if err != nil {
		return err
	}
	reply(w, 200, donationResponse(d))
	return nil
}

func validateDonationNickname(nickname string) error {
	if nickname == "" || !utf8.ValidString(nickname) || utf8.RuneCountInString(nickname) > 128 {
		return bad("赞赏昵称须为 1–128 个字符，不能全部为空白")
	}
	return nil
}

func (a *App) suggestDonationNicknames(ctx context.Context, nickname string) ([]string, error) {
	prefix := []rune(nickname)
	if len(prefix) > 119 {
		prefix = prefix[:119]
	}
	suggestions := []string{}
	seen := map[string]bool{}
	for tries := 0; len(suggestions) < 3 && tries < 12; tries++ {
		candidate := string(prefix) + "-" + randomToken()[:8]
		var used bool
		if err := a.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM donations WHERE nickname=?)", candidate).Scan(&used); err != nil {
			return nil, err
		}
		if !used && !seen[candidate] {
			seen[candidate] = true
			suggestions = append(suggestions, candidate)
		}
	}
	return suggestions, nil
}

// 昵称查询可被其他人使用，显式列出公开字段，避免暴露网关内部标识和诊断信息。
func donationResponse(d Donation) map[string]any {
	checkout := ""
	if d.PaymentStatus == "pending" {
		checkout = d.CheckoutURL
	}
	return map[string]any{"id": d.ID, "title": d.Title, "nickname": d.Nickname, "show_public": d.ShowPublic, "amount": d.Amount, "currency": d.Currency, "checkout_url": checkout, "payment_status": d.PaymentStatus, "expires_at": d.ExpiresAt, "paid_at": d.PaidAt, "created_at": d.CreatedAt}
}

func (a *App) lookupDonation(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Nickname  string `json:"nickname"`
		Page      int    `json:"page"`
		RequestID string `json:"request_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	input.Nickname = strings.TrimSpace(input.Nickname)
	if err := validateDonationNickname(input.Nickname); err != nil {
		return err
	}
	if input.Page < 0 || input.Page > 100000 {
		return bad("页码超限")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	rows, err := a.db.QueryContext(r.Context(), donationSelect+" WHERE nickname=? ORDER BY created_at DESC,id DESC LIMIT 21 OFFSET ?", input.Nickname, (input.Page-1)*20)
	if err != nil {
		return err
	}
	items := []map[string]any{}
	for rows.Next() {
		d, err := scanDonation(rows)
		if err != nil {
			rows.Close()
			return err
		}
		items = append(items, donationResponse(d))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(items) == 0 && input.Page == 1 {
		return &apiError{404, "赞赏昵称不存在"}
	}
	more := len(items) > 20
	if more {
		items = items[:20]
	}
	matched := ""
	if input.RequestID != "" {
		var exists bool
		if err := a.db.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM donations WHERE nickname=? AND request_id=?)", input.Nickname, input.RequestID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			matched = input.RequestID
		}
	}
	reply(w, 200, map[string]any{"items": items, "page": input.Page, "has_more": more, "matched_request_id": matched})
	return nil
}

func (a *App) publicDonations(w http.ResponseWriter, r *http.Request) error {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		return bad("页码超限")
	}
	rows, err := a.db.QueryContext(r.Context(), "SELECT nickname,amount,currency,paid_at FROM donations WHERE show_public=1 AND payment_status='paid' ORDER BY paid_at DESC,id DESC LIMIT 21 OFFSET ?", (page-1)*20)
	if err != nil {
		return err
	}
	defer rows.Close()
	type publicDonation struct {
		Nickname string `json:"nickname"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
		PaidAt   int64  `json:"paid_at"`
	}
	items := []publicDonation{}
	for rows.Next() {
		var item publicDonation
		if err := rows.Scan(&item.Nickname, &item.Amount, &item.Currency, &item.PaidAt); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	more := len(items) > 20
	if more {
		items = items[:20]
	}
	reply(w, 200, map[string]any{"items": items, "page": page, "has_more": more})
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
		remote, checkout, err := client.createPayment(ctx, paymentRequest{d.ID, d.Title, d.Currency, d.Amount}, strings.TrimRight(c.PublicURL, "/")+"/center/donations/"+d.ID)
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
	where := " WHERE deleted_at IS NULL"
	args := []any{}
	if nickname := strings.TrimSpace(r.URL.Query().Get("q")); nickname != "" {
		where += " AND instr(lower(nickname),lower(?))>0"
		args = append(args, nickname)
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	grouped := r.URL.Query().Get("group_by") == "nickname"
	query := donationSelect + where + " ORDER BY created_at DESC,id DESC LIMIT 21 OFFSET ?"
	const groupKey = "donations.nickname"
	if grouped {
		// 先按昵称分页，再取完整历史，避免同一个人被拆到不同页。
		query = `WITH supporter_page AS (
SELECT ` + groupKey + ` AS supporter,MAX(created_at) AS latest FROM donations` + where + `
GROUP BY supporter ORDER BY latest DESC,supporter DESC LIMIT 21 OFFSET ?
) ` + donationSelect + ` WHERE deleted_at IS NULL AND ` + groupKey + ` IN (SELECT supporter FROM supporter_page)
ORDER BY (SELECT latest FROM supporter_page WHERE supporter=` + groupKey + `) DESC,` + groupKey + ` DESC,created_at DESC,id DESC`
	}
	rows, err := tx.QueryContext(r.Context(), query, append(args, (page-1)*20)...)
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
	rows, err = tx.QueryContext(r.Context(), "SELECT currency,COUNT(*),SUM(amount) FROM donations WHERE deleted_at IS NULL AND payment_status='paid' GROUP BY currency ORDER BY currency")
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
	more := false
	if grouped {
		seen := map[string]bool{}
		for i, item := range items {
			seen[item.Nickname] = true
			if len(seen) > 20 {
				more = true
				items = items[:i]
				break
			}
		}
	} else if len(items) > 20 {
		more = true
		items = items[:20]
	}
	reply(w, 200, map[string]any{"items": items, "page": page, "has_more": more, "totals": totals})
	return nil
}

func (a *App) deleteDonation(w http.ResponseWriter, r *http.Request) error {
	// 后台逻辑删除，保留昵称查询、请求幂等及网关回调映射。
	id, now := r.PathValue("id"), unixNow()
	result, err := a.db.ExecContext(r.Context(), "UPDATE donations SET deleted_at=COALESCE(deleted_at,?),updated_at=? WHERE id=?", now, now, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return &apiError{404, "赞赏记录或凭证无效"}
	}
	audit("donation_deleted", id)
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) recheckDonation(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	var existing string
	if err := a.db.QueryRowContext(r.Context(), "SELECT id FROM donations WHERE id=? AND deleted_at IS NULL", id).Scan(&existing); err == sql.ErrNoRows {
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
