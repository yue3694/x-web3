# Runbook: DLQ recovery

> **触发条件**
> - `worker_cert_jobs_dead_letter_total` 持续 > 0
> - `GET /admin/dlq` 返回 `count > 0` 且 `kind=mint_dead` / `kind=gap`
> - 工单 / 客服反馈"证书没发下来 / 订单卡住"
>
> **核心原则**：**先看清 DLQ 内容**，再决定 replay / ignore / manual；不要直接
> `UPDATE certificate_jobs SET status='pending'`。

## 1. 看 DLQ

```bash
curl -sS -H "Cookie: xweb3_sid=$SID" \
  https://api.x-web3.example.com/api/v1/admin/dlq | jq
```

返回结构：

```json
{
  "count": 3,
  "items": [
    {
      "id": 42,
      "consumer": "cert_mint",
      "chainId": 11155111,
      "kind": "mint_dead",
      "severity": "error",
      "summary": "certificate mint exhausted retries: nonce conflict",
      "payload": {
        "jobId": "uuid",
        "certificateId": "uuid",
        "attempt": 5,
        "reason": "nonce conflict"
      },
      "retryCount": 0,
      "resolved": false,
      "createdAt": "2026-08-10T..."
    }
  ]
}
```

分类决策：

| `kind`       | 触发语义                                              | 默认动作 |
|--------------|------------------------------------------------------|----------|
| `gap`        | reconcile scanner 检测到漏块（depth ≥ confirmDepth） | replay   |
| `mint_dead`  | cert job attempt 满 5，state=dead                     | replay   |
| `reorg`      | worker 检测到 reorg                                   | manual   |
| `rpc_*`      | RPC 错误（已 retry 过）                              | ignore   |

## 2. 三种 resolution

`POST /admin/dlq/{id}/retry` 接受 `resolution`：
- `replayed` — 重放到 in-process queue / 外部队列（由 worker 端 driver 决定）；
- `ignored` — 标记为"已知，不处理"，retry_count +1；
- `manual` — 需要人工跑 SQL 或外链修复，标记后等手动。

所有路径都写 audit_logs（Action 分派到 `ActionDLQRetriedReplay` / `Ignored` / `Manual`）。

## 3. mint_dead 重放

```bash
# 1. 取 confirm token
CT=$(curl -sS -X POST -H "Cookie: xweb3_sid=$SID" \
  https://api.x-web3.example.com/api/v1/admin/confirm | jq -r '.token')
# 2. replay
curl -sS -X POST -H "Cookie: xweb3_sid=$SID" -H "X-Confirm-Token: $CT" \
  -H 'Content-Type: application/json' \
  -d '{"resolution":"replayed"}' \
  https://api.x-web3.example.com/api/v1/admin/dlq/42/retry | jq
# 3. 验证
psql "$DATABASE_URL" -c "
SELECT id, status, attempt, last_error
FROM certificate_jobs
WHERE id='<jobId>'"
# 期望：status='pending' 或 'minting'，next_retry_at 在未来几秒
```

> 注意：admin `POST /admin/certificates/{id}/retry` 是另一条等价路径，它直接
> 翻 certificates + certificate_jobs 状态；DLQ requeue 路径需要 worker 端有
> 真正的 in-process / SQS bridge（MVP 阶段仅翻状态）。生产时优先用
> `POST /admin/certificates/{id}/retry`。

## 4. gap 重放

```bash
# 1. 摸清 gap 范围
psql "$DATABASE_URL" -c "
SELECT payload
FROM dlq_events
WHERE consumer='indexer' AND kind='gap' AND resolved=false
ORDER BY created_at DESC LIMIT 5"
# 2. 走 chain-replay.md：rewind 到 gapFrom，会重新拉 logs 并应用
# 3. 等 worker 处理完后：
psql "$DATABASE_URL" -c "
UPDATE dlq_events
SET resolved=true, resolution='replayed', resolved_by=$ADMIN_USER, updated_at=now()
WHERE kind='gap' AND id=<dlq_id>"
# 然后调 admin endpoint 让 audit 留痕：
curl -sS -X POST -H "Cookie: xweb3_sid=$SID" -H "X-Confirm-Token: $CT" \
  -H 'Content-Type: application/json' -d '{"resolution":"replayed"}' \
  https://api.x-web3.example.com/api/v1/admin/dlq/<id>/retry | jq
```

## 5. ignore（已知即可）

```bash
curl -sS -X POST -H "Cookie: xweb3_sid=$SID" -H "X-Confirm-Token: $CT" \
  -H 'Content-Type: application/json' -d '{"resolution":"ignored"}' \
  https://api.x-web3.example.com/api/v1/admin/dlq/<id>/retry | jq
```

适用：旧 RPC 错误、过期事件、已经被人工处理过的孤儿。

## 6. manual（需人工）

```bash
curl -sS -X POST -H "Cookie: xweb3_sid=$SID" -H "X-Confirm-Token: $CT" \
  -H 'Content-Type: application/json' -d '{"resolution":"manual"}' \
  https://api.x-web3.example.com/api/v1/admin/dlq/<id>/retry | jq
# 标记后，紧接着人工操作：
psql "$DATABASE_URL" -c "..."  # 写明应在 runbook 文本里
```

## 7. 验证

```bash
# 1. 重放后 cert 应当 5 min 内 confirmed
psql "$DATABASE_URL" -c "
SELECT id, status, tx_hash, confirmed_at
FROM certificates
WHERE id='<certId>'"
# 2. metrics
curl -sS http://<worker>:9090/metrics | grep -E '^worker_cert_jobs_(succeeded|dead_letter)_total'
# 3. dlq_unresolved 计数
curl -sS -H "Cookie: xweb3_sid=$SID" \
  https://api.x-web3.example.com/api/v1/admin/dlq | jq '.count'
```

## 8. 边缘

- **同一 DLQ 并发 retry**：DB 唯一约束 + admin handler 的 `MarkResolved` 原子
  翻转保证幂等；先到的赢，后到的 409。
- **重放后仍 dead**：先看 `last_error`；通常是 underlying 问题（recipient 0x0
  / cert_id 越界 / 合约回滚），需要走 [signer-rotation.md](./signer-rotation.md) 或
  人工修 certificates 行（**注意** audit_logs 留痕完整）。
- **DLQ 暴涨**：先停下 worker，看 worker 日志确认错误源；不要把单个 DLQ 项
  一键 replay 全部。

## 9. 沟通

- DLQ.id / kind / summary / 决策（replay / ignore / manual）；
- 重放后的 cert_id / tx_hash / 截图；
- 关联 audit_logs 行 id。
