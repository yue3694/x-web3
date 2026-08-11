/**
 * Swap 切片共享类型（F05 — YD ↔ USDC via Uniswap V3）。
 *
 * SwapCard / SlippageControl / PriceImpactBadge 共用这里的类型；
 * 纯类型文件，不含运行时逻辑（常量见 swapConfig.ts，纯函数见 swapUtils.ts）。
 */

import type {Address} from "viem";

/**
 * Swap 状态机。
 *
 *   idle → quoting → signing → confirming → done
 *                                        ↘ failed
 *
 * - idle       — 初始态 / 用户还没输入金额。
 * - quoting    — QuoterV2 报价请求在飞（useReadContract loading）。
 * - signing    — 钱包弹窗已弹出，等用户签名。
 * - confirming — 交易已广播，useWaitForTransactionReceipt 轮询中。
 * - done       — receipt 落账，已算出 actualReceived。
 * - failed     — 终态失败，用户可重试（重试回到 idle）。
 */
export type SwapState = "idle" | "quoting" | "checking" | "approving" | "signing" | "confirming" | "done" | "failed";

/** MVP 只支持 YD ↔ USDC 这一对，token 选择器据此渲染。 */
export type TokenSymbol = "YD" | "USDC";

/** 单个代币的前端元数据（地址来自 deployments / env，可能未部署 → undefined）。 */
export interface TokenMeta {
  symbol: TokenSymbol;
  /** 展示用全名。 */
  name: string;
  /** ERC-20 decimals：YD = 18，USDC = 6。parseUnits/formatUnits 依赖它。 */
  decimals: number;
  /** 目标测试链上的合约地址；未配置时 undefined，UI 需降级为「未部署」。 */
  address: Address | undefined;
}

/** 路由描述。MVP 是单跳，保留数组形状方便后续接多跳。 */
export interface SwapRoute {
  /** 按顺序排列的代币符号，如 ["YD", "USDC"]。 */
  hops: readonly TokenSymbol[];
  /** V3 手续费档位（百万分之一）：3000 = 0.3%。 */
  feeTier: number;
}

/** QuoterV2 报价结果 + 前端派生字段。 */
export interface QuoteResult {
  /** 预期输出（tokenOut 最小单位）。 */
  amountOut: bigint;
  /** 扣掉滑点后的最低可接受输出，直接作为 amountOutMinimum 传给 router。 */
  minAmountOut: bigint;
  /**
   * 价格影响百分比（1.5 表示 1.5%）。
   * null = 无法计算（探针报价缺失），UI 显示「—」且不阻断提交，
   * 因为 amountOutMinimum 仍然提供了兜底保护。
   */
  priceImpactPct: number | null;
  /** 本次报价走的路由。 */
  route: SwapRoute;
  /** QuoterV2 返回的 gas 估算（wei 计价的 gas 单位数）。 */
  gasEstimateWei: bigint;
}

/** 一次 swap 的完整入参，由 SwapCard 的表单状态收敛而来。 */
export interface SwapInput {
  tokenIn: TokenSymbol;
  tokenOut: TokenSymbol;
  /** 输入金额（tokenIn 最小单位，parseUnits 解析后的结果）。 */
  amountIn: bigint;
  /** 滑点容忍度（基点）：50 = 0.5%。 */
  slippageBps: number;
  /** 截止时间（分钟），转成 unix 秒后作为 deadline 传链上。 */
  deadlineMins: number;
}

/** 归一化后的错误，用于 banner 展示。 */
export interface SwapError {
  code:
    | "wrong-network"
    | "not-connected"
    | "user-rejected"
    | "abi-missing"
    | "not-deployed"
    | "no-quote"
    | "price-impact"
    | "tx-failed"
    | "unknown";
  message: string;
}

/** swap 完成后的结算摘要（done 态展示）。 */
export interface SwapSettlement {
  txHash: `0x${string}`;
  /** 提交时承诺的最低到账（= amountOutMinimum）。 */
  minReceived: bigint;
  /** 从 receipt 的 Transfer 日志里解析出的实际到账；解析失败为 null。 */
  actualReceived: bigint | null;
}

/** 滑点 / 价格影响的色带分级，驱动 SlippageControl 与 PriceImpactBadge 配色。 */
export type RiskTone = "ok" | "warn" | "danger" | "blocked";
