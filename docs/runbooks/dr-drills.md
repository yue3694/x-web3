# DR Drills — 灾难恢复演练

> **目标**：把 [runbooks](./index.md) 转化为可执行、可验收、可追责的季度演练；
> 演练不背熟流程，只验证「在压力下能否找到正确路径 + 是否留痕完整 + 沟通是否到位」。
> **节奏**：每季度一次；SEV-1 / SEV-2 真实事故发生后一周内必须补做对应场景。
> **不通过的后果**：runbook 升级为 blocking；infra / SRE lead 必须 1 个月内修通。

## 索引

| Drill | 触发 runbook | 环境 | 频率 |
| --- | --- | --- | --- |
| [D1 — RDS 恢复](#d1--rds-恢复) | [backup-restore.md](./backup-restore.md) | staging → prod（影子 RDS） | 季度 |
| [D2 — RPC 故障](#d2--rpc-故障) | [chain-replay.md §6](./chain-replay.md) | staging | 季度 |
| [D3 — Worker 崩溃](#d3--worker-崩溃) | （本文件 §3 专属） | staging | 季度 |
| [D4 — DLQ 回放](#d4--dlq-回放) | [dlq-recovery.md](./dlq-recovery.md) | staging | 季度 |
| [D5 — Signer 轮换](#d5--signer-轮换) | [signer-rotation.md](./signer-rotation.md) | staging（专用 KMS alias） | 半年 |

每次演练的最终产出：
- 在 [dr-drills-ledger.md](./dr-drills-ledger.md) 追加一行；
- 演练报告（< 2 页）发到 `#xweb3-dr-drills` 频道；
- 任何「不通过」项 1 周内开 ticket，进入下一个 sprint。

## 通用准备

```bash
# 1. 角色
aws sts get-caller-identity --profile xweb3-staging
# 2. 演练专用 incident 频道
/incident drill-d1-rds-restore-2026Q3
# 3. 时间预算
#    - 准备 / 触发：30 min
#    - 恢复 / 切换：60 min
#    - 验收 + 收尾：30 min
#    总计 2 h；超时升级 SEV-2。
# 4. preflight 必走 [index.md preflight](./index.md#通用-preflight每次必走)
```

---

## D1 — RDS 恢复

**目的**：验证 snapshot 路径 + 切流量不丢 audit_logs；PITR 至少演练一次 / 半年。

**准备**：
- 影子 RDS：`xweb3-dr-restore-<date>`，与 prod 同 instance class 但单独 subnet；
- KMS：使用 `aws/rds` 默认 key（演练用，不需要客户密钥）；
- 数据集：从最新 snapshot 还原后，注入 1 万行 `audit_logs` fake 行用于 diff。

**触发**：

```bash
# 1. 模拟"误删 orders"：删除影子 DB 上一批订单
psql "$SHADOW_DATABASE_URL" -c "
DELETE FROM orders WHERE id IN (SELECT id FROM orders ORDER BY id LIMIT 500)"
# 2. 记录 T0（删除完成时间）
date -u +'%Y-%m-%dT%H:%M:%SZ'
```

**执行**：
- 走 [backup-restore.md §3 快照恢复](./backup-restore.md#3-快照恢复snapshot-restore)；
- PITR 路径演练一次，restore-time 选 T0 之前 1 min。

**验收**：

| 验收项 | 通过条件 |
| --- | --- |
| 影子 DB 行数恢复 | `SELECT count(*) FROM orders` ≥ 删除前 - 500 |
| audit_logs 完整 | 期间任何 audit_logs 行不缺（diff 行数 == 注入数） |
| ECS service 启动 | `xweb3-api / xweb3-worker` desired=2 / 1，ready tasks ≥ 1 |
| 切流量不丢请求 | 健康探针 + metrics 在切换期间无 5xx 突增 |
| 时间 | T0 → 影子 DB ready ≤ 60 min |

**记录**：在 [dr-drills-ledger.md](./dr-drills-ledger.md) 写：
- drill=D1、staging 时间、影子 DB id、是否 PITR、每项验收结果。
- 失败项原因 + ticket id。

---

## D2 — RPC 故障

**目的**：验证 worker 能在主 RPC 不可达时切到 secondary，并在 primary 恢复后切回。

**准备**：
- staging worker 的 `WORKER_RPC_URLS` 已配两条：`https://primary-drill.invalid,https://rpc.sepolia.org`；
- 主 RPC 用 `https://primary-drill.invalid`（不可解析 / 不可达）；
- 副 RPC 真实可达。

**触发**：

```bash
# 1. 确认 staging 在用 primary（看 worker 日志中 `primary=<host>`）
# 2. 把 primary 切到 invalid（演练：起 ALB rule 屏蔽）
# 3. 或在 ECS env 里把 WORKER_RPC_URLS[0] 改成 invalid
aws ecs update-service --cluster xweb3-staging --service xweb3-worker \
  --force-new-deployment
```

**执行**：观察 5 min 内 worker 自动切到 secondary。

**验收**：

| 验收项 | 通过条件 |
| --- | --- |
| `worker_indexer_rpc_errors_total{kind="http"}` 增长 | 持续 30 s 内非 0 |
| `worker_indexer_rpc_swap_events_total` 增长 | 5 min 内至少 +1 |
| `worker_chain_indexer_rpc_available` 切到 0 再 1 | 在切换期间观察到 0 值 |
| `worker_chain_indexer_lag_blocks` 不无限增长 | 5 min 内 lag < 32 |
| worker 不重启 | 5 min 内 restart_count == 0（看 ECS `Service.TaskErrors`） |

**恢复演练**：

```bash
# 切回 primary
aws ecs update-service --cluster xweb3-staging --service xweb3-worker \
  --force-new-deployment
# 5 min 内观察：primary 切回 + rpc_available=1 + rpc_errors 收敛
```

**记录**：[dr-drills-ledger.md](./dr-drills-ledger.md) 写 D2 行 + 切换时间线截图。

---

## D3 — Worker 崩溃

**目的**：验证「worker 进程在 SendTransaction 与 WaitMined 之间崩溃」场景下，
sweepStaleMintings 能在 5 min 内把死掉的 `minting` 行复位。

**准备**：
- staging 部署一个会「崩溃」的 worker（注入 `STAGE_CRASH_AFTER_SEND=1` 演练 env）；
- 一条 dangling 证书 job：`certificates.status='minting'` 且 `started_at < now() - 5 minutes`。

**触发**：

```bash
# 1. 制造 dangling 行
psql "$DATABASE_URL" -c "
UPDATE certificates SET status='minting', started_at=now() - interval '10 minutes',
       last_error='drill: forced crash simulation'
WHERE id IN (SELECT id FROM certificates WHERE status='pending' LIMIT 1)"
# 2. 启动会崩溃的 worker
STAGE_CRASH_AFTER_SEND=1 \
  aws ecs update-service --cluster xweb3-staging --service xweb3-worker-drill \
    --force-new-deployment
```

**执行**：观察 5 min。

**验收**：

| 验收项 | 通过条件 |
| --- | --- |
| `certificate_jobs.status` 翻回 `pending` | 5 min 内 `sweepStaleMintings` 触达该行 |
| `next_retry_at` 立即可消费 | ≤ now() + 5 s |
| `worker_cert_jobs_processed_total` 增长 | 同一 job 再次进入 hot path |
| 演练 worker 被 normal worker 接续 | 普通 worker `succeeded_total` 增长，无新 dead |
| 无双重 mint | `certificates` 终态唯一（确认完只一次） |

**记录**：[dr-drills-ledger.md](./dr-drills-ledger.md) 写 D3 行 + 任务接力截图。

---

## D4 — DLQ 回放

**目的**：验证三种 DLQ resolution（replayed / ignored / manual）流程 + audit 留痕。

**准备**：
- 在 staging 注入 6 条 DLQ 行：3 条 `mint_dead`、2 条 `gap`、1 条 `rpc_*`；
- 标记其中 2 条 `mint_dead` 用 `replayed`、1 条用 `ignored`、剩余用 `manual`（带修数据 SQL）。

**触发**：

```bash
# 注入 staging DLQ
psql "$DATABASE_URL" -c "
INSERT INTO dlq_events(consumer, kind, severity, summary, payload, retry_count)
VALUES
  ('cert_mint','mint_dead','error','drill mint dead 1','{\"jobId\":\"00000000-0000-0000-0000-000000000001\"}'::jsonb,0),
  ('cert_mint','mint_dead','error','drill mint dead 2','{\"jobId\":\"00000000-0000-0000-0000-000000000002\"}'::jsonb,0),
  ('cert_mint','mint_dead','error','drill mint dead 3','{\"jobId\":\"00000000-0000-0000-0000-000000000003\"}'::jsonb,0),
  ('indexer','gap','error','drill gap 1','{\"gapFrom\":1,\"gapTo\":10}'::jsonb,0),
  ('indexer','gap','error','drill gap 2','{\"gapFrom\":11,\"gapTo\":20}'::jsonb,0),
  ('indexer','rpc_*','warn','drill rpc 1','{}'::jsonb,0)"
```

**执行**：按 [dlq-recovery.md §3-§6](./dlq-recovery.md) 跑完三种 resolution，每条
都走 admin endpoint + X-Confirm-Token。

**验收**：

| 验收项 | 通过条件 |
| --- | --- |
| `replayed` 路径 | DLQ 行 `resolved=true` 且 `retry_count=1` |
| `ignored` 路径 | DLQ 行 `resolved=true` + 标注 `resolution='ignored'` |
| `manual` 路径 | DLQ 行 `resolved=true` + 人工修数据 SQL 已记录在频道 |
| audit_logs | 6 条 DLQ 操作每条都有一条 audit row（Action 按 resolution 分派） |
| cert 重放 | `replayed` 的 mint_dead 对应 certificate 在 5 min 内 confirmed |
| gap 闭合 | `replayed` 的 gap 对应 reorg 路径走通；`chain_reorgs` 多 1 行 |

**记录**：[dr-drills-ledger.md](./dr-drills-ledger.md) 写 D4 行 + 每条 DLQ 决策。

---

## D5 — Signer 轮换

**目的**：验证「先加新 MINTER_ROLE、后撤旧」+ KMS alias 切换不出现死 job。

**准备**：
- 在 KMS 起两个 alias：`xweb3/drill/old` + `xweb3/drill/new`；
- staging worker 先用 `xweb3/drill/old`；
- 部署一份 `xweb3-drill-worker` 跑 `xweb3/drill/new`（临时 ECS service）。

**触发**：

```bash
# 1. 启动 new worker
aws ecs update-service --cluster xweb3-staging --service xweb3-drill-worker \
  --desired-count 1
# 2. 在合约上加 new MINTER_ROLE（Safe UI 演练版）
cast send $CERT_NFT \
  "grantRole(bytes32,address)" \
  $(cast keccak "MINTER_ROLE") \
  $NEW_ADDR \
  --rpc-url $SEPOLIA_RPC --private-key $STAGING_DEPLOYER
# 3. 让 staging 主 worker 切到 new KMS alias
aws ecs update-service --cluster xweb3-staging --service xweb3-worker \
  --force-new-deployment
# 4. 等 in-flight 清空
psql "$DATABASE_URL" -c "
SELECT count(*) FROM certificate_jobs
WHERE status IN ('pending','minting')"
# 5. revoke 旧 MINTER_ROLE
cast send $CERT_NFT \
  "revokeRole(bytes32,address)" \
  $(cast keccak "MINTER_ROLE") \
  $OLD_ADDR \
  --rpc-url $SEPOLIA_RPC --private-key $STAGING_DEPLOYER
```

**验收**：

| 验收项 | 通过条件 |
| --- | --- |
| 旧地址 mint 失败 | `cast send ... mintCertificate` revert AccessControl |
| 新地址 mint 成功 | staging 端到端：完课 → cert confirmed |
| 无 in-flight 死 | revoke 后无 mint_dead DLQ 增长 |
| KMS 端到端 | worker 启动日志中 `kms_alias=xweb3/drill/new` 出现 ≥ 1 次 |
| audit_logs 完整 | grantRole / revokeRole 都通过 admin log（手动 cast 需手动写 log） |

**记录**：[dr-drills-ledger.md](./dr-drills-ledger.md) 写 D5 行 + Etherscan tx hash。

---

## 通用收尾

```bash
# 1. 清理演练数据
psql "$DATABASE_URL" -c "
DELETE FROM audit_logs WHERE payload->>'drill' = 'true';
DELETE FROM dlq_events WHERE summary LIKE 'drill %';"
# 2. 恢复 staging
aws ecs update-service --cluster xweb3-staging --service xweb3-worker --desired-count 1
# 3. 在 ledger 写最终结论
# 4. 演练报告贴 #xweb3-dr-drills，2 工作日内 review
```

## 升级到生产事故

任何 DR 演练发现的问题，应当在 1 周内：

1. 开 ticket（标题 `[DR-DRILL] D<n> - <一句话>`）；
2. 关联到 runbook 对应章节；
3. 在 sprint review 上报；
4. 修复完成前，相关 runbook 段标记 ⚠️ 「演练不通过」。

> DR 演练不是「仪式」。SEV-1 真实事故的恢复时间（RTO）= 上次成功演练的时间 + 1 个
> sprint。如果 3 个月内没有成功演练，把所有 infra 变更 hold 住，先补演练。
