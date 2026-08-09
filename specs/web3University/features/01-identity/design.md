# F01 — 身份与权限 设计

## 1. monorepo 落点

```text
apps/api/                                    # 新建 Go API
├── cmd/api/main.go
├── internal/
│   ├── auth/        # Privy JWT verifier + JWKS 缓存 + session
│   ├── rbac/        # middleware + 对象级 service
│   ├── wallet/      # nonce 签名绑定 + 唯一性冲突处理
│   ├── user/        # users / wallets / roles repository
│   ├── audit/       # append-only logger
│   └── httpkit/     # 错误格式、request ID、CORS
└── migrations/      # golang-migrate（与 database/migrations 协同）

apps/web/src/auth/                            # 新建（当前仅 components/, contracts/）
├── PrivyProvider.tsx
├── SessionBootstrap.tsx
├── PermissionContext.tsx
├── RequireAuth.tsx
└── RequirePermission.tsx

database/migrations/0001_identity.sql
packages/shared/openapi/auth.yaml            # OpenAPI 契约
```

## 2. 登录与开户流程

```text
Browser (Privy SDK)
  │ access token (JWT)
  ▼
POST /api/v1/auth/privy/session
  │ 1. 校验 issuer / audience / expiry / signature
  │ 2. 读 privy_user_id
  │ 3. tx: upsert users by privy_user_id
  │ 4. 同步已验证的钱包（来自 Privy linkedAccounts）
  │ 5. 返回 profile + permissions
  ▼
Set-Cookie: sid=... (httpOnly, secure, sameSite=lax)
```

**关键点**：
- Privy JWT 只在后端校验，JWKS 缓存 10 分钟并支持轮换。
- `privy_user_id` 是**唯一**幂等键；同一 user_id 的二次请求只刷新 `updated_at`。
- session 用随机 sid + 服务端 store（Redis），避免 JWT 长 token。

## 3. 钱包绑定（nonce 签名）

```text
POST /api/v1/me/wallets/link
Body: { chainNamespace, chainId, address, nonce, expiry, signature }

API 校验顺序：
  1. nonce 未使用过（Redis SETNX 5 分钟）
  2. expiry > now
  3. domain = 当前 API 域名（防跨站签名滥用）
  4. ecrecover(signature, nonce || chainId || address) == address
  5. tx:
     - 同 (chain_id, address) 已绑定别人 → 409 + audit "wallet_already_bound"
     - 同 user 已绑同地址 → idempotent ok
     - 否则 INSERT wallets(user_id, ...)
  6. 返回 wallets[] 列表
```

## 4. RBAC 模型

**两层级**：role（粗粒度，用于 UI 默认视图）+ permission（细粒度，用于 API 鉴权）。

```text
role ─── role_permissions ─── permission
user ─── user_roles        ─── role
```

- `student` 默认权限：`COURSE_READ`, `ORDER_CREATE`, `LESSON_PROGRESS_WRITE`, `CERTIFICATE_READ`
- `teacher`：`+ COURSE_CREATE / COURSE_EDIT（仅作者） / MEDIA_UPLOAD`
- `super_admin`：`*`（含 `COURSE_APPROVE / SYSTEM_ADMIN / CHAIN_SYNC_REPLAY`）

**API middleware**：
```go
RequirePermission("COURSE_EDIT")  // 路由级
RequireOwner(resource)            // 对象级（service 内二次校验）
```

**前端**：
- `<RequirePermission code="COURSE_CREATE">` 隐藏 UI（**仅 UX**，不可信）。
- **真正鉴权**永远走 API。

## 5. 错误格式（统一）

```json
{
  "error": {
    "code": "WALLET_ALREADY_BOUND",
    "message": "wallet is bound to another user",
    "requestId": "req_xxx",
    "details": { "chainId": 11155111, "address": "0x..." }
  }
}
```

## 6. 关键技术决策

| 决策 | 选择 | 理由 |
|---|---|---|
| JWT 校验库 | `github.com/golang-jwt/jwt/v5` + `github.com/MicahParks/keyfunc` | 主流、JWKS 缓存友好 |
| Session store | Redis（ElastiCache） | MVP 无状态 API；sid 比 JWT 易撤销 |
| 钱包签名算法 | EIP-191 `personal_sign`（EVM） | Privy 默认；与 wagmi 兼容 |
| Permission 缓存 | sync.Map + TTL 60s | 避免每请求查 DB |
| 对象级授权 | 在 service 层调用 `authz.CanEdit(ctx, user, course)` | 防 IDOR |

## 7. 安全检查清单

- [ ] Privy JWT：issuer + audience + expiry 必校验；JWKS 轮换日志留痕。
- [ ] 钱包签名：nonce 一次性 + domain + chain 绑定 + expiry。
- [ ] 任何修改角色 / 权限的接口必须 super_admin + audit_log。
- [ ] 解绑最后一钱包：service 层先校验 `count(wallets) > 1 || 已发起的 pending order == 0`。
- [ ] CORS 白名单；cookie httpOnly + secure + sameSite=lax。
- [ ] 限流：登录 10/min/IP；绑定 5/min/user。

## 8. 测试策略

- **单测**：JWT verifier、ecrecover、RBAC matrix、audit writer。
- **集成**：用 testcontainers 起 PostgreSQL + Redis；走完整开户/绑定/解绑路径。
- **E2E（Phase 8）**：Playwright 模拟 Privy stub，跑登录→绑定→访问受保护页。