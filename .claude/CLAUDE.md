# x-web3

Web3 monorepo: **Vite + React + wagmi** frontend talks to **Foundry** smart
contracts deployed to **Sepolia** (`https://sepolia.etherscan.io/`).

## 技术栈

- 包管理: pnpm 10.x workspaces
- 前端 (`apps/web`): Vite 5 · React 18 · wagmi v2 · viem v2 · @tanstack/react-query v5 · TypeScript 5.6 (strict)
- 合约 (`packages/contracts`): Foundry (forge/cast/anvil) · Solidity 0.8.24 · OpenZeppelin Contracts

## 常用命令

```bash
pnpm install                                    # 一次性安装所有 workspace
pnpm dev                                        # apps/web — http://localhost:5173
pnpm build                                      # apps/web — tsc + vite build
pnpm typecheck                                  # apps/web — tsc --noEmit
pnpm contracts:compile                          # forge build
pnpm contracts:test                             # forge test
pnpm contracts:deploy:sepolia                   # deploy + 自动验证 Etherscan
pnpm --filter @x-web3/contracts export:abi      # 把 ABI 拷到 apps/web/src/contracts
```

合约依赖（OpenZeppelin + forge-std）走 npm，不需要 `forge install`：

```bash
pnpm install     # 自动拉 @openzeppelin/contracts + forge-std 到 packages/contracts/node_modules
```

`remappings.txt` 把 forge 指向 `node_modules/`——不用 git submodule，没有 `.gitmodules`。

## 目录结构

```
.
├── apps/
│   └── web/
│       ├── src/
│       │   ├── components/   # ConnectButton, Notepad
│       │   ├── contracts/    # counter.abi.ts, notepad.abi.ts, deployments.ts  ← 由 export:abi 生成
│       │   ├── App.tsx · main.tsx · wagmi.ts · styles.css
│       └── vite.config.ts · tsconfig.json
├── packages/
│   └── contracts/
│       ├── src/              # *.sol 生产合约
│       ├── test/             # *.t.sol
│       ├── script/           # Deploy*.s.sol + export-abi.mjs
│       ├── node_modules/     # OZ + forge-std (pnpm 管理，gitignore)
│       └── foundry.toml · remappings.txt
├── package.json · pnpm-workspace.yaml
├── .env.example · .gitignore · README.md
└── .claude/                  # ← 本目录: 规则 & 项目门面
```

## Sepolia 部署前置

`.env` (git-ignored) 必须填：

| 变量 | 来源 |
|------|------|
| `SEPOLIA_RPC_URL` | Alchemy / Infura / 公共 RPC |
| `ETHERSCAN_API_KEY` | <https://etherscan.io/apis> |
| `DEPLOYER_PRIVATE_KEY` | 部署者热钱包 hex key（**专用账户**，非主仓） |

部署账户必须有 Sepolia ETH（≥ 0.05 ETH）：<https://sepoliafaucet.com/>

## 规则

@rules/coding-style.md
@rules/testing.md
@rules/security.md
@rules/git-workflow.md
@rules/frontend.md
@rules/smart-contract.md