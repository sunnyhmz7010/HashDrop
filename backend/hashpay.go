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
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type GatewayOrder struct {
	ID         string      `json:"id"`
	MerchantID string      `json:"merchantId"`
	MerchantNo string      `json:"merchantNo"`
	Amount     json.Number `json:"amount"`
	Currency   string      `json:"currency"`
	Status     string      `json:"status"`
	ExpiresAt  int64       `json:"expiresAt"`
	ExpireAt   int64       `json:"expireAt"`
	Payment    struct {
		TX struct {
			ConfirmedBy string `json:"confirmedBy"`
		} `json:"tx"`
	} `json:"payment"`
}
type hashPayClient struct {
	config HashPayConfig
	key    *rsa.PrivateKey
	http   *http.Client
}

type hashPayHTTPError struct {
	Status int
	Key    string
}

func (e *hashPayHTTPError) Error() string { return fmt.Sprintf("HashPay HTTP %d", e.Status) }

func newHashPay(config HashPayConfig) (*hashPayClient, error) {
	if config.BaseURL == "" || config.MerchantID == "" || config.PrivateKeyFile == "" {
		return nil, errors.New("HashPay 尚未配置")
	}
	info, err := os.Stat(config.PrivateKeyFile)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("HashPay 私钥文件不存在或权限不是 0600")
	}
	data, err := os.ReadFile(config.PrivateKeyFile)
	if err != nil {
		return nil, errors.New("HashPay 私钥文件无法读取")
	}
	key, err := parsePrivateKey(data)
	if err != nil {
		return nil, err
	}
	return &hashPayClient{config: config, key: key, http: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("HashPay 私钥 PEM 无效")
	}
	var key *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		key, _ = parsed.(*rsa.PrivateKey)
	} else {
		key, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	if key == nil || key.N.BitLen() < 2048 {
		return nil, errors.New("HashPay 私钥必须为至少 2048 位 RSA")
	}
	if err := key.Validate(); err != nil {
		return nil, errors.New("HashPay 私钥无效")
	}
	return key, nil
}
func (c *hashPayClient) request(ctx context.Context, method, path string, body []byte, out any) error {
	timestamp := strconv.FormatInt(unixNow(), 10)
	sum := sha256.Sum256([]byte(method + "\n" + path + "\n" + timestamp + "\n" + string(body)))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, sum[:])
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.config.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Merchant-Id", c.config.MerchantID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", base64.StdEncoding.EncodeToString(sig))
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return errors.New("HashPay 请求失败或超时")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return errors.New("HashPay 响应读取失败或超限")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		failure := &hashPayHTTPError{Status: res.StatusCode}
		// 只识别原生 API 的 JSON 错误码，代理的 HTML 404 不代表订单删除。
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(res.Header.Get("Content-Type"), ";", 2)[0]), "application/json") {
			var payload struct {
				Error struct {
					Key string `json:"key"`
				} `json:"error"`
			}
			if json.Unmarshal(data, &payload) == nil {
				failure.Key = payload.Error.Key
			}
		}
		return failure
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err = dec.Decode(out); err != nil {
		return errors.New("HashPay 响应格式无效")
	}
	if err = dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("HashPay 响应格式无效")
	}
	return nil
}
func (c *hashPayClient) create(ctx context.Context, o Order, returnURL string) (GatewayOrder, string, error) {
	return c.createPayment(ctx, paymentRequest{o.ID, o.ProductName, o.Currency, o.Total}, returnURL)
}

// 商品订单和赞赏仅共用网关协议，库存、优惠和交付仍归各自业务处理。
type paymentRequest struct {
	ID, Description, Currency string
	Amount                    int64
}

func (c *hashPayClient) matchesPayment(remote GatewayOrder, id, gatewayID, currency string, amount int64) bool {
	minor, err := decimalMinor(remote.Amount.String(), currency)
	return err == nil && remote.ID == gatewayID && remote.MerchantID == c.config.MerchantID && remote.MerchantNo == id && remote.Currency == currency && minor == amount
}

func (c *hashPayClient) createPayment(ctx context.Context, p paymentRequest, returnURL string) (GatewayOrder, string, error) {
	payload := map[string]any{"merchantNo": p.ID, "amount": json.Number(minorDecimal(p.Amount, p.Currency)), "currency": p.Currency, "description": p.Description, "return_url": returnURL}
	body, err := json.Marshal(payload)
	if err != nil {
		return GatewayOrder{}, "", err
	}
	var result struct {
		Order       GatewayOrder `json:"order"`
		CheckoutURL string       `json:"checkoutUrl"`
	}
	if err = c.request(ctx, "POST", "/api/merchant/new", body, &result); err != nil {
		return GatewayOrder{}, "", err
	}
	// 原生创建接口仅返回摘要；完整归属在后续签名查询中核验。
	if result.Order.ID == "" || result.Order.Currency != p.Currency ||
		(result.Order.MerchantID != "" && result.Order.MerchantID != c.config.MerchantID) ||
		(result.Order.MerchantNo != "" && result.Order.MerchantNo != p.ID) {
		return GatewayOrder{}, "", errors.New("HashPay 创建结果不匹配")
	}
	amount, err := decimalMinor(result.Order.Amount.String(), p.Currency)
	if err != nil || amount != p.Amount {
		return GatewayOrder{}, "", errors.New("HashPay 创建金额不匹配")
	}
	gateway, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return GatewayOrder{}, "", err
	}
	u, err := url.Parse(result.CheckoutURL)
	if err != nil || u.Scheme != gateway.Scheme || u.Host != gateway.Host || u.User != nil || u.Fragment != "" {
		return GatewayOrder{}, "", errors.New("HashPay 支付地址无效")
	}
	return result.Order, result.CheckoutURL, nil
}
func (c *hashPayClient) query(ctx context.Context, id string) (GatewayOrder, error) {
	var o GatewayOrder
	err := c.request(ctx, "GET", "/api/order/"+url.PathEscape(id), nil, &o)
	return o, err
}

type callbackEnvelope struct {
	Algorithm string `json:"alg"`
	Key       string `json:"key"`
	IV        string `json:"iv"`
	Data      string `json:"data"`
}
type callbackPlain struct {
	Timestamp int64 `json:"timestamp"`
	Payload   struct {
		OrderID    string `json:"orderId"`
		MerchantNo string `json:"merchantNo"`
	} `json:"payload"`
}

func (c *hashPayClient) decryptCallback(e callbackEnvelope) (callbackPlain, error) {
	var plain callbackPlain
	if e.Algorithm != "RSA-OAEP-256+A256GCM" {
		return plain, errors.New("算法无效")
	}
	keyData, err := base64.StdEncoding.DecodeString(e.Key)
	if err != nil {
		return plain, err
	}
	key, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, c.key, keyData, nil)
	if err != nil || len(key) != 32 {
		return plain, errors.New("密钥解密失败")
	}
	iv, err := base64.StdEncoding.DecodeString(e.IV)
	if err != nil || len(iv) != 12 {
		return plain, errors.New("IV 无效")
	}
	data, err := base64.StdEncoding.DecodeString(e.Data)
	if err != nil {
		return plain, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return plain, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return plain, err
	}
	decoded, err := gcm.Open(nil, iv, data, nil)
	if err != nil {
		return plain, err
	}
	err = json.Unmarshal(decoded, &plain)
	return plain, err
}
func recentTimestamp(value int64) bool {
	delta := unixNow() - value
	return delta >= -300 && delta <= 300
}
func (a *App) webhook(w http.ResponseWriter, r *http.Request) error {
	if !a.limits.allow("webhook:"+clientIP(r), 60, time.Minute) {
		return &apiError{429, "通知过于频繁"}
	}
	cfg := a.settings().HashPay
	if cfg.MerchantID == "" || r.Header.Get("X-HashPay-Merchant") != cfg.MerchantID || r.Header.Get("X-HashPay-Encryption") != "RSA-OAEP-256+A256GCM" {
		return bad("通知无效")
	}
	ts, err := strconv.ParseInt(r.Header.Get("X-HashPay-Timestamp"), 10, 64)
	if err != nil || !recentTimestamp(ts) {
		return bad("通知时间无效")
	}
	var envelope callbackEnvelope
	if err = readJSON(w, r, &envelope); err != nil {
		return err
	}
	client, err := newHashPay(cfg)
	if err != nil {
		return &apiError{503, "支付网关未就绪"}
	}
	plain, err := client.decryptCallback(envelope)
	if err != nil || !recentTimestamp(plain.Timestamp) || plain.Timestamp != ts {
		return bad("通知解密或时间校验失败")
	}
	// 优先查赞赏映射；商品订单的随机 ID 也可能碰巧带有同一前缀。
	if strings.HasPrefix(plain.Payload.MerchantNo, "don_") {
		d, err := a.loadDonation(r.Context(), plain.Payload.MerchantNo)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			if d.GatewayID == "" || d.GatewayID != plain.Payload.OrderID {
				return bad("通知订单映射不匹配")
			}
			if err = a.syncDonation(r.Context(), d.ID); err != nil {
				return &apiError{503, "付款暂未核实，请重试"}
			}
			reply(w, 200, map[string]bool{"ok": true})
			return nil
		}
	}
	o, err := a.loadOrder(r.Context(), plain.Payload.MerchantNo)
	if err != nil {
		return bad("通知订单不存在")
	}
	if o.GatewayID == "" || o.GatewayID != plain.Payload.OrderID {
		return bad("通知订单映射不匹配")
	}
	// 解密成功不是来源证明；始终以签名查询的结果核实付款。
	if err = a.syncOrder(r.Context(), o.ID); err != nil {
		return &apiError{503, "付款暂未核实，请重试"}
	}
	reply(w, 200, map[string]bool{"ok": true})
	return nil
}
