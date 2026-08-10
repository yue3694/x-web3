/**
 * Swap 错误归一 + 回执解析。
 *
 * 从 swapUtils 拆出来的原因：这两块都是「跟外部世界打交道」的解析逻辑
 * （钱包异常字符串、链上 log），与滑点/价格影响那类纯数学关注点不同。
 */

import {hexToBigInt} from "viem";
import type {Address} from "viem";

import type {SwapError} from "./swapTypes";

/** ERC-20 `Transfer(address,address,uint256)` 的 topic0。 */
const TRANSFER_TOPIC0 = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef" as const;

const USER_REJECTED_PATTERNS = [
  "user rejected",
  "user canceled",
  "user cancelled",
  "denied transaction",
  "denied transaction signature",
];

export function isUserRejected(err: unknown): boolean {
  if (!err) return false;
  const msg = err instanceof Error ? err.message : String(err);
  return USER_REJECTED_PATTERNS.some((p) => msg.toLowerCase().includes(p));
}

export function normalizeError(err: unknown): SwapError {
  if (isUserRejected(err)) {
    return {code: "user-rejected", message: "User rejected"};
  }
  if (err instanceof Error && err.message.toLowerCase().includes("abi not yet exported")) {
    return {code: "abi-missing", message: err.message};
  }
  return {
    code: "unknown",
    message: err instanceof Error ? err.message : "Unknown error",
  };
}

/** receipt log 的最小结构（便于脱离 viem 类型做单测）。 */
export interface TransferLogLike {
  address: string;
  topics: readonly `0x${string}`[];
  data: `0x${string}`;
}

/**
 * 从 receipt 里解出「实际到账」：取 tokenOut 合约发出的、to == recipient 的
 * 最后一条 Transfer 的 value。
 *
 * 取最后一条而不是求和：多跳路由中间会经过 router，只有最终那条才是用户收款。
 * 解析失败返回 null —— UI 退化成只展示 minReceived，不编造数字。
 */
export function extractReceivedAmount(
  logs: readonly TransferLogLike[],
  tokenOut: Address,
  recipient: Address,
): bigint | null {
  const token = tokenOut.toLowerCase();
  const to = recipient.toLowerCase().slice(2).padStart(64, "0");
  let found: bigint | null = null;
  for (const log of logs) {
    if (log.address.toLowerCase() !== token) continue;
    if (log.topics.length < 3 || log.topics[0]?.toLowerCase() !== TRANSFER_TOPIC0) continue;
    if (log.topics[2]?.toLowerCase().slice(2) !== to) continue;
    try {
      found = hexToBigInt(log.data);
    } catch {
      // 非标准 Transfer（data 不是 32 字节）—— 跳过，不污染结果。
      continue;
    }
  }
  return found;
}
