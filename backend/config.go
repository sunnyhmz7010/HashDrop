package main

import (
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type HashPayConfig struct {
	BaseURL        string `json:"base_url"`
	MerchantID     string `json:"merchant_id"`
	PrivateKeyFile string `json:"private_key_file"`
	ManualPolicy   string `json:"manual_policy"`
}
type Config struct {
	Listen         string            `json:"listen"`
	PublicURL      string            `json:"public_url"`
	Database       string            `json:"database"`
	BackupDir      string            `json:"backup_dir"`
	Title          string            `json:"title"`
	Notice         string            `json:"notice"`
	Development    bool              `json:"development"`
	AdminPassword  string            `json:"admin_password_hash"`
	TOTPSecret     string            `json:"totp_secret,omitempty"`
	CurrentKey     string            `json:"current_key"`
	EncryptionKeys map[string]string `json:"encryption_keys"`
	DedupKey       string            `json:"dedup_key"`
	HashPay        HashPayConfig     `json:"hashpay"`
}

func readConfig(path string) (Config, *vault, error) {
	var c Config
	info, err := os.Stat(path)
	if err != nil {
		return c, nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return c, nil, errors.New("配置文件权限必须为 0600")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c, nil, err
	}
	if err = json.Unmarshal(data, &c); err != nil {
		return c, nil, err
	}
	if err = validateConfig(c); err != nil {
		return c, nil, err
	}
	v := &vault{current: c.CurrentKey, keys: map[string][]byte{}}
	for version, value := range c.EncryptionKeys {
		if version == "" || strings.Contains(version, ":") {
			return c, nil, errors.New("密钥版本无效")
		}
		b, e := base64.StdEncoding.DecodeString(value)
		if e != nil || len(b) != 32 {
			return c, nil, errors.New("加密密钥必须为 32 字节 Base64")
		}
		v.keys[version] = b
	}
	if len(v.keys[v.current]) != 32 {
		return c, nil, errors.New("当前加密密钥不存在")
	}
	v.dedup, err = base64.StdEncoding.DecodeString(c.DedupKey)
	if err != nil || len(v.dedup) != 32 {
		return c, nil, errors.New("去重密钥无效")
	}
	return c, v, nil
}
func validateConfig(c Config) error {
	if c.Database == "" || c.BackupDir == "" {
		return errors.New("数据库与备份目录不能为空")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil || host != "127.0.0.1" {
		return errors.New("服务只允许监听 127.0.0.1")
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return errors.New("站点地址必须是无路径的完整地址")
	}
	if c.Development {
		if u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
			return errors.New("开发模式只允许 HTTP 回环站点")
		}
	} else if u.Scheme != "https" {
		return errors.New("生产站点必须使用 HTTPS")
	}
	if c.Title == "" || len(c.Title) > 120 || len(c.Notice) > 2000 {
		return errors.New("站点名称或公告长度无效")
	}
	parts := strings.Split(c.AdminPassword, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=2" {
		return errors.New("管理员密码哈希无效")
	}
	salt, saltErr := base64.RawStdEncoding.DecodeString(parts[4])
	hash, hashErr := base64.RawStdEncoding.DecodeString(parts[5])
	if saltErr != nil || hashErr != nil || len(salt) != 16 || len(hash) != 32 {
		return errors.New("管理员密码哈希无效")
	}
	if c.TOTPSecret != "" {
		key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(c.TOTPSecret)))
		if err != nil || len(key) < 16 {
			return errors.New("TOTP 密钥必须为至少 16 字节的 Base32")
		}
	}
	if c.HashPay.ManualPolicy != "hold" && c.HashPay.ManualPolicy != "accept" {
		return errors.New("人工付款策略必须为 hold 或 accept")
	}
	if c.HashPay.BaseURL != "" {
		if err = validGatewayURL(c.HashPay.BaseURL); err != nil {
			return err
		}
	}
	return nil
}
func validGatewayURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("HashPay 地址必须为无路径的 HTTPS 地址")
	}
	return nil
}
func writeConfig(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tinyfaka-config-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
