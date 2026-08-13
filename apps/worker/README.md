# apps/worker · x-web3 链事件索引服务

> 监听 Sepolia 链事件（`CoursePurchased`），把订单推到 confirmed 并派生
> `enrollments`；周期性扫漏块写 DLQ；监控 treasury / hot wallet 余额；
> 用 KMS / keystore / anvil 签名器铸造 `CertificateNFT`。
>
> 全局架构、产品流程、合约地址与 AWS 拓扑统一在顶层
> [README.md](../../README.md) 维护，本文件只覆盖 worker 模块自身的子系统、
> 启动顺序、跨 app 契约和运行约束。

---

## 1. 模块定位

`cmd/worker` 是一个进程，内部跑 5 个长生命周期子系统，按职责分到不同
`internal/` 子包：

| 子系统 | 包 | 干什么 |
|---|---|---|
| Indexer | `internal/indexer` | WS 订阅 + HTTP 回扫 + 多 RPC fallback + checkpoint 推进 + reorg 检测 |
| Confirmer | `internal/order` | 把已校验的 `CoursePurchased` 入库 → 推 orders → 派生 enrollments → 写 outbox |
| Reconcile | `internal/reconcile` | 周期性扫 `[lastConfirmed-depth, lastIndexed]` 区间找漏块，写 DLQ |
| CertConsumer | `internal/certificate` | 消费 `certificate_jobs` → 签名 mint → 等链确认 → 落库 `nft_token_id` |
| TreasuryMonitor | `internal/treasury` | treasury / minter / hot wallet / YD 余额 + 新鲜度告警 |

辅助包：`internal/chain`（ABI / topic 解码）、`internal/metrics`（Prometheus）、
`internal/config`（env 加载）。

模块内部禁止循环依赖；`cmd/worker/main.go` 只做装配。

---

## 2. 目录结构

```text
apps/worker/
├── cmd/worker/
│   ├── main.go              # 启动入口；详见 §3
│   └── main_test.go
├── go.mod                   # github.com/x-web3/worker · Go 1.25
└── internal/
    ├── config/              # dotenv 加载
    ├── indexer/             # 链事件索引（最重；含 reorg + checkpoint）
    │   ├── runner.go        # 主循环（WS 订阅 + HTTP poll fallback）
    │   ├── checkpoint.go    # chain_checkpoints PG 持久化
    │   ├── reorg.go         # HandleReorg + ManualRewind
    │   └── client.go        # RPC client (WS/HTTP) + RPCPool
    ├── order/               # 事件 → DB 落库（Confirmer）
    │   └── confirmer.go     # 单事务：chain_events → orders → enrollments → outbox
    ├── chain/               # ABI 编码：CoursePurchasedTopic + Decode
    ├── reconcile/           # gap scanner + DLQ writer
    ├── certificate/         # mint signer + chain tx params + Consumer
    ├── treasury/            # treasury / hot wallet 监控
    └── metrics/             # Prometheus 端点 + 内部指标聚合
```

---

## 3. 启动顺序

`cmd/worker/main.go` 的装配序列（详见文件头注释）：

```text
1. config.LoadDotenv                       // 找不到 .env 不 fatal（prod 走真 env）
2. slog.NewJSONHandler(os.Stdout)
3. PostgreSQL pool (pgx)                   // 必须可连；否则 exit(1)
4. 读 env：CHAIN_ID / CONFIRM_DEPTH / CONSUMER / METRICS_ADDR / RPC_URLS / WS_URL …
5. Indexer（如 RPC_URLS 或 WS_URL 任一非空）：
     - Dial 出 RPC client（WS 优先放 primary）
     - NewRPCPool（多 RPC health window 30s）
     - NewRunner（订阅 / poll / batch / checkpoint / reorg callback）
     - runner.Start(ctx)
6. Confirmer 装配（pool）
7. Reconcile（如 RECONCILE_ENABLED != "0"）：
     - reconcile.NewScanner(pool, DLQWriter, …)
     - 后台 ticker，每轮 ScanOnce 后调 metrics.SetReconcileSnapshot
8. CertConsumer（如 rpcPool != nil + SIGNER_DRIVER 非空 + CERT_NFT_ADDRESS 非零）：
     - SignerConfigFromEnv → NewMintSigner → ChainTxParams
     - NewConsumer，Run(ctx) 进 goroutine
9. TreasuryMonitor（如 rpcPool != nil + 任一监控地址非空）：
     - treasury.LoadConfigFromEnv → NewMonitor → monitor.Start(ctx)
10. metrics.Register + metrics.Start      // 起 /metrics HTTP server
11. main 循环：每 2s 从 in-memory queue（pend）取 ApplyInput 调 Confirmer.Apply
12. SIGINT/SIGTERM → 优雅退出
```

「任一配置缺失」对应「跳过该子系统并 log warn」的 short-circuit 行为：

| 子系统 | 跳过条件 |
|---|---|
| Indexer | `WORKER_RPC_URLS` 与 `WORKER_WS_URL` 都为空 |
| Reconcile | `RECONCILE_ENABLED=0` |
| CertConsumer | `rpcPool == nil` 或 `SIGNER_DRIVER` 空 或 `CERT_NFT_ADDRESS` 零地址 |
| TreasuryMonitor | `rpcPool == nil` 或所有监控地址 + YD 都空 |

这让本地开发可以「跑 worker 但不配齐所有 RPC / 签名器」。

---

## 4. Indexer 详解（`internal/indexer`）

### 4.1 状态机

```text
           ┌──── WS ok ─────┐
startup───▶│  subscribeHead │──new head──▶ backfill(range next..head)
           └────┬───────────┘
                │ subscribeErr
                ▼
         ┌────────────┐
         │ pollCycle  │ 每 PollInterval 拉一次 head
         └────┬───────┘
              ▼
       backfill → 推进 checkpoint → emit event
```

### 4.2 多 RPC fallback

- 启动时构造 primary + secondary 多个 `Client`；
- 主用 primary；任何错误（除 ctx 取消）→ 标记 primary unhealthy；
- `health_window` 内不重试，循环 secondary；窗口结束后回到 primary；
- secondary 也挂 → 退化到纯 polling。

### 4.3 Reorg 检测（`internal/indexer/reorg.go`）

三类来源：

1. **深度错位**：推进 checkpoint 时若 `next_block-1` 的实际 hash 与
   `last_block_hash` 不符 → `HandleReorg`。
2. **WS 推送 removed log**：`ethclient.SubscriptionFilterLogs` 在 reorg 时
   重发 `removed=true` 的旧 log。
3. **手动 rewind**：API 端 `POST /admin/chain/rewind` → `ManualRewind`。
   跨 app 锁契约详见 [§6](#6-跨-app-契约)。

副作用都在同一事务：

```sql
BEGIN;
  SELECT ... FOR UPDATE ON chain_checkpoints(chain_id, consumer);  -- 串行化
  UPDATE chain_events SET canonical=false
    WHERE chain_id=$1 AND block_number >= $2;
  UPDATE orders      SET status='reorged', failure_code='EVENT_REORGED'
    WHERE …（同上 filter）;
  INSERT INTO chain_reorgs(...);
  UPDATE chain_checkpoints SET next_block=$2, last_block_hash=NULL;
COMMIT;
```

`enrollments` 不动：reorg 后若同笔 tx 在新链仍 confirmed，`Confirmer.Apply`
走幂等路径会再次推到 confirmed；`enrollments(user_id, course_id)` 唯一约束
保证不会重复发证。

### 4.4 Checkpoint（`internal/indexer/checkpoint.go`）

`chain_checkpoints(chain_id, consumer, next_block, last_block_hash)`：

- 退出前 flush；
- 启动时恢复；
- 跨 app 锁契约（与 API admin rewind 互斥）见 [§6](#6-跨-app-契约)。

---

## 5. Confirmer 详解（`internal/order`）

`Confirmer.Apply(ctx, ApplyInput)` 是 worker 的「落库原子」入口。一个事务
里做四件事（详见文件头注释）：

```sql
-- 1) 事件落库：unique (chain_id, tx_hash, log_index)
INSERT INTO chain_events(...) ON CONFLICT DO NOTHING;

-- 2) 推进订单：WHERE status IN ('submitted','confirming') 守卫
UPDATE orders SET status='confirmed'|'failed'|'reorged', block_number, log_index,
                  block_hash, confirmed_at, failure_code
  WHERE id=$1 AND status IN ('submitted','confirming');

-- 3) 派生 enrollment：unique (user_id, course_id)
INSERT INTO enrollments(user_id, course_id) ...;

-- 4) 通知其它系统
INSERT INTO outbox_events(aggregate, type, payload) ...;
```

幂等与 reorg 保护要点：

- `chain_events` 唯一约束 `(chain_id, tx_hash, log_index)`：同 tx 不同 log_index
  （multi-log tx）允许重复插入。
- `orders.status IN ('submitted','confirming')` 是 UPDATE 守卫——已 `confirmed
  / failed / reorged` 的订单不会被回退中间态。
- `enrollments(user_id, course_id)` 唯一约束保证不出现双重选课。

`cmd/worker` 把 Confirmer 适配到 `indexer.LogDecoder`：

- `cmd/worker/indexerLogDecoder.Decode` 用 `chain.Decode` 解码
  `CoursePurchased`；topic mismatch → 静默 skip；`ErrLogRemoved` → 走 reorg。
- `cmd/worker/confirmerAdapter.Apply` 适配到 `indexer.Confirmer` 接口。
- 主 goroutine 还有一条 in-memory queue（`pend`）做 in-process 重试：失败后
  把 input 推回队列，下一轮 ticker 再处理。

---

## 6. 跨 app 契约

> **必须与 [apps/api](../api/README.md) 同步**。下列字段 / 算法 / 锁是 SSOT。

### 6.1 事件 topic 与 ABI（`internal/chain`）

`chain.CoursePurchasedTopic = keccak256("CoursePurchased(bytes32,address,address,uint256,bytes16,uint256)")` 与
[CourseMarket.sol](../../packages/contracts/src/CourseMarket.sol) 事件签名严格一致。

### 6.2 `courseKey` 算法（sha256，非 keccak256）

```text
courseKey = sha256(uuid 16 字节 binary) → 32 字节 hex（带 0x）
```

与 [apps/web checkout derive](../../apps/web/src/features/checkout/derive.ts)、
[apps/api order.Service](../../apps/api/internal/order/order.go)、合约事件
ABI 必须一致。**历史上 doc 曾误写为 keccak256，实际是 sha256**。

### 6.3 `intentId` 截断

合约 `CoursePurchased(intentId)` 字段是 `bytes16`，对应 UUID 高 128 位。
worker 收到事件后用 `uuid.FromBytes(in.Event.IntentID[:])` 把高 128 位还原
成 UUID，再去查 `orders.intent_id`；前 16 字节必须能落到 `purchase_intents.id`
的高 128 位上。

### 6.4 `chain_checkpoints` 行锁契约（critical）

worker `HandleReorg / ManualRewind` 与 API `admin.handlers.chain_rewind`
**必须**在事务内获取 `chain_checkpoints(chain_id, consumer)` 的 `FOR UPDATE`
行锁。锁在事务 commit/rollback 时释放；**不要持锁发 RPC**。

锁的作用：

- 把同一 `(chain_id, consumer)` 的 rewind 操作串行化；
- 避开 worker 自动检测与 admin 手动 rewind 之间的 race。

---

## 7. CertConsumer（`internal/certificate`）

`Consumer.Run(ctx)` 主循环：

1. 拉 `certificate_jobs` 中 `status='pending'` 的任务；
2. 用 Signer 发 `CertificateNFT.mint(...)` 交易；
3. 等 `ConfirmDepth` 区块确认；
4. 把 `tx_hash / block_number / nft_token_id / token_uri` 写回 jobs；
5. 失败 → DLQ（`apps/api/admin/dlq` 可手动 retry）。

启动条件（任一缺失 → log warn 跳过）：

- `rpcPool != nil`
- `SIGNER_DRIVER` 非空（`anvil` / `keystore` / `kms`）
- `CERT_NFT_ADDRESS` 非零地址

签名器工厂（`NewMintSigner`）按 driver 派发：

- `anvil` — 本地 keystore，Anvil 默认私钥；仅本地开发。
- `keystore` — 加密 JSON 文件 + 密码；适合测试链 staging。
- `kms` — AWS KMS 异步签名；生产链必经路径。

`ChainTxParams(rpcPool, 0)` 每笔交易前实时从 `eth_getTransactionCount`
取 nonce，避免本地缓存漂移。

---

## 8. Reconcile（`internal/reconcile`）

`Scanner.ScanOnce()` 检查区间 `[next_block - ConfirmDepth, lastIndexed]`：

- 若 `chain_events` 中最大 `block_number` 比 checkpoint 落后超过阈值 → 写一条
  DLQ `gap` 事件，供 API `/admin/dlq` 列表与排查。
- 默认间隔 `RECONCILE_INTERVAL_MINUTES=30`；可用 `RECONCILE_ENABLED=0` 关闭。

指标：

- `reconcile_last_scan_unix`
- `reconcile_scan_runs_total`
- `reconcile_gap_detected_total`

---

## 9. TreasuryMonitor（`internal/treasury`）

监控目标（env 任一非空即启用）：

- `TREASURY_ADDRESSES` — 多个 treasury 地址，余额下限告警。
- `MINTER_ADDRESS` — Worker 自己的签名地址。
- `HOT_WALLET_ADDRESSES` — 热钱包，余额上下限告警。
- `YD_TOKEN_ADDRESS` — YD 余额 + 数据新鲜度。

指标：`worker_treasury_alerts_total{address,asset,severity}`。

空配置 short-circuit 让本地开发可以跑 worker 而不必先配监控目标；详见
`cmd/worker.startTreasuryMonitor` 注释。

---

## 10. Metrics（`internal/metrics`）

`/metrics` 端点暴露：

| 来源 | 指标 |
|---|---|
| Indexer | `worker_indexer_*`（heads / logs / rpc / gap / checkpoint / ws / http / drain） |
| CertConsumer | `worker_cert_*`（mint attempts / successes / failures / lag） |
| Reconcile | `reconcile_*`（scan / gap） |
| Chain lag | `chain_lag_blocks = head - next_block`（从 `chain_checkpoints` + RPC head） |

`/metrics` 的端口由 `WORKER_METRICS_ADDR` 控制；默认 `:9090`。

---

## 11. 配置

最小本地运行：

```bash
DATABASE_URL=postgres://…

# Indexer
WORKER_RPC_URLS=https://sepolia.infura.io/v3/<KEY>     # 多个用逗号分隔
# WORKER_WS_URL= wss://…                                # 可选；订阅用
WORKER_CHAIN_ID=11155111
CHAIN_CONFIRMATION_DEPTH=12
WORKER_POLL_INTERVAL_SECONDS=5
WORKER_BATCH_SIZE=1000
WORKER_RPC_HEALTH_WINDOW_SECONDS=30

# Confirmer（in-process queue）
WORKER_CONSUMER=indexer
WORKER_DEBUG_PENDING=

# Reconcile
RECONCILE_ENABLED=1
RECONCILE_INTERVAL_MINUTES=30

# CertConsumer（如启用）
SIGNER_DRIVER=anvil                                     # anvil/keystore/kms
CERT_NFT_ADDRESS=0x…
CERT_SIGNER_KEYSTORE_PATH=…                             # keystore driver
CERT_SIGNER_KEYSTORE_PASSWORD=…
KMS_KEY_ID=…                                            # kms driver
KMS_REGION=us-east-1

# TreasuryMonitor（如启用）
TREASURY_ADDRESSES=0x…
MINTER_ADDRESS=0x…
HOT_WALLET_ADDRESSES=0x…
YD_TOKEN_ADDRESS=0x…

# Metrics
WORKER_METRICS_ADDR=:9090
```

`SIGNER_DRIVER=kms` 在主网前必须配置；当前测试链可用 `anvil`。

---

## 12. 本地开发

```bash
# 1) 数据库迁移（顶层命令）
pnpm db:migrate

# 2) 启动 worker（自动加载仓库根 .env）
pnpm worker:dev
# 等价：cd apps/worker && go run ./cmd/worker

# 3) 验证（连真实 Sepolia RPC）
curl http://localhost:9090/metrics | grep worker_

# 4) 本地 Anvil + 假链闭环（不依赖 Sepolia）
# 详见 docs/dev/anvil-loop.md
```

---

## 13. 测试

```bash
# 全部 unit + integration 测试
pnpm worker:test
# 等价：cd apps/worker && go test ./...

# 单独跑某个包
cd apps/worker && go test ./internal/indexer/... -v
cd apps/worker && go test ./internal/order/...    -v
cd apps/worker && go test ./internal/reconcile/... -v

# reorg 集成测试（需要 testcontainers）
cd apps/worker && go test ./internal/indexer/ -run TestReorg -v

# 覆盖率
cd apps/worker && go test ./... -coverprofile=cover.out && go tool cover -html=cover.out
```

测试矩阵：

| 包 | 工具 | 覆盖 |
|---|---|---|
| `internal/chain` | 单测 | ABI 解码 / 边界 / removed log |
| `internal/indexer` | 单测 + testcontainers | runner / reorg（人工 + 合成 + 集成）/ checkpoint / client pool |
| `internal/order` | 单测 + testcontainers | Confirmer.Apply（已 confirmed 不回退、enrollment 幂等、outbox 写入）|
| `internal/reconcile` | 单测 + 集成 | gap 检测、DLQ writer |
| `internal/certificate` | 单测 + 集成 | signer / chain tx params / consumer end-to-end |

---

## 14. 部署与运维

- **运行模式**：systemd 单进程；不要把 `cmd/worker` 拆成多个容器（共享 in-memory queue）。
- **签名私钥**：`SIGNER_DRIVER=kms` 走 AWS KMS；当前测试链 `anvil` / `keystore`
  私钥**禁止**进入镜像或 `.env` 提交。
- **RPC**：生产用 Alchemy / Infura，逗号分隔多个 fallback。WS 优先用于订阅。
- **Reorg / DLQ**：参见 [docs/runbooks/chain-replay.md](../../docs/runbooks/chain-replay.md)
  与 [docs/runbooks/dlq-recovery.md](../../docs/runbooks/dlq-recovery.md)。
- **运维命令**：通过 API `POST /admin/chain/rewind`、`GET /admin/dlq`、
  `GET /admin/chain/sync` 触发；不要直接改 DB。
- **告警**：Prometheus 接 `worker_indexer_*` 与 `chain_lag_blocks`；阈值
  视业务定（建议 lag > 10 blocks 触发）。

---

## 15. 进一步阅读

- 全局架构：[docs/ARCHITECTURE.md](../../docs/ARCHITECTURE.md)
- 产品流程：[docs/PRODUCT-FLOWS.md](../../docs/PRODUCT-FLOWS.md)
- 链回放 runbook：[docs/runbooks/chain-replay.md](../../docs/runbooks/chain-replay.md)
- DLQ 恢复 runbook：[docs/runbooks/dlq-recovery.md](../../docs/runbooks/dlq-recovery.md)
- 签名轮换 runbook：[docs/runbooks/signer-rotation.md](../../docs/runbooks/signer-rotation.md)
- 合约：[packages/contracts](../../packages/contracts)
- API 同伴：[apps/api](../api/README.md)