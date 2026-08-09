/**
 * 链 registry：单一来源，避免在组件/合约脚本硬编码。
 *
 * 注：MVP 仅 Sepolia。其它 chain 是占位，等 OQ-001 评审后补齐。
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