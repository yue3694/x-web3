BEGIN;

-- 课程价格版本：每个 (course_id, chain_id) 维护一条当前生效版本；价格变更 → 新版本。
CREATE TABLE course_prices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    version integer NOT NULL,
    chain_id bigint NOT NULL,
    token_address text NOT NULL,
    amount numeric(78,0) NOT NULL,
    decimals integer NOT NULL,
    market_address text NOT NULL,
    valid_from timestamptz NOT NULL DEFAULT now(),
    valid_to timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT course_prices_version_chk CHECK (version > 0),
    CONSTRAINT course_prices_decimals_chk CHECK (decimals BETWEEN 0 AND 36),
    CONSTRAINT course_prices_amount_chk CHECK (amount > 0),
    UNIQUE (course_id, version, chain_id)
);
CREATE INDEX course_prices_current_idx ON course_prices(course_id, chain_id) WHERE valid_to IS NULL;

-- 购买意图：用户准备付钱；TTL 15 min；idempotency_key 保证幂等。
CREATE TABLE purchase_intents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    wallet_id uuid NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    course_id uuid NOT NULL REFERENCES courses(id) ON DELETE RESTRICT,
    price_id uuid NOT NULL REFERENCES course_prices(id) ON DELETE RESTRICT,
    course_key bytea NOT NULL,
    price_version integer NOT NULL,
    chain_id bigint NOT NULL,
    token_address text NOT NULL,
    amount numeric(78,0) NOT NULL,
    market_address text NOT NULL,
    idempotency_key text NOT NULL,
    status text NOT NULL DEFAULT 'created',
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT purchase_intents_status_chk CHECK (status IN ('created','submitted','confirming','confirmed','failed','expired','reorged')),
    CONSTRAINT purchase_intents_amount_chk CHECK (amount > 0),
    CONSTRAINT purchase_intents_course_key_len_chk CHECK (octet_length(course_key) = 32),
    UNIQUE (user_id, idempotency_key)
);
CREATE INDEX purchase_intents_user_idx ON purchase_intents(user_id, created_at DESC);
CREATE INDEX purchase_intents_course_idx ON purchase_intents(course_id, created_at DESC);
CREATE INDEX purchase_intents_status_idx ON purchase_intents(status) WHERE status IN ('created','submitted','confirming');

-- 订单：每条 intent 对应一条；链上确认时填 tx_hash / block / log_index。
CREATE TABLE orders (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    intent_id uuid NOT NULL UNIQUE REFERENCES purchase_intents(id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    course_id uuid NOT NULL REFERENCES courses(id) ON DELETE RESTRICT,
    status text NOT NULL DEFAULT 'submitted',
    chain_id bigint NOT NULL,
    tx_hash bytea,
    block_number bigint,
    log_index integer,
    block_hash bytea,
    confirmed_at timestamptz,
    failure_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT orders_status_chk CHECK (status IN ('submitted','confirming','confirmed','failed','expired','reorged')),
    CONSTRAINT orders_tx_unique UNIQUE (chain_id, tx_hash)
);
CREATE INDEX orders_user_idx ON orders(user_id, created_at DESC);
CREATE INDEX orders_course_idx ON orders(course_id, created_at DESC);
CREATE INDEX orders_status_idx ON orders(status) WHERE status IN ('submitted','confirming');

-- 链事件原始记录：worker 入库；唯一约束保证幂等消费。
CREATE TABLE chain_events (
    chain_id bigint NOT NULL,
    tx_hash bytea NOT NULL,
    log_index integer NOT NULL,
    block_number bigint NOT NULL,
    block_hash bytea NOT NULL,
    event_signature bytea NOT NULL,
    payload jsonb NOT NULL,
    canonical boolean NOT NULL DEFAULT true,
    seen_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chain_id, tx_hash, log_index)
);
CREATE INDEX chain_events_block_idx ON chain_events(chain_id, block_number);

-- checkpoint：每个 consumer 独立推进；记录 last_block_hash 用于 reorg 检测。
CREATE TABLE chain_checkpoints (
    chain_id bigint NOT NULL,
    consumer text NOT NULL,
    next_block bigint NOT NULL,
    last_block_hash bytea,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chain_id, consumer)
);

-- outbox：worker 写入，单独的 publisher 进程消费（后续 Phase 加）。
CREATE TABLE outbox_events (
    id bigserial PRIMARY KEY,
    aggregate text NOT NULL,
    type text NOT NULL,
    payload jsonb NOT NULL,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX outbox_unpublished_idx ON outbox_events(created_at) WHERE published_at IS NULL;

-- 触发器：保持 updated_at。
CREATE TRIGGER purchase_intents_touch_updated_at BEFORE UPDATE ON purchase_intents FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
CREATE TRIGGER orders_touch_updated_at BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
CREATE TRIGGER chain_checkpoints_touch_updated_at BEFORE UPDATE ON chain_checkpoints FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

COMMIT;
