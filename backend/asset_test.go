package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newAssetTestApp(t *testing.T) *App {
	t.Helper()
	directory := t.TempDir()
	db, err := openDatabase(filepath.Join(directory, "hashdrop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := &App{db: db, vault: &vault{current: "v1", keys: map[string][]byte{"v1": bytes.Repeat([]byte{7}, 32)}, dedup: bytes.Repeat([]byte{8}, 32)}, config: Config{Database: filepath.Join(directory, "hashdrop.db"), PublicURL: "http://127.0.0.1:8080"}, limits: newLimiter()}
	a.initialized.Store(true)
	return a
}

func assetUploadRequest(t *testing.T, name string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/admin/assets", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func uploadTestAsset(t *testing.T, a *App, name string, data []byte) Asset {
	t.Helper()
	w := httptest.NewRecorder()
	if err := a.uploadAsset(w, assetUploadRequest(t, name, data)); err != nil {
		if cause := errors.Unwrap(err); cause != nil {
			t.Fatalf("附件上传失败 %T: %v", err, cause)
		}
		t.Fatal("附件上传失败", err)
	}
	var asset Asset
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &asset) != nil {
		t.Fatalf("附件上传失败: %d %s", w.Code, w.Body.String())
	}
	return asset
}

type assetDeadlineResponse struct {
	*httptest.ResponseRecorder
	readDeadline, writeDeadline time.Time
	readError, writeError       error
}

func newAssetDeadlineResponse() *assetDeadlineResponse {
	return &assetDeadlineResponse{ResponseRecorder: httptest.NewRecorder()}
}

func (w *assetDeadlineResponse) SetReadDeadline(deadline time.Time) error {
	w.readDeadline = deadline
	return w.readError
}

func (w *assetDeadlineResponse) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadline = deadline
	return w.writeError
}

func assertAssetDeadline(t *testing.T, deadline, before time.Time) {
	t.Helper()
	if deadline.Before(before.Add(assetTransferTimeout)) || deadline.After(time.Now().Add(assetTransferTimeout)) {
		t.Fatal("附件传输期限没有延长至 15 分钟", deadline)
	}
}

func TestAssetTransferDeadlineErrorsStopHandlers(t *testing.T) {
	a := newAssetTestApp(t)
	failure := errors.New("connection deadline failed")
	for _, operation := range []string{"upload-read", "upload-write", "download-write"} {
		t.Run(operation, func(t *testing.T) {
			w := newAssetDeadlineResponse()
			if operation == "upload-read" {
				w.readError = failure
			} else {
				w.writeError = failure
			}
			var err error
			if operation == "download-write" {
				err = a.serveAsset(w, httptest.NewRequest("GET", "/", nil), newID())
			} else {
				err = a.uploadAsset(w, assetUploadRequest(t, "secret.txt", []byte("private")))
			}
			if !errors.Is(err, failure) || w.Body.Len() != 0 {
				t.Fatal("连接期限设置失败后仍继续处理附件", err)
			}
		})
	}
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM assets").Scan(&count); err != nil || count != 0 {
		t.Fatal("连接期限设置失败仍保存了附件", count, err)
	}
	if err := assetTransferDeadlines(httptest.NewRecorder(), true); err != nil {
		t.Fatal("无连接的响应记录器不应要求 deadline 支持", err)
	}
}

type assetDeadlineCall struct {
	write bool
	at    time.Time
}

type assetDeadlineListener struct {
	net.Listener
	mu    sync.Mutex
	calls []assetDeadlineCall
}

func (l *assetDeadlineListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &assetDeadlineConn{Conn: conn, listener: l}, nil
}

func (l *assetDeadlineListener) record(write bool, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, assetDeadlineCall{write: write, at: at})
}

func (l *assetDeadlineListener) takeCalls() []assetDeadlineCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	calls := l.calls
	l.calls = nil
	return calls
}

type assetDeadlineConn struct {
	net.Conn
	listener *assetDeadlineListener
}

func (c *assetDeadlineConn) SetReadDeadline(at time.Time) error {
	c.listener.record(false, at)
	return c.Conn.SetReadDeadline(at)
}

func (c *assetDeadlineConn) SetWriteDeadline(at time.Time) error {
	c.listener.record(true, at)
	return c.Conn.SetWriteDeadline(at)
}

func TestAssetTransferDeadlinesOverrideRealServerDefaults(t *testing.T) {
	a := newAssetTestApp(t)
	asset := uploadTestAsset(t, a, "download.txt", []byte("private"))
	token := randomToken()
	if _, err := a.db.Exec("INSERT INTO admin_sessions(token_hash,expires_at,created_at) VALUES(?,?,?)", tokenHash(token), unixNow()+60, unixNow()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(a.routes()) // httptest 仅监听回环地址。
	listener := &assetDeadlineListener{Listener: server.Listener}
	server.Listener = listener
	server.Config.ReadTimeout = 15 * time.Second
	server.Config.WriteTimeout = 45 * time.Second
	server.Start()
	defer server.Close()
	a.config.PublicURL = server.URL
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for _, tc := range []struct {
		name, path            string
		upload, authenticated bool
		wantStatus            int
		wantRead, wantWrite   bool
	}{
		{"普通接口", "/api/site", false, false, 200, false, false},
		{"未授权上传", "/api/admin/assets", true, false, 401, false, false},
		{"已授权上传", "/api/admin/assets", true, true, 201, true, true},
		{"未授权下载", "/api/admin/assets/" + asset.ID, false, false, 401, false, false},
		{"已授权下载", "/api/admin/assets/" + asset.ID, false, true, 200, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", server.URL+tc.path, nil)
			if tc.upload {
				request = assetUploadRequest(t, "upload.txt", []byte("private"))
				request.URL.Scheme, request.URL.Host = "http", listener.Addr().String()
			}
			request.RequestURI = ""
			if tc.authenticated {
				request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
				request.Header.Set("Origin", server.URL)
				request.Header.Set("X-CSRF-Token", a.csrf(token))
			}
			listener.takeCalls()
			before := time.Now()
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if err != nil || response.StatusCode != tc.wantStatus {
				t.Fatal("附件路由响应失败", response.StatusCode, err)
			}
			var extendedRead, extendedWrite, defaultRead, defaultWrite bool
			for _, call := range listener.takeCalls() {
				if call.at.IsZero() {
					continue
				}
				if call.at.After(before.Add(time.Minute)) {
					assertAssetDeadline(t, call.at, before)
					if call.write {
						extendedWrite = true
					} else {
						extendedRead = true
					}
				} else if call.write {
					defaultWrite = true
				} else {
					defaultRead = true
				}
			}
			if !defaultRead || !defaultWrite || extendedRead != tc.wantRead || extendedWrite != tc.wantWrite {
				t.Fatalf("连接期限覆盖不正确: 默认 read=%v write=%v, 延长 read=%v write=%v", defaultRead, defaultWrite, extendedRead, extendedWrite)
			}
		})
	}
}

func TestAssetCipherAuthenticatesContentIdentityAndTermination(t *testing.T) {
	a := newAssetTestApp(t)
	id := newID()
	plain := bytes.Repeat([]byte("分块附件保密内容"), assetChunkSize/10)
	var sealed, restored bytes.Buffer
	size, err := writeEncryptedAsset(context.Background(), a.vault, id, &sealed, bytes.NewReader(plain))
	if err != nil || size != int64(len(plain)) {
		t.Fatal("附件加密失败", size, err)
	}
	if bytes.Contains(sealed.Bytes(), []byte("分块附件保密内容")) {
		t.Fatal("加密文件包含明文")
	}
	if err = readEncryptedAsset(context.Background(), a.vault, id, size, &restored, bytes.NewReader(sealed.Bytes())); err != nil || !bytes.Equal(restored.Bytes(), plain) {
		t.Fatal("附件解密结果错误", err)
	}
	for _, tc := range []struct {
		name string
		id   string
		data []byte
	}{
		{"替换资源身份", newID(), sealed.Bytes()},
		{"截断终止记录", id, sealed.Bytes()[:sealed.Len()-20]},
		{"附加未认证内容", id, append(append([]byte{}, sealed.Bytes()...), 1)},
		{"修改认证标签", id, append(append([]byte{}, sealed.Bytes()[:sealed.Len()-1]...), sealed.Bytes()[sealed.Len()-1]^1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := readEncryptedAsset(context.Background(), a.vault, tc.id, size, io.Discard, bytes.NewReader(tc.data)); err == nil {
				t.Fatal("损坏或越界的附件通过认证")
			}
		})
	}
}

type assetZeroReader struct{}

func (assetZeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestAssetUploadSizeLimit(t *testing.T) {
	a := newAssetTestApp(t)
	_, err := writeEncryptedAsset(context.Background(), a.vault, newID(), io.Discard, io.LimitReader(assetZeroReader{}, maxAssetSize+1))
	var failure *apiError
	if !errors.As(err, &failure) || failure.Status != 413 {
		t.Fatal("超限附件没有被拒绝", err)
	}
	response := httptest.NewRecorder()
	wrap(a.uploadAsset)(response, assetUploadRequest(t, "empty.txt", nil))
	if response.Code != 400 {
		t.Fatal("空附件没有被拒绝", response.Code)
	}
}

func TestAssetUploadRequiresAdminAndCSRF(t *testing.T) {
	a := newAssetTestApp(t)
	handler := wrap(a.admin(a.uploadAsset))
	response := newAssetDeadlineResponse()
	handler(response, assetUploadRequest(t, "a.txt", []byte("secret")))
	if response.Code != 401 || !response.readDeadline.IsZero() || !response.writeDeadline.IsZero() {
		t.Fatal("未登录可以上传附件", response.Code)
	}
	token := randomToken()
	if _, err := a.db.Exec("INSERT INTO admin_sessions(token_hash,expires_at,created_at) VALUES(?,?,?)", tokenHash(token), unixNow()+60, unixNow()); err != nil {
		t.Fatal(err)
	}
	request := assetUploadRequest(t, "a.txt", []byte("secret"))
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	response = newAssetDeadlineResponse()
	handler(response, request)
	if response.Code != 403 || !response.readDeadline.IsZero() || !response.writeDeadline.IsZero() {
		t.Fatal("缺少 CSRF 的上传通过", response.Code)
	}
	request = assetUploadRequest(t, "a.txt", []byte("secret"))
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	request.Header.Set("Origin", a.config.PublicURL)
	request.Header.Set("X-CSRF-Token", a.csrf(token))
	response = newAssetDeadlineResponse()
	before := time.Now()
	handler(response, request)
	if response.Code != http.StatusCreated {
		t.Fatal("合法管理员上传失败", response.Code, response.Body.String())
	}
	assertAssetDeadline(t, response.readDeadline, before)
	assertAssetDeadline(t, response.writeDeadline, before)
}

func TestAssetDownloadChecksOrderDeliveryAndAsset(t *testing.T) {
	a := newAssetTestApp(t)
	plain := []byte("<html><script>alert('private')</script></html>")
	asset := uploadTestAsset(t, a, "安装说明.html", plain)
	other := uploadTestAsset(t, a, "另一订单.txt", []byte("other"))
	productID, orderID, deliveryID, key := newID(), newID(), newID(), "附件领取密钥"
	doc := ContentDocument{Blocks: []ContentBlock{{ID: "file", Type: "file", AssetID: asset.ID}}}
	raw, err := encodeContent(doc)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := a.vault.encrypt(raw, "product-plan:"+productID)
	content, _ := a.vault.encrypt(raw, "delivery-content:"+deliveryID)
	result, _ := a.vault.encrypt(raw, "delivery:"+deliveryID)
	if _, err = a.db.Exec("INSERT INTO products(id,name,price,currency,delivery_plan,created_at,updated_at) VALUES(?, '附件商品',100,'CNY',?,1,1)", productID, plan); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec("INSERT INTO orders(id,product_id,product_name,unit_price,currency,quantity,total,token_hash,payment_status,created_at,updated_at) VALUES(?,?,'附件商品',100,'CNY',1,100,?,'pending',1,1)", orderID, productID, tokenHash(key)); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec("INSERT INTO deliveries(id,order_id,ordinal,content,result,status,updated_at) VALUES(?,?,1,?,?,'done',1)", deliveryID, orderID, content, result); err != nil {
		t.Fatal(err)
	}
	read := func(resource, credential string) *assetDeadlineResponse {
		r := httptest.NewRequest("GET", "/api/orders/"+orderID+"/deliveries/"+deliveryID+"/assets/"+resource, nil)
		r.SetPathValue("id", orderID)
		r.SetPathValue("delivery", deliveryID)
		r.SetPathValue("asset", resource)
		if credential != "" {
			r.Header.Set("Authorization", "ClaimKey "+base64.RawURLEncoding.EncodeToString([]byte(credential)))
		}
		w := newAssetDeadlineResponse()
		wrap(a.deliveryAsset)(w, r)
		return w
	}
	if response := read(asset.ID, key); response.Code == 200 || !response.writeDeadline.IsZero() {
		t.Fatal("未付款可以下载附件")
	}
	if _, err = a.db.Exec("UPDATE orders SET payment_status='paid' WHERE id=?", orderID); err != nil {
		t.Fatal(err)
	}
	for _, credential := range []string{"", "错误密钥"} {
		if response := read(asset.ID, credential); response.Code == 200 || !response.writeDeadline.IsZero() {
			t.Fatal("错误凭证可以下载附件")
		}
	}
	if response := read(other.ID, key); response.Code != 404 || !response.writeDeadline.IsZero() {
		t.Fatal("允许读取交付文档之外的附件", response.Code)
	}
	before := time.Now()
	response := read(asset.ID, key)
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), plain) || response.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment;") || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("合法附件下载内容或安全响应头错误", response.Code, response.Header())
	}
	assertAssetDeadline(t, response.writeDeadline, before)
	if !response.readDeadline.IsZero() {
		t.Fatal("下载改变了请求读取期限")
	}
	file, _ := assetFile(assetDirectory(a.config.Database), asset.ID)
	damaged, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	damaged[len(damaged)-1] ^= 1
	if err = os.WriteFile(file, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	if response := read(asset.ID, key); response.Code != 503 || bytes.Contains(response.Body.Bytes(), plain) {
		t.Fatal("损坏文件输出了成功响应或明文", response.Code)
	}
	if _, err = a.db.Exec("UPDATE deliveries SET status='pending' WHERE id=?", deliveryID); err != nil {
		t.Fatal(err)
	}
	if response := read(asset.ID, key); response.Code == 200 {
		t.Fatal("未交付内容可以下载附件")
	}
}

func TestAssetReferencesRejectUnsafeImagesAndPreserveHistory(t *testing.T) {
	a := newAssetTestApp(t)
	asset := uploadTestAsset(t, a, "教程.svg", []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"><script>alert(1)</script></svg>"))
	tx, err := a.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = a.replaceAssetRefs(context.Background(), tx, "product", "product", ContentDocument{Blocks: []ContentBlock{{ID: "image", Type: "image", AssetID: asset.ID}}}); err == nil {
		t.Fatal("允许主动内容作为内嵌图片")
	}
	tx.Rollback()
	var picture bytes.Buffer
	if err = png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	imageAsset := uploadTestAsset(t, a, "教程.png", picture.Bytes())
	tx, err = a.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = a.replaceAssetRefs(context.Background(), tx, "delivery", "old-delivery", ContentDocument{Blocks: []ContentBlock{{ID: "image", Type: "image", AssetID: imageAsset.ID}}}); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec("UPDATE assets SET created_at=1,unreferenced_at=1"); err != nil {
		t.Fatal(err)
	}
	if err = a.cleanupAssets(context.Background()); err != nil {
		t.Fatal(err)
	}
	orphanPath, _ := assetFile(assetDirectory(a.config.Database), asset.ID)
	keptPath, _ := assetFile(assetDirectory(a.config.Database), imageAsset.ID)
	if _, err = os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatal("过期无引用附件未清理", err)
	}
	if _, err = os.Stat(keptPath); err != nil {
		t.Fatal("历史交付引用附件被清理", err)
	}
}

func TestBackupIncludesImmutableAssetsAndCanRestore(t *testing.T) {
	a := newAssetTestApp(t)
	plain := bytes.Repeat([]byte("历史订单附件"), 10000)
	asset := uploadTestAsset(t, a, "delivery.bin", plain)
	orphan := uploadTestAsset(t, a, "unsaved.bin", []byte("temporary"))
	tx, err := a.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = a.replaceAssetRefs(context.Background(), tx, "delivery", "historical", ContentDocument{Blocks: []ContentBlock{{ID: "file", Type: "file", AssetID: asset.ID}}}); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "complete-backup")
	if err = a.backupData(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if err = a.backupData(context.Background(), target); err == nil {
		t.Fatal("备份覆盖了已有目录")
	}
	restored, err := openDatabase(filepath.Join(target, "hashdrop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var count int
	if err = restored.QueryRow("SELECT COUNT(*) FROM assets WHERE id=?", orphan.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("备份包含未保存的临时上传", err)
	}
	var size int64
	if err = restored.QueryRow("SELECT size FROM assets WHERE id=?", asset.ID).Scan(&size); err != nil || size != int64(len(plain)) {
		t.Fatal("备份丢失被引用附件", err)
	}
	file, _ := assetFile(filepath.Join(target, "assets"), asset.ID)
	input, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	var output bytes.Buffer
	if err = readEncryptedAsset(context.Background(), a.vault, asset.ID, size, &output, input); err != nil || !bytes.Equal(output.Bytes(), plain) {
		t.Fatal("备份附件无法恢复", err)
	}
	if _, err = os.Stat(filepath.Join(target, "manifest.json")); err != nil {
		t.Fatal("备份缺少校验清单", err)
	}
	source, _ := assetFile(assetDirectory(a.config.Database), asset.ID)
	if err = os.Remove(source); err != nil {
		t.Fatal(err)
	}
	failedTarget := filepath.Join(t.TempDir(), "must-not-publish")
	if err = a.backupData(context.Background(), failedTarget); err == nil {
		t.Fatal("附件缺失仍标记备份完成")
	}
	if _, err = os.Stat(failedTarget); !os.IsNotExist(err) {
		t.Fatal("失败备份发布了最终目录", err)
	}
}
