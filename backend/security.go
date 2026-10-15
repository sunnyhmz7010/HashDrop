package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"strconv"
	"strings"
)

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func newID() string { return randomToken()[:22] }
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func constantEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

type vault struct {
	current string
	keys    map[string][]byte
	dedup   []byte
}

func (v *vault) encrypt(plain, aad string) (string, error) {
	block, err := aes.NewCipher(v.keys[v.current])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), []byte(aad))
	return v.current + ":" + base64.RawStdEncoding.EncodeToString(sealed), nil
}
func (v *vault) decrypt(value, aad string) (string, error) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return "", errors.New("密文格式无效")
	}
	key, ok := v.keys[parts[0]]
	if !ok {
		return "", errors.New("密钥版本缺失")
	}
	data, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("密文长度无效")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(aad))
	return string(plain), err
}
func (v *vault) fingerprint(inventory, content string) string {
	h := hmac.New(sha256.New, v.dedup)
	h.Write([]byte(inventory + "\x00" + content))
	return hex.EncodeToString(h.Sum(nil))
}
func passwordHash(password string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))
}
func passwordMatches(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=2" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// 首版支持固定法币精度；不通过 float64 转换订单金额。
func currencyScale(currency string) (int, error) {
	switch currency {
	case "CNY", "USD", "EUR", "GBP", "HKD":
		return 2, nil
	case "JPY":
		return 0, nil
	case "USDT", "USDC":
		return 6, nil
	}
	return 0, errors.New("不支持的币种")
}
func decimalMinor(value, currency string) (int64, error) {
	scale, err := currencyScale(currency)
	if err != nil {
		return 0, err
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts[0]) == 0 {
		return 0, errors.New("金额格式无效")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > scale {
		if strings.Trim(fraction[scale:], "0") != "" {
			return 0, errors.New("金额精度超限")
		}
		fraction = fraction[:scale]
	}
	digits := parts[0] + fraction + strings.Repeat("0", scale-len(fraction))
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, errors.New("金额格式无效")
		}
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n <= 0 || n > 9_000_000_000_000 {
		return 0, errors.New("金额超限")
	}
	return n, nil
}
func minorDecimal(value int64, currency string) string {
	scale, _ := currencyScale(currency)
	s := strconv.FormatInt(value, 10)
	if scale == 0 {
		return s
	}
	if len(s) <= scale {
		s = strings.Repeat("0", scale-len(s)+1) + s
	}
	return s[:len(s)-scale] + "." + s[len(s)-scale:]
}
