# apps/web E2E（Playwright + Privy stub）

F01-T14 骨架。覆盖 `auth.spec.ts` 几条核心身份路径。

## 跑测试

```bash
# 一次性下载浏览器（本地可能需要）
pnpm --filter @x-web3/web e2e:install

# headless 跑
pnpm --filter @x-web3/web e2e

# 调试 UI（headed + inspector）
pnpm --filter @x-web3/web e2e:ui
```

CI：

```bash
CI=1 pnpm --filter @x-web3/web e2e
```

会切到 `github` reporter，`reuseExistingServer=false`，并自动 retry 2 次。

## 工作原理

1. **`playwright.config.ts`** 启动 `vite dev server`（端口 `4173`），注入：
   - `VITE_PRIVY_DEV_STUB=1` → `PrivyRuntime` 直接返回 children，不加载 `@privy-io/react-auth`；
   - `VITE_API_BASE_URL=/api/v1` → 走同源，让 `page.route` 能稳定拦截。
2. **`e2e/fixtures/privy-stub.ts`** 用 `page.route('**/api/v1/**')` 拦截后端调用，
   模拟：
   - `POST /auth/privy/session` — 信任任意 token，返回固定 profile + `sid` cookie；
   - `GET /me` — 按 sid 是否存在返回 200/401；
   - `DELETE /auth/session` — 清 sid；
   - `POST /me/wallets/link` — 同地址幂等；冲突地址 → 409 `WALLET_ALREADY_BOUND`；
   - `GET /admin/*` — 默认 401，验证 R-ID-006「隐藏路由不可信」。
3. **`e2e/auth.spec.ts`** 跑 F01 验收矩阵：登录、幂等、角色门、登出、钱包冲突。

## 与真实 Go API 的对接

- Go API 上线后，把 `playwright.config.ts` 的 `webServer.command` 之后追加 backend 启动，
  或把 VITE_API_BASE_URL 改回 `http://localhost:8080/api/v1` 并配合 `webServer` 启 Go API；
- 然后删除 `e2e/fixtures/privy-stub.ts` 中的路由拦截，转而用真实 `testcontainers`
  起 Postgres + Redis + `apps/api`（对应 F01-T12 集成测试）。当前 stub 行为是后端
  spec 的「契约快照」，上线时需逐项对照 OpenAPI `packages/shared/openapi/auth.yaml`。

## 已知边界

- ConnectKit（钱包连接 UI）在测试中不会被点击，避免依赖真钱包扩展；
- `PrivyRuntime` 在 dev-stub 模式下不会触发 lazy chunk 加载，所以 chromium 看到
  的 network 没有 `@privy-io/react-auth` 的请求——这是预期。
- 调试某个用例：`pnpm exec playwright test --debug e2e/auth.spec.ts`。