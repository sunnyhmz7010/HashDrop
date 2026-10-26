package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hashdrop"
	"io"
	"log/slog"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const databaseVersion = 7

func openDatabase(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uri := databaseURI(absolute)
	db, err := sql.Open("sqlite", uri+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	// 所有事务串行进入 SQLite；网络请求绝不占用数据库事务。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if err = os.Chmod(absolute, 0600); err != nil {
		db.Close()
		return nil, err
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version != 0 && version != databaseVersion {
		db.Close()
		return nil, fmt.Errorf("数据库结构版本 %d 不受支持", version)
	}
	if version == 0 {
		if err = initializeDatabase(db); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func initializeDatabase(db *sql.DB) error {
	schema, err := hashdrop.Assets.ReadFile("schema.sql")
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(schema)); err != nil {
		return err
	}
	return tx.Commit()
}

func backupDatabase(ctx context.Context, db *sql.DB, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		return fmt.Errorf("备份路径必须不存在")
	}
	// VACUUM INTO 创建一致的在线快照，不复制运行中的 WAL 文件。
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", target); err != nil {
		return err
	}
	if err := os.Chmod(target, 0600); err != nil {
		return err
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	check, err := sql.Open("sqlite", databaseURI(absolute)+"?mode=ro")
	if err != nil {
		return err
	}
	defer check.Close()
	var result string
	if err = check.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("备份完整性校验失败")
	}
	rows, err := check.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("备份外键校验失败")
	}
	return rows.Err()
}

type backupAsset struct {
	ID     string `json:"id"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// 输出目录包含数据库和其引用的不可变附件；只有全部验证完成才发布最终目录。
// config.json 中的密钥仍须与备份配套保存，备份包不额外复制商户私钥。
func (a *App) backupData(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	absolute, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if _, err = os.Stat(absolute); !os.IsNotExist(err) {
		return fmt.Errorf("备份目录必须不存在")
	}
	parent := filepath.Dir(absolute)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".hashdrop-backup-")
	if err != nil {
		return err
	}
	defer func() {
		// stage 仅来自本次 MkdirTemp，绝不清理调用者传入的既有目录。
		if err := os.RemoveAll(stage); err != nil {
			slog.Warn("备份临时目录清理失败")
		}
	}()
	assetFilesMu.RLock()
	defer assetFilesMu.RUnlock()
	database := filepath.Join(stage, "hashdrop.db")
	if err = backupDatabase(ctx, a.db, database); err != nil {
		return err
	}
	snapshot, err := sql.Open("sqlite", databaseURI(database)+"?_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	defer snapshot.Close()
	// 未保存的上传不属于业务快照，恢复时无需保留这类临时资源。
	if _, err = snapshot.ExecContext(ctx, "DELETE FROM assets WHERE NOT EXISTS(SELECT 1 FROM asset_refs WHERE asset_id=assets.id)"); err != nil {
		return err
	}
	rows, err := snapshot.QueryContext(ctx, "SELECT id,size FROM assets ORDER BY id")
	if err != nil {
		return err
	}
	assets := []backupAsset{}
	for rows.Next() {
		var item backupAsset
		if err = rows.Scan(&item.ID, &item.Size); err != nil {
			rows.Close()
			return err
		}
		assets = append(assets, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	destination := filepath.Join(stage, "assets")
	if err = os.Mkdir(destination, 0700); err != nil {
		return err
	}
	for index := range assets {
		item := &assets[index]
		if err = ctx.Err(); err != nil {
			return err
		}
		sourcePath, err := assetFile(assetDirectory(a.settings().Database), item.ID)
		if err != nil {
			return err
		}
		destinationPath, err := assetFile(destination, item.ID)
		if err != nil {
			return err
		}
		item.SHA256, err = copyBackupAsset(ctx, sourcePath, destinationPath, a.vault, item.ID, item.Size)
		if err != nil {
			return fmt.Errorf("附件 %s 备份失败: %w", item.ID, err)
		}
	}
	var integrity string
	if err = snapshot.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		if err != nil {
			return err
		}
		return fmt.Errorf("备份数据库完整性校验失败")
	}
	if _, err = snapshot.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return err
	}
	if err = snapshot.Close(); err != nil {
		return err
	}
	databaseHash, err := backupFileHash(database)
	if err != nil {
		return err
	}
	manifest := struct {
		Version        int           `json:"version"`
		CreatedAt      int64         `json:"created_at"`
		DatabaseSHA256 string        `json:"database_sha256"`
		Assets         []backupAsset `json:"assets"`
	}{Version: databaseVersion, CreatedAt: unixNow(), DatabaseSHA256: databaseHash, Assets: assets}
	document, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(stage, "manifest.json"), append(document, '\n'), 0600); err != nil {
		return err
	}
	if _, err = os.Stat(absolute); !os.IsNotExist(err) {
		return fmt.Errorf("备份目录必须不存在")
	}
	return os.Rename(stage, absolute)
}

func copyBackupAsset(ctx context.Context, source, target string, v *vault, id string, size int64) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer output.Close()
	hash := sha256.New()
	if _, err = io.Copy(io.MultiWriter(output, hash), input); err != nil {
		return "", err
	}
	if err = output.Sync(); err != nil {
		return "", err
	}
	if err = output.Close(); err != nil {
		return "", err
	}
	actualHash, err := backupFileHash(target)
	if err != nil {
		return "", err
	}
	if actualHash != hex.EncodeToString(hash.Sum(nil)) {
		return "", fmt.Errorf("附件复制校验失败")
	}
	check, err := os.Open(target)
	if err != nil {
		return "", err
	}
	defer check.Close()
	if err = readEncryptedAsset(ctx, v, id, size, io.Discard, check); err != nil {
		return "", err
	}
	return actualHash, nil
}

func backupFileHash(file string) (string, error) {
	input, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer input.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, input); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func unixNow() int64 { return time.Now().Unix() }

func databaseURI(absolute string) string {
	path := filepath.ToSlash(absolute)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
