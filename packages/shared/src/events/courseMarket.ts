/**
 * CourseMarket 事件 schema 单一来源。
 * 后端 Worker 用此 schema 做 abi-decode；前端用它做 log 解析与测试断言。
 *
 * 字段命名严格匹配 Solidity event，避免在 worker 与合约之间漂移。
 */

import type { Address, Hash, Hex } from './primitives';

export interface CourseConfiguredEvent {
  courseKey: `0x${string}`;
  token: Address;
  amount: bigint;
  priceVersion: bigint;
}

export interface CoursePurchasedEvent {
  courseKey: `0x${string}`;
  buyer: Address;
  token: Address;
  amount: bigint;
  intentId: `0x${string}`; // bytes16 hex (32 hex chars)
  priceVersion: bigint;
}

/** Worker 入库前的最小校验结构。 */
export interface DecodedLog<T> {
  chainId: number;
  blockNumber: bigint;
  blockHash: Hash;
  txHash: Hash;
  logIndex: number;
  address: Address; // 触发合约地址（= market）
  event: T;
  rawTopics: readonly Hex[];
  rawData: Hex;
}

export const COURSE_MARKET_EVENT_SIGNATURES = {
  CourseConfigured: '0x7c4bd32c23ea1943334ebe7040a4294f81f2b76a6c27bfc63245e86971ff9264',
  CoursePurchased: '0xee2c004361a941cef00dd638a722b034a58392d57a99eab2617793b17a6005ad',
} as const;