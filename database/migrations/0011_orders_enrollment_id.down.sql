-- 0011_orders_enrollment_id.down.sql
-- 回滚 0011：撤掉 enrollment_id 列与 partial index。
DROP INDEX IF EXISTS orders_enrollment_id_idx;
ALTER TABLE orders DROP COLUMN IF EXISTS enrollment_id;