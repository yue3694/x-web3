-- 0001_identity.up.sql
-- F01 切片：users / wallets / roles / permissions / user_roles / role_permissions / audit_logs
--
-- 设计要点：
--   1. UUID 主键；privy_user_id 唯一；
--   2. wallets(chain_id, address) 唯一 → 全局唯一绑定；
--   3. audit_logs 月分区 + append-only（DB role 无 UPDATE/DELETE）；
--   4. 所有 created_at/updated_at 用 now() 默认；
--   5. 金额字段在后续 migration（course_prices / orders）。

BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;  -- gen_random_uuid()

-- ============================================================
-- users: 以 UUID 为主键；privy_user_id 是与 Privy 的唯一对应键。
-- ============================================================
CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    privy_user_id   text NOT NULL,
    display_name    text NOT NULL DEFAULT 'Anonymous',
    status          text NOT NULL DEFAULT 'active',  -- active / suspended
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_privy_user_id_unique UNIQUE (privy_user_id)
);

CREATE INDEX users_status_idx ON users(status);

-- ============================================================
-- wallets: 一个 user 可绑多个钱包；(chain_id, address) 全局唯一。
-- chain_namespace 预留：未来 Solana/Polygon 用 'solana'/'polygon'。
-- ============================================================
CREATE TABLE wallets (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chain_namespace text NOT NULL DEFAULT 'eip155',
    chain_id        bigint NOT NULL,
    address         text NOT NULL,
    is_primary      boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT wallets_unique UNIQUE (chain_namespace, chain_id, address),
    CONSTRAINT wallets_address_chk CHECK (address ~ '^0x[a-fA-F0-9]{40}$')
);

CREATE INDEX wallets_user_id_idx ON wallets(user_id);

-- ============================================================
-- roles & permissions: 静态字典，初始由 seed 注入。
-- ============================================================
CREATE TABLE roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code        text NOT NULL,
    description text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT roles_code_unique UNIQUE (code)
);

CREATE TABLE permissions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code        text NOT NULL,
    description text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT permissions_code_unique UNIQUE (code)
);

CREATE TABLE role_permissions (
    role_id        uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id  uuid NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE user_roles (
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id     uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    granted_by  uuid REFERENCES users(id),
    granted_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX user_roles_role_id_idx ON user_roles(role_id);

-- ============================================================
-- audit_logs: append-only；monthly 分区便于归档/保留策略。
-- DB role 上需 REVOKE UPDATE/DELETE。
-- ============================================================
CREATE TABLE audit_logs (
    id              bigserial,
    actor_user_id   uuid REFERENCES users(id),
    action          text NOT NULL,
    target_type     text,
    target_id       text,
    before          jsonb,
    after           jsonb,
    correlation_id  text,
    ip              inet,
    user_agent      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE INDEX audit_logs_actor_idx ON audit_logs(actor_user_id, created_at DESC);
CREATE INDEX audit_logs_action_idx ON audit_logs(action, created_at DESC);
CREATE INDEX audit_logs_target_idx ON audit_logs(target_type, target_id, created_at DESC);

-- 创建当月 + 下一月分区；后续由 ops cron 续期
CREATE TABLE audit_logs_default PARTITION OF audit_logs DEFAULT;

CREATE OR REPLACE FUNCTION ensure_audit_partition(month_start timestamptz)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    part_name text;
    range_from timestamptz;
    range_to   timestamptz;
BEGIN
    range_from := date_trunc('month', month_start);
    range_to   := range_from + interval '1 month';
    part_name  := 'audit_logs_' || to_char(range_from, 'YYYY_MM');
    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS %I PARTITION OF audit_logs FOR VALUES FROM (%L) TO (%L)',
        part_name, range_from, range_to
    );
END $$;

SELECT ensure_audit_partition(now());
SELECT ensure_audit_partition(now() + interval '1 month');

-- 触发器：users updated_at 自动维护
CREATE OR REPLACE FUNCTION touch_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END $$;

CREATE TRIGGER users_touch_updated_at
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- 每个新用户自动授予 student 角色（如果 seed 已注入 role 'student'）。
-- 此处不能直接 join roles 表，因为 seed 还没跑。
-- 由 service 层（UpsertByPrivySubject 后调 GrantDefaultRole）兜底。

COMMIT;