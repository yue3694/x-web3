# ADR 0001: 生产目标链、确认数与 gas 策略

- **状态**：草案（基于 OQ-001）
- **日期**：2026-08-08

## 决策

| 项 | 取值 | 说明 |
|---|---|---|
| MVP 测试链 | **Sepolia**（chainId 11155111） | 与 `packages/contracts/foundry.toml` 一致；Etherscan + 水龙头成熟 |
| 生产链（待评审） | TBD；MVP 不上线 | 候选：Base / OP / Arbitrum，gas 与生态考量 |
| Confirmation depth | **12 块**（Sepolia ≈ 36s） | 兼顾 UX 与 reorg 容忍 |
| Gas 代付 | **用户自付**（MVP 不做 paymaster） | 简化 Worker signer 与 nonce 管理 |
| RPC | **多供应商 fallback**（Alchemy / Infura / 公共） | 防单点；M0 暂用单一供应商 |

## 后果

- Sepolia confirmation 12 块引入 ~36s 等待，UX 可接受（MVP）。
- 用户需自备 ETH → 必须在前端做强引导（账户检测 + 切换链）。
- 后续上主网时需重新审计 confirmation depth 与 reorg 策略。

## 后续动作

- `infra/aws/` 接入多供应商 RPC（Phase 8）。
- Worker 增加 `chain_id` 配置化，支持多链。