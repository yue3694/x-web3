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
