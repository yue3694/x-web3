# apps/api · x-web3 后端 HTTP 服务

> Go + Gin + pgx + go-redis + Privy JWT 的业务 API。负责鉴权、课程目录、
> 评论、订单 intent 颁发、链下与链上的协调、S3 媒体签名、证书 metadata 生成、
> 以及对 worker / chain 的运营入口（chain rewind、DLQ、用户管理）。
>
> 全局架构、产品流程、合约地址与 AWS 拓扑统一在顶层
> [README.md](../../README.md) 维护，本文件只覆盖 API 模块自身的结构、启动
> 顺序、路由表、横切关注点和运行约束。

---

## 1. 模块定位

| 关注点 | 落在哪里 |
|---|---|
| 业务规则 | `internal/{course,catalog,review,order,learning,comment,certificate,media,user,wallet}` |
| HTTP 接入 | `internal/handlers`（业务 handler）+ `internal/admin/handlers`（运维 handler） |
| 横切 | `internal/{httpkit,auth,rbac,audit,errcode,config}` |
| 测试 | `internal/integration`（testcontainers + 真 Postgres）+ 各 package 单测 |

模块间无任何循环依赖；handler 通过依赖注入拿 service / repo。

---

## 2. 目录结构

```text
apps/api/
├── cmd/api/main.go              # 启动入口；详见 §3
├── go.mod                       # github.com/x-web3/api · Go 1.25
├── internal/
│   ├── config/                  # env 加载 + 校验（fail-fast）
│   ├── httpkit/                 # gin Router + 中间件 + 错误信封 + 限流
│   ├── auth/                    # Privy JWT verifier + Session cookie + middleware
│   ├── wallet/                  # EIP-191 nonce + SIWE-like 校验
│   ├── user/                    # user/role 表的 repo + 权限常量
│   ├── rbac/                    # 基于 user_role 的中间件
│   ├── audit/                   # audit_log 写入器（敏感操作留痕）
│   ├── errcode/                 # 错误码枚举
│   ├── catalog/                 # 课程目录服务（带 Redis 缓存失效订阅）
│   ├── course/                  # 课程、版本、定价、审批状态机
│   ├── review/                  # 课程审核状态机
│   ├── media/                   # 媒体上传 intent + finalize
│   ├── objectstore/             # S3 / fake 抽象（生产用 S3，开发用内存 fake）
│   ├── learning/                # 课程播放凭证 + 学习进度
│   ├── comment/                 # 课程评论 + 管理员审核
│   ├── order/                   # Purchase Intent + tx 提交 + 订单查询
│   ├── certificate/             # 完课证书 metadata 生成 + 铸造任务
│   ├── admin/handlers/          # /admin/* 路由（rewind / dlq / users / chain/sync / cert retry）
│   ├── handlers/                # 业务路由（auth / wallet / me / courses / orders / teacher / lessons）
│   └── integration/             # testcontainers 全链路集成测试
```

`internal/` 之外不会有任何业务代码；`cmd/api/main.go` 只做装配，不写规则。

---

## 3. 启动顺序

`cmd/api/main.go` 的装配序列写在文件头注释里，这里再列一遍方便对照：

```text
1. config.LoadDotenv / config.MustLoad       // 校验必填 + prod-only 约束
2. zap logger (production / development)
3. PostgreSQL pool (pgx)                      // ping 必须通过，否则 fatal
4. Redis client                               // ping 必须通过
5. Privy verifier (auth.Config{DevStub, …})  // staging/prod 禁开 DevStub
6. Session store + wallet nonce store + audit writer + rbac engine
7. courseRepo / catalogSvc / mediaRepo / commentRepo
8. objectstore：prod 走 S3，dev/test 走 fake（避免本地依赖 AWS）
9. learningSvc / orderSvc / certificateSvc
10. httpkit.NewRouter + Prometheus /metrics + /healthz + /readyz
11. 注册所有 /api/v1/* 路由（见 §4）
12. catalogSvc.SubscribeInvalidate(ctx) 后台 goroutine
13. http.Server.ListenAndServe，graceful shutdown on SIGINT/SIGTERM
```

每一行失败都会 `logger.Fatal(...)`；不允许「部分启动 + 部分失败」。

---

## 4. 路由表

所有业务路由挂在 `/api/v1` 之下；`/metrics`、`/healthz`、`/readyz` 直挂根路径。

| 分组 | 路径 | 方法 | 鉴权 | 用途 |
|---|---|---|---|---|
| `/auth` | `privy/session` | POST | IP 限流 | Privy access token 兑换 sid cookie |
| `/auth` | `wallet/nonce` | POST | IP 限流 | 颁发 EIP-191 nonce |
| `/auth` | `wallet/session` | POST | IP 限流 | 校验签名 + 颁发 sid cookie |
| `/auth` | `session/refresh` | POST | session | 刷新 sid |
| `/auth` | `session` | DELETE | — | 登出 |
| `/me` | `` (root) | GET/PATCH | session | 当前用户信息 / 修改昵称 |
| `/me/wallets` | `nonce` / `link` / `:id` | POST/POST/DELETE | session + 用户级限流 | 钱包绑定 |
| `/me` | `enrollments` / `certificates` / `comments` / `orders` | GET | session | 「我的」四类资源 |
| `/courses` | `` / `:id` | GET | 公开 / 可选 session | 课程列表 + 详情 |
| `/courses/:id/comments` | | GET | 公开 | 课程评论列表 |
| `/courses/:id/comments` | | POST | session | 发表评论 |
| `/courses/comments/:id` | | DELETE | session | 删自己的评论 |
| `/courses/:id/complete` | | POST | session + 有 enrollment | 完课（推证书铸造） |
| `/orders` | `purchase-intents` | POST | session | 颁发 Purchase Intent |
| `/orders/:intentId/transactions` | | POST | session | 上报 txHash |
| `/orders/:id` | | GET | session | 查询订单 |
| `/teacher/courses` | | GET/POST | RBAC(`course:create`) | 教师草稿管理 |
| `/teacher/courses/:id` | | PUT | RBAC(`course:edit`) | 编辑课程 |
| `/teacher/courses/:id/curriculum` | | PUT | RBAC(`course:edit`) | 替换目录 |
| `/teacher/courses/:id/submit` | | POST | RBAC(`course:edit`) | 提交审核 |
| `/teacher/media` | upload-intent / finalize / mine | POST | session | S3 预签名上传 |
| `/teacher/lessons/:id/preview` | | GET | RBAC(`course:edit`) | 教师预览课节 |
| `/admin/courses` | / `:id/review` / `:id/archive` | GET/POST/POST | session + RBAC(`course:approve`) | 审核与上下架 |
| `/admin/comments/:id` | | PATCH | session + RBAC(`comment:moderate`) | 评论审核 |
| `/lessons/:id/playback` | | GET | session + enrollment | 播放凭证 |
| `/lessons/:id/progress` | | POST | session + enrollment | 上报进度 |
| `/admin/chain/rewind` | | POST | session + RBAC(`system:admin`) | 手动 rewind |
| `/admin/dlq` / `/admin/dlq/:id/retry` | | GET/POST | session + RBAC(`system:admin`) | DLQ 列表 / 重试 |
| `/admin/users` / `/admin/users/:id/roles` | | GET/POST/DELETE | session + RBAC(`system:admin`) | 用户与角色 |
| `/admin/chain/sync` | | GET | session + RBAC(`system:admin`) | 索引器健康 |
| `/admin/certificates/:id/retry` | | POST | session + RBAC(`system:admin`) | 证书 mint 重试 |
| `/admin/ping` | | GET | session + RBAC(`system:admin`) | 探活 |
| `/metrics` | | GET | 无（**生产请用防火墙限制**） | Prometheus |
| `/healthz` | | GET | 无 | 进程探活 |
| `/readyz` | | GET | 无 | DB + Redis 探活 |

> **RBAC 权限常量**：`user.PermCourseCreate / PermCourseEdit / PermCourseApprove /
> PermCommentModerate / PermSystemAdmin`，定义在 `internal/user/repo.go`，
> 具体角色在 `internal/rbac/engine.go` 解析。

---

## 5. 横切关注点

### 5.1 中间件栈（`internal/httpkit/router.go`）

`gin.New()` 之后顺序挂：

1. `requestIDMiddleware()` — 取 `X-Request-ID` header，缺失则生成 UUID，注入 `gin.Context` 与响应头。
2. `corsMiddleware(allowedOrigin)` — 只放行 `WEB_ORIGIN`；缺失 `Origin` 的同源 / CLI 调用放行。响应携带 `Access-Control-Allow-Credentials`。
3. `accessLogMiddleware(logger)` — 每个请求写一条结构化日志（method / path / status / latency / request_id / client ip）。
4. `MetricsMiddleware()` — 默认 Registry，统计 HTTP QPS / 延迟。
5. `recoveryMiddleware(logger)` — panic → 500 + 结构化堆栈日志。

`/metrics` 不经过该栈（避免 Prometheus 高频抓取污染指标），由 `main.go` 直接挂 `promhttp.HandlerFor(...)`。

### 5.2 鉴权（`internal/auth`）

- `Verifier.Verify(ctx, token)` 校验 Privy JWT（JWKS 拉取 + audience 校验 + dev stub）。
- `SessionStore` 用 Redis 存 session，cookie 名 `sid`，HMAC 签名；`SESSION_TTL_HOURS` 控制 TTL。
- `Middleware(verifier, sessionStore, pool)` 注入 `user_id` 到 `gin.Context`；`OptionalMiddleware` 同名但不强制。
- 「钱包登录」走 `internal/wallet` 的 EIP-191 nonce + SIWE 风格签名校验，签发同样的 `sid`。

### 5.3 RBAC（`internal/rbac`）

`Engine.Middleware(perm)` 在 `auth.Middleware` 之后挂；命中 `user_role` 表判断是否拥有该权限常量。未通过调用 `audit.Writer` 写一条 `denied` 记录。

### 5.4 Audit（`internal/audit`）

敏感动作（admin rewind / dlq retry / user role / cert retry / 课程审批 / 评论审核）必须写一条 `audit_log`，包含 actor / action / target / before / after / IP / UA / correlation_id。Handler 在动手前先写 `attempted`，失败再写 `failed`，成功再写终态。

### 5.5 错误信封（`internal/httpkit` + `internal/errcode`）

```jsonc
{
  "error": {
    "code": "ORDER_ALREADY_PURCHASED",
    "message": "已经购买过这门课程",
    "requestId": "…",
    "details": { "courseId": "…" }
  }
}
```

`errcode` 集中维护错误码常量，handler 用 `httpkit.Error(c, status, code, msg, details)` 输出。

### 5.6 限流（`internal/httpkit/rate_limit.go`）

基于 Redis token bucket。两个 key 维度：

- `ClientIPKey` — `login` / `wallet-login`（防撞库、防 nonce 风暴）。
- `UserIDKeyFunc` — `wallet`（防单个用户钱包绑定洪水）。

额度来自 `config.LoginRateLimit / WalletRateLimit`。

### 5.7 对象存储（`internal/objectstore`）

```go
// 选择规则：prod → S3；其余 → 内存 fake
if cfg.IsProd() {
    objStore, _ = objectstore.NewS3Store(ctx, cfg.AWSRegion, cfg.ObjectStoreBucket)
} else {
    objStore = objectstore.NewFakeStore()
}
```

本地开发 / 测试不需要 AWS 账号；走 fake store 也能跑完整集成测试。

---

## 6. 关键业务规则速查

| 模块 | 不变量 | 文件 |
|---|---|---|
| `order.Service.CreateIntent` | 冻结 `amount / courseKey / market / token / priceVersion / chainId`；同 `(user_id, idempotency_key)` 幂等；已购直接 `ALREADY_PURCHASED` | [internal/order/order.go](internal/order/order.go) |
| `courseKey` 算法 | `sha256(uuid 16 字节 hex)`（**非 keccak256**），与 web / worker / 合约事件保持一致 | 同上 |
| `order.Service.PostTransaction` | `(chain_id, tx_hash)` 唯一；链必须与 intent 一致 | 同上 |
| `learning.Service` | 进度单调递增；完成条件由 `internal/certificate.CompleteCourse` 检查 | [internal/learning/learning.go](internal/learning/learning.go) |
| `certificate.Service.CompleteCourse` | 必须在已有 enrollment 的前提下调用；写 `certificate_jobs` 给 worker 消费 | [internal/certificate/completion.go](internal/certificate/completion.go) |
| `catalog.Service` | 课程发布后才出现在目录；缓存由 `course` 域事件驱动失效 | [internal/catalog/catalog.go](internal/catalog/catalog.go) |
| `objectstore.NewS3Store` | 仅 prod 使用；其他环境用 fake，否则本地依赖 AWS | [internal/objectstore/s3.go](internal/objectstore/s3.go) |
| `admin.handlers.ChainRewind` | 走 `chain_checkpoints(chain_id, consumer)` 行锁，与 worker 自动 reorg 互斥 | [internal/admin/handlers/chain_rewind.go](internal/admin/handlers/chain_rewind.go) |

---

## 7. 配置

详见 [internal/config/config.go](internal/config/config.go)。`MustLoad()` 失败直接 panic。

最小本地运行所需环境变量：

```bash
DATABASE_URL=postgres://…
REDIS_URL=redis://…
SESSION_SECRET=（≥32 字节随机串）

# dev 可选
PRIVY_DEV_STUB=1
PRIVY_DEV_STUB_SUBJECT=dev-user-id
WEB_ORIGIN=http://localhost:5173
CHAIN_ID=31337           # 本地 Anvil；Sepolia=11155111
YD_TOKEN_ADDRESS=0x…
COURSE_MARKET_ADDRESS=0x…
```

prod-only 约束（`IsProd()` 后会校验）：

- `SESSION_COOKIE_SECURE=true`
- `OBJECT_STORE_BUCKET` 非空
- `PRIVY_DEV_STUB=0`

---

## 8. 本地开发

```bash
# 1) 启动 Postgres + Redis（仓库根的 deploy/ 提供 compose；也可复用现有实例）
docker compose -f deploy/docker-compose.yml up -d redis anvil

# 2) 迁移
pnpm db:migrate

# 3) 跑 API（自动加载仓库根 .env 与 apps/api/.env）
pnpm api:dev
# 等价：cd apps/api && go run ./cmd/api

# 4) 调一个无 auth 的端点验证
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

完整本地闭环（含 Anvil + Worker）见 [docs/dev/anvil-loop.md](../../docs/dev/anvil-loop.md)。

---

## 9. 测试

```bash
# 单元测试（每个 internal 包都带 _test.go）
pnpm api:test
# 等价：cd apps/api && go test ./...

# 集成测试（testcontainers + Postgres + Anvil）：自动起容器，需 docker 可用
cd apps/api && go test ./internal/integration/... -tags=integration
```

测试分层：

| 层 | 工具 | 覆盖 |
|---|---|---|
| 单元 | `go test` | `config / httpkit / rbac / auth / wallet / order / certificate / catalog / audit` |
| 集成 | `testcontainers-go`（Postgres）+ miniredis | 全 endpoint 跨 service 流程（identity / wallet_bind / order / learning / completion / comment / F03 AC 校验） |
| 共享 fixture | `internal/integration/testenv` + `helpers_test.go` | 用户 / 角色 / 钱包 / 课程的 setup；多测试复用 |

跑覆盖率：

```bash
cd apps/api && go test ./... -coverprofile=cover.out && go tool cover -html=cover.out
```

---

## 10. 部署注意事项

- `apps/api` 通过 CloudFront `/api/*` 回源到 EC2 Nginx（Nginx 再转发到 Go API）。EC2 80 端口**只允许 CloudFront 回源网段**，否则会被任意调用。
- `/metrics` 是公开抓取端点（无 auth），生产请用防火墙 / sidecar 限制到内部抓取网段；`main.go` 注释里已强调。
- Secrets Manager 注入 `DATABASE_URL / REDIS_URL / SESSION_SECRET / PRIVY_*`，**禁止**写入镜像或 `.env` 提交。
- `SESSION_COOKIE_SECURE=true` 在 prod 必须打开；`SESSION_SECRET` 至少 32 字节。
- `IS_PROD=1` 时 `objectstore` 强制 S3；其余环境走 fake。

---

## 11. 进一步阅读

- 全局架构：[docs/ARCHITECTURE.md](../../docs/ARCHITECTURE.md)
- 产品流程：[docs/PRODUCT-FLOWS.md](../../docs/PRODUCT-FLOWS.md)
- 链 rewind / DLQ 详细契约：[docs/runbooks/chain-replay.md](../../docs/runbooks/chain-replay.md)
- 钱包签名 + EIP-191 流程：[internal/wallet/service.go](internal/wallet/service.go)
- Worker 链索引侧：[../worker/README.md](../worker/README.md)