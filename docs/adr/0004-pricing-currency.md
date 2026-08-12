# ADR 0004: 课程定价币种

- **状态**：草案（基于 OQ-004）
- **日期**：2026-08-08

## 决策（MVP）

- **仅 YD Token** 定价。
- USD 锚定展示层做（前端 `displayPriceUsd = priceYd * oracleUsdPerYd`），**不入会计**。
- 价格波动风险由平台承担（短期）；后续可引入稳定币。

## 后续

- `course_prices` 表支持 `(chain_id, token)` 多版本，引入 USDC 时新增版本号。
- 前端报价组件按 `token` 自动选择滑点与精度。