package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeGateway struct {
	mu             sync.Mutex
	key            *rsa.PrivateKey
	orders         map[string]GatewayOrder
	returnURLs     []string
	corrupt        string
	corruptCreate  string
	server         *httptest.Server
	creates        int
	failCreateOnce bool
	queryStatus    int
	queryType      string
	queryBody      string
	queries        int
}

func newTestApp(t *testing.T) (*App, *fakeGateway) {
	t.Helper()
	dir := t.TempDir()
	db, err := openDatabase(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "merchant.pem")
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	v := &vault{current: "v1", keys: map[string][]byte{"v1": bytes.Repeat([]byte{7}, 32)}, dedup: bytes.Repeat([]byte{8}, 32)}
	g := &fakeGateway{key: key, orders: map[string]GatewayOrder{}}
	g.server = httptest.NewServer(http.HandlerFunc(g.handler))
	t.Cleanup(g.server.Close)
	c := Config{Listen: "127.0.0.1:8080", PublicURL: "http://127.0.0.1:8080", Title: "TinyFaka", Development: true, Database: filepath.Join(dir, "test.db"), BackupDir: filepath.Join(dir, "backups"), AdminUsername: "admin", AdminPassword: passwordHash("test-password-only"), HashPay: HashPayConfig{BaseURL: g.server.URL, MerchantID: "merchant-test", PrivateKeyFile: keyPath}}
	app := &App{db: db, vault: v, config: c, limits: newLimiter(), wake: make(chan struct{}, 1)}
	app.initialized.Store(true)
	return app, g
}
func (g *fakeGateway) handler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "body", 400)
		return
	}
	ts := r.Header.Get("X-Timestamp")
	sum := sha256.Sum256([]byte(r.Method + "\n" + r.URL.EscapedPath() + "\n" + ts + "\n" + string(body)))
	signature, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Signature"))
	if err != nil || rsa.VerifyPKCS1v15(&g.key.PublicKey, crypto.SHA256, sum[:], signature) != nil || r.Header.Get("X-Merchant-Id") != "merchant-test" {
		http.Error(w, "signature", 401)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.Method == "POST" {
		var input struct {
			MerchantNo string      `json:"merchantNo"`
			Amount     json.Number `json:"amount"`
			Currency   string      `json:"currency"`
			ReturnURL  string      `json:"return_url"`
		}
		if err = json.Unmarshal(body, &input); err != nil {
			http.Error(w, "json", 400)
			return
		}
		o, exists := g.orders[input.MerchantNo]
		if !exists {
			o = GatewayOrder{ID: "gw-" + input.MerchantNo, MerchantID: "merchant-test", MerchantNo: input.MerchantNo, Amount: input.Amount, Currency: input.Currency, Status: "pending", ExpiresAt: unixNow() + 900, ExpireAt: unixNow() + 900}
			g.orders[input.MerchantNo] = o
			g.creates++
		}
		g.returnURLs = append(g.returnURLs, input.ReturnURL)
		if g.failCreateOnce {
			g.failCreateOnce = false
			http.Error(w, "temporary", 503)
			return
		}
		// 对齐 HashPay 原生创建摘要：身份字段只有查单接口返回。
		created := map[string]any{"id": o.ID, "amount": o.Amount, "currency": o.Currency, "status": o.Status, "expiresAt": o.ExpiresAt}
		if g.corruptCreate == "merchant" {
			created["merchantId"] = "other"
		} else if g.corruptCreate == "number" {
			created["merchantNo"] = "other"
		}
		reply(w, 200, map[string]any{"order": created, "checkoutUrl": g.server.URL + "/pay/" + o.ID, "reused": exists})
		return
	}
	g.queries++
	if g.queryStatus != 0 {
		w.Header().Set("Content-Type", g.queryType)
		w.WriteHeader(g.queryStatus)
		fmt.Fprint(w, g.queryBody)
		return
	}
	for _, o := range g.orders {
		if strings.TrimPrefix(r.URL.Path, "/api/order/") == o.ID {
			switch g.corrupt {
			case "amount":
				o.Amount = json.Number("99.99")
			case "currency":
				o.Currency = "USD"
			case "merchant":
				o.MerchantID = "other"
			case "number":
				o.MerchantNo = "other"
			case "id":
				o.ID = "other"
			}
			reply(w, 200, o)
			return
		}
	}
	reply(w, http.StatusNotFound, map[string]any{"error": map[string]any{"key": "errors.order_not_found", "params": map[string]any{}}})
}
func (g *fakeGateway) set(id, status, by string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	o := g.orders[id]
	o.Status = status
	o.Payment.TX.ConfirmedBy = by
	g.orders[id] = o
}
func insertProduct(t *testing.T, a *App, kind string, stock int) string {
	t.Helper()
	id := newID()
	content := ""
	var err error
	if kind == "static" {
		content, err = a.vault.encrypt("购买时内容", "inventory:"+id)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = a.db.Exec("INSERT INTO inventories(id,name,kind,content,created_at,updated_at) VALUES(?,?,?,?,?,?)", id, "测试库存池", kind, content, unixNow(), unixNow()); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec("INSERT INTO products(id,name,price,currency,inventory_id,active,created_at,updated_at) VALUES(?,?,?,?,?,1,?,?)", id, "测试商品", 1234, "CNY", id, unixNow(), unixNow()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < stock; i++ {
		stockID := newID()
		plain := fmt.Sprintf("卡密-%d", i)
		encrypted, e := a.vault.encrypt(plain, "stock:"+stockID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = a.db.Exec("INSERT INTO stock_items(id,inventory_id,content,fingerprint,created_at) VALUES(?,?,?,?,?)", stockID, id, encrypted, a.vault.fingerprint(id, plain), unixNow()); e != nil {
			t.Fatal(e)
		}
	}
	return id
}
func reserve(t *testing.T, a *App, product string, n int) (Order, string) {
	t.Helper()
	token := randomToken()
	o, err := a.reserveOrder(context.Background(), product, n, token, newID())
	if err != nil {
		t.Fatal(err)
	}
	return o, token
}
func syncPaid(t *testing.T, a *App, g *fakeGateway, o Order) {
	t.Helper()
	if err := a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	g.set(o.ID, "paid", "system")
	if err := a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentReservation(t *testing.T) {
	a, _ := newTestApp(t)
	id := insertProduct(t, a, "card", 5)
	var wg sync.WaitGroup
	success := make(chan Order, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := a.reserveOrder(context.Background(), id, 1, randomToken(), newID())
			if err == nil {
				success <- o
			}
		}()
	}
	wg.Wait()
	close(success)
	if len(success) != 5 {
		t.Fatalf("预留成功数 %d，预期 5", len(success))
	}
	var reserved, orders int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM stock_items WHERE status='reserved'").Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if reserved != 5 || orders != 5 {
		t.Fatalf("reserved=%d orders=%d", reserved, orders)
	}
}
func TestVerifiedPaymentAndToken(t *testing.T) {
	a, g := newTestApp(t)
	useTLSGateway(t, a, g)
	product := insertProduct(t, a, "account", 2)
	o, token := reserve(t, a, product, 2)
	syncPaid(t, a, g, o)
	if err := a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	var sold, delivered int
	a.db.QueryRow("SELECT COUNT(*) FROM stock_items WHERE status='sold'").Scan(&sold)
	a.db.QueryRow("SELECT COUNT(*) FROM deliveries WHERE status='done'").Scan(&delivered)
	if sold != 2 || delivered != 2 {
		t.Fatalf("sold=%d delivered=%d", sold, delivered)
	}
	for _, tc := range []struct {
		token string
		want  int
	}{{token, 200}, {randomToken(), 404}, {"", 401}} {
		req := httptest.NewRequest("GET", "/api/orders/"+o.ID, nil)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		a.routes().ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("领取返回 %d 预期 %d", w.Code, tc.want)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("敏感响应未禁止缓存")
		}
		if tc.want == 200 && !strings.Contains(w.Body.String(), "卡密-") {
			t.Fatal("缺少真实交付内容")
		}
	}
	raw, err := os.ReadFile(a.config.Database)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, []byte("卡密-0")) {
		t.Fatal("数据库存在明文凭证或卡密")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, url := range g.returnURLs {
		if strings.Contains(url, token) || strings.Contains(url, "#") {
			t.Fatal("领取凭证传给网关")
		}
	}
}
func TestPaymentMismatchHoldsInventory(t *testing.T) {
	for _, field := range []string{"amount", "currency", "merchant", "number", "id"} {
		t.Run(field, func(t *testing.T) {
			a, g := newTestApp(t)
			o, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
			if err := a.syncOrder(context.Background(), o.ID); err != nil {
				t.Fatal(err)
			}
			g.set(o.ID, "paid", "system")
			g.mu.Lock()
			g.corrupt = field
			g.mu.Unlock()
			if err := a.syncOrder(context.Background(), o.ID); err == nil {
				t.Fatal("不匹配结果被接受")
			}
			o, err := a.loadOrder(context.Background(), o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if o.PaymentStatus == "paid" || o.ReservationStatus != "reserved" {
				t.Fatalf("错误状态 %+v", o)
			}
		})
	}
}
func TestStaticSnapshotAndGatewayAdminPayment(t *testing.T) {
	a, g := newTestApp(t)
	id := insertProduct(t, a, "static", 0)
	o, _ := reserve(t, a, id, 1)
	changed, err := a.vault.encrypt("后来修改的内容", "inventory:"+id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec("UPDATE inventories SET content=? WHERE id=?", changed, id); err != nil {
		t.Fatal(err)
	}
	if err = a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	g.set(o.ID, "expired", "")
	if err = a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	g.set(o.ID, "paid", "admin")
	if err = a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := a.loadOrder(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PaymentStatus != "paid" {
		t.Fatal("HashPay 人工确认的已付款状态未被接受")
	}
	results, err := a.deliveryResults(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Content != "购买时内容" {
		t.Fatalf("快照未保存 %+v", results)
	}
}
func envelope(t *testing.T, key *rsa.PublicKey, order, merchantNo string) []byte {
	t.Helper()
	aesKey := bytes.Repeat([]byte{3}, 32)
	wrapped, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, key, aesKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, 12)
	if _, err = rand.Read(iv); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"timestamp": unixNow(), "payload": map[string]string{"orderId": order, "merchantNo": merchantNo}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(callbackEnvelope{"RSA-OAEP-256+A256GCM", base64.StdEncoding.EncodeToString(wrapped), base64.StdEncoding.EncodeToString(iv), base64.StdEncoding.EncodeToString(gcm.Seal(nil, iv, body, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func sendWebhook(a *App, data []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/webhooks/hashpay", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HashPay-Merchant", "merchant-test")
	req.Header.Set("X-HashPay-Encryption", "RSA-OAEP-256+A256GCM")
	req.Header.Set("X-HashPay-Timestamp", strconv.FormatInt(unixNow(), 10))
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, req)
	return w
}
func TestForgedAndDuplicateCallback(t *testing.T) {
	a, g := newTestApp(t)
	o, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
	if err := a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := a.loadOrder(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	forged := envelope(t, &g.key.PublicKey, loaded.GatewayID, o.ID)
	if w := sendWebhook(a, forged); w.Code != 200 {
		t.Fatalf("通知查询失败 %s", w.Body)
	}
	loaded, err = a.loadOrder(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PaymentStatus == "paid" {
		t.Fatal("仅凭可伪造密文确认了付款")
	}
	g.set(o.ID, "paid", "system")
	for i := 0; i < 3; i++ {
		if w := sendWebhook(a, forged); w.Code != 200 {
			t.Fatalf("通知失败 %s", w.Body)
		}
	}
	deliveries, err := a.deliveryResults(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 || deliveries[0].Status != "done" {
		t.Fatal("重复通知交付错误")
	}
	if w := sendWebhook(a, []byte(`{"alg":"bad"}`)); w.Code != 400 {
		t.Fatal("未拒绝无效通知")
	}
}

func TestSessionCSRFAndBackup(t *testing.T) {
	a, _ := newTestApp(t)
	req := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(`{"username":"admin","password":"test-password-only"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", a.config.PublicURL)
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("会话 cookie 属性错误")
	}
	var response struct {
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, csrf := range []string{"", response.CSRF} {
		req = httptest.NewRequest("POST", "/api/admin/logout", nil)
		req.AddCookie(cookies[0])
		req.Header.Set("Origin", a.config.PublicURL)
		req.Header.Set("X-CSRF-Token", csrf)
		w = httptest.NewRecorder()
		a.routes().ServeHTTP(w, req)
		want := 403
		if csrf != "" {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("csrf 返回 %d 预期 %d", w.Code, want)
		}
	}
	insertProduct(t, a, "card", 2)
	target := filepath.Join(t.TempDir(), "backup.db")
	if err := backupDatabase(context.Background(), a.db, target); err != nil {
		t.Fatal(err)
	}
	restored, err := openDatabase(target)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var n int
	if err = restored.QueryRow("SELECT COUNT(*) FROM stock_items").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatal("恢复备份库存数量不一致")
	}
}
func TestAmountAndEncryptionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		input, currency string
		want            int64
		valid           bool
	}{{"12.34", "CNY", 1234, true}, {"1.2300", "USD", 123, true}, {"0.000001", "USDT", 1, true}, {"1", "JPY", 1, true}, {"0", "CNY", 0, false}, {"1.001", "CNY", 0, false}, {"1e2", "CNY", 0, false}, {"-1", "USD", 0, false}, {"999999999999999", "CNY", 0, false}} {
		got, err := decimalMinor(tc.input, tc.currency)
		if (err == nil) != tc.valid || (tc.valid && got != tc.want) {
			t.Fatalf("%s %s -> %d %v", tc.input, tc.currency, got, err)
		}
	}
	a, _ := newTestApp(t)
	cipher, err := a.vault.encrypt("secret", "stock:one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.vault.decrypt(cipher, "stock:two"); err == nil {
		t.Fatal("密文可跨记录替换")
	}
}

// 浏览器测试只替换 HashPay 边界，商城、后台、数据库与交付均运行真实代码。
func TestBrowserFixture(t *testing.T) {
	if os.Getenv("TINYFAKA_BROWSER_FIXTURE") != "1" {
		t.Skip("仅供 Playwright 启动真实后端")
	}
	a, g := newTestApp(t)
	useTLSGateway(t, a, g)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a.config.PublicURL = "http://" + ln.Addr().String()
	a.configPath = filepath.Join(t.TempDir(), "config.json")
	a.config.CurrentKey = "v1"
	a.config.EncryptionKeys = map[string]string{"v1": base64.StdEncoding.EncodeToString(a.vault.keys["v1"])}
	a.config.DedupKey = base64.StdEncoding.EncodeToString(a.vault.dedup)
	// 测试网关使用临时 TLS 证书，信任仅注入测试进程。
	insertProduct(t, a, "card", 3)
	insertProduct(t, a, "static", 0)
	fresh := newSetupApp(t)
	freshServer := httptest.NewServer(fresh.routes())
	t.Cleanup(freshServer.Close)
	fresh.config.Listen = strings.TrimPrefix(freshServer.URL, "http://")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /__test/reset-limits", func(w http.ResponseWriter, r *http.Request) {
		// 各浏览器用例分别使用真实限额，避免共享夹具累积请求导致互相限流。
		a.limits.mu.Lock()
		clear(a.limits.entries)
		a.limits.mu.Unlock()
		reply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /__test/media/clip.webm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/webm")
		http.ServeFile(w, r, "../frontend/tests/fixtures/test-1s.webm")
	})
	mux.HandleFunc("GET /__test/media/image.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="220" viewBox="0 0 640 220"><rect width="640" height="220" rx="16" fill="#eef8f1"/><rect x="28" y="28" width="64" height="164" rx="8" fill="#18a058"/><text x="120" y="110" font-family="sans-serif" font-size="32" fill="#111827">TinyFaka</text><text x="120" y="155" font-family="sans-serif" font-size="20" fill="#6b7280">Product media preview</text></svg>`)
	})
	mux.HandleFunc("GET /__test/setup-fixture", func(w http.ResponseWriter, r *http.Request) {
		key, err := os.ReadFile(a.config.HashPay.PrivateKeyFile)
		if err != nil {
			http.Error(w, "fixture", 500)
			return
		}
		reply(w, 200, map[string]string{"url": freshServer.URL, "hashpay_url": g.server.URL, "private_key": string(key)})
	})
	mux.HandleFunc("POST /__test/pay/{id}", func(w http.ResponseWriter, r *http.Request) {
		g.set(r.PathValue("id"), "paid", "system")
		if err := a.syncOrder(r.Context(), r.PathValue("id")); err != nil {
			http.Error(w, "payment", 500)
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /__test/pay-donation/{id}", func(w http.ResponseWriter, r *http.Request) {
		g.set(r.PathValue("id"), "paid", "system")
		if err := a.syncDonation(r.Context(), r.PathValue("id")); err != nil {
			http.Error(w, "payment", 500)
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	})
	mux.Handle("/", a.routes())
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	fmt.Println("TINYFAKA_FIXTURE_URL=" + a.config.PublicURL)
	if path := os.Getenv("TINYFAKA_FIXTURE_URL_FILE"); path != "" {
		if err = os.WriteFile(path, []byte(a.config.PublicURL), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	a.runScheduler(ctx)
}

func useTLSGateway(t *testing.T, a *App, g *fakeGateway) {
	t.Helper()
	plain := g.server
	g.server = httptest.NewTLSServer(http.HandlerFunc(g.handler))
	t.Cleanup(g.server.Close)
	plain.Close()
	original := http.DefaultTransport
	http.DefaultTransport = g.server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = original })
	a.config.HashPay.BaseURL = g.server.URL
}

func TestUnknownGatewayCreationReusesOrder(t *testing.T) {
	a, g := newTestApp(t)
	o, _ := reserve(t, a, insertProduct(t, a, "card", 1), 1)
	g.failCreateOnce = true
	if err := a.syncOrder(context.Background(), o.ID); err == nil {
		t.Fatal("未模拟到创建结果不明")
	}
	first, err := a.loadOrder(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.PaymentStatus != "creating" || first.GatewayID != "" || first.ReservationStatus != "reserved" {
		t.Fatal("不明结果错误释放库存")
	}
	if err = a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	if g.creates != 1 {
		t.Fatalf("创建了 %d 个网关订单", g.creates)
	}
}

func TestGatewayTerminalStatePreservesAllocation(t *testing.T) {
	a, g := newTestApp(t)
	product := insertProduct(t, a, "card", 1)
	o, _ := reserve(t, a, product, 1)
	if err := a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	g.set(o.ID, "expired", "")
	if err := a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	expired, err := a.loadOrder(context.Background(), o.ID)
	if err != nil || expired.PaymentStatus != "expired" || expired.ReservationStatus != "reserved" {
		t.Fatal("过期状态未同步或库存被释放")
	}
	g.set(o.ID, "invalid", "")
	if err := a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	next, _ := reserve(t, a, product, 1)
	// 即使网关异常地从终态转回付款，也不能拿走后来订单预留的库存。
	g.set(o.ID, "paid", "system")
	if err := a.syncOrder(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	old, err := a.loadOrder(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.PaymentStatus != "paid" || old.DeliveryStatus != "manual" {
		t.Fatal("释放后的迟到付款未转人工")
	}
	var assigned string
	if err = a.db.QueryRow("SELECT order_id FROM stock_items WHERE inventory_id=?", product).Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	if assigned != next.ID {
		t.Fatal("后续订单库存被迟到付款占用")
	}
}
