# @x-web3/contracts

Foundry workspace for the smart contracts. Targets **Sepolia** by default
(`https://sepolia.etherscan.io/`).

> **New here?** Read [`TOUR.md`](./TOUR.md) — a guided walkthrough that
> tells you the right reading order for every file in this directory and
> what to focus on in each.

## Prerequisites

```bash
# Foundry (forge / cast / anvil)
curl -L https://foundry.paradigm.xyz | bash
foundryup
```

You also need:

- A funded Sepolia account (≥ 0.05 ETH for gas).
  Faucets: <https://sepoliafaucet.com/>, <https://cloud.google.com/application/3/sep>
- An RPC URL — Alchemy / Infura / public.
- An Etherscan API key from <https://etherscan.io/apis>.

Copy `.env.example` → `.env` at the repo **root** (Foundry loads it from
the working directory).

## Install dependencies

OpenZeppelin & forge-std live in `lib/` as git submodules:

```bash
cd packages/contracts
forge install OpenZeppelin/openzeppelin-contracts --no-commit
forge install foundry-rs/forge-std --no-commit
```

## Compile & test

```bash
pnpm contracts:compile
pnpm contracts:test
```

## Deploy to Sepolia

```bash
pnpm contracts:deploy:sepolia
```

The script (`script/DeployCounter.s.sol`) auto-verifies on Etherscan via
`--verify` using `ETHERSCAN_API_KEY`.

## Wire ABI into the frontend

```bash
pnpm contracts:export:abi
```

This copies `Counter.abi` to `apps/web/src/contracts/counter.abi.ts`
for `wagmi`'s `useReadContract` / `useWriteContract` to consume.

## Layout

```
packages/contracts/
├── src/             # Production contracts
├── test/            # Forge tests (*.t.sol)
├── script/          # Forge deploy scripts + export-abi helper
├── lib/             # Git submodules (forge-std, openzeppelin-contracts)
├── foundry.toml
└── remappings.txt
```