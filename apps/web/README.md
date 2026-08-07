# @x-web3/web

Vite + React + wagmi frontend targeting Sepolia.

## Dev

```bash
pnpm install
pnpm dev          # http://localhost:5173
```

The first time, populate the contract binding:

```bash
# 1. Install Foundry libs and compile contracts
cd packages/contracts
forge install OpenZeppelin/openzeppelin-contracts --no-commit
forge install foundry-rs/forge-std --no-commit
pnpm compile

# 2. Copy ABI into the frontend
pnpm --filter @x-web3/contracts export:abi

# 3. Deploy to Sepolia (writes the address into the broadcast log)
pnpm --filter @x-web3/contracts deploy:sepolia

# 4. Paste the deployed address into
#    apps/web/src/contracts/deployments.ts → `counterDeployments.sepolia.address`
```

After that, `pnpm dev` should let you connect MetaMask and click +/−.

## Network

- Chain: **Sepolia** (`chainId: 11155111`)
- Explorer: <https://sepolia.etherscan.io/>

## Stack

- [Vite 5](https://vite.dev/)
- [React 18](https://react.dev/)
- [wagmi v2](https://wagmi.sh/) + [viem v2](https://viem.sh/)
- [@tanstack/react-query v5](https://tanstack.com/query/latest)
- TypeScript strict

No wallet UI kit is bundled — `ConnectButton` uses wagmi's built-in
`injected` connector. Plug in
[RainbowKit](https://www.rainbowkit.com/) or
[ConnectKit](https://docs.family.co/connectkit) when you need a polished
modal — both are one provider away.