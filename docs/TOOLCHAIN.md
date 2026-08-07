# Smart contract toolchain

> 合约工具链参考：ABI 的产生与作用、编译与发布流程、Foundry / Hardhat / Ganache / Truffle
> 四件套对比、以及现代合约主流的 7 阶段开发流程。
>
> 本仓（x-web3）只走 Foundry 路线，1–4 阶段落地；5–7 是高价值项目的扩展。
>
> 项目内的具体入口：[docs/ARCHITECTURE.md](ARCHITECTURE.md)（前后端 ABI 桥 / Sepolia 部署）、
> [docs/DEPLOYMENTS.md](DEPLOYMENTS.md)（已部署地址档案）。

---

## 1. ABI — 合约的接口契约

### 1.1 ABI 是什么

ABI = **Application Binary Interface**。合约编译产物里的一份 JSON 清单，描述：

| 内容 | 例子（取自 [packages/contracts/src/Notepad.sol](../packages/contracts/src/Notepad.sol)） |
|------|------|
| 函数签名 + 参数 + 返回值 | `getNotes(address) → Note[]` |
| 事件签名 | `NoteCreated(address,uint256,uint64)` |
| 自定义错误 | `TitleTooLong()` / `NoteNotFound()` |
| 状态变量 getter（自动生成） | `MAX_NOTES_PER_USER()` |

把它当成「合约的 type signature」：调用方靠它把 `"getNotes"` + 参数编码成 `0x...` calldata，
再把返回的 `0x...` 解码成结构化数据。**没有 ABI，前端根本不知道合约暴露了哪些函数、
参数顺序、返回结构**。

### 1.2 怎么产生（本仓三站）

#### 站 1：Solidity 源码

[packages/contracts/src/](../packages/contracts/src/) — 纯文本，函数、事件、错误按 Solidity 语法写。

#### 站 2：forge build 产物（**ABI 的真正源头**）

```bash
pnpm contracts:compile        # = forge build
```

输出 `packages/contracts/out/Notepad.sol/Notepad.json`。这个 JSON 里 `abi` 字段就是完整的
ABI 数组 — 由 solc 在编译时根据函数/事件/错误声明自动生成。

> ABI **不是手写的**，是编译器产物。任何「合约改了 ABI 没跟着变」的问题，
> 几乎都是因为忘了重跑 forge build。

#### 站 3：导成 TS 模块（给前端用）

```bash
pnpm contracts:export:abi
```

跑 [packages/contracts/script/export-abi.mjs](../packages/contracts/script/export-abi.mjs)：

1. 读 `out/<Name>.sol/<Name>.json`
2. 抽 `artifact.abi`
3. 写成 [apps/web/src/contracts/notepad.abi.ts](../apps/web/src/contracts/notepad.abi.ts)：

```ts
export const notepadAbi = [
  {
    type: 'function',
    name: 'getNotes',
    stateMutability: 'view',
    inputs:  [{ name: 'owner', type: 'address' }],
    outputs: [{ name: '',     type: 'tuple[]', components: [...] }],
  },
  // ... createNote / updateNote / deleteNote / 3 个事件 / 4 个错误
] as const;
export type NotepadAbi = typeof notepadAbi;
```

**关键的 `as const`**：把数组字面量收窄成 literal type — wagmi 才能从中推断出
「`getNotes` 存在、参数是 `address`、返回是 `Note[]`」。

### 1.3 干什么用 — 让 wagmi 类型安全

在 [apps/web/src/components/Notepad.tsx](../apps/web/src/components/Notepad.tsx) 里：

```tsx
const { data: notes } = useReadContract({
  abi:     notepadAbi,                          // ← 注入 ABI
  address: notepadDeployments.sepolia.address,
  functionName: 'getNotes',                     // ← 必须存在于 ABI
  args:    [address],                           // ← 类型由 ABI 推出 = address
});
```

ABI 在 wagmi 调用链里干了三件事：

| 时机 | 角色 |
|------|------|
| **编译期 (TypeScript)** | 校验 `functionName` 是 ABI 里有的函数；`args` 类型与 ABI 一致；`data` 返回类型是 ABI 推导出的 `Note[]`。**写错函数名 / 参数顺序错 = TS 编译失败**。 |
| **运行期 (wagmi → viem)** | 把 `functionName + args` 编码成 calldata（`0x...`），通过 RPC 发 `eth_call`；再把返回的 hex 用 ABI 解码回结构化对象。 |
| **运行期 (事件)** | 解码 `logs`：合约 emit `NoteCreated(address,uint256,uint64)`，前端用 ABI 的 `events.NoteCreated.inputs` 把 log data 解码成 `{owner, id, at}`。 |

### 1.4 关键约束

- **ABI 只从 forge 产物生成** — 脚本
  [export-abi.mjs:62-83](../packages/contracts/script/export-abi.mjs#L62-L83) 找不到 artifact 就报错，
  让你先跑 `pnpm contracts:compile`。
- **绝不在组件里手写 ABI** — 见 [rules/frontend.md](../.claude/rules/frontend.md)：
  `ABI 与地址来自 src/contracts/*，不要在组件里写硬编码`。
- **ABI 改了要重导出** — 合约函数签名变了 → 重跑 `pnpm contracts:compile` →
  `pnpm contracts:export:abi`。否则前端拿到的还是旧 ABI，调用会 revert / TypeScript 报错。
- **ABI 不包含地址** — 地址在 [apps/web/src/contracts/deployments.ts](../apps/web/src/contracts/deployments.ts)
  单独维护。ABI = 「合约长什么样」（不变、随源码走），地址 = 「合约部署在哪里」（变、跟部署走）。

---

## 2. 编译与部署流程

### 2.1 编译流程

`pnpm contracts:compile` = `forge build`。在 [packages/contracts/](../packages/contracts/) 里跑：

```
src/Notepad.sol
   │  1) solc 0.8.24 编译
   │     - imports 通过 remappings.txt 解析
   │       forge-std/        → node_modules/forge-std/src/
   │       @openzeppelin/... → node_modules/@openzeppelin/contracts/contracts/
   │     - AST → IR → EVM bytecode
   ▼
out/Notepad.sol/Notepad.json
   ├─ bytecode        : 部署字节码（构造函数 + runtime code）
   ├─ deployedBytecode: runtime code（部署后留在链上的）
   ├─ abi             : 函数 / 事件 / 错误 描述数组  ← 后端 ABI 导出靠这个
   ├─ metadata        : 编译器版本、optimizer、sourceHash（验证要用）
   └─ storageLayout   : 变量 slot（升级 / gas 估算用）
```

干的事情：

| 步骤 | 内容 |
|------|------|
| 解析 import | 用 [packages/contracts/remappings.txt](../packages/contracts/remappings.txt)，不依赖 git submodule |
| 编译所有 `src/*.sol` | solc + optimizer（`foundry.toml` 配置） |
| 输出 JSON artifact | 路径 = `out/<ContractName>.sol/<ContractName>.json` |
| 不发任何交易 | `forge build` 纯本地，无网络 |

### 2.2 部署流程

`pnpm contracts:deploy:notepad:sepolia` = `forge script ... --broadcast --verify`。

以 [script/DeployNotepad.s.sol](../packages/contracts/script/DeployNotepad.s.sol) 为例，逐步走：

```
1) 读 .env（前提：当前目录有 .env）
   DEPLOYER_PRIVATE_KEY  → vm.envUint
   SEPOLIA_RPC_URL       → 命令行 --rpc-url 传入

2) 反推 deployer 地址
   address deployer = vm.addr(privateKey)
   用于 console2.log 打出来人眼核对

3) 启动广播
   vm.startBroadcast(privateKey)
   ┌─────────────────────────────────────────────────────┐
   │ 之后的每一笔 EVM 调用都会被 forge 打包成真实交易       │
   │ 由 deployer 签名 + 通过 --rpc-url 提交到 Sepolia       │
   └─────────────────────────────────────────────────────┘
   
   Notepad notepad = new Notepad();    // ← 真实部署交易
   
   vm.stopBroadcast();

4) forge 自动生成 broadcast/11155111/run-latest.json
   - 部署交易 hash
   - 合约地址
   - 编译器参数
   - 所有 console2 输出

5) --verify 触发 Etherscan 验证
   forge 把源码 + 编译器参数 + bytecode hash 一起 POST 给 Etherscan
   Etherscan 比对 → 显示 "Verified" 绿标
```

完整命令链：

```bash
# 1) 单测
forge test
# 2) dry-run 部署（不发交易，仅模拟）
forge script script/DeployNotepad.s.sol:DeployNotepad \
    --rpc-url $SEPOLIA_RPC_URL                # 没有 --broadcast = dry-run
# 3) 真部署 + 自动验证
forge script script/DeployNotepad.s.sol:DeployNotepad \
    --rpc-url $SEPOLIA_RPC_URL --broadcast --verify -vvvv
# 4) 把 console 输出的地址 paste 进 deployments.ts
```

### 2.3 其他部署方式

按「使用门槛 / 适用场景」分四档：

#### A. Foundry 自家替代（同一个生态，代码不用改）

| 工具 | 何时用 | 命令 |
|------|--------|------|
| `forge create` | 单合约、没构造参数、没复杂初始化 | `forge create --rpc-url $SEPOLIA_RPC_URL --private-key $DEPLOYER_PRIVATE_KEY src/Notepad.sol:Notepad` |
| `cast send` | 手动构造一笔部署交易（最底层） | `cast send --rpc-url $SEPOLIA_RPC_URL --private-key $PK --create 0x<Notepad bytecode>` |
| `forge script --ledger / --trezor` | 硬件钱包（Ledger/Trezor）签名 | 加 `--ledger` 或 `--trezor`，forge 调 USB 签名 |
| `forge verify-contract` | 已经部署但忘记带 `--verify`，事后补验证 | `forge verify-contract --chain sepolia --watch 0x... src/Notepad.sol:Notepad` |

`forge create` 与 `forge script` 的区别：

- `forge create` = 一个命令部署单个合约
- `forge script` = 跑一段 Solidity 脚本（可批量、链上预调用、用 cheatcode 准备状态）

#### B. 跨链 / 批量部署

| 工具 | 场景 |
|------|------|
| **forge script + 多 RPC** | 同一次 deploy 顺带发到 Sepolia + Base Sepolia + Arbitrum Sepolia（脚本里循环 `vm.startBroadcast` 多次） |
| **CREATE2 工厂** | 同一合约在不同链**确定性地址**（地址 = `keccak256(deployer, salt, bytecode)`）。OZ 的 `Create2Deployer` 常用 |
| **Safe (Gnosis Safe) 模块** | 资金合约 / 需要多人共管的合约。从 Safe 通过 `execTransaction` 触发 `safeDeployer` 调用 |
| **thirdweb Deploy** | 托管服务，免私钥交互；适合不想自己管 RPC + 验证的团队 |

#### C. 更高层 / 托管

| 工具 | 优势 | 劣势 |
|------|------|------|
| **Hardhat + hardhat-deploy** | JS 生态插件丰富，已有项目迁移成本低 | 比 Foundry 慢；本仓没装 |
| **Remix IDE**（浏览器） | 零安装、所见即所得、适合教学 | 不进 CI，全手动 |
| **OpenZeppelin Defender** | 自动 relayer + 监控 + 多签 | 服务费 + 把私钥交给云 |
| **Tenderly / Thirdweb** | 一键部署 + 自动 verify + UI | 中心化、平台依赖 |

#### D. 本地测试网（不上主链）

| 工具 | 用途 |
|------|------|
| `anvil` | 本地节点，链 ID 默认 31337；CI / 本地集成测试用 |
| `anvil --fork` | fork 主网某区块状态，适合 fork 测试 |
| 测试合约里的 `deployCode("Notepad")` | forge cheatcode 部署 — **不消耗 gas、不发交易**，纯单元测试用 |

### 2.4 本仓现状 vs 备选

| 维度 | 当前选择 | 是否合理 |
|------|---------|---------|
| 框架 | **Foundry** | ✅ 编译 / 测试 / 部署 / verify 一站式，本项目已经统一 |
| 部署脚本 | `forge script` | ✅ 留 `run-latest.json` 留档，方便审计 |
| 签名方式 | 环境变量私钥 | ⚠️ 本地可以；生产建议 Ledger 或 Safe multisig |
| 验证 | `--verify` 自动 | ✅ 一行命令；Etherscan 是事实标准 |
| 多链 | 单脚本（可扩展） | ✅ 加 `--rpc-url` 即可；但 CREATE2 还没接入，地址不能跨链预测 |

**下一步可能的升级路径**（如果项目长大）：

1. 部署走 Safe multisig（保护 deployer 私钥）
2. 用 CREATE2 工厂发到多链，保证地址一致
3. CI 里用 `forge script` + Tenderly 模拟 mainnet 部署再 `forge verify-contract` 上主网
4. 把部署脚本切到 `forge create` + 一个 manifest JSON，省去 `DeployXxx.s.sol` 重复样板

---

## 3. 工具生态对比

### 3.1 Foundry vs Hardhat vs Ganache vs Truffle

| 维度 | Foundry | Hardhat | Ganache | Truffle |
|------|---------|---------|---------|---------|
| 语言 | Rust CLI + Solidity 脚本 | Node.js CLI + JS/TS 脚本 | 桌面 GUI + Node CLI | JS（ES5 老派） |
| 测试写法 | Solidity（forge-std + cheatcode） | Mocha + chai（JS） | 无 | Mocha + chai（JS） |
| 测试速度 | **极快**（原生二进制） | 中（solc-js 缓存） | — | 慢（solc-js） |
| Fuzzing | 内置（默认 256 runs） | 内置（较弱） | — | 第三方 |
| 部署脚本 | `script/*.s.sol` + `vm.startBroadcast` | `scripts/deploy.js` 或 Ignition | 无（外部 Truffle 调用） | `migrations/*.js` |
| Etherscan | `forge --verify` 内置 | `hardhat-verify` 插件 | 无 | `truffle-plugin-verify` 插件 |
| 本地节点 | `anvil`（独立二进制） | Hardhat Network（in-process） | Ganache（独立 + GUI） | 默认依赖 Ganache |
| 调试 | `forge debug`（栈帧回溯） | console + stack traces | GUI 可视化 | `truffle debug`（逐 op） |
| 生态 | 较新，生态薄但增长快 | **最厚**，OZ/Thirdweb/Defender 全优先支持 | 旧且停更 | **弃用**（2024） |
| 类型安全 | Solidity + `as const` ABI | TS 完整支持 | — | 弱 |
| 项目状态 | ✅ 主流 | ✅ 主流 | ⚠️ 维护模式 | ⚠️ 弃用维护 |

### 3.2 本仓选 Foundry 的原因

| 理由 | 项目里的证据 |
|------|------|
| **单仓库统一 Solidity** | 测试、部署脚本、cheatcode 全 Solidity — 不用为合约维护两套语言 |
| **速度快** | CI 跑 `forge test` + `forge fmt --check` 几秒出；Hardhat 一启动就要十几秒 |
| **ABI 真源 = 编译产物** | `out/<Name>.sol/<Name>.json` 直接喂给前端；Hardhat 同样有 artifacts 机制但路径更绕 |
| **Fuzz + Invariant 内置** | [rules/testing.md](../.claude/rules/testing.md) 要求 `forge test` 默认 256 runs、核心 ≥ 1000 |
| **Foundry 的 verify 流程最稳** | `--verify` 一次过，Etherscan 那边跟 solc 参数匹配度高 |

### 3.3 什么时候反而该选其他

| 场景 | 推荐 |
|------|------|
| 团队全是 JS 背景 | Hardhat（cheatcode 学习曲线是门槛） |
| 项目要集成一堆 JS 工具链 | Hardhat（typechain、defender、tenderly、graph 等） |
| 复杂的链下脚本（mock 服务、抓事件、数据库写） | Hardhat（JS 比 Solidity 写业务逻辑舒服） |
| 跨链 deploy 需要声明式 | Hardhat Ignition |
| 教学 / GUI 调试 | Ganache GUI（图形界面看账户 / 区块 / 事件 / 存储） |
| 维护 2017–2021 老 dApp | 继续用 Truffle / Ganache |

### 3.4 切换成本

| 切到 | 工作量 |
|------|--------|
| Hardhat | 重写所有 `*.t.sol` → `test/*.js`、`*.s.sol` → `hardhat-deploy` 脚本；Foundry artifact 路径 → Hardhat artifact 路径；前端 `export-abi.mjs` → `typechain` 或 `hardhat-export-abi` |
| Foundry → Foundry 新版 | 几乎为零（向后兼容） |
| Hardhat → Foundry | 同上反过来，工作量相当 |

---

## 4. 现代合约主流开发流程（7 阶段）

整个生命周期是 **7 个阶段 × 多重防护**，每阶段都有固定产出与卡点。
**越靠近钱的项目，门禁越厚**——个人项目到 CI 即可；DeFi 协议到审计 + Bug Bounty；
L1 / Bridge 还要加形式化证明。

### 4.1 立项 — 选型

| 决策 | 主流选择 | 理由 |
|------|---------|------|
| 框架 | Foundry（速度 + Solidity 一统）或 Hardhat（JS 生态厚） | 看团队背景 |
| Solidity 版本 | `0.8.24+`（默认 checked 算术） | 防溢出 |
| 基础库 | OpenZeppelin Contracts | 标准化 + 审计充分 |
| 测试 | forge-std / hardhat + chai | 框架自带 |
| 工具链 | forge / hardhat + plugins | — |
| RPC / 链 | 测试网 Sepolia / Holesky，主网前必经 | 真实环境暴露 bug |

本仓配置：[foundry.toml](../packages/contracts/foundry.toml)（0.8.24、optimizer 200）、
OpenZeppelin via pnpm + remappings。

### 4.2 开发 — 写 + 测

#### 写合约
- NatSpec `@title @notice @dev` 必填
- 自定义 error 优先于 `require(string, ...)`
- 事件用过去式 + `indexed` 关键字段
- 状态变量 `internal > private > public` 优先
- 重入场景必走 **checks-effects-interactions** + `ReentrancyGuard`
- 库函数走 `internal`；公共走 `library`
- 默认 **不可升级**（升级需显式 UUPS + 24h timelock）

#### 测试金字塔

```
        /\
       /  \         Invariant / Property（最深路径）
      /----\
     /      \       Fuzz（256–1000 runs）
    /--------\
   /          \     Integration（多合约交互）
  /------------\
 /              \   Unit（每个分支、每个 revert）
```

- **Unit** — 每个分支至少一个测试；`vm.expectRevert(Contract.CustomError.selector)` 显式断言
- **Fuzz** — `forge test` 默认 256 runs，核心数值函数 ≥ 1000
- **Invariant** — `forge test --match-contract Invariant`；状态不变量随机调用后仍成立
- **Integration** — 部署脚本先在 anvil 跑通
- **Coverage** — `forge coverage` ≥ 90% 行覆盖

本仓：[packages/contracts/test/](../packages/contracts/test/) 走 unit + fuzz。

### 4.3 提交前 CI — 自动化门禁

PR 必须全绿的卡点：

```yaml
# 典型 CI pipeline
- pnpm install
- pnpm contracts:fmt:check       # forge fmt --check (CI 拒 warning)
- pnpm contracts:test            # forge test -vvv
- pnpm contracts:coverage        # ≥ 90%
- pnpm typecheck                 # tsc --noEmit (前端)
- slither . --filter high        # 静态分析，阻断 high/medium
- forge test --gas-report        # 关键函数 gas 上限检查
```

工具矩阵：

| 工具 | 作用 |
|------|------|
| `forge fmt --check` | 格式（合约） |
| `forge test` / `hardhat test` | 测试 |
| `forge coverage` | 行覆盖 |
| **Slither** | 静态分析（最常用的事实标准） |
| **Mythril** | 符号执行（找深层路径 bug） |
| **Aderyn** | Rust 写的 Slither 替代品，更快 |
| **solhint** | Lint（命名 / 可见性 / 风格） |
| **Certora** / **K Framework** | 形式化验证（数学证明级别，贵但强） |
| `forge test --gas-report` | Gas 报告 |
| `tsc --noEmit` / `eslint` | 前端类型 / lint |

参考：[rules/testing.md](../.claude/rules/testing.md) 与 [rules/smart-contract.md](../.claude/rules/smart-contract.md)
已经把这些门禁硬约束住了。

### 4.4 测试网部署 — 真实环境冒烟

1. **Dry-run on fork** — `anvil --fork-url $MAINNET_RPC` 起主网分叉，部署脚本先跑一遍无广播版本
   （`forge script ... --fork-url` 不带 `--broadcast`），确认 revert / gas / 状态都对。
2. **真部署到测试网** — Sepolia / Holesky / Base Sepolia：
   - `--broadcast` 发交易
   - `--verify` 自动 Etherscan 验证
3. **前端冒烟** — 把部署地址灌进前端，本地 `pnpm dev`，连钱包走完整路径
4. **Etherscan 复核** — 手工确认 Verified 状态，源码与 bytecode 哈希一致
5. **留档** — `broadcast/<chain>/run-latest.json` 进 git；地址登记 [docs/DEPLOYMENTS.md](DEPLOYMENTS.md)

### 4.5 审计 — 高价值项目必经

| 级别 | 做什么 | 工具 / 服务 |
|------|--------|------------|
| **L0 自审** | 开发团队内部 review + 测试补强 | — |
| **L1 自动化扫描** | Slither / Mythril / Aderyn | 集成到 CI |
| **L2 外部审计** | 专业团队人工 review | Trail of Bits / OpenZeppelin / Spearbit / ChainSecurity / Cantina / Certora |
| **L3 Bug Bounty** | 公开悬赏 | Immunefi / Code4rena / Sherlock |
| **L4 形式化验证** | 数学证明关键不变量 | Certora / K Framework / Act / Veridise |
| **L5 持续审计** | 上线后监控新威胁 | Forta / Defender Sentinel / Tenderly Alert |

**审计前**准备清单：
- 测试覆盖率 ≥ 95%
- NatSpec 完整
- 文档清晰（spec / 设计文档 / 已知风险）
- Slither / Aderyn 已清 high/medium
- 测试网已部署 ≥ 2 周稳定

**审计后**：
- 公开审计报告（`docs/audit/`）
- 修复所有 critical / high
- 跟踪 medium / low（写进 todo）

### 4.6 主网部署 — 严肃仪式

主网部署**绝对不能像 Sepolia 那样直接用热钱包私钥**：

| 关键点 | 标准做法 |
|--------|---------|
| **签名** | 硬件钱包（Ledger / Trezor）+ Safe 多签 |
| **预演** | 在 mainnet fork 上模拟主网部署 |
| **升级治理** | UUPS + 24h / 48h Timelock + 多签 |
| **验证** | `--verify` + 手动 Etherscan 复核 |
| **监控** | Tenderly Alert + Forta Sentinel + Defender |
| **回滚预案** | 升级路径明确，紧急暂停（`Pausable` 或 circuit breaker） |
| **公告** | 部署前社区预告，部署后披露地址 + 验证链接 |
| **应急联系** | Security email / Telegram 群 / Twitter |
| **保险** | Nexus Mutual / InsurAce（高 TVL 才有意义） |

工具栈：
- **Safe**（多签）：核心 admin 走 3/5 或 5/7 多签
- **Tenderly**：fork / 监控 / alert
- **Defender**：relayer + 自动化 + 监控
- **Forta**：去中心化事件监控 bot
- **OpenZeppelin Defender Relayer**：交易中继 + key 保管

### 4.7 上线后 — 持续守护

```
合同代码  ──►  监控服务  ──►  异常事件  ──►  应急响应
   │             │              │              │
   │             │              │              ▼
   │             │              │        pause / upgrade / migrate
   │             │              ▼
   │             │         alert (Slack / PagerDuty)
   │             ▼
   │         指标 / 仪表盘 (Tenderly / Dune)
   ▼
升级流程: Timelock 24h → 多签投票 → 执行
```

持续任务：
- 跟踪协议新威胁（如新依赖版本 / 已知 CVE）
- 监控 TVL、交易量、异常调用模式
- 定期 review Forta bot 日志
- 季度复盘 + 必要时补丁升级

### 4.8 完整流程图

```
┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐
│ 立项选型 │───►│ 合约+测试 │───►│ CI门禁  │───►│ 测试网  │───►│  审计   │
│ Foundry │    │ Fuzz+Inv │    │ Slither │    │ Sepolia │    │(高价值) │
└─────────┘    └─────────┘    └─────────┘    └─────────┘    └────┬────┘
                                                                  │
                                                                  ▼
              ┌─────────────────────────────────────────────────────────┐
              │                       主网部署                            │
              │  多签 + 硬件钱包 + Timelock + Tenderly + Forta            │
              └────────────────────────────┬────────────────────────────┘
                                           │
                                           ▼
              ┌─────────────────────────────────────────────────────────┐
              │                       上线后                            │
              │  监控 + Bug Bounty + 升级治理 + 季度复盘                  │
              └─────────────────────────────────────────────────────────┘
```

---

## 5. 本仓（x-web3）覆盖到哪一阶段

| 阶段 | 状态 | 证据 |
|------|------|------|
| 1 立项 | ✅ | Foundry + OZ + 0.8.24 |
| 2 开发 | ✅ | [Notepad.sol](../packages/contracts/src/Notepad.sol) + 测试 + 部署脚本 |
| 3 CI 门禁 | ✅ | `forge fmt --check && forge test && tsc --noEmit` 全绿 |
| 4 测试网 | ✅ | Notepad 已部署到 Sepolia [DEPLOYMENTS.md](DEPLOYMENTS.md) |
| 5 审计 | ❌ 未做 | 个人 / 教学项目未走 |
| 6 主网 | ❌ 未上 | 教学只跑 Sepolia |
| 7 监控 | ❌ 未接 | 无 Tenderly / Forta |

**定位：教学 + 脚手架**——把流程 1–4 跑通作为参考实现。流程 5–7 是工业级才需要的扩展。

---

## 6. 速查卡

### 6.1 核心流程一句话

> **Solidity 源码 → forge build → out/*.json（含 ABI） → forge script --broadcast --verify → Sepolia 链上 → export-abi.mjs → apps/web/src/contracts/*.abi.ts → React 组件 (wagmi hooks)**。

### 6.2 ABI 三句话

1. ABI = 编译器给你的合约接口描述（函数 / 事件 / 错误）。
2. 真源在 `out/<Name>.sol/<Name>.json`；前端拿到的是 `as const` 收窄的 TS 模块。
3. 改合约后必须重跑 `compile + export:abi`，否则前端 ABI 漂移。

### 6.3 选 Foundry 不选 Hardhat 一句话

> 快、Solidity 一统天下、cheatcode 强、ABI 真源一致；JS 生态厚度不及 Hardhat，
> 但本项目不需要。

### 6.4 主流 7 阶段一句话

> 立项 → 开发 → CI 门禁 → 测试网 → 审计 → 主网部署 → 上线监控；**越靠近钱、门禁越厚**。

### 6.5 关键文件跳转

| 想了解 | 看 |
|--------|-----|
| 项目内 ABI 桥与部署管道 | [ARCHITECTURE.md](ARCHITECTURE.md) §3、§4 |
| 已部署地址档案 | [DEPLOYMENTS.md](DEPLOYMENTS.md) |
| 新增合约 cookbook | [ARCHITECTURE.md](ARCHITECTURE.md) §7 |
| 工具链命令 | [README.md](../README.md) 「Daily workflow」 |
| 合约规则 | [.claude/rules/smart-contract.md](../.claude/rules/smart-contract.md) |
| 测试规则 | [.claude/rules/testing.md](../.claude/rules/testing.md) |
| 安全规则 | [.claude/rules/security.md](../.claude/rules/security.md) |