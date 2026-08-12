# Runbook: Chain replay / rewind

> **触发条件**：worker indexer 滞后 / reorg 漏处理 / `chain_events.block_number`
> 与 `chain_checkpoints.next_block` 错位 / 业务侧反馈订单状态不正确。
> **手段**：优先走 admin API `POST /admin/chain/rewind`；**严禁**直接 `UPDATE chain_checkpoints`。

## 1. 评估

```bash
# 1. 看监控
watch -n 5 'curl -sS http://<worker>:9090/metrics | grep -E "worker_chain_indexer_(lag_blocks|next_block|head_block|rpc_available)"'
# 2. 看 admin sync endpoint
curl -sS -H "Cookie: xweb3_sid=$SID" \
  'https://api.x-web3.example.com/api/v1/admin/chain/sync?chainId=11155111' | jq
# 3. 看 DLQ 增量
curl -sS -H "Cookie: xweb3_sid=$SID" \
  https://api.x-web3.example.com/api/v1/admin/dlq | jq '.items[] | select(.kind=="gap")'
```

判定：
- `lag_blocks` 持续 > 64 且 `rpc_available=1` → RPC 没事但 indexer 卡在某 range；先看
  worker 日志找具体 range。
- `lag_blocks` 持续 > 64 且 `rpc_available=0` → 切 RPC（见 §6）；**不要** rewind。
- 检测到深 reorg（> 20 blocks）→ 必须 rewind 到 common block。

## 2. 找 common block

```bash
# 列出最近 200 块 hash 的本地入库（worker chain_events）
psql "$DATABASE_URL" -c "
SELECT block_number, block_hash, canonical
FROM chain_events
WHERE chain_id=11155111
ORDER BY block_number DESC
LIMIT 200"
```

然后在 Sepolia Etherscan 上比对最近一段：

```text
https://sepolia.etherscan.io/block/<blockNumber>
```

找到"worker 视角"与"链上视角"最新的共有块 `commonBlock`（通常选高度差 ≤ 12 的连续段起点）。

## 3. 二次确认 + rewind

```bash
# 1. confirm token
CT=$(curl -sS -X POST -H "Cookie: xweb3_sid=$SID" \
  https://api.x-web3.example.com/api/v1/admin/confirm | jq -r '.token')
# 2. rewind
curl -sS -X POST -H "Cookie: xweb3_sid=$SID" -H "X-Confirm-Token: $CT" \
  -H 'Content-Type: application/json' \
  -d "{\"chainId\":11155111,\"fromBlock\":${COMMON_BLOCK},\"reason\":\"incident #<id> reorg after deploy\"}" \
  https://api.x-web3.example.com/api/v1/admin/chain/rewind | jq
```

返回：

```json
{
  "chainId": 11155111,
  "fromBlock": 12345678,
  "orphanedEvents": 12,
  "affectedOrders": 4,
  "rewoundAt": "2026-08-10T..."
}
```

> 该 path 在事务内：
> 1. `SELECT ... FOR UPDATE` 锁 `(chain_id, 'indexer')` 行 — 与 worker 互斥；
> 2. `chain_events` ≥ fromBlock 标 `canonical=false`；
> 3. `orders` ≥ fromBlock 且 confirming/confirmed 标 `reorged`；
> 4. 写 `chain_reorgs` 留痕；
> 5. `chain_checkpoints.next_block = fromBlock`, `last_block_hash = NULL`。
>
> worker indexer 下次 cycle 会从 `fromBlock` 重新拉，期间应用 `last_block_hash` 校验
> 触发 reorg 检测（与自动 reorg 路径一致）。

## 4. 验证

```bash
# 1. 看 lag
sleep 30
curl -sS http://<worker>:9090/metrics | grep -E '^worker_chain_indexer_(lag_blocks|next_block|head_block)'
# 2. 看 reorg counter 增长
curl -sS http://<worker>:9090/metrics | grep -E '^worker_indexer_reorgs_total'
# 3. 抽查一个被标记 reorged 的 order
psql "$DATABASE_URL" -c "
SELECT id, status, failure_code, block_number
FROM orders
WHERE chain_id=11155111 AND status='reorged'
ORDER BY updated_at DESC LIMIT 5"
# 4. admin sync 状态
curl -sS -H "Cookie: xweb3_sid=$SID" \
  'https://api.x-web3.example.com/api/v1/admin/chain/sync?chainId=11155111' | jq
```

期望：`lag_blocks` 收敛下降、`worker_indexer_reorgs_total` 略增、`rpc_available=1`、被 reorged 的 order 不再被重放。

## 5. 边缘 case

- **被重放的 order 状态错乱**：order 处于 `reorged` 后，**不会**再被自动 replay；
  如确需重新 mint，只能在前端手动重发行程（这部分业务尚未提供 admin UI）。
- **多链事故**：每条链独立 rewind；不要在事务内跨链（API 不支持，DLQ 也不会跨链）。
- **rewind 期间 worker 仍在跑**：事务锁确保互斥；不要 kill worker。
- **「rewind 0」误操作**：API 拒绝 `fromBlock < 0`，但 `fromBlock=0` 是合法 ——
  等价于「全量重放」，仅在全新链初始化时使用。

## 6. RPC 切换（不 rewind）

如果 `rpc_available=0`，先切 RPC：

```bash
# 在 ECS task environment 里更新 WORKER_RPC_URLS（aliyun / Alchemy 优先），再 rolling restart。
aws ecs update-service --cluster xweb3-prod --service xweb3-worker \
  --force-new-deployment \
  --query 'service.deployments[0].status'
```

切 RPC 后观察 5 min：`worker_indexer_rpc_errors_total`、`rpc_available`、lag 三项必须改善；
若仍 lag → 重新走 §1。

## 7. 沟通

事故沟通频道 `incident-<YYYYMMDD-HHMM>` 必填：

- `chain_id` / `fromBlock` / `expectedCommonBlock` / `actualCommonBlock`；
- `orphanedEvents` / `affectedOrders` 数字；
- 触发根因（reorg / RPC 故障 / deploy 漏改 / 0）；
- rewind 后的 `next_block` / `lag_blocks` 收敛曲线截图；
- 关联 `audit_logs` 行 id（admin rewind 必留痕）。
