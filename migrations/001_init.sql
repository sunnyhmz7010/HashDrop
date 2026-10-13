CREATE TABLE IF NOT EXISTS products (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
 price INTEGER NOT NULL CHECK(price > 0), currency TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('card','account','static','api')),
 active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0,1)),
 config TEXT NOT NULL DEFAULT '{}', content TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS orders (
 id TEXT PRIMARY KEY, product_id TEXT NOT NULL REFERENCES products(id),
 product_name TEXT NOT NULL, kind TEXT NOT NULL, unit_price INTEGER NOT NULL CHECK(unit_price > 0),
 currency TEXT NOT NULL, quantity INTEGER NOT NULL CHECK(quantity BETWEEN 1 AND 100),
 total INTEGER NOT NULL CHECK(total > 0), content TEXT NOT NULL DEFAULT '', config TEXT NOT NULL DEFAULT '{}',
 token_hash TEXT NOT NULL UNIQUE, gateway_id TEXT UNIQUE, checkout_url TEXT NOT NULL DEFAULT '',
 payment_status TEXT NOT NULL CHECK(payment_status IN ('creating','pending','paid','review','closed')),
 delivery_status TEXT NOT NULL DEFAULT 'pending' CHECK(delivery_status IN ('pending','processing','done','manual')),
 reservation_status TEXT NOT NULL DEFAULT 'reserved' CHECK(reservation_status IN ('reserved','consumed','released')),
 expires_at INTEGER NOT NULL DEFAULT 0, paid_at INTEGER, next_check INTEGER NOT NULL DEFAULT 0,
 attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS stock_items (
 id TEXT PRIMARY KEY, product_id TEXT NOT NULL REFERENCES products(id), content TEXT NOT NULL,
 fingerprint TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'available' CHECK(status IN ('available','reserved','sold')),
 order_id TEXT REFERENCES orders(id), created_at INTEGER NOT NULL,
 UNIQUE(product_id,fingerprint),
 CHECK((status='available' AND order_id IS NULL) OR (status IN ('reserved','sold') AND order_id IS NOT NULL))
);
CREATE TABLE IF NOT EXISTS deliveries (
 id TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES orders(id), ordinal INTEGER NOT NULL CHECK(ordinal > 0),
 stock_id TEXT UNIQUE REFERENCES stock_items(id), result TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','done','manual')),
 idempotency_key TEXT NOT NULL UNIQUE, attempts INTEGER NOT NULL DEFAULT 0,
 next_retry INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL,
 UNIQUE(order_id,ordinal)
);
CREATE TABLE IF NOT EXISTS admin_sessions (
 token_hash TEXT PRIMARY KEY, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS stock_available ON stock_items(product_id,status);
CREATE INDEX IF NOT EXISTS orders_schedule ON orders(payment_status,next_check);
CREATE INDEX IF NOT EXISTS deliveries_schedule ON deliveries(status,next_retry);
PRAGMA user_version = 1;
