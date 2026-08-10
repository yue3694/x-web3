# Swap (F05)

YD ↔ USDC exchange on **Uniswap V3 / Sepolia**. Quotes come from `QuoterV2`
(`eth_call`, no state), swaps go through `SwapRouter.exactInputSingle` with an
`amountOutMinimum` + `deadline` guard.

## Files

| File | Purpose |
|------|---------|
| `swapTypes.ts` | `SwapState`, `QuoteResult`, `SwapInput`, `SwapSettlement`, `TokenMeta`, `RiskTone`. |
| `swapConfig.ts` | Addresses, fee tier, slippage/impact thresholds, token table. |
| `swapUtils.ts` | Pure math: amount parsing, slippage, deadline, price impact, tone bands. |
| `swapErrors.ts` | Error normalization + `Transfer` log parsing for actual received. |
| `useSwapQuote.ts` | QuoterV2 reads (real quote + probe quote → price impact). |
| `useSwapExecute.ts` | Write path: sign → broadcast → receipt → settlement. |
| `SwapCard.tsx` | Orchestrator + layout. |
| `SwapAmountFields.tsx` | You-pay / you-receive legs + flip button. |
| `SwapSummary.tsx` | Quote breakdown + post-trade min vs actual. |
| `SwapSubmitButton.tsx` | Submit / switch-chain button and its label matrix. |
| `SlippageControl.tsx` | 0.5 / 1 / 2% presets + custom input. |
| `PriceImpactBadge.tsx` | Colour-coded impact badge. |

## State machine (`SwapState`)

```
idle ──quote in flight──▶ quoting ──▶ idle
  │
  └─click Swap─▶ signing ─▶ confirming ─▶ done
                    │            │
                    │            └──▶ failed   (receipt error / reverted)
                    └──▶ idle    (user rejected — not an error)
                    └──▶ failed  (wallet / RPC error)
```

- **idle** — nothing in flight; the button shows the reason it is disabled
  (`Price impact too high` / `Slippage too high`) when a guard trips.
- **quoting** — `useSwapQuote` is loading. Only the *main* quote counts; the
  probe quote loading never shows a spinner (it only feeds the badge).
- **signing** — wallet prompt is open (`writeContractAsync`).
- **confirming** — tx broadcast; `useWaitForTransactionReceipt` is polling.
- **done** — receipt landed; `minReceived` vs `actualReceived` rendered.
- **failed** — terminal; the button becomes "Retry swap".

Read and write states live in two hooks and are merged in `SwapCard`:
`execute.state === "idle" && quoting ? "quoting" : execute.state`, so an
in-flight background re-quote can never mask *signing* or *confirming*.

**User rejection is not a failure.** It resets to `idle` with a "User rejected"
notice so the user can immediately re-click. We never auto-retry a swap.

## Slippage math

`amountOutMinimum` is what actually protects the user on-chain — the UI number
is advisory, this value is binding.

```
minAmountOut = amountOut × (10_000 − slippageBps) ÷ 10_000
```

- All-`bigint` integer division, so it always **rounds down** — the error is
  biased toward protecting the user (a stricter bound), never toward accepting
  a worse fill.
- `slippageBps` is clamped to `[0, 1000]` inside `applySlippage`, so an
  out-of-range value can never widen the bound past 10% even if a caller
  bypasses the UI guard.
- Defaults / bands (`swapConfig.ts`):

| Band | bps | UI |
|------|-----|----|
| default | 50 (0.5%) | preset selected |
| presets | 50 / 100 / 200 | 0.5% · 1% · 2% |
| warn | > 500 (5%) | yellow hint, still submittable |
| reject | > 1000 (10%) | red hint, **submit disabled** |

## Deadline math

```
deadline = floor(Date.now() / 1000) + deadlineMins × 60      // unix seconds
```

Default 20 min, bounded 1–60. This is a **client-side clock**: the router only
checks `block.timestamp <= deadline`, so a few seconds of drift is harmless —
which is also why `block.timestamp` is never used as a precise deadline source
(see `.claude/rules/smart-contract.md`).

> **If the deployed router is SwapRouter02**, `deadline` is *not* a struct
> field — `IV3SwapRouter.ExactInputSingleParams` drops it and you pass it via
> `multicall(uint256 deadline, bytes[] data)` instead. `swap.abi.ts` currently
> models the original `SwapRouter` (deadline inside the struct). Switching
> routers means changing both the ABI and the call shape in `useSwapExecute`.

## Price impact

There is no oracle here, so impact is derived from **two quotes**:

```
probeIn  = 1/1000 of one tokenIn unit          //近似边际价, small enough to barely move the pool
ideal    = probeOut × amountIn ÷ probeIn       // linear extrapolation of the marginal price
impact   = (ideal − amountOut) ÷ ideal × 100
```

- `null` when the probe quote is unavailable → badge shows "—" and the swap is
  **not** blocked, because `amountOutMinimum` still bounds the downside.
- Negative impact (better than marginal price, or rounding) clamps to `0`.

| Band | Impact | Badge |
|------|--------|-------|
| ok | < 1% | green |
| warn | 1–5% | yellow |
| danger | 5–10% | red |
| blocked | ≥ 10% | red + **submit disabled** (F05-T06) |

## ABI source

Both ABIs are **stubs** and live outside this folder:

| File | Upstream |
|------|----------|
| `@/contracts/quoter.abi.ts` | `v3-periphery/contracts/lens/QuoterV2.sol` |
| `@/contracts/swap.abi.ts` | `v3-periphery/contracts/interfaces/ISwapRouter.sol` |

These are Uniswap-deployed contracts, **not** ours — `pnpm contracts:export:abi`
will never generate them. Replace the stubs by copying the real ABIs from
`@uniswap/v3-periphery` / `@uniswap/swap-router-contracts` artifacts.

Two things to preserve when you do:

1. **`quoteExactInputSingle` is marked `view` in our stub but is `nonpayable`
   on-chain.** QuoterV2 works by reverting inside the swap callback and decoding
   the revert payload, so it cannot be `view` in Solidity — but it writes
   nothing and is safe over `eth_call`. wagmi's `useReadContract` only accepts
   `view`/`pure`, so the override is deliberate (standard Uniswap-frontend
   practice). Dropping it breaks `useSwapQuote` at the type level.
2. The `deadline` placement note above.

Per the F05 contract, `SwapCard` throws `"ABI not yet exported"` at render if
either ABI is empty — same guard as `CheckoutButton`.

## Addresses

| Value | Source |
|-------|--------|
| YD | `ydTokenDeployments.sepolia` (`deployments.ts`) |
| USDC | `VITE_USDC_ADDRESS` ⚠️ temporary |
| SwapRouter | `VITE_SWAP_ROUTER_ADDRESS` ⚠️ temporary |
| QuoterV2 | `VITE_QUOTER_ADDRESS` ⚠️ temporary |

⚠️ **`deployments.ts` only has `ydTokenDeployments` today.** The spec calls for
`swapRouterDeployments.sepolia` / `quoterDeployments.sepolia`, but that file was
out of scope for this change, so `swapConfig.ts` reads the other three from env
using the same `optionalAddress` validation. Once the deployment slots exist,
delete the `envAddress` branch in `swapConfig.ts` and import them directly.

Any missing address degrades the UI to a "not configured" notice with the
submit button disabled — it never sends a transaction to `undefined`.

## TODOs

- Register `swapRouterDeployments` / `quoterDeployments` / `usdcDeployments` in
  `deployments.ts`; drop `envAddress` from `swapConfig.ts`.
- Replace both ABI stubs with the real artifacts (keep the two notes above).
- **ERC-20 approval is not implemented.** `exactInputSingle` pulls `tokenIn` via
  `transferFrom`, so a swap reverts unless the router already has an allowance.
  An `allowance` read + `approve` step (or Permit2) is required before this is
  usable end-to-end.
- Fee tier is hard-coded to 0.3%; a real router would quote across
  500/3000/10000 and pick the best.
- No balance check — the button lets you submit more than you hold and the tx
  reverts on-chain.
- No `vitest` coverage yet; `swapUtils.ts` / `swapErrors.ts` are pure and were
  written to be directly testable.
- Styles assume `.swap-card__*` / `.slippage-control__*` /
  `.price-impact-badge--*` classes that do **not** exist in `styles.css` yet;
  it renders unstyled until a design pass adds them.
