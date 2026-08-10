/**
 * Checkout 切片共享类型（F03 — design.md §3）。
 *
 * CheckoutButton / CheckoutPanel 共用 CheckoutState 状态机；
 * 业务规则由后端 purchase-intents 流程主导，前端仅负责表达状态。
 */

export type CheckoutState =
    | "idle"
    | "preparing"
    | "signing"
    | "confirming"
    | "done"
    | "failed";

export interface CheckoutContextProps {
    /** 课程唯一 ID（catalog 域，UUID）。 */
    courseId: string;
    /** 课程可读标题（用于 UI 展示）。 */
    courseTitle: string;
    /** YD 标价（bigint 字符串，最小单位 wei）—— 用于价格拆解展示。
     *  实际链上金额以 intent.amount（API 锁定）为准。 */
    priceYD: string;
    /** 链上课程键（bytes32 hex，0x 开头，64 字符）。 */
    courseKey: `0x${string}`;
    /** 收款地址。当前 buyCourse 合约签名不再需要 recipient（金额由
     *  course_prices.amount + intent.amount 锁定），字段保留为可选，
     *  老 caller 不报错。 */
    recipient?: `0x${string}`;
    /** 用户在该 chain 上的 wallet.id（UUID），用于 /purchase-intents 入参。 */
    walletId: string;
    /** 幂等键（必须每次点 Buy 都重新生成；不可随机生成又忽略 → 重发就重复扣费）。 */
    generateIdempotencyKey: () => string;
    /** 成功后回调（拿到广播后的 txHash）。 */
    onSuccess?: (txHash: `0x${string}`) => void;
}

/** 后端 purchase-intents 返回结构，对齐 `packages/shared/openapi/order.yaml#/PurchaseIntentResponse`。 */
export interface PurchaseIntent {
    id: string;
    userId: string;
    walletId: string;
    courseId: string;
    priceId: string;
    courseKey: `0x${string}`;
    priceVersion: number;
    chainId: number;
    tokenAddress: `0x${string}`;
    amount: string;
    marketAddress: `0x${string}`;
    contractAddress?: `0x${string}`;
    idempotencyKey: string;
    status: "created" | "submitted" | "confirming" | "confirmed" | "failed" | "expired" | "reorged";
    expiresAt: string;
    createdAt: string;
    updatedAt: string;
}

/** 后端 orders/{intentId}/transactions ack 返回（成功提交 txHash 后）。 */
export interface OrderTransactionAck {
    orderId: string;
    intentId: string;
    onchainTxHash: `0x${string}`;
    chainId: number;
    status: "pending" | "confirmed" | "failed" | "dead";
}

/** 通用错误（用于 banner）：把 wagmi / API 异常统一收敛。 */
export interface CheckoutError {
    code: "wrong-network" | "user-rejected" | "abi-missing" | "api" | "tx-failed" | "unknown";
    message: string;
}
