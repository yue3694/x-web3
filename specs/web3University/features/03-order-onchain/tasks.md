# F03 — 订单与链上购买 任务清单

## 任务列表

- [ ] **F03-T01** CourseMarket.sol：configureCourse + buyCourse + 防重购 + 事件 `contracts:packages/contracts/src/CourseMarket.sol` ~6h
- [ ] **F03-T02** CourseMarket 单测 + fuzz + invariant（资金守恒/防重购） `contracts:packages/contracts/test/CourseMarket.t.sol` ~6h
- [ ] **F03-T03** 部署脚本：CourseMarket 配置 + 角色转移 `contracts:packages/contracts/script/DeployCourseMarket.s.sol` ~3h
- [ ] **F03-T04** ABI 导出 + chain registry（market / token 地址按 chain） `contracts+web:apps/web/src/contracts/market.ts` ~2h
- [ ] **F03-T05** migration：course_prices / purchase_intents / orders / chain_events / chain_checkpoints / outbox_events `database:database/migrations/0003_order.sql` ~4h
- [ ] **F03-T06** API：创建购买意图（含 courseKey、price_version、过期、idempotency） `api:apps/api/internal/order/intent.go` ~5h
- [ ] **F03-T07** API：POST /orders/{intentId}/transactions { txHash }（仅标记 submitted） `api:apps/api/internal/order/submit.go` ~3h
- [ ] **F03-T08** API：GET /orders/{id}（owner/admin） `api:apps/api/internal/order/query.go` ~2h
- [ ] **F03-T09** Worker 初始化：WS 监听 + HTTP 回扫 + checkpoint + RPC fallback + 优雅退出 `worker:apps/worker/cmd/worker/main.go,internal/indexer/` ~10h
- [ ] **F03-T10** CoursePurchased decoder + receipt 全字段校验 `worker:apps/worker/internal/chain/decoder.go` ~6h
- [ ] **F03-T11** 原子事务：chain_events → orders → enrollments → outbox `worker:apps/worker/internal/order/` ~8h
- [ ] **F03-T12** reorg 检测 + checkpoint 推进规则 + 回放 API `worker+api:apps/worker/internal/indexer/reorg.go,apps/api/internal/admin/` ~6h
- [ ] **F03-T13** reconcile：定时漏块扫描 + DLQ 告警 `worker:apps/worker/internal/reconcile/` ~4h
- [ ] **F03-T14** OpenAPI：order + chain-sync `shared:packages/shared/openapi/order.yaml` ~4h
- [ ] **F03-T15** 前端：购买交易状态机组件 + wagmi 集成 `web:apps/web/src/features/checkout/` ~10h
- [ ] **F03-T16** 前端：MyOrders（订单列表 + 链上状态） `web:apps/web/src/features/account/MyOrders.tsx` ~4h
- [ ] **F03-T17** 集成测试：伪造 tx/错误链/错误买家/重复消费/reorg `worker+api:**/*_test.go` ~10h
- [ ] **F03-T18** Anvil 全栈 E2E：登录→购买→同步→enrollment（占位） `qa:tests/e2e/purchase.spec.ts` ~6h

## 依赖与并行

- **依赖**：F01（RBAC）、F02（courseKey/价格）、F05 决议（YD Token 地址）。
- **可并行**：合约 T-01～T-03 与 API T-05/06 并行；前端 T-15 可基于 OpenAPI mock 并行。
- **阻塞下游**：F04-T01（enrollment 由本特性派生）。

## 退出条件（DoD）

- [ ] `forge test` 全绿，CourseMarket 覆盖率 ≥ 90%。
- [ ] Worker reorg / 重复消费 / 漏块全部测试覆盖。
- [ ] AC-006、AC-007、AC-008、AC-009、AC-010 通过。
- [ ] `expectedAmount` 与 `intentId` 防 price tampering 攻击验证。
- [ ] Anvil E2E：完整购买→确认→enrollment。

## 风险

- **confirmation depth 与 UX 矛盾**：太短易 reorg，太长 UX 差；建议 Sepolia = 12 块 ≈ 36s。
- **RPC 限流**：批量回扫必须分块 + 退避；订阅多链时尤其注意。
- **课程价格波动**：MVP 用 YD 单价，OQ-004 决定是否引入稳定币。