# ADR 0002: YD Token 经济模型

- **状态**：草案（基于 OQ-002）
- **日期**：2026-08-08

## 决策（MVP 占位）

| 项 | 取值 |
|---|---|
| 标准 | ERC-20 + Permit + AccessControl + Pausable（OZ v5） |
| `cap` | **1,000,000,000**（10 亿，1e9，decimals 18） |
| 初始供应 | **200,000,000**（2 亿；80% 留给后续空投与生态） |
| Treasury | 部署后由 **Gnosis Safe 多签**接管 DEFAULT_ADMIN_ROLE + MINTER_ROLE |
| 暂停策略 | `PAUSER_ROLE` = treasury 多签；紧急情况可暂停 transfer |
| 增发 | cap 未到顶前可增发；cap = initial supply 时撤销 MINTER_ROLE 永久 |

## 开放问题

- 监管定位（utility vs security）：需法务评估；本 MVP 不上线主网仅 Sepolia。
- 销毁机制：MVP 不实现；可后续加 `burn`。

## 后果

- Treasury 多签门槛：MVP 部署脚本自动 revoke deployer 角色并打印清单。
- 测试网无真实经济价值，初始供应与 cap 仅占位。