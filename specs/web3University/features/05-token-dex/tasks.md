# F05 — Token / DEX / Oracle 任务清单

## 任务列表

- [x] **F05-T01** YDToken.sol：ERC20 + Permit + AccessControl + Pausable + cap + treasury `contracts:packages/contracts/src/YDToken.sol` ~5h *(per ADR-0002: cap=1B, initial=200M)*
- [x] **F05-T02** YDToken 单测：cap / mint / pause / permit + fuzz `contracts:packages/contracts/test/YDToken.t.sol` ~4h *(17 unit + 1 fuzz + 1 invariant)*
- [x] **F05-T03** YDToken 部署脚本：treasury 多签转移 checklist `contracts:packages/contracts/script/DeployYDToken.s.sol` ~3h *(Mode A env + Mode B JSON；17 单测)*
- [x] **F05-T04** ABI 导出 + chain registry（YD/USDC 主池地址） `contracts+web:apps/web/src/contracts/token.ts,swap.ts` ~3h *(ABI export script + deployments.ts + ydTokenDeployments slot)*
- [x] **F05-T05** 前端 SwapCard：网络/余额检查 + QuoterV2 + 滑点/deadline/影响 `web:apps/web/src/features/swap/` ~10h
- [x] **F05-T06** 前端价格影响提示 + 阻止逻辑（≥ 10% 阻止） `web:apps/web/src/features/swap/SlippageControl.tsx` ~3h
- [ ] **F05-T07** 集成测试：本地 anvil fork Uniswap 主网做 swap `web:apps/web/src/features/swap/SwapCard.test.tsx` ~4h
- [ ] **F05-T08** Oracle 抽象与 StalePriceGuard（条件阶段，OQ-007 通过后才做） `contracts:packages/contracts/src/libs/StalePriceGuard.sol` ~3h
- [ ] **F05-T09** Oracle 应用示例 + 测试（条件阶段） `worker+contracts:apps/worker/internal/oracle/` ~6h
- [x] **F05-T10** E2E：Sepolia swap YD → USDC 演示 `qa:tests/e2e/swap.spec.ts` ~3h

## 依赖与并行

- **依赖 F03**：CourseMarket 部署需要 YD 地址（CourseMarket 配置早于 swap UI）。
- **可并行**：T-01～T-04（合约）与前端 T-05/06 并行（基于 mock 报价）。
- **阻塞下游**：无（功能独立）。

## 退出条件（DoD）

- [x] YD Token cap / mint / pause 单测覆盖。 *(17 unit tests + fuzz + invariant)*
- [x] YDToken 部署脚本：Mode A env + Mode B JSON。 *(DeployYDToken.s.sol + 17 单测)*
- [ ] Sepolia 部署后多签角色转移 checklist 完成并截图留档。 *(F05-T03 pending; 部署脚本已就绪，待 Sepolia 演练)*
- [x] SwapCard：滑点 + deadline + 价格影响全部 UI 校验。 *(apps/web/src/features/swap/{SwapCard,SlippageControl,PriceImpactBadge}.tsx)*
- [ ] Anvil fork 测试 swap 路径。 *(F05-T07 pending)*
- [ ] （条件阶段）Oracle stale/fallback 测试。 *(F05-T08/T09 conditional)*

## 风险

- **OQ-002/004 未决**：tokenomics、是否引入稳定币会改变 YD 部署配置；先用占位 cap。
- **Uniswap 主池流动性**：上 mainnet 前必须有真实流动性，否则价格剧烈波动。
- **Chainlink SLA**：若引入，必须有明确 SLA 与 fallback 策略；否则放弃。