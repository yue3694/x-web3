import {createConfig, http} from "wagmi";
import {sepolia} from "wagmi/chains";
import {getDefaultConfig} from "connectkit";

const SEPOLIA_RPC_URL =
    import.meta.env.VITE_SEPOLIA_RPC_URL ?? "https://rpc.sepolia.org";

// WalletConnect Cloud project ID — required only for the WalletConnect connector.
// Get one at https://cloud.walletconnect.com. Fallback to empty string keeps
// MetaMask/Rabby (injected) usable; WalletConnect will throw a friendlier
// warning if left blank.
const WALLETCONNECT_PROJECT_ID =
    import.meta.env.VITE_WALLETCONNECT_PROJECT_ID ?? "";

// ConnectKit's `getDefaultConfig` returns the args expected by `createConfig`,
// with sensible defaults for connectors/transports injected. We then forward
// our own RPC + Sepolia chain so we talk directly to the testnet.
export const wagmiConfig = createConfig(
    getDefaultConfig({
        appName: "x-web3",
        appDescription: "On-chain Notepad",
        appUrl: "https://x-web3.local",
        walletConnectProjectId: WALLETCONNECT_PROJECT_ID,
        chains: [sepolia],
        transports: {
            [sepolia.id]: http(SEPOLIA_RPC_URL),
        },
        ssr: false,
    }),
);

declare module "wagmi" {
    interface Register {
        config: typeof wagmiConfig;
    }
}
