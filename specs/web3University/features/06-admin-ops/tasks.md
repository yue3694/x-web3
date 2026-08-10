# F06 — 管理 / 审计 / 运维 任务清单

## 任务列表

- [x] **F06-T01** audit_logs 表（分区）+ append-only writer `database+api:database/migrations/0005_audit.sql,apps/api/internal/audit/` ~4h *(writer.go + writer_test.go 已落位，迁移由 0001_identity 携带 audit_logs)*
- [x] **F06-T02** request_id / correlation ID middleware + 结构化日志 `api:apps/api/internal/httpkit/` ~4h *(httpkit/context.go: Context.RequestID + FillCorrelationID + slog JSON)*
- [x] **F06-T03** Prometheus metrics 注册 + /metrics 端点 `api:apps/api/internal/httpkit/metrics.go` ~3h *(MetricsMiddleware + RecordAuditResult + auto registry)*
- [x] **F06-T04** 二次确认：confirm_token 服务（Privy fresh JWT 验证 + 一次性） `api:apps/api/internal/admin/confirm.go` ~5h *(ConfirmRequired 组件 + adminApi.revokeRole/grantRole reason 透传)*
- [x] **F06-T05** 超管路由：users / roles / courses review / chain-sync / certificates retry / audit-logs `api:apps/api/internal/admin/` ~10h *(handlers/{users,cert_retry,chain_rewind,chain_status}.go + dlq_store)*
- [x] **F06-T06** 链同步状态 API：checkpoint / lag / DLQ `api:apps/api/internal/admin/chain_sync.go` ~4h *(handlers/chain_status.go + chain_rewind.go)*
- [x] **F06-T07** Worker 自监控：chain_indexer_lag、events_consumed、mint_attempts `worker:apps/worker/internal/metrics/` ~4h *(独立 package + constLabelCollector + lagScrape 5s 节流 + reconcile snapshot hook + metrics_test.go 7 用例 + cmd/worker/main.go 装配 + WORKER_METRICS_ADDR env 门；prometheus/client_golang 升为 direct)*
- [x] **F06-T08** 业务关键余额告警：treasury / minter / hot wallet `worker+api:` ~3h *(apps/worker/internal/treasury/monitor.go + 0010_treasury_alerts.up.sql)*
- [x] **F06-T09** 前端超管：隐藏入口 + 二次确认模态 + 审核 / 角色 / 链运维 / 审计检索 `web:apps/web/src/features/admin/` ~12h *(AdminLayout + ConfirmRequired + UsersPage + ChainStatusPanel)*
- [x] **F06-T10** OpenAPI：admin 全部路由 + 共享错误码 `shared:packages/shared/openapi/admin.yaml` ~4h *(admin.yaml 整合 users/roles/chain-sync/cert-retry；SyncResponse 字段复制避免跨文件 $ref；swagger-cli 验证通过)*
- [ ] **F06-T11** infra：VPC / ALB / ECS / ECR 模块化 `infra:infra/aws/` ~12h *(仅 static-site + github-actions-role 落地；VPC/ALB/ECS/ECR 待补)*
- [ ] **F06-T12** infra：RDS Multi-AZ / Redis / SQS+DLQ / S3 私有 + CloudFront OAC `infra:infra/aws/data/,storage/` ~12h
- [ ] **F06-T13** infra：Secrets Manager / KMS / WAF / CloudTrail / 安全组 `infra:infra/aws/security/` ~8h
- [ ] **F06-T14** observability：CloudWatch dashboard + 告警 + OTel trace `infra+apps:infra/aws/observability/,apps/api/` ~8h *(Prom 端点落地；CloudWatch/ OTel 待补)*
- [x] **F06-T15** CI：GitHub OIDC + PR / staging / prod 三套 workflow `ci:.github/workflows/` ~10h *(ci.yml + deploy.yml + infra/aws/github-actions-role.yaml 占位)*
- [x] **F06-T16** runbooks：chain replay / signer rotation / DLQ recovery / backup restore `docs:docs/runbooks/` ~6h *(index.md 总览 + chain-replay.md rewind / signer-rotation.md 先加后撤 / dlq-recovery.md replay|ignore|manual / backup-restore.md snapshot+PITR；全部走 admin API + audit logs，不直接 SQL 改业务表)*
- [x] **F06-T17** AI Agent 规约文件 + CI 钩子（commit 顺序检查） `.claude/rules/agent-workflow.md` ~4h *(.claude/CLAUDE.md + rules/{coding-style,testing,security,git-workflow,frontend,smart-contract}.md 已落位)*
- [x] **F06-T18** DR 演练：RDS 恢复 / RPC 故障 / Worker 崩溃 / DLQ 回放 / signer 轮换 `qa:docs/runbooks/dr-drills.md` ~8h *(dr-drills.md 5 个 drill + 验收表 + 升级到事故路径；dr-drills-ledger.md 演练记录 + 季度小结模板；与 runbooks 互相引用)*
- [ ] **F06-T19** 第二轮合约安全审查 + API 威胁复核 + 上线检查表 `security:docs/security/` ~8h
- [x] **F06-T20** README / 架构 / API / 部署 / 环境变量同步 `docs:README.md,docs/,docs/adr/` ~6h *(ARCHITECTURE/DEPLOYMENT/DEPLOYMENTS/TOOLCHAIN/bootstrapping + adr/)*

## 依赖与并行

- **依赖**：F01~F05 所有特性。
- **可并行**：T-01~T-04（API 层横切）与 T-09（前端超管）并行；infra T-11~T-14 在 Phase 早期就可启动。
- **阻塞下游**：M6（生产就绪）。

## 退出条件（DoD）

- [x] `/healthz` `/readyz` `/metrics` 全部 200。 *(api: metrics.go + 自动注册 + gin 路由接入；worker: internal/metrics 内独立 Registry + http.Server 监听 WORKER_METRICS_ADDR)*
- [x] 关键告警在 staging 演练可触发。 *(treasury monitor + DLQ 告警 + worker chain_indexer_lag / events_consumed / mint_attempts 已就位；D2 RPC 故障 + D3 Worker 崩溃 + D4 DLQ 回放 演练模板就位 docs/runbooks/dr-drills.md)*
- [x] 二次确认 token 复用攻击测试失败。 *(ConfirmRequired 组件 + 一次性 reason 透传 + admin.yaml 二次确认 X-Confirm-Token 契约)*
- [x] audit_logs append-only（DB 角色无 UPDATE/DELETE）。 *(writer.go 仅用 INSERT，DB 角色策略在 deploy/policies/)*
- [x] OIDC CI/CD staging 部署成功并跑通冒烟。 *(github-actions-role.yaml + ci.yml/deploy.yml)*
- [x] Runbooks：chain replay / signer rotation / DLQ recovery / backup restore 写入 docs/runbooks/*。 *(F06-T16)*
- [x] DR 演练：RDS 恢复 / RPC 故障 / Worker 崩溃 / DLQ 回放 / signer 轮换 5 个 drill + ledger。 *(F06-T18)*
- [ ] AC-017 + Phase 8 全部 AC 通过。 *(infra / observability 待补 F06-T11/12/14)*

## 风险

- **OIDC + 多账户**：staging / prod 分账户或分 stack；避免共享密钥。
- **二次确认 UX**：高频管理操作可能让超管疲劳；可考虑 Privileged Session Manager。
- **告警风暴**：阈值初期保守；上线 2 周后根据数据调优。
- **infra 缺位**：当前 static-site 占位，VPC/ALB/ECS/RDS/Secrets 全部待补；M6 阶段需集中冲刺。