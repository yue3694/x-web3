// Filled in by hand after each deploy:
//   pnpm contracts:deploy:sepolia          -> counterDeployments
//   pnpm contracts:deploy:notepad:sepolia   -> notepadDeployments
// The deploy script prints the address — paste it into the matching chain
// entry below.
import type {Address} from "viem";

export const counterDeployments = {
    sepolia: {
        address: undefined as Address | undefined,
        chainId: 11155111,
    },
} as const;

export const notepadDeployments = {
    sepolia: {
        address: undefined as Address | undefined,
        chainId: 11155111,
    },
} as const;