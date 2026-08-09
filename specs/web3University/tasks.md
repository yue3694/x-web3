# Web3 University — 任务清单

## 架构

`monorepo + separated + web3`。执行顺序采用合约/契约先行并自底向上推进：决策与共享契约 → 数据库 → 合约 → API/Worker → Web → AWS → E2E。时间为单人有效开发时间的粗估，不含外部审核、等待测试网确认和产品决策。

## Phase 0：范围冻结与工程基线

- [ ] **T-001**：对 OQ-001～OQ-008 做产品/合规决策并写 ADR `docs:docs/adr/` ~4h
- [ ] **T-002**：定义目标 monorepo 包名、Go workspace、统一命令和 CI 门禁 `workspace:package.json,pnpm-workspace.yaml,go.work` ~2h
- [ ] **T-003**：建立 OpenAPI、链事件 schema、错误码和版本策略 `shared:packages/shared/` ~4h
- [ ] **T-004**：补充本地开发 compose（PostgreSQL、Redis、Anvil）与示例环境变量 `deploy:deploy/docker-compose.yml,.env.example` ~4h

## Phase 1：数据库与身份基础

- [ ] **T-005**：引入 migration 工具并创建 users、wallets、roles、permissions、关联表 `database:database/migrations/` ~4h
- [ ] **T-006**：创建 courses、versions、chapters、lessons、media、comments 表与索引 `database:database/migrations/` ~4h
- [ ] **T-007**：创建 price、intent、order、event、checkpoint、outbox 表与幂等约束 `database:database/migrations/` ~4h
- [ ] **T-008**：创建 enrollment、progress、completion、certificate、jobs、audit 表 `database:database/migrations/` ~4h
- [ ] **T-009**：实现 migration smoke/rollback 策略和真实 PostgreSQL 集成测试基座 `database:database/tests/` ~3h
- [ ] **T-010**：初始化 Go API 分层结构、配置、健康检查、日志、request ID `api:apps/api/` ~4h
- [ ] **T-011**：实现 Privy JWT verifier、JWKS 缓存和幂等开户 `api:apps/api/internal/auth/` ~6h
- [ ] **T-012**：实现钱包 nonce/签名绑定、唯一性冲突与解绑 `api:apps/api/internal/wallet/` ~6h
- [ ] **T-013**：实现 RBAC middleware/service、对象级授权和角色种子数据 `api:apps/api/internal/rbac/` ~6h
- [ ] **T-014**：为 Auth/RBAC/IDOR/重放攻击编写单元和集成测试 `api:apps/api/internal/**/*_test.go` ~5h

## Phase 2：智能合约与 ABI

- [ ] **T-015**：落实 YD tokenomics ADR 并定义 IYDToken/ICourseMarket/ICertificateNFT 接口 `contracts:packages/contracts/src/interfaces/` ~3h
- [ ] **T-016**：实现 YDToken（cap/roles/pause/permit 按 ADR） `contracts:packages/contracts/src/YDToken.sol` ~5h
- [ ] **T-017**：实现 CourseMarket 配置、SafeERC20 支付、重复购买保护与事件 `contracts:packages/contracts/src/CourseMarket.sol` ~8h
- [ ] **T-018**：实现 CertificateNFT 唯一证书、受限铸造及转让策略 `contracts:packages/contracts/src/CertificateNFT.sol` ~6h
- [ ] **T-019**：编写三份合约部署脚本、角色转移和本地/测试网配置 `contracts:packages/contracts/script/` ~5h
- [ ] **T-020**：完成合约单测、失败路径、fuzz/invariant 和覆盖率 ≥ 90% `contracts:packages/contracts/test/` ~12h
- [ ] **T-021**：运行静态安全扫描并完成第一轮人工安全审查和威胁模型 `contracts:docs/security/` ~6h
- [ ] **T-022**：扩展 ABI 导出和 chain deployment registry，生成前端/Worker 类型 `contracts+web:packages/contracts/script/,apps/web/src/contracts/` ~4h

## Phase 3：课程、媒体与审核 API

- [ ] **T-023**：实现课程/版本/章节/课时 repository 与 service `api:apps/api/internal/course/` ~8h
- [ ] **T-024**：实现老师草稿编辑、乐观锁和作者对象级权限 `api:apps/api/internal/course/` ~6h
- [ ] **T-025**：实现提交/审核/发布状态机和 audit log `api:apps/api/internal/review/` ~6h
- [ ] **T-026**：实现公开课程分页、筛选、稳定排序和缓存 `api:apps/api/internal/catalog/` ~5h
- [ ] **T-027**：实现 S3 预签名上传、媒体状态、校验和私有播放凭证 `api:apps/api/internal/media/` ~8h
- [ ] **T-028**：实现评论写入、购买校验、审核和软删除 `api:apps/api/internal/comment/` ~4h
- [ ] **T-029**：补齐课程/审核/媒体 OpenAPI 与 API 集成测试 `api+shared:packages/shared/openapi/,apps/api/` ~6h

## Phase 4：订单与链同步

- [ ] **T-030**：实现价格版本与购买意图创建、过期和 Idempotency-Key `api:apps/api/internal/order/` ~6h
- [ ] **T-031**：实现 tx hash 提交和订单查询，确保 pending 不授予访问权 `api:apps/api/internal/order/` ~4h
- [ ] **T-032**：初始化独立 Go Worker、任务循环、优雅退出和指标 `worker:apps/worker/` ~4h
- [ ] **T-033**：实现 CoursePurchased decoder、receipt 全字段校验和确认数策略 `worker:apps/worker/internal/chain/` ~8h
- [ ] **T-034**：实现事件原始入库、订单确认、enrollment 和 outbox 原子事务 `worker:apps/worker/internal/order/` ~8h
- [ ] **T-035**：实现 checkpoint、HTTP 回扫、RPC fallback、reorg 检测和指定区块回放 `worker:apps/worker/internal/indexer/` ~10h
- [ ] **T-036**：实现 reconciliation、重试、DLQ 和超管处置接口 `worker+api:apps/worker/internal/reconcile/,apps/api/internal/admin/` ~8h
- [ ] **T-037**：测试伪造 tx、错误事件、重复投递、崩溃恢复、漏块和 reorg `worker:apps/worker/internal/**/*_test.go` ~10h

## Phase 5：学习与证书

- [ ] **T-038**：实现 enrollment 授权和私有课时访问策略 `api:apps/api/internal/learning/` ~5h
- [ ] **T-039**：实现幂等且不倒退的学习进度、版本化完课规则 `api:apps/api/internal/learning/` ~6h
- [ ] **T-040**：实现 completion 与唯一 certificate job 的原子创建 `api:apps/api/internal/certificate/` ~4h
- [ ] **T-041**：实现 metadata 生成/固定、mint signer 和证书 mint Worker `worker:apps/worker/internal/certificate/` ~8h
- [ ] **T-042**：实现 mint receipt 确认、重试、DLQ 和证书查询 API `worker+api:apps/worker/internal/certificate/,apps/api/internal/certificate/` ~6h
- [ ] **T-043**：测试重复完课、重复 job、失败重试、非 minter、链重组和 metadata 完整性 `api+worker+contracts:apps/,packages/contracts/test/` ~8h

## Phase 6：单 Web 前端

- [ ] **T-044**：接入 Privy Provider、session bootstrap、权限 context 和受保护路由 `web:apps/web/src/auth/` ~6h
- [ ] **T-045**：实现公开课程列表/详情、分页筛选和响应式布局 `web:apps/web/src/features/catalog/` ~8h
- [ ] **T-046**：实现老师课程编辑器、章节排序、媒体上传和提交审核 `web:apps/web/src/features/teacher/` ~12h
- [ ] **T-047**：实现超管隐藏入口、课程审核、角色管理和审计日志 `web:apps/web/src/features/admin/` ~10h
- [ ] **T-048**：实现钱包/网络/余额检查和购买交易状态机 `web:apps/web/src/features/checkout/` ~10h
- [ ] **T-049**：实现学习播放器、进度同步、课程完成和错误恢复 `web:apps/web/src/features/learning/` ~10h
- [ ] **T-050**：实现个人中心的订单、enrollment、钱包和 NFT 证书展示 `web:apps/web/src/features/account/` ~8h
- [ ] **T-051**：生成 OpenAPI client，统一 API error、query cache 和 correlation ID `web+shared:packages/shared/,apps/web/src/api/` ~5h
- [ ] **T-052**：完成前端组件、权限、交易状态机、可访问性和响应式测试 `web:apps/web/src/**/*.test.tsx` ~10h

## Phase 7：Uniswap 与 Chainlink（条件阶段）

- [ ] **T-053**：依据 ADR 创建 YD/USDC 池、初始化价格和流动性运营 runbook `contracts+docs:packages/contracts/script/,docs/runbooks/` ~8h
- [ ] **T-054**：实现前端报价、滑点、deadline、price impact 和 swap receipt `web:apps/web/src/features/swap/` ~8h
- [ ] **T-055**：实现并测试明确的 Chainlink feed/Automation/Functions 用例及 stale/fallback 保护 `contracts+worker:packages/contracts/src/,apps/worker/internal/oracle/` ~10h

## Phase 8：AWS、可观测性与交付

- [ ] **T-056**：把现有静态站点 IaC 扩展为环境化 VPC、ALB、ECS、ECR `infra:infra/aws/` ~12h
- [ ] **T-057**：配置 RDS、Redis、SQS/DLQ、S3 media、CloudFront OAC 和备份 `infra:infra/aws/` ~12h
- [ ] **T-058**：配置 Secrets Manager/KMS、最小 IAM、WAF、CloudTrail 和安全组 `infra:infra/aws/` ~8h
- [ ] **T-059**：配置 API/Worker 日志、trace、dashboard 和业务/链同步告警 `infra+apps:infra/aws/,apps/api/,apps/worker/` ~8h
- [ ] **T-060**：建立 GitHub OIDC CI/CD、migration gate、镜像扫描和环境晋级 `ci:.github/workflows/` ~10h
- [ ] **T-061**：执行备份恢复、RPC 故障、Worker 崩溃、DLQ 回放和 signer 权限演练 `qa:docs/runbooks/` ~8h
- [ ] **T-062**：完成 Anvil 全栈 E2E 与 Sepolia 登录→购买→学习→mint 验收 `qa:tests/e2e/` ~12h
- [ ] **T-063**：完成第二轮合约安全审查、API 威胁复核和上线检查表 `security:docs/security/` ~8h
- [ ] **T-064**：同步 README、架构、API、部署、环境变量和 AI Agent 开发规则 `docs:README.md,docs/,.claude/` ~6h

## 依赖关系与并行建议

- T-001 阻塞 token、结算、生产链、NFT 和 Oracle 的最终实现；无结论时只可做接口/mock。
- T-003 → T-005～T-008、T-015、T-029；契约和命名应先冻结。
- T-005～T-014 → T-023～T-043；Auth/RBAC 是所有写 API 的前置。
- T-015 → T-016～T-018 → T-019～T-022；T-020/T-021 通过后才能部署测试网。
- T-022、T-030 → T-048；前端购买可先基于固定 mock 并行开发。
- T-007、T-017、T-030 → T-033～T-037；事件 schema、DB 唯一约束和合约事件必须一致。
- T-034 → T-038；只有确认订单派生的 enrollment 才能解锁学习。
- T-039～T-040、T-018 → T-041～T-043。
- T-023～T-043 的 API 可与 T-044～T-052 基于 OpenAPI mock 并行，T-051 后切真实 API。
- T-053～T-055 仅在对应 ADR 通过后进入迭代，不阻塞核心课程闭环。
- T-056～T-060 可在业务开发中期并行；T-062 依赖此前所有 MVP 核心任务。

## 里程碑

| 里程碑 | 覆盖任务 | 退出条件 |
|---|---|---|
| M0 工程就绪 | T-001～T-014 | 本地 DB/API、Privy、RBAC 测试通过 |
| M1 合约就绪 | T-015～T-022 | 合约覆盖率/安全门禁通过，ABI 可生成 |
| M2 课程平台 | T-023～T-029 | 老师创建、超管审核、学生浏览可用 |
| M3 链上购买 | T-030～T-037 | Sepolia 购买与幂等同步通过 |
| M4 学习证书 | T-038～T-043 | 完课只铸造一个有效证书 |
| M5 单端产品 | T-044～T-052 | 三角色关键 UI 和测试通过 |
| M6 生产准备 | T-056～T-064 | AWS staging、演练、安全与 E2E 通过 |

## 风险点

- **Token 合规与经济模型未定**：在 OQ-002～OQ-004 决策前，不上线真实价值流通。
- **前端框架冲突**：原讨论要求 Next.js，现仓是 Vite；默认保留现状，避免无验收收益的重写。
- **链上/链下漂移**：以 raw event、checkpoint、confirmation、reconciliation 和唯一约束共同控制。
- **RPC 单点/限流**：配置多供应商 fallback、批量回扫、退避和同步延迟告警。
- **私钥风险**：生产 admin 用多签；mint signer 最小权限、密钥托管、余额/频率告警。
- **证书语义不清**：soulbound、撤销、更正与隐私须在合约冻结前决定。
- **视频盗链和成本**：私有对象、短时签名、转码/带宽预算和生命周期策略须在媒体实现前确认。
- **范围膨胀**：Uniswap、Chainlink、退款/分账和多链均作为条件阶段，不阻塞 MVP 主闭环。

## 规模估算

- 任务总数：**64**。
- 粗略有效开发时间：约 **436 小时（约 55 人日）**，不含产品决策、外部安全审计、合规评估与等待链上确认。
- 建议先交付 M0～M4 的垂直闭环，再扩展完整前端、DEX/Oracle 和生产基础设施。
