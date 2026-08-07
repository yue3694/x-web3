// Filled in by hand after each deploy:
//   pnpm contracts:deploy:sepolia          -> counterDeployments
//   pnpm contracts:deploy:notepad:sepolia   -> notepadDeployments
// The deploy script prints the address — paste it into the matching chain
// entry below.
import type {Address} from "viem";

function optionalAddress(value: string | undefined): Address | undefined {
    return value?.match(/^0x[0-9a-fA-F]{40}$/) ? (value as Address) : undefined;
}

export const counterDeployments = {
    sepolia: {
        address: optionalAddress(import.meta.env.VITE_COUNTER_CONTRACT_ADDRESS),
        chainId: 11155111,
    },
} as const;

export const notepadDeployments = {
    sepolia: {
        address: optionalAddress(import.meta.env.VITE_NOTEPAD_CONTRACT_ADDRESS),
        chainId: 11155111,
    },
} as const;
