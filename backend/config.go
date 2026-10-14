package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"
)

type HashPayConfig struct {
	BaseURL        string `json:"base_url"`
	MerchantID     string `json:"merchant_id"`
	PrivateKeyFile string `json:"private_key_file"`
}
type Config struct {
	Listen         string            `json:"listen"`
	PublicURL      string            `json:"public_url"`
	Database       string            `json:"database"`
	BackupDir      string            `json:"backup_dir"`
	Title          string            `json:"title"`
	Notice         string            `json:"notice"`
	Development    bool              `json:"development"`
	AdminUsername  string            `json:"admin_username"`
	AdminPassword  string            `json:"admin_password_hash"`
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
	// 旧配置仅保存密码，升级后使用默认管理员账号。
	if c.AdminUsername == "" {
		c.AdminUsername = "admin"
	}
	c.PublicURL, err = normalizePublicURL(c.PublicURL)
	if err != nil {
		return c, nil, err
	}
	if err = validateConfig(c); err != nil {
		return c, nil, err
	}
	v, err := vaultFromConfig(c)
	return c, v, err
}

func vaultFromConfig(c Config) (*vault, error) {
	v := &vault{current: c.CurrentKey, keys: map[string][]byte{}}
	for version, value := range c.EncryptionKeys {
		if version == "" || strings.Contains(version, ":") {
			return nil, errors.New("密钥版本无效")
		}
		b, e := base64.StdEncoding.DecodeString(value)
		if e != nil || len(b) != 32 {
			return nil, errors.New("加密密钥必须为 32 字节 Base64")
		}
		v.keys[version] = b
	}
	if len(v.keys[v.current]) != 32 {
		return nil, errors.New("当前加密密钥不存在")
	}
	var err error
	v.dedup, err = base64.StdEncoding.DecodeString(c.DedupKey)
	if err != nil || len(v.dedup) != 32 {
		return nil, errors.New("去重密钥无效")
	}
	return v, nil
}
func validateConfig(c Config) error {
	if c.Database == "" || c.BackupDir == "" {
		return errors.New("数据库与备份目录不能为空")
	}
	if err := validateListen(c.Listen); err != nil {
		return err
	}
	if err := validatePublicURL(c.PublicURL); err != nil {
		return err
	}
	if c.Title == "" || len(c.Title) > 120 || len(c.Notice) > 2000 {
		return errors.New("站点名称或公告长度无效")
	}
	if strings.TrimSpace(c.AdminUsername) == "" || utf8.RuneCountInString(c.AdminUsername) > 120 {
		return errors.New("管理员账号不能为空或超过 120 字符")
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
	if c.HashPay.BaseURL != "" {
		if err := validGatewayURL(c.HashPay.BaseURL); err != nil {
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

func validateListen(value string) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil || (host != "127.0.0.1" && host != "0.0.0.0") {
		return errors.New("监听地址须为 127.0.0.1 或 0.0.0.0 的有效端口")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("监听端口须为 1–65535 的整数")
	}
	if runtime.GOOS == "windows" && host != "127.0.0.1" {
		return errors.New("Windows 本地服务只允许监听 127.0.0.1")
	}
	return nil
}
func validatePublicURL(value string) error {
	_, err := normalizePublicURL(value)
	return err
}
func normalizePublicURL(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("站点地址必须为无路径的完整 HTTP 或 HTTPS 地址")
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	for _, ch := range host {
		if ch > 127 {
			return "", errors.New("站点地址无效，请复制浏览器中的完整地址")
		}
	}
	if host == "" {
		return "", errors.New("站点地址缺少主机名")
	}
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", errors.New("站点端口无效")
		}
		port = strconv.Itoa(n)
	}
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u.Host, u.Path, u.RawPath, u.ForceQuery = host, "", "", false
	return u.String(), nil
}
func secureCookie(c Config) bool { return strings.HasPrefix(c.PublicURL, "https://") }
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
