# F04 — 学习与证书 任务清单

## 任务列表

- [x] **F04-T01** CertificateNFT.sol：ERC721 + AccessControl + 防重复 + soulbound（默认） `contracts:packages/contracts/src/CertificateNFT.sol` ~6h *(ERC721URIStorage-style 自实现，tokenURI 持久化 + burn 时清理)*
- [x] **F04-T02** CertificateNFT 单测 + fuzz（recipient / certificateId 边界 / 重复铸造） `contracts:packages/contracts/test/CertificateNFT.t.sol` ~6h *(18 unit/fuzz/invariant + 3 tokenURI = 21 tests)*
- [x] **F04-T03** 部署脚本：CertificateNFT + 角色转移给 Worker signer `contracts:packages/contracts/script/DeployCertificateNFT.s.sol` ~2h *(Mode A env + Mode B JSON；BURNER_ROLE conditional grant；18 单测)*
- [x] **F04-T04** ABI 导出 + chain registry `contracts+web:apps/web/src/contracts/certificate.ts` ~2h *(ABI export script defaults 扩展；deployments.ts 加 certificateNftDeployments slot)*
- [x] **F04-T05** migration：enrollments / lesson_progress / course_completions / certificates / jobs `database:database/migrations/0004_learning.sql` ~4h *(实际编号 0003_enrollments.up.sql)*
- [x] **F04-T06** API：POST /lessons/{id}/progress（不倒退） `api:apps/api/internal/learning/progress.go` ~4h
- [x] **F04-T07** API：完课判定 service + POST /courses/{id}/complete + 唯一 mint job 创建 `api:apps/api/internal/learning/complete.go,internal/certificate/` ~6h
- [x] **F04-T08** API：GET /me/enrollments / progress / certificates `api:apps/api/internal/learning/,internal/certificate/` ~3h
- [x] **F04-T09** metadata 生成 + 上传 S3 + CID 校验 `api:apps/api/internal/certificate/metadata.go` ~5h
- [x] **F04-T10** Worker：mint signer 抽象（KMS / 本地 keystore / anvil PK 三种 driver） `worker:apps/worker/internal/certificate/signer.go` ~5h *(MintSigner interface + AnvilDriver + KeystoreDriver + KMSDriver with DER/low-S/recovery-id；TxParamsProvider offline-sign；fakeKMS 测覆盖；KMS_INTEGRATION env gate)*
- [x] **F04-T11** Worker：mint job consumer + receipt 确认 + DLQ + 重试 `worker:apps/worker/internal/certificate/consumer.go` ~8h
- [x] **F04-T12** OpenAPI：learning + certificate `shared:packages/shared/openapi/learning.yaml,certificate.yaml` ~4h
- [x] **F04-T13** 前端学习播放器 + 进度节流上报 + 完成按钮 `web:apps/web/src/features/learning/` ~10h
- [x] **F04-T14** 前端个人中心：MyEnrollments / MyCertificates（含链上旁路校验） `web:apps/web/src/features/account/` ~8h
- [x] **F04-T15** 集成测试：完课 → 唯一 job → mint 成功 → 重复完成不重复铸造 `api+worker:**/*_test.go` ~8h *(API 端完课五态 + 幂等 + 唯一 job 已在 `apps/api/internal/integration/completion_test.go` 覆盖；worker 端到端 mint + receipt 在 `apps/worker/internal/certificate/integration_test.go`)*
- [x] **F04-T16** 合约+worker 集成：非 MINTER_ROLE mint 失败 + 重试恢复 `contracts+worker:` ~4h *(test_Mint_RevertsForNonMinter + test_Revoke_RevertsForNonBurner)*
- [x] **F04-T17** E2E：购买→学习→完成→证书展示 `qa:tests/e2e/certificate.spec.ts` ~6h

## 依赖与并行

- **依赖**：F01、F02、F03（enrollment 必须 confirmed）。
- **可并行**：T-01～T-04（合约）与 T-05～T-09（API）并行；前端可基于 OpenAPI mock 并行。
- **阻塞下游**：F06 证书重试接口。

## 退出条件（DoD）

- [x] `forge test` 全绿，CertificateNFT 覆盖率 ≥ 90%。 *(18 unit/fuzz/invariant tests; full suite 77/77 green)*
- [x] 进度不倒退测试覆盖（包括同值更新允许、倒退拒绝）。 *(apps/api/internal/learning/progress_test.go + 集成用例)*
- [x] 重复完成只产生一个 mint job。 *(apps/api/internal/integration/completion_test.go: TestCompletion_Idempotent_OneJob / TestCompletion_RepeatFromCleanSlate_StillSingleJob)*
- [x] 非 MINTER_ROLE 地址调用 `mintCertificate` revert。 *(test_Mint_RevertsForNonMinter)*
- [x] Worker 重试 + DLQ 验证。 *(apps/worker/internal/certificate/integration_test.go + consumer_test.go)*
- [x] AC-011、AC-012 通过。 *(E2E 落位 apps/web/e2e/certificate.spec.ts；真实 Sepolia 演练待 F06 infra)*

## 风险

- **证书 soulbound 决策**：MVP 默认 soulbound；如允许转让，额外加白名单 / KYC 流程。
- **signer 私钥托管**：生产必须 KMS；本地用 anvil PK；staging 用 AWS Secrets Manager。
- **metadata 可用性**：链上只存 URI；CDN/存储失效 → 证书图片丢失；建议 IPFS pinning。