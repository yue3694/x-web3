import {createConfig, http} from "wagmi";
import {sepolia} from "wagmi/chains";
import {injected} from "wagmi/connectors";

const SEPOLIA_RPC_URL =
    import.meta.env.VITE_SEPOLIA_RPC_URL ?? "https://rpc.sepolia.org";

// Injected (MetaMask/Rabby/etc.) covers the dev happy path.
// Add `walletConnect`, `coinbaseWallet`, etc. here as you grow.
export const wagmiConfig = createConfig({
    chains: [sepolia],
    connectors: [injected({shimDisconnect: true})],
    transports: {
        [sepolia.id]: http(SEPOLIA_RPC_URL),
    },
    ssr: false,
});

declare module "wagmi" {
    interface Register {
        config: typeof wagmiConfig;
    }
}