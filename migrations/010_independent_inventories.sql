-- 每个旧商品迁入同 ID 的库存池，保持库存指纹、预留及交付记录不变。
CREATE TABLE inventories (
 id TEXT PRIMARY KEY, name TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('card','account','static')),
 content TEXT NOT NULL DEFAULT '', content_aad TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, deleted_at INTEGER,
 CHECK(content='' OR content_aad IN ('inventory:' || id,'product:' || id))
);
-- 旧固定内容保留原密文认证上下文，下次修改内容时再使用库存池上下文。
INSERT INTO inventories(id,name,kind,content,content_aad,created_at,updated_at)
 SELECT id,name,kind,content,CASE WHEN content<>'' THEN 'product:' || id ELSE '' END,created_at,updated_at FROM products;

CREATE TABLE products_next (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
 details TEXT NOT NULL DEFAULT '', price INTEGER NOT NULL CHECK(price > 0), currency TEXT NOT NULL,
 inventory_id TEXT REFERENCES inventories(id),
 active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0,1)),
 hidden INTEGER NOT NULL DEFAULT 0 CHECK(hidden IN (0,1)),
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, deleted_at INTEGER,
 CHECK(active=0 OR inventory_id IS NOT NULL)
);
INSERT INTO products_next(id,name,description,details,price,currency,inventory_id,active,hidden,created_at,updated_at,deleted_at)
 SELECT id,name,description,details,price,currency,id,active,hidden,created_at,updated_at,deleted_at FROM products;
DROP TABLE products;
ALTER TABLE products_next RENAME TO products;
CREATE INDEX products_inventory ON products(inventory_id);

CREATE TABLE stock_items_next (
 id TEXT PRIMARY KEY, inventory_id TEXT NOT NULL REFERENCES inventories(id), content TEXT NOT NULL,
 fingerprint TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'available' CHECK(status IN ('available','reserved','sold')),
 order_id TEXT REFERENCES orders(id), created_at INTEGER NOT NULL,
 UNIQUE(inventory_id,fingerprint),
 CHECK((status='available' AND order_id IS NULL) OR (status IN ('reserved','sold') AND order_id IS NOT NULL))
);
INSERT INTO stock_items_next(id,inventory_id,content,fingerprint,status,order_id,created_at)
 SELECT id,product_id,content,fingerprint,status,order_id,created_at FROM stock_items;
DROP TABLE stock_items;
ALTER TABLE stock_items_next RENAME TO stock_items;
CREATE INDEX stock_available ON stock_items(inventory_id,status);

-- 商品改绑库存池后，订单仍保留原库存来源和交付类型快照。
ALTER TABLE orders ADD COLUMN inventory_id TEXT REFERENCES inventories(id);
UPDATE orders SET inventory_id=(SELECT inventory_id FROM products WHERE products.id=orders.product_id);
PRAGMA user_version = 10;
