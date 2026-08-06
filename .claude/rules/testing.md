---
description: 合约与前端的测试约定
---

# Testing

## Foundry (合约)

- 测试文件与生产代码并列：`packages/contracts/test/Foo.t.sol`。
- 测试函数命名：`test_<场景>_<期望>`，如 `test_Increment_RevertsOnPause`。
- 失败用例必须显式断言：`vm.expectRevert(Counter.Underflow.selector)`；
  不要 `try/catch` 吞错。
- Fuzz 默认开启：`forge test` 默认 256 runs，覆盖核心数值路径。
- 跑覆盖：`pnpm contracts:test` 之前先 `forge coverage` 看下行覆盖；
  生产合约目标 ≥ **90%**。
- 部署脚本必须能在 `anvil`（本地）上跑通：`forge script ... --rpc-url http://127.0.0.1:8545`。
- 提交前：`forge fmt --check` + `forge test` 必须双绿。

## Frontend (待补 vitest)

当前前端仅依赖合约端测试；后续接入 `vitest` 时遵循：

- 组件测试文件名：`*.test.tsx`，与组件并列。
- Wagmi hooks 测试用 `@wagmi/core/test` 的 mock provider；不要 mock viem。
- E2E 走 `playwright` 单独仓库，本仓不引入。

## CI 期望

- PR 触发：`forge fmt --check && forge test && tsc --noEmit` 全绿。