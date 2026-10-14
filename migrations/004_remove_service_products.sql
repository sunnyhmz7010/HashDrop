-- 保留商品 ID 和历史订单；已停止支持的商品下架并转为待填写内容的固定文本商品。
CREATE TABLE products_next (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
 price INTEGER NOT NULL CHECK(price > 0), currency TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('card','account','static')),
 active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0,1)),
 content TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
INSERT INTO products_next SELECT
 id,name,description,price,currency,
 CASE WHEN kind IN ('card','account','static') THEN kind ELSE 'static' END,
 CASE WHEN kind IN ('card','account','static') THEN active ELSE 0 END,
 content,created_at,updated_at FROM products;
DROP TABLE products;
ALTER TABLE products_next RENAME TO products;

ALTER TABLE orders DROP COLUMN config;
CREATE TABLE deliveries_next (
 id TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES orders(id), ordinal INTEGER NOT NULL CHECK(ordinal > 0),
 stock_id TEXT UNIQUE REFERENCES stock_items(id), result TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','done','manual')),
 attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL,
 UNIQUE(order_id,ordinal)
);
INSERT INTO deliveries_next(id,order_id,ordinal,stock_id,result,status,attempts,last_error,updated_at)
 SELECT id,order_id,ordinal,stock_id,result,status,attempts,last_error,updated_at FROM deliveries;
DROP TABLE deliveries;
ALTER TABLE deliveries_next RENAME TO deliveries;

-- 历史已付款订单保留已交付内容；无法继续交付的旧类型进入人工处理。
UPDATE deliveries SET status='manual',last_error='商品交付类型已停止支持'
 WHERE status<>'done' AND order_id IN (
 SELECT id FROM orders WHERE kind NOT IN ('card','account','static') AND payment_status='paid'
 );
UPDATE orders SET delivery_status='manual',last_error='商品交付类型已停止支持'
 WHERE kind NOT IN ('card','account','static') AND payment_status='paid' AND delivery_status<>'done';
PRAGMA user_version = 4;
