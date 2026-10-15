-- 赞赏使用独立记录，不参与商品库存、交付和优惠。
CREATE TABLE donation_settings (
 id INTEGER PRIMARY KEY CHECK(id = 1), document TEXT NOT NULL
);
INSERT INTO donation_settings(id,document) VALUES(1,
 '{"enabled":false,"title":"赞赏支持","description":"","currency":"CNY","amounts":[500,1000,2000],"min_amount":100,"max_amount":100000}');
CREATE TABLE donations (
 id TEXT PRIMARY KEY, title TEXT NOT NULL,
 amount INTEGER NOT NULL CHECK(amount > 0 AND amount <= 9000000000000), currency TEXT NOT NULL,
 token_hash TEXT NOT NULL UNIQUE, gateway_id TEXT UNIQUE,
 gateway_url TEXT NOT NULL, gateway_merchant_id TEXT NOT NULL,
 checkout_url TEXT NOT NULL DEFAULT '',
 payment_status TEXT NOT NULL CHECK(payment_status IN ('creating','pending','paid','expired','invalid','review','closed')),
 expires_at INTEGER NOT NULL DEFAULT 0, paid_at INTEGER,
 next_check INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE INDEX donations_schedule ON donations(payment_status,next_check);
CREATE INDEX donations_created ON donations(created_at DESC,id DESC);
PRAGMA user_version = 9;
