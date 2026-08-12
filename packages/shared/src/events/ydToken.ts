/**
 * YDToken 事件 schema 单一来源。
 * 字段命名严格匹配 Solidity event；topic0 由 keccak256(eventSignature) 计算。
 *
 * 注：ERC-20 Transfer/Approval、AccessControl RoleGranted/Revoked、Pausable Paused/Unpaused
 * 是 OZ 标准事件，topic0 与版本无关；自研事件（Minted/CapSet）当前占位，
 * 等合约编译后由 CI 脚本（scripts/compute-topics.mjs）回填。
 */

import type { Address } from './primitives';

export interface TransferEvent {
  from: Address;
  to: Address;
  value: bigint;
}

export interface ApprovalEvent {
  owner: Address;
  spender: Address;
  value: bigint;
}

export interface RoleGrantedEvent {
  role: `0x${string}`;
  account: Address;
  sender: Address;
}

export interface RoleRevokedEvent {
  role: `0x${string}`;
  account: Address;
  sender: Address;
}

export interface PausedEvent {
  account: Address;
}

export interface UnpausedEvent {
  account: Address;
}

/** YDToken 自定义事件（待合约定义后由 CI 回填 topic0） */
export interface MintedEvent {
  to: Address;
  amount: bigint;
}

export interface CapSetEvent {
  newCap: bigint;
}

export const YD_TOKEN_EVENT_SIGNATURES = {
  // OZ 通用 topic0（keccak256 签名）
  Transfer: '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef',
  Approval: '0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925',
  RoleGranted: '0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d',
  RoleRevoked: '0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b',
  Paused: '0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258',
  Unpaused: '0x5db9ee0a495bf2e6ff9c91a7834c1ba4fdd244a5e8aa4e537bd38aeae4b073aa',
  // 自研事件：等合约定义后由 scripts/compute-topics.mjs 回填
  Minted: '0x' as `0x${string}`,
  CapSet: '0x' as `0x${string}`,
} as const;