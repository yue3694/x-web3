# F02 — 课程与内容 设计

## 1. monorepo 落点

```text
apps/api/internal/
├── course/         # repository + service（草稿 CRUD、乐观锁）
├── review/         # 状态机 + audit
├── catalog/        # 公开列表/详情 + 缓存
├── media/          # presigned upload/finalize + 校验
├── comment/        # 评论 + 审核 + 软删除
└── learning/       # 播放凭证签发（与 F04 共享）

apps/web/src/features/
├── catalog/        # CourseList / CourseDetail
├── teacher/        # CourseEditor / ChapterEditor / MediaManager / SubmitReview
├── learning/       # LearningPlayer（与 F04 共享）
└── account/        # MyComments（评论历史）

database/migrations/0002_course.sql
packages/shared/openapi/course.yaml
```

## 2. API 契约

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `/courses` | 公开 | 列表（paging/filter） |
| `GET` | `/courses/{id}` | 公开/登录 | 详情（enrolled=true 时含受保护内容） |
| `POST` | `/teacher/courses` | `COURSE_CREATE` | 创建 draft |
| `PUT` | `/teacher/courses/{id}` | 作者 + `COURSE_EDIT` | 乐观锁（`If-Match: version`） |
| `POST` | `/teacher/courses/{id}/submit` | 作者 | draft → pending_review |
| `POST` | `/admin/courses/{id}/review` | `COURSE_APPROVE` | approve/reject + reason |
| `POST` | `/admin/courses/{id}/archive` | `COURSE_APPROVE` | published → archived |
| `POST` | `/media/upload-intent` | `MEDIA_UPLOAD` | 返回 presigned PUT |
| `POST` | `/media/{id}/finalize` | `MEDIA_UPLOAD` | 校验对象 + checksum |
| `GET` | `/lessons/{id}/playback` | 登录 + 资源策略 | 签发播放凭证 |
| `POST` | `/courses/{id}/comments` | 已购买 | 写评论 |
| `PATCH` | `/admin/comments/{id}` | `COMMENT_MODERATE` | 审核/删除 |

## 3. 乐观锁

```sql
UPDATE courses SET title=?, current_version=current_version+1, updated_at=now()
WHERE id=? AND current_version=?  -- 来自 If-Match
RETURNING current_version;
```

更新返回 0 行 → `409 STALE_VERSION`。

## 4. 状态机实现

`internal/review/state.go`：

```go
var transitions = map[Status]map[Action]Status{
    Draft:           {Submit: PendingReview},
    PendingReview:   {Approve: Published, Reject: Draft},
    Published:       {Archive: Archived},
    Archived:        {Unarchive: Draft},
}
```

每个 transition 调 `audit.Write("course.review", ...)`，reason 入库。

## 5. 公开列表缓存

- key：`catalog:courses:p={page}:f={filters_hash}`，TTL 60s。
- filter hash 包含 `category, q, teacher_id, price_min/max, sort`；变更时主动失效（`catalog:invalidate` Redis pub/sub）。
- 价格筛选在 Redis 里只缓存轻量字段（id/title/price_min/teacher/published_at）；详情按需 DB 读。

## 6. 媒体上传安全

| 检查 | 位置 |
|---|---|
| MIME 白名单（mp4/webm/mov/pdf） | API 上传意图 |
| size 上限（默认 2 GB，可在 ADR 改） | API 上传意图 |
| presigned PUT 仅允许 `Content-Type` 与 `Content-Length` 范围 | S3 POST policy |
| finalize 时 `HeadObject` 校验 size + `etag`（客户端给 checksum 时再算一次 sha256） | API finalize |
| 视频扫描/转码（MVP 不做，但留 async hook） | 预留 `media_assets.scan_status` 字段 |
| 私有 S3 + CloudFront OAC + signed cookie/URL | infra |

## 7. 错误码

| code | http | 含义 |
|---|---|---|
| `COURSE_STATE_CONFLICT` | 409 | 非法状态跳转 |
| `STALE_VERSION` | 409 | 乐观锁失败 |
| `MEDIA_CHECKSUM_MISMATCH` | 422 | finalize 校验失败 |
| `NOT_ENROLLED` | 403 | 播放凭证签发拒绝 |
| `COMMENT_NOT_PURCHASED` | 403 | 未购买不可评论 |

## 8. 测试策略

- **单测**：状态机迁移、乐观锁、缓存 key hash、presigned URL 构造。
- **集成**：用 testcontainers + LocalStack（S3）跑上传/下载；评论权限矩阵。
- **E2E（Phase 8）**：老师创建→提交→超管发布→学生浏览→已购买学生评论。