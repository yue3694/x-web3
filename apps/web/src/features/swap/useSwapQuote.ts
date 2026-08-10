/**
 * useSwapQuote — 封装 QuoterV2 报价（含价格影响推导）。
 *
 * 发两个 useReadContract：
 *   1. 真实报价：quoteExactInputSingle(amountIn)
 *   2. 探针报价：quoteExactInputSingle(1/1000 token) —— 近似「无滑点现价」
 * 两者相除得到 priceImpactPct（数学见 swapUtils.computePriceImpactPct）。
 *
 * 探针失败不会让整笔报价失败：priceImpactPct 退化为 null，
 * 提交仍受 amountOutMinimum 保护。
 */

import {useMemo} from "react";
import {useReadContract} from "wagmi";

import {quoterAbi} from "@/contracts/quoter.abi";

import {
  DEFAULT_FEE_TIER,
  NO_PRICE_LIMIT,
  TARGET_CHAIN_ID,
  TOKENS,
  probeAmount,
  quoterAddress,
} from "./swapConfig";
import {applySlippage, computePriceImpactPct} from "./swapUtils";
import type {QuoteResult, TokenSymbol} from "./swapTypes";

interface UseSwapQuoteArgs {
  tokenIn: TokenSymbol;
  tokenOut: TokenSymbol;
  /** 已解析的输入金额；null / 0 表示不发请求。 */
  amountIn: bigint | null;
  slippageBps: number;
}

interface UseSwapQuoteResult {
  quote: QuoteResult | null;
  isLoading: boolean;
  error: Error | null;
  refetch: () => void;
}

/** QuoterV2 返回四元组：[amountOut, sqrtPriceX96After, ticksCrossed, gasEstimate]。 */
type QuoteTuple = readonly [bigint, bigint, number, bigint];

export function useSwapQuote({
  tokenIn,
  tokenOut,
  amountIn,
  slippageBps,
}: UseSwapQuoteArgs): UseSwapQuoteResult {
  const inMeta = TOKENS[tokenIn];
  const outMeta = TOKENS[tokenOut];
  const ready = Boolean(
    quoterAddress && inMeta.address && outMeta.address && amountIn && amountIn > 0n,
  );

  const baseParams = {
    tokenIn: inMeta.address ?? "0x",
    tokenOut: outMeta.address ?? "0x",
    fee: DEFAULT_FEE_TIER,
    sqrtPriceLimitX96: NO_PRICE_LIMIT,
  } as const;

  const main = useReadContract({
    address: quoterAddress,
    abi: quoterAbi,
    functionName: "quoteExactInputSingle",
    args: [{...baseParams, amountIn: amountIn ?? 0n}],
    chainId: TARGET_CHAIN_ID,
    query: {enabled: ready},
  });

  const probeIn = probeAmount(inMeta.decimals);
  const probe = useReadContract({
    address: quoterAddress,
    abi: quoterAbi,
    functionName: "quoteExactInputSingle",
    args: [{...baseParams, amountIn: probeIn}],
    chainId: TARGET_CHAIN_ID,
    query: {enabled: ready},
  });

  const mainData = main.data as QuoteTuple | undefined;
  const probeData = probe.data as QuoteTuple | undefined;

  const quote = useMemo<QuoteResult | null>(() => {
    if (!mainData || !amountIn || amountIn <= 0n) return null;
    const [amountOut, , , gasEstimateWei] = mainData;
    if (amountOut <= 0n) return null;

    const priceImpactPct = probeData
      ? computePriceImpactPct({amountIn, amountOut, probeIn, probeOut: probeData[0]})
      : null;

    return {
      amountOut,
      minAmountOut: applySlippage(amountOut, slippageBps),
      priceImpactPct,
      route: {hops: [tokenIn, tokenOut], feeTier: DEFAULT_FEE_TIER},
      gasEstimateWei,
    };
  }, [mainData, probeData, amountIn, probeIn, slippageBps, tokenIn, tokenOut]);

  return {
    quote,
    // 探针的 loading 不计入：它只影响 badge，不该让整个卡片转圈。
    isLoading: ready && main.isLoading,
    error: main.error ?? null,
    refetch: () => {
      void main.refetch();
      void probe.refetch();
    },
  };
}
