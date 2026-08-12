# F03 — 订单与链上购买 设计

## 1. monorepo 落点

```text
packages/contracts/src/
├── CourseMarket.sol         # 配置 + buyCourse + 防重复 + 事件
├── interfaces/ICourseMarket.sol
└── libraries/Bytes16.sol    # intentId 类型工具

apps/api/internal/
├── order/                   # 购买意图、tx 提交、订单查询
└── admin/                   # chain-sync 状态查询、回放（与 F06 共享）

apps/worker/                                  # 新建独立 worker
├── cmd/worker/main.go
└── internal/
    ├── indexer/             # WS + HTTP 回扫 + checkpoint + reorg
    ├── chain/               # event decoder + receipt 全字段校验
    ├── order/               # 事件入库 + 订单确认 + enrollment 派生
    ├── reconcile/           # 定时对账 + 漏块补单
    └── jobs/                # 通用 runner（指数退避、DLQ）

apps/web/src/features/checkout/
├── PurchaseButton.tsx       # 状态机组件
├── usePurchaseMachine.ts    # xstate 或 reducer
└── TxStatusBadge.tsx

apps/web/src/features/account/
└── MyOrders.tsx

database/migrations/0003_order.sql
packages/shared/openapi/order.yaml
packages/shared/events/courseMarket.ts      # 事件 ABI + 类型常量
```

## 2. Worker 架构

```text
┌─────────────┐     ┌─────────────┐     ┌──────────────┐
│ WS Listener │────▶│ Raw Event    │────▶│ Validator    │
└─────────────┘     │  Channel     │     │ (receipt +   │
                    └─────────────┘     │  全字段)      │
                                        └──────┬───────┘
                                               ▼
                                        ┌──────────────┐
                                        │ Confirmer    │
                                        │ (confirmation│
                                        │  depth wait) │
                                        └──────┬───────┘
                                               ▼
                  ┌─────────────────────────────────────────┐
                  │ Tx: chain_events → orders → enrollments │
                  │      → outbox_events                   │
                  └─────────────────────────────────────────┘

┌─────────────┐     ┌─────────────┐     ┌──────────────┐
│ HTTP Backfill│────▶│ Per-chain   │────▶│ Reorg Detect │
│ (cron 1m)   │     │ Checkpoint  │     │ (block_hash) │
└─────────────┘     └─────────────┘     └──────────────┘
```

## 3. receipt 校验清单

| 字段 | 校验 |
|---|---|
| `receipt.status` | = `success` (0x1) |
| `receipt.contractAddress` | == `course_prices.market_address` |
| `receipt.blockNumber` | >= `chain_checkpoints.next_block` |
| `receipt.confirmations` | >= 配置 `confirmation_depth` |
| `logs[*].address` | == market |
| `logs[*].topics[0]` | == `keccak("CoursePurchased(...)")` |
| decoded args | `courseKey == intent.course_key && buyer == intent.wallet && token == intent.token && amount == intent.amount && intentId == intent.id && priceVersion == intent.price_version` |

任何一项不符：`orders.failure_code = RECEIPT_MISMATCH`，进入 admin 对账。

## 4. 状态机

```text
created ──POST transactions──> submitted ──event seen──> confirming
                                                              │
                                                              ▼
                                                        confirmed ──> enrollment
                                                              │
                                          reorg detected──> reorged ──> 人工对账
                                                              │
                                          tx reverted─────> failed
                                                              │
                                          intent expires──> expired
```

## 5. 幂等与并发

- 三处唯一约束共同保证幂等：
  - `orders(chain_id, tx_hash) WHERE tx_hash IS NOT NULL`
  - `chain_events(chain_id, tx_hash, log_index)`
  - `enrollments(user_id, course_id)`
- Worker 多实例并发：使用 `SELECT ... FOR UPDATE SKIP LOCKED` 抢占 `chain_events`；重复消费命中 unique 直接视为成功。

## 6. reorg 处理

- checkpoint 推进时记录 `last_block_hash`；下一次监听该块若 hash 改变 → 标记该区块之后所有 provisional 事件为 `canonical=false`。
- `orders.status` 设为 `reorged`，触发 admin alert。
- 等待 canonical chain 重新确认；若是相同 tx 在新链上依然有效则恢复 `confirmed`，否则进入人工对账。

## 7. RPC 容灾

- 每个 chain 配置 `rpc_urls: [primary, secondary, ...]`，按 `CircuitBreaker` 切换。
- 批量回扫 `eth_getLogs` 分块 `from..to`，单块 1000～5000 events，避免 RPC 限流。
- 同步水位 = `latest_block - checkpoint.next_block`，> N 触发告警。

## 8. 测试策略

- **合约**：单测（配置/购买/防重复/事件）、fuzz（amount/intentId 边界）、invariant（资金守恒、防重购）。
- **Worker**：reorg 模拟（`anvil` 提供的 `anvil_mine` + `anvil_reorg`）、WS 断线重连、重复消费。
- **集成**：伪造 tx hash、错误链、错误买家、错误金额全部无法解锁。
- **E2E**：Anvil 全栈（API + Worker + Web）跑完购买闭环。

## 9. 安全检查

- [ ] `intents.expires_at` 后不能再用；worker 端也校验。
- [ ] `expectedAmount` 防止前端 price tampering。
- [ ] `intentId` 使用 UUID v4 → bytes16（高 128 位）；前后端一致。
- [ ] Worker signer 与 deployer 隔离；独立 KMS key。
- [ ] chain_admin replay 接口必须 `SYSTEM_ADMIN` + audit + 指定 `from/to_block` 范围。