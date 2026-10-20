package main

import (
	"database/sql"
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
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, foreignKeys, sessions int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
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

// 保留旧版实际赞赏结构，避免用当前 schema 伪造迁移前的列和默认值。
const donationSchemaV1ForTest = `
CREATE TABLE donation_settings(id INTEGER PRIMARY KEY,document TEXT NOT NULL);
INSERT INTO donation_settings VALUES(1,'{"enabled":true,"title":"旧版赞赏","description":"保留说明","currency":"USD","amounts":[250,500],"min_amount":100,"max_amount":10000}');
CREATE TABLE donations (
 id TEXT PRIMARY KEY,title TEXT NOT NULL,
 amount INTEGER NOT NULL CHECK(amount>0 AND amount<=9000000000000),currency TEXT NOT NULL,
 token_hash TEXT NOT NULL UNIQUE,gateway_id TEXT UNIQUE,
 gateway_url TEXT NOT NULL,gateway_merchant_id TEXT NOT NULL,
 checkout_url TEXT NOT NULL DEFAULT '',
 payment_status TEXT NOT NULL CHECK(payment_status IN ('creating','pending','paid','expired','invalid','review','closed')),
 expires_at INTEGER NOT NULL DEFAULT 0,paid_at INTEGER,
 next_check INTEGER NOT NULL DEFAULT 0,attempts INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL
);
INSERT INTO donations(id,title,amount,currency,token_hash,gateway_url,gateway_merchant_id,payment_status,created_at,updated_at)
 VALUES('legacy-donation','旧记录',250,'USD','legacy-token-hash','https://old.example','old-merchant','paid',1,1),
 ('legacy-donation-2','另一笔匿名记录',500,'USD','legacy-token-hash-2','https://old.example','old-merchant','pending',2,2);
CREATE INDEX donations_schedule ON donations(payment_status,next_check);
CREATE INDEX donations_created ON donations(created_at DESC,id DESC);
CREATE TABLE orders(id TEXT PRIMARY KEY,token_hash TEXT NOT NULL,content TEXT NOT NULL);
INSERT INTO orders VALUES('old-order','old-order-token','v1:原订单密文');
PRAGMA user_version=1;
`

func TestDatabaseMigratesV1DonationsWithoutRewritingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(donationSchemaV1ForTest); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		db, err := openDatabase(path)
		if err != nil {
			t.Fatalf("旧库迁移或再次打开失败: %v", err)
		}
		var version int
		var nickname, requestID, hash, document, orderHash, cipher string
		var shown bool
		if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
			db.Close()
			t.Fatal("迁移未升级到版本2", version, err)
		}
		if err := db.QueryRow("SELECT nickname,show_public,request_id,token_hash FROM donations WHERE id='legacy-donation'").Scan(&nickname, &shown, &requestID, &hash); err != nil || nickname != "" || shown || requestID != "" || hash != "legacy-token-hash" {
			db.Close()
			t.Fatal("迁移公开了旧匿名记录或改写了旧凭证", nickname, shown, requestID, hash, err)
		}
		if err := db.QueryRow("SELECT document FROM donation_settings WHERE id=1").Scan(&document); err != nil || document != `{"enabled":true,"title":"旧版赞赏","description":"保留说明","currency":"USD","amounts":[250,500],"min_amount":100,"max_amount":10000}` {
			db.Close()
			t.Fatal("迁移改写了旧版设置文档", err)
		}
		settings, err := readDonationSettings(db.QueryRow("SELECT document FROM donation_settings WHERE id=1"))
		if err != nil || len(settings.Currencies) != 1 || settings.Currencies[0].Currency != "USD" || len(settings.Currencies[0].Amounts) != 2 || settings.Currencies[0].Amounts[0] != 250 || settings.Currencies[0].MinAmount != 100 || settings.Currencies[0].MaxAmount != 10000 {
			db.Close()
			t.Fatal("旧版单币种配置未在读取时兼容", settings, err)
		}
		if err := db.QueryRow("SELECT token_hash,content FROM orders WHERE id='old-order'").Scan(&orderHash, &cipher); err != nil || orderHash != "old-order-token" || cipher != "v1:原订单密文" {
			db.Close()
			t.Fatal("赞赏迁移改变了商品订单数据", err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db, err := openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	insert := func(id, nickname, requestID string) error {
		_, err := db.Exec(`INSERT INTO donations(id,title,amount,currency,token_hash,gateway_url,gateway_merchant_id,payment_status,created_at,updated_at,nickname,request_id)
 VALUES(?,'新增记录',100,'USD',?,'https://old.example','old-merchant','creating',3,3,?,?)`, id, "token-"+id, nickname, requestID)
		return err
	}
	if err := insert("named", "唯一昵称", "unique-request"); err != nil {
		t.Fatal(err)
	}
	if err := insert("nickname-conflict", "唯一昵称", "another-request"); err == nil {
		t.Fatal("迁移后的唯一索引允许重复非空昵称")
	}
	if err := insert("request-conflict", "另一个昵称", "unique-request"); err == nil {
		t.Fatal("迁移后的唯一索引允许重复非空请求标识")
	}
	if err := insert("anonymous-3", "", ""); err != nil {
		t.Fatal("非空唯一索引错误限制了旧版匿名记录", err)
	}
}

func TestDonationV2MigrationFailureRollsBackAllColumns(t *testing.T) {
	db, err := sql.Open("sqlite", databaseURI(filepath.Join(t.TempDir(), "migration-failure.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(donationSchemaV1ForTest); err != nil {
		t.Fatal(err)
	}
	// 在第二列制造冲突，验证前面成功的 ALTER TABLE 也随事务回滚。
	if _, err := db.Exec("ALTER TABLE donations ADD COLUMN show_public INTEGER NOT NULL DEFAULT 0"); err != nil {
		t.Fatal(err)
	}
	if err := migrateDatabaseV2(db); err == nil {
		t.Fatal("迁移中的列冲突被忽略")
	}
	var columns, version, records int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('donations') WHERE name IN ('nickname','request_id')").Scan(&columns); err != nil || columns != 0 {
		t.Fatal("迁移失败留下了部分新列", columns, err)
	}
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatal("迁移失败仍升级了版本", version, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM donations WHERE token_hash IN ('legacy-token-hash','legacy-token-hash-2')").Scan(&records); err != nil || records != 2 {
		t.Fatal("迁移失败改变了旧赞赏记录", records, err)
	}
}
