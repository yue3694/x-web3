# F02 — 课程与内容 任务清单

## 任务列表

- [ ] **F02-T01** migration：courses / course_versions / chapters / lessons / media_assets / comments / categories `database:database/migrations/0002_course.sql` ~4h
- [ ] **F02-T02** repository：课程/版本/章节/课时（含乐观锁 SQL） `api:apps/api/internal/course/` ~5h
- [ ] **F02-T03** service：草稿 CRUD、作者对象级权限、章节排序 `api:apps/api/internal/course/` ~4h
- [ ] **F02-T04** 状态机：transitions 表 + audit writer 集成 `api:apps/api/internal/review/` ~4h
- [ ] **F02-T05** 公开列表 + 筛选 + cursor 分页 + Redis 缓存 `api:apps/api/internal/catalog/` ~6h
- [ ] **F02-T06** 详情：published/enrolled 两种返回 `api:apps/api/internal/catalog/` ~3h
- [ ] **F02-T07** media：presigned upload-intent + finalize + checksum 校验 `api:apps/api/internal/media/` ~8h
- [ ] **F02-T08** 播放凭证：CloudFront signed cookie 或 S3 presigned GET（TTL ≤ 5 min） `api:apps/api/internal/learning/playback.go` ~5h
- [ ] **F02-T09** 评论：已购买校验 + 审核状态 + 软删除 `api:apps/api/internal/comment/` ~4h
- [ ] **F02-T10** OpenAPI：course / media / comments `shared:packages/shared/openapi/course.yaml` ~4h
- [ ] **F02-T11** 前端公开列表 + 详情 + 响应式 `web:apps/web/src/features/catalog/` ~8h
- [ ] **F02-T12** 前端老师编辑器（章节排序拖拽 + 媒体上传 + 提交审核） `web:apps/web/src/features/teacher/` ~12h
- [ ] **F02-T13** 前端学习播放器外壳（受保护凭证 + 进度上报占位） `web:apps/web/src/features/learning/Player.tsx` ~6h
- [ ] **F02-T14** 前端评论区 + 我的评论 `web:apps/web/src/features/catalog/Comments.tsx,account/` ~4h
- [ ] **F02-T15** 集成测试：状态机 + 乐观锁 + 媒体上传（LocalStack） + 评论权限 `api:apps/api/internal/**/*_test.go` ~8h
- [ ] **F02-T16** 组件测试：列表筛选/分页、编辑器保存冲突提示 `web:apps/web/src/**/*.test.tsx` ~6h

## 依赖与并行

- **依赖**：F01（RBAC）。
- **可并行**：T-01/02/03（数据 + 草稿）→ T-05（公开列表）可后续；T-07/08（媒体）独立。
- **阻塞下游**：F03（购买需 `courseKey`）、F04（enrollment 与 lessons 绑定）。

## 退出条件（DoD）

- [ ] 状态机非法跳转全部 409 测试覆盖。
- [ ] 乐观锁冲突返回 `STALE_VERSION`。
- [ ] 媒体上传到 LocalStack 通过 + finalize 校验失败路径覆盖。
- [ ] 未购买学生调 `/lessons/{id}/playback` 返回 403。
- [ ] AC-004、AC-005 通过。

## 风险

- **乐观锁与 UI**：编辑器多 tab 时频繁冲突；需要 UI 层"重新加载"提示。
- **视频盗链**：签名 cookie TTL 太长易泄漏；建议 5 min。
- **课程版本回滚**：MVP 不做；驳回只回到 draft，版本号不变。