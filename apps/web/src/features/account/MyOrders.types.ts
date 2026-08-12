/**
 * MyOrders 共享类型与纯函数。
 *
 * 把 OrderRecord / OrderListResponse / 工具函数从 MyOrders 抽出，
 * 便于 OrderRow 组件单独引用并保持单文件 ≤ 200 行。
 *
 * 类型字段对齐 `packages/shared/openapi/order.yaml#/OrderResponse`。
 * 后端 `GET /me/orders` 当前只支持 `limit`（不含 `page`/`status` 过滤）；
 * 本前端组件使用 status 客户端过滤 + 累加分页，详见 MyOrders.tsx。
 */

export type OrderStatus = "all" | "pending" | "confirmed" | "failed" | "dead";

// OrderResponse 对齐 spec；courseTitle / priceYD 不在 server 返回里，
// 由前端调用 GET /courses/{id} 异步补齐；找不到时显示占位。
export interface OrderRecord {
    id: string;
    intentId: string;
    userId: string;
    courseId: string;
    courseTitle?: string;
    priceYD?: string;
    status: Exclude<OrderStatus, "all">;
    chainId: number;
    onchainTxHash?: `0x${string}`;
    blockNumber?: number | null;
    confirmedAt?: string | null;
    enrollmentId?: string | null;
    failureCode?: string | null;
    createdAt: string;
    updatedAt: string;
}

export interface OrderListResponse {
    items: OrderRecord[];
}

const ETHERSCAN_BASE: Record<number, string> = {
    1: "https://etherscan.io",
    11155111: "https://sepolia.etherscan.io",
};

export function etherscanUrl(chainId: number, hash: string): string {
    const base = ETHERSCAN_BASE[chainId] ?? ETHERSCAN_BASE[1];
    return `${base}/tx/${hash}`;
}

export function formatDate(iso: string): string {
    const date = new Date(iso);
    if (Number.isNaN(date.valueOf())) return iso;
    return new Intl.DateTimeFormat("en-US", {
        year: "numeric",
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
    }).format(date);
}
