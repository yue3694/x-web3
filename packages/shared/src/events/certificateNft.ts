/**
 * CertificateNFT 事件 schema 单一来源。
 * 字段命名严格匹配 Solidity event；topic0 由 keccak256(eventSignature) 计算。
 *
 * CertificateNFT 仅暴露一个自研事件 CertificateMinted(address,uint256,string)，
 * 其余（Transfer/Approval）是 ERC-721 标准事件，复用 OZ topic0。
 */

import type { Address } from './primitives';

export interface TransferEvent {
  from: Address;
  to: Address;
  tokenId: bigint;
}

export interface ApprovalEvent {
  owner: Address;
  approved: Address;
  tokenId: bigint;
}

export interface ApprovalForAllEvent {
  owner: Address;
  operator: Address;
  approved: boolean;
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

/** CertificateNFT 自研事件 */
export interface CertificateMintedEvent {
  to: Address;
  certificateId: bigint;
  uri: string;
}

export const CERTIFICATE_NFT_EVENT_SIGNATURES = {
  // ERC-721 通用 topic0
  Transfer: '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef',
  Approval: '0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925',
  ApprovalForAll: '0x17307eab39ab6107e8899845ad3d59bd9653f200f220920489ca2b5937696c31',
  // AccessControl
  RoleGranted: '0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d',
  RoleRevoked: '0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b',
  // 自研
  CertificateMinted: '0xde5d6fd45e32fffb472bcbaeedf6a2e3944633c9701eec7d99adfa07ad6104c1',
} as const;