package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

var errClaimKeyUsed = errors.New("领取密钥已被使用")

func validateClaimKey(key string) error {
	// 不改变大小写或前后空格，避免查询时出现与买家保存内容不同的凭证。
	if strings.TrimSpace(key) == "" || !utf8.ValidString(key) || utf8.RuneCountInString(key) > 128 {
		return bad("领取密钥须为 1–128 个字符，不能全部为空白")
	}
	return nil
}

func claimKeyFromAuthorization(r *http.Request) (string, bool) {
	authorization := r.Header.Get("Authorization")
	if encoded, ok := strings.CutPrefix(authorization, "ClaimKey "); ok {
		// 编码保留 UTF-8、百分号和首尾空格，Header 只传 ASCII。
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return "", false
		}
		key := string(decoded)
		return key, validateClaimKey(key) == nil
	}
	return "", false
}

func validRequestID(id string) bool {
	if len(id) < 22 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (a *App) suggestClaimKeys(ctx context.Context, key string) ([]string, error) {
	prefix := []rune(key)
	if len(prefix) > 119 {
		prefix = prefix[:119]
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	suggestions := []string{}
	for tries := 0; len(suggestions) < 3 && tries < 12; tries++ {
		suffix := make([]byte, 8)
		for i := range suffix {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if err != nil {
				return nil, err
			}
			suffix[i] = alphabet[n.Int64()]
		}
		candidate := string(prefix) + "-" + string(suffix)
		var used bool
		if err := a.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM orders WHERE token_hash=?)", tokenHash(candidate)).Scan(&used); err != nil {
			return nil, err
		}
		if !used {
			suggestions = append(suggestions, candidate)
		}
	}
	return suggestions, nil
}

func (a *App) beginClaim(r *http.Request) (func(), bool) {
	key := "claim-failed:" + clientIP(r)
	until, allowed := a.limits.reserve(key, 10, time.Minute)
	// 查询前原子占用预算，验证成功再归还，防止排队中的猜测穿透失败上限。
	return func() { a.limits.refund(key, until) }, allowed
}

func (a *App) lookupOrder(w http.ResponseWriter, r *http.Request) error {
	refund, allowed := a.beginClaim(r)
	if !allowed {
		return &apiError{429, "密钥尝试过于频繁，请稍后再试"}
	}
	var input struct {
		Key string `json:"key"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return err
	}
	if err := validateClaimKey(input.Key); err != nil {
		return err
	}
	o, err := scanOrder(a.db.QueryRowContext(r.Context(), orderSelect+" WHERE token_hash=?", tokenHash(input.Key)))
	if err == sql.ErrNoRows {
		return &apiError{404, "订单或凭证无效"}
	}
	if err != nil {
		refund()
		return err
	}
	refund()
	return a.replyOrder(w, r, o)
}
