# F05 — Token / DEX / Oracle 设计

## 1. monorepo 落点

```text
packages/contracts/src/
├── YDToken.sol                    # ERC20 + Permit + AccessControl + Pausable
├── interfaces/IYDToken.sol
└── libs/StalePriceGuard.sol       # Oracle stale/fallback 校验

apps/web/src/features/swap/
├── SwapCard.tsx
├── useQuote.ts                    # 调用 QuoterV2
├── useSwap.ts                     # 调用 SwapRouter02
└── SlippageControl.tsx

apps/web/src/contracts/
├── token.ts                       # YD ABI + 地址（多链 registry）
├── swap.ts                        # SwapRouter02 + QuoterV2 ABI
└── oracle.ts                      # （条件阶段）

packages/shared/openapi/swap.yaml  # 可选：后端不做 swap 代理，仅记录报价历史
```

## 2. YDToken 设计要点

- 继承顺序：`ERC20 → ERC20Permit → AccessControl → Pausable`（OZ v5 标准）。
- `constructor(address admin, address treasury, uint256 cap, uint256 initialSupply)`：
  - `cap` 若为 0 则不可增发；`initialSupply` 铸给 treasury。
  - `_grantRole(DEFAULT_ADMIN_ROLE, admin)`，`_grantRole(MINTER_ROLE, treasury)`（若 cap > 0）。
  - `Pausable` 初始 unpaused。
- `mint` 仅 MINTER_ROLE；`pause/unpause` 仅 PAUSER_ROLE（建议 treasury 持有）。
- 自定义错误：`CapExceeded()`、`NotMinter()`、`NotPauser()`。
- 事件：`RoleGranted / RoleRevoked / Paused / Unpaused`（OZ 自带）。

## 3. Treasury 与多签

- 部署后必须立即：
  ```
  DEFAULT_ADMIN_ROLE.revokeRole(deployer)
  DEFAULT_ADMIN_ROLE.grantRole(gnosisSafe)
  MINTER_ROLE.revokeRole(deployer)
  ```
- 部署脚本末尾打印"multisig takeover checklist"，人工在 Safe 端确认。

## 4. Uniswap 集成路径

**路径 A（MVP 默认）**：前端直接调链上合约，不走后端。
- 优点：信任最小化、不增加后端攻击面。
- 缺点：RPC 限流时体验差。

**路径 B（可选）**：API 提供报价缓存 + swap helper。
- `GET /swap/quote?tokenIn=...&tokenOut=...&amountIn=...&slippageBps=...`
- 后端用 worker 维护池子状态缓存，TTL 10 s。
- 暂不实施，OQ 决议后评估。

## 5. 前端报价 / 滑点 / 影响

```ts
const quote = await quoter.quoteExactInputSingle({
  tokenIn: YD, tokenOut: USDC, fee: 3000,
  amountIn, sqrtPriceLimitX96: 0
})

const priceImpact = computeImpact(quote, midPrice)
if (priceImpact > 0.10) throw new PriceImpactTooHigh()

const minOut = applySlippage(quote.amountOut, slippageBps)
const deadline = now() + 60 * 20  // 20 min

await swapRouter.exactInputSingle({
  tokenIn, tokenOut, fee, recipient, deadline,
  amountIn, amountOutMinimum: minOut, sqrtPriceLimitX96: 0
})
```

## 6. Oracle（条件阶段）

```solidity
abstract contract StalePriceGuard {
  uint256 public heartbeat;
  uint256 public maxDeviationBps;

  function _validate(int256 answer, uint256 updatedAt, uint256 roundId) internal view {
    if (block.timestamp - updatedAt > heartbeat) revert StalePrice();
    if (answer <= 0) revert InvalidAnswer();
    // roundId 检查在 consumer 中
  }
}
```

应用场景举例（OQ-007 通过后）：
- 完课证书价值锚定（仅展示用，不入会计）。
- 自动化 treasury rebalance（不参与课程完成判定）。
- 不替代课程完成的链上事件权威。

## 7. 测试策略

- **合约**：YDToken（cap / mint / pause / permit 单测 + fuzz）；Oracle stale/fallback。
- **前端**：SwapCard 组件测试（mock viem）；价格影响边界。
- **E2E（Sepolia）**：swap YD → USDC，检查 receipt 与余额变化。

## 8. 安全检查

- [ ] YD Token cap 与 initialSupply 在部署前经多签确认。
- [ ] `MINTER_ROLE` 默认由 treasury 多签持有；不允许 EOA 长期持有。
- [ ] Uniswap 集成不走 `tx.origin`；recipient 必须是 `msg.sender`。
- [ ] 滑点上限；价格影响上限；deadline 必须设。
- [ ] Oracle 数据不写入核心会计；只用于展示或非关键判定。