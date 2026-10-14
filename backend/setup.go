package main

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

func bootstrapConfig(dataDir, listen string) (Config, error) {
	if listen == "" {
		listen = "127.0.0.1:8080"
	}
	if err := validateListen(listen); err != nil {
		return Config{}, err
	}
	absolute, err := filepath.Abs(dataDir)
	if err != nil {
		return Config{}, err
	}
	return Config{Listen: listen, Database: filepath.Join(absolute, "tinyfaka.db"), BackupDir: filepath.Join(absolute, "backups"), Title: "TinyFaka"}, nil
}

type setupInput struct {
	Title           string `json:"title"`
	PublicURL       string `json:"public_url"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"password_confirm"`
	HashPayURL      string `json:"hashpay_url"`
	MerchantID      string `json:"merchant_id"`
	PrivateKey      string `json:"private_key"`
}

func (a *App) setupStatus(w http.ResponseWriter, r *http.Request) error {
	reply(w, 200, map[string]bool{"initialized": a.initialized.Load()})
	return nil
}
func setupOrigin(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "" && strings.EqualFold(u.Host, r.Host)
}
func (a *App) setup(w http.ResponseWriter, r *http.Request) error {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	if a.initialized.Load() {
		return &apiError{409, "系统已初始化，不能重复设置"}
	}
	if !a.limits.allow("setup:"+clientIP(r), 20, time.Minute) {
		return &apiError{429, "初始化尝试过于频繁"}
	}
	if !setupOrigin(r) {
		return &apiError{403, "初始化请求来源无效"}
	}
	var input setupInput
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	if input.Password == "" || utf8.RuneCountInString(input.Password) > 256 || input.Password != input.PasswordConfirm {
		return bad("管理员密码不能为空或超过 256 字符，且两次输入须一致")
	}
	c := a.settings()
	c.Title = strings.TrimSpace(input.Title)
	c.AdminUsername = strings.TrimSpace(input.Username)
	publicURL, err := normalizePublicURL(strings.TrimSpace(input.PublicURL))
	if err != nil {
		return bad(err.Error())
	}
	c.PublicURL = publicURL
	c.AdminPassword = passwordHash(input.Password)
	c.CurrentKey = "v1"
	key, _ := base64.RawURLEncoding.DecodeString(randomToken())
	dedup, _ := base64.RawURLEncoding.DecodeString(randomToken())
	c.EncryptionKeys = map[string]string{"v1": base64.StdEncoding.EncodeToString(key)}
	c.DedupKey = base64.StdEncoding.EncodeToString(dedup)
	c.HashPay = HashPayConfig{BaseURL: strings.TrimRight(strings.TrimSpace(input.HashPayURL), "/"), MerchantID: strings.TrimSpace(input.MerchantID)}
	if err := validateConfig(c); err != nil {
		return bad(err.Error())
	}
	if c.HashPay.BaseURL != "" || c.HashPay.MerchantID != "" || input.PrivateKey != "" {
		if c.HashPay.BaseURL == "" || c.HashPay.MerchantID == "" || input.PrivateKey == "" {
			return bad("HashPay 地址、商户 ID 和私钥须完整填写，或全部留空稍后配置")
		}
		path, err := savePrivateKey(a.configPath, input.PrivateKey)
		if err != nil {
			return err
		}
		c.HashPay.PrivateKeyFile = path
	}
	v, err := vaultFromConfig(c)
	if err != nil {
		return err
	}
	// 缺失配置时不能拿新密钥覆盖旧数据库；初始化仅创建全新的空库。
	if _, err = os.Stat(c.Database); !os.IsNotExist(err) {
		return &apiError{409, "数据库已存在，请恢复原配置，不要重新初始化"}
	}
	db, err := openDatabase(c.Database)
	if err != nil {
		return err
	}
	if err = writeConfig(a.configPath, c); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			slog.Error("初始化数据库关闭失败", "error", closeErr)
		}
		for _, path := range []string{c.Database, c.Database + "-wal", c.Database + "-shm"} {
			if e := os.Remove(path); e != nil && !os.IsNotExist(e) {
				slog.Error("初始化空库清理失败", "error", e)
			}
		}
		return err
	}
	a.configMu.Lock()
	a.config = c
	a.configMu.Unlock()
	a.db = db
	a.vault = v
	// 原子发布初始化结果，业务路由和调度器在此之后才可访问 db/vault。
	a.initialized.Store(true)
	audit("system_initialized", "")
	reply(w, 201, map[string]any{"initialized": true, "public_url": c.PublicURL, "title": c.Title})
	return nil
}

func savePrivateKey(configPath, value string) (string, error) {
	if len(value) > 65536 {
		return "", bad("商户私钥长度超限")
	}
	key, err := parsePrivateKey([]byte(value))
	if err != nil {
		return "", bad(err.Error())
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", errors.New("商户私钥编码失败")
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", errors.New("商户公钥编码失败")
	}
	fingerprint := sha256.Sum256(public)
	directory, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, "hashpay-"+hex.EncodeToString(fingerprint[:12])+".pem")
	file, err := os.CreateTemp(directory, ".hashpay-key-*")
	if err != nil {
		return "", err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}))
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err = os.Rename(temporary, path); err != nil {
		return "", err
	}
	return path, nil
}
