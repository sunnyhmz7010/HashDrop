package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const sessionCookie = "hashdrop_session"

func audit(action, id string) { slog.Info("管理与交易事件", "action", action, "object_id", id) }
func (a *App) csrf(token string) string {
	h := hmac.New(sha256.New, a.vault.dedup)
	h.Write([]byte("csrf:" + token))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (a *App) sameOrigin(r *http.Request) bool {
	return strings.TrimRight(r.Header.Get("Origin"), "/") == strings.TrimRight(a.settings().PublicURL, "/")
}
func (a *App) admin(next endpoint) endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || len(cookie.Value) != 43 {
			return &apiError{401, "请先登录"}
		}
		var expires int64
		if err = a.db.QueryRowContext(r.Context(), "SELECT expires_at FROM admin_sessions WHERE token_hash=?", tokenHash(cookie.Value)).Scan(&expires); err == sql.ErrNoRows {
			return &apiError{401, "会话已失效，请重新登录"}
		} else if err != nil {
			return err
		}
		if expires <= unixNow() {
			return &apiError{401, "会话已过期，请重新登录"}
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if !a.sameOrigin(r) || !constantEqual(a.csrf(cookie.Value), r.Header.Get("X-CSRF-Token")) {
				return &apiError{403, "安全校验失败，请刷新页面"}
			}
		}
		return next(w, r)
	}
}
func (a *App) login(w http.ResponseWriter, r *http.Request) error {
	if !a.sameOrigin(r) {
		return &apiError{403, "请求来源无效"}
	}
	if !a.limits.allow("login:"+clientIP(r), 5, 15*time.Minute) || !a.limits.allow("login:global", 30, 15*time.Minute) {
		return &apiError{429, "登录尝试过多，请 15 分钟后再试"}
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	c := a.settings()
	// 限制 Argon2 的并行内存占用，适合单台低配 VPS。
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if input.Password == "" || utf8.RuneCountInString(input.Password) > 256 || !passwordMatches(c.AdminPassword, input.Password) || !constantEqual(c.AdminUsername, strings.TrimSpace(input.Username)) {
		audit("login_rejected", "")
		return &apiError{401, "账号或密码不正确"}
	}
	token := randomToken()
	now := unixNow()
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM admin_sessions WHERE expires_at<=?", now); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO admin_sessions(token_hash,expires_at,created_at) VALUES(?,?,?)", tokenHash(token), now+12*3600, now); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: secureCookie(c), SameSite: http.SameSiteStrictMode, MaxAge: 12 * 3600})
	audit("admin_login", "")
	reply(w, 200, map[string]string{"csrf": a.csrf(token)})
	return nil
}
func (a *App) session(w http.ResponseWriter, r *http.Request) error {
	cookie, _ := r.Cookie(sessionCookie)
	reply(w, 200, map[string]string{"csrf": a.csrf(cookie.Value)})
	return nil
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) error {
	cookie, _ := r.Cookie(sessionCookie)
	if _, err := a.db.Exec("DELETE FROM admin_sessions WHERE token_hash=?", tokenHash(cookie.Value)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, Secure: secureCookie(a.settings()), SameSite: http.SameSiteStrictMode, MaxAge: -1})
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}
func (a *App) overview(w http.ResponseWriter, r *http.Request) error {
	var products, stock, orders, paid, attention int
	var err error
	for _, q := range []struct {
		query  string
		target *int
	}{{"SELECT COUNT(*) FROM products WHERE deleted_at IS NULL", &products}, {"SELECT COUNT(*) FROM stock_items WHERE deleted_at IS NULL AND status='available' AND inventory_id IN (SELECT id FROM inventories WHERE deleted_at IS NULL)", &stock}, {"SELECT COUNT(*) FROM orders WHERE deleted_at IS NULL", &orders}, {"SELECT COUNT(*) FROM orders WHERE deleted_at IS NULL AND payment_status='paid'", &paid}, {"SELECT COUNT(*) FROM orders o WHERE o.deleted_at IS NULL AND (o.payment_status='review' OR EXISTS(SELECT 1 FROM deliveries d WHERE d.order_id=o.id AND d.status='failed'))", &attention}} {
		if err = a.db.QueryRowContext(r.Context(), q.query).Scan(q.target); err != nil {
			return err
		}
	}
	reply(w, 200, map[string]int{"products": products, "stock": stock, "orders": orders, "paid": paid, "delivery_attention": attention})
	return nil
}

var zeroFilterAmount = regexp.MustCompile("^0+(\\.0+)?$")

func (a *App) adminOrders(w http.ResponseWriter, r *http.Request) error {
	values := r.URL.Query()
	page, _ := strconv.Atoi(values.Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		return bad("页码超限")
	}
	where := " WHERE deleted_at IS NULL"
	args := []any{}
	if delivery := values.Get("delivery_status"); delivery != "" {
		switch delivery {
		case "pending", "partial", "done", "failed", "cancelled":
		default:
			return bad("交付状态无效")
		}
		where += " AND delivery_status=?"
		args = append(args, delivery)
	}
	if query := strings.TrimSpace(values.Get("q")); query != "" {
		if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 120 {
			return bad("订单搜索内容过长")
		}
		where += " AND (id=? OR gateway_id=? OR instr(lower(product_name),lower(?))>0)"
		args = append(args, query, query, query)
	}
	if status := values.Get("status"); status != "" {
		switch status {
		case "creating", "pending", "paid", "expired", "invalid", "review", "closed":
		default:
			return bad("状态无效")
		}
		where += " AND payment_status=?"
		args = append(args, status)
	}
	currency := values.Get("currency")
	if currency != "" {
		if _, err := currencyScale(currency); err != nil {
			return bad(err.Error())
		}
		where += " AND currency=?"
		args = append(args, currency)
	}
	bounds := map[string]int64{}
	for _, field := range []string{"created_from", "created_to", "min_amount", "max_amount"} {
		value := strings.TrimSpace(values.Get(field))
		if value == "" {
			continue
		}
		var amount int64
		var err error
		column, operator := "created_at", ">="
		if field == "created_to" || field == "max_amount" {
			operator = "<="
		}
		if field == "min_amount" || field == "max_amount" {
			if currency == "" {
				return bad("金额筛选须选择币种")
			}
			column = "total"
			if !zeroFilterAmount.MatchString(value) {
				amount, err = decimalMinor(value, currency)
			}
		} else {
			amount, err = strconv.ParseInt(value, 10, 64)
		}
		if err != nil || amount < 0 {
			return bad("筛选区间格式或精度无效")
		}
		bounds[field] = amount
		where += " AND " + column + operator + "?"
		args = append(args, amount)
	}
	for _, pair := range [][2]string{{"created_from", "created_to"}, {"min_amount", "max_amount"}} {
		low, hasLow := bounds[pair[0]]
		high, hasHigh := bounds[pair[1]]
		if hasLow && hasHigh && low > high {
			return bad("筛选区间起点不能大于终点")
		}
	}
	// 总数和当前页使用同一数据库快照，避免订单变化导致页码与列表不一致。
	tx, err := a.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM orders"+where, args...).Scan(&total); err != nil {
		return err
	}
	lastPage := (total + 19) / 20
	if lastPage < 1 {
		lastPage = 1
	}
	if page > lastPage {
		page = lastPage
	}
	rows, err := tx.QueryContext(r.Context(), orderSelect+where+" ORDER BY created_at DESC,id DESC LIMIT 20 OFFSET ?", append(args, (page-1)*20)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Order{}
	for rows.Next() {
		order, scanErr := scanOrder(rows)
		if scanErr != nil {
			return scanErr
		}
		items = append(items, order)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	reply(w, 200, map[string]any{"items": items, "page": page, "total": total, "page_size": 20, "has_more": page*20 < total})
	return nil
}
func (a *App) adminOrder(w http.ResponseWriter, r *http.Request) error {
	o, err := scanOrder(a.db.QueryRowContext(r.Context(), orderSelect+" WHERE id=? AND deleted_at IS NULL", r.PathValue("id")))
	if err == sql.ErrNoRows {
		return &apiError{404, "订单不存在"}
	}
	if err != nil {
		return err
	}
	deliveries, err := a.deliveryResults(r.Context(), o.ID)
	if err != nil {
		return err
	}
	reply(w, 200, map[string]any{"order": o, "deliveries": deliveries})
	return nil
}
func (a *App) recheckOrder(w http.ResponseWriter, r *http.Request) error {
	var id string
	if err := a.db.QueryRowContext(r.Context(), "SELECT id FROM orders WHERE id=? AND deleted_at IS NULL", r.PathValue("id")).Scan(&id); err == sql.ErrNoRows {
		return &apiError{404, "订单不存在"}
	} else if err != nil {
		return err
	}
	if err := a.syncOrder(r.Context(), r.PathValue("id")); err != nil {
		return &apiError{502, safeGatewayError(err)}
	}
	audit("order_rechecked", r.PathValue("id"))
	return a.adminOrder(w, r)
}
func (a *App) retryOrder(w http.ResponseWriter, r *http.Request) error {
	var input struct{}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	o, err := scanOrder(a.db.QueryRowContext(r.Context(), orderSelect+" WHERE id=? AND deleted_at IS NULL", r.PathValue("id")))
	if err == sql.ErrNoRows {
		return &apiError{404, "订单不存在"}
	}
	if err != nil {
		return err
	}
	if o.PaymentStatus != "paid" || o.ReservationStatus == "released" {
		return bad("订单尚未核实付款或库存已释放")
	}
	if _, err = a.db.ExecContext(r.Context(), "UPDATE deliveries SET status='pending',last_error='' WHERE order_id=? AND status='failed'", o.ID); err != nil {
		return err
	}
	if err = a.deliverOrder(r.Context(), o); err != nil {
		return err
	}
	audit("delivery_retried", o.ID)
	return a.adminOrder(w, r)
}
func (a *App) deleteOrder(w http.ResponseWriter, r *http.Request) error {
	// 后台逻辑删除，不取消网关订单、不释放库存、不破坏领取与回调映射。
	result, err := a.db.ExecContext(r.Context(), "UPDATE orders SET deleted_at=COALESCE(deleted_at,?),updated_at=? WHERE id=?", unixNow(), unixNow(), r.PathValue("id"))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return &apiError{404, "订单不存在"}
	}
	audit("order_deleted", r.PathValue("id"))
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}

type SettingsView struct {
	Title         string `json:"title"`
	Notice        string `json:"notice"`
	PublicURL     string `json:"public_url"`
	HashPayURL    string `json:"hashpay_url"`
	MerchantID    string `json:"merchant_id"`
	KeyConfigured bool   `json:"key_configured"`
}

func (a *App) getSettings(w http.ResponseWriter, r *http.Request) error {
	c := a.settings()
	reply(w, 200, SettingsView{c.Title, c.Notice, c.PublicURL, c.HashPay.BaseURL, c.HashPay.MerchantID, c.HashPay.PrivateKeyFile != ""})
	return nil
}
func (a *App) updateSettings(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Title      string `json:"title"`
		Notice     string `json:"notice"`
		HashPayURL string `json:"hashpay_url"`
		MerchantID string `json:"merchant_id"`
		PrivateKey string `json:"private_key"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	input.HashPayURL = strings.TrimRight(strings.TrimSpace(input.HashPayURL), "/")
	input.MerchantID = strings.TrimSpace(input.MerchantID)
	// 补查和配置切换共用锁；已完成交付或可靠终止的历史订单不再依赖原商户。
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	a.configMu.Lock()
	defer a.configMu.Unlock()
	c := a.config
	if input.HashPayURL != strings.TrimRight(c.HashPay.BaseURL, "/") || input.MerchantID != c.HashPay.MerchantID {
		var count int
		// 逻辑删除仍保留补查与交付责任，不能仅因后台列表隐藏就放行。
		if err := a.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM orders WHERE payment_status NOT IN ('closed','invalid') AND NOT (payment_status='paid' AND delivery_status='done')").Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return bad("存在待核验付款或未完成交付的订单，暂不能更换支付网关或商户")
		}
		if err := a.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM donations WHERE payment_status NOT IN ('paid','closed','invalid')").Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return bad("存在待核验付款的赞赏，暂不能更换支付网关或商户")
		}
	}
	c.Title = strings.TrimSpace(input.Title)
	c.Notice = input.Notice
	c.HashPay.BaseURL = input.HashPayURL
	c.HashPay.MerchantID = input.MerchantID
	if c.HashPay.BaseURL != "" || c.HashPay.MerchantID != "" || input.PrivateKey != "" {
		if c.HashPay.BaseURL == "" || c.HashPay.MerchantID == "" || (c.HashPay.PrivateKeyFile == "" && input.PrivateKey == "") {
			return bad("HashPay 地址、商户 ID 和私钥须完整填写")
		}
	}
	if err := validateConfig(c); err != nil {
		return bad(err.Error())
	}
	if input.PrivateKey != "" {
		path, err := savePrivateKey(a.configPath, input.PrivateKey)
		if err != nil {
			return err
		}
		c.HashPay.PrivateKeyFile = path
	}
	if err := writeConfig(a.configPath, c); err != nil {
		return err
	}
	a.config = c
	audit("settings_saved", "")
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}
