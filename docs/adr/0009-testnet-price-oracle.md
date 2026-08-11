# ADR 0009: 测试链价格预言机

- **状态**：已接受
- **日期**：2026-08-11
- **取代**：ADR 0007

## 决策

- 测试阶段接入 Chainlink-compatible 价格预言机。
- Anvil 使用 `MockV3Aggregator + ChainlinkPriceOracle`，Sepolia 使用相同 Adapter
  包装评审通过的 Chainlink feed。
- 当前用途仅为 YD/USD（或支付币/USD）参考价展示；课程结算仍使用
  `purchase_intents.amount` 冻结的 ERC-20 数量。
- Adapter 强制验证：`answer > 0`、`updatedAt`、最大年龄、未来时间戳和
  `answeredInRound >= roundId`。

## 暂不允许

- 未定义 feed 与 SLA 前，不得用 Oracle 自动修改课程价格。
- 不得把 Anvil Mock 地址带到 Sepolia 或任何正式环境。
- 不得把单一 Oracle 返回值直接写入核心会计或完课判定。
