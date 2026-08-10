-- 0007_reorg_reconcile.up.sql
-- F03-T12 / F03-T13 切片：reorg 历史 + DLQ。
--
-- 设计要点：
--   1. chain_reorgs：每次 reorg 落一行；用于事后对账 / 审计；append-only。
--   2. dlq_events：worker 在 reconcile 时发现漏块 / 异常 → 写入 DLQ；
--      admin 介入可重试或忽略。
--   3. 任何 error / payload 都是 JSONB；size 不限但生产应 cap。
--   4. index 覆盖：reorgs(chain_id, common_block) 加速 rewind；dlq(resolved, created_at) 加速 admin 列表。

BEGIN;

-- ============================================================
-- chain_reorgs：reorg 事件留痕
-- ============================================================
CREATE TABLE IF NOT EXISTS chain_reorgs (
    id              bigserial PRIMARY KEY,
    chain_id        bigint NOT NULL,
    common_block    bigint NOT NULL,   -- reorg 前最后一致块（含）
    new_block_hash  bytea,             -- 新链上 common_block 之后的 hash（采样）
    orphaned_events integer NOT NULL DEFAULT 0,
    affected_orders integer NOT NULL DEFAULT 0,
    reason          text NOT NULL,     -- 'depth_miss' | 'manual_rewind' | 'rpc_mismatch'
    payload         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS chain_reorgs_chain_block_idx
    ON chain_reorgs(chain_id, common_block DESC);

-- ============================================================
-- dlq_events：reconcile / 异常事件死信
-- ============================================================
CREATE TABLE IF NOT EXISTS dlq_events (
    id              bigserial PRIMARY KEY,
    consumer        text NOT NULL,                 -- 'indexer' | 'reconcile' | 'confirmer'
    chain_id        bigint,
    kind            text NOT NULL,                 -- 'gap' | 'reorg' | 'decode' | 'persist'
    severity        text NOT NULL DEFAULT 'warn',  -- 'warn' | 'error'
    summary         text NOT NULL,
    payload         jsonb NOT NULL DEFAULT '{}'::jsonb,
    retry_count     integer NOT NULL DEFAULT 0,
    resolved        boolean NOT NULL DEFAULT false,
    resolved_at     timestamptz,
    resolved_by     uuid REFERENCES users(id),
    resolution      text,                          -- 'replayed' | 'ignored' | 'manual'
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS dlq_events_unresolved_idx
    ON dlq_events(resolved, created_at DESC)
    WHERE resolved = false;

CREATE INDEX IF NOT EXISTS dlq_events_consumer_idx
    ON dlq_events(consumer, created_at DESC);

COMMIT;
