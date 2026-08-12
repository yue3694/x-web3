/**
 * SwapSummary — 报价明细 + 成交后结算对比（F05）。
 *
 * 两块内容都是「只读展示」，从 SwapCard 拆出以保持主组件专注于状态机。
 * 成交后同时展示 minReceived 与 actualReceived，让用户核对滑点是否被吃掉。
 *
 * 语义：报价明细用 <dl>/<div>/<dt>/<dd> 分行，左侧标签 + 右侧数值；
 * 读屏器从 label 读到 value，符合「先识别控件再看值」的可访问性惯例。
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
            <dl className="swap-card__meta" aria-label="报价明细">
                <div className="meta-row meta-row--accent">
                    <dt>价格影响</dt>
                    <dd>
                        <PriceImpactBadge pct={quote?.priceImpactPct ?? null} loading={quoting} />
                    </dd>
                </div>

                <div className="meta-row">
                    <dt>最少收到</dt>
                    <dd>
                        {quote
                            ? `${formatTokenAmount(quote.minAmountOut, outMeta.decimals)} ${outMeta.symbol}`
                            : "—"}
                    </dd>
                </div>

                <div className="meta-row">
                    <dt>兑换路径</dt>
                    <dd>
                        {quote
                            ? `${quote.route.hops.join(" → ")} · ${quote.route.feeTier / 10_000}%`
                            : "—"}
                    </dd>
                </div>

                <div className="meta-row">
                    <dt>预计 Gas</dt>
                    <dd>{quote ? `${quote.gasEstimateWei.toString()} wei` : "—"}</dd>
                </div>
            </dl>

            {settlement ? (
                <dl className="swap-card__settlement" aria-label="本次成交">
                    <div>
                        <dt>最少收到</dt>
                        <dd>
                            {formatTokenAmount(settlement.minReceived, outMeta.decimals)} {outMeta.symbol}
                        </dd>
                    </div>
                    <div>
                        <dt>实际收到</dt>
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
