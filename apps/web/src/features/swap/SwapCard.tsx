/**
 * SwapCard — YD ↔ USDC 兑换主界面（F05）。
 *
 * 状态机：idle → quoting → signing → confirming → done | failed
 *   - quoting                        来自 useSwapQuote（读链路）
 *   - signing/confirming/done/failed 来自 useSwapExecute（写链路）
 *
 * 本组件只做编排：合并两条链路的状态、跑提交前校验（连接 / 链 / 滑点 ≤10% /
 * 价格影响 <10%，见 F05-T06）、把表单值喂给两个 hook。完整流程见 README。
 *
 * 布局：左右两列（移动端单列堆叠）
 *   左  ─ 主表单 + 错误 + 提交（视觉重点）
 *   右  ─ 滑点 / 截止 / 报价 / 路径 / Gas / 成交对比
 *
 * 注意：绝不在 useEffect 里发交易；只有点击按钮才触发。
 */

import {useEffect, useMemo, useState} from "react";
import {useAccount, useChainId, useSwitchChain} from "wagmi";

import {quoterAbi} from "@/contracts/quoter.abi";
import {swapRouterAbi} from "@/contracts/swap.abi";
import {TARGET_CHAIN_NAME} from "@/chains";

import {SlippageControl} from "./SlippageControl";
import {SwapAmountFields} from "./SwapAmountFields";
import {SwapSubmitButton} from "./SwapSubmitButton";
import {SwapSummary} from "./SwapSummary";
import {useSwapExecute} from "./useSwapExecute";
import {useSwapQuote} from "./useSwapQuote";
import {
  DEFAULT_DEADLINE_MINS, DEFAULT_FEE_TIER, DEFAULT_SLIPPAGE_BPS, MAX_DEADLINE_MINS,
  MIN_DEADLINE_MINS, TARGET_CHAIN_ID, TOKENS, swapRouterAddress,
} from "./swapConfig";
import {isPriceImpactBlocked, isSlippageRejected, parseAmountInput} from "./swapUtils";
import type {SwapSettlement, SwapState, TokenSymbol} from "./swapTypes";

interface SwapCardProps {
  /** 初始输入代币，默认 YD。 */
  defaultTokenIn?: TokenSymbol;
  /** 成交后回调（回执已落账）。 */
  onSuccess?: (settlement: SwapSettlement) => void;
}

export function SwapCard({defaultTokenIn = "YD", onSuccess}: SwapCardProps) {
  const {address, isConnected} = useAccount();
  const chainId = useChainId();
  const {switchChain, isPending: isSwitching} = useSwitchChain();

  const [tokenIn, setTokenIn] = useState<TokenSymbol>(defaultTokenIn);
  const [amountText, setAmountText] = useState("");
  const [slippageBps, setSlippageBps] = useState(DEFAULT_SLIPPAGE_BPS);
  const [deadlineMins, setDeadlineMins] = useState(DEFAULT_DEADLINE_MINS);

  // 与 CheckoutButton 一致的守卫：ABI 未导出时直接炸，而不是静默发错交易。
  if (!swapRouterAbi.length || !quoterAbi.length) {
    throw new Error("ABI not yet exported");
  }

  // MVP 只有一对，tokenOut 恒为另一侧。
  const tokenOut: TokenSymbol = tokenIn === "YD" ? "USDC" : "YD";
  const inMeta = TOKENS[tokenIn];
  const outMeta = TOKENS[tokenOut];

  const amountIn = useMemo(
    () => parseAmountInput(amountText, inMeta.decimals),
    [amountText, inMeta.decimals],
  );

  const {quote, isLoading: quoting, error: quoteError, refetch} = useSwapQuote({
    tokenIn,
    tokenOut,
    amountIn,
    slippageBps,
  });

  const execute = useSwapExecute(onSuccess);

  // 成交后刷新报价（不要在 effect 里发交易，只刷读）。
  useEffect(() => {
    if (execute.state === "done") refetch();
  }, [execute.state, refetch]);

  const onWrongChain = isConnected && chainId !== TARGET_CHAIN_ID;
  const impactBlocked = isPriceImpactBlocked(quote?.priceImpactPct ?? null);
  const slippageBlocked = isSlippageRejected(slippageBps);
  const notDeployed = !swapRouterAddress || !inMeta.address || !outMeta.address;
  const busy = ["checking", "approving", "signing", "confirming"].includes(execute.state);
  // 写链路优先：签名/确认中不能被 quoting 覆盖。
  const state: SwapState = execute.state === "idle" && quoting ? "quoting" : execute.state;

  const onFlip = () => {
    setTokenIn(tokenOut);
    setAmountText("");
    execute.reset();
  };

  const onSwap = () => {
    execute.setError(null);
    if (!isConnected || !address) return execute.setError("请先连接钱包。");
    if (onWrongChain) return execute.setError(`请切换到 ${TARGET_CHAIN_NAME} 后继续。`);
    if (notDeployed || !inMeta.address || !outMeta.address) {
      return execute.setError(`${TARGET_CHAIN_NAME} 上尚未配置兑换合约。`);
    }
    if (!amountIn) return execute.setError("请输入大于零的金额。");
    if (!quote) return execute.setError("暂无可用报价。");
    if (slippageBlocked) return execute.setError("滑点超过 10% 已被拒绝。");
    if (impactBlocked) return execute.setError("价格影响达到或超过 10%，兑换已拦截。");

    void execute.swap({
      tokenIn: inMeta.address,
      tokenOut: outMeta.address,
      recipient: address,
      amountIn,
      minAmountOut: quote.minAmountOut,
      deadlineMins,
    });
  };

  // 把「配置缺失」从错误里抽出来单独渲染：不是交易失败，而是功能状态。
  // 当用户在错链时，给出切链 CTA；其他情况只展示一行说明即可。
  const runningErrors = [
    quoteError ? `报价失败：${quoteError.message}` : null,
    execute.error,
  ].filter((n): n is string => n !== null);

  return (
    <section className="swap-card panel" aria-labelledby="swap-card-title">
      <header className="swap-card__header">
        <div className="swap-card__heading">
          <span className="eyebrow">兑换</span>
          <h2 id="swap-card-title">
            {tokenIn} → {tokenOut}
          </h2>
        </div>
        <p className="swap-card__protocol">
          Uniswap V3 · {DEFAULT_FEE_TIER / 10_000}% 池 · {TARGET_CHAIN_NAME}
        </p>
      </header>

      <div className="swap-card__grid">
        {/* 左列：主表单 + 错误 + 提交（视觉重点） */}
        <div className="swap-card__main">
          <SwapAmountFields
            tokenIn={tokenIn}
            outMeta={outMeta}
            amountText={amountText}
            amountIn={amountIn}
            quote={quote}
            quoting={quoting}
            disabled={busy}
            onAmountChange={setAmountText}
            onFlip={onFlip}
          />

          {notDeployed ? (
            <div className="swap-card__notice" role="status">
              <strong className="swap-card__notice-title">兑换暂不可用</strong>
              <p className="swap-card__notice-body">
                当前网络（{TARGET_CHAIN_NAME}）尚未配置兑换路由或代币地址。请切换到已部署兑换合约的网络，或在 <code>.env</code> 中补齐
                <code> VITE_SWAP_ROUTER_ADDRESS</code> / <code>VITE_QUOTER_ADDRESS</code> / <code>VITE_USDC_ADDRESS</code>。
              </p>
              {chainId !== TARGET_CHAIN_ID ? (
                <button
                  type="button"
                  className="btn btn--primary"
                  onClick={() => switchChain({chainId: TARGET_CHAIN_ID})}
                  disabled={isSwitching}
                >
                  {isSwitching ? "切换中…" : `切换到 ${TARGET_CHAIN_NAME}`}
                </button>
              ) : null}
            </div>
          ) : null}

          {runningErrors.length > 0 ? (
            <div className="swap-card__notices" role="region" aria-label="兑换提示">
              {runningErrors.map((notice) => (
                <p key={notice} className="notice notice--error" role="alert">
                  {notice}
                </p>
              ))}
            </div>
          ) : null}

          <SwapSubmitButton
            state={state}
            tokenIn={tokenIn}
            tokenOut={tokenOut}
            isConnected={isConnected}
            onWrongChain={onWrongChain}
            isSwitching={isSwitching}
            impactBlocked={impactBlocked}
            slippageBlocked={slippageBlocked}
            notDeployed={notDeployed}
            disabled={
              !isConnected ||
              notDeployed ||
              busy ||
              !amountIn ||
              !quote ||
              quoting ||
              slippageBlocked ||
              impactBlocked
            }
            onSwap={onSwap}
            onSwitchChain={() => switchChain({chainId: TARGET_CHAIN_ID})}
          />
        </div>

        {/* 右列：滑点 / 截止 / 报价详情 / 成交对比 */}
        <aside className="swap-card__aside" aria-label="兑换设置与报价">
          <SlippageControl valueBps={slippageBps} onChange={setSlippageBps} disabled={busy} />

          <label className="swap-card__deadline">
            <span>交易截止时间（分钟）</span>
            <input
              type="number"
              min={MIN_DEADLINE_MINS}
              max={MAX_DEADLINE_MINS}
              value={deadlineMins}
              onChange={(e) => setDeadlineMins(Number(e.target.value) || DEFAULT_DEADLINE_MINS)}
              disabled={busy}
            />
          </label>

          <SwapSummary
            quote={quote}
            quoting={quoting}
            outMeta={outMeta}
            settlement={execute.settlement}
          />
        </aside>
      </div>
    </section>
  );
}
