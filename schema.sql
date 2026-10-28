-- 全新安装结构；初始化在单个事务内完成。
CREATE TABLE inventories (
 id TEXT PRIMARY KEY, name TEXT NOT NULL,
 reusable INTEGER NOT NULL DEFAULT 0 CHECK(reusable IN (0,1)),
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, deleted_at INTEGER
);
CREATE TABLE products (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
 details TEXT NOT NULL DEFAULT '', price INTEGER NOT NULL CHECK(price > 0), currency TEXT NOT NULL,
 inventory_strategy TEXT NOT NULL DEFAULT 'oldest' CHECK(inventory_strategy IN ('random','oldest','newest')),
 active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0,1)),
 hidden INTEGER NOT NULL DEFAULT 0 CHECK(hidden IN (0,1)),
 delivery_plan TEXT NOT NULL,
 delivery_note TEXT NOT NULL DEFAULT '',
 min_quantity INTEGER NOT NULL DEFAULT 1 CHECK(min_quantity BETWEEN 1 AND 100),
 max_quantity INTEGER NOT NULL DEFAULT 100 CHECK(max_quantity BETWEEN min_quantity AND 100),
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, deleted_at INTEGER
);
CREATE TABLE inventory_products (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 inventory_id TEXT NOT NULL REFERENCES inventories(id), product_id TEXT NOT NULL REFERENCES products(id),
 slot_id TEXT NOT NULL, created_at INTEGER NOT NULL, UNIQUE(inventory_id,product_id,slot_id)
);
CREATE INDEX inventory_products_product ON inventory_products(product_id,id);
CREATE TABLE orders (
 id TEXT PRIMARY KEY, product_id TEXT NOT NULL REFERENCES products(id),
 product_name TEXT NOT NULL,
 unit_price INTEGER NOT NULL CHECK(unit_price > 0), currency TEXT NOT NULL,
 quantity INTEGER NOT NULL CHECK(quantity BETWEEN 1 AND 100), total INTEGER NOT NULL CHECK(total > 0),
 discounts TEXT NOT NULL DEFAULT '[]',
 delivery_note TEXT NOT NULL DEFAULT '',
 token_hash TEXT NOT NULL UNIQUE, gateway_id TEXT UNIQUE,
 gateway_url TEXT NOT NULL DEFAULT '', gateway_merchant_id TEXT NOT NULL DEFAULT '',
 checkout_url TEXT NOT NULL DEFAULT '',
 payment_status TEXT NOT NULL CHECK(payment_status IN ('creating','pending','paid','expired','invalid','review','closed')),
 delivery_status TEXT NOT NULL DEFAULT 'pending' CHECK(delivery_status IN ('pending','partial','done','failed','cancelled')),
 reservation_status TEXT NOT NULL DEFAULT 'reserved' CHECK(reservation_status IN ('reserved','consumed','released')),
 expires_at INTEGER NOT NULL DEFAULT 0, paid_at INTEGER, next_check INTEGER NOT NULL DEFAULT 0,
 attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, deleted_at INTEGER
);
CREATE INDEX orders_schedule ON orders(payment_status,next_check);
CREATE INDEX orders_delivery ON orders(delivery_status,created_at DESC) WHERE deleted_at IS NULL AND payment_status='paid';
CREATE TABLE stock_items (
 id TEXT PRIMARY KEY, inventory_id TEXT NOT NULL REFERENCES inventories(id), content TEXT NOT NULL,
 fingerprint TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'available' CHECK(status IN ('available','reserved','sold')),
 order_id TEXT REFERENCES orders(id), created_at INTEGER NOT NULL,
 last_allocated INTEGER NOT NULL DEFAULT 0, deleted_at INTEGER,
 CHECK((status='available' AND order_id IS NULL) OR (status IN ('reserved','sold') AND order_id IS NOT NULL))
);
CREATE UNIQUE INDEX stock_fingerprint ON stock_items(inventory_id,fingerprint) WHERE deleted_at IS NULL;
CREATE INDEX stock_available ON stock_items(inventory_id,status,deleted_at,last_allocated);
CREATE TABLE deliveries (
 id TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES orders(id), ordinal INTEGER NOT NULL CHECK(ordinal > 0),
 result TEXT NOT NULL DEFAULT '', content TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','done','failed','cancelled')),
 attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL,
 UNIQUE(order_id,ordinal)
);
CREATE TABLE delivery_allocations (
 id TEXT PRIMARY KEY, delivery_id TEXT NOT NULL REFERENCES deliveries(id),
 stock_id TEXT NOT NULL REFERENCES stock_items(id), inventory_id TEXT NOT NULL REFERENCES inventories(id),
 slot_id TEXT NOT NULL, ordinal INTEGER NOT NULL CHECK(ordinal > 0),
 consumes INTEGER NOT NULL CHECK(consumes IN (0,1)),
 status TEXT NOT NULL DEFAULT 'reserved' CHECK(status IN ('reserved','consumed','released')),
 UNIQUE(delivery_id,slot_id,ordinal)
);
CREATE UNIQUE INDEX allocated_stock ON delivery_allocations(stock_id) WHERE consumes=1 AND status IN ('reserved','consumed');
CREATE INDEX allocations_delivery ON delivery_allocations(delivery_id,status);
CREATE TABLE admin_sessions (
 token_hash TEXT PRIMARY KEY, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL
);
CREATE TABLE promotions (
 id TEXT PRIMARY KEY, name TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('automatic','code')), code TEXT NOT NULL DEFAULT '',
 discount_type TEXT NOT NULL CHECK(discount_type IN ('percentage','fixed')), value INTEGER NOT NULL CHECK(value > 0),
 currency TEXT NOT NULL DEFAULT '', min_total INTEGER NOT NULL DEFAULT 0 CHECK(min_total >= 0),
 min_quantity INTEGER NOT NULL DEFAULT 1 CHECK(min_quantity BETWEEN 1 AND 100),
 scope TEXT NOT NULL CHECK(scope IN ('all','selected')), stackable INTEGER NOT NULL DEFAULT 0 CHECK(stackable IN (0,1)),
 enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
 starts_at INTEGER NOT NULL DEFAULT 0 CHECK(starts_at >= 0), ends_at INTEGER NOT NULL DEFAULT 0 CHECK(ends_at >= 0),
 usage_limit INTEGER NOT NULL DEFAULT 0 CHECK(usage_limit >= 0), deleted_at INTEGER,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 CHECK((kind='automatic' AND code='') OR (kind='code' AND code<>'')),
 CHECK(discount_type<>'percentage' OR value<=9999),
 CHECK(discount_type<>'fixed' OR currency<>''),
 CHECK(min_total=0 OR currency<>''),
 CHECK(ends_at=0 OR ends_at>starts_at)
);
CREATE UNIQUE INDEX promotion_code ON promotions(code) WHERE kind='code' AND deleted_at IS NULL;
CREATE TABLE promotion_products (
 promotion_id TEXT NOT NULL REFERENCES promotions(id), product_id TEXT NOT NULL REFERENCES products(id),
 PRIMARY KEY(promotion_id,product_id)
);
CREATE TABLE order_promotions (
 order_id TEXT NOT NULL REFERENCES orders(id), promotion_id TEXT NOT NULL REFERENCES promotions(id),
 PRIMARY KEY(order_id,promotion_id)
);
CREATE INDEX promotion_orders ON order_promotions(promotion_id,order_id);
-- 赞赏独立于商品订单、库存和优惠。
CREATE TABLE donation_settings (
 id INTEGER PRIMARY KEY CHECK(id = 1), document TEXT NOT NULL
);
INSERT INTO donation_settings(id,document) VALUES(1,
 '{"enabled":false,"title":"赞赏支持","description":"","currencies":[{"currency":"CNY","amounts":[500,1000,2000],"min_amount":100,"max_amount":100000}]}');
CREATE TABLE donations (
 id TEXT PRIMARY KEY, title TEXT NOT NULL,
 nickname TEXT NOT NULL CHECK(length(nickname) BETWEEN 1 AND 128), show_public INTEGER NOT NULL DEFAULT 0 CHECK(show_public IN (0,1)),
 request_id TEXT NOT NULL DEFAULT '',
 amount INTEGER NOT NULL CHECK(amount > 0 AND amount <= 9000000000000), currency TEXT NOT NULL,
 gateway_id TEXT UNIQUE, gateway_url TEXT NOT NULL, gateway_merchant_id TEXT NOT NULL,
 checkout_url TEXT NOT NULL DEFAULT '',
 payment_status TEXT NOT NULL CHECK(payment_status IN ('creating','pending','paid','expired','invalid','review','closed')),
 expires_at INTEGER NOT NULL DEFAULT 0, paid_at INTEGER,
 next_check INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, deleted_at INTEGER
);
CREATE INDEX donations_schedule ON donations(payment_status,next_check);
CREATE INDEX donations_created ON donations(created_at DESC,id DESC);
CREATE INDEX donations_nickname ON donations(nickname,created_at DESC,id DESC);
CREATE UNIQUE INDEX donations_request ON donations(request_id) WHERE request_id <> '';
CREATE INDEX donations_public ON donations(payment_status,show_public,paid_at DESC,id DESC);
PRAGMA user_version = 8;
