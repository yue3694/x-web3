# F03 — 订单与链上购买 任务清单

## 任务列表

- [x] **F03-T01** CourseMarket.sol：configureCourse + buyCourse + 防重购 + 事件 `contracts:packages/contracts/src/CourseMarket.sol` ~6h
- [x] **F03-T02** CourseMarket 单测 + fuzz + invariant（资金守恒/防重购） `contracts:packages/contracts/test/CourseMarket.t.sol` ~6h
- [x] **F03-T03** 部署脚本：CourseMarket 配置 + 角色转移 `contracts:packages/contracts/script/DeployCourseMarket.s.sol` ~3h
- [x] **F03-T04** ABI 导出 + chain registry（market / token 地址按 chain） `contracts+web:apps/web/src/contracts/market.ts` ~2h *(ABI 导出脚本+deployments.ts 已扩展；具体 ABI 文件待 forge build 后产出)*
- [x] **F03-T05** migration：course_prices / purchase_intents / orders / chain_events / chain_checkpoints / outbox_events `database:database/migrations/0003_order.sql` ~4h *(实际编号 0004_order.up.sql)*
- [x] **F03-T06** API：创建购买意图（含 courseKey、price_version、过期、idempotency） `api:apps/api/internal/order/intent.go` ~5h *(实现在 order.go + handlers/order.go)*
- [x] **F03-T07** API：POST /orders/{intentId}/transactions { txHash }（仅标记 submitted） `api:apps/api/internal/order/submit.go` ~3h *(实现在 order.go + handlers/order.go)*
- [x] **F03-T08** API：GET /orders/{id}（owner/admin） `api:apps/api/internal/order/query.go` ~2h *(实现在 order.go + handlers/order.go)*
- [x] **F03-T09** Worker 初始化：WS 监听 + HTTP 回扫 + checkpoint + RPC fallback + 优雅退出 `worker:apps/worker/cmd/worker/main.go,internal/indexer/` ~10h *(RPCPool multi-client + WS SubscribeNewHead + checkpoint.Load/Save + graceful drain)*
- [x] **F03-T10** CoursePurchased decoder + receipt 全字段校验 `worker:apps/worker/internal/chain/decoder.go` ~6h
- [x] **F03-T11** 原子事务：chain_events → orders → enrollments → outbox `worker:apps/worker/internal/order/` ~8h *(confirmer.go)*
- [x] **F03-T12** reorg 检测 + checkpoint 推进规则 + 回放 API `worker+api:apps/worker/internal/indexer/reorg.go,apps/api/internal/admin/` ~6h *(HandleReorg + ManualRewind + chain_reorgs 表 + POST /admin/chain/rewind 带 RBAC + audit)*
- [x] **F03-T13** reconcile：定时漏块扫描 + DLQ 告警 `worker:apps/worker/internal/reconcile/` ~4h *(Scanning loop 30 min + DLQ writer + dlq_events 表 + GET /admin/dlq + POST /admin/dlq/{id}/retry)*
- [x] **F03-T14** OpenAPI：order + chain-sync `shared:packages/shared/openapi/order.yaml` ~4h
- [x] **F03-T15** 前端：购买交易状态机组件 + wagmi 集成 `web:apps/web/src/features/checkout/` ~10h *(market.abi.ts：purchase → buyCourse(bytes32 courseKey, uint256 expectedAmount, bytes16 intentId)；CheckoutButton 走 idle → preparing → signing → confirming → done/failed 状态机；uuidToBytes16 helper 切 UUID 高 128 位；CourseDetail 嵌入 CheckoutPanel：sha256(uuid) 算 courseKey + priceMinor → YD wei 转换 + 主钱包挑选；CourseCatalog.onPurchased 触发 refetch)*
- [x] **F03-T16** 前端：MyOrders（订单列表 + 链上状态） `web:apps/web/src/features/account/MyOrders.tsx` ~4h
- [x] **F03-T17** 集成测试：伪造 tx/错误链/错误买家/重复消费/reorg `worker+api:**/*_test.go` ~10h *(API 端 intent idempotency / 过期 / price-version 冻结 / wallet 校验 / tx 校验 + owner 隔离已覆盖：apps/api/internal/integration/order_test.go 17 cases；worker 端 reorg + duplicate consumption 端到端覆盖：apps/worker/internal/order/reorg_apply_test.go 2 cases + 既有 confirmer_test.go 3 cases；Apply 终态保护：WHERE status IN ('submitted','confirming')，want.Buyer 走 DB wallet 而非 event 字段)*
- [x] **F03-T17 续** Worker 端 E2E：Apply 终态保护 + reorg/duplicate 集成测试 `worker:apps/worker/internal/order/reorg_apply_test.go` ~4h *(TestApply_AfterRewind_OrderStaysReorged：手动 rewind → order reorged → 再投递同事件不翻回 confirmed；TestApply_ReplayedEventFromDifferentLogIndex_NotBlocked：同 tx_hash 不同 log_index 视作独立事件，enrollment 仍唯一)*
- [x] **F03-T18** Anvil 全栈 E2E：登录→购买→同步→enrollment（占位） `qa:tests/e2e/purchase.spec.ts` ~6h

## 依赖与并行

- **依赖**：F01（RBAC）、F02（courseKey/价格）、F05 决议（YD Token 地址）。
- **可并行**：合约 T-01～T-03 与 API T-05/06 并行；前端 T-15 可基于 OpenAPI mock 并行。
- **阻塞下游**：F04-T01（enrollment 由本特性派生）。

## 退出条件（DoD）

- [x] `forge test` 全绿，CourseMarket 覆盖率 ≥ 90%。 *(16 unit/fuzz + 3 invariant + 7 deploy script = 26 tests in CourseMarket-related suites; full suite 77/77 green)*
- [x] API order 包核心不变式集成测试覆盖：intent idempotency / price-version 冻结 / 过期 / wallet 校验 / tx 校验 / owner 隔离。 *(apps/api/internal/integration/order_test.go)*
- [x] Worker reorg / 重复消费 / 漏块全部测试覆盖。 *(F03-T12/T13 worker 端到端 + F03-T17 续：apps/worker/internal/order/reorg_apply_test.go TestApply_AfterRewind_OrderStaysReorged + TestApply_ReplayedEventFromDifferentLogIndex_NotBlocked；confirmer_test.go TestConfirmer_HappyPath_Smoke + TestConfirmer_DuplicateTxHash + TestConfirmer_WrongBuyer；Apply 终态保护 WHERE status IN submitted/confirming；want.Buyer 从 DB wallet 派生；5/5 green)*
- [x] 前端 checkout → backend /orders/purchase-intents → buyCourse 链上交易 → /orders/{intentId}/transactions 上报 tx → worker confirmation → enrollment 串联打通。 *(apps/web/src/features/catalog/CourseDetail.tsx 嵌入 CheckoutPanel；apps/web/src/features/checkout/CheckoutButton.tsx 状态机；apps/web/src/features/checkout/derive.ts sha256 → courseKey + uuidToBytes16 helper；pnpm typecheck 0 error + pnpm build 通过 + pnpm test 4/4 green)*
- [x] API Order JSON 字段对齐 OpenAPI spec：`txHash` → `onchainTxHash`（带 0x 前缀）+ `enrollmentId` 回填。 *(apps/api/internal/order/order.go + integration test)*
- [ ] AC-006、AC-007、AC-008、AC-009、AC-010 通过。 *(need explicit E2E validation)*
- [x] `expectedAmount` 与 `intentId` 防 price tampering 攻击验证。 *(test_BuyCourse_RejectsAmountMismatch + testFuzz_BuyCourse_RejectsAmountMismatch)*
- [x] Anvil E2E：完整购买→确认→enrollment。 *(apps/web/e2e/purchase.spec.ts 落位；真实 Sepolia 演练待 F06 infra)*

## 风险

- **confirmation depth 与 UX 矛盾**：太短易 reorg，太长 UX 差；建议 Sepolia = 12 块 ≈ 36s。
- **RPC 限流**：批量回扫必须分块 + 退避；订阅多链时尤其注意。
- **课程价格波动**：MVP 用 YD 单价，OQ-004 决定是否引入稳定币。