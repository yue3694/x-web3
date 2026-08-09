# F05 — Token、DEX 与 Oracle（Token / DEX / Oracle）

> 来源：上级 `requirements.md` F-031 ~ F-034；本特性在 monorepo 中的实现切片。
> 状态：**条件阶段**，OQ-001/002/004/007 决议前只做最小可用版本（YD Token + 接口）。

## 1. 范围

- YD Token（ERC-20 + Permit + AccessControl + Pausable）。
- 高权限角色多签；生产禁单热钱包。
- Uniswap YD/USDC 主池集成（前端报价 + swap）。
- Chainlink 仅在明确用例下启用（MVP 默认不接）。

## 2. 功能需求

| ID | 描述 | 验收 |
|---|---|---|
| **R-TX-001** | YD Token 基于 OpenZeppelin ERC20 + Permit + AccessControl + Pausable；部署配置中显式 cap / initial supply / treasury / mint 权限 | 部署脚本 + 文档化配置 |
| **R-TX-002** | `DEFAULT_ADMIN_ROLE` / `MINTER_ROLE` 必须多签持有；生产禁单热钱包长期持有 | 部署后角色 transfer 文档化 |
| **R-TX-003** | Uniswap 集成优先 YD/USDC 单一主池；前端报价含滑点、deadline、minOut、价格影响提示 | E2E swap demo |
| **R-TX-004** | Chainlink 仅用于已定义的外部价格 / 自动化 / 证明；Oracle 结果必须校验 `updatedAt`、round、decimals、偏差、fallback | 待 OQ-007 决议 |
| **R-TX-005** | 角色 / 暂停变更必须 emit 事件 | 单测覆盖 |

## 3. YD Token 接口

```solidity
interface IYDToken is IERC20, IERC20Permit, IAccessControl, IPausable {
  function cap() external view returns (uint256);
  function treasury() external view returns (address);
  function mint(address to, uint256 amount) external;          // MINTER_ROLE
  function burn(uint256 amount) external;                       // public or role
  function pause() external;                                    // PAUSER_ROLE
  function unpause() external;
  // 事件：Transfer (OZ) / Role events / Paused / Unpaused
}
```

## 4. Uniswap 集成

```text
前端 (apps/web/src/features/swap/):
- 使用 Uniswap v3 SDK / viem 直接调用 SwapRouter02
- Quote: QuoterV2.quoteExactInputSingle
- Swap: SwapRouter02.exactInputSingle{ value: 0 }(
    SwapExactInputSingleParams{
      tokenIn, tokenOut, fee, recipient, deadline,
      amountIn, amountOutMinimum, sqrtPriceLimitX96
    }
  )
- 滑点默认 0.5%（可在 UI 调整，max 5%）
- 价格影响 > 3% 弹窗警告；> 10% 阻止
```

## 5. 数据模型

```sql
swap_quotes(id, user_id, token_in, token_out, amount_in, amount_out_estimated, slippage_bps, expires_at)  -- 可选审计
swap_records(id, user_id, chain_id, tx_hash unique, token_in, token_out, amount_in, amount_out, status, created_at)
```

swap 不强制落库，但保留以便 audit + 推荐"最近成交价"。

## 6. Oracle 接入（条件阶段）

```solidity
interface IPriceOracle {
  function latestRoundData() external view returns (
    int256 answer, uint256 updatedAt, uint256 roundId
  );
  function decimals() external view returns (uint8);
  function description() external view returns (string memory);
}
```

校验清单：
- `updatedAt` 在 `block.timestamp - heartbeat` 内
- `answer > 0`
- `roundId` 单调递增（与上一轮对比）
- `decimals` 与使用处一致
- 偏差 > 阈值 fallback / 告警

## 7. 非功能需求

- 报价 P95 ≤ 800 ms（含 RPC roundtrip）。
- 滑点与 deadline 校验在提交前完成；不依赖后端。

## 8. 边界

- **依赖 F03**：YD Token 地址需在 CourseMarket 配置前确定。
- **不在范围**：Treasury 多签合约本身（推荐 Gnosis Safe，由 OQ 决议）。
- **被依赖**：前端购买体验（用户需 YD）。