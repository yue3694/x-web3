# F02 — 课程与内容 任务清单

## 任务列表

- [x] **F02-T01** migration：courses / course_versions / chapters / lessons / media_assets / comments / categories `database:database/migrations/0002_course.sql` ~4h
- [x] **F02-T02** repository：课程/版本/章节/课时（含乐观锁 SQL） `api:apps/api/internal/course/` ~5h
- [x] **F02-T03** service：草稿 CRUD、作者对象级权限、章节排序 `api:apps/api/internal/course/` ~4h
- [x] **F02-T04** 状态机：transitions 表 + audit writer 集成 `api:apps/api/internal/review/` ~4h
- [x] **F02-T05** 公开列表 + 筛选 + cursor 分页 + Redis 缓存 `api:apps/api/internal/catalog/` ~6h
- [x] **F02-T06** 详情：published/enrolled 两种返回 `api:apps/api/internal/catalog/` ~3h
- [x] **F02-T07** media：presigned upload-intent + finalize + checksum 校验 `api:apps/api/internal/media/` ~8h
- [x] **F02-T08** 播放凭证：S3 presigned GET（TTL ≤ 5 min，objectstore.Fake 注入） `api:apps/api/internal/learning/` ~5h
- [x] **F02-T09** 评论：已购买校验 + 审核状态 + 软删除 `api:apps/api/internal/comment/` ~4h
- [x] **F02-T10** OpenAPI：course / media / comments `shared:packages/shared/openapi/course.yaml` ~4h
- [x] **F02-T11** 前端公开列表 + 详情 + 响应式（list 与响应式已完成） `web:apps/web/src/features/catalog/` ~8h
- [ ] **F02-T12** 前端老师编辑器（章节拖拽与媒体上传 UI 仍待） `web:apps/web/src/features/teacher/` ~12h
- [x] **F02-T13** 前端学习播放器外壳（受保护凭证 + 进度上报占位） `web:apps/web/src/features/learning/Player.tsx` ~6h
- [x] **F02-T14** 前端评论区 + 我的评论 `web:apps/web/src/features/catalog/Comments.tsx,account/` ~4h *(Comments.tsx：已购买校验 + moderation 徽章 + 软删 + 评论上限 2000；MyComments.tsx：自评论全状态 + 软删；GET /me/comments 由 CommentHandler.GetMyComments + main.go 路由挂载；repo.ListMyByUser 已就位)*
- [x] **F02-T15** 集成测试：状态机 + 乐观锁（已有）+ 评论权限矩阵 + catalog enrolled `api:apps/api/internal/integration/{course,comment}_test.go` ~8h
- [ ] **F02-T16** 组件测试：列表筛选/分页、编辑器保存冲突提示 `web:apps/web/src/**/*.test.tsx` ~6h

## 依赖与并行

- **依赖**：F01（RBAC）。
- **可并行**：T-01/02/03（数据 + 草稿）→ T-05（公开列表）可后续；T-07/08（媒体）独立。
- **阻塞下游**：F03（购买需 `courseKey`）、F04（enrollment 与 lessons 绑定）。

## 退出条件（DoD）

- [x] 状态机非法跳转全部 409 测试覆盖（`internal/integration/comment_test.go` 中的 `TestCourseStateMachine_RejectsInvalidTransitions`，覆盖 draft→archive / draft→approve / pending→archive / published→submit）。
- [x] 乐观锁冲突返回 `STALE_VERSION`（已有 `identity_test.go::TestCourseLifecycleOptimisticLockAndCatalog` 覆盖；`catalog.DetailView` / `course.UpdateDraft` 全部走 `ErrStaleVersion` 路径）。
- [ ] 媒体上传到 LocalStack 通过 + finalize 校验失败路径覆盖。
  - 已完成：`internal/media` 实现 + handler + `objectstore.FakeStore` 可在本地跑通 dev；缺：LocalStack-based 集成测试（待 F07 infra）。
- [ ] 未购买学生调 `/lessons/{id}/playback` 返回 403。
  - 已完成：`learning.Service.Playback` 通过 `enrollments` 表判定；handler `mapLearningErr` 翻译 `ErrNotEligible` → `NOT_ENROLLED` 403。集成测试待补（需已购买 fixture）。
- [x] AC-004、AC-005 通过：`TestCourseLifecycleOptimisticLockAndCatalog` 覆盖 AC-004（草稿/提交/审批/发布）；catalog 已实现 enrolled 视图对应 AC-005。

## 风险

- **乐观锁与 UI**：编辑器多 tab 时频繁冲突；需要 UI 层"重新加载"提示。
- **视频盗链**：签名 cookie TTL 太长易泄漏；服务硬限 5 min。
- **课程版本回滚**：MVP 不做；驳回只回到 draft，版本号不变。
- **对象存储**：当前 FakeStore 仅供 dev；F07 上 AWS S3 + LocalStack 集成测试。
