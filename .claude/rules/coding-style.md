---
description: 跨包命名 / 缩进 / import / 注释规范
---

# Coding style

## 通用

- 行宽 ≤ **100** 列（合约）；前端 ≤ **120** 列（`prettier` 默认）。
- 缩进：Solidity 4 空格；TypeScript 2 空格；JSON 2 空格。
- 文件名：`PascalCase.sol` / `PascalCase.t.sol` / `PascalCase.s.sol`（合约）；
  `PascalCase.tsx` 组件；`camelCase.ts` 工具；`kebab-case.md` 文档。
- 单一职责：一个合约只做一件事；一个组件 ≤ 200 行（含 JSX）。

## Solidity

- 严格遵循 `forge fmt`（已在 `foundry.toml [fmt]` 配置）。
- 状态变量 `internal > private` 优先；`external` 仅在需要 `this.fn` 时使用。
- 自定义错误必须以错误类型命名：`error Unauthorized();` 而不是 `string` revert。
- 事件名用动词过去式：`Incremented`、`TransferPerformed`。
- NatSpec：`@title` / `@notice` / `@dev` 必须在 `public`/`external` 函数上。
- Pragma：`^0.8.24`，禁止 `^0.8.0` 这种宽松锁。

## TypeScript / React

- `tsconfig` 已开 `strict` + `noUnusedLocals` + `noUnusedParameters`，提交前必须 0 error。
- Imports 用 `import type` 区分类型导入，IDE 自动检查。
- React 函数组件，默认导出仅在路由文件中使用；其它组件**具名导出**。
- Hooks 顺序：`useAccount → useReadContract → useWriteContract → useWaitForTransactionReceipt → useEffect`。
- 不要在 `useEffect` 里发交易；用 `useEffect(() => refetch(), [isConfirmed])` 模式刷新读。

## Imports

- Solidity：`remappings.txt` 已配置 `@openzeppelin/contracts/=...` 与 `forge-std/`；
  不要再写相对路径 `../../lib/...`。
- TS：使用 `@/` 路径别名（已在 `vite.config.ts` 与 `tsconfig.json` 中配置）。

## 注释

- 解释 *why*，不解释 *what*。
- 关键业务逻辑必须中文注释（合约前端统一中文约定）。
- TODO 格式：`// TODO(your-handle): 简述 — yyyy-mm-dd`