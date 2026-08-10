# Runbook: RDS backup & restore

> **触发条件**：DB 数据损坏 / 误删 / 跨区事故 / 应急演练。
> **核心原则**：先 snapshot，再回切；恢复期间 **必须** 停 worker（避免 worker
> 写入与还原数据竞争）。

## 0. 资产

- **RDS**：Multi-AZ，启用 PITR（Point-in-Time Recovery）默认 35 天。
- **Snapshot**：每日 02:00 UTC 自动 snapshot，保留 30 天。
- **加密**：KMS `aws/rds` 默认密钥；不能跨账户分享。
- **网络**：DB 私有 subnet；跳板机通过 SSM Session Manager 接入。

## 1. 准备

```bash
# 1. 拿到 RDS 标识
aws rds describe-db-instances --db-instance-identifier xweb3-prod \
  --query 'DBInstances[0].{Endpoint:Endpoint.Address,MultiAZ:MultiAZ,Engine:Engine,Version:EngineVersion}'
# 2. 找到最近的可用 snapshot
aws rds describe-db-snapshots --db-instance-identifier xweb3-prod \
  --snapshot-type automated \
  --query 'reverse(sort_by(DBSnapshots,&SnapshotCreateTime))[:5].{Id:DBSnapshotIdentifier,Time:SnapshotCreateTime,Status:Status}'
# 3. 看 worker / api 是否在写
watch -n 2 'psql "$DATABASE_URL" -c "
SELECT count(*) AS active
FROM pg_stat_activity
WHERE state=\x27active\x27 AND datname=current_database()
  AND application_name <> \x27psql\x27
  AND xact_start > now() - interval \x2730 seconds\x27"'
```

## 2. 停服务

```bash
# 1. 关 ECS service（前面）→ 不再写 DB
aws ecs update-service --cluster xweb3-prod --service xweb3-api \
  --desired-count 0
aws ecs update-service --cluster xweb3-prod --service xweb3-worker \
  --desired-count 0
# 2. 等 in-flight 写完（30s 通常足够）
sleep 30
# 3. 验证无活跃连接
psql "$DATABASE_URL" -c "
SELECT count(*) FROM pg_stat_activity
WHERE state='active' AND datname=current_database()
  AND application_name <> 'psql'"
# 期望：=0
```

## 3. 快照恢复（snapshot restore）

```bash
# 1. 从 snapshot 起新实例（不要 overwrite 原实例；改名 -restored suffix）
aws rds restore-db-instance-from-db-snapshot \
  --db-instance-identifier xweb3-prod-restored \
  --db-snapshot-identifier rds:xweb3-prod-2026-08-10-02-00 \
  --db-instance-class db.r6g.large \
  --multi-az \
  --no-publicly-accessible \
  --db-subnet-group-name xweb3-prod \
  --vpc-security-group-ids sg-xxxxxxxx \
  --kms-key-id alias/aws/rds
# 2. 等 status=available
aws rds wait db-instance-available --db-instance-identifier xweb3-prod-restored
# 3. 验证行数 / 表完整
NEW_URL=$(aws rds describe-db-instances --db-instance-identifier xweb3-prod-restored \
  --query 'DBInstances[0].Endpoint.Address' --output text)
psql "postgres://xweb3:$PWD@${NEW_URL}:5432/xweb3" -c "
SELECT
  (SELECT count(*) FROM users)        AS users,
  (SELECT count(*) FROM orders)       AS orders,
  (SELECT count(*) FROM certificates) AS certs,
  (SELECT count(*) FROM enrollments) AS enr"
```

## 4. PITR（精确时间点）

仅在「OperationalError 中包含 audit_logs / chain_events 单点误改」场景下使用：
snapshot 是日粒度，PITR 是秒级。

```bash
# 1. 恢复到一个新实例，PITR 到误操作前 1 min
aws rds restore-db-instance-to-point-in-time \
  --source-db-instance-identifier xweb3-prod \
  --target-db-instance-identifier xweb3-prod-pitr \
  --restore-time 2026-08-10T03:29:00Z \
  --no-publicly-accessible \
  --db-subnet-group-name xweb3-prod \
  --vpc-security-group-ids sg-xxxxxxxx
aws rds wait db-instance-available --db-instance-identifier xweb3-prod-pitr
# 2. 验证
NEW_URL=$(aws rds describe-db-instances --db-instance-identifier xweb3-prod-pitr \
  --query 'DBInstances[0].Endpoint.Address' --output text)
psql "postgres://xweb3:$PWD@${NEW_URL}:5432/xweb3" -c "
-- 抽查 audit_logs 在那个时间点前后的内容
SELECT id, action, created_at, payload
FROM audit_logs
WHERE created_at BETWEEN '2026-08-10 03:28:00' AND '2026-08-10 03:30:00'
ORDER BY id DESC LIMIT 20"
```

## 5. 切流量

```bash
# 1. 在 Secrets Manager 切换 DATABASE_URL
aws secretsmanager update-secret --secret-id xweb3-prod/database-url \
  --secret-string "postgres://xweb3:$PWD@${NEW_URL}:5432/xweb3?sslmode=require"
# 2. 重启 ECS service
aws ecs update-service --cluster xweb3-prod --service xweb3-api --desired-count 2
aws ecs update-service --cluster xweb3-prod --service xweb3-worker --desired-count 1
# 3. 验证
curl -sS https://api.x-web3.example.com/readyz
psql "$DATABASE_URL" -c "SELECT now()"  # 重新载入新值
```

## 6. 旧实例处理

> **不要**立即删旧的。保留 24h 让团队复盘；之后 `delete-db-instance`
> + `skip-final-snapshot`（已在新实例上验证）。

```bash
aws rds delete-db-instance \
  --db-instance-identifier xweb3-prod \
  --skip-final-snapshot
```

## 7. 已知陷阱

- **跨区事故**：default subnet 在单 region；切 region 需新起 VPC peering / TGW
  （infra 阶段 F06-T11 之后才完善）。
- **RLS / 角色**：RDS 角色 (deployer / migrator / report) 在 PITR 副本上**保留**；
  不要用 master 密码连 application。
- **worker 持有旧 chain_checkpoints 状态**：恢复后 worker 会从 `next_block`
  继续；只有当 `next_block` 已经原链高度后才安全首发。PITR 必须确认
  `chain_checkpoints.next_block` ≤ 当前链头高度。
- **audit_logs 缺失**：恢复后 audit_logs 反映的是「恢复时刻」的视图；写入
  `audit_logs` 时打 `recovered_at` 标签便于排查旧 vs 新混用。
- **infra 阶段 RDS 加密 / KMS 分离**：F06-T13 之后会拆出 `xweb3/rds` 客户密钥；
  当前先用 `aws/rds` 即可。

## 8. 演练流程

每季度一次：

- 1. 申请测试 DB（独立 region / 独立 stack）；
- 2. 注入 seed → 跑 RW 混合 5 min；
- 3. 模拟 snapshot 恢复（§3）；
- 4. 验证 row counts / FK / index；
- 5. 演练记录贴 incident 频道 + 改本文档的"已知陷阱"段。

## 9. 沟通

- DB 实例标识 / 误操作时间 / 选 snapshot or PITR / 目标时间点；
- 切换前 ECS service 状态截图（desired=0、in-flight=0）；
- 切流量后 readiness probe 截图；
- 关联 audit_logs：worker / admin 在恢复期间的所有动作。
