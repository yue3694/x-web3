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

**No `forge install` needed.** OpenZeppelin Contracts and forge-std are
declared as `dependencies` in `package.json` and installed by pnpm into
`node_modules/`. `remappings.txt` points forge at those paths.

```bash
# from the repo root
pnpm install
```

That's the whole setup. To pin different versions, edit the
`dependencies` block in `package.json` (or `pnpm add @openzeppelin/contracts@^5.1`).
To add a new library: `pnpm add <pkg>` + add a remapping line.

> Why not `forge install`? It clones entire git repos (slow on cold
> connections) and writes `.gitmodules`. pnpm goes through npm's CDN,
> pins a version, and shares files across workspaces.

## Compile & test

```bash
pnpm contracts:compile
pnpm contracts:test
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
├── node_modules/    # OZ contracts + forge-std (managed by pnpm)
├── foundry.toml
└── remappings.txt   # points forge at node_modules
```