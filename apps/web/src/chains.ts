import {anvil, sepolia} from "wagmi/chains";

const configuredChainId = Number(import.meta.env.VITE_TARGET_CHAIN_ID ?? sepolia.id);

if (configuredChainId !== anvil.id && configuredChainId !== sepolia.id) {
    throw new Error(
        `Unsupported VITE_TARGET_CHAIN_ID=${configuredChainId}; expected ${anvil.id} (Anvil) or ${sepolia.id} (Sepolia)`,
    );
}

export const targetChain = configuredChainId === anvil.id ? anvil : sepolia;
export const TARGET_CHAIN_ID = targetChain.id;
export const TARGET_CHAIN_NAME = targetChain.name;

export const targetRpcUrl =
    import.meta.env.VITE_RPC_URL ??
    (TARGET_CHAIN_ID === anvil.id
        ? import.meta.env.VITE_ANVIL_RPC_URL ?? "http://127.0.0.1:8545"
        : import.meta.env.VITE_SEPOLIA_RPC_URL ?? "https://rpc.sepolia.org");
