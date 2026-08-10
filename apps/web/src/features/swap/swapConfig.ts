/**
 * Swap 切片常量与地址解析（F05）。
 *
 * ⚠️ 地址来源说明：
 * 任务书要求从 `@/contracts/deployments.ts` 读取 `ydTokenDeployments.sepolia`、
 * `swapRouterDeployments.sepolia`、`quoterDeployments.sepolia`。
 * 目前 deployments.ts 里**只有** ydTokenDeployments —— router / quoter / USDC
 * 三个槽位尚未登记，而本次改动不允许修改 deployments.ts。
 * 因此这里用与 deployments.ts 完全一致的 optionalAddress 校验方式，从 env 读取
 * 剩余三个地址作为临时槽位。
 *
 * TODO(spin): deployments.ts 补上 swapRouterDeployments / quoterDeployments /
 * usdcDeployments 后，删掉本文件的 envAddress 分支，直接 import。 — 2026-08-10
 */

import {sepolia} from "wagmi/chains";
import type {Address} from "viem";

import {ydTokenDeployments} from "@/contracts/deployments";

import type {TokenMeta, TokenSymbol} from "./swapTypes";

/** 与 deployments.ts::optionalAddress 同款校验：非法/缺失一律 undefined。 */
function envAddress(value: unknown): Address | undefined {
  return typeof value === "string" && /^0x[0-9a-fA-F]{40}$/.test(value)
    ? (value as Address)
    : undefined;
}

export const TARGET_CHAIN_ID = sepolia.id;

/**
 * Uniswap V3 手续费档位（百万分之一）。MVP 固定 0.3% 池。
 * 换池子（0.05% / 1%）需要同时改报价与写入，别只改一处。
 */
export const DEFAULT_FEE_TIER = 3000;

/** 不限价：交由 amountOutMinimum 兜底，避免 sqrtPriceLimitX96 误设成硬约束。 */
export const NO_PRICE_LIMIT = 0n;

/** 滑点预设（基点）。默认 0.5%。 */
export const SLIPPAGE_PRESETS_BPS = [50, 100, 200] as const;
export const DEFAULT_SLIPPAGE_BPS = 50;
/** 超过 5% 黄色警告。 */
export const SLIPPAGE_WARN_BPS = 500;
/** 超过 10% 直接拒绝（红色 + 禁用提交）。 */
export const SLIPPAGE_MAX_BPS = 1000;

/** 价格影响色带（百分比）：green <1、yellow 1–5、red 5–10、blocked ≥10。 */
export const PRICE_IMPACT_WARN_PCT = 1;
export const PRICE_IMPACT_DANGER_PCT = 5;
/** F05-T06：价格影响 ≥ 10% 必须禁用提交。 */
export const PRICE_IMPACT_BLOCK_PCT = 10;

/** 默认截止时间（分钟）。 */
export const DEFAULT_DEADLINE_MINS = 20;
export const MIN_DEADLINE_MINS = 1;
export const MAX_DEADLINE_MINS = 60;

/**
 * 探针报价金额（用于估算「无滑点现价」以推导价格影响）。
 * 取 tokenIn 的 1/1000 个单位：足够小到贴近边际价，又不会因为过小被池子舍入成 0。
 */
export function probeAmount(decimals: number): bigint {
  const unit = 10n ** BigInt(decimals);
  const probe = unit / 1000n;
  return probe > 0n ? probe : 1n;
}

/**
 * MVP 代币表：只有 YD ↔ USDC。
 * YD 地址来自 deployments.ts；USDC 是 Sepolia 上的测试网 USDC，走 env 注入。
 */
export const TOKENS: Readonly<Record<TokenSymbol, TokenMeta>> = {
  YD: {
    symbol: "YD",
    name: "YiDeng Token",
    decimals: 18,
    address: ydTokenDeployments.sepolia.address,
  },
  USDC: {
    symbol: "USDC",
    name: "USD Coin",
    decimals: 6,
    address: envAddress(import.meta.env.VITE_USDC_ADDRESS),
  },
};

/** 可选代币列表，喂给下拉框。 */
export const TOKEN_SYMBOLS: readonly TokenSymbol[] = ["YD", "USDC"];

/** Uniswap V3 SwapRouter（Sepolia）。未配置 → UI 降级为「未部署」。 */
export const swapRouterAddress = envAddress(import.meta.env.VITE_SWAP_ROUTER_ADDRESS);

/** Uniswap V3 QuoterV2（Sepolia）。未配置 → 无法报价。 */
export const quoterAddress = envAddress(import.meta.env.VITE_QUOTER_ADDRESS);
