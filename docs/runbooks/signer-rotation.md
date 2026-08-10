# Runbook: Signer rotation

> **触发条件**：worker KMS 密钥即将过期 / 私钥疑似泄露 / `MINTER_ROLE` 持有者
> 变更 / 切换 staging ↔ prod KMS。
> **核心原则**：永远「先加新，后撤旧」；撤销必须等「无 in-flight mint job」以后，
> 否则会有死掉的 certificate_jobs 落 DLQ。

## 0. 角色 & 路径

worker signer 适配由 `apps/worker/internal/certificate/signer.go` 装配：
- `KMS_INTEGRATION=1` → `KMSDriver`（生产 / staging）
- `SIGNER_KEYSTORE_PATH` 设置 → `KeystoreDriver`（本地 + staging 旁路）
- `SIGNER_ANVIL_PRIVATE_KEY` 设置 → `AnvilDriver`（本地 / 集成测试，**禁止 staging/prod**）

合约端 `CertificateNFT.MINTER_ROLE` 由 `init()` 授予 `initialMinter`（部署时的参数）；
后续通过 `grantRole(MINTER_ROLE, newAddr)` / `revokeRole(MINTER_ROLE, oldAddr)` 调整。
调用方 = 多签 / Safe（生产）；脚本 = `cast` 或 worker 内置的 admin cli。

## 1. 准备

```bash
# 1. 摸清新旧地址
OLD_ADDR=$(cast call $CERT_NFT "MINTER_ROLE_HAS() returns (address)" --rpc-url $SEPOLIA_RPC)
# 实际拿当前 MINTER_ROLE 持有者：访问 Etherscan (Read Contract) 或 Etherscan API
# Token holders 视图：https://sepolia.etherscan.io/token/<cert_nft_addr>#readContract
# 2. 准备 NEW_ADDR（同链新 KMS alias 的以太坊地址）
#  - KMS: 在 CloudWatch / KMS console 把 alias 的 public key 导出；
#  - 计算其 secp256k1 pubkey → eth address（KMSDriver 启动时打日志）。
NEW_ADDR="0xNEW..."
# 3. 反馈旧地址是否仍有 pending mint job
psql "$DATABASE_URL" -c "
SELECT count(*) AS pending_for_old
FROM certificate_jobs cj
JOIN certificates c ON c.id=cj.certificate_id
WHERE c.status IN ('pending','minting')"
```

> 只有当 `pending_for_old=0` 时才能 Revoke；否则先走 [dlq-recovery.md](./dlq-recovery.md)。

## 2. 启动新 signer

> 新 signer 必须先完成至少一次成功 mint（沙箱证书 / 测试 course），确认上线后再撤旧。

```bash
# 1. ECS / k8s 更新 worker config，引入 NEW signer 驱动
# staging 先演练：
aws ecs update-service --cluster xweb3-staging --service xweb3-worker \
  --force-new-deployment
# 2. 等 worker 起来，看日志里出现 `kms_alias=... signer_addr=0xNEW...`
# 3. 在 staging 跑一次端到端：买课 → 完成 → 确认证书 mint 成功
#    SHA EIP-191 / low-s / recovery-id 通过 worker_internal/certificate/integration_test.go 覆盖。
```

## 3. 合约侧加新 MINTER_ROLE

```bash
# 多签 Safe 走 UI / cast：
cast send $CERT_NFT \
  "grantRole(bytes32,address)" \
  $(cast keccak "MINTER_ROLE") \
  $NEW_ADDR \
  --rpc-url $SEPOLIA_RPC \
  --ledger \
  --sender $MULTISIG_ADDR
```

> 这一步是**安全的** — 增加新权限不会破坏现有 minter。

## 4. 把 worker 切到 NEW signer

```bash
# 1. 部署新 KMS alias / 替换 SIGNER env
# 2. rolling restart worker
aws ecs update-service --cluster xweb3-prod --service xweb3-worker \
  --force-new-deployment
# 3. 等 worker 起来后，看 metrics：
curl -sS http://<worker>:9090/metrics | grep -E '^worker_cert_jobs_(processed|succeeded)'
# 4. 强制一次端到端：API 创建一笔 completions → 完课 → admin /me/certificates 看 confirmed
```

## 5. 撤销旧 MINTER_ROLE

```bash
# 检查旧地址是否真有 in-flight
psql "$DATABASE_URL" -c "
SELECT count(*) AS still_running
FROM certificate_jobs cj
JOIN certificates c ON c.id=cj.certificate_id
WHERE c.status IN ('pending','minting')
  AND c.updated_at > now() - interval '10 minutes'"
# 必须=0
```

```bash
cast send $CERT_NFT \
  "revokeRole(bytes32,address)" \
  $(cast keccak "MINTER_ROLE") \
  $OLD_ADDR \
  --rpc-url $SEPOLIA_RPC \
  --ledger \
  --sender $MULTISIG_ADDR
```

## 6. 验证

```bash
# 1. 旧地址应不能再 mint
cast send $CERT_NFT \
  "mintCertificate(address,uint256,string)" \
  $VICTIM_WALLET 1 "ipfs://..." \
  --rpc-url $SEPOLIA_RPC \
  --private-key $OLD_PK
# 期望：revert AccessControl
# 2. 新地址仍可 mint（健康检查业务路径）
cast send $CERT_NFT \
  "mintCertificate(address,uint256,string)" \
  $VICTIM_WALLET 1 "ipfs://..." \
  --rpc-url $SEPOLIA_RPC \
  --private-key $NEW_PK
# 期望：成功
# 3. DLQ 视图无新 cert_mint dead
curl -sS -H "Cookie: xweb3_sid=$SID" \
  https://api.x-web3.example.com/api/v1/admin/dlq | jq '.items[] | select(.kind=="mint_dead")'
```

## 7. 紧急：私钥疑似泄露

> 立即执行 §5 撤销，**不**等清空 in-flight；in-flight job 走 [dlq-recovery.md](./dlq-recovery.md)。

```bash
# 0. 立刻 revoke（不等清空）
cast send $CERT_NFT "revokeRole(bytes32,address)" \
  $(cast keccak "MINTER_ROLE") $OLD_ADDR \
  --rpc-url $SEPOLIA_RPC --ledger --sender $MULTISIG_ADDR
# 1. 替换 KMS alias（必须新建，不要 rotate 现有 alias 的明文）：
#    KMS CreateAlias → 仅生成新公钥，旧公钥对应的私钥仍可能被滥用。
# 2. 走 §2..§4 完整流程；如果攻击者已经 mint 了垃圾证书，走 §8 清理。
```

## 8. 吊销垃圾证书（可选）

如果是 soulbound 不允许 transfer，但可以 **burn**：

```bash
cast send $CERT_NFT "burn(uint256)" $TOKEN_ID \
  --rpc-url $SEPOLIA_RPC --ledger --sender $MULTISIG_ADDR
# 仅 `BURNER_ROLE` 持有者可调用 — 默认 deployer，转移到多签。
```

## 9. 沟通

- 旧地址 / 新地址 / 触发原因（过期 / 泄露 / 切换环境）；
- 步骤 §3 / §5 的 Etherscan tx hash；
- staging 演练结果（cert mint 成功截图 / tx hash）；
- 全链路 audit_logs：worker 启动 log + admin 操作留痕。
