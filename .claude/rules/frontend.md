---
description: apps/web/** 的组件、wagmi 用法、状态管理约定
globs: apps/web/**
---

# Frontend (apps/web)

## 组件

- 函数组件，**具名导出**；只有路由文件用 default export。
- 一个组件只做一件事；超过 200 行就拆。
- 受控组件优先；非受控只用于纯展示。
- 列表 key 必须是稳定 ID，禁止 `index`。

## wagmi v2

- **永远用 v2 hooks**：`useAccount` / `useConnect` / `useDisconnect` /
  `useReadContract` / `useWriteContract` / `useWaitForTransactionReceipt`。
- 写交易前必须 `useAccount()` 确认 `isConnected` 与 `chainId === 目标链`，
  否则给用户错误提示（参见 `CounterCard.tsx`）。
- ABI 与地址来自 `src/contracts/*`，**不要**在组件里写硬编码。
- 每次写交易成功后用 `useEffect + refetch()` 刷新读；
  不要在 `useEffect` 里 `writeContract`。
- 链切换：通过 `useSwitchChain()`；不要在 dApp 里手动调 `wallet_switchEthereumChain`。

## 状态管理

- 服务端状态 → `@tanstack/react-query`（已挂载 `QueryClientProvider`）。
- 客户端临时态 → `useState`；跨组件共享 → `useContext` 或 zustand（按需）。
- 不引入 Redux / MobX。

## 路由

- 当前无路由库；如需路由统一用 `react-router@6`（data router 模式）。
- 路由懒加载用 `React.lazy + Suspense`。

## 样式

- 单文件 `src/styles.css`，CSS 变量驱动主题；不引入 Tailwind / styled-components。
- 颜色变量已在 `:root` 与 `@media (prefers-color-scheme: dark)` 中定义，
  新增色板请走变量。

## RPC / 网络

- `wagmi.ts` 中通过 `VITE_SEPOLIA_RPC_URL` 注入；fallback 到公共 RPC。
- 生产前换 Alchemy / Infura，并加上 `fallback` 配置。

## TypeScript

- `strict: true`，禁止 `any`；如需临时绕过用 `unknown` + 类型守卫。
- 路径别名 `@/` → `src/`（已在 `vite.config.ts` 与 `tsconfig.json` 中）。