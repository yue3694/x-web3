# apps/web · x-web3 前端应用

> React 18 + Vite 5 + wagmi v2 + viem v2 + ConnectKit + React Router 6 +
> TanStack Query 5 的 Web3 课程平台前端。直接与 Sepolia 链上的 Foundry 合约
> 交互，配合 Go API 完成鉴权 / 课程目录 / 订单 / 学习 / 证书。
>
> 全局架构、产品流程、合约地址与 AWS 拓扑统一在顶层
> [README.md](../../README.md) 维护。本文件覆盖前端模块自身的目录结构、
> 启动顺序、Provider 栈、路由、状态边界、ABI 桥、鉴权与钱包链路、测试与
> 部署约束。

---

## 1. 模块定位

| 关注点 | 落在哪里 |
|---|---|
| 业务 UI | `src/features/**`（每个 feature 一个子目录） |
| 路由壳 | `src/App.tsx` + `src/pages/**`（`AppShell` + 懒加载页面） |
| 应用入口 / Providers | `src/main.tsx` |
| 钱包 / wagmi 配置 | `src/wagmi.ts` + `src/chains.ts` |
| 鉴权 / Session | `src/auth/**` |
| 后端 HTTP 接入 | `src/api/**` |
| 合约 ABI / 部署地址 | `src/contracts/**`（ABI 由 Foundry artifact 生成，地址手填） |
| 通用组件 | `src/components/**`（TopNav / Footer / Notify / Hero / Select） |
| 样式 | 单文件 `src/styles.css`，CSS 变量驱动主题 |
| 测试 | Vitest 单测 + Playwright e2e（`e2e/`） |

`src/` 之外不会有任何业务代码；`src/main.tsx` 只做 Providers 装配，
不写业务规则。

---

## 2. 目录结构

```text
apps/web/
├── public/                            # 静态资源（favicon 等）
├── src/
│   ├── main.tsx                       # 应用入口：装配 Providers 栈
│   ├── App.tsx                        # 路由表 + AppShell
│   ├── App.test.tsx                   # 路由 smoke test
│   ├── wagmi.ts                       # createConfig(connectkit default + Sepolia RPC)
│   ├── chains.ts                      # 业务链常量 (TARGET_CHAIN_ID/NAME, targetRpcUrl)
│   ├── vite-env.d.ts                  # import.meta.env 类型声明
│   ├── styles.css                     # 全局样式 + CSS 变量（赛博朋克主题）
│   ├── auth/                          # 鉴权 & Session
│   │   ├── PrivyRuntime.tsx                # Privy 入口包裹器
│   │   ├── PrivyProviderRuntime.tsx        # Privy Provider 装配
│   │   ├── PrivySignInButton.tsx           # 第三方登录按钮
│   │   ├── SignInButton.tsx                # 钱包登录按钮（EIP-191）
│   │   ├── SessionContext.tsx              # Session Context + Provider
│   │   ├── SessionContext.test.tsx
│   │   ├── WalletAutoSession.tsx           # 钱包连接 → 自动注册 walletId
│   │   ├── RequireAuth.tsx                 # 路由守卫：未登录跳登录
│   │   └── RequirePermission.tsx           # 路由守卫：RBAC
│   ├── api/                           # 后端 HTTP 接入
│   │   ├── client.ts                  # ApiClient：fetch + ApiClientError + X-Request-ID
│   │   ├── learning.ts                # 学习进度 / 完课专用方法
│   │   └── types.ts                   # 视图层 DTO（订单 / 证书 / 评论 等）
│   ├── components/                    # 通用组件
│   │   ├── TopNav.tsx · Footer.tsx · Hero.tsx · Select.tsx
│   │   └── NotifyProvider.tsx         # 全局 toast 通知
│   ├── contracts/                     # 合约绑定（ABI 由 export:abi 自动生成）
│   │   ├── *.abi.ts                   # 每个合约一份
│   │   └── deployments.ts              # 每个合约的 chain → address（手填）
│   ├── features/                      # 业务域
│   │   ├── wallet/WalletLink.tsx                  # 钱包绑定
│   │   ├── catalog/                                # 课程目录 + 详情 + 评论
│   │   │   ├── CourseCatalog.tsx · CourseCatalog.test.ts
│   │   │   ├── CourseDetail.tsx
│   │   │   ├── Comments.tsx · CommentItem.tsx
│   │   ├── checkout/                               # F03 课程购买主流程
│   │   │   ├── CheckoutButton.tsx                  # 状态机：preparing → ... → done
│   │   │   ├── CheckoutPanel.tsx                   # 容器（价格 + intentId + walletId）
│   │   │   ├── OracleReferencePrice.tsx            # ETH/USD 参考价展示
│   │   │   ├── checkoutTypes.ts                    # 状态 / 类型
│   │   │   ├── checkoutUtils.ts · checkoutUtils.test.ts
│   │   │   └── derive.ts · derive.test.ts          # courseKey / intentId 派生
│   │   ├── swap/SepoliaEthYDSwap.tsx               # F05 SepoliaETH → YD
│   │   ├── learning/                               # F04 学习播放与进度
│   │   │   ├── Player.tsx
│   │   │   ├── ProgressReporter.tsx
│   │   │   └── playbackRules.ts · playbackRules.test.ts
│   │   ├── teacher/                                # 教师工作台
│   │   │   ├── CourseEditor.tsx · CourseEditor.test.tsx
│   │   │   ├── ChapterReorderList.tsx
│   │   │   ├── MediaUploader.tsx · MediaUrlAttacher.tsx
│   │   │   └── teacherTypes.ts
│   │   ├── account/                                # 「我的」四类资源
│   │   │   ├── MyOrders.tsx · OrderRow.tsx · MyOrders.types.ts
│   │   │   ├── MyEnrollments.tsx
│   │   │   ├── MyCertificates.tsx
│   │   │   ├── MyComments.tsx
│   │   │   ├── UserMenu.tsx                        # 账户菜单（YD 余额 / 角色 / 钱包地址）
│   │   │   └── types.ts
│   │   └── admin/                                  # 管理后台
│   │       ├── AdminLayout.tsx · ConfirmRequired.tsx
│   │       ├── adminApi.ts · adminTypes.ts
│   │       ├── courses/CourseReviewPage.tsx
│   │       ├── users/UsersPage.tsx · UserRow.tsx
│   │       ├── chain/ChainStatusPanel.tsx
│   │       └── dlq/DlqPage.tsx
│   └── pages/                         # 路由文件（懒加载入口）
│       ├── HomePage.tsx · CatalogPage.tsx · CoursePage.tsx
│       ├── SwapPage.tsx · LearningPage.tsx · StudioPage.tsx
│       ├── AccountLayout.tsx · NotFoundPage.tsx
├── e2e/                               # Playwright 端到端
│   ├── auth.spec.ts · purchase.spec.ts · certificate.spec.ts
│   ├── fixtures/ · README.md
├── vite.config.ts                     # alias @/* · proxy /api → 127.0.0.1:8080
├── tsconfig.json · tsconfig.node.json
├── vitest.config.ts                   # jsdom + React Testing Library
├── playwright.config.ts               # Chromium + localStorage state 持久化
├── eslint.config.js
├── index.html
└── package.json                       # @x-web3/web · 依赖 @x-web3/shared
```

---

## 3. 应用入口与 Providers 栈

`src/main.tsx` 自下而上装配：

```text
PrivyRuntime                              # 第三方登录（Privy wrapper）
  └─ WagmiProvider(wagmiConfig)           # wagmi.ts：Sepolia RPC + connectkit
       └─ QueryClientProvider             # @tanstack/react-query
            └─ ConnectKitProvider         # 钱包连接 UI + 赛博朋克主题
                 └─ SessionProvider       # Go API cookie 会话 (auth/SessionContext)
                      └─ NotifyProvider   # 全局 toast
                           └─ WalletAutoSession  # 钱包连接 → 调 API 注册 walletId
                                └─ BrowserRouter
                                     └─ <App />    # App.tsx：路由表 + AppShell
```

任何 Provider 替换或顺序调整都会改变整体行为，调整前请阅读
[`.claude/rules/frontend.md`](../../.claude/rules/frontend.md) 的相关约束。

---

## 4. 路由表

`App.tsx` 的路由用 React Router 6 + `React.lazy` 实现页面级懒加载，
`AppShell` 提供 `TopNav` / `Footer` / 滚动复位 / 跳转主内容。

| 路径 | 页面 | 鉴权 | 用途 |
|---|---|---|---|
| `/` | `HomePage` | 公开 | 产品入口 + 学习旅程 |
| `/courses` | `CatalogPage` | 公开 | 课程目录（搜索 / 分页） |
| `/courses/:courseId` | `CoursePage` | 公开 / 可选 session | 课程详情、评论、购买入口 |
| `/swap` | `SwapPage` | 钱包连接 | SepoliaETH → YD |
| `/learn/:courseId?lesson=:lessonId` | `LearningPage` | 报名 + session | 选课节、播放、上报进度 |
| `/account` | `AccountLayout` | session | 容器；默认重定向到 `enrollments` |
| `/account/orders` | `AccountOrdersPage` | session | 链上订单状态 |
| `/account/enrollments` | `AccountEnrollmentsPage` | session | 报名列表 |
| `/account/certificates` | `AccountCertificatesPage` | session | 完课证书 |
| `/account/comments` | `AccountCommentsPage` | session | 我的评论 |
| `/studio` | `StudioPage` | RBAC(`course:create\|edit`) | 教师工作台 |
| `/admin` | `AdminLayout` | RBAC(`system:admin`) | 容器；默认跳 `/admin/courses` |
| `/admin/courses` | `CourseReviewPage` | RBAC(`course:approve`) | 课程审核 |
| `/admin/users` | `UsersPage` | RBAC(`system:admin`) | 用户与角色 |
| `/admin/chain` | `ChainStatusPanel` | RBAC(`system:admin`) | 索引器健康 |
| `/admin/dlq` | `DlqPage` | RBAC(`system:admin`) | DLQ 列表 / 重试 |
| `*` | `NotFoundPage` | — | 404 |

> 路由守卫在 `auth/RequireAuth.tsx` 与 `auth/RequirePermission.tsx` 实现；
> 真正的权限判定在 API 端，前端守卫只是 UX。

---

## 5. 鉴权 / 钱包链路

### 5.1 两条登录路径

1. **Privy 第三方登录**：`PrivyRuntime` → `PrivyProviderRuntime` →
   `PrivySignInButton`。换取 Privy access token → `POST /api/v1/auth/privy/session`
   → 颁发 `sid` cookie。
2. **钱包登录（EIP-191）**：`SignInButton` 触发 →
   `POST /api/v1/auth/wallet/nonce` 拿 nonce → 钱包签名 → `POST /api/v1/auth/wallet/session`
   颁发 `sid` cookie。

`SessionProvider`（`auth/SessionContext.tsx`）集中持有当前 session（user / 角色 / 钱包绑定），
所有业务 hook 都从这里读。

### 5.2 WalletAutoSession

钱包连接（`useAccount().isConnected` 翻转）→ 自动调
`POST /api/v1/me/wallets/link` 把当前地址绑定到登录用户。
未登录时这一步 no-op；登录后再连钱包会自动补登记。

### 5.3 SessionContext 类型覆盖：

- `user: { id, nickname, roles[] }`
- `wallets: WalletBinding[]`
- `ydBalance: bigint`
- `setNickname / refresh / signOut`

### 5.4 RBAC 权限常量

- 前端只读：`@x-web3/shared` 的权限码 + API 返回的 `roles[]`。
- 严格守卫：每个受控 UI（教师工作台、管理后台）都要过
  `RequirePermission`；真实权限判定永远在 API 端再校一次。

---

## 6. wagmi / viem 配置

### 6.1 `src/chains.ts`

```ts
export const targetChain = sepolia;
export const TARGET_CHAIN_ID = targetChain.id;
export const TARGET_CHAIN_NAME = targetChain.name;
export const targetRpcUrl =
    import.meta.env.VITE_SEPOLIA_RPC_URL
    ?? import.meta.env.VITE_RPC_URL
    ?? "https://rpc.sepolia.org";
```

> `targetChain` 是合约调用与守卫的事实来源；改链必须同步更新
> `deployments.ts` 与 `wagmi.ts`。

### 6.2 `src/wagmi.ts`

- 用 `connectkit` 的 `getDefaultConfig` 注入 Sepolia + RPC；
- `chains: [targetChain]`；
- `batch: { multicall: ... }` 在 `VITE_PRIVY_DEV_STUB=1` 时关闭，便于 Playwright 拦截单独的 JSON-RPC 调用做断言。
- `declare module "wagmi"` 把 config 注册到 wagmi 类型系统，让 hooks 自动推断参数与返回。

### 6.3 wagmi hooks 使用规约（`.claude/rules/frontend.md`）

- **永远用 v2 hooks**：`useAccount / useConnect / useDisconnect /
  useReadContract / useWriteContract / useWaitForTransactionReceipt`。
- **写交易前必须** `useAccount()` 确认 `isConnected` 与
  `chainId === targetChain.id`，否则给用户错误提示。
- **不要在 `useEffect` 里发交易**；用 `useEffect(() => refetch(), [isConfirmed])`
  模式刷新读。
- **链切换**：通过 `useSwitchChain()`；不要在 dApp 里手动调 `wallet_switchEthereumChain`。

---

## 7. 合约 ABI 与地址（`src/contracts/`）

### 7.1 ABI 自动生成

ABI 文件（`*.abi.ts`）由 `packages/contracts/script/export-abi.mjs` 从 forge
构建产物 `out/<Contract>.sol/<Contract>.json` 提取，每个 ABI 都带
`as const` 让 wagmi 推断函数签名 / 参数 / 返回。

**绝对不要手动编辑 ABI 文件**——改完下次 `pnpm contracts:export:abi` 会被覆盖。

```bash
pnpm contracts:compile        # forge build
pnpm contracts:export:abi     # 生成 / 覆盖 apps/web/src/contracts/*.abi.ts
```

`export:abi` 默认导出：`Counter / Notepad / CourseMarket / YDToken / CertificateNFT / ChainlinkPriceOracle`。
新合约加完后，把名字加进 `packages/contracts/package.json` 的 `export:abi` 行。

### 7.2 部署地址手填

`src/contracts/deployments.ts` 是**唯一**知道「合约名 → chain → 部署地址」
的地方。结构示例：

```ts
export const courseMarketDeployments = {
    target: {
        address: optionalAddress(import.meta.env.VITE_COURSE_MARKET_ADDRESS),
        chainId: TARGET_CHAIN_ID,
    },
} as const;
```

- `optionalAddress(...)` 用正则 `^0x[0-9a-fA-F]{40}$` 校验；缺失 / 不合法
  返回 `undefined`，UI 走「合约尚未部署」分支而不是崩溃。
- 重部署后**必须**更新对应 `VITE_*` env（或修改 `optionalAddress` 内的字面量）。

---

## 8. 后端 HTTP 接入（`src/api/`）

### 8.1 `ApiClient`（`api/client.ts`）

封装 `fetch`，统一注入：

- `credentials: 'include'`（让 `sid` cookie 自动随请求发出）；
- `X-Request-ID` 头（默认 `crypto.randomUUID()`，便于服务端日志关联）；
- `Content-Type: application/json`；
- 错误信封解析：`{ error: { code, message, requestId, details? } }` →
  `ApiClientError`（含 `code / status / requestId / details`）。

**禁止在组件里直接 `fetch`**——所有调用都必须走 `apiClient`，否则上面的
横切关注点会漂移。

### 8.2 共享契约

错误码常量来自 `@x-web3/shared/errors`（`ErrorCode`）；事件 ABI 来自
`@x-web3/shared/events`。HTTP envelope / EventTopic 任何一处改动都要三端
（web / api / worker）同步。

### 8.3 BaseURL

```ts
baseURL ?? import.meta.env.VITE_API_BASE_URL ?? "/api/v1"
```

- 默认走 Vite dev server 的 `/api/v1`，由 `vite.config.ts` 的 proxy 转发到
  `http://127.0.0.1:8080`（即本地 Go API）。
- 生产环境通过 `VITE_API_BASE_URL=https://api.example.com/api/v1` 走
  CloudFront 回源。

---

## 9. 关键 Feature 速查

### 9.1 课程购买（`features/checkout/`）

完整状态机（写在 `CheckoutButton.tsx` 文件头注释）：

```text
idle → preparing → checking → approving? → signing → confirming → done | failed
```

| 文件 | 作用 |
|---|---|
| `CheckoutButton.tsx` | 状态机主体；守卫 + RPC 预检 + ERC20 approve + buyCourse + 上报 txHash |
| `CheckoutPanel.tsx` | 容器（接收 `courseId / priceYD / courseKey / walletId`） |
| `OracleReferencePrice.tsx` | 展示 ETH/USD 参考价（不参与实际汇率） |
| `derive.ts` | `courseKeyFromUuid`（**sha256**）/ `uuidToBytes16`（高 128 位）|
| `derive.test.ts` | 算法 fixture，覆盖算法 SSOT 漂移 |
| `checkoutUtils.ts` | 按钮文案 / 错误归一 / `isUserRejected` |

**SSOT（必须与 [apps/api](../api/README.md) + [apps/worker](../worker/README.md) 对齐）**：

- `courseKey = sha256(uuid 16 字节 binary)`（**非 keccak256**）。
- `intentId = uuid 高 128 位 hex`（合约事件字段是 `bytes16`）。

### 9.2 课程目录 / 详情（`features/catalog/`）

- `CourseCatalog.tsx` 走 `apiClient.get("/courses?...")` 拿分页；
- `CourseDetail.tsx` 走 `apiClient.get("/courses/:id")`，可选 session（详情 API
  在 [api/internal/catalog](../../apps/api/internal/catalog/)）；
- `Comments.tsx` 用乐观更新调 `POST /courses/:id/comments`，评论展示用
  `createdAt` 倒序。

### 9.3 Swap（`features/swap/SepoliaEthYDSwap.tsx`）

走 `SepoliaYDSale.quote() / buy(recipient)`，Gas 与输入都是 SepoliaETH。
详见 [docs/adr/0007-chainlink-mvp.md](../../docs/adr/0007-chainlink-mvp.md)
与 ADR-0004（定价货币）。

### 9.4 学习（`features/learning/`）

- `Player.tsx` 拿到 `playbackRules.ts` 算出的播放 URL（短时凭证）后挂播放器；
- `ProgressReporter.tsx` 单调上报 `POST /lessons/:id/progress`；
- `playbackRules.ts` 集中保存上课节 / 进度 / 凭证规则（单测覆盖）。

### 9.5 教师（`features/teacher/CourseEditor.tsx`）

课程草稿 / 版本 / 目录 / 提交审核。前端组件级测试见
`CourseEditor.test.tsx`。

### 9.6 管理（`features/admin/`）

- `AdminLayout` + `ConfirmRequired`（敏感操作二次确认）；
- 5 个子页面对应后端 `/admin/*`：课程审核、用户、链状态、DLQ、证书重试。

### 9.7 账户（`features/account/`）

5 个列表 + 菜单；YD 余额在 `UserMenu` 中以 `useReadContract({ yDToken.balanceOf })`
直读链上。

---

## 10. 样式约定

- 单文件 `src/styles.css`，CSS 变量驱动主题；
- 颜色变量在 `:root` 与 `@media (prefers-color-scheme: dark)` 中定义；
- 新增色板请走变量，不要硬编码 hex；
- 不引入 Tailwind / styled-components；
- 列表 key 必须是稳定 ID，**禁止** `index`。

---

## 11. 环境变量（前端可注入的）

只有 `VITE_*` 前缀的变量会进入 bundle。只放公开值。

| 变量 | 用途 | 必填？ |
|---|---|---|
| `VITE_API_BASE_URL` | API base URL（如 `https://api.example.com/api/v1`） | 否（默认 `/api/v1` + dev proxy） |
| `VITE_APP_URL` | ConnectKit 展示用 `appUrl` | 否 |
| `VITE_WALLETCONNECT_PROJECT_ID` | WalletConnect Cloud project id | 是（如使用 WalletConnect） |
| `VITE_SEPOLIA_RPC_URL` / `VITE_RPC_URL` | Sepolia RPC endpoint | 否（兜底 `https://rpc.sepolia.org`） |
| `VITE_ANVIL_RPC_URL` | 本地 Anvil RPC | 否 |
| `VITE_PRIVY_DEV_STUB` | `1` 关闭 wagmi multicall，便于 Playwright stub | 仅 dev |
| `VITE_COUNTER_CONTRACT_ADDRESS` | Counter 部署地址 | 否 |
| `VITE_NOTEPAD_CONTRACT_ADDRESS` | Notepad 部署地址 | 否 |
| `VITE_COURSE_MARKET_ADDRESS` | CourseMarket 部署地址 | 否 |
| `VITE_YD_TOKEN_ADDRESS` | YDToken 部署地址 | 否 |
| `VITE_CERTIFICATE_NFT_ADDRESS` | CertificateNFT 部署地址 | 否 |
| `VITE_PRICE_ORACLE_ADDRESS` | ChainlinkPriceOracle 部署地址 | 否 |
| `VITE_PRIVY_APP_ID` | Privy App ID | 启用 Privy 登录时 |

`apps/web/.env` 已在 `.gitignore`；`.env.example` 是模板，可以提交。

---

## 12. 本地开发

```bash
# 1) 一次性安装
pnpm install

# 2) 复制 env 模板
cp apps/web/.env.example apps/web/.env

# 3) 启动 dev server（http://localhost:5173；HMR）
pnpm dev
# 等价：pnpm --filter @x-web3/web dev
```

dev server 通过 `vite.config.ts` 的 proxy 把 `/api/*` 转发到本地 API：

```ts
proxy: {
    "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
    },
},
```

钱包调试：手机钱包需连同一局域网；`server.host = true` 让 Vite 暴露 LAN 地址。

### 12.1 完整闭环

```bash
# 启动 Redis + Anvil
pnpm dev:stack

# 数据库迁移
pnpm db:migrate

# 启动 API / Worker（在另一个 shell）
pnpm api:dev
pnpm worker:dev

# 启动 web
pnpm dev
```

详细：[docs/dev/anvil-loop.md](../../docs/dev/anvil-loop.md)。

---

## 13. 测试

```bash
# 单元 / 组件测试（Vitest + jsdom + Testing Library）
pnpm --filter @x-web3/web test
# 或
cd apps/web && pnpm test        # vitest run

# 类型检查（strict + noUnusedLocals + noUnusedParameters）
pnpm --filter @x-web3/web typecheck

# Lint
pnpm --filter @x-web3/web lint

# E2E（Playwright Chromium）
pnpm e2e:web:install         # 安装浏览器
pnpm e2e:web                 # 跑全部 spec
pnpm --filter @x-web3/web e2e:ui   # UI 模式
```

测试分层：

| 层 | 工具 | 覆盖 |
|---|---|---|
| 单元 | Vitest | `derive.ts` / `checkoutUtils.ts` / `playbackRules.ts` 等纯函数 |
| 组件 | Vitest + Testing Library | `App.tsx` 路由 smoke / `CourseEditor.test.tsx` / `CourseCatalog.test.ts` |
| E2E | Playwright | `auth.spec.ts` / `purchase.spec.ts` / `certificate.spec.ts`（共享 `e2e/fixtures/`） |

`vitest.config.ts` 用 `jsdom`；`playwright.config.ts` 配置 Chromium + 持久化
localStorage state，便于多 spec 复用登录态。

### 13.1 E2E 前置

- `pnpm e2e:web:install` 会拉 Chromium；
- dev stub（`VITE_PRIVY_DEV_STUB=1`）让 wagmi multicall 关闭，Playwright
  可拦截单个 JSON-RPC；
- 完整 E2E 需要本地 API + Anvil + Worker 一起跑（参见
  [docs/dev/anvil-loop.md](../../docs/dev/anvil-loop.md)）。

---

## 14. 生产构建与部署

```bash
pnpm --filter @x-web3/web build   # tsc --noEmit && vite build
```

- 目标 `es2022`；
- sourcemap 开启（CloudFront 缓存诊断用）；
- bundle 体积关注：`wagmi + @privy-io + connectkit` 都比较大，按需 tree-shaking
  即可；不要手动 chunk-split 引入复杂度。

部署走 `pnpm deploy:aws`（仓库根脚本）→ 推 S3 → CloudFront 失效。
EC2 的 `deploy-backend-aws.sh` 单独构建并发布 API / Worker。详见
[infra/aws/static-site.yaml](../../infra/aws/static-site.yaml) 与
[docs/DEPLOYMENT.md](../../docs/DEPLOYMENT.md)。

### 14.1 安全边界

- **绝不**把私钥 / RPC key / Session secret 放进 `VITE_*`；
- `deployments.ts` 只能放公开合约地址；
- 任何 secret 走后端 / Cloud Function / KMS。

---

## 15. 跨模块契约速查

| 契约 | 来自 | 落在前端的位置 |
|---|---|---|
| 错误码常量 | `@x-web3/shared/errors` | `api/client.ts`、`features/admin/*` |
| 事件 topic / ABI shape | `@x-web3/shared/events` | （直接走 wagmi ABI；事件 shape 用作日志解析 / 测试断言） |
| 链 registry | `@x-web3/shared/chains` | `src/chains.ts` 同步 |
| CourseMarket ABI | `packages/contracts/out → export:abi` | `src/contracts/courseMarket.abi.ts` |
| `courseKey = sha256(uuid)` | SSOT | `features/checkout/derive.ts` |
| `intentId = uuid 高 128 位` | SSOT | 同上 |
| HTTP 错误信封 | 后端 `httpkit` | `api/client.ts::ApiClientError` |

---

## 16. 进一步阅读

- 全局架构：[docs/ARCHITECTURE.md](../../docs/ARCHITECTURE.md)
- 产品流程：[docs/PRODUCT-FLOWS.md](../../docs/PRODUCT-FLOWS.md)
- ABI 桥：[packages/contracts/script/export-abi.mjs](../../packages/contracts/script/export-abi.mjs)
- 后端同伴：[apps/api](../api/README.md)
- Worker 链子：[apps/worker](../worker/README.md)
- 共享契约：[packages/shared](../shared/README.md)
- 前端规则：[../../.claude/rules/frontend.md](../../.claude/rules/frontend.md)