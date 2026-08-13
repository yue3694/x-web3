# packages/contracts · Foundry 智能合约

> Solidity 0.8.24 + Foundry + OpenZeppelin 5 的链上结算与凭证合约集合。
> 部署目标 Sepolia（chain id `11155111`）；同时支持本地 Anvil（chain id `31337`）
> 做完整闭环测试。
>
> 全局架构、产品流程、合约地址与 AWS 拓扑统一在顶层
> [README.md](../../README.md) 维护。本文件覆盖合约包自身的目录结构、配置、
> 编译 / 测试 / 部署命令、ABI 导出链路、SSO 与不变量。

---

## 1. 模块定位

合约集合按业务域分组：

| 域 | 合约 | 职责 |
|---|---|---|
| 链上记事本（demo） | `Notepad.sol` | 单地址隔离的 CRUD 笔记，公开读、私写；demo / e2e fixture |
| 计数器（demo） | `Counter.sol` | 最小 increment / decrement；演示 / 测试用 |
| 课程结算（核心） | `CourseMarket.sol` | 配置课程（token / amount / priceVersion）→ 购买；防重购 + reentrant + paused |
| ERC-20 支付币（核心） | `YDToken.sol` | 18 位精度 ERC20，cap 1B，role-based mint/burn |
| 兑换入口（核心） | `SepoliaYDSale.s.sol` | SepoliaETH → YD 测试兑换；treasury 收 ETH |
| 参考价（辅助） | `ChainlinkPriceOracle.sol` | 兼容 Chainlink AggregatorV3Interface 的 ETH/USD adapter；新鲜度校验 |
| 完课证书（核心） | `CertificateNFT.sol` | soulbound（不可转让）ERC-721；MINTER_ROLE only |
| 接口 | `interfaces/` | `ICertificateNFT / IPriceOracle / IYDToken` |
| 测试桩 | `mocks/MockV3Aggregator.sol` | Chainlink 测试桩 |

---

## 2. 目录结构

```text
packages/contracts/
├── foundry.toml                   # Foundry 配置（solc 0.8.24 / optimizer 200 / via_ir off）
├── remappings.txt                 # 依赖映射：forge-std / @openzeppelin → node_modules
├── package.json                   # @x-web3/contracts · pnpm 工作区
├── README.md / TOUR.md            # 本文件 + 巡游文档
├── src/                           # 生产合约（*.sol）
│   ├── Notepad.sol                # 笔记 demo
│   ├── Counter.sol                # 计数 demo
│   ├── CourseMarket.sol           # F03 课程结算
│   ├── YDToken.sol                # F05 结算 ERC-20
│   ├── SepoliaYDSale.sol          # F05 SepoliaETH → YD
│   ├── ChainlinkPriceOracle.sol   # ETH/USD 参考价
│   ├── CertificateNFT.sol         # F04 完课证书
│   ├── interfaces/                # 对外接口（ICertificateNFT / IPriceOracle / IYDToken）
│   └── mocks/MockV3Aggregator.sol # Chainlink V3 Aggregator 测试桩
├── test/                          # 测试（*.t.sol）
│   ├── Notepad.t.sol · Counter.t.sol · CourseMarket.t.sol
│   ├── YDToken.t.sol · SepoliaYDSale.t.sol · ChainlinkPriceOracle.t.sol
│   ├── CertificateNFT.t.sol
│   ├── DeployCourseMarketScript.t.sol · DeployCertificateNFTScript.t.sol
│   └── DeployYDToken.t.sol
├── script/                        # 部署 / 工具脚本
│   ├── DeployCounter.s.sol · DeployNotepad.s.sol
│   ├── DeployCourseMarket.s.sol   # Mode A（deploy-only）/ Mode B（JSON 批量配置）
│   ├── DeployYDToken.s.sol · DeploySepoliaYDSale.s.sol
│   ├── DeployCertificateNFT.s.sol
│   ├── DeployPriceOracle.s.sol · DeployTestOracle.s.sol
│   ├── export-abi.mjs             # 编译产物 → apps/web/src/contracts/*.abi.ts
│   └── compute-topics.mjs         # topic0 计算工具
├── lib/                           # 占位（实际依赖走 node_modules + remappings）
├── node_modules/                  # pnpm 管理：forge-std + @openzeppelin/contracts
│                                   # 不提交 .gitmodules
├── out/                           # forge build 产物（含 ABI）
│                                   # 不提交
├── cache/                         # forge cache
├── broadcast/                     # forge script --broadcast 留档（run-latest.json）
└── docs/                          # forge doc 输出
```

`src/` 是唯一被 `forge build` 扫描的生产代码目录；测试与脚本与之并列。

---

## 3. Foundry 配置（`foundry.toml`）

| 字段 | 值 | 说明 |
|---|---|---|
| `solc_version` | `0.8.24` | **禁止**单独改动，需与所有 pragma `^0.8.24` 同步 |
| `optimizer` | `true`，`optimizer_runs = 200` | 通用部署档；CI profile 拒 warning |
| `via_ir` | `false` | 默认 IR pipeline 关闭；与 notepad / coursemarket 行为一致 |
| `src / test / script / out` | 约定值 | 见上节 |
| `libs = ["node_modules", "lib"]` | 双重扫描 | `remappings.txt` 已指向 `node_modules`；此处冗余是兜底 |
| `fs_permissions` | 读 `./` + `node_modules`，读写 `./test/fixtures` | 让 forge 能在测试中读 JSON 配置 |
| `auto_detect_solc` | `false` | 强制锁定 0.8.24 |
| `verbosity` | `2` | 失败时打印 revert reason |
| `[invariant]` | `runs=32, depth=64, fail_on_revert=false` | CI 友好 |
| `[fmt]` | `line_length=100, tab_width=4, bracket_spacing=false, int_types="long", quote_style="double", number_underscore="thousands"` | 与 `.claude/rules/coding-style.md` 对齐 |
| `[profile.ci]` | `deny_warnings = true` | `FOUNDRY_PROFILE=ci` 时 warning = error |
| `[profile.mainnet]` | `eth_rpc_url = "${MAINNET_RPC_URL}"` | 仅 mainnet 部署时显式启用 |

### 3.1 依赖映射（`remappings.txt`）

```text
forge-std/=node_modules/forge-std/src/
@openzeppelin/contracts/=node_modules/@openzeppelin/contracts/
```

- 不使用 `forge install`，没有 `.gitmodules`，没有 `lib/` 真依赖。
- OpenZeppelin 与 forge-std 由 pnpm 装到 `packages/contracts/node_modules`。
- Solidity 内 `import` 一律用 `forge-std/Test.sol` /
  `@openzeppelin/contracts/token/ERC20/IERC20.sol`，**不要**写
  `../../node_modules/...`。

---

## 4. 合约接口（速查）

### 4.1 `CourseMarket`（核心）

```solidity
interface ICourseMarket {
    function buyCourse(bytes32 courseKey, uint256 expectedAmount, bytes16 intentId) external;
    function configureCourse(bytes32 courseKey, address token, uint256 amount, uint256 priceVersion) external;

    event CourseConfigured(bytes32 indexed courseKey, address token, uint256 amount, uint256 priceVersion);
    event CoursePurchased(
        bytes32 indexed courseKey, address indexed buyer, address token,
        uint256 amount, bytes16 intentId, uint256 priceVersion
    );
}
```

继承：`Ownable` + `Pausable` + `ReentrancyGuard`。`buyCourse` 严格 CEI，
同 `(buyer, courseKey)` 防重购。详见
[CourseMarket.sol](src/CourseMarket.sol) 文件头注释。

**`bytes16 intentId` 必须 = UUID 高 128 位**（与前端
[checkout/derive.ts](../../apps/web/src/features/checkout/derive.ts) 、
API [order.Service](../../apps/api/internal/order/order.go) 、
worker [chain.Decode](../../apps/worker/internal/chain/decoder.go) 同步）。

### 4.2 `YDToken`

- 标准 ERC-20 + `AccessControl`（`MINTER_ROLE / BURNER_ROLE`）+ `Capped`（1B cap）；
- `mint` 仅 `MINTER_ROLE`；`burn` 仅 `BURNER_ROLE` 或 `holder` 自烧；
- 精度 18 位；
- 事件 `Transfer / Approval / Minted / CapSet / Role*`；
- 参见 [YDToken.sol](src/YDToken.sol)。

### 4.3 `SepoliaYDSale`

- SepoliaETH → YD 测试兑换；
- 维护一个固定 `ydPerEth`（非 oracle 实时喂价）；
- 接收 ETH → 转出 YD；建议 treasury 多签收 ETH；
- 仅 Sepolia 测试用，正式生产需替换为带 oracle 的版本（见
  ADR-0004 / 0007）。

### 4.4 `ChainlinkPriceOracle`

- 兼容 `AggregatorV3Interface`（`latestRoundData / decimals / description / version`）；
- 在 adapter 上层加 `staleness` 校验（`updatedAt + heartbeatSeconds < now` revert）；
- 用于前端展示 ETH/USD 参考价，**当前不参与实际汇率**（SepoliaYDSale 走固定汇率）。

### 4.5 `CertificateNFT`

- soulbound：默认禁用转账与 setApprovalForAll（除非合约明确开启）；
- `mint(to, courseId, tokenURI)` 仅 `MINTER_ROLE`（worker signer）；
- 事件 `Transfer / Approval / CertificateMinted / Role*`；
- 配套接口 `ICertificateNFT`。

### 4.6 `Notepad`（demo）

```solidity
struct Note {
    uint256 id;          // 1-based，单调递增，永不复用
    string  title;       // ≤ 64 字节
    string  body;        // ≤ 1024 字节
    uint64  createdAt;
    uint64  updatedAt;
}
```

读公开、写仅本人。删除走 swap-and-pop。**前端必须按 `id` 升序展示**
（数组存储顺序会被 swap 改）。详见 [docs/ARCHITECTURE.md §5.2](../../docs/ARCHITECTURE.md)。

---

## 5. 编译 / 测试 / 覆盖率

```bash
# 安装依赖（一次性）
pnpm install

# 编译
pnpm contracts:compile            # 等价：forge build
# CI 模式（warning → error）
FOUNDRY_PROFILE=ci forge build

# 跑全部测试
pnpm contracts:test               # 等价：forge test --threads 1

# 单合约 / 单函数
forge test --match-contract CourseMarket
forge test --match-test test_BuyCourse

# Fuzz / invariant
forge test --match-contract CourseMarket --fuzz-runs 1000
forge test --match-contract CourseMarketInvariant

# 详细输出
forge test -vvv

# 覆盖率（行）
pnpm contracts:test
forge coverage
# HTML
forge coverage --report lcov

# 格式
forge fmt                         # 写
forge fmt --check                 # 只检查（CI）
```

### 5.1 约束

- **forge fmt 必须 0 diff**（CI 必跑 `forge fmt --check`）；
- **forge test 必须 0 failure**（CI 必跑）；
- 覆盖率目标：**生产合约 ≥ 90% 行覆盖**；
- 失败用例必须 `vm.expectRevert(Contract.Selector.selector)`；不要 `try/catch` 吞错。

### 5.2 Fuzz / Invariant

- 默认 fuzz = 256 runs；核心数值函数建议 ≥ 1000（覆盖 `expectedAmount` /
  `priceVersion` 边界）。
- Invariant suite 默认 `runs=32, depth=64`；CI 控制总耗时；
  业务复杂时可改用 `forge test --invariant-runs 256` 提覆盖。

---

## 6. 部署脚本（`script/`）

### 6.1 部署列表

| 命令 | 脚本 | 用途 |
|---|---|---|
| `pnpm contracts:deploy:sepolia` | `DeployCounter.s.sol` | Counter demo |
| `pnpm contracts:deploy:notepad:sepolia` | `DeployNotepad.s.sol` | Notepad demo |
| `pnpm contracts:deploy:course-market:sepolia` | `DeployCourseMarket.s.sol` | F03 课程市场 |
| `pnpm contracts:deploy:yd-token:sepolia` | `DeployYDToken.s.sol` | F05 YDToken |
| `pnpm contracts:deploy:certificate-nft:sepolia` | `DeployCertificateNFT.s.sol` | F04 证书 NFT |
| `pnpm contracts:deploy:oracle:sepolia` | `DeployPriceOracle.s.sol` | ETH/USD 参考价 |
| `pnpm contracts:deploy:oracle:anvil` | `DeployTestOracle.s.sol` | 本地 Anvil 测试桩 |
| `pnpm contracts:verify:sepolia` | `forge verify-contract` | 单独补验证 |

每个 `deploy:*:sepolia` 都同步提供 `*:sepolia:no-verify` 版本（验证已通过或本地
调试时省 API key）。

### 6.2 CourseMarket 部署模式

`DeployCourseMarket.s.sol` 支持两种模式：

- **Mode A（默认）**：只部署空 `CourseMarket`；课程随后由 owner 配置。
- **Mode B**：通过 `COURSES_CONFIG_PATH` 指向 JSON，部署后逐条
  `configureCourse`。

```bash
# Mode A
forge script script/DeployCourseMarket.s.sol:DeployCourseMarket \
    --rpc-url $SEPOLIA_RPC_URL --broadcast --verify -vvvv

# Mode B
export COURSES_CONFIG_PATH=./courses.json
forge script ... --broadcast --verify
```

JSON 形状（每条）：`{ courseKey, token, amount, priceVersion }`，详见脚本文件头注释。

### 6.3 Sepolia 部署前置

`packages/contracts/.env` 必须填：

```bash
SEPOLIA_RPC_URL=https://eth-sepolia.g.alchemy.com/v2/<KEY>
ETHERSCAN_API_KEY=<KEY>
DEPLOYER_PRIVATE_KEY=0x<hex>          # 专用热钱包，非主仓
```

部署账户至少 0.05 SepoliaETH（[faucet](https://sepoliafaucet.com/)）。

### 6.4 部署后必做

1. `forge script --verify` 自动调 Etherscan `verifysourcecode`，确认
   Verified 状态变绿。
2. 保留 `broadcast/<chainid>/run-latest.json` 留档——**commit 进仓库**。
   重新部署前把旧 `run-latest.json` 重命名为 `run-2026-08-13.json`。
3. 把控制台输出的地址登记到
   [apps/web/src/contracts/deployments.ts](../../apps/web/src/contracts/deployments.ts)
   （通过对应 `VITE_*` env）。
4. 在 [docs/DEPLOYMENTS.md](../../docs/DEPLOYMENTS.md) 留一行部署档案。

---

## 7. ABI 导出（与前端桥）

ABI 不会出现在 Solidity 源里；它来自 `forge build` 产物
`out/<Contract>.sol/<Contract>.json`。

```bash
# 单次导出（默认 = Counter / Notepad / CourseMarket / YDToken / CertificateNFT / ChainlinkPriceOracle）
pnpm contracts:export:abi

# 子集导出
node script/export-abi.mjs CourseMarket
```

工作流：

```text
.sol 源码
  → forge build                           # 生成 out/<Contract>.sol/<Contract>.json（含 ABI）
  → script/export-abi.mjs                 # 抽 ABI → apps/web/src/contracts/<name>.abi.ts
                                          # 形如 export const courseMarketAbi = [...] as const
  → apps/web/src/contracts/deployments.ts # 手填 chain → address
  → apps/web/src/components               # 通过 wagmi hooks 使用
```

- **永远不要手动编辑 `*.abi.ts`**；改合约后必须重新 `export:abi`。
- 新增合约后把名字加进 `package.json` 的 `export:abi` 行。
- 部署地址**手填**——`export-abi.mjs` 不动 `deployments.ts`，避免脚本覆盖
  你刚贴进去的地址。

### 7.1 topic0 计算（`compute-topics.mjs`）

离线算 `keccak256("EventName(type,...)")` 取前 4 字节 hex；在跑测试 / e2e
前用来对 worker 的解码做断言。SSOT 见
[packages/shared/src/events](../../packages/shared/src/events/)。

---

## 8. 安全约束（合约层）

来自 `.claude/rules/smart-contract.md`：

- [ ] 重入：所有 `external` 函数涉及 ETH / 状态写入遵循 CEI；本仓库 CourseMarket
      已 `nonReentrant` + `whenNotPaused`。
- [ ] 整数：Solidity 0.8 checked；`unchecked` 必须有 `// unchecked-safety: <理由>`
      注释（当前仓库 0 处）。
- [ ] 权限：`Ownable` 做管理员；普通用户权限走 `AccessControl`（YDToken /
      CertificateNFT）。
- [ ] 输入校验：地址非零、数值上下界、数组长度显式检查。
- [ ] 自定义错误优先于 `require(string, ...)`；所有自定义错误类型化命名（见
      `CourseMarket.NotConfigured / AlreadyPurchased / AmountMismatch`）。
- [ ] NatSpec：`@title / @notice / @dev` 在 `public / external` 函数必填。
- [ ] 事件：所有状态变更必须 emit；`indexed` 标注过滤字段（`owner / courseKey / buyer`）。
- [ ] ETH 收发：使用 `Call / DelegateCall` 谨慎；优先 pull over push。
- [ ] Oracle / 外部合约：返回值必校验（`ChainlinkPriceOracle` 做 staleness 校验）。
- [ ] Gas：循环尽量避免 unbounded；mapping 优于 array 用于查找。
- [ ] 升级性：默认**不可升级**；如需升级显式引入 UUPS + 24h timelock。
- [ ] 反模式（直接拒绝）：`tx.origin` 鉴权、`block.timestamp` 作随机源、
      `selfdestruct`、低层 call 不检查返回值、构造函数里 `transfer` ETH。

---

## 9. 与其他模块的契约

### 9.1 SSOT

| 项 | 合约（这里） | 前端 | 后端 API | Worker |
|---|---|---|---|---|
| 事件 topic0 | `CoursePurchased(bytes32,address,address,uint256,bytes16,uint256)` | `apps/web/src/contracts/courseMarket.abi.ts` | `@x-web3/shared/events` | `internal/chain/decoder.go` |
| `courseKey` 算法 | 当 mapping key，不验内容 | `sha256(uuid)` | `sha256(uuid)` | 与事件 ABI 解码对齐 |
| `intentId` 字段 | `bytes16`（高 128 位） | `uuidToBytes16` 取高 128 位 | 颁发 UUID | `uuid.FromBytes(event.IntentID[:])` |
| `Ownable` 角色 | 部署者；多签接管 | — | 配置课程时校验 | — |
| `MINTER_ROLE` | 仅 worker signer | — | 颁证书时不下发 tx | 持有私钥 / KMS key |

### 9.2 链 ID

| 链 | chain id | 用途 |
|---|---|---|
| Sepolia | `11155111` | 生产测试 |
| Anvil | `31337` | 本地开发 |

前端常量来自
[apps/web/src/chains.ts](../../apps/web/src/chains.ts) +
[@packages/shared/src/chains/registry.ts](../../packages/shared/src/chains/registry.ts)。

---

## 10. 测试组织

### 10.1 命名与覆盖

- 测试文件：`packages/contracts/test/<Name>.to.s.sol`。
- 测试函数：`test_<场景>_<期望>`（如 `test_BuyCourse_RevertsOnAlreadyPurchased`）。
- 失败用例必须显式断言：`vm.expectRevert(CourseMarket.AlreadyPurchased.selector)`。
- Fuzz 默认 256 runs；核心数值函数建议 ≥ 1000。
- 覆盖率目标：生产合约 ≥ 90% 行。

### 10.2 部署脚本测试

`test/DeployCourseMarketScript.t.sol` / `DeployCertificateNFTScript.t.sol` /
`DeployYDToken.t.sol` 用 `vm.readFile` + cheatcode 模拟「deploy + 配置」
全链路，确保部署脚本在 anvil / Sepolia 都跑得通。

### 10.3 Mock 与 fixtures

- 测试桩 [mocks/MockV3Aggregator.sol](src/mocks/MockV3Aggregator.sol)：
  Chainlink V3 Aggregator 的最小可编程实现，可手动 push round data。
- 测试 fixtures 写在 [test/fixtures/](test/fixtures/)（forge 写权限），
  供 Mode B 部署脚本共享。

---

## 11. 常用命令汇总

```bash
# 一次性
pnpm install

# 日常
pnpm contracts:compile
pnpm contracts:test
pnpm contracts:test -- --match-contract CourseMarket
pnpm contracts:export:abi

# 部署
pnpm contracts:deploy:notepad:sepolia
pnpm contracts:deploy:course-market:sepolia
pnpm contracts:deploy:yd-token:sepolia
pnpm contracts:deploy:certificate-nft:sepolia
pnpm contracts:verify:sepolia

# 质量
forge fmt
forge fmt --check
forge coverage

# 文档（forge doc）
forge doc
```

---

## 12. 进一步阅读

- 全局架构：[docs/ARCHITECTURE.md](../../docs/ARCHITECTURE.md)
- 工具链总览：[docs/TOOLCHAIN.md](../../docs/TOOLCHAIN.md)
- 部署档案：[docs/DEPLOYMENTS.md](../../docs/DEPLOYMENTS.md)
- 智能合约规则：[../../.claude/rules/smart-contract.md](../../.claude/rules/smart-contract.md)
- 合约 ↔ 前端 ABI 桥：[docs/ARCHITECTURE.md §3](../../docs/ARCHITECTURE.md)
- 链回放 runbook：[docs/runbooks/chain-replay.md](../../docs/runbooks/chain-replay.md)
- 签名轮换：[docs/runbooks/signer-rotation.md](../../docs/runbooks/signer-rotation.md)
- 共享事件 SSOT：[packages/shared/src/events](../../packages/shared/src/events/)
- 前端：[apps/web](../web/README.md)
- API：[apps/api](../api/README.md)
- Worker：[apps/worker](../worker/README.md)