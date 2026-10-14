-- 保留现有订单及外键关系，扩展 HashPay 原生状态并支持后台逻辑删除。
CREATE TABLE orders_next (
 id TEXT PRIMARY KEY, product_id TEXT NOT NULL REFERENCES products(id),
 product_name TEXT NOT NULL, kind TEXT NOT NULL, unit_price INTEGER NOT NULL CHECK(unit_price > 0),
 currency TEXT NOT NULL, quantity INTEGER NOT NULL CHECK(quantity BETWEEN 1 AND 100),
 total INTEGER NOT NULL CHECK(total > 0), content TEXT NOT NULL DEFAULT '', config TEXT NOT NULL DEFAULT '{}',
 token_hash TEXT NOT NULL UNIQUE, gateway_id TEXT UNIQUE, checkout_url TEXT NOT NULL DEFAULT '',
 payment_status TEXT NOT NULL CHECK(payment_status IN ('creating','pending','paid','expired','invalid','review','closed')),
 delivery_status TEXT NOT NULL DEFAULT 'pending' CHECK(delivery_status IN ('pending','processing','done','manual','cancelled')),
 reservation_status TEXT NOT NULL DEFAULT 'reserved' CHECK(reservation_status IN ('reserved','consumed','released')),
 expires_at INTEGER NOT NULL DEFAULT 0, paid_at INTEGER, next_check INTEGER NOT NULL DEFAULT 0,
 attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, deleted_at INTEGER
);
INSERT INTO orders_next (
 id,product_id,product_name,kind,unit_price,currency,quantity,total,content,config,
 token_hash,gateway_id,checkout_url,payment_status,delivery_status,reservation_status,
 expires_at,paid_at,next_check,attempts,last_error,created_at,updated_at
) SELECT
 id,product_id,product_name,kind,unit_price,currency,quantity,total,content,config,
 token_hash,gateway_id,checkout_url,payment_status,delivery_status,reservation_status,
 expires_at,paid_at,next_check,attempts,last_error,created_at,updated_at
FROM orders;
DROP TABLE orders;
ALTER TABLE orders_next RENAME TO orders;
CREATE INDEX orders_schedule ON orders(payment_status,next_check);
PRAGMA user_version = 2;
