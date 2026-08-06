---
description: packages/contracts/** 的 Solidity / Foundry 安全规范与部署流程
globs: packages/contracts/**
---

# Smart contracts

## 必备安全检查

- [ ] 重入：CEI（checks-effects-interactions）顺序；`external` 函数涉及
      ETH / 状态修改时使用 `ReentrancyGuard`（来自 OpenZeppelin）。
- [ ] 整数：默认 checked；`unchecked` 必须有 `// unchecked-safety: <理由>` 注释。
- [ ] 权限：管理员用 `Ownable`（单一）或 `AccessControl`（多角色）。
- [ ] 输入校验：地址非零、数值上下界、数组长度显式检查。
- [ ] 自定义错误优先于 `require(string, ...)`；NatSpec 在 `public/external` 上必填。
- [ ] 事件：所有状态变更必须 emit 事件；事件参数用 `indexed` 标注过滤字段。
- [ ] ETH 收发：使用 `Call`/`DelegateCall` 谨慎；优先 pull over push（提款模式）。
- [ ] Oracle / 外部合约：返回值必校验；不可信返回值不进入核心会计。
- [ ] Gas：循环尽量避免 unbounded；mapping 优于 array 用于查找。

## 测试要求

- 单元测试覆盖每个分支；失败路径显式 `vm.expectRevert`。
- Fuzz 默认 256 runs，核心数值函数 ≥ 1000。
- `forge coverage` ≥ **90%** 行覆盖。
- 部署脚本必须能在 `anvil` 上端到端跑通。

## 部署流程（Sepolia）

1. 确认 `.env` 已填三项：`SEPOLIA_RPC_URL` / `ETHERSCAN_API_KEY` / `DEPLOYER_PRIVATE_KEY`。
2. `forge build` — 0 warning（CI profile 下 `-D warnings` 会拒绝）。
3. `forge test` — 全绿。
4. `forge script script/DeployXxx.s.sol --rpc-url $SEPOLIA_RPC_URL --broadcast --verify -vvvv`。
5. 把 console 输出的地址登记到 `apps/web/src/contracts/deployments.ts`。
6. 在 Etherscan 手动复核 Verified 状态；保留 `broadcast/<chain>/run-latest.json`。

## 工具约定

- `remappings.txt` 已配置 OpenZeppelin 与 forge-std；**不要**写相对路径。
- 工具函数放 `packages/contracts/src/utils/`，标 `internal`；
  外部公共函数一律走 library。
- `forge fmt` 在提交前必须过；CI 用 `forge fmt --check`。
- Solidity 版本固定 `0.8.24`（`foundry.toml`），不要随意降级。

## 升级性

- 默认 **不可升级**；升级需求显式引入 `UUPSUpgradeable`，加 24h timelock。
- 任何 storage 变更必须考虑 storage layout；用 `@custom:storage-location`
  NatSpec 标注。

## 已知反模式（直接拒绝）

- `tx.origin` 用于鉴权。
- `block.timestamp` 作为随机源或精确截止（容忍 ~15s 漂移）。
- `selfdestruct`（除 EIP-6780 允许场景）。
- 未声明返回值的低层 call 不检查返回值。
- 在构造函数里 `transfer` ETH。