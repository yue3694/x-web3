# F04 — 学习与证书（Learning & Certificate）

> 来源：上级 `requirements.md` F-024 ~ F-030；本特性在 monorepo 中的实现切片。

## 1. 范围

- 只有 confirmed enrollment 可访问受保护课程与写入进度。
- 学习进度幂等、不倒退；服务端记录完成时间。
- 完课规则由服务端按必修课时计算（阈值 + 版本）。
- 完课后创建唯一证书铸造任务；Worker 用受限 MINTER_ROLE 铸造 ERC-721。
- 个人中心展示 enrollment / 订单 / 进度 / 链上证书。

## 2. 功能需求

| ID | 描述 | 验收 |
|---|---|---|
| **R-LC-001** | 只有 confirmed enrollment 可读受保护课程内容与写进度 | AC-011 |
| **R-LC-002** | 进度 `(enrollment_id, lesson_id)` 唯一，幂等更新不倒退；完成时间由服务端记录 | AC-011 |
| **R-LC-003** | 完课规则由服务端按必修课时计算，阈值可配置带版本 | AC-011 |
| **R-LC-004** | 完课后创建唯一证书铸造任务；`(user, course, cert_version)` 唯一 | AC-011、AC-012 |
| **R-LC-005** | CertificateNFT 仅允许 `MINTER_ROLE` 铸造；`certificateId`（bytes32）唯一 | AC-012 |
| **R-LC-006** | Worker 校验资格 → mint → 等确认 → 保存 chain / tx_hash / token_id | AC-012 |
| **R-LC-007** | 个人中心展示 enrollment / 订单 / 进度 / 链上证书 | E2E |

## 3. 数据模型

```sql
enrollments(id, user_id, course_id, source_order_id, status, granted_at, unique(user_id, course_id))
lesson_progress(id, enrollment_id, lesson_id, progress_bps int, completed_at nullable, version int, unique(enrollment_id, lesson_id))
course_completions(id, enrollment_id, rule_version, completed_at, unique(enrollment_id, rule_version))
certificates(id, completion_id unique, recipient_wallet, metadata_uri, status, chain_id, tx_hash nullable, token_id nullable, attempts int, last_error)
jobs(id, type, dedupe_key unique, payload JSONB, status, attempts, run_after, last_error)
```

## 4. 完课规则

```sql
course_versions.completion_rule = {
  "type": "all_required_lessons",   -- MVP 唯一规则
  "version": 1,
  "params": { "min_completion_bps": 10000 }
}
```

服务端计算：

```text
required = lessons WHERE required = true AND chapter IN course_version.chapters
completed = lesson_progress WHERE enrollment_id = ? AND lesson_id IN required AND progress_bps = 10000
done iff count(completed) == count(required)
```

## 5. 证书铸造

```solidity
interface ICertificateNFT {
  function mintCertificate(address recipient, bytes32 certificateId, string uri) external;
  event CertificateMinted(bytes32 indexed certificateId, address indexed recipient, uint256 tokenId);
}
```

- `certificateId = keccak256("cert:" || user_id || ":" || course_id || ":" || cert_version)`
- mapping `certificates(bytes32) → bool` 防重复铸造。
- 默认 `soulbound = true`（`transferFrom` revert）— 待 ADR 决定。
- metadata URI 走 IPFS / Arweave（MVP 用 S3 静态托管 + 内容寻址 hash）。

## 6. 端到端流程

```text
1. POST /lessons/{id}/progress { progressBps } 
   → API 检查 enrollment、计算完成度、写 lesson_progress
2. POST /courses/{id}/complete
   → 计算完成；满足则 tx: course_completions + INSERT jobs(dedupe_key=...)
3. Worker (apps/worker):
   a. 抢 job（SELECT FOR UPDATE SKIP LOCKED）
   b. 校验资格 + 拉 metadata
   c. mintCertificate(...)
   d. 等 confirmation → UPDATE certificates SET token_id, status=confirmed
   e. 失败：指数退避；attempts > N → DLQ
```

## 7. 错误码

| code | http | 含义 |
|---|---|---|
| `NOT_ENROLLED` | 403 | 无有效 enrollment |
| `PROGRESS_REGRESSION` | 409 | 进度倒退 |
| `ALREADY_COMPLETED` | 409 | 已完课，重复请求幂等返回 |
| `MINT_NOT_AUTHORIZED` | 422 | Worker signer 非 MINTER_ROLE |
| `CERTIFICATE_DUPLICATE` | 409 | 同 (user, course, cert_version) 已铸造 |

## 8. 非功能需求

- 写进度 P95 ≤ 100 ms。
- 完课判定 P95 ≤ 300 ms（课程 100 课时内）。
- mint 端到端（含 12 块确认）P95 ≤ 60 s。

## 9. 边界

- **依赖**：F01（RBAC）、F02（课程版本/lesson）、F03（enrollment）、F05（若证书含 token URI 引用 YD 储备等可省）。
- **被依赖**：F06（admin 证书重试）。