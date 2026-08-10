-- 0010_treasury_alerts.up.sql
-- F06-T08 业务关键余额告警：treasury / minter / hot wallet 的 ETH + ERC20 余额
-- 周期性监控，低于阈值时写入本表 + Prometheus counter + zap warn。
--
-- 设计要点：
--   1. 每次低于阈值就插入一条新记录（同 address+asset 重复触发 OK）；
--      resolved_at 用于人工或自动确认后填上，不强制 unique —— 监控场景
--      需要看到「这个地址这条资产在过去 24h 被击穿过几次」。
--   2. asset 文本保留泛化能力（'ETH' / 'YD' / 后续 ERC20 都能写）；
--      severity 限定 'warn' | 'critical'，便于前端过滤。
--   3. balance / threshold 使用 numeric(78,0) —— ETH wei (10^18) / YD
--      (10^18) 都远小于 78 位上限（2^256 ~ 1.16e77），与链上 uint256 一致。
--   4. 部分索引：resolved_at IS NULL 用于未解决告警查询；
--      (address, asset, detected_at DESC) 用于最近告警展示。
--   5. 不引入外键：treasury / minter / hot wallet 是链上地址，未必在 users
--      表里有对应行。

BEGIN;

CREATE TABLE IF NOT EXISTS treasury_alerts (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    address      text NOT NULL,
    asset        text NOT NULL,
    balance      numeric(78, 0) NOT NULL,
    threshold    numeric(78, 0) NOT NULL,
    severity     text NOT NULL,
    detected_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at  timestamptz,
    CONSTRAINT treasury_alerts_asset_chk
        CHECK (asset IN ('ETH', 'YD')),
    CONSTRAINT treasury_alerts_severity_chk
        CHECK (severity IN ('warn', 'critical')),
    CONSTRAINT treasury_alerts_address_chk
        CHECK (address ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT treasury_alerts_balance_chk
        CHECK (balance >= 0),
    CONSTRAINT treasury_alerts_threshold_chk
        CHECK (threshold >= 0)
);

-- 未解决告警快速列表（resolved_at IS NULL 是稳态热点路径）
CREATE INDEX IF NOT EXISTS treasury_alerts_unresolved
    ON treasury_alerts(detected_at)
    WHERE resolved_at IS NULL;

-- 同 (address, asset) 最近告警
CREATE INDEX IF NOT EXISTS treasury_alerts_address_asset_idx
    ON treasury_alerts(address, asset, detected_at DESC);

COMMIT;
