# F06 — 管理、审计与运维（Admin / Audit / Ops）

> 来源：上级 `requirements.md` F-035 ~ F-038；本特性在 monorepo 中的实现切片。
> 横切关注点：审计日志、可观测性、CI/CD、AI Agent 开发规约。

## 1. 范围

- 超管角色管理、课程审核（与 F02 共享）、订单 / 同步异常处置、证书重试（与 F04 共享）。
- 高风险操作二次确认 + audit log。
- 健康检查、结构化日志、指标、告警（API / DB / 队列 / RPC 水位 / 失败交易）。
- 所有跨系统写操作用 idempotency key 或唯一约束。
- AI Agent 必须按 migration → API/合约 → 测试 → UI → 文档顺序；禁绕过 RBAC、禁业务数据上链。

## 2. 功能需求

| ID | 描述 | 验收 |
|---|---|---|
| **R-AD-001** | 超管可管理角色、课程审核、订单/同步异常、证书重试 | API + UI |
| **R-AD-002** | 高风险操作（角色变更 / 强制下架 / 重放链事件）需二次确认 + audit log | AC-017 |
| **R-AD-003** | 系统提供 /healthz、/readyz、结构化日志（含 correlation ID）、指标、告警 | k6 / Alertmanager 演练 |
| **R-AD-004** | 链同步异常可检索并带完整 correlation/audit 信息 | AC-017 |
| **R-AD-005** | 所有跨系统写操作用 idempotency key 或唯一约束 | 集成测试 |
| **R-AD-006** | AI Agent 开发顺序：migration → API/合约 → 测试 → UI → 文档；禁绕过 RBAC；禁业务数据上链 | `.claude/rules/` 落地 |

## 3. 数据模型

```sql
audit_logs(id, actor_user_id nullable, actor_address nullable, action, target_type, target_id, before JSONB, after JSONB, correlation_id, ip, user_agent, created_at) -- append-only, partition by month
admin_actions(id, admin_user_id, action, params JSONB, confirm_token_used bool, status, created_at)
system_alerts(id, source, severity, message, payload JSONB, fired_at, resolved_at nullable)
```

## 4. API 契约（超管路由）

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `/admin/users` | `SYSTEM_ADMIN` | 用户列表 + 角色 |
| `PATCH` | `/admin/users/{id}/roles` | `SYSTEM_ADMIN` + 二次确认 | 角色变更 |
| `POST` | `/admin/courses/{id}/review` | `COURSE_APPROVE` | approve/reject |
| `GET` | `/admin/chain-sync` | `SYSTEM_ADMIN` | checkpoint / 延迟 / DLQ |
| `POST` | `/admin/chain-sync/replay` | `SYSTEM_ADMIN` + 二次确认 | 指定区块范围回放 |
| `GET` | `/admin/certificates/pending` | `SYSTEM_ADMIN` | 待重试的 mint job |
| `POST` | `/admin/certificates/{id}/retry` | `SYSTEM_ADMIN` | 重试 mint |
| `GET` | `/admin/audit-logs` | `SYSTEM_ADMIN` | 审计检索（分页 + 过滤） |

**二次确认**：请求带 `X-Confirm-Token: t=<signed_jti>`，需最近 60 s 内使用 Privy 重新登录获得的短期 JWT 签名。

## 5. 可观测性

### 日志
- 结构化 JSON：`{ ts, level, msg, request_id, user_id, route, status, latency_ms, ... }`
- 强制字段：`request_id`、`user_id`、`route`（来自 middleware）。

### 指标（Prometheus）
- `http_requests_total{route,method,status}`
- `http_request_duration_seconds{route,method}` (Histogram)
- `db_pool_active / idle / wait_count`
- `sqs_visible_messages / age_seconds / dlq_depth`
- `chain_indexer_lag_blocks{chain_id}`
- `chain_indexer_events_consumed_total{event_type,status}`
- `certificate_mint_attempts_total{status}`
- `yd_treasury_balance` / `certificate_minter_balance`（业务关键余额告警）

### 告警
| 触发 | 严重度 |
|---|---|
| 5xx rate > 1% (5 min) | P3 |
| DB connection pool wait > 50% | P2 |
| SQS age > 5 min | P2 |
| DLQ depth > 0 | P2 |
| chain_indexer_lag > 50 blocks | P2 |
| chain_indexer_lag > 200 blocks | P1 |
| certificate mint failure rate > 10% | P2 |
| treasury / minter balance < threshold | P1 |

## 6. CI/CD（GitHub Actions OIDC）

```text
PR:
  - pnpm contracts:test
  - forge fmt --check
  - go test ./...
  - pnpm typecheck
  - pnpm lint

main:
  - build images (api, worker, web) → ECR
  - run migrations on staging
  - deploy api/worker to ECS staging
  - deploy web to CloudFront staging
  - smoke tests

release tag:
  - manual approval gate
  - deploy prod with change set
```

## 7. AI Agent 开发规约（写入 `.claude/rules/agent-workflow.md`）

1. 必须先读对应 feature 的 `specs/.../features/XX-{feature}/{requirements,design,tasks}.md`。
2. 提交顺序：`migration → API/合约 → 测试 → UI → 文档`。
3. 禁绕过 RBAC（前端声明永远不可信）。
4. 禁把业务数据（评论 / 视频元数据 / 学习进度）上链。
5. 任何跨系统写必须带 idempotency key 或命中唯一约束。
6. 高风险操作必须二次确认 + audit log。
7. 提交前必跑：`forge test && pnpm typecheck && go test ./...`。

## 8. 边界

- **横切**：所有特性均依赖。
- **AWS 基础设施**：见 `infra/aws/`（VPC、ALB、ECS、RDS、S3、CloudFront、Secrets Manager、WAF、CloudWatch）。
- **DR / 备份**：本期为非生产，备份恢复演练放 Phase 8。