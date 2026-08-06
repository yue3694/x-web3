# Contracts Tour — guided walkthrough

> 第一次接触 Foundry + Solidity？这份导览按"应该按什么顺序读"组织文件，
> 每一步告诉你**为什么**要看这一段、**重点**看什么、**陷阱**在哪里。
>
> 配套源码已重度注释——本文件不重复注释里的内容，而是指出**路径**与
> **关注点**。

---

## 0. 准备工作

```bash
# 一次性
curl -L https://foundry.paradigm.xyz | bash && foundryup

# 在仓库根
cd /Users/huyi/Documents/Coding/github/x-web3
pnpm install

# 装子模块（Counter 依赖 OZ，Notepad.t.sol 依赖 forge-std）
cd packages/contracts
forge install OpenZeppelin/openzeppelin-contracts --no-commit
forge install foundry-rs/forge-std --no-commit

# 跑一遍测试，确认环境正常
forge test
# 预期：17 passed (Counter 5 + Notepad 12 + 1 fuzz 在 Notepad 内)
```

如果失败，回到仓库根 `docs/ARCHITECTURE.md` §6 排查。

---

## 1. 阅读顺序

按这个顺序读，每个文件 5–15 分钟：

```
  foundry.toml                ← 编译器配置（5 min）
       ↓
  remappings.txt              ← import 路径映射（3 min）
       ↓
  src/Counter.sol             ← 最小教学合约（30 min）
       ↓
  src/Notepad.sol             ← 项目核心合约（45 min）
       ↓
  test/Counter.t.sol          ← forge-std 测试入门（20 min）
       ↓
  test/Notepad.t.sol          ← 复杂测试 + fuzz（30 min）
       ↓
  script/DeployCounter.s.sol  ← 部署脚本结构（15 min）
       ↓
  script/DeployNotepad.s.sol  ← 最小化部署脚本（10 min）
       ↓
  script/export-abi.mjs       ← ABI 导出（10 min，可选）
```

---

## 2. 逐文件导览

### 2.1 `foundry.toml` — 编译器配置

**重点看**：
- `[profile.default]` 下的 `solc_version = "0.8.24"` —— **必须**与所有 .sol 文件的 `pragma solidity ^0.8.24;` 严格一致。
- `optimizer = true` 与 `optimizer_runs = 200` —— 影响部署成本 vs 调用成本的折中。
- `via_ir = false` —— 关闭 IR 管线（更快的编译、更直观的栈追踪；某些优化场景需开）。
- `[profile.ci] deny_warnings = true` —— CI profile 把 warning 当 error，方便 catch。
- `[fmt]` —— `forge fmt` 的格式化规则（行宽、tab、缩进、引号风格）。

**陷阱**：
- 改了 `solc_version` 但忘了同步 .sol 文件的 pragma → 编译失败。
- 误把 `[profile.mainnet]` 下的 `eth_rpc_url` 指向 mainnet 但只有 Sepolia ETH → 部署永远 pending。

---

### 2.2 `remappings.txt` — import 路径映射

**重点看**：
```
forge-std/=lib/forge-std/src/
@openzeppelin/contracts/=lib/openzeppelin-contracts/contracts/
```

它告诉编译器："代码里写 `import "forge-std/Test.sol";` 时，去 `lib/forge-std/src/Test.sol` 找"。

**陷阱**：
- 路径写错 → 编译报"Source not found"。
- 路径写对但 `lib/` 下没装子模块 → 同样错。
- 多个 remappings 互相冲突 → 静默使用其中之一，难以察觉。

---

### 2.3 `src/Counter.sol` — 最小教学合约

**重点看**（按文件内注释的 §1–§10 编号）：
- §1 `// SPDX-License-Identifier: MIT` —— 必须出现在文件首行（注释也可以），编译期检查。
- §2 `pragma solidity ^0.8.24;` —— 见上。
- §3 `import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";` —— remapping 生效示例。
- §4 `/// @title / @notice / @dev` —— NatSpec。`forge doc` 可以基于这些生成 Markdown 文档。
- §5 `uint256 public count;` —— 自动 getter；`public` vs `private` vs `internal` 的取舍。
- §6 `event Increment(address indexed by, uint256 newCount);` —— `indexed` 字段可以过滤。
- §7 `error Underflow();` —— 自定义错误比 `require(cond, "string")` 省 gas。
- §8 `constructor(address initialOwner) Ownable(initialOwner) {}` —— OZ v5 必须显式传 owner（v4 默认是 `msg.sender`）。
- §9 `function increment() external { ... }` —— `external` 仅外部调用；与 `public` 的取舍。
- §10 `function reset() external onlyOwner { ... }` —— 修饰器把权限检查写在外面。

**推荐练习**：
1. 给 Counter 加一个 `multiply(uint256 n)` 函数，自己写测试验证。
2. 给 Counter 加一个 `decrementBy(uint256 n)` 函数（不能减成负数）。
3. 看 OpenZeppelin 源码 `lib/openzeppelin-contracts/contracts/access/Ownable.sol`，了解 `onlyOwner` 的实现。

**陷阱**：
- 改 `public` 到 `private` 时记得 ABI 不再有 getter。
- `error` 加参数时（`error X(uint256 a)`）测试断言要用 `abi.encodeWithSelector`。

---

### 2.4 `src/Notepad.sol` — 项目核心合约

**重点看**（按 §1–§7 编号）：
- §1 **限制常量** —— `MAX_TITLE_LEN / MAX_BODY_LEN / MAX_NOTES_PER_USER` 都是 `public constant`，前端 ABI 可以读到。
- §2 **Note 结构体** —— 5 字段；`uint64` 时间戳的选型理由在注释里。
- §2 末段 **存储布局** —— `mapping(address => Note[])` 的 gas / RPC 体积权衡。
- §3 **事件** —— `indexed owner + indexed id` 是 subgraph 过滤的天然维度。
- §4 **错误** —— 4 个自定义错误，全部 4 字节 selector。
- §5 `createNote` —— **CEI 顺序** 的样板：checks → effects → emit。注意无 external call，所以"interactions"为空。
- §5 `updateNote` —— 同样 CEI；额外演示"`_loadOwned` 内部对 id 校验抛错"。
- §5 `deleteNote` —— **swap-and-pop** 是最关键的设计点。重点看注释里的图示。
- §6 读函数 —— 都是 `view`，链下调零 gas。
- §7 内部辅助 —— `_checkTitle` 用 `bytes(s).length` 而不是 `s.length`（UTF-8 字节数 vs 字符数）。

**推荐练习**：
1. 把 `_indexOf` 改成二分查找（数组已按 id 升序？注意：storage 顺序是 swap-and-pop 后的顺序，不一定有序——先要排序）。
2. 加一个 `transferNote(address to, uint256 id)` 函数，把笔记所有权转给另一个地址（注意事件、存储清理）。
3. 加一个 `getNotesPaginated(address owner, uint256 offset, uint256 limit)` 函数，给未来解除 50 条上限留接口。

**陷阱**：
- swap-and-pop 后 id 不复用 → 任何"按数组下标"的假设都要推翻。**前端必须按 id 排序**。
- `bytes(s).length` 计的是 UTF-8 字节，不是字符 → 标题允许中文 / emoji 时不要想当然。

---

### 2.5 `test/Counter.t.sol` — forge-std 入门

**重点看**：
- `import {Test} from "forge-std/Test.sol";` —— 测试基类。
- `address internal alice = makeAddr("alice");` —— 用 `makeAddr` 而不是硬编码 `address(0xBEEF)`。
- `function setUp() public { counter = new Counter(owner); }` —— 每个 test 前自动跑一次。
- `vm.prank(alice);` —— 把下一次调用的 `msg.sender` 换成 alice。
- `vm.expectRevert(Counter.Underflow.selector);` —— 断言下一次调用 revert。
- `vm.warp(ts);` —— 操纵 `block.timestamp`。
- `test_InitialCountIsZero() public view { ... }` —— `view` 测试可标记为只读（forge 会尝试并行）。

**推荐练习**：
1. 用 `console.log` 在 `setUp` 后打印 `counter` 地址。
2. 加一个 `test_IncrementFuzz` 函数 `testFuzz_Increment(uint8 n)`，验证连续调用 n 次后 count == n。
3. 看 forge-std 源码 `lib/forge-std/src/Test.sol`，了解所有 cheatcode。

**陷阱**：
- `vm.expectRevert` 必须在被测调用**之前**调用——放反了就 miss。
- `vm.prank` 只影响**下一次**调用。多次调用要用 `vm.startPrank / vm.stopPrank`。

---

### 2.6 `test/Notepad.t.sol` — 复杂测试 + fuzz

**重点看**：
- 5 + 3 + 3 + 2 = 13 个 `test_*` 函数覆盖所有公开 API 路径。
- `test_DeleteNote_SwapsAndPops_PreservesOtherIds` —— 这是整个合约最重要的不变量测试，对应 `src/Notepad.sol::deleteNote` 注释里的图示。
- `test_DeleteNote_EmitsNoteDeleted` —— `vm.expectEmit(true, true, false, true)` 比对事件 topics + data 的样板。
- `testFuzz_CreateAndUpdate(string calldata title, string calldata body)` —— `testFuzz_` 前缀 + `vm.assume` 限制输入。

**推荐练习**：
1. 加 `invariant test_IdsAreMonotonic()` —— 不变量测试（forge 会尝试在随机调用序列里找反例）。
2. 把 `test_GetNotes_ReturnsAllInOrder` 改成"先 swap-and-pop 一次，再 assert 排序后的结果"。

**陷阱**：
- `vm.expectEmit` 的 4 个 bool 参数含义：`topic1, topic2, topic3, data` —— 选错会误报。
- `vm.assume` 不剔除的输入会重复出现 → 想剔除的逻辑要明确写在第一个 assume。
- fuzz 测试默认 256 runs；要更稳：`forge test --fuzz-runs 10000`。

---

### 2.7 `script/DeployCounter.s.sol` & `DeployNotepad.s.sol` — 部署脚本

**重点看**：
- `import {Script, console2} from "forge-std/Script.sol";` —— 与 Test 不同。
- `vm.envUint("DEPLOYER_PRIVATE_KEY")` —— 自动从 `.env` 读取。
- `vm.addr(privateKey)` —— 私钥反推地址，便于打印。
- `vm.startBroadcast(pk) / vm.stopBroadcast()` —— 配对使用，标记"广播区间"。
- `console2.log` —— 脚本用 console2，测试用 console（细微差别）。

**部署三态**：
- **Dry-run**（默认）：跑脚本但不广播。需要 `--broadcast` 才真正发交易。
- **Broadcast only**：`--broadcast`，交易上链但不验证。
- **Broadcast + Verify**：`--broadcast --verify`，上链 + 自动提交 Etherscan。

**推荐练习**：
1. 把 DeployNotepad 改成接受构造函数参数（如初始化 owner 限制名单）。
2. 在 DeployNotepad 里加一个"部署后立即调用一次 `createNote` 写入欢迎语"的逻辑，验证全流程。

**陷阱**：
- 缺 `.env` 时 `vm.envUint` 会 revert，错误信息有指引。
- `vm.startBroadcast` 没有 `stopBroadcast` 收尾 → 后续所有合约调用都被广播，**很危险**。

---

### 2.8 `script/export-abi.mjs` — ABI 导出（可选）

**重点看**：
- Node.js 脚本，不在 Solidity 工具链内。
- 读 `out/<Name>.sol/<Name>.json` → 写 `apps/web/src/contracts/<name>.abi.ts`。
- `as const` 的关键作用：把 ABI 字面量保留为 narrow 类型，让 wagmi 能推断参数类型。
- 默认参数 `[Counter, Notepad]` —— 不传则导出全部；想导子集就 `node export-abi.mjs Notepad`。
- **不再覆盖 `deployments.ts`** —— 之前的版本每次跑会清空人手填的地址，已修。

**推荐练习**：
1. 加一个 `--watch` 模式：监听 `out/` 目录变化，触发自动导出。
2. 加一个 `--check` 模式：CI 时检查 ABI 是否与最新合约一致。

**陷阱**：
- ABI 文件必须经过此脚本生成，**不要**手写。手写 ABI 类型与合约不一致时 wagmi 会报奇怪的错误。

---

## 3. 实战里程碑

按顺序跑通以下 6 步，你就掌握了整个项目：

### 里程碑 1：环境跑通

```bash
forge test --match-contract CounterTest
# 5 个测试应全绿
```

### 里程碑 2：理解 Notepad 核心不变量

读 `src/Notepad.sol::deleteNote` 的注释和图示，然后**手动推演**：

```
3 条笔记:  [{id:1}, {id:2}, {id:3}]
删除 id=2 之后：______？
再删除 id=1 之后：______？
再创建一条新笔记后：______？(id 应该是几？)
```

答案在 `test_DeleteNote_SwapsAndPops_PreservesOtherIds` 里。

### 里程碑 3：本地 dry-run 部署

```bash
anvil &
forge script script/DeployNotepad.s.sol:DeployNotepad \
    --rpc-url http://127.0.0.1:8545 \
    --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
```

应该看到 `Notepad deployed at: 0x...`。

### 里程碑 4：导出 ABI 并接入前端

```bash
cd ../..
pnpm contracts:export:abi
# 看 apps/web/src/contracts/notepad.abi.ts，应该被填满了
```

### 里程碑 5：Sepolia 真链部署

按 `docs/ARCHITECTURE.md` §4 走完整流程。

### 里程碑 6：浏览器端到端验证

```bash
pnpm dev
# http://localhost:5173 — MetaMask + Sepolia — 创建 / 编辑 / 删除
```

走完 6 步就算入门了 🎓。

---

## 4. 推荐外部阅读

| 主题 | 链接 |
|------|------|
| Foundry Book（权威） | <https://book.getfoundry.sh/> |
| Solidity 0.8 文档 | <https://docs.soliditylang.org/en/v0.8.24/> |
| OpenZeppelin Contracts | <https://docs.openzeppelin.com/contracts/5.x/> |
| wagmi v2 文档 | <https://wagmi.sh/react/api/hooks> |
| viem 文档 | <https://viem.sh/> |
| EIP-1967（代理存储槽） | <https://eips.ethereum.org/EIPS/eip-1967> |
| Foundry 安全 cheatsheet | <https://www.rareskills.io/post/foundry-tutorial> |

---

## 5. 常见疑问 FAQ

**Q: 为什么 foundry.toml 里 `solc_version = "0.8.24"` 而 .sol 文件写 `^0.8.24`？**
A: 前者锁定实际编译器版本（`forge` 不会自己挑）；后者声明合约源代码兼容范围（0.8.24–0.9.0）。两者**必须**保持 `^solc_version`，否则编译失败。

**Q: 为什么 Notepad 不继承 OZ 的 `Ownable`？**
A: Notepad 没有"管理员"概念——每个用户都是自己的管理员。把权限直接嵌入 `mapping(address => Note[])` 而不是用 OZ role 体系，更贴合数据模型。

**Q: 为什么 id 不用 `uint64`？**
A: id 上限是用户一辈子可能创建的笔记数（几十到几百），uint64 完全够用。但保留 `uint256` 是因为 (a) EVM 字面宽度更省 gas，(b) ABI 一致性。

**Q: 为什么 `deleteNote` 不标记数组最后一个 slot 为零？**
A: `pop()` 已经让 `length` 减 1，EVM 会自动回收尾 slot——这是 storage layout 的天然机制。手动清零反而多一次 SSTORE。

**Q: 为什么 swap-and-pop 而不是直接删？**
A: 直接删（`delete list[i]`）会把 slot 清零但 length 不变；之后 push 进去的元素会填到这个空 slot，导致 `id` 与下标失同步。swap-and-pop 保持 length 与下标的对应关系。

**Q: 怎样把 Notepad 升级成带分页的版本？**
A: 加 `getNotesPaginated(address, uint256 offset, uint256 limit)` view 函数；前端按需调用。**不要**用 storage 数组的下标当游标——swap-and-pop 会让下标错位。改用 id-based cursor。