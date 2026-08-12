import {sepolia} from "wagmi/chains";

export const targetChain = sepolia;
export const TARGET_CHAIN_ID = targetChain.id;
export const TARGET_CHAIN_NAME = targetChain.name;

export const targetRpcUrl = import.meta.env.VITE_SEPOLIA_RPC_URL
    ?? import.meta.env.VITE_RPC_URL
    ?? "https://rpc.sepolia.org";
