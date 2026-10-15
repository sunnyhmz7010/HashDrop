package main

import (
	"context"
	"database/sql"
	"fmt"
	"hashdrop"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
	if version != 0 && version != 1 {
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

func unixNow() int64 { return time.Now().Unix() }

func databaseURI(absolute string) string {
	path := filepath.ToSlash(absolute)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
