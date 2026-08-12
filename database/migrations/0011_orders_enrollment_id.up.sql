-- 0011_orders_enrollment_id.up.sql
-- F03-T15 续：在 orders 上回写 enrollment_id，便于前端 GetOrder / GetMyOrders
-- 拿到的 OrderResponse 直接含 enrollmentId（OpenAPI spec 已对齐）。
--
-- worker Confirmer.Apply 在 confirmed 分支写入 enrollments 后，把新 enrollment.id
-- 回填到 orders.enrollment_id（NULL when status != 'confirmed'）。
--
-- 这条 migration 只加列 + 索引；不重构任何既有 contract。失败的可空约束保证老 rows 安全。

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS enrollment_id uuid
    REFERENCES enrollments(id) ON DELETE SET NULL;

-- partial index：只对 confirmed 的 orders 建 enrollment_id 索引。
-- 大部分订单终态是 confirmed，但仍有少量 failed / reorged — 不为它们建索引
-- 可以让索引体量保持在 order 总量 30% 以下。
CREATE INDEX IF NOT EXISTS orders_enrollment_id_idx
    ON orders(enrollment_id)
    WHERE enrollment_id IS NOT NULL;

-- down：撤掉列与索引。
-- DROP INDEX IF EXISTS orders_enrollment_id_idx;
-- ALTER TABLE orders DROP COLUMN IF EXISTS enrollment_id;