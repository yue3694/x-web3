---
description: 密钥、敏感文件、私钥 / RPC / Etherscan 凭据处理规范
---

# Security

## 禁止事项

- **严禁**把任何真实私钥、助记词、`ETHERSCAN_API_KEY` 提交到仓库。
- **严禁**在 JS / TS 代码里 `hardcode` 地址、私钥、API key；一律走 `import.meta.env` 或 `process.env`。
- **严禁**在 PR 描述里贴私钥；任何疑似泄露立即轮换。
- **严禁**把部署账户当主仓使用 — 它是热钱包，余额控制在够付 gas 即可。

## 密钥与凭据

| 变量 | 存放 | 是否进 git |
|------|------|-----------|
| `DEPLOYER_PRIVATE_KEY` | `.env` (root & `packages/contracts/.env`) | ❌ |
| `SEPOLIA_RPC_URL` | `.env` | ❌ |
| `ETHERSCAN_API_KEY` | `.env` | ❌ |
| `VITE_SEPOLIA_RPC_URL` | `apps/web/.env` | ❌（Vite 仅暴露 `VITE_` 前缀） |
| `VITE_DEPLOYER_ADDRESS` | `apps/web/.env` | ❌ |

`.env` 已在 `.gitignore` 中忽略；`.env.example` 是模板，**可以**提交。

## 前端公开变量边界

- `import.meta.env.VITE_*` 会出现在最终 bundle 里 — 只放**公开值**（RPC 端点、项目 ID）。
- 任何 *secret* 必须走后端 / Cloud Function / KMS，不能进前端。

## 合约层防御

- 重入：所有 `external` 函数若涉及 ETH / 状态写入，遵循 checks-effects-interactions。
- 整数：Solidity 0.8 默认 checked，但 `unchecked` 块必须有 `// unchecked-safety:` 注释说明为何安全。
- 权限：`Ownable` 仅做管理员；普通用户权限用 `AccessControl`（OpenZeppelin）。
- 升级：合约默认 **不可升级**；如需升级显式引入 UUPS 并加时间锁。

## 部署后

- Etherscan 验证后**再次**检查合约源码与字节码是否一致。
- 在 `packages/contracts/broadcast/<chain>/run-latest.json` 留档，作为部署证据。
- 把已部署地址登记到 `apps/web/src/contracts/deployments.ts`，避免 drift。