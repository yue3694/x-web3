# F03 — 订单与链上购买（Order & On-chain Purchase）

> 来源：上级 `requirements.md` F-015 ~ F-023；本特性在 monorepo 中的实现切片。

## 1. 范围

- `courseKey` 与课程 ID 不可变映射；价格版本化。
- 购买意图（intent）创建 + 过期 + idempotency；tx hash 提交；订单状态机。
- Worker 链监听 + receipt 全字段校验 + 幂等事件入库 + enrollment 派生。
- Reorg / RPC 故障 / 漏块 / 重复事件的恢复与人工对账。

## 2. 功能需求

| ID | 描述 | 验收 |
|---|---|---|
| **R-OR-001** | `courseKey = keccak256(course_id)` 在创建课程时确定，不可变更 | AC-006 |
| **R-OR-002** | 已发布付费课程须保存 `chain_id / token / amount / decimals / market / price_version`，价格变更 → 新版本 | AC-006 |
| **R-OR-003** | 创建购买意图时冻结 `price_version`，过期时间 15 min；同一 idempotency key 幂等 | AC-006 |
| **R-OR-004** | 前端必须先 `allowance` 检查并 `approve`，再 `buyCourse(courseKey, amount, intentId)` | AC-007 |
| **R-OR-005** | UI 显式状态机：`idle → checking → approvalRequired → approving → purchasing → confirming → success/error` | E2E |
| **R-OR-006** | 订单状态机：`created / submitted / confirming / confirmed / failed / expired / reorged`，全审计 | AC-007 |
| **R-OR-007** | Worker 按 `(chain_id, tx_hash, log_index)` 幂等消费，达确认数后置 `confirmed` + enrollment | AC-007、AC-009 |
| **R-OR-008** | API 必须校验 receipt / 合约地址 / 事件签名 / buyer / course / token / amount / chainId 才能授予 | AC-008 |
| **R-OR-009** | Reorg / RPC 失败 / 重复事件可恢复；支持按区块范围回放 + 人工对账 | AC-010 |

## 3. 数据模型

```sql
course_prices(id, course_id, version, chain_id, token_address, amount numeric(78,0), decimals int, market_address, valid_from, valid_to nullable, unique(course_id, version, chain_id))
purchase_intents(id uuid, user_id, wallet_id, course_id, price_id fk, idempotency_key unique, expires_at, status enum)
orders(id, intent_id unique, status, chain_id, tx_hash nullable, block_number nullable, log_index nullable, confirmed_at, failure_code, unique(chain_id, tx_hash) WHERE tx_hash IS NOT NULL)
chain_events(chain_id, tx_hash, log_index, block_number, block_hash, event_signature, payload JSONB, canonical bool, primary key(chain_id, tx_hash, log_index))
chain_checkpoints(chain_id, consumer, next_block, last_block_hash, updated_at, unique(chain_id, consumer))
outbox_events(id, aggregate, type, payload JSONB, published_at nullable, created_at)
```

## 4. 合约接口

```solidity
interface ICourseMarket {
  function buyCourse(bytes32 courseKey, uint256 expectedAmount, bytes16 intentId) external;
  function configureCourse(bytes32 courseKey, address token, uint256 amount, uint256 priceVersion) external;
  event CourseConfigured(bytes32 indexed courseKey, address token, uint256 amount, uint256 priceVersion);
  event CoursePurchased(bytes32 indexed courseKey, address indexed buyer, address token, uint256 amount, bytes16 intentId, uint256 priceVersion);
}
```

合约要求：CEI + nonReentrant + whenNotPaused；`(buyer, courseKey)` 默认防重复购买。

## 5. 端到端流程

```text
1. POST /courses/{id}/purchase-intents
   → API 在 tx 中创建 purchase_intents（含 courseKey、price_version、chain、market、expires_at）
2. Web 用 wagmi:
   a. 检查 chainId
   b. 检查 ERC20 balance + allowance
   c. 若 allowance < amount → approve(max)
   d. buyCourse(courseKey, amount, intentId)
3. POST /orders/{intentId}/transactions { txHash }
   → 订单 status=submitted（不授予）
4. Worker（apps/worker）:
   a. 监听 CoursePurchased event
   b. 等到 confirmation_depth
   c. 全字段校验：market / token / amount / buyer / courseKey / intentId / priceVersion
   d. tx 内：INSERT chain_events（unique 键幂等） → UPDATE orders → INSERT enrollments → outbox
   e. checkpoint 推进
5. Web 用 useEffect + refetch 拉订单状态，confirmed 后才显示"已购买"
```

## 6. 错误码

| code | http | 含义 |
|---|---|---|
| `INTENT_EXPIRED` | 410 | 购买意图过期 |
| `PRICE_VERSION_MISMATCH` | 409 | 价格版本已变更 |
| `INVALID_TX_RECEIPT` | 422 | receipt 校验失败 |
| `ALREADY_PURCHASED` | 409 | 该钱包已购买该课程 |
| `EVENT_REORGED` | 409 | 事件被重组，订单冻结 |
| `RPC_UNAVAILABLE` | 503 | RPC 不可用，提示重试 |

## 7. 非功能需求

- 创建购买意图 P95 ≤ 200 ms。
- Worker 事件消费到订单 confirmed P95 ≤ 30 s（含 confirmation depth）。
- reorg 检测：每事件额外保存 `block_hash`；checkpoint 推进前对比 `last_block_hash`。

## 8. 边界

- **不在范围内**：退款、讲师分账、税务（OQ-003/004 待决）。
- **依赖**：F01（RBAC）、F02（courseKey/价格版本）、F05（YD Token 接口）。
- **被依赖**：F04（enrollment 由本特性派生）。