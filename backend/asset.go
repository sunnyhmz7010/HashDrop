package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxAssetSize int64 = 100 * 1024 * 1024
const assetChunkSize = 64 * 1024
const assetTransferTimeout = 15 * time.Minute

// 文件不可原地修改；引用变更由数据库事务提交，文件清理与备份互斥。
var assetFilesMu sync.RWMutex

type Asset struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
}

func assetDirectory(database string) string { return filepath.Join(filepath.Dir(database), "assets") }

func assetFile(directory, id string) (string, error) {
	if len(id) != 22 || !validRequestID(id) {
		return "", bad("附件标识无效")
	}
	return filepath.Join(directory, id+".asset"), nil
}

func safeAssetName(name string) (string, error) {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == ".." || name == "/" || strings.TrimSpace(name) == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 255 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", bad("附件名称无效")
	}
	return name, nil
}

func safeInlineImage(mediaType string) bool {
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

func assetCipher(v *vault, version, id string) (cipher.AEAD, error) {
	key, exists := v.keys[version]
	if !exists {
		return nil, errors.New("附件加密密钥版本缺失")
	}
	// 每个随机资源 ID 派生独立文件密钥，避免不同文件的分块 nonce 共用密钥。
	derive := hmac.New(sha256.New, key)
	derive.Write([]byte("hashdrop-asset-v1\x00" + id))
	block, err := aes.NewCipher(derive.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// 每块独立认证，并认证块序号、长度、文件身份和头部；空终止块防止截断。
// 上传和下载均只保留一块明文，不因大文件占用同等大小的内存。
func writeEncryptedAsset(ctx context.Context, v *vault, id string, dst io.Writer, src io.Reader) (int64, error) {
	if len(v.current) == 0 || len(v.current) > 1024 {
		return 0, errors.New("附件加密密钥版本无效")
	}
	aead, err := assetCipher(v, v.current, id)
	if err != nil {
		return 0, err
	}
	header := make([]byte, 6+len(v.current)+8)
	copy(header, "HDA1")
	binary.BigEndian.PutUint16(header[4:6], uint16(len(v.current)))
	copy(header[6:], v.current)
	if _, err = rand.Read(header[len(header)-8:]); err != nil {
		return 0, err
	}
	if _, err = dst.Write(header); err != nil {
		return 0, err
	}
	nonce := make([]byte, aead.NonceSize())
	copy(nonce, header[len(header)-8:])
	baseAAD := append(append([]byte{}, header...), []byte(id)...)
	chunk := make([]byte, assetChunkSize)
	var total int64
	for sequence := uint32(0); ; sequence++ {
		if err = ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := io.ReadFull(src, chunk)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return total, readErr
		}
		total += int64(n)
		if total > maxAssetSize {
			return total, &apiError{413, "单个附件不能超过 100 MiB"}
		}
		binary.BigEndian.PutUint32(nonce[len(nonce)-4:], sequence)
		var framing [8]byte
		binary.BigEndian.PutUint32(framing[:4], sequence)
		binary.BigEndian.PutUint32(framing[4:], uint32(n))
		aad := append(append([]byte{}, baseAAD...), framing[:]...)
		sealed := aead.Seal(nil, nonce, chunk[:n], aad)
		if _, err = dst.Write(framing[4:]); err != nil {
			return total, err
		}
		if _, err = dst.Write(sealed); err != nil {
			return total, err
		}
		if n == 0 {
			return total, nil
		}
	}
}

func readEncryptedAsset(ctx context.Context, v *vault, id string, expected int64, dst io.Writer, src io.Reader) error {
	var prefix [6]byte
	if _, err := io.ReadFull(src, prefix[:]); err != nil {
		return err
	}
	keyLength := int(binary.BigEndian.Uint16(prefix[4:]))
	if string(prefix[:4]) != "HDA1" || keyLength < 1 || keyLength > 1024 || expected < 1 || expected > maxAssetSize {
		return errors.New("附件加密头无效")
	}
	header := make([]byte, 6+keyLength+8)
	copy(header, prefix[:])
	if _, err := io.ReadFull(src, header[6:]); err != nil {
		return err
	}
	aead, err := assetCipher(v, string(header[6:6+keyLength]), id)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	copy(nonce, header[len(header)-8:])
	baseAAD := append(append([]byte{}, header...), []byte(id)...)
	var total int64
	for sequence := uint32(0); ; sequence++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		var framing [8]byte
		binary.BigEndian.PutUint32(framing[:4], sequence)
		if _, err = io.ReadFull(src, framing[4:]); err != nil {
			return err
		}
		size := int(binary.BigEndian.Uint32(framing[4:]))
		if size > assetChunkSize || total+int64(size) > expected {
			return errors.New("附件分块长度无效")
		}
		sealed := make([]byte, size+aead.Overhead())
		if _, err = io.ReadFull(src, sealed); err != nil {
			return err
		}
		binary.BigEndian.PutUint32(nonce[len(nonce)-4:], sequence)
		aad := append(append([]byte{}, baseAAD...), framing[:]...)
		plain, err := aead.Open(nil, nonce, sealed, aad)
		if err != nil {
			return err
		}
		if size == 0 {
			var trailing [1]byte
			n, finalErr := src.Read(trailing[:])
			if total != expected || n != 0 || finalErr != io.EOF {
				return errors.New("附件长度或终止记录无效")
			}
			return nil
		}
		if _, err = dst.Write(plain); err != nil {
			return err
		}
		total += int64(size)
	}
}

func (a *App) uploadAsset(w http.ResponseWriter, r *http.Request) error {
	// 此入口由管理员中间件保护；只为已授权附件传输延长连接期限。
	if err := assetTransferDeadlines(w, true); err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAssetSize+64*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		return bad("请使用 multipart/form-data 上传附件")
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		return bad("请选择一个附件上传")
	}
	defer part.Close()
	name, err := safeAssetName(part.FileName())
	if err != nil {
		return err
	}
	var prefix [512]byte
	n, err := io.ReadFull(part, prefix[:])
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return uploadReadError(err)
	}
	if n == 0 {
		return bad("不能上传空附件")
	}
	asset := Asset{ID: newID(), Name: name, MediaType: http.DetectContentType(prefix[:n])}
	directory := assetDirectory(a.settings().Database)
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".upload-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		temporary.Close()
		if err := os.Remove(temporaryName); err != nil && !os.IsNotExist(err) {
			slog.Warn("附件临时文件清理失败", "asset_id", asset.ID)
		}
	}()
	asset.Size, err = writeEncryptedAsset(r.Context(), a.vault, asset.ID, temporary, io.MultiReader(bytes.NewReader(prefix[:n]), part))
	if err != nil {
		return uploadReadError(err)
	}
	if _, err = reader.NextPart(); err != io.EOF {
		if err != nil {
			return uploadReadError(err)
		}
		return bad("每次只能上传一个附件")
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	finalName, err := assetFile(directory, asset.ID)
	if err != nil {
		return err
	}
	assetFilesMu.RLock()
	defer assetFilesMu.RUnlock()
	if err = os.Rename(temporaryName, finalName); err != nil {
		return err
	}
	now := unixNow()
	if _, err = a.db.ExecContext(r.Context(), "INSERT INTO assets(id,name,media_type,size,created_at,unreferenced_at) VALUES(?,?,?,?,?,?)", asset.ID, asset.Name, asset.MediaType, asset.Size, now, now); err != nil {
		if cleanupErr := os.Remove(finalName); cleanupErr != nil {
			slog.Warn("未登记附件清理失败", "asset_id", asset.ID)
		}
		return err
	}
	audit("asset_uploaded", asset.ID)
	reply(w, http.StatusCreated, asset)
	return nil
}

func uploadReadError(err error) error {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return &apiError{413, "单个附件不能超过 100 MiB"}
	}
	return err
}

func assetTransferDeadlines(w http.ResponseWriter, upload bool) error {
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(assetTransferTimeout)
	// 无连接的响应记录器可能不支持 deadline；真实 net/http 响应支持它。
	// 其余错误不能降级继续传输，否则仍会被普通接口的短超时截断。
	if upload {
		if err := controller.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return fmt.Errorf("设置附件读取期限: %w", err)
		}
	}
	if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("设置附件响应期限: %w", err)
	}
	return nil
}

func (a *App) replaceAssetRefs(ctx context.Context, tx *sql.Tx, ownerType, ownerID string, doc ContentDocument) error {
	if ownerType != "product" && ownerType != "stock" && ownerType != "delivery" {
		return errors.New("附件引用归属无效")
	}
	for _, block := range doc.Blocks {
		if block.Type != "image" && block.Type != "file" {
			continue
		}
		var mediaType string
		if err := tx.QueryRowContext(ctx, "SELECT media_type FROM assets WHERE id=?", block.AssetID).Scan(&mediaType); err == sql.ErrNoRows {
			return bad("附件不存在或已清理，请重新上传")
		} else if err != nil {
			return err
		}
		if block.Type == "image" && !safeInlineImage(mediaType) {
			return bad("图片仅支持 PNG、JPEG、GIF 和 WebP")
		}
		file, err := assetFile(assetDirectory(a.settings().Database), block.AssetID)
		if err != nil {
			return err
		}
		if info, err := os.Stat(file); err != nil || !info.Mode().IsRegular() {
			return bad("附件文件不可用，请重新上传")
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM asset_refs WHERE owner_type=? AND owner_id=?", ownerType, ownerID); err != nil {
		return err
	}
	for _, id := range contentAssetIDs(doc) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO asset_refs(owner_type,owner_id,asset_id) VALUES(?,?,?)", ownerType, ownerID, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE assets SET unreferenced_at=NULL WHERE id=?", id); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) adminAsset(w http.ResponseWriter, r *http.Request) error {
	return a.serveAsset(w, r, r.PathValue("asset"))
}

func (a *App) deliveryAsset(w http.ResponseWriter, r *http.Request) error {
	_, delivery, err := a.authorizeDeliveryAccess(r)
	if err != nil {
		return err
	}
	allowed := false
	if delivery.Content != nil {
		for _, id := range contentAssetIDs(*delivery.Content) {
			if id == r.PathValue("asset") {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		return &apiError{404, "附件不存在或无权访问"}
	}
	return a.serveAsset(w, r, r.PathValue("asset"))
}

func (a *App) serveAsset(w http.ResponseWriter, r *http.Request, id string) error {
	if err := assetTransferDeadlines(w, false); err != nil {
		return err
	}
	file, err := assetFile(assetDirectory(a.settings().Database), id)
	if err != nil {
		return &apiError{404, "附件不存在或无权访问"}
	}
	assetFilesMu.RLock()
	defer assetFilesMu.RUnlock()
	var asset Asset
	if err = a.db.QueryRowContext(r.Context(), "SELECT id,name,media_type,size FROM assets WHERE id=?", id).Scan(&asset.ID, &asset.Name, &asset.MediaType, &asset.Size); err == sql.ErrNoRows {
		return &apiError{404, "附件不存在或无权访问"}
	} else if err != nil {
		return err
	}
	input, err := os.Open(file)
	if err != nil {
		return &apiError{503, "附件暂时不可用，请联系商家"}
	}
	defer input.Close()
	// 先验证全部认证块再发送 HTTP 正文，损坏文件不能被误报为成功下载。
	if err = readEncryptedAsset(r.Context(), a.vault, id, asset.Size, io.Discard, input); err != nil {
		return &apiError{503, "附件完整性校验失败，请联系商家"}
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return err
	}
	disposition, mediaType := "attachment", "application/octet-stream"
	if safeInlineImage(asset.MediaType) {
		disposition, mediaType = "inline", asset.MediaType
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": asset.Name}))
	w.Header().Set("Content-Length", fmt.Sprint(asset.Size))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if err = readEncryptedAsset(r.Context(), a.vault, id, asset.Size, w, input); err != nil {
		// 响应已经开始，不能再追加 JSON；长度不完整会使客户端拒绝本次下载。
		slog.Warn("附件响应中断", "asset_id", id)
	}
	return nil
}

func (a *App) cleanupAssets(ctx context.Context) error {
	assetFilesMu.Lock()
	defer assetFilesMu.Unlock()
	now := unixNow()
	// 引用刚解除的文件先进入保留期，不能按最初上传时间立即删除。
	if _, err := a.db.ExecContext(ctx, "UPDATE assets SET unreferenced_at=? WHERE unreferenced_at IS NULL AND NOT EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=assets.id)", now); err != nil {
		return err
	}
	rows, err := a.db.QueryContext(ctx, "SELECT id FROM assets WHERE unreferenced_at<=? AND NOT EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=assets.id)", now-int64((24*time.Hour)/time.Second))
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	directory := assetDirectory(a.settings().Database)
	for _, id := range ids {
		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		// 删除条件在写事务内复核，避免候选查询后新绑定的附件被清理。
		result, err := tx.ExecContext(ctx, "DELETE FROM assets WHERE id=? AND unreferenced_at<=? AND NOT EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=assets.id)", id, now-int64((24*time.Hour)/time.Second))
		if err != nil {
			tx.Rollback()
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		if count == 1 {
			file, err := assetFile(directory, id)
			if err != nil {
				return err
			}
			if err = os.Remove(file); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	// 回收进程在文件落盘、元数据提交之间退出留下的文件。
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if now-info.ModTime().Unix() < int64((24*time.Hour)/time.Second) {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".upload-") {
			if err = os.Remove(filepath.Join(directory, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		id, ok := strings.CutSuffix(name, ".asset")
		if !ok || len(id) != 22 || !validRequestID(id) {
			continue
		}
		var exists bool
		if err = a.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM assets WHERE id=?)", id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if err = os.Remove(filepath.Join(directory, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
