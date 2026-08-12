/**
 * SwapSubmitButton — 提交按钮 + 错链切换。
 *
 * 从 SwapCard 拆出来的原因：按钮文案是状态机 × 校验结果的笛卡尔积，
 * 内联在卡片里会淹没主流程。这里只负责「显示什么、能不能点」。
 */

import {TARGET_CHAIN_ID} from "./swapConfig";
import {TARGET_CHAIN_NAME} from "@/chains";
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
  /** 兑换路由 / 代币地址在目标链上缺失。 */
  notDeployed?: boolean;
  disabled: boolean;
  onSwap: () => void;
  onSwitchChain: () => void;
}

function label(p: SwapSubmitButtonProps): string {
  if (!p.isConnected) return "连接钱包以兑换";
  switch (p.state) {
    case "quoting":
      return "正在获取报价…";
    case "checking":
      return "正在检查余额…";
    case "approving":
      return `请在钱包中授权 ${p.tokenIn}…`;
    case "signing":
      return "请在钱包中签名…";
    case "confirming":
      return "等待链上确认…";
    case "done":
      return "再次兑换";
    case "failed":
      return "重试兑换";
    case "idle":
      // 阻断原因优先于默认文案：用户要知道为什么按钮是灰的。
      if (p.notDeployed) return "兑换尚未配置";
      if (p.impactBlocked) return "价格影响过高";
      if (p.slippageBlocked) return "滑点过高";
      return `用 ${p.tokenIn} 兑换 ${p.tokenOut}`;
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
        aria-label={`切换到 chain ${TARGET_CHAIN_ID}`}
      >
        {props.isSwitching ? "切换中…" : `切换到 ${TARGET_CHAIN_NAME}`}
      </button>
    );
  }

  const busy = ["checking", "approving", "signing", "confirming"].includes(props.state);

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
