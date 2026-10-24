package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestDatabaseInitializesAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "全新安装", "hashdrop.db")
	db, err := openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := readDonationSettings(db.QueryRow("SELECT document FROM donation_settings WHERE id=1"))
	if err != nil || settings.Enabled || len(settings.Currencies) != 1 || settings.Currencies[0].Currency != "CNY" {
		t.Fatalf("全新安装的赞赏设置无效: %+v %v", settings, err)
	}
	if _, err = db.Exec("INSERT INTO admin_sessions(token_hash,expires_at,created_at) VALUES('saved-session',100,1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE donation_settings SET document=json_set(document,'$.title','已保存的标题') WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`
INSERT INTO products(id,name,price,currency,inventory_strategy,active,created_at,updated_at) VALUES('saved-product','已保存商品',100,'CNY','newest',1,1,1);
INSERT INTO inventories(id,name,kind,created_at,updated_at) VALUES('saved-pool','已保存库存池','card',1,1);
INSERT INTO inventory_products(inventory_id,product_id,created_at) VALUES('saved-pool','saved-product',1);`); err != nil {
		t.Fatal("新建数据库不能保存商品分配策略或库存绑定", err)
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
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
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
	product, err := scanProduct(db.QueryRow(productSelect + " WHERE p.id='saved-product'"))
	if err != nil || product.InventoryStrategy != "newest" || product.InventoryCount != 1 || product.Kind != "card" {
		t.Fatal("重新打开数据库后丢失商品分配策略或库存绑定", product, err)
	}
	if _, err = db.Exec("INSERT INTO inventory_products(inventory_id,product_id,created_at) VALUES('missing','saved-product',1)"); err == nil {
		t.Fatal("数据库接受了不存在的库存池")
	}
	if _, err = db.Exec("INSERT INTO inventory_products(inventory_id,product_id,created_at) VALUES('saved-pool','missing',1)"); err == nil {
		t.Fatal("数据库接受了不存在的商品绑定")
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

func TestDatabaseRejectsOtherVersionsWithoutChangingRecords(t *testing.T) {
	for _, version := range []int{1, 2, 3, 4, 5, 7} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "unsupported.db")
			db, err := sql.Open("sqlite", databaseURI(path))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(fmt.Sprintf("CREATE TABLE marker(value TEXT); INSERT INTO marker VALUES('保留记录'); PRAGMA user_version=%d;", version)); err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			if opened, err := openDatabase(path); err == nil {
				opened.Close()
				t.Fatal("接受了非当前结构版本", version)
			}
			db, err = sql.Open("sqlite", databaseURI(path))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var savedVersion int
			var value string
			if err = db.QueryRow("PRAGMA user_version").Scan(&savedVersion); err != nil || savedVersion != version {
				t.Fatal("改写了不支持的结构版本", savedVersion, err)
			}
			if err = db.QueryRow("SELECT value FROM marker").Scan(&value); err != nil || value != "保留记录" {
				t.Fatal("改写了不支持版本的数据", value, err)
			}
		})
	}
}
