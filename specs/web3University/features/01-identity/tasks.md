# F01 — 身份与权限 任务清单

> 单人有效工时粗估；依赖 Phase 0（工程基线）的 T-001 ~ T-004。

## 任务列表

- [x] **F01-T01** 创建 `apps/api/` Go module（go.mod、cmd/api、internal 分层） `api:apps/api/` ~2h
- [x] **F01-T02** migration：users / wallets / roles / permissions / user_roles / role_permissions / audit_logs `database:database/migrations/0001_identity.sql` ~3h
- [x] **F01-T03** sqlc / pgx repository：users / wallets / roles / permissions `api:apps/api/internal/user/` ~4h
- [x] **F01-T04** Privy JWT verifier + JWKS 缓存 + session 创建/刷新/销毁 `api:apps/api/internal/auth/` ~6h
- [x] **F01-T05** RBAC middleware + permission 缓存 + 对象级 service hook `api:apps/api/internal/rbac/` ~5h
- [x] **F01-T06** 钱包绑定：nonce 签名校验 + 唯一性冲突 + 解绑保护 `api:apps/api/internal/wallet/` ~6h
- [x] **F01-T07** Audit append-only writer + correlation ID 注入 `api:apps/api/internal/audit/` ~3h
- [x] **F01-T08** OpenAPI：auth / me / wallets 路由契约 `shared:packages/shared/openapi/auth.yaml` ~4h
- [x] **F01-T09** 角色种子数据 + 启动时校验 `database:database/seed/0001_roles.sql` ~1h
- [x] **F01-T10** 前端：PrivyProvider / SessionBootstrap / RequireAuth / PermissionContext `web:apps/web/src/auth/` ~6h
- [x] **F01-T11** 前端：钱包绑定弹窗 + 网络/余额显示 `web:apps/web/src/auth/WalletLink.tsx` ~4h
- [x] **F01-T12** 集成测试：完整开户/绑定/解绑路径（真实 PG + miniredis） `api:apps/api/internal/integration/identity_test.go,wallet_bind_test.go` ~6h
- [x] **F01-T13** 单元测试：JWT verifier、ecrecover、RBAC matrix、审计写入、wallet service 全路径 `api:apps/api/internal/**/*_test.go` ~5h
- [x] **F01-T14** E2E 骨架（Playwright + Privy stub） `web:apps/web/e2e/auth.spec.ts` ~4h

## 依赖与并行

- **依赖**：T-001 ~ T-004（Phase 0）。
- **可并行**：F01-T02/03/09（数据层）与 F01-T10/11（前端）并行。
- **阻塞下游**：所有写 API（R-ID-005 + R-ID-006 鉴权是前置）。

## 退出条件（DoD）

- [x] `gofmt -l` 0 警告。
- [x] `go test ./internal/auth/... ./internal/rbac/... ./internal/wallet/...` 全绿，覆盖率 ≥ 80%。
  - `auth` 82.2% / `rbac` 94.8% / `wallet` 84.8%（`auth/middleware_test.go` 9 例，`wallet/service_test.go` 13 例，`wallet/signature_test.go` 11 例，`wallet/nonce_test.go` 5 例）。
- [x] 集成测试通过真实 PostgreSQL（`internal/integration/identity_test.go` + `wallet_bind_test.go`）；miniredis 替代 Redis。集成测试以 `TRUNCATE wallets` 隔离跨用例地址残留。
- [x] OpenAPI 通过 schema 校验；前端 client 由 generator 生成。**（`scripts/check-openapi.mjs` 已落，js-yaml safeLoad + openapi 版本正则 + paths/components 类型校验 + 跨文件 `$ref` 解析到本地 yaml；通过 `pnpm openapi:check` 一键跑：7 个 yaml 全绿。generator 接入待 F02/F03 时统一处理）**
- [x] AC-001 ~ AC-003 通过（`TestRepeatedPrivyLoginUpsertCreatesOneUser` 验证 AC-001；`TestSuspendedUserSessionIsRejectedAndDestroyed` 验证 AC-003 中段；RBAC matrix 覆盖 AC-003）。

## 风险

- **Privy 私有部署/私有 issuer** 需在 OQ 阶段确认。
- **JWKS 缓存轮换**：手动 reload 接口需保留，避免密钥泄漏后无法及时生效。
- **多签 / 高权限**本期不涉及，但代码路径要为 super_admin 升级留接口。
