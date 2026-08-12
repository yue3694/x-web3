/**
 * 链 registry：单一来源，避免在组件/合约脚本硬编码。
 *
 * 测试阶段支持本地 Anvil 与 Sepolia；不包含任何正式链。
 */

export type ChainNamespace = 'eip155';

export interface ChainInfo {
  namespace: ChainNamespace;
  chainId: number;
  name: string;
  shortName: string;
  rpcEnvVar: string; // 用于从 import.meta.env / process.env 读取
  blockExplorer: string;
  nativeToken: 'ETH';
  isTestnet: boolean;
  /** CourseMarket 默认 confirmation depth */
  confirmationDepth: number;
}

export const CHAINS: Record<number, ChainInfo> = {
  31337: {
    namespace: 'eip155',
    chainId: 31337,
    name: 'Anvil',
    shortName: 'anvil',
    rpcEnvVar: 'VITE_ANVIL_RPC_URL',
    blockExplorer: '',
    nativeToken: 'ETH',
    isTestnet: true,
    confirmationDepth: 1,
  },
  11155111: {
    namespace: 'eip155',
    chainId: 11155111,
    name: 'Sepolia',
    shortName: 'sepolia',
    rpcEnvVar: 'VITE_SEPOLIA_RPC_URL',
    blockExplorer: 'https://sepolia.etherscan.io',
    nativeToken: 'ETH',
    isTestnet: true,
    confirmationDepth: 12,
  },
};

export function getChain(chainId: number): ChainInfo {
  const info = CHAINS[chainId];
  if (!info) {
    throw new Error(`Unsupported chainId ${chainId}; add to packages/shared/src/chains/registry.ts`);
  }
  return info;
}
