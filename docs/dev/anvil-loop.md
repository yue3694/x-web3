# Anvil 最小闭环 E2E 指南

> 阶段 A 收尾：把 worker / api / web / 合约在本地 Anvil（chain id 31337）跑通
> happy path，**不需要 Sepolia ETH**，便于 PR 自测 + 演示。

## 0. 前置

```bash
# 1) PostgreSQL + Redis + Anvil
docker compose -f deploy/docker-compose.yml up -d redis anvil postgres

# 2) 数据库 migrate
cd database && ./migrate.sh up && cd ..

# 3) 复制根 .env.example → .env，填必要字段
cp .env.example .env
#   - ANVIL_RPC_URL=http://localhost:8545
#   - WORKER_RPC_URLS=http://localhost:8545
#   - SIGNER_DRIVER=anvil
#   - CERT_NFT_ADDRESS=0x...（见步骤 2）
```

## 1. 起 Anvil

```bash
anvil --chain-id 31337 --block-time 1
# 默认账户 #0：0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266
# 私钥（**仅本地开发**）：0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
```

## 2. 部署合约到 Anvil

```bash
# CourseMarket（学生买课入口）
cd packages/contracts
SIGNER_DRIVER=anvil CERT_NFT_ADDRESS=... PAYMENT_TOKEN_ADDRESS=... \
  forge script script/DeployCourseMarket.s.sol:DeployCourseMarket \
    --rpc-url http://localhost:8545 --broadcast
cd -

# CertificateNFT（证书 NFT）
SIGNER_DRIVER=anvil CERT_NFT_ADDRESS=... \
  forge script script/DeployCertificateNFT.s.sol:DeployCertificateNFT \
    --rpc-url http://localhost:8545 --broadcast
```

记下输出里的 `CourseMarket deployed at: 0x...` 与 `CertificateNFT deployed at: 0x...`，
填到 `.env` 的 `VITE_COURSE_MARKET_ADDRESS` / `CERT_NFT_ADDRESS`。

## 3. 起 worker / api / web

```bash
# 三终端独立跑（或用 tmux）
cd apps/worker && go run ./cmd/worker   # 终端 1
cd apps/api && go run ./cmd/api          # 终端 2
cd apps/web && pnpm dev                  # 终端 3
```

worker 启动日志应包含：

```text
cert_consumer_initialized signerDriver=anvil contract=0x... confirmDepth=12
treasury_monitor_disabled_empty hint="设置 ..."
```

## 4. Happy Path

1. 浏览器开 http://localhost:5173，登录（Privy dev stub 已开）
2. 选课 → 点 Buy → 钱包签名 `buyCourse(bytes32 courseKey, uint256 amount, bytes16 intentId)`
3. 等 5–10 秒（worker 拉到 CoursePurchased log → confirmer.Apply → enrollments INSERT）
4. 回到 My Orders → order.status = `confirmed`，enrollmentId 可见

## 5. 数据库断言

```sql
-- 订单 + 报名都落库
SELECT id, status, tx_hash FROM orders ORDER BY created_at DESC LIMIT 1;
-- 期望：1 行，status=confirmed，tx_hash=0x...

SELECT id, source FROM enrollments ORDER BY created_at DESC LIMIT 1;
-- 期望：1 行，source='order'

SELECT id, aggregate, type FROM outbox_events ORDER BY created_at DESC LIMIT 1;
-- 期望：1 行，type='order.confirmed'
```

## 6. 链上断言

```bash
# 课程是否在 CourseMarket 里
cast call 0xCourseMarketAddress "courses(bytes32)(address,uint256,uint256)" \
  0xCourseKeyHex --rpc-url http://localhost:8545

# 证书是否 mint
cast call 0xCertNFTAddress "balanceOf(address)(uint256)" \
  0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 \
  --rpc-url http://localhost:8545
# 期望：连续 5 张证书 → balanceOf = 5
```

## 7. Prometheus 指标

```bash
curl -s http://localhost:9090/metrics | grep worker_
```

期望见到的指标：

- `worker_chain_indexer_logs_decoded_total > 0`
- `worker_cert_consumer_succeeded_total > 0`
- `worker_treasury_alerts_total{...}`（仅当 TREASURY_ADDRESSES 配了之后才出现）

## 8. 常见故障

| 现象 | 排查 |
|---|---|
| worker 卡在「rpc_dial_failed」 | `WORKER_RPC_URLS` 是否指向 anvil（`http://localhost:8545`） |
| order.status 一直 `submitted` | 1) `indexerLogDecoder.Decode` 是否把事件 Apply？2) topic0 是否匹配：`chain.CoursePurchasedTopic = keccak256("CoursePurchased(bytes32,address,address,uint256,bytes16,uint256)")` |
| order.status `failed`，failure_code=`RECEIPT_MISMATCH` | courseKey / token / amount / intentId / priceVersion 任意一个与 DB intent 不一致。常见原因：priceVersion 改了但 API 还没重发 intent |
| CertConsumer 没起来 | log 应有 `cert_consumer_disabled_no_signer_driver` 或 `_no_contract`，对应填 SIGNER_DRIVER / CERT_NFT_ADDRESS |
| 连续 2 张证书第 2 张 mint 失败 | ChainTxParams 没装（PR-A4 之前的版本，nonce=0） |
| 余额告警没出 | TreasuryMonitor 默认 short-circuit：空配置 log `treasury_monitor_disabled_empty` 是预期的 |
| reorg 测试后状态错乱 | `sweepStaleMintings` 5min 兜底；如想立刻复跑，`UPDATE certificate_jobs SET status='pending'` |

## 9. 性能冒烟

```bash
# 连铸 5 张 cert，验证 nonce 连续
for i in 1..5; do ... ; done
cast block 0xCertNFTAddress latest --rpc-url http://localhost:8545
```

`miner_getLogs` 在 anvil 上 limit=10000，CI 跑大批量 mint 注意分批。

## 10. 下一步（阶段 B 预告）

- KMS signer driver（`SIGNER_DRIVER=kms` + `SIGNER_KMS_KEY_ID`）真实接入
- WS head 订阅优化 + reorg 深度回滚
- CourseKey 算法升级（sha256 → keccak256）需先在合约端加 `courseKey = keccak256(abi.encodePacked(courseId))` 校验
- TreasuryMonitor YD ERC20 真接入（需要先部署 YDToken 合约）
- 多链 / 多 RPC failover（RPCPool 现有 swap 逻辑已就绪）
