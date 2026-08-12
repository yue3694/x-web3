/// <reference types="vite/client" />

interface ImportMetaEnv {
    readonly VITE_RPC_URL?: string;
    readonly VITE_SEPOLIA_RPC_URL?: string;
    readonly VITE_WALLETCONNECT_PROJECT_ID?: string;
    readonly VITE_DEPLOYER_ADDRESS?: string;
    readonly VITE_PRICE_ORACLE_ADDRESS?: string;
    readonly VITE_SEPOLIA_YD_SALE_ADDRESS?: string;
}

interface ImportMeta {
    readonly env: ImportMetaEnv;
}
