# ADR 0008: 前端框架（Vite vs Next.js）

- **状态**：草案（基于 OQ-008）
- **日期**：2026-08-08

## 决策

- **MVP 保留 Vite 5 + React 18 + wagmi/viem**（与现仓一致）。
- 不引入 Next.js。
- 理由：
  - 无明确 SSR/SEO 业务需求（课程内容在登录后）。
  - 迁移成本高（wagmi SSR 配置、路由约定差异）。
  - Vite SPA 部署简单（S3 + CloudFront），CDN 缓存命中率高。

## 后续触发

- 公共课程页 SEO 是硬需求时：开 ADR 评估。
- 私有 dashboard 仍用 Vite，不动。

## 当前 monorepo 兼容性

- `apps/web` 已是 Vite；本决策零代码改动。