# F01 — 身份与权限（Identity & RBAC）

> 来源：上级 `requirements.md` F-001 ~ F-007；本文件为本特性在 monorepo 中的实现切片。

## 1. 范围

- Privy 登录、用户自动开户、单用户多钱包绑定。
- `student / teacher / super_admin` 三角色 + permission 级 RBAC。
- 隐藏入口的超管鉴权（UI 不展示，API 必须拒绝越权）。
- 会话与个人资料查询；解绑最后一钱包不得破坏账号。

## 2. 功能需求

| ID | 描述 | 验收 |
|---|---|---|
| **R-ID-001** | 系统以数据库 `users.id`（UUID）作为唯一用户标识；钱包地址不作为主键 | AC-001：首次登录创建一行；重复登录不重复创建 |
| **R-ID-002** | API 必须验证 Privy access token，按 `privy_user_id` 幂等 upsert user | AC-001 |
| **R-ID-003** | 一个用户可绑定多钱包；`wallets(chain_id, normalized_address)` 全局唯一 | AC-002：他人已绑定时返回 409 + audit |
| **R-ID-004** | 系统支持 `student / teacher / super_admin` 角色 + permission 级授权；新用户默认仅 student | AC-003 |
| **R-ID-005** | teacher 角色需申请/审批或由超管授予；客户端声明不能提升权限 | AC-003：篡改前端角色仍 403 |
| **R-ID-006** | 超管路由在公开导航中隐藏；安全性必须由 API 鉴权保证，不依赖隐藏 URL | E2E 直接 GET `/admin/*` 返回 401/403 |
| **R-ID-007** | 解绑最后一钱包不得破坏 Privy 账号与历史订单；仅失去链上购买入口 | 集成测试 |

## 3. 用户故事

- 作为访客，我想注册并绑定钱包，以便使用 Web2 + Web3 功能。
- 作为学生，我想查看我的会话、钱包和角色，以便确认登录状态。
- 作为运维，我想通过 audit log 追溯钱包绑定冲突，以便排查恶意绑定。

## 4. 数据要求

- `users(id uuid pk, privy_user_id unique, status, created_at, updated_at)`
- `wallets(id, user_id fk, chain_namespace, chain_id, address_normalized, is_primary, created_at)`；`unique(namespace, chain_id, address)`
- `roles(id, code unique)` 种子：`student / teacher / super_admin`
- `permissions(id, code unique)` 例如 `COURSE_CREATE / COURSE_EDIT / COURSE_APPROVE / SYSTEM_ADMIN`
- `user_roles(user_id, role_id)` unique；`role_permissions(role_id, permission_id)` unique
- `audit_logs(actor, action, target, before/after JSONB, correlation_id, ip, created_at)` append-only

## 5. 非功能需求

- Privy token 校验 P95 ≤ 100 ms（JWKS 缓存命中）。
- RBAC middleware 不得引入额外 DB roundtrip（permission 走 in-memory cache，TTL 60s）。
- 钱包绑定签名必须防重放：nonce + domain + chain + expiry 四元组一次性。

## 6. 依赖与边界

- **依赖**：`apps/api`（新建 Go 服务）、`apps/web`（Vite+React 现有）；不直接依赖合约。
- **被依赖**：所有写 API 都依赖本特性的 RBAC middleware。
- **不包含**：Privy 控制台配置（属 `deploy/`）、密钥托管（属 `infra/aws`）。