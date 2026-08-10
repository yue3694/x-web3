/**
 * Swap 切片纯数学（金额解析 / 滑点 / 截止时间 / 价格影响分级）。
 *
 * 全部与 React 解耦，方便单测；SwapCard 只负责把状态串起来。
 * 错误归一与回执解析在 swapErrors.ts。
 */

import {formatUnits, parseUnits} from "viem";

import {
  PRICE_IMPACT_BLOCK_PCT,
  PRICE_IMPACT_DANGER_PCT,
  PRICE_IMPACT_WARN_PCT,
  SLIPPAGE_MAX_BPS,
  SLIPPAGE_WARN_BPS,
} from "./swapConfig";
import type {RiskTone} from "./swapTypes";

const BPS_DENOMINATOR = 10_000n;

/**
 * 把用户输入的十进制字符串解析成最小单位 bigint。
 * 非法输入（空串 / 多个小数点 / 超出精度）返回 null，由调用方决定提示。
 */
export function parseAmountInput(raw: string, decimals: number): bigint | null {
  const trimmed = raw.trim();
  if (!trimmed || !/^\d*\.?\d*$/.test(trimmed) || trimmed === ".") return null;
  try {
    const parsed = parseUnits(trimmed, decimals);
    return parsed > 0n ? parsed : null;
  } catch {
    // parseUnits 在小数位超过 decimals 时抛错 —— 视为无效输入而非崩溃。
    return null;
  }
}

/** 展示用格式化：截断到 maxFractionDigits 位，避免 18 位小数刷屏。 */
export function formatTokenAmount(value: bigint, decimals: number, maxFractionDigits = 6): string {
  const full = formatUnits(value, decimals);
  const dot = full.indexOf(".");
  if (dot === -1) return full;
  const truncated = full.slice(0, dot + 1 + maxFractionDigits).replace(/\.?0+$/, "");
  return truncated === "" ? "0" : truncated;
}

/**
 * 滑点数学：minAmountOut = amountOut × (10000 − slippageBps) ÷ 10000。
 *
 * 全程 bigint 整除，天然向下取整 —— 对用户是保守（更安全）的方向。
 * slippageBps 会被夹到 [0, SLIPPAGE_MAX_BPS]，防止越界输入穿透到链上。
 */
export function applySlippage(amountOut: bigint, slippageBps: number): bigint {
  const bps = BigInt(clampSlippageBps(slippageBps));
  return (amountOut * (BPS_DENOMINATOR - bps)) / BPS_DENOMINATOR;
}

export function clampSlippageBps(slippageBps: number): number {
  if (!Number.isFinite(slippageBps)) return 0;
  return Math.min(Math.max(Math.round(slippageBps), 0), SLIPPAGE_MAX_BPS);
}

/**
 * 截止时间数学：deadline = 当前 unix 秒 + deadlineMins × 60。
 *
 * 用 Date.now() 而不是 block.timestamp：这是**客户端**约束，链上只做
 * `require(block.timestamp <= deadline)`，几秒漂移无害。
 */
export function deadlineFromNow(deadlineMins: number, nowMs: number = Date.now()): bigint {
  const mins = Number.isFinite(deadlineMins) && deadlineMins > 0 ? deadlineMins : 1;
  return BigInt(Math.floor(nowMs / 1000) + Math.floor(mins * 60));
}

/**
 * 价格影响：拿一笔极小额「探针报价」当作无滑点现价，再比对真实报价。
 *
 *   ideal  = probeOut × amountIn ÷ probeIn      // 按边际价线性外推的理想输出
 *   impact = (ideal − amountOut) ÷ ideal × 100
 *
 * 任一入参 ≤ 0 → 返回 null（UI 显示「—」，不阻断提交，因为 amountOutMinimum
 * 仍然提供兜底保护）。amountOut ≥ ideal（正滑点 / 舍入）时归零。
 */
export function computePriceImpactPct(args: {
  amountIn: bigint;
  amountOut: bigint;
  probeIn: bigint;
  probeOut: bigint;
}): number | null {
  const {amountIn, amountOut, probeIn, probeOut} = args;
  if (amountIn <= 0n || amountOut <= 0n || probeIn <= 0n || probeOut <= 0n) return null;
  const ideal = (probeOut * amountIn) / probeIn;
  if (ideal <= 0n) return null;
  if (amountOut >= ideal) return 0;
  // 先放大 1e4 再转 Number：impact 上限 100，不会丢精度也不会溢出。
  const impactBps = ((ideal - amountOut) * BPS_DENOMINATOR) / ideal;
  return Number(impactBps) / 100;
}

/** 价格影响色带：green <1% / yellow 1–5% / red 5–10% / blocked ≥10%。 */
export function priceImpactTone(pct: number | null): RiskTone {
  if (pct === null) return "ok";
  if (pct >= PRICE_IMPACT_BLOCK_PCT) return "blocked";
  if (pct >= PRICE_IMPACT_DANGER_PCT) return "danger";
  if (pct >= PRICE_IMPACT_WARN_PCT) return "warn";
  return "ok";
}

/** F05-T06：价格影响 ≥ 10% 必须禁用提交。 */
export function isPriceImpactBlocked(pct: number | null): boolean {
  return pct !== null && pct >= PRICE_IMPACT_BLOCK_PCT;
}

/** 滑点色带：>10% blocked（禁用提交），>5% warn，其余 ok。 */
export function slippageTone(slippageBps: number): RiskTone {
  if (slippageBps > SLIPPAGE_MAX_BPS) return "blocked";
  if (slippageBps > SLIPPAGE_WARN_BPS) return "warn";
  return "ok";
}

/** 滑点是否越过硬上限（10%）。越过则 SwapCard 禁用提交。 */
export function isSlippageRejected(slippageBps: number): boolean {
  return slippageBps > SLIPPAGE_MAX_BPS;
}

/** bps → 百分比字符串，如 50 → "0.5"。 */
export function bpsToPercentText(bps: number): string {
  return String(Math.round(bps) / 100);
}

/** 百分比输入 → bps；非法输入返回 null。 */
export function percentTextToBps(raw: string): number | null {
  const trimmed = raw.trim();
  if (!trimmed || !/^\d*\.?\d*$/.test(trimmed) || trimmed === ".") return null;
  const pct = Number(trimmed);
  if (!Number.isFinite(pct) || pct < 0) return null;
  return Math.round(pct * 100);
}
