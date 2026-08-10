/**
 * SwapAmountFields — 「You pay / You receive」两条腿 + 翻转按钮。
 *
 * 纯受控展示：金额解析、报价都在 SwapCard 完成，这里只渲染。
 * MVP 只有 YD ↔ USDC 一对，所以选任一 token 都等价于翻转方向。
 */

import {TOKEN_SYMBOLS} from "./swapConfig";
import {formatTokenAmount} from "./swapUtils";
import type {QuoteResult, TokenMeta, TokenSymbol} from "./swapTypes";

interface SwapAmountFieldsProps {
  tokenIn: TokenSymbol;
  outMeta: TokenMeta;
  amountText: string;
  /** 解析后的金额；null 且输入非空 = 非法输入，输入框标红。 */
  amountIn: bigint | null;
  quote: QuoteResult | null;
  quoting: boolean;
  disabled: boolean;
  onAmountChange: (raw: string) => void;
  onFlip: () => void;
}

export function SwapAmountFields({
  tokenIn,
  outMeta,
  amountText,
  amountIn,
  quote,
  quoting,
  disabled,
  onAmountChange,
  onFlip,
}: SwapAmountFieldsProps) {
  return (
    <>
      <div className="swap-card__leg">
        <label className="swap-card__field">
          <span>You pay</span>
          <input
            type="text"
            inputMode="decimal"
            placeholder="0.0"
            value={amountText}
            onChange={(e) => onAmountChange(e.target.value)}
            disabled={disabled}
            aria-invalid={amountText !== "" && amountIn === null}
          />
        </label>
        <select
          className="swap-card__token"
          value={tokenIn}
          onChange={(e) => {
            if (e.target.value !== tokenIn) onFlip();
          }}
          disabled={disabled}
          aria-label="Token to sell"
        >
          {TOKEN_SYMBOLS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>

      <button
        type="button"
        className="btn btn--ghost swap-card__flip"
        onClick={onFlip}
        disabled={disabled}
        aria-label={`Flip direction to ${outMeta.symbol} → ${tokenIn}`}
      >
        ↓ Flip
      </button>

      <div className="swap-card__leg">
        <label className="swap-card__field">
          <span>You receive (estimated)</span>
          <output className="swap-card__output">
            {quoting ? "…" : quote ? formatTokenAmount(quote.amountOut, outMeta.decimals) : "—"}
          </output>
        </label>
        <span className="swap-card__token swap-card__token--static">{outMeta.symbol}</span>
      </div>
    </>
  );
}
