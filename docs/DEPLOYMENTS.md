# Deployed contracts registry

> 链上部署的**永久档案**。这份文档跟代码一起进 git，所以只放**公开地址**，
> 任何私钥 / RPC URL / API key 都不写在这里。
>
> 配合使用：
> - `apps/web/src/contracts/deployments.ts` —— 前端运行时读这里（也是 git 跟踪）
> - `packages/contracts/broadcast/<chain>/run-latest.json` —— Foundry 的部署 receipt（git 跟踪）
> - `apps/web/.env` —— 仅 `VITE_NOTEPAD_CONTRACT_ADDRESS` 这种公开值的本地副本

## Sepolia (chainId `11155111`)

Etherscan: <https://sepolia.etherscan.io/>

### Notepad

| 字段 | 值 |
|------|------|
| 合约 | `Notepad` |
| 地址 | [`0xA59D7C02c8C67C9aE21Cf674fd92564209f627aD`](https://sepolia.etherscan.io/address/0xa59d7c02c8c67c9ae21cf674fd92564209f627ad) |
| 部署交易 | [`0xb2f88f98...dbc99bd7219`](https://sepolia.etherscan.io/tx/0xb2f88f98e156da2b94aae36db5adf31909441e6b5714e280f88f5dbc99bd7219) |
| 区块 | `0xae81bf` (11436415) |
| 部署者 | `0x83b5c0F729885DA3FF70f30fD8Fa400b8A0bDD93` |
| 编译器 | `solc 0.8.24` · optimizer 200 · via_ir false |
| 验证状态 | ✅ Verified (Single file) |
| 部署时间 | 2026-08-07 |
| 部署命令 | `forge script script/DeployNotepad.s.sol:DeployNotepad --rpc-url https://ethereum-sepolia.publicnode.com --broadcast --verify` |
| 部署凭据 | `packages/contracts/broadcast/DeployNotepad.s.sol/11155111/run-latest.json` |
| 源码 | `packages/contracts/src/Notepad.sol` |
| ABI | `apps/web/src/contracts/notepad.abi.ts` |
| 前端登记 | `apps/web/src/contracts/deployments.ts → notepadDeployments.sepolia.address` |

**功能**：每用户最多 50 条记事，标题 ≤ 64 字节、正文 ≤ 1024 字节；swap-and-pop 删除；
id 单调递增不复用。详见 `docs/ARCHITECTURE.md` §5 与 `packages/contracts/TOUR.md` §2.4。

### Counter

> 占位教学合约，演示 Ownable + 自定义 error + 事件。
> 与 Notepad 并列存在，未在 `apps/web` 中接入 UI，仅作 wagmi/foundry 用法参考。

| 字段 | 值 |
|------|------|
| 合约 | `Counter` |
| 状态 | ⏳ **未部署**（源码在 `packages/contracts/src/Counter.sol`） |
| 构造函数 | `constructor(address initialOwner)` OZ v5 显式 owner |

如需部署：参考 `packages/contracts/script/DeployCounter.s.sol`，
部署完把地址补到本表 + `apps/web/src/contracts/deployments.ts → counterDeployments.sepolia.address`。

### YDToken

| 字段 | 值 |
|------|------|
| 地址 | [`0x734F98B0b7e34B7A4E655378baA9760a9368AE97`](https://sepolia.etherscan.io/address/0x734f98b0b7e34b7a4e655378baa9760a9368ae97) |
| 部署交易 | [`0xcf670061…8aea4787`](https://sepolia.etherscan.io/tx/0xcf670061c1183def32865b0d8fdfc62db03d2529906de6f7eb7ae2548aea4787) |
| 区块 | `11466727` |
| 状态 | 已部署，源码待 Etherscan API key 验证 |

### CourseMarket

| 字段 | 值 |
|------|------|
| 地址 | [`0x95F3C314f43fBFB1D5420197F7569d8fE0c92706`](https://sepolia.etherscan.io/address/0x95f3c314f43fbfb1d5420197f7569d8fe0c92706) |
| 部署交易 | [`0xc51e7882…f04f0ff9`](https://sepolia.etherscan.io/tx/0xc51e78824160ecc07d4bc95a381230a74a614aeb2cf368a931cdbfc4f04f0ff9) |
| 区块 | `11466730` |
| 状态 | 已部署（尚未配置课程），源码待验证 |

### CertificateNFT

| 字段 | 值 |
|------|------|
| 地址 | [`0xC708E0040FA8659ee3A869D0eba6020206a0b90C`](https://sepolia.etherscan.io/address/0xc708e0040fa8659ee3a869d0eba6020206a0b90c) |
| 部署交易 | [`0x4b5b6eb3…87c07cc4`](https://sepolia.etherscan.io/tx/0x4b5b6eb3740c85cec2aab2be2cc1bb62d5c6aa6644077649a772718887c07cc4) |
| BURNER_ROLE 交易 | [`0xaab46978…a57818b`](https://sepolia.etherscan.io/tx/0xaab46978bc8bfaaee06f1807dc4f46f862b6e782562375563847f695fa57818b) |
| 区块 | `11466733` |
| 状态 | 已部署，admin/minter/burner 为测试部署钱包，源码待验证 |

---

## 维护流程

增删一条部署后，**至少**做三件事：

1. **更新本表**（每个合约一行，包含 tx hash + 验证状态）。
2. **同步 `apps/web/src/contracts/deployments.ts`**（前端运行时读）。
3. **保留 `packages/contracts/broadcast/<chain>/run-latest.json`**（自动生成，git 跟踪）。

如果只是 redo 同一合约（代码改了重新部署），建议：
- 把旧的 `run-latest.json` 改名为 `run-2026-08-07-11436415.json` 之类，避免被新部署覆盖；
- 把本表旧行移到表底，标注 `(deprecated — replaced by <new address on 日期>)`。

## 重新部署

代码改动后：

```bash
# 1. 编译 + 单测
cd packages/contracts
~/.foundry/bin/forge build
~/.foundry/bin/forge test

# 2. 部署（替换 Sepolia RPC、--private-key 或 .env）
~/.foundry/bin/forge script script/DeployNotepad.s.sol:DeployNotepad \
    --rpc-url $SEPOLIA_RPC_URL \
    --broadcast --verify -vvvv

# 3. 把新地址粘到
#    - apps/web/src/contracts/deployments.ts
#    - apps/web/.env (VITE_NOTEPAD_CONTRACT_ADDRESS)
#    - 本文件的上方表格

# 4. 重新导出 ABI
cd ../..
pnpm --filter @x-web3/contracts export:abi

# 5. typecheck
pnpm --filter @x-web3/web typecheck
```

## 跨链扩展

如果要发到 mainnet / 其他测试网：

1. 复制对应的 RPC URL 与 Etherscan-equivalent key 到 `.env`。
2. 在 `apps/web/src/contracts/deployments.ts` 加一条新链：
   ```ts
   export const notepadDeployments = {
       sepolia: { address: '0xA59D...', chainId: 11155111 },
       mainnet: { address: '0x???', chainId: 1 },
   } as const;
   ```
3. 在本表加一段新的 `<ChainName> (chainId ...)`。
4. 前端 `useAccount().chainId` 判断后切换对应部署条目。
