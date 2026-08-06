---
description: 分支命名、commit 规范、PR 流程
---

# Git workflow

## 分支

- `main` — 始终可部署；保护分支，需 PR。
- `feat/<scope>-<short-desc>` — 新功能，如 `feat/contracts-staking`。
- `fix/<scope>-<short-desc>` — 修复，如 `fix/web-wrong-network-detect`。
- `chore/<scope>-<short-desc>` — 杂项（依赖、CI、文档）。

## Commit message（Conventional Commits）

```
<type>(<scope>): <subject>

[optional body]

[optional footer]
```

- `type`: `feat | fix | chore | docs | test | refactor | perf | security`
- `scope`: `web | contracts | workspace | deps`
- subject 中文 / 英文均可，但**不超过 72 字符**，无句号。
- Breaking change 在 footer 写 `BREAKING CHANGE: <描述>`。

示例：
```
feat(contracts): add Counter decrement + underflow guard
fix(web): auto-switch wallet to Sepolia on mismatch
chore(deps): bump wagmi to 2.13
```

## PR 流程

1. 分支命名遵循上面规则。
2. PR 描述：What / Why / How to test / Screenshots（前端）。
3. 必跑：`pnpm contracts:test && pnpm typecheck`。
4. 合约变更：附 `forge coverage` 摘要与任何审计点。
5. Squash merge 到 `main`，commit message 由 GitHub 自动生成。

## 紧急修复

- 生产级问题用 `hotfix/<scope>-<short-desc>` 直接 PR 到 `main`，
  通过后立即回 cherry-pick 到活跃开发分支。