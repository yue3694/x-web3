# F06 — 管理 / 审计 / 运维 设计

## 1. monorepo 落点

```text
apps/api/internal/admin/                # 超管路由
apps/api/internal/audit/                # append-only writer
apps/api/internal/httpkit/middleware/   # request_id / log / metrics
apps/api/internal/httpkit/metrics.go    # Prometheus 注册

apps/worker/internal/ops/               # 自监控指标端点

apps/web/src/features/admin/
├── AdminShell.tsx                      # 隐藏入口 + 二次确认模态
├── ReviewQueue.tsx
├── UsersRoles.tsx
├── ChainOps.tsx
├── AuditLogs.tsx
└── CertificateRetry.tsx

infra/aws/                              # 现有静态站点 IaC 扩展
├── network/                            # VPC / 子网 / 安全组
├── compute/                            # ECS Fargate + ALB
├── data/                               # RDS / ElastiCache / SQS / DLQ
├── storage/                            # 私有 S3 + CloudFront OAC
├── security/                           # Secrets Manager / KMS / WAF / CloudTrail
└── observability/                      # CloudWatch dashboards / alarms / OTel

deploy/                                 # 环境模板 + runbooks
├── docker-compose.yml                  # 本地全栈
├── runbooks/
│   ├── chain-replay.md
│   ├── signer-rotation.md
│   └── dlq-recovery.md
└── envs/{dev,staging,prod}.env.example

.github/workflows/
├── pr.yml                              # contracts/test + go/test + web/typecheck
├── deploy-staging.yml                  # OIDC → AWS staging
└── deploy-prod.yml                     # 手动 approval + change set
```

## 2. 二次确认机制

```text
1. 前端点击高风险操作 → 弹窗 "Confirm with re-login"
2. 调 Privy re-auth → 获得短时 fresh JWT (TTL 60s)
3. POST /admin/.../confirm-token → API 校验 fresh JWT 签名 + jti 未用过 → 返回 confirm_token
4. 提交实际操作请求：Header X-Confirm-Token: <confirm_token>
5. API 在 service 层校验 token 未过期（60s）+ 未使用 → 执行 + audit
```

confirm_token 服务端一次性（Redis SETNX），过期即失效。

## 3. Audit 设计

- 表分区：`audit_logs PARTITION BY RANGE (created_at)`，每月一分区。
- 写入路径：service 层 `audit.Log(ctx, "course.approve", courseID, before, after)`。
- 中间件注入 `correlation_id`：进 / 出全链路（HTTP → DB → Worker → Chain event）。
- 检索 API：`GET /admin/audit-logs?actor=&action=&from=&to=&correlation_id=`，cursor 分页。
- 导出：CSV（admin only）。

## 4. 监控与告警接线

```text
apps/api/cmd/api/main.go
  → promhttp.Handler() at /metrics

apps/worker/cmd/worker/main.go
  → promhttp.Handler() at :9090/metrics

infra/aws/observability/
  → task definition env: OTEL_EXPORTER_OTLP_ENDPOINT
  → CloudWatch Agent → OTel collector
  → dashboards: API latency / Error rate / Chain lag / Mint success
  → alarms via SNS → PagerDuty/OpsGenie
```

## 5. CI/CD 关键门禁

| 阶段 | 触发 | 检查 |
|---|---|---|
| **PR** | pull_request | `forge fmt --check`、`forge test`、`go test ./...`、`pnpm typecheck`、`pnpm lint` |
| **main** | push to main | build images → ECR；deploy to **staging**；run migrations；smoke |
| **release** | tag `v*` | 手动 approval；deploy to **prod** via change set；migration gate |

**OIDC**：GitHub → AWS 不需 long-lived secret；最小 IAM 仅授予 ECR push / ECS update / RDS migration invoke。

## 6. AI Agent 规约落地

文件：`.claude/rules/agent-workflow.md`，与现有 `.claude/rules/*.md` 并列。强制要点：

- 每个 PR 必须引用 `specs/.../features/XX/...md` 的具体章节（提供勾选框）。
- 提交顺序自动检查：CI 校验 commit 顺序（migration 在前、合约/API 在中、UI/文档在后）。
- 高风险 PR（改 RBAC、改 price、改 mint）必须 2 名 reviewer + 强制审计字段填写。

## 7. 安全检查

- [ ] 超管路由隐藏 + API 鉴权双层；URL 不可信。
- [ ] 二次确认机制可被 SSRF/重放攻击测试。
- [ ] audit_logs 表权限：app 服务 only-INSERT，admin 只读。
- [ ] 密钥仅在 Secrets Manager；CD/CI 用 OIDC。
- [ ] WAF 规则：限制 admin 路由 source IP（可选，prod 加 VPN）。
- [ ] CloudTrail 启用并归档到独立 log 账户。

## 8. 测试策略

- **API**：超管路由权限矩阵、audit writer append-only、二次确认 token 复用检测。
- **Worker**：自监控指标端点冒烟、DLQ 告警触发。
- **E2E**：登录 → 提交审核 → 超管 review → audit log 可检索。
- **DR 演练**：RDS snapshot 恢复、Chain replay、Worker signer 轮换。