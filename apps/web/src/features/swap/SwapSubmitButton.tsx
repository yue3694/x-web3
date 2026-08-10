/**
 * SwapSubmitButton — 提交按钮 + 错链切换。
 *
 * 从 SwapCard 拆出来的原因：按钮文案是状态机 × 校验结果的笛卡尔积，
 * 内联在卡片里会淹没主流程。这里只负责「显示什么、能不能点」。
 */

import {TARGET_CHAIN_ID} from "./swapConfig";
import type {SwapState, TokenSymbol} from "./swapTypes";

interface SwapSubmitButtonProps {
  state: SwapState;
  tokenIn: TokenSymbol;
  tokenOut: TokenSymbol;
  isConnected: boolean;
  onWrongChain: boolean;
  isSwitching: boolean;
  /** 价格影响 ≥ 10%（F05-T06）。 */
  impactBlocked: boolean;
  /** 滑点 > 10%。 */
  slippageBlocked: boolean;
  disabled: boolean;
  onSwap: () => void;
  onSwitchChain: () => void;
}

function label(p: SwapSubmitButtonProps): string {
  if (!p.isConnected) return "Connect wallet to swap";
  switch (p.state) {
    case "quoting":
      return "Fetching quote…";
    case "signing":
      return "Sign in wallet…";
    case "confirming":
      return "Confirming on-chain…";
    case "done":
      return "Swap again";
    case "failed":
      return "Retry swap";
    case "idle":
      // 阻断原因优先于默认文案：用户要知道为什么按钮是灰的。
      if (p.impactBlocked) return "Price impact too high";
      if (p.slippageBlocked) return "Slippage too high";
      return `Swap ${p.tokenIn} for ${p.tokenOut}`;
  }
}

export function SwapSubmitButton(props: SwapSubmitButtonProps) {
  if (props.onWrongChain) {
    return (
      <button
        type="button"
        className="btn btn--primary"
        onClick={props.onSwitchChain}
        disabled={props.isSwitching}
        aria-label={`Switch network to chain ${TARGET_CHAIN_ID}`}
      >
        {props.isSwitching ? "Switching…" : "Switch to Sepolia"}
      </button>
    );
  }

  const busy = props.state === "signing" || props.state === "confirming";

  return (
    <button
      type="button"
      className="btn btn--primary swap-card__submit"
      onClick={props.onSwap}
      disabled={props.disabled}
      data-state={props.state}
      aria-busy={busy}
    >
      {label(props)}
    </button>
  );
}
