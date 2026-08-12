// SPDX-License-Identifier: MIT
// ============================================================================
//  Counter.sol  ——  最小可用的合约示例（教学用脚手架）
// ----------------------------------------------------------------------------
//  本文件是项目里"第一个会读到的合约"。它的存在不是为了演示业务，而是
//  展示 Foundry 工程里 Solidity 合约的标准结构：
//
//    1) 许可证声明             // SPDX-License-Identifier
//    2) pragma 版本锁          // ^0.8.24
//    3) import（库 / 接口 / 其它合约）
//    4) NatSpec 文档块          // @title @notice @dev
//    5) 状态变量                // storage
//    6) 事件                   // event + indexed 字段
//    7) 自定义错误             // error ...()  取代 require(string, ...)
//    8) 构造函数               // constructor
//    9) 外部 / 公共函数        // external / public
//   10) 修饰器                 // onlyOwner 等
//
//  阅读顺序：自上而下。
// ============================================================================

// `pragma solidity ^0.8.24;` 是版本指令。`^` 表示 "兼容 0.8.24 且小于 0.9.0"。
// 0.8 系列自带整数溢出 / 下溢检查（不写 `SafeMath`），是当前事实标准的最小版本。
pragma solidity ^0.8.24;

// ----------------------------------------------------------------------------
//  引入 OpenZeppelin 的 Ownable。
//  `remappings.txt` 把 `@openzeppelin/contracts/` 映射到本仓库里的子模块，
//  所以这里的 import 不会去 npm 拉包——它从 `lib/openzeppelin-contracts/`
//  读源文件。
//
//  Ownable 给合约加上一个 owner 概念：
//    - constructor 时把 `msg.sender` 设为 owner
//    - 提供 `onlyOwner` 修饰器保护敏感函数
//    - 提供 `transferOwnership` / `renounceOwnership` 转移 / 放弃所有权
//
//  OpenZeppelin Contracts v5.x 是当前主流版本，写在 foundry.toml 的 solc
//  版本下的标准选择。引入前要 `forge install OpenZeppelin/openzeppelin-contracts --no-commit`。
// ----------------------------------------------------------------------------
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

/// @title  Counter
/// @notice 一个最简的、可被 owner 重置的计数合约。仅作教学示例，业务请看 `Notepad.sol`。
/// @dev    本合约演示：
///
///   - 继承 OpenZeppelin 的 `Ownable`；
///   - 使用 `error`（自定义错误）代替字符串 require；
///   - 使用 `event`（带 `indexed` 字段）记录状态变更；
///   - 使用 `onlyOwner` 修饰器限制敏感操作；
///   - 不持有任何 ETH（无 `payable`），因此无须防重入。
///
/// @custom:security-contact  none (示例合约)
// ============================================================================
//  合约本体
// ============================================================================
contract Counter is Ownable {
    // ------------------------------------------------------------------------
    //  状态变量
    // ------------------------------------------------------------------------
    //
    //  存储位置：storage（持久上链，gas 最贵）。
    //  可见性：`public` 自动生成同名的 getter 函数（`count()`）。
    //  类型选择：`uint256` 是 EVM 原生宽度，gas 最低；不需要节省空间时首选。
    //
    //  注意 `private` vs `internal`：storage 变量即便是 `private`，链上数据
    //  仍然是公开可读的（任何人都能用 eth_getStorageAt 读到）。`private` 只
    //  限制其它合约**通过 Solidity 代码**直接访问。多数场景下应当选
    //  `internal`（子合约可访问）或 `private`（仅本合约使用）。
    // ------------------------------------------------------------------------
    uint256 public count;

    // ------------------------------------------------------------------------
    //  事件
    // ------------------------------------------------------------------------
    //
    //  事件是合约向链下"广播日志"的唯一方式。事件数据本身**不可读**——但
    //  它写入交易日志，subgraph / 前端可以用 RPC 节点过滤（`eth_getLogs`）。
    //
    //  `indexed` 字段：最多 3 个，会被写入 topics，效率高、可按值过滤。
    //  非 indexed 字段：作为 ABI 编码写入 data，gas 便宜但不能按值过滤。
    //
    //  这里把 `by` 标 indexed（"谁触发"的常见过滤维度），`newCount` 不标
    //  （"变到多少"通常不在前端热路径上按值过滤）。
    // ------------------------------------------------------------------------

    /// @notice `increment()` 被调用时触发。
    event Increment(address indexed by, uint256 newCount);

    /// @notice `decrement()` 被调用时触发。
    event Decrement(address indexed by, uint256 newCount);

    /// @notice `reset()` 被调用时触发——owner 把 count 归零。
    event Reset(address indexed by);

    // ------------------------------------------------------------------------
    //  自定义错误
    // ------------------------------------------------------------------------
    //
    //  自 0.8.4 起 Solidity 支持自定义错误，比 `require(cond, "string")` 节
    //  省 gas（错误数据比字符串短得多），并且更易于在链下解码。
    //
    //  命名约定：动作 + 失败原因，名词形式。`Underflow()` 比
    //  `CountCannotGoBelowZero()` 简洁。带参数可写成 `error Insufficient(uint256 have, uint256 want);`。
    //
    //  触发：`revert Underflow();` 或 `revert Underflow();`（带参数时
    //  `revert Insufficient(have, want);`）。
    // ------------------------------------------------------------------------
    error Underflow();

    // ------------------------------------------------------------------------
    //  构造函数
    // ------------------------------------------------------------------------
    //
    //  仅在合约部署时执行一次。`initialOwner` 在工厂模式 / UUPS 升级模式
    //  下很重要——把 owner 写成可注入的而不是写死 `msg.sender`。
    //
    //  调用方式：`new Counter(deployerAddress)`。OpenZeppelin v5 的
    //  `Ownable(initialOwner)` 写法要求**显式**传入 owner（v4 时代默认
    //  是 `msg.sender`），这是 v4 → v5 的 breaking change，需要注意。
    // ------------------------------------------------------------------------
    constructor(address initialOwner) Ownable(initialOwner) {}

    // ------------------------------------------------------------------------
    //  外部函数
    // ------------------------------------------------------------------------
    //
    //  `external` 仅能从合约**外部**调用（`this.fn()` 不行），gas 比
    //  `public` 略省，且强制了调用面。本合约没有内部调用此函数的场景，选
    //  `external`。
    //
    //  本函数**不写存储**之外的副作用——也就无须 CEI（checks-effects-
    //  interactions）顺序。完整 CEI 模式参考 `Notepad.sol::createNote`。
    // ------------------------------------------------------------------------

    /// @notice count 自增 1。任何人都能调用。
    /// @dev    0.8 自带 checked 算术，溢出在这里会 revert。
    function increment() external {
        // 1) effects（修改本合约状态）。无外部调用、无交互阶段。
        count += 1;
        // 2) emit（在状态变更后；事件本身不消耗 state 但习惯写在最后）。
        emit Increment(msg.sender, count);
    }

    /// @notice count 自减 1。任何人都能调用，但不允许跌破 0。
    /// @dev    自定义错误在 0.8.4+ 是首选 revert 方式。
    function decrement() external {
        // checks：先校验前置条件。注意 `count == 0` 的字面量写法
        // 编译器会优化为 ISZERO，省 gas。
        if (count == 0) revert Underflow();

        count -= 1;
        emit Decrement(msg.sender, count);
    }

    // ------------------------------------------------------------------------
    //  受限函数（仅 owner）
    // ------------------------------------------------------------------------
    //
    //  `onlyOwner` 是 OpenZeppelin 自带的修饰器。展开后等价于：
    //      require(msg.sender == owner(), "Ownable: caller is not the owner");
    //  但使用自定义错误 `OwnableUnauthorizedAccount(address)`，更省 gas 且
    //  链下解码更明确。
    //
    //  习惯：把仅 owner 可见的敏感操作集中放在合约末尾，方便审计时一眼扫
    //  完所有"特权"能力。
    // ------------------------------------------------------------------------

    /// @notice 把 count 归零。仅 owner 可调用。
    /// @dev    业务上极少使用；保留作 `onlyOwner` 修饰器的演示。
    function reset() external onlyOwner {
        count = 0;
        emit Reset(msg.sender);
    }
}

// ============================================================================
//  阅读建议
// ----------------------------------------------------------------------------
//   1. 对照 OpenZeppelin 的 `Ownable.sol` 源（lib/openzeppelin-contracts/
//      contracts/access/Ownable.sol），看 `onlyOwner` 修饰器的实现。
//   2. 读 `test/Counter.t.sol`，看 `vm.expectRevert(Underflow.selector)`、
//      `OwnableUnauthorizedAccount.selector` 等用法。
//   3. 对比本合约与 `Notepad.sol`——后者不使用 Ownable，而是把权限直接嵌入
//      到 `mapping(address => Note[])` 之中。
// ============================================================================
