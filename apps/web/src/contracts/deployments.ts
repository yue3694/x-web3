// Filled in by hand after the first deploy:
//   pnpm contracts:deploy:sepolia
// The script logs the address — paste it into `sepolia.address` below.
import type {Address} from "viem";

export const counterDeployments = {
    sepolia: {
        address: undefined as Address | undefined,
        chainId: 11155111,
    },
} as const;