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
ALTER TABLE orders ADD COLUMN discounts TEXT NOT NULL DEFAULT '[]';
PRAGMA user_version = 8;
