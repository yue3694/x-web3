# F02 — 课程与内容（Course & Content）

> 来源：上级 `requirements.md` F-008 ~ F-014；本特性在 monorepo 中的实现切片。

## 1. 范围

- 老师创建/编辑/归档课程、章节、课时、视频元数据。
- 课程状态机 `draft → pending_review → published → archived`，驳回回 draft 并记原因。
- 公开课程列表 / 详情 / 筛选；学生可对已购买课程评论。
- S3 私有视频 + 短时签名 URL / CloudFront 凭证。

## 2. 功能需求

| ID | 描述 | 验收 |
|---|---|---|
| **R-CC-001** | 老师可创建/编辑/归档课程，维护章节/课时/视频元数据/排序 | AC-004 |
| **R-CC-002** | 课程状态机：`draft → pending_review → published → archived`；驳回回 draft + reason | AC-004：非法跳转返回 409 |
| **R-CC-003** | 只有课程作者可改 draft；超管可审核/下架/查全部 | AC-004 |
| **R-CC-004** | 访客只读 published；已购买学生读受保护视频 | AC-005 |
| **R-CC-005** | 视频对象存 S3，DB 只存元数据与对象键；访问短时签名 URL / CloudFront 凭证 | 攻击者拿到对象键无法播放 |
| **R-CC-006** | 课程列表支持分页 / 分类 / 关键词 / 老师 / 价格筛选，结果稳定 | 集成测试 |
| **R-CC-007** | 学生可对已购买课程评论；保留审核状态与软删除 | 集成测试 |

## 3. 数据模型

```sql
courses(id uuid pk, teacher_id fk→users, slug unique, title, status, current_version, published_at, deleted_at)
course_versions(id, course_id fk, version int, description, completion_rule JSONB, unique(course_id, version))
chapters(id, course_version_id fk, position int, title, unique(course_version_id, position))
lessons(id, chapter_id fk, position int, title, required bool, media_asset_id fk nullable, duration_seconds)
media_assets(id, s3_key, content_type, size_bytes, status, checksum_sha256, created_at)
comments(id, course_id fk, user_id fk, body text, moderation_status enum, deleted_at)
course_categories(id, slug unique, title)            -- 简单二级分类
course_category_map(course_id, category_id)
```

## 4. 状态机

```text
draft ──submit──> pending_review
pending_review ──approve──> published
pending_review ──reject(reason)──> draft
published ──archive──> archived
archived ──unarchive──> draft      -- 仅超管；恢复时课程版本号 +1
```

每次状态迁移必须写 `course_audit_logs(course_id, from_status, to_status, actor, reason, created_at)`。

## 5. 媒体上传流程

```text
Teacher → POST /media/upload-intent { fileName, contentType, size }
API 校验白名单 MIME / size 上限 → 返回 presigned PUT URL + media_asset_id (status=draft)

Teacher → PUT s3://... (presigned)
S3 EventBridge (可选) 或 前端主动 → POST /media/{id}/finalize { checksum }

API 校验 head object / checksum → status=ready
Teacher → lessons.media_asset_id = ...
```

## 6. 播放凭证

- 学生访问 lesson 时：`GET /lessons/{id}/playback` 检查 enrollment，确认后返回 CloudFront signed cookie 或 S3 presigned GET（TTL ≤ 5 分钟）。
- 教师预览自己 draft 时返回同样凭证但带 `purpose=preview` 审计标记。

## 7. 非功能需求

- 课程列表 P95 ≤ 300 ms（命中 Redis 缓存）。
- 默认分页 ≤ 50 条，cursor 稳定（`WHERE (published_at, id) < (?, ?)`）。
- S3 key 命名 `courses/{course_id}/versions/{version}/lessons/{lesson_id}/{uuid}.{ext}`，永远私有。

## 8. 边界

- **不在范围内**：视频转码、DRM、直播、CDN 选型评审（OQ-005）。
- **依赖 F01**：RBAC、audit。
- **被依赖**：F03（购买链上价格）、F04（学习/证书依赖 enrollment 与课程版本）。