/// <reference types="vite/client" />

interface ImportMetaEnv {
    readonly VITE_SEPOLIA_RPC_URL?: string;
    readonly VITE_WALLETCONNECT_PROJECT_ID?: string;
    readonly VITE_DEPLOYER_ADDRESS?: string;
}

interface ImportMeta {
    readonly env: ImportMetaEnv;
}