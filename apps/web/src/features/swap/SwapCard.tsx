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
    if (!isConnected || !address) return execute.setError("Connect a wallet first.");
    if (onWrongChain) return execute.setError(`Switch to ${TARGET_CHAIN_NAME} to continue.`);
    if (notDeployed || !inMeta.address || !outMeta.address) {
      return execute.setError(`Swap contracts are not configured on ${TARGET_CHAIN_NAME} yet.`);
    }
    if (!amountIn) return execute.setError("Enter an amount greater than zero.");
    if (!quote) return execute.setError("No quote available yet.");
    if (slippageBlocked) return execute.setError("Slippage above 10% is rejected.");
    if (impactBlocked) return execute.setError("Price impact is 10% or higher — swap blocked.");

    void execute.swap({
      tokenIn: inMeta.address,
      tokenOut: outMeta.address,
      recipient: address,
      amountIn,
      minAmountOut: quote.minAmountOut,
      deadlineMins,
    });
  };

  // 三类错误共用一条 banner 通道，按严重程度从配置问题到交易失败排列。
  const notices = [
    notDeployed ? `Swap router / token addresses are not configured for ${TARGET_CHAIN_NAME} yet.` : null,
    quoteError ? `Quote failed: ${quoteError.message}` : null,
    execute.error,
  ].filter((n): n is string => n !== null);

  return (
    <section className="swap-card panel" aria-labelledby="swap-card-title">
      <header className="section-heading">
        <div>
          <span className="eyebrow">Swap</span>
          <h2 id="swap-card-title">
            {tokenIn} → {tokenOut}
          </h2>
          <p>Uniswap V3 · {DEFAULT_FEE_TIER / 10_000}% pool · {TARGET_CHAIN_NAME}</p>
        </div>
      </header>

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

      <SwapSummary
        quote={quote}
        quoting={quoting}
        outMeta={outMeta}
        settlement={execute.settlement}
      />

      <SlippageControl valueBps={slippageBps} onChange={setSlippageBps} disabled={busy} />

      <label className="swap-card__deadline">
        <span>Transaction deadline (minutes)</span>
        <input
          type="number"
          min={MIN_DEADLINE_MINS}
          max={MAX_DEADLINE_MINS}
          value={deadlineMins}
          onChange={(e) => setDeadlineMins(Number(e.target.value) || DEFAULT_DEADLINE_MINS)}
          disabled={busy}
        />
      </label>

      {notices.map((notice) => (
        <p key={notice} className="notice notice--error" role="alert">
          {notice}
        </p>
      ))}

      <SwapSubmitButton
        state={state}
        tokenIn={tokenIn}
        tokenOut={tokenOut}
        isConnected={isConnected}
        onWrongChain={onWrongChain}
        isSwitching={isSwitching}
        impactBlocked={impactBlocked}
        slippageBlocked={slippageBlocked}
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
    </section>
  );
}
