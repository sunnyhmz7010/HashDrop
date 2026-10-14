-- 旧订单须通过一次成功的签名查询核验归属，不能根据当前配置推测历史商户。
ALTER TABLE orders ADD COLUMN gateway_url TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN gateway_merchant_id TEXT NOT NULL DEFAULT '';
PRAGMA user_version = 3;
