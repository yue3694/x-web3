/**
 * SwapSummary — 报价明细 + 成交后结算对比。
 *
 * 两块内容都是「只读展示」，从 SwapCard 拆出以保持主组件专注于状态机。
 * 成交后同时展示 minReceived 与 actualReceived，让用户核对滑点是否被吃掉。
 */

import {PriceImpactBadge} from "./PriceImpactBadge";
import {formatTokenAmount} from "./swapUtils";
import type {QuoteResult, SwapSettlement, TokenMeta} from "./swapTypes";

interface SwapSummaryProps {
  quote: QuoteResult | null;
  quoting: boolean;
  outMeta: TokenMeta;
  settlement: SwapSettlement | null;
}

export function SwapSummary({quote, quoting, outMeta, settlement}: SwapSummaryProps) {
  return (
    <>
      <div className="swap-card__meta">
        <PriceImpactBadge pct={quote?.priceImpactPct ?? null} loading={quoting} />
        {quote ? (
          <>
            <span className="swap-card__min">
              Min received {formatTokenAmount(quote.minAmountOut, outMeta.decimals)} {outMeta.symbol}
            </span>
            <span className="swap-card__route">
              Route {quote.route.hops.join(" → ")} · {quote.route.feeTier / 10_000}%
            </span>
            <span className="swap-card__gas">Gas est. {quote.gasEstimateWei.toString()}</span>
          </>
        ) : null}
      </div>

      {settlement ? (
        <dl className="swap-card__settlement">
          <div>
            <dt>Min received</dt>
            <dd>
              {formatTokenAmount(settlement.minReceived, outMeta.decimals)} {outMeta.symbol}
            </dd>
          </div>
          <div>
            <dt>Actual received</dt>
            <dd>
              {settlement.actualReceived === null
                ? "—"
                : `${formatTokenAmount(settlement.actualReceived, outMeta.decimals)} ${outMeta.symbol}`}
            </dd>
          </div>
        </dl>
      ) : null}
    </>
  );
}
