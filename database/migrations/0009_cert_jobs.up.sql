-- 0009_cert_jobs.up.sql
-- F04 切片：完课记录 + 证书记录 + mint job 表。
--
-- 设计要点：
--   1. course_completions(enrollment_id, rule_version) 唯一：同一完课规则版本下
--      一个 enrollment 只算一次完课，避免重复 mint job 触发（与 jobs.dedupe_key 互为冗余）。
--   2. certificates.completion_id 唯一：mint 上链后 token_id / tx_hash / status 落表。
--      status 取值 'pending' | 'minting' | 'confirmed' | 'failed' | 'dead'。
--   3. certificate_jobs 是 worker 主消费的队列表（区别于通用 jobs 表）：
--      - SELECT FOR UPDATE SKIP LOCKED 防多 worker 抢同一条；
--      - payload JSONB 包含 recipient_wallet / certificate_id / metadata_uri 等；
--      - attempt / next_retry_at / last_error 走 retry-with-backoff 模式；
--      - max_attempts 由 worker 端默认 5 兜底（不存表，避免配置漂移）。
--   4. 所有金额 / token_id 为 numeric(78,0)；tx_hash 用 bytea 32B。
--   5. 索引按 worker hot-path 优化：status='pending' AND attempt<max AND run_after<=now()。

BEGIN;

-- ============================================================
-- lesson_progress: 学习进度（按 user + lesson 维度，非递减）
-- ============================================================
CREATE TABLE IF NOT EXISTS lesson_progress (
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lesson_id   uuid NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
    pct         integer NOT NULL DEFAULT 0,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, lesson_id),
    CONSTRAINT lesson_progress_pct_chk CHECK (pct BETWEEN 0 AND 100)
);
CREATE INDEX IF NOT EXISTS lesson_progress_lesson_idx
    ON lesson_progress(lesson_id);

-- ============================================================
-- course_completions: 完课记录
-- ============================================================
CREATE TABLE IF NOT EXISTS course_completions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    enrollment_id   uuid NOT NULL REFERENCES enrollments(id) ON DELETE CASCADE,
    rule_version    integer NOT NULL DEFAULT 1,
    completed_at    timestamptz NOT NULL DEFAULT now(),
    revoked_at      timestamptz,
    CONSTRAINT course_completions_rule_version_chk CHECK (rule_version > 0),
    UNIQUE (enrollment_id, rule_version)
);
CREATE INDEX IF NOT EXISTS course_completions_enrollment_idx
    ON course_completions(enrollment_id);

-- ============================================================
-- certificates: 铸造记录（链上旁路表，DB 是权威）
-- ============================================================
CREATE TABLE IF NOT EXISTS certificates (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    completion_id       uuid NOT NULL UNIQUE REFERENCES course_completions(id) ON DELETE RESTRICT,
    user_id             uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    course_id           uuid NOT NULL REFERENCES courses(id) ON DELETE RESTRICT,
    cert_version        integer NOT NULL DEFAULT 1,
    certificate_id      numeric(78, 0) NOT NULL, -- 与链上 bytes32 / uint256 一致
    recipient_wallet    text NOT NULL,
    metadata_uri        text NOT NULL,
    metadata_sha256     text NOT NULL,
    chain_id            bigint NOT NULL,
    status              text NOT NULL DEFAULT 'pending',
    tx_hash             bytea,
    token_id            numeric(78, 0),
    confirmed_block     bigint,
    confirmed_at        timestamptz,
    attempts            integer NOT NULL DEFAULT 0,
    last_error          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT certificates_status_chk CHECK (status IN ('pending','minting','confirmed','failed','dead')),
    CONSTRAINT certificates_tx_unique UNIQUE (chain_id, tx_hash),
    CONSTRAINT certificates_recipient_chk CHECK (recipient_wallet ~ '^0x[a-fA-F0-9]{40}$'),
    CONSTRAINT certificates_cert_id_chk CHECK (certificate_id >= 0),
    UNIQUE (user_id, course_id, cert_version)
);
CREATE INDEX IF NOT EXISTS certificates_user_idx ON certificates(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS certificates_status_idx ON certificates(status) WHERE status IN ('pending','minting','failed');

-- ============================================================
-- certificate_jobs: worker 主消费队列
-- ============================================================
CREATE TABLE IF NOT EXISTS certificate_jobs (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    certificate_id      uuid NOT NULL UNIQUE REFERENCES certificates(id) ON DELETE CASCADE,
    status              text NOT NULL DEFAULT 'pending',
    attempt             integer NOT NULL DEFAULT 0,
    last_error          text,
    next_retry_at       timestamptz NOT NULL DEFAULT now(),
    started_at          timestamptz,
    confirmed_at        timestamptz,
    tx_hash             bytea,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT certificate_jobs_status_chk CHECK (status IN ('pending','minting','confirmed','failed','dead'))
);
-- worker hot-path 索引：status=pending AND run_after<=now() 按创建时间顺序。
CREATE INDEX IF NOT EXISTS certificate_jobs_pending_idx
    ON certificate_jobs(status, next_retry_at, created_at)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS certificate_jobs_cert_idx
    ON certificate_jobs(certificate_id);

-- 触发器：自动维护 updated_at
CREATE TRIGGER certificates_touch_updated_at
BEFORE UPDATE ON certificates FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TRIGGER certificate_jobs_touch_updated_at
BEFORE UPDATE ON certificate_jobs FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

COMMIT;