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
        return {code: "user-rejected", message: "已取消"};
    }
    if (err instanceof Error && err.message.toLowerCase().includes("abi not yet exported")) {
        return {code: "abi-missing", message: err.message};
    }
    return {code: "unknown", message: err instanceof Error ? err.message : "未知错误"};
}

export function buttonLabel(opts: {
    isConnected: boolean;
    onWrongChain: boolean;
    isSwitching: boolean;
    state: CheckoutState;
    priceYD: string;
    receiptLoading: boolean;
}): string {
    if (!opts.isConnected) return "连接钱包以购买";
    if (opts.onWrongChain) return opts.isSwitching ? "切换中…" : "切换网络";
    switch (opts.state) {
        case "idle":
            return `用 ${opts.priceYD} YD 购买`;
        case "preparing":
            return "正在准备支付意图…";
        case "checking":
            return "正在检查 YD 余额…";
        case "approving":
            return "请在钱包中授权 YD…";
        case "signing":
            return "请在钱包中签名…";
        case "confirming":
            return opts.receiptLoading ? "等待链上确认…" : "等待回执中…";
        case "done":
            return "购买完成";
        case "failed":
            return "重试购买";
    }
}
