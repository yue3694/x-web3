# Web3 University — PLAN（执行顺序与里程碑）

> 本文件是 monorepo 落地的总路线图。6 个 feature 的 `requirements/design/tasks` 是单点切片，本文件负责**跨切片依赖、并行建议、里程碑、风险与决策待办**。

## 1. monorepo 目标结构（演进态）

```text
apps/
  web/                    # 现有 Vite + React + wagmi/viem
  api/                    # 新建 Go HTTP API
  worker/                 # 新建 Go Worker（链监听、mint、对账）
packages/
  contracts/              # 现有 Foundry + OZ；新增 YDToken / CourseMarket / CertificateNFT
  shared/                 # 新建 OpenAPI / 事件 schema / 错误码常量
database/
  migrations/             # golang-migrate SQL（按 0001～ 顺序）
  queries/                # sqlc queries
infra/aws/                # IaC（现有静态站点扩展）
deploy/                   # compose、runbooks、env 模板
specs/web3 university/    # 本目录
  ├── requirements.md     # 上级规格
  ├── design.md
  ├── tasks.md
  ├── PLAN.md             # 本文件
  └── features/
      ├── 01-identity/
      ├── 02-course-content/
      ├── 03-order-onchain/
      ├── 04-learning-certificate/
      ├── 05-token-dex/
      └── 06-admin-ops/
docs/
  ├── adr/                # 关键决策
  ├── security/
  └── runbooks/
.claude/rules/
  ├── agent-workflow.md   # AI Agent 开发顺序
  └── ...（既有）
```

## 2. 6 个 Feature 职责矩阵

| Feature | 关键路径 | 涉及合约 | 涉及 API 模块 | 涉及 Worker 模块 | 涉及前端模块 |
|---|---|---|---|---|---|
| **F01 Identity & RBAC** | Privy 登录 / 多钱包 / 三角色 | — | `auth/ rbac/ wallet/ user/ audit/` | — | `auth/ RequirePermission` |
| **F02 Course & Content** | 课程生命周期 / 媒体 / 评论 | — | `course/ review/ catalog/ media/ comment/` | — | `features/catalog/ teacher/ learning/Player` |
| **F03 Order & On-chain** | 价格版本 / intent / event 同步 | **CourseMarket** | `order/ admin/chain-sync` | **indexer/ chain/ order/ reconcile/** | `features/checkout/ account/MyOrders` |
| **F04 Learning & Certificate** | 进度 / 完课 / mint | **CertificateNFT** | `learning/ certificate/` | **certificate/** | `features/learning/ account/MyCertificates` |
| **F05 Token / DEX / Oracle** | YD / Uniswap / Oracle | **YDToken** | — | oracle（条件） | `features/swap/` |
| **F06 Admin / Audit / Ops** | 横切：超管 / 审计 / 监控 / CI | — | `admin/ audit/ httpkit/` | `ops/` | `features/admin/` |

## 3. 执行顺序（自底向上 + 并行优先）

```text
Phase 0  ── 工程基线
   ├── T-001 ADR (OQ-001~008 决议)
   ├── T-002 monorepo + Go workspace + CI
   ├── T-003 shared: OpenAPI + 事件 schema + 错误码
   └── T-004 docker-compose（PG + Redis + Anvil）

Phase 1  ── 数据库与身份基础 (F01 数据层)
   ├── F01-T02 migration 0001
   ├── F01-T03 repository
   ├── F01-T09 role/permission seed
   └── F01-T04/T05/T06 auth + RBAC + wallet

Phase 2  ── 课程平台骨架 (F02)
   ├── F02-T01 migration 0002
   ├── F02-T02/T03/T04 course/review
   ├── F02-T05/T06 catalog
   └── F02-T07/T08 media + playback

Phase 3  ── 合约与 ABI (F05 前置)
   ├── F05-T01/T02 YDToken
   ├── F03-T01/T02 CourseMarket + 测试
   ├── F04-T01/T02 CertificateNFT + 测试
   ├── T-019 部署脚本
   └── T-022 ABI 导出 + chain registry

Phase 4  ── 订单与链同步 (F03)
   ├── F03-T05 migration 0003
   ├── F03-T06/T07/T08 API order
   ├── F03-T09 Worker indexer
   ├── F03-T10 decoder + 校验
   ├── F03-T11 原子事务
   └── F03-T12 reorg + 回放

Phase 5  ── 学习与证书 (F04)
   ├── F04-T05 migration 0004
   ├── F04-T06/T07/T08 learning + certificate API
   ├── F04-T09 metadata
   ├── F04-T10/T11 Worker mint signer + consumer
   └── F04-T15/T16 集成

Phase 6  ── 单端产品 (前端整合 F01~F04)
   ├── F01-T10/T11 auth
   ├── F02-T11~T14 catalog/teacher/learning
   ├── F03-T15/T16 checkout/orders
   ├── F04-T13/T14 player/certificates
   └── F04-T17 E2E

Phase 7  ── Uniswap (条件阶段 F05)
   └── F05-T05/T06/T07 swap UI + 测试

Phase 8  ── 管理、可观测性与生产 (F06)
   ├── F06-T01~T08 横切 middleware / metrics / confirm
   ├── F06-T09 超管 UI
   ├── F06-T11~T14 infra AWS
   ├── F06-T15 CI/CD OIDC
   ├── F06-T16/T18 runbooks + DR 演练
   └── F06-T19/T20 安全 + 文档同步
```

## 4. 里程碑

| 里程碑 | 覆盖 | 退出条件 | 阻塞下站 |
|---|---|---|---|
| **M0 工程就绪** | Phase 0 + F01 数据/API | 本地 DB/API、Privy、RBAC 测试通过 | F02 写 API |
| **M1 合约就绪** | Phase 3 + F03/F04 接口 | 合约覆盖 ≥ 90%、安全门禁过、ABI 可生成 | Sepolia 集成 |
| **M2 课程平台** | Phase 2 | 老师创建、超管审核、学生浏览可用 | 购买闭环 |
| **M3 链上购买** | Phase 4 | Sepolia 购买与幂等同步通过 | 证书闭环 |
| **M4 学习证书** | Phase 5 | 完课只铸造一个有效证书 | 前端整合 |
| **M5 单端产品** | Phase 6 | 三角色关键 UI + 测试通过 | DEX/管理 |
| **M6 生产准备** | Phase 8 | staging 部署、演练、安全与 E2E 通过 | 上线 |

## 5. 依赖图（简化）

```text
F01 ──► F02 ──► F03 ──► F04
            │
            └──► F05 (YD) ──► F03 (CourseMarket 用 YD)

F01~F04 ──► F06 (横切)
```

## 6. 并行建议

- **F01 数据层** 与 **前端 auth 骨架** 可并行（基于 mock session）。
- **F02 数据/服务** 与 **F03 合约** 可并行（接口先 mock）。
- **F03 API** 与 **Worker** 可并行（用 anvil + 模拟事件）。
- **F04 合约** 与 **F02 课程版本完成规则** 可并行。
- **infra/aws**（VPC / ECS / RDS）从 M0 后即可启动；不必等业务完成。

## 7. 决策待办（OQ → ADR）

| OQ | 主题 | 决议前可用 |
|---|---|---|
| OQ-001 | 生产目标链 / 确认数 / gas 代付 | Sepolia 演示；confirmation = 12 块 |
| OQ-002 | YD tokenomics（cap / treasury / 监管） | 占位 cap = 1B；treasury = deployer（部署后多签接管） |
| OQ-003 | 课程款结算（treasury 直收 / 讲师分账） | MVP treasury 直收；分账后置 |
| OQ-004 | 课程定价（仅 YD / 加稳定币） | MVP 仅 YD |
| OQ-005 | 视频转码 / DRM | MVP 仅私有 S3 + 签名 URL；预留 transcoded_status 字段 |
| OQ-006 | 完课是否需外部证明 | MVP 仅平台事件；预留 Oracle 接入点 |
| OQ-007 | Chainlink MVP 用例 | 默认不接；OQ 通过后才进 F05-T08/T09 |
| OQ-008 | Next.js 迁移 | 保留 Vite + React；ADR 决策后才迁移 |

每项决议对应 `docs/adr/000X-*.md`，由 T-001 完成。

## 8. 风险登记

| 风险 | 缓解 |
|---|---|
| OQ 未决卡死业务 | 用 mock 配置 + ADR 占位；Phase 0 即 T-001 推动决议 |
| Vite vs Next.js 路径冲突 | 默认 Vite；ADR 决议前不引入 Next.js |
| 链上/链下漂移 | raw event → checkpoint → confirmation → reconciliation 四重护栏 |
| RPC 单点 | 多供应商 fallback + 批量回扫 + 同步延迟告警 |
| 私钥风险 | treasury 多签；Worker mint signer 独立 KMS + 额度告警 |
| 证书语义不清 | 默认 soulbound；撤销/更正留 ADR |
| 视频盗链 / 成本 | 私有对象 + 短时签名 + lifecycle policy |
| 范围膨胀 | Uniswap / Oracle / 退款 / 分账 / 多链均条件阶段 |

## 9. 估算

- 任务总数：**~108**（F01~F06 子任务合计）
- 粗略有效开发时间：约 **500 小时（约 63 人日）**，不含产品决策 / 外部审计 / 合规评估 / 等待链上确认 / AWS 配额审批。
- 建议先交付 M0~M4 垂直闭环，再扩展前端、DEX/Oracle、生产基础设施。

## 10. 立即可执行（Next 5 PRs）

1. **PR-1 Phase 0**：ADR 草案、monorepo workspaces、docker-compose、`shared/openapi/errors.yaml`。
2. **PR-2 F01 数据 + API 骨架**：migration 0001、users/wallets/roles repo、Privy stub verifier、RBAC middleware。
3. **PR-3 F01 前端**：PrivyProvider、SessionBootstrap、RequirePermission、WalletLink。
4. **PR-4 F02 数据**：migration 0002、courses/chapters/lessons repo + 乐观锁。
5. **PR-5 F03 合约**：CourseMarket.sol + 单测 + invariant + Sepolia 部署脚本（待 OQ-001 决议）。