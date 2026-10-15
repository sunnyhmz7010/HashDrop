package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestDatabaseInitializesAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "全新安装", "tinyfaka.db")
	db, err := openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := readDonationSettings(db.QueryRow("SELECT document FROM donation_settings WHERE id=1"))
	if err != nil || settings.Enabled || settings.Currency != "CNY" {
		t.Fatalf("全新安装的赞赏设置无效: %+v %v", settings, err)
	}
	if _, err = db.Exec("INSERT INTO admin_sessions(token_hash,expires_at,created_at) VALUES('saved-session',100,1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE donation_settings SET document=json_set(document,'$.title','已保存的标题') WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, foreignKeys, sessions int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatal("数据库初始化版本无效", err)
	}
	if err = db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatal("重新打开数据库后未启用外键", err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM admin_sessions WHERE token_hash='saved-session'").Scan(&sessions); err != nil || sessions != 1 {
		t.Fatal("重新打开数据库丢失已有记录", err)
	}
	settings, err = readDonationSettings(db.QueryRow("SELECT document FROM donation_settings WHERE id=1"))
	if err != nil || settings.Title != "已保存的标题" {
		t.Fatal("重新打开数据库重置了已保存的设置", err)
	}
	if _, err = db.Exec("INSERT INTO products(id,name,price,currency,inventory_id,created_at,updated_at) VALUES('invalid','无效关联',100,'CNY','missing',1,1)"); err == nil {
		t.Fatal("数据库接受了不存在的库存关联")
	}
}

func TestDatabaseInitializationRollsBackOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conflict.db")
	db, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// 在初始化中途制造表名冲突，确认前面的建表操作随整个事务回滚。
	if _, err = db.Exec("CREATE TABLE products(marker TEXT); INSERT INTO products VALUES('原记录')"); err != nil {
		t.Fatal(err)
	}
	if opened, err := openDatabase(path); err == nil {
		opened.Close()
		t.Fatal("初始化冲突被忽略")
	}
	var count, version int
	if err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='inventories'").Scan(&count); err != nil || count != 0 {
		t.Fatal("初始化失败留下了部分结构", err)
	}
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 0 {
		t.Fatal("初始化失败仍标记为完成", err)
	}
	var marker string
	if err = db.QueryRow("SELECT marker FROM products").Scan(&marker); err != nil || marker != "原记录" {
		t.Fatal("初始化失败改动了已有记录", err)
	}
}
