# F06 — 管理 / 审计 / 运维 任务清单

## 任务列表

- [ ] **F06-T01** audit_logs 表（分区）+ append-only writer `database+api:database/migrations/0005_audit.sql,apps/api/internal/audit/` ~4h
- [ ] **F06-T02** request_id / correlation ID middleware + 结构化日志 `api:apps/api/internal/httpkit/` ~4h
- [ ] **F06-T03** Prometheus metrics 注册 + /metrics 端点 `api:apps/api/internal/httpkit/metrics.go` ~3h
- [ ] **F06-T04** 二次确认：confirm_token 服务（Privy fresh JWT 验证 + 一次性） `api:apps/api/internal/admin/confirm.go` ~5h
- [ ] **F06-T05** 超管路由：users / roles / courses review / chain-sync / certificates retry / audit-logs `api:apps/api/internal/admin/` ~10h
- [ ] **F06-T06** 链同步状态 API：checkpoint / lag / DLQ `api:apps/api/internal/admin/chain_sync.go` ~4h
- [ ] **F06-T07** Worker 自监控：chain_indexer_lag、events_consumed、mint_attempts `worker:apps/worker/internal/ops/metrics.go` ~4h
- [ ] **F06-T08** 业务关键余额告警：treasury / minter / hot wallet `worker+api:` ~3h
- [ ] **F06-T09** 前端超管：隐藏入口 + 二次确认模态 + 审核 / 角色 / 链运维 / 审计检索 `web:apps/web/src/features/admin/` ~12h
- [ ] **F06-T10** OpenAPI：admin 全部路由 + 共享错误码 `shared:packages/shared/openapi/admin.yaml` ~4h
- [ ] **F06-T11** infra：VPC / ALB / ECS / ECR 模块化 `infra:infra/aws/` ~12h
- [ ] **F06-T12** infra：RDS Multi-AZ / Redis / SQS+DLQ / S3 私有 + CloudFront OAC `infra:infra/aws/data/,storage/` ~12h
- [ ] **F06-T13** infra：Secrets Manager / KMS / WAF / CloudTrail / 安全组 `infra:infra/aws/security/` ~8h
- [ ] **F06-T14** observability：CloudWatch dashboard + 告警 + OTel trace `infra+apps:infra/aws/observability/,apps/api/` ~8h
- [ ] **F06-T15** CI：GitHub OIDC + PR / staging / prod 三套 workflow `ci:.github/workflows/` ~10h
- [ ] **F06-T16** runbooks：chain replay / signer rotation / DLQ recovery / backup restore `docs:deploy/runbooks/` ~6h
- [ ] **F06-T17** AI Agent 规约文件 + CI 钩子（commit 顺序检查） `.claude/rules/agent-workflow.md` ~4h
- [ ] **F06-T18** DR 演练：RDS 恢复 / RPC 故障 / Worker 崩溃 / DLQ 回放 / signer 轮换 `qa:docs/runbooks/dr-drills.md` ~8h
- [ ] **F06-T19** 第二轮合约安全审查 + API 威胁复核 + 上线检查表 `security:docs/security/` ~8h
- [ ] **F06-T20** README / 架构 / API / 部署 / 环境变量同步 `docs:README.md,docs/,docs/adr/` ~6h

## 依赖与并行

- **依赖**：F01~F05 所有特性。
- **可并行**：T-01~T-04（API 层横切）与 T-09（前端超管）并行；infra T-11~T-14 在 Phase 早期就可启动。
- **阻塞下游**：M6（生产就绪）。

## 退出条件（DoD）

- [ ] `/healthz` `/readyz` `/metrics` 全部 200。
- [ ] 关键告警在 staging 演练可触发。
- [ ] 二次确认 token 复用攻击测试失败。
- [ ] audit_logs append-only（DB 角色无 UPDATE/DELETE）。
- [ ] OIDC CI/CD staging 部署成功并跑通冒烟。
- [ ] AC-017 + Phase 8 全部 AC 通过。

## 风险

- **OIDC + 多账户**：staging / prod 分账户或分 stack；避免共享密钥。
- **二次确认 UX**：高频管理操作可能让超管疲劳；可考虑 Privileged Session Manager。
- **告警风暴**：阈值初期保守；上线 2 周后根据数据调优。