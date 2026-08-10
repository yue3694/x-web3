/**
 * Checkout 切片工具函数。
 *
 * 把状态机相关的纯函数从 CheckoutButton 抽出，便于：
 *   - 单元测试（不依赖 React）
 *   - CheckoutPanel 等其他组件复用
 */

import {ApiClientError} from "@/api/client";

import type {CheckoutError, CheckoutState} from "./checkoutTypes";

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
    const lower = msg.toLowerCase();
    return USER_REJECTED_PATTERNS.some((p) => lower.includes(p));
}

export function normalizeError(err: unknown): CheckoutError {
    if (err instanceof ApiClientError) {
        return {code: "api", message: err.message};
    }
    if (isUserRejected(err)) {
        return {code: "user-rejected", message: "User rejected"};
    }
    if (err instanceof Error && err.message.toLowerCase().includes("abi not yet exported")) {
        return {code: "abi-missing", message: err.message};
    }
    return {code: "unknown", message: err instanceof Error ? err.message : "Unknown error"};
}

export function buttonLabel(opts: {
    isConnected: boolean;
    onWrongChain: boolean;
    isSwitching: boolean;
    state: CheckoutState;
    priceYD: string;
    receiptLoading: boolean;
}): string {
    if (!opts.isConnected) return "Connect wallet to buy";
    if (opts.onWrongChain) return opts.isSwitching ? "Switching…" : "Switch to Sepolia";
    switch (opts.state) {
        case "idle":
            return `Buy for ${opts.priceYD} YD`;
        case "preparing":
            return "Preparing intent…";
        case "signing":
            return "Sign in wallet…";
        case "confirming":
            return opts.receiptLoading ? "Confirming on-chain…" : "Awaiting receipt…";
        case "done":
            return "Purchase complete";
        case "failed":
            return "Retry purchase";
    }
}
