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

合约子模块（首次或更新时）：
```bash
cd packages/contracts
forge install OpenZeppelin/openzeppelin-contracts --no-commit
forge install foundry-rs/forge-std --no-commit
```

## 目录结构

```
.
├── apps/
│   └── web/
│       ├── src/
│       │   ├── components/   # ConnectButton, CounterCard
│       │   ├── contracts/    # counter.abi.ts, deployments.ts  ← 由 export:abi 生成
│       │   ├── App.tsx · main.tsx · wagmi.ts · styles.css
│       └── vite.config.ts · tsconfig.json
├── packages/
│   └── contracts/
│       ├── src/              # *.sol 生产合约
│       ├── test/             # *.t.sol
│       ├── script/           # Deploy*.s.sol + export-abi.mjs
│       ├── lib/              # git submodules (forge-std, openzeppelin-contracts)
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