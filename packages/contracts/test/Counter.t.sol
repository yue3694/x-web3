// SPDX-License-Identifier: MIT
// ============================================================================
//  Counter.t.sol  ——  Counter.sol 的 forge-std 单元测试
// ----------------------------------------------------------------------------
//  Foundry 把测试也写在 Solidity 里（不是 JS / TS）。原因：
//
//    - 测试代码与合约共享编译器，能 100% 复现部署后的字节码；
//    - 用 `vm.*` cheatcodes 直接操纵 EVM 状态（时间、msg.sender、revert
//      期望等），比 Hardhat 的 fixture / loadFixture 模型更直接；
//    - 内置 fuzz / invariant 测试，只要把测试函数参数化即可；
//    - 跑得快（Rust）。
//
//  本文件覆盖 Counter 的：
//    1) 初始状态（count == 0）
//    2) increment
//    3) decrement（含 underflow revert）
//    4) reset（onlyOwner）
//
//  阅读顺序：自上而下，每一段都附解释。
// ============================================================================

// 版本锁——必须与被测合约一致。
pragma solidity ^0.8.24;

// ---------------------------------------------------------------------------
//  forge-std 的 `Test` 合约提供所有测试基础设施：
//
//    - assertEq / assertGt / assertTrue 等断言；
//    - vm.* cheatcodes（vm.prank / vm.warp / vm.expectRevert / vm.deal ...）；
//    - setUp() 钩子在每个 test 函数前自动跑一遍；
//    - 暴露 `console.log`（注意：脚本里要用 console2.log）。
//
//  `remappings.txt` 把 `forge-std/` 映射到 `lib/forge-std/src/`。
//  首次跑测试前需要 `forge install foundry-rs/forge-std --no-commit`。
// ---------------------------------------------------------------------------
import {Test} from "forge-std/Test.sol";

// 引入被测合约。注意相对路径：测试文件与合约文件并列，都位于 test/ 与 src/。
import {Counter} from "../src/Counter.sol";

// OZ 的 Ownable 用于断言 `OwnableUnauthorizedAccount` 自定义错误的 selector。
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

// ===========================================================================
//  测试合约：必须 is Test，才有 vm.* cheatcodes 与断言宏。
//  命名习惯：`XxxTest`（被测合约名 + Test）。
// ===========================================================================
contract CounterTest is Test {
    // -------------------------------------------------------------------------
    //  被测合约实例 + 测试用地址
    //
    //  `internal`：测试不需要从外部访问——只在本测试合约内用。
    //
    //  `makeAddr("owner")` 是 forge-std 的 helper：在测试地址空间里分配一
    //  个确定的、人类可读的地址（私钥永远不可知，等价于一个"假账户"）。
    //  比 `address(0xBEEF)` 这种魔术地址可读得多，也避免了与真实地址碰巧
    //  撞车的尴尬。
    // -------------------------------------------------------------------------
    Counter internal counter;
    address internal owner = makeAddr("owner");
    address internal alice = makeAddr("alice");

    // -------------------------------------------------------------------------
    //  setUp()：每个 test_* 函数执行前都会跑一遍。
    //  这是 forge-std 的约定（名字必须叫 `setUp`）。
    //
    //  把合约部署写在这里：
    //    - 每个 test_* 拿到一个全新的、互不污染的合约实例；
    //    - 失败时容易定位（是 setUp 挂了还是 test 本身挂了）。
    // -------------------------------------------------------------------------
    function setUp() public {
        // 部署时把 owner 设为我们之前 makeAddr 的 owner。
        // 注意：这里的 msg.sender 是测试合约本身（CounterTest 地址），
        // 但构造函数要求显式传 `initialOwner`，所以传哪个都无所谓——不是
        // msg.sender。
        counter = new Counter(owner);
    }

    // =========================================================================
    //  测试用例
    // =========================================================================
    //
    //  命名约定（项目规则见 .claude/rules/testing.md）：
    //    test_<动作>_<期望>           —— 正常路径
    //    test_<动作>_Reverts<错误>    —— 失败路径
    //
    //  每个函数都是 `public`（不是 `external`）——这是 forge-std 要求的，
    //  它通过 selector + 反射找到所有 test_* 函数。
    // =========================================================================

    /// @notice 部署后初始状态应为 0。
    function test_InitialCountIsZero() public view {
        // assertEq(actual, expected, [optional msg])
        // `view` 修饰：本测试不写状态，可以并行执行（forge 默认串行，但
        // `view` 测试在更高级别的优化下可以并发）。
        assertEq(counter.count(), 0);
    }

    /// @notice increment 应使 count +1，连续调用累加。
    function test_Increment() public {
        counter.increment();
        assertEq(counter.count(), 1);

        counter.increment();
        assertEq(counter.count(), 2);
    }

    /// @notice decrement 在 count > 0 时正常减 1。
    function test_Decrement() public {
        // 准备数据
        counter.increment();
        counter.increment();
        // 测
        counter.decrement();
        assertEq(counter.count(), 1);
    }

    /// @notice decrement 在 count == 0 时应当 revert `Underflow`。
    /// @dev    用 `vm.expectRevert(selector)` 在**下一次调用前**断言 revert。
    ///         selector 是错误类型的 4 字节 keccak256 哈希。
    ///         写法：`Counter.Underflow.selector`（Solidity 0.8.4+ 自动
    ///         给自定义错误生成 selector）。
    function test_DecrementRevertsOnUnderflow() public {
        // vm.expectRevert 必须在被测调用**之前**调用。
        vm.expectRevert(Counter.Underflow.selector);
        counter.decrement();
    }

    /// @notice reset 只允许 owner 调用。
    /// @dev    这里演示两个 cheatcode：
    ///           - vm.prank(addr)：把下一次调用的 msg.sender 改为 addr；
    ///           - vm.expectRevert(abi.encodeWithSelector(...))：断言带参数的
    ///             自定义错误 revert。Ownable v5 的错误是
    ///             `OwnableUnauthorizedAccount(address account)`，需要传
    ///             实际调用者地址。
    function test_ResetOnlyOwner() public {
        counter.increment(); // 准备数据：count = 1

        // 1) 非 owner 调用 → revert
        vm.prank(alice); // 接下来 1 次调用的 msg.sender = alice
        vm.expectRevert(
            abi.encodeWithSelector(
                Ownable.OwnableUnauthorizedAccount.selector,
                alice // 错误里要带的参数：哪个账户被拒
            )
        );
        counter.reset();

        // 2) owner 调用 → 成功
        vm.prank(owner);
        counter.reset();
        assertEq(counter.count(), 0);
    }
}

// ===========================================================================
//  调试技巧
// ----------------------------------------------------------------------------
//   - 在 test 里加 `console.log(counter.count())` —— 来自 forge-std/Test
//     的 `log` 函数（`import "forge-std/console.sol"` 也行）。
//   - 跑详细日志：`forge test --match-test test_Reset -vvvv`（v 数越多
//     越详细）。
//   - 跑覆盖率：`forge coverage`，看哪些分支没被覆盖。
//   - 单测失败时，先看 revert reason 与 trace；再判断是合约 bug 还是测试
//     期望写错了。
// ===========================================================================
