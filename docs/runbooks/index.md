# Runbooks

> **目标读者**：on-call 工程师 / SRE / 平台 owner。
> **范围**：x-web3 production（Sepolia + AWS）。
> **约定**：所有写入操作必须走 admin API（带 SYSTEM_ADMIN + X-Confirm-Token）或
> `psql` 直连 RDS 走 IAM 临时凭证；任何 SQL 写入前先 dry-run / 列 preview。

## 索引

| 场景 | 跑哪份 |
| --- | --- |
| 链 indexer 卡住 / 漂移 / reorg 漏处理 | [chain-replay.md](./chain-replay.md) |
| 证书 mint 死循环 / DLQ 累积 / 私钥疑似泄露 | [dlq-recovery.md](./dlq-recovery.md) + [signer-rotation.md](./signer-rotation.md) |
| 改 KMS / 替换 worker deployer / 紧急吊销旧 signer | [signer-rotation.md](./signer-rotation.md) |
| RDS 数据损坏 / 误删 / 跨区事故 | [backup-restore.md](./backup-restore.md) |
| 季度演练 / 事故后补做 | [dr-drills.md](./dr-drills.md) + [dr-drills-ledger.md](./dr-drills-ledger.md) |

## 通用 preflight（每次必走）

```bash
# 1. 角色
aws sts get-caller-identity --profile xweb3-prod
# 期望：arn:aws:iam::<prod>:role/AdminAccess 之类，且 work IAM 不在 dev。
# 2. 健康探针
curl -sS https://api.x-web3.example.com/healthz
curl -sS https://api.x-web3.example.com/readyz
# 3. 关键指标
curl -sS https://api.x-web3.example.com/metrics | grep -E '^(worker_indexer|worker_chain_indexer|http_requests_total|orders_created_total)'
# 4. Worker /metrics（直连 ECS task IP 或 sidecar）
curl -sS http://<worker-task-ip>:9090/metrics | grep -E '^worker_chain_indexer'
# 5. 链同步状态
curl -sS -H "Cookie: xweb3_sid=..." \
  'https://api.x-web3.example.com/api/v1/admin/chain/sync?chainId=11155111' | jq
# 6. DLQ 概览
curl -sS -H "Cookie: xweb3_sid=..." \
  https://api.x-web3.example.com/api/v1/admin/dlq | jq '.count'
```

> 永远先看仪表盘 → 再写 SQL。**不要**直接 UPDATE 业务表（orders / certificates / chain_events）；
> 写操作必须走 admin API 或 admin 提供的封装路径，否则 audit_logs 留痕缺失。

## 二次确认（X-Confirm-Token）

所有"写入型"admin endpoint（grant / revoke / cert retry / chain rewind）要求调用
方先拿一次 5 min TTL 的 confirm token，再在 header 携带：

```bash
# 1. 获取 fresh confirm token（admin UI 模态框走的就是这条；命令行直接调内部 endpoint）
curl -sS -X POST -H "Cookie: xweb3_sid=$SID" \
  https://api.x-web3.example.com/api/v1/admin/confirm | jq -r '.token'
# 2. 携带 X-Confirm-Token 做事
curl -sS -X POST -H "Cookie: xweb3_sid=$SID" -H "X-Confirm-Token: $CT" \
  -H 'Content-Type: application/json' -d '{"chainId":11155111,"fromBlock":1234,"reason":"..."}' \
  https://api.x-web3.example.com/api/v1/admin/chain/rewind
```

缺失 / 过期 / 复用 → 403。详情见 [admin.yaml](../../packages/shared/openapi/admin.yaml)。

## 沟通话术

每个 runbook 末尾都附一段"沟通"段：在事故沟通中**必须**包含的内容。
事故从 `incident-<YYYYMMDD-HHMM>` 频道记录，所有写入操作的 audit_logs 行
都贴回频道便于事后追溯。

## 升级路径

| 严重度 | 升级 |
| --- | --- |
| SEV-3（单用户 / 单课程） | 平台 owner，30 min |
| SEV-2（多用户 / indexer 漂移） | + infra lead，15 min |
| SEV-1（关键余额 / signer 疑似泄露 / RDS 损坏） | + CTO + 安全 + 通讯组，立即 |
