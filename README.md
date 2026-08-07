# x-web3

Web3 monorepo for an EVM dApp — **Vite + React + wagmi** frontend talking to
**Foundry** smart contracts deployed on the **Sepolia** testnet.

```
x-web3/
├── apps/
│   └── web/              # @x-web3/web — Vite + React + wagmi
└── packages/
    └── contracts/        # @x-web3/contracts — Foundry (forge / cast / anvil)
```

## Prerequisites

| Tool | Min version | Install |
|------|-------------|---------|
| Node | 20.x | <https://nodejs.org/> |
| pnpm | 10.x | `npm i -g pnpm` |
| Foundry | latest | `curl -L https://foundry.paradigm.xyz \| bash && foundryup` |

You'll also need:

- **Sepolia ETH** for the deployer account (faucets:
  <https://sepoliafaucet.com/>, <https://cloud.google.com/application/3/sep>)
- An **RPC URL** (Alchemy / Infura / public)
- An **Etherscan API key** for verification
  (<https://etherscan.io/apis>)

## First-time setup

```bash
# 1. install JS deps (this also pulls OpenZeppelin Contracts + forge-std
#    for the contracts package — no `forge install` step required)
pnpm install

# 2. configure env (root + each package's .env.example is your template)
cp .env.example .env
cp packages/contracts/.env.example packages/contracts/.env
cp apps/web/.env.example apps/web/.env
# …fill in SEPOLIA_RPC_URL, ETHERSCAN_API_KEY, DEPLOYER_PRIVATE_KEY
```

That's it — no `forge install`, no `git submodule update`. Foundry reads
`packages/contracts/node_modules/` via `remappings.txt`.

## Daily workflow

```bash
pnpm contracts:test                       # forge test
pnpm contracts:compile                     # forge build
pnpm contracts:deploy:sepolia              # deploy + auto-verify on Etherscan
pnpm --filter @x-web3/contracts export:abi # copy ABI → apps/web/src/contracts
pnpm dev                                   # vite dev server on :5173
```

After the first deploy, paste the printed address into
`apps/web/src/contracts/deployments.ts → counterDeployments.sepolia.address`.

## Core flow — contract to UI

整条流水线是单向的：合约源码 → Sepolia 链上 → 前端 wagmi 调用。

```text
.sol
  │  forge build
  ▼
out/Foo.sol/Foo.json          ← ABI 在这里（forge build artifact）
  │
  ├──forge script --broadcast ──► Sepolia 合约 (Etherscan auto-verified)
  │                                  │
  │                                  ▼
  │              env: VITE_<NAME>_CONTRACT_ADDRESS
  │                                  │
  ▼                                  ▼
export-abi.mjs                  deployments.ts
  │                                  │
  └────────► apps/web/src/contracts/ ◄┘
                   │
                   ▼
            React 组件 (wagmi hooks)
```

5 步：

1. **写合约 + 测试** — [packages/contracts/src/](packages/contracts/src/)（`*.sol`）与
   [test/](packages/contracts/test/)（`*.t.sol`）；通过 `remappings.txt` 引用
   OpenZeppelin + forge-std，无需 `forge install`。
2. **编译 + 部署脚本** — `pnpm contracts:compile` 生成 `out/<Name>.sol/<Name>.json`；
   [script/DeployXxx.s.sol](packages/contracts/script/DeployCounter.s.sol)
   继承 `Script`，用 `vm.startBroadcast(pk)` 真实广播 `new Foo(...)`。
3. **部署到 Sepolia** — `packages/contracts/.env` 填三项：
   `SEPOLIA_RPC_URL` / `ETHERSCAN_API_KEY` / `DEPLOYER_PRIVATE_KEY`；
   `pnpm contracts:deploy:sepolia` 等价于
   `forge script ... --broadcast --verify -vvvv`，自动 Etherscan 验证。
   凭据留档在 `broadcast/<chain>/run-latest.json`。
4. **ABI 与地址灌到前端** — `pnpm contracts:export:abi` 把 forge 产物转成
   `as const` 的 TS 模块，输出到 [apps/web/src/contracts/*.abi.ts](apps/web/src/contracts/)；
   部署地址**手工** paste 到 [apps/web/src/contracts/deployments.ts](apps/web/src/contracts/deployments.ts)
   （当前实现从 `VITE_<NAME>_CONTRACT_ADDRESS` env 读）。
5. **前端调用** — 组件按固定顺序用 wagmi v2 hooks：
   `useAccount → useReadContract → useWriteContract → useWaitForTransactionReceipt → useEffect(refetch on isConfirmed)`。
   ABI 与地址**只**从 `src/contracts/*` 拿，绝不在组件里硬编码。

> 深度内容（CEI 安全姿态、swap-and-pop 语义、`run-latest.json` 留档意义、新增合约 cookbook）
> 见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)。已上链地址与 Etherscan 链接
> 见 [docs/DEPLOYMENTS.md](docs/DEPLOYMENTS.md)。

## Network

- **Sepolia** — `chainId 11155111` — <https://sepolia.etherscan.io/>

## Tooling reference

- pnpm workspaces — `pnpm-workspace.yaml`
- TypeScript 5.6, ES2022, strict mode
- Vite 5 + React 18, wagmi v2 + viem v2, `@tanstack/react-query` v5
- Foundry (forge / cast / anvil), Solidity 0.8.24, OpenZeppelin contracts

## Architecture & feature docs

- **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** — full system walkthrough:
  the contract ↔ frontend ABI bridge, Sepolia deploy pipeline, Notepad
  storage invariants (swap-and-pop, monotonic ids), local dev workflow,
  and a cookbook for adding new contracts.
- **[docs/TOOLCHAIN.md](docs/TOOLCHAIN.md)** — contract toolchain reference:
  ABI 产生与作用、编译与发布流程、Foundry / Hardhat / Ganache / Truffle 对比、
  现代合约主流 7 阶段开发流程。
- **[docs/DEPLOYMENTS.md](docs/DEPLOYMENTS.md)** — registry of deployed
  contract addresses (Sepolia Notepad, etc.) with tx hashes, Etherscan
  links, and redeploy instructions.
- **[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)** — repeatable AWS S3 + CloudFront
  deployment, custom-domain certificate setup, and Cloudflare DNS automation.

## Security

- **Never** commit a real private key or seed phrase. `.env` is git-ignored.
- The deployer key should be a *throwaway* hot wallet, not a treasury.
- Treat the frontend `VITE_*` vars as public; only put public values there.
- After every contract change, run `forge test` and `forge coverage` before
  redeploying.

See [`.claude/rules/`](./.claude/rules) for the full house rules.
