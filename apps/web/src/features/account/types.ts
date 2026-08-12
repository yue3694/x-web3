/**
 * Account 模块的共享类型与纯函数。
 *
 * 类型字段对齐 `packages/shared/openapi/learning.yaml`（live F04 spec）：
 *   - `EnrollmentItem`   → /me/enrollments
 *   - `CompletionRecord` → /courses/{id}/complete（证书摘要从此派生）
 *
 * 实际数据由 `api/learning.ts` 持有；这里只 re-export + 工具函数。
 */

export type {
    EnrollmentItem,
    EnrollmentListResponse,
    CompletionRecord,
} from "@/api/learning";

import type {CompletionRecord} from "@/api/learning";

/** 链浏览器基础 URL（与 MyOrders.types.ts 对齐）。 */
const ETHERSCAN_BASE: Record<number, string> = {
    1: "https://etherscan.io",
    11155111: "https://sepolia.etherscan.io",
};

/** 课程证书的 etherscan 链接（confirmed 状态才有意义）。 */
export function certificateTxUrl(chainId: number, hash: string): string {
    const base = ETHERSCAN_BASE[chainId] ?? ETHERSCAN_BASE[1];
    return `${base}/tx/${hash}`;
}

/** 钱包地址脱敏：`0x1234…5678`。 */
export function truncateAddress(address: string): string {
    if (address.length < 10) return address;
    return `${address.slice(0, 6)}…${address.slice(-4)}`;
}

/** 完课状态徽章文案（与 `learning.yaml#/CompletionRecord.status` 对齐）。 */
export const COMPLETION_STATUS_LABEL: Record<CompletionRecord["status"], string> = {
    pending: "待处理",
    minting: "铸造中",
    confirmed: "已确认",
    failed: "失败",
    dead: "已失效",
};
