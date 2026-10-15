package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return e.Message }
func bad(message string) error    { return &apiError{400, message} }

type endpoint func(http.ResponseWriter, *http.Request) error

func wrap(fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			var e *apiError
			if errors.As(err, &e) {
				reply(w, e.Status, map[string]string{"error": e.Message})
			} else {
				slog.Error("请求失败", "method", r.Method, "error_type", fmt.Sprintf("%T", err))
				reply(w, 500, map[string]string{"error": "操作失败，请稍后重试"})
			}
		}
	}
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Warn("响应发送失败", "error", err)
	}
}
func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return &apiError{415, "请使用 application/json"}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return bad("请求格式或字段无效")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return bad("请求只能包含一个 JSON 对象")
	}
	return nil
}
func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/site", wrap(a.siteInfo))
	mux.HandleFunc("GET /api/setup/status", wrap(a.setupStatus))
	mux.HandleFunc("POST /api/setup", wrap(a.setup))
	mux.HandleFunc("GET /api/products", wrap(a.listProducts))
	mux.HandleFunc("GET /api/products/{id}", wrap(a.productDetail))
	mux.HandleFunc("POST /api/orders", wrap(a.createOrder))
	mux.HandleFunc("POST /api/orders/quote", wrap(a.quoteOrder))
	mux.HandleFunc("GET /api/orders/{id}", wrap(a.orderDetail))
	mux.HandleFunc("POST /api/webhooks/hashpay", wrap(a.webhook))
	mux.HandleFunc("POST /api/admin/login", wrap(a.login))
	mux.HandleFunc("GET /api/admin/session", wrap(a.admin(a.session)))
	mux.HandleFunc("POST /api/admin/logout", wrap(a.admin(a.logout)))
	mux.HandleFunc("GET /api/admin/overview", wrap(a.admin(a.overview)))
	mux.HandleFunc("GET /api/admin/products", wrap(a.admin(a.adminProducts)))
	mux.HandleFunc("POST /api/admin/products", wrap(a.admin(a.saveProduct)))
	mux.HandleFunc("PUT /api/admin/products/{id}", wrap(a.admin(a.saveProduct)))
	mux.HandleFunc("DELETE /api/admin/products/{id}", wrap(a.admin(a.deleteProduct)))
	mux.HandleFunc("POST /api/admin/products/{id}/stock", wrap(a.admin(a.importStock)))
	mux.HandleFunc("GET /api/admin/products/{id}/stock", wrap(a.admin(a.listStock)))
	mux.HandleFunc("DELETE /api/admin/products/{id}/stock/{stock}", wrap(a.admin(a.deleteStock)))
	mux.HandleFunc("GET /api/admin/orders", wrap(a.admin(a.adminOrders)))
	mux.HandleFunc("GET /api/admin/promotions", wrap(a.admin(a.adminPromotions)))
	mux.HandleFunc("POST /api/admin/promotions", wrap(a.admin(a.savePromotion)))
	mux.HandleFunc("PUT /api/admin/promotions/{id}", wrap(a.admin(a.savePromotion)))
	mux.HandleFunc("DELETE /api/admin/promotions/{id}", wrap(a.admin(a.deletePromotion)))
	mux.HandleFunc("GET /api/admin/orders/{id}", wrap(a.admin(a.adminOrder)))
	mux.HandleFunc("DELETE /api/admin/orders/{id}", wrap(a.admin(a.deleteOrder)))
	mux.HandleFunc("POST /api/admin/orders/{id}/recheck", wrap(a.admin(a.recheckOrder)))
	mux.HandleFunc("POST /api/admin/orders/{id}/retry", wrap(a.admin(a.retryOrder)))
	mux.HandleFunc("GET /api/admin/settings", wrap(a.admin(a.getSettings)))
	mux.HandleFunc("PUT /api/admin/settings", wrap(a.admin(a.updateSettings)))
	mux.Handle("/", a.staticHandler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; media-src 'self' https:; font-src 'self'; connect-src 'self'; frame-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if !a.limits.allow("api:"+clientIP(r), 240, time.Minute) {
				reply(w, 429, map[string]string{"error": "请求过于频繁"})
				return
			}
			if !a.initialized.Load() && r.URL.Path != "/api/setup/status" && r.URL.Path != "/api/setup" && r.URL.Path != "/api/site" {
				reply(w, 503, map[string]string{"error": "请先完成系统初始化"})
				return
			}
		}
		defer func() {
			if recover() != nil {
				slog.Error("HTTP 处理发生异常")
				http.Error(w, "服务暂时不可用", 500)
			}
		}()
		mux.ServeHTTP(w, r)
	})
}

type limitEntry struct {
	count int
	until time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]limitEntry
}

func newLimiter() *limiter { return &limiter{entries: map[string]limitEntry{}} }
func (l *limiter) allow(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.entries) > 5000 {
		for k, e := range l.entries {
			if now.After(e.until) {
				delete(l.entries, k)
			}
		}
		if len(l.entries) > 5000 {
			return false
		}
	}
	e := l.entries[key]
	if now.After(e.until) {
		e = limitEntry{until: now.Add(window)}
	}
	e.count++
	l.entries[key] = e
	return e.count <= max
}
func clientIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if real := net.ParseIP(r.Header.Get("X-Real-IP")); real != nil {
			return real.String()
		}
	}
	return host
}
func (a *App) siteInfo(w http.ResponseWriter, r *http.Request) error {
	c := a.settings()
	reply(w, 200, map[string]any{"title": c.Title, "notice": c.Notice})
	return nil
}
