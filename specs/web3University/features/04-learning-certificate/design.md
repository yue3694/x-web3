# F04 — 学习与证书 设计

## 1. monorepo 落点

```text
packages/contracts/src/
├── CertificateNFT.sol      # ERC721 + MINTER_ROLE + 防重复 + soulbound
└── interfaces/ICertificateNFT.sol

apps/api/internal/
├── learning/               # enrollment 校验 + 进度 + 完成判定
└── certificate/            # mint job 创建 + 证书查询

apps/worker/internal/
└── certificate/            # mint signer + receipt 确认 + DLQ + 重试

apps/web/src/features/
├── learning/
│   ├── LearningPlayer.tsx
│   ├── useProgressSync.ts
│   └── CompleteButton.tsx
└── account/
    ├── MyCertificates.tsx  # 链上 NFT 展示
    └── MyEnrollments.tsx

database/migrations/0004_learning.sql
packages/shared/openapi/learning.yaml
packages/shared/openapi/certificate.yaml
packages/shared/events/certificate.ts
```

## 2. API 契约

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` | `/lessons/{id}/progress` | enrollment owner | 幂等写进度 |
| `POST` | `/courses/{id}/complete` | enrollment owner | 重算完成；满足则创建 mint job |
| `GET` | `/me/enrollments` | 登录 | 我的 enrollment |
| `GET` | `/me/progress?course_id=` | 登录 owner | 我的进度 |
| `GET` | `/me/certificates` | 登录 | 我的证书（含链上状态） |
| `GET` | `/certificates/{id}` | owner / admin | 证书详情 |
| `POST` | `/admin/certificates/{id}/retry` | `SYSTEM_ADMIN` | 重试 mint job |

## 3. 进度不倒退实现

```sql
INSERT INTO lesson_progress (enrollment_id, lesson_id, progress_bps, completed_at, version)
VALUES ($1, $2, $3, $4, 1)
ON CONFLICT (enrollment_id, lesson_id) DO UPDATE
  SET progress_bps = GREATEST(lesson_progress.progress_bps, EXCLUDED.progress_bps),
      completed_at = COALESCE(lesson_progress.completed_at, EXCLUDED.completed_at),
      version = lesson_progress.version + 1
  WHERE EXCLUDED.progress_bps >= lesson_progress.progress_bps;
```

`WHERE` 子句是兜底：若 EXCLUDED 倒退，0 行更新 → API 返回 `409 PROGRESS_REGRESSION`。

## 4. 完课判定

```go
func IsCompleted(ctx context.Context, db sqlx.QueryerContext, enrollmentID uuid.UUID) (bool, int, error) {
  var rule struct {
    Type   string `json:"type"`
    Params struct{ MinBps int } `json:"params"`
  }
  // load course_versions.completion_rule via enrollment
  // count required lessons
  // count completed (progress_bps >= MinBps) in required
  // return done iff count(completed) == count(required)
}
```

阈值用 `MinBps` 表达，可配置（如 "看完 80%" 也算）。

## 5. mint job 唯一性

```sql
INSERT INTO jobs (type, dedupe_key, payload, status, run_after)
VALUES (
  'certificate.mint',
  'cert:' || user_id || ':' || course_id || ':' || cert_version,
  jsonb_build_object('completion_id', $1, 'recipient_wallet', $2, 'certificate_id', $3, 'metadata_uri', $4),
  'pending',
  now()
) ON CONFLICT (dedupe_key) DO NOTHING;
```

`certificates(completion_id)` 唯一约束是最后一道防线。

## 6. Worker mint 流程

```text
1. jobs.poll(limit=10) FOR UPDATE SKIP LOCKED
2. payload: completion_id + recipient + certificateId + uri
3. pre-check：
   - 课程仍 published
   - 完课记录存在且未撤销
   - 未铸造过
4. signer = KMS-managed wallet（独立于 deployer）
5. tx: certificateNFT.mintCertificate(recipient, certificateId, uri)
6. 等 confirmation_depth
7. UPDATE certificates SET token_id, tx_hash, status='confirmed'
8. 失败：attempts++，run_after = now() + 2^attempts min，超过 N → DLQ + admin alert
```

## 7. metadata 生成

```text
GET /certificates/{id}/metadata
→ 生成 JSON:
{
  "name": "Course Completion Certificate",
  "description": "...",
  "image": "ipfs://.../badge.png" 或 "https://cdn.../badge-{hash}.png",
  "attributes": [
    { "trait_type": "course_id", "value": "..." },
    { "trait_type": "course_version", "value": 1 },
    { "trait_type": "issued_at", "value": "2026-..." }
  ]
}
→ 上传到 S3 私有桶 → 返回 { uri: "https://cdn.../metadata-{cid}.json" }
→ CID = sha256(JSON) hex
```

链上存 URI，metadata 不可变（CID 内容寻址）。

## 8. 前端学习播放器

- 进度上报：`progressBps = Math.floor((currentTime / duration) * 10000)`；节流 5 秒一次。
- 完成按钮：调 `/courses/{id}/complete`，展示证书铸造进度（轮询 job 状态或订阅）。
- 证书展示：用 wagmi 读 `ownerOf(tokenId)` + `tokenURI(tokenId)` 作为旁路校验（DB 是权威，但链上可作可信展示）。

## 9. 测试策略

- **合约**：CertificateNFT 单测（MINTER_ROLE、防重复、soulbound）、fuzz（recipient/certificateId 边界）。
- **API**：进度不倒退、完课判定、job 创建幂等。
- **Worker**：mint 失败重试、DLQ、metadata 完整性。
- **E2E**：登录→购买→完课→铸造→个人中心展示。

## 10. 安全检查

- [ ] `MINTER_ROLE` 由受限 KMS signer 持有，**不**用 deployer。
- [ ] signer 余额 + 频率告警；额度上限可选。
- [ ] metadata URI 内容寻址：mint 前 hash 校验，mint 后 URI 不可改。
- [ ] 撤销/更正策略：MVP 默认不可撤销；如需，单独 ADR + UI 流程。