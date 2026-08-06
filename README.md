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
# 1. install JS deps
pnpm install

# 2. configure env (root + each package's .env.example is your template)
cp .env.example .env
cp packages/contracts/.env.example packages/contracts/.env
cp apps/web/.env.example apps/web/.env
# …fill in SEPOLIA_RPC_URL, ETHERSCAN_API_KEY, DEPLOYER_PRIVATE_KEY

# 3. install Foundry libs into the contracts package
cd packages/contracts
forge install OpenZeppelin/openzeppelin-contracts --no-commit
forge install foundry-rs/forge-std --no-commit
cd -
```

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

## Network

- **Sepolia** — `chainId 11155111` — <https://sepolia.etherscan.io/>

## Tooling reference

- pnpm workspaces — `pnpm-workspace.yaml`
- TypeScript 5.6, ES2022, strict mode
- Vite 5 + React 18, wagmi v2 + viem v2, `@tanstack/react-query` v5
- Foundry (forge / cast / anvil), Solidity 0.8.24, OpenZeppelin contracts

## Security

- **Never** commit a real private key or seed phrase. `.env` is git-ignored.
- The deployer key should be a *throwaway* hot wallet, not a treasury.
- Treat the frontend `VITE_*` vars as public; only put public values there.
- After every contract change, run `forge test` and `forge coverage` before
  redeploying.

See [`.claude/rules/`](./.claude/rules) for the full house rules.