package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func newSetupApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	c, err := bootstrapConfig(filepath.Join(dir, "data"), "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	token := randomToken()
	a := &App{config: c, configPath: filepath.Join(dir, "config", "config.json"), setupHash: tokenHash(token), limits: newLimiter(), adapters: map[string]APIAdapter{}, wake: make(chan struct{}, 1)}
	t.Cleanup(func() {
		if a.db != nil {
			a.db.Close()
		}
	})
	return a, token
}
func setupBody() setupInput {
	return setupInput{Title: "网页初始化店铺", PublicURL: "http://127.0.0.1:8080", Password: "setup-password-only", PasswordConfirm: "setup-password-only", ManualPolicy: "hold"}
}
func postSetup(a *App, token, origin string, input setupInput) *httptest.ResponseRecorder {
	body, _ := json.Marshal(input)
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/setup", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	return w
}
func TestSetupRequiresCredentialAndLeavesFailedInputUncommitted(t *testing.T) {
	a, token := newSetupApp(t)
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, httptest.NewRequest("GET", "/api/products", nil))
	if w.Code != 503 {
		t.Fatal("未初始化时开放了业务 API")
	}
	a.runPending(context.Background()) // 初始化之前调度器不能访问空数据库。
	input := setupBody()
	for _, test := range []struct{ token, origin string }{{randomToken(), input.PublicURL}, {token, "http://other.example"}, {token, ""}} {
		if w = postSetup(a, test.token, test.origin, input); w.Code != 403 {
			t.Fatalf("接受了无效凭证或来源: %d %s", w.Code, w.Body)
		}
	}
	input.PasswordConfirm = "different-password"
	if w = postSetup(a, token, input.PublicURL, input); w.Code != 400 {
		t.Fatal("接受了不一致的密码")
	}
	input = setupBody()
	input.TOTPEnabled = true
	if w = postSetup(a, token, input.PublicURL, input); w.Code != 400 {
		t.Fatal("启用 TOTP 却允许留空密钥")
	}
	input.TOTPSecret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	input.TOTPCode = "invalid"
	if w = postSetup(a, token, input.PublicURL, input); w.Code != 400 {
		t.Fatal("启用 TOTP 却接受了错误验证码")
	}
	input = setupBody()
	input.HashPayURL, input.MerchantID, input.PrivateKey = "https://pay.example.com", "merchant", "not-a-key"
	if w = postSetup(a, token, input.PublicURL, input); w.Code != 400 {
		t.Fatal("接受了无效私钥")
	}
	if a.initialized.Load() || a.db != nil {
		t.Fatal("失败的初始化改变了运行状态")
	}
	for _, path := range []string{a.configPath, a.config.Database} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("失败的初始化留下配置或数据库: %s", path)
		}
	}
}
func TestPublicURLMatchesBrowserOrigin(t *testing.T) {
	for input, want := range map[string]string{
		"http://Shop.Example.com:80/":  "http://shop.example.com",
		"https://Shop.Example.com:443": "https://shop.example.com",
		"http://127.0.0.1:08080/":      "http://127.0.0.1:8080",
		"http://[::1]:80":              "http://[::1]",
	} {
		got, err := normalizePublicURL(input)
		if err != nil || got != want {
			t.Fatalf("%s 未匹配浏览器 Origin: %s %v", input, got, err)
		}
	}
	for _, input := range []string{"http://shop.example.com/orders", "http://user:password@shop.example.com", "http://shop.example.com:65536", "http://shop.example.com:0", "javascript:alert(1)"} {
		if _, err := normalizePublicURL(input); err == nil {
			t.Fatalf("接受了无效站点地址: %s", input)
		}
	}
}
func TestSetupPersistsKeysAndSerializesConcurrentSubmissions(t *testing.T) {
	a, token := newSetupApp(t)
	source, _ := newTestApp(t)
	key, err := os.ReadFile(source.config.HashPay.PrivateKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	input := setupBody()
	input.HashPayURL, input.MerchantID, input.PrivateKey = "https://pay.example.com", "merchant", string(key)
	responses := make(chan *httptest.ResponseRecorder, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() { defer group.Done(); responses <- postSetup(a, token, input.PublicURL, input) }()
	}
	group.Wait()
	close(responses)
	counts := map[int]int{}
	for w := range responses {
		counts[w.Code]++
		for _, secret := range []string{token, input.Password, input.PrivateKey, "encryption_keys", "dedup_key"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("初始化响应泄露凭据")
			}
		}
	}
	if counts[201] != 1 || counts[409] != 1 || a.setupHash != "" {
		t.Fatalf("初始化未保持一次性: %v", counts)
	}
	c, v, err := readConfig(a.configPath)
	if err != nil || !passwordMatches(c.AdminPassword, input.Password) || c.PublicURL != input.PublicURL || c.HashPay.PrivateKeyFile == "" {
		t.Fatalf("网页配置未持久化: %v", err)
	}
	if runtime.GOOS != "windows" {
		for _, path := range []string{a.configPath, c.HashPay.PrivateKeyFile} {
			info, e := os.Stat(path)
			if e != nil || info.Mode().Perm() != 0600 {
				t.Fatal("凭据文件权限不是 0600")
			}
		}
	}
	sealed, err := a.vault.encrypt("重启后仍可解密", "test:setup")
	if err != nil {
		t.Fatal(err)
	}
	if plain, e := v.decrypt(sealed, "test:setup"); e != nil || plain != "重启后仍可解密" {
		t.Fatal("重启读取配置丢失加密密钥")
	}
	if _, err = newHashPay(c.HashPay); err != nil {
		t.Fatalf("保存的商户私钥不可用: %v", err)
	}
	assertLoginCookie(t, a, false)
	// HTTPS 配置保持 Secure，即使并非开发模式。
	a.configMu.Lock()
	a.config.PublicURL = "https://shop.example.com"
	a.configMu.Unlock()
	assertLoginCookie(t, a, true)
}
func assertLoginCookie(t *testing.T, a *App, secure bool) {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/admin/login", strings.NewReader(`{"password":"setup-password-only"}`))
	r.Header.Set("Origin", a.settings().PublicURL)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	cookies := w.Result().Cookies()
	if w.Code != 200 || len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Secure != secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("网页初始化后登录或 Cookie 策略错误: %d %v", w.Code, cookies)
	}
}
func TestSetupDoesNotReplaceExistingDatabase(t *testing.T) {
	a, token := newSetupApp(t)
	if err := os.MkdirAll(filepath.Dir(a.config.Database), 0700); err != nil {
		t.Fatal(err)
	}
	old := []byte("existing-database-evidence")
	if err := os.WriteFile(a.config.Database, old, 0600); err != nil {
		t.Fatal(err)
	}
	input := setupBody()
	w := postSetup(a, token, input.PublicURL, input)
	if w.Code != 409 || a.initialized.Load() {
		t.Fatal("配置丢失时允许重新初始化已有数据库")
	}
	data, err := os.ReadFile(a.config.Database)
	if err != nil || string(data) != string(old) {
		t.Fatal("已有数据库被修改")
	}
}
func TestSetupConfigWriteFailureAllowsSafeRetry(t *testing.T) {
	a, token := newSetupApp(t)
	// 把配置父目录占为文件，模拟不能保存配置；空库必须清理，避免锁死初始化。
	dir := filepath.Dir(a.configPath)
	if err := os.WriteFile(dir, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	input := setupBody()
	if w := postSetup(a, token, input.PublicURL, input); w.Code != 500 {
		t.Fatalf("配置保存失败未被报告: %d", w.Code)
	}
	if a.initialized.Load() || a.db != nil {
		t.Fatal("保存失败仍开放业务路由")
	}
	if _, err := os.Stat(a.config.Database); !os.IsNotExist(err) {
		t.Fatal("失败后保留了新建空库，无法重试")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if w := postSetup(a, token, input.PublicURL, input); w.Code != 201 {
		t.Fatalf("修复写权限后不能重试: %s", w.Body)
	}
}
