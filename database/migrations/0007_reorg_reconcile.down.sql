-- 0007_reorg_reconcile.down.sql
BEGIN;
DROP TABLE IF EXISTS dlq_events;
DROP TABLE IF EXISTS chain_reorgs;
COMMIT;
