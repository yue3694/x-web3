# Web3 University — 技术设计

## 项目架构

- **架构类型**：monorepo + 前后端分离 + Web3。
- **现状**：`apps/web` 为 Vite 5 + React 18 + wagmi/viem；`packages/contracts` 为 Foundry + Solidity 0.8.24；AWS 目前仅有静态站点基础设施。
- **涉及模块**：`apps/web`、新增 `apps/api`、新增 `apps/worker`、`packages/contracts`、新增 `packages/shared`、新增 `database`、`infra/aws`、`deploy`、`docs`。
- **演进原则**：在现有 workspace 内增量扩展。原讨论中的 Next.js 不直接覆盖当前 Vite 应用；只有 SSR/SEO 被验证为硬需求后才通过 ADR 迁移。

## 方案概述

系统采用“链下业务、链上结算与凭证”模式。Web 通过 Privy 登录并调用 Go API；涉及 YD 支付时由用户钱包直接签名。CourseMarket 发出规范事件，独立 Go Worker 等待确认、校验 receipt 后更新 PostgreSQL，并授予课程访问。学习完成由 API 判定，Worker 使用受限 minter 身份铸造 CertificateNFT。

```text
Browser / Privy wallet
  ├── HTTPS ──> CloudFront/WAF ──> ALB ──> Go API ──> PostgreSQL
  │                                         ├──────> Redis
  │                                         ├──────> S3 signed media
  │                                         └──────> SQS
  └── RPC ───> ERC20 YD + CourseMarket + CertificateNFT
                                           │ events
                                           v
                           Go Worker <── RPC/WebSocket
                              ├── PostgreSQL checkpoint/outbox
                              ├── SQS/DLQ
                              └── NFT mint signer (KMS/managed signer)
```

## Web2 / Web3 责任边界

| 数据/行为 | 权威来源 | 原因 |
|---|---|---|
| 用户、角色、权限 | PostgreSQL | 可变业务身份，不公开 PII |
| 课程、章节、视频、评论 | PostgreSQL + S3 | 高频读写、检索和访问控制 |
| 购买意图、订单视图 | PostgreSQL | 用户体验和工作流状态 |
| 实际支付 | EVM receipt + CourseMarket event | 可验证结算事实 |
| enrollment | PostgreSQL，由已确认事件派生 | 高效内容授权 |
| 学习进度/完课 | PostgreSQL | 高频、隐私、可更正 |
| 证书归属 | CertificateNFT 链上状态 | 公开可验证凭证 |
| DEX 报价/成交 | Uniswap pool/receipt | 去中心化兑换事实 |

## Monorepo 目标结构

```text
apps/
  web/                  # 当前 Vite React 单前端
  api/                  # Go HTTP API
  worker/               # 链监听、对账、mint、异步任务
packages/
  contracts/            # Foundry: YDToken/CourseMarket/CertificateNFT
  shared/               # OpenAPI 生成类型、事件 schema、常量
database/
  migrations/           # PostgreSQL migration
  queries/              # sqlc queries（若选 sqlc）
infra/aws/              # IaC 模块与环境入口
deploy/                 # 环境配置模板、runbooks
specs/web3 univercity/  # 本规格
docs/                   # 运维、部署、ADR
```

## 核心流程

### 登录与开户

1. Web 从 Privy 获取短时 access token。
2. API 验证 issuer、audience、signature、expiry，读取 Privy subject。
3. 事务内按 `privy_user_id` upsert users，并同步经过验证的钱包。
4. API 返回平台 session/profile/permissions；所有受保护路由再次从服务端授权。

### 课程发布

1. Teacher 创建草稿、章节和 lesson，上传视频到受控 S3 key。
2. Teacher 提交审核，快照课程版本与价格草案。
3. Super Admin 审核后发布；若为付费课程，将稳定 `courseKey`、token、价格版本同步至 CourseMarket 管理配置。
4. 合约配置交易确认后，课程价格版本才进入 `active`；失败则保持不可购买。

### YD 购买与事件同步

1. API 创建 `purchase_intent`，返回 courseKey、token、amount、chain、market、expiry。
2. Web 校验网络和余额，按需 approve，然后调用 `buyCourse(courseKey, amount, intentId)`。
3. Web 仅回报 tx hash，订单进入 submitted，不授予权限。
4. Worker 拉取事件，按确认数校验 canonical block、receipt status、market、buyer、courseKey、token、amount 和 intentId。
5. 数据库事务内插入 `chain_events`、确认 order、创建 enrollment 和 outbox 事件；唯一键保证幂等。
6. 定时 reconciliation 从 checkpoint 回扫，补偿 WebSocket 漏事件；reorg 将事件/订单标记并进入处置流程。

### 完课与证书

1. API 幂等记录 lesson progress，只允许 enrollment owner 写入。
2. 服务端依据版本化规则计算完成状态，插入 completion 和唯一 mint job。
3. Worker 读取 job，调用 CertificateNFT `mintCertificate(recipient, certificateId, uri)`。
4. receipt 达确认数后保存 tokenId；失败按指数退避重试，永久失败进入 DLQ/人工处理。

## 数据模型

### 身份与权限

| 表 | 关键字段/约束 |
|---|---|
| `users` | `id uuid pk`, `privy_user_id unique`, `status`, profile timestamps |
| `wallets` | `id`, `user_id fk`, `chain_namespace`, `chain_id`, `address_normalized`, `is_primary`; unique(namespace, chain_id, address) |
| `roles` | `id`, `code unique` (`student/teacher/super_admin`) |
| `permissions` | `id`, `code unique` |
| `user_roles` | unique(user_id, role_id), `granted_by`, timestamps |
| `role_permissions` | unique(role_id, permission_id) |
| `audit_logs` | actor, action, target, before/after JSONB, correlation_id, ip, created_at；append-only |

### 课程与学习

| 表 | 关键字段/约束 |
|---|---|
| `courses` | UUID、teacher_id、slug、title、status、current_version、published_at、deleted_at |
| `course_versions` | course_id + version unique、description、completion_rule JSONB |
| `chapters` / `lessons` | course_version_id、position unique per parent、required、media_asset_id |
| `media_assets` | S3 key、content type、size、status、checksum；不保存公开 URL |
| `comments` | course_id、user_id、body、moderation_status、deleted_at |
| `enrollments` | user_id + course_id unique、source_order_id、status、granted_at |
| `lesson_progress` | enrollment_id + lesson_id unique、progress_bps、completed_at、version |
| `course_completions` | enrollment_id + rule_version unique、completed_at |

### 支付与链同步

| 表 | 关键字段/约束 |
|---|---|
| `course_prices` | course_id、version、chain_id、token、amount numeric(78,0)、decimals、market、valid range |
| `purchase_intents` | UUID、user/wallet/course/price_version、expires_at、idempotency_key unique |
| `orders` | intent_id unique、status、tx_hash nullable、confirmed_at、failure_code；unique(chain_id, tx_hash) where tx_hash not null |
| `chain_events` | chain_id、tx_hash、log_index unique、block_number/hash、event_type、payload、canonical |
| `chain_checkpoints` | chain_id + consumer unique、next_block、last_block_hash |
| `outbox_events` | aggregate、type、payload、published_at；数据库事务后投递 |

### 证书

| 表 | 关键字段/约束 |
|---|---|
| `certificates` | completion_id unique、recipient_wallet、metadata_uri、status、chain_id、tx_hash、token_id |
| `jobs` | type、dedupe_key unique、payload、status、attempts、run_after、last_error |

所有业务表包含 `created_at/updated_at`；软删除仅用于需要保留审计的数据。金额不使用 float。迁移只能前向追加，破坏性变更采用 expand/contract。

## API 契约

统一前缀 `/api/v1`，JSON 使用 camelCase，时间为 UTC RFC 3339。写请求接受 `Idempotency-Key`。错误格式：

```json
{
  "error": {
    "code": "COURSE_STATE_CONFLICT",
    "message": "course cannot be published from draft",
    "requestId": "...",
    "details": {}
  }
}
```

| 方法与路由 | 权限 | 说明 / 主要响应 |
|---|---|---|
| `POST /auth/privy/session` | Privy token | upsert 用户，返回 profile/permissions |
| `GET /me` | 登录 | 当前用户、钱包、角色 |
| `POST /me/wallets/link` | 登录 + wallet proof | 绑定钱包 |
| `GET /courses` | 公开 | 已发布课程分页列表 |
| `GET /courses/{id}` | 公开/登录 | 详情；受保护资源按 enrollment 裁剪 |
| `POST /teacher/courses` | `COURSE_CREATE` | 创建草稿 |
| `PUT /teacher/courses/{id}` | 作者 + `COURSE_EDIT` | 更新草稿，乐观锁 |
| `POST /teacher/courses/{id}/submit` | 作者 | 提交审核 |
| `POST /admin/courses/{id}/review` | `COURSE_APPROVE` | approve/reject + reason |
| `POST /courses/{id}/purchase-intents` | Student | 返回冻结后的链上调用参数 |
| `POST /orders/{intentId}/transactions` | owner | 提交 tx hash，仅标记 submitted |
| `GET /orders/{id}` | owner/admin | 订单及确认状态 |
| `POST /lessons/{id}/progress` | enrollment owner | 幂等更新进度 |
| `POST /courses/{id}/complete` | enrollment owner | 重新计算完成，返回证书任务 |
| `GET /me/certificates` | 登录 | 链上证书列表 |
| `GET /admin/chain-sync` | `SYSTEM_ADMIN` | checkpoint、延迟、DLQ |
| `POST /admin/chain-sync/replay` | `SYSTEM_ADMIN` | 指定安全区块范围回放并审计 |

OpenAPI 文件应成为 API 的契约源，并生成 TypeScript client/types；Go handler 必须经过 schema validation。

## 合约设计

### YDToken.sol

- OpenZeppelin `ERC20`, `ERC20Permit`, `AccessControl`, `Pausable`。
- `DEFAULT_ADMIN_ROLE` 交给多签，`MINTER_ROLE` 是否存在由 tokenomics 决定；若总量固定则部署后永久撤销。
- 明确 cap、treasury、initial distribution；所有角色/暂停变更 emit 事件。

### CourseMarket.sol

- 接口建议：`buyCourse(bytes32 courseKey, uint256 expectedAmount, bytes16 intentId)`。
- 课程配置保存 token、amount、enabled、priceVersion；管理员变更 emit `CourseConfigured`。
- 购买使用 SafeERC20 `safeTransferFrom`，CEI + `nonReentrant` + `whenNotPaused`。
- `(buyer, courseKey)` 默认只允许成功一次；如允许重购必须由明确业务规则打开。
- `CoursePurchased` 至少索引 buyer、courseKey，并携带 token、amount、intentId、priceVersion。
- 资金优先进入 treasury；讲师分账/退款在需求确定后再加入，避免 MVP 会计复杂度。

### CertificateNFT.sol

- `ERC721`, `AccessControl`, `Pausable`；可选 `ERC721URIStorage`。
- `certificateId`（数据库生成 bytes32）唯一，mapping 防重复。
- 只有 `MINTER_ROLE` 可 mint；默认不可转让还是可转让必须产品确认。教育证书建议 soulbound，并实现清晰的撤销/更正策略。
- 合约默认不可升级；任何升级需求需 UUPS + timelock + storage layout 检查。

## 链监听与一致性设计

- Worker 同时支持 WebSocket 快速监听和 HTTP 区块回扫，回扫是正确性保障。
- 每条链配置 `start_block`、confirmation depth、max batch、RPC fallback。
- 先写 raw event，再在同一事务内更新派生状态；重复事件命中唯一键后视为成功。
- checkpoint 仅在整个区块批次提交后前移；保存 block hash 检测 reorg。
- 对 `confirmation_depth` 内数据标为 provisional，不授予持久权益。
- reconciliation 定时比较链上 receipt、orders 和 enrollments；异常进入告警与人工工作台。

## 前端组件与状态设计

```text
AppShell
├── PublicRoutes: CourseList, CourseDetail
├── StudentRoutes: Checkout, LearningPlayer, MyOrders, MyCertificates
├── TeacherRoutes: CourseEditor, MediaManager, TeacherAnalytics
└── AdminRoutes: ReviewQueue, UsersRoles, ChainOps, AuditLogs
```

- 服务端状态使用 TanStack Query；表单/临时态使用 React state，必要时小型 Context/Zustand。
- Privy 负责认证/钱包体验，wagmi/viem 负责链切换、读写和 receipt 跟踪。
- 交易 UI 是显式状态机：`idle → checking → approvalRequired → approving → purchasing → confirming → success/error`。
- ABI 由 Foundry artifact 自动导出，部署地址按 chain registry 管理，不在组件硬编码。
- 管理路由可隐藏，但进入页面和每次 API 请求都必须重新鉴权。

## AWS 部署设计

- **Web**：S3 + CloudFront（保留 Vite SPA 时）；若未来 Next.js SSR，则单独使用 ECS/Lambda 方案。
- **API/Worker**：ECS Fargate 独立 service/task definition；API 经 ALB，Worker 无公网入口。
- **数据**：RDS PostgreSQL Multi-AZ（生产）、ElastiCache Redis、SQS + DLQ。
- **媒体**：私有 S3、CloudFront OAC、签名 URL/Cookie；上传使用预签名 URL 和内容校验。
- **安全**：VPC 私有子网、最小 IAM、Secrets Manager/KMS、WAF、CloudTrail、ECR 扫描。
- **可观测性**：CloudWatch logs/metrics/alarms，OpenTelemetry trace；关键告警为 5xx、DB 连接、SQS age、DLQ、chain lag、mint failure、treasury/minter balance。
- **交付**：GitHub Actions OIDC；按 dev/staging/prod 分离账号或至少分离 stack、数据库和密钥；IaC change set/diff 后部署。

## 安全考虑

- Privy JWT 只在后端验证；校验 issuer/audience/expiry，JWKS 缓存需支持轮换。
- 钱包绑定需要 nonce、domain、chain、expiry 的一次性签名证明，防重放和跨站签名滥用。
- 所有对象级权限在 service 层校验，防 IDOR；老师只能访问自己的资源。
- 上传验证 MIME/size/checksum，隔离未扫描对象；播放凭证短时有效。
- API 限流按 IP、user、wallet 和高风险动作分层；管理员操作要求 MFA/Privy 强验证。
- 合约遵循仓库 `.claude/rules/smart-contract.md`：CEI、自定义错误、输入边界、事件、无界循环禁用、测试覆盖 ≥ 90%。
- deployer、treasury、admin、minter 分权；生产 admin 使用多签，Worker signer 权限最小化并设置额度/告警。
- Oracle 数据必须检查 `updatedAt`、round、decimals、偏差和 fallback；不得把不可信 Oracle 返回直接写入核心会计。

## 测试策略

- **合约**：单元、失败路径、fuzz、invariant（资金守恒、不可重复购买/铸造）、fork integration、Slither/静态扫描、Sepolia smoke。
- **Go**：service table tests、repository integration（真实 PostgreSQL）、JWT/RBAC、event decoder golden tests、Worker 重放/reorg/幂等测试。
- **Web**：组件、权限导航、交易状态机、错误网络、API mock；关键路径 Playwright E2E。
- **端到端**：Anvil + PostgreSQL + API + Worker + Web；Sepolia 演示购买与 mint。
- **非功能**：课程列表/API load、RPC 故障演练、SQS/DLQ 恢复、备份恢复和权限审计。

## 技术决策

| 决策 | 选择 | 理由 |
|---|---|---|
| 前端框架 | MVP 保留 Vite + React | 与现仓一致，避免无明确收益的 Next.js 重写 |
| 后端 | Go API + 独立 Go Worker | 区分请求生命周期与长任务/链同步 |
| 数据库 | PostgreSQL | 事务、约束、审计和复杂查询适合核心业务 |
| 异步系统 | PostgreSQL outbox + SQS/DLQ | 可追踪、可重试，降低丢消息概率 |
| 身份主键 | users UUID + unique Privy subject | 支持多钱包和钱包迁移 |
| 支付确认 | 合约事件 + receipt 校验 | 不信任前端 tx hash |
| 合约升级 | 默认不可升级 | 降低代理和权限风险 |
| DEX | YD/USDC 单一主池优先 | 稳定计价并减少流动性碎片 |
| Chainlink | 有明确外部信任需求才接入 | 避免为“上链”而引入 Oracle 复杂度 |
| IaC | 沿用 `infra/aws` 并模块化 | 与现有 AWS 资产衔接，所有环境可复现 |

## 待 ADR 的决策

1. Vite 保留还是 Next.js 迁移（SEO/SSR 数据验证后决定）。
2. Go router、migration、query 代码生成的具体库。
3. 生产链、RPC 多供应商、确认数和 reorg 处置策略。
4. CertificateNFT 是否 soulbound、是否允许撤销/更正。
5. YD tokenomics、讲师结算、退款和税务/合规边界。
6. Chainlink 的具体 MVP 用例；若没有明确输入和 SLA，则从 MVP 移除。

