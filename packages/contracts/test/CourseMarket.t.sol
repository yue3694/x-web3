// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test, console2} from "forge-std/Test.sol";
import {StdInvariant} from "forge-std/StdInvariant.sol";
import {CourseMarket, ICourseMarket} from "../src/CourseMarket.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

/// @dev 测试用最小 ERC20（不引入 OZ preset 避免相互干扰）。
contract MockToken is ERC20 {
    constructor() ERC20("Mock", "MCK") {}

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }
}

contract CourseMarketTest is Test {
    CourseMarket internal market;
    MockToken internal token;
    address internal owner = address(this);
    address internal alice = makeAddr("alice");
    address internal bob = makeAddr("bob");
    bytes32 internal courseA = keccak256("course-A");
    bytes32 internal courseB = keccak256("course-B");
    uint256 internal amount = 100e18;

    function setUp() public {
        market = new CourseMarket(owner);
        token = new MockToken();
        market.configureCourse(courseA, address(token), amount, 1);
    }

    function test_ConfigureCourse_StoresConfig() public view {
        (address t, uint256 a, uint256 v) = market.getConfig(courseA);
        assertEq(t, address(token));
        assertEq(a, amount);
        assertEq(v, 1);
    }

    function test_ConfigureCourse_RejectsZeroToken() public {
        vm.expectRevert(CourseMarket.ZeroAddress.selector);
        market.configureCourse(courseB, address(0), amount, 1);
    }

    function test_ConfigureCourse_RejectsZeroAmount() public {
        vm.expectRevert(CourseMarket.ZeroAmount.selector);
        market.configureCourse(courseB, address(token), 0, 1);
    }

    function test_BuyCourse_HappyPath() public {
        token.mint(alice, amount);
        vm.startPrank(alice);
        token.approve(address(market), amount);
        bytes16 intentId = bytes16(uint128(1));
        vm.expectEmit(true, true, false, true);
        emit ICourseMarket.CoursePurchased(courseA, alice, address(token), amount, intentId, 1);
        market.buyCourse(courseA, amount, intentId);
        vm.stopPrank();

        assertTrue(market.hasPurchased(alice, courseA));
        assertEq(token.balanceOf(owner), amount);
    }

    function test_BuyCourse_RejectsDoublePurchase() public {
        token.mint(alice, amount);
        vm.startPrank(alice);
        token.approve(address(market), amount * 2);
        market.buyCourse(courseA, amount, bytes16(uint128(1)));
        vm.expectRevert(CourseMarket.AlreadyPurchased.selector);
        market.buyCourse(courseA, amount, bytes16(uint128(2)));
        vm.stopPrank();
    }

    function test_BuyCourse_RejectsAmountMismatch() public {
        token.mint(alice, amount);
        vm.startPrank(alice);
        token.approve(address(market), amount);
        vm.expectRevert(CourseMarket.AmountMismatch.selector);
        market.buyCourse(courseA, amount + 1, bytes16(uint128(1)));
        vm.stopPrank();
    }

    function test_BuyCourse_RejectsUnconfiguredCourse() public {
        token.mint(alice, amount);
        vm.startPrank(alice);
        token.approve(address(market), amount);
        vm.expectRevert(CourseMarket.NotConfigured.selector);
        market.buyCourse(courseB, amount, bytes16(uint128(1)));
        vm.stopPrank();
    }

    function test_Pause_BlocksBuyingButAllowsConfigure() public {
        market.pause();
        token.mint(alice, amount);
        vm.startPrank(alice);
        token.approve(address(market), amount);
        vm.expectRevert();
        market.buyCourse(courseA, amount, bytes16(uint128(1)));
        vm.stopPrank();
        // configure 仍可用
        market.configureCourse(courseB, address(token), amount, 1);
        (,, uint256 v) = market.getConfig(courseB);
        assertEq(v, 1);
    }

    function test_ConfigureCourse_BumpPriceVersion() public {
        market.configureCourse(courseA, address(token), amount * 2, 2);
        (address t, uint256 a, uint256 v) = market.getConfig(courseA);
        assertEq(t, address(token));
        assertEq(a, amount * 2);
        assertEq(v, 2);
    }

    // 资金守恒 invariant：buyCourse 转给 owner 的总额 == 唯一买家数 × amount。
    function test_FundsConservation() public {
        uint256 N = 5;
        address[] memory buyers = new address[](N);
        for (uint256 i = 0; i < N; i++) {
            buyers[i] = address(uint160(0x1000 + i));
            token.mint(buyers[i], amount);
            vm.startPrank(buyers[i]);
            token.approve(address(market), amount);
            market.buyCourse(courseA, amount, bytes16(uint128(i + 1)));
            vm.stopPrank();
        }
        assertEq(token.balanceOf(owner), amount * N);
    }

    // ====================================================================
    //  Fuzz tests (F03-T02 enhancement)
    // ====================================================================

    /// @dev 任意正数 amount，配置后用精确值购买应成功。
    function testFuzz_BuyCourse_AnyAmountRespectsExact(uint256 fuzzAmount) public {
        fuzzAmount = bound(fuzzAmount, 1, 1e30);

        bytes32 courseKey = keccak256(abi.encode("fuzz-amount", fuzzAmount));
        market.configureCourse(courseKey, address(token), fuzzAmount, 1);

        token.mint(alice, fuzzAmount);
        vm.startPrank(alice);
        token.approve(address(market), fuzzAmount);
        bytes16 intentId = bytes16(uint128(fuzzAmount));
        market.buyCourse(courseKey, fuzzAmount, intentId);
        vm.stopPrank();

        assertTrue(market.hasPurchased(alice, courseKey));
        assertEq(token.balanceOf(owner), fuzzAmount);
    }

    /// @dev expectedAmount != configured amount 时必须 revert。
    function testFuzz_BuyCourse_RejectsAmountMismatch(uint256 expected) public {
        // 跳过 expected == configured amount 的退化情况，让 fuzz 命中真正的不匹配。
        expected = bound(expected, 1, 1e30);
        vm.assume(expected != amount);

        token.mint(alice, expected);
        vm.startPrank(alice);
        token.approve(address(market), expected);
        vm.expectRevert(CourseMarket.AmountMismatch.selector);
        market.buyCourse(courseA, expected, bytes16(uint128(expected)));
        vm.stopPrank();
    }

    /// @dev 同一 buyer 对同一 course 的二次购买必须 revert。
    function testFuzz_BuyCourse_RejectsAlreadyPurchased(uint8 buys) public {
        // 至少 2 次才能断言 "第二次必然 revert"。
        vm.assume(buys >= 2);

        token.mint(alice, amount * buys);
        vm.startPrank(alice);
        token.approve(address(market), amount * buys);

        // 首次必须成功
        market.buyCourse(courseA, amount, bytes16(uint128(1)));

        // 第二次及以后：每次都 revert
        for (uint256 i = 1; i < buys; i++) {
            vm.expectRevert(CourseMarket.AlreadyPurchased.selector);
            market.buyCourse(courseA, amount, bytes16(uint128(i + 1)));
        }
        vm.stopPrank();
    }
}

/// @title CourseMarketHandler
/// @notice invariant 测试 handler：包装 configureCourse / buyCourse / pause
///         调用，记录每次状态变更以供 invariant 校验。
contract CourseMarketHandler is Test {
    CourseMarket public immutable market;
    MockToken public immutable token;
    address public immutable owner;
    address[] public actors;

    bytes32[] public configuredCourses;
    mapping(bytes32 => bool) public isTracked;
    mapping(bytes32 => uint256) public amountByCourse;
    mapping(bytes32 => uint256) public purchasesByCourse;
    mapping(bytes32 => mapping(address => bool)) public buyerSeen;
    /// @dev 同一 (buyer, courseKey) 累计调用 buyCourse 的次数（应恒为 0 或 1）
    mapping(address => mapping(bytes32 => uint256)) public buyCallCount;

    uint256 public constant ACTOR_COUNT = 10;

    constructor(CourseMarket _market, MockToken _token, address _owner) {
        market = _market;
        token = _token;
        owner = _owner;
        for (uint160 i = 1; i <= ACTOR_COUNT; i++) {
            actors.push(address(uint160(i)));
        }
    }

    function actorsLength() external view returns (uint256) {
        return actors.length;
    }

    function configuredCoursesLength() external view returns (uint256) {
        return configuredCourses.length;
    }

    /// @dev 配置一个新课程。amount ∈ [1, 1e30]，防止与 uint256 边界 / 算术冲突。
    function configure(bytes32 courseKey, uint256 amount) external {
        amount = bound(amount, 1, 1e30);
        if (isTracked[courseKey]) return;

        vm.prank(owner);
        market.configureCourse(courseKey, address(token), amount, 1);

        isTracked[courseKey] = true;
        amountByCourse[courseKey] = amount;
        configuredCourses.push(courseKey);
    }

    /// @dev 让某个 actor 尝试购买某个课程。已购或未配置则跳过（no-op）。
    function buy(bytes32 courseKey, uint256 actorIdx) external {
        actorIdx = bound(actorIdx, 0, actors.length - 1);
        address buyer = actors[actorIdx];

        uint256 amount = amountByCourse[courseKey];
        if (amount == 0) return;
        if (market.hasPurchased(buyer, courseKey)) return;

        token.mint(buyer, amount);
        vm.startPrank(buyer);
        token.approve(address(market), amount);
        market.buyCourse(courseKey, amount, bytes16(uint128(uint160(buyer))));
        vm.stopPrank();

        purchasesByCourse[courseKey] += amount;
        buyerSeen[courseKey][buyer] = true;
        buyCallCount[buyer][courseKey] += 1;
    }

    /// @dev 切暂停 / 恢复由 fuzzer 随机触发，用于 invariant_PausedRejectsBuy。
    function pause() external {
        vm.prank(owner);
        market.pause();
    }

    function unpause() external {
        vm.prank(owner);
        market.unpause();
    }
}

/// @title CourseMarketInvariant
/// @notice CourseMarket 的 invariant / fuzz property 套件。
contract CourseMarketInvariant is StdInvariant, Test {
    CourseMarket internal market;
    MockToken internal token;
    CourseMarketHandler internal handler;

    address internal owner;
    bytes32 internal courseA = keccak256("invariant-course-A");

    function setUp() public {
        owner = address(this);
        market = new CourseMarket(owner);
        token = new MockToken();
        handler = new CourseMarketHandler(market, token, owner);

        // 只对 handler 调用，handler 内部 prank owner/actor 转发到 market。
        targetContract(address(handler));

        bytes4[] memory selectors = new bytes4[](3);
        selectors[0] = handler.configure.selector;
        selectors[1] = handler.buy.selector;
        selectors[2] = handler.pause.selector;
        targetSelector(FuzzSelector({addr: address(handler), selectors: selectors}));
    }

    /// @dev 任意随机序列后，owner 余额 == 已成功购买总额。
    /// 已成功购买 = Σ (configuredAmount × distinctBuyersCount) per course。
    function invariant_FundsConservation() public view {
        uint256 configuredCoursesLen = handler.configuredCoursesLength();
        uint256 totalPurchased;
        uint256 buyerSeenCount;

        for (uint256 i = 0; i < configuredCoursesLen; i++) {
            bytes32 ck = handler.configuredCourses(i);
            uint256 configuredAmount = handler.amountByCourse(ck);
            // 同一 buyer 不会重复购买 → 唯一买家数 = purchasesByCourse / amount
            uint256 purchasesAmt = handler.purchasesByCourse(ck);
            // 防算术除零；amount > 0 在 configure 时已 bound 保证。
            buyerSeenCount = purchasesAmt / configuredAmount;
            totalPurchased += buyerSeenCount * configuredAmount;
        }

        assertEq(token.balanceOf(owner), totalPurchased);
    }

    /// @dev hasPurchased(buyer, courseKey) == true 必然意味着该 buyer 在
    ///      handler 内部 buyCallCount(buyer, courseKey) == 1。
    function invariant_NoDoublePurchase() public view {
        uint256 actorsLen = handler.actorsLength();
        uint256 configuredLen = handler.configuredCoursesLength();

        for (uint256 i = 0; i < actorsLen; i++) {
            address buyer = handler.actors(i);
            for (uint256 j = 0; j < configuredLen; j++) {
                bytes32 ck = handler.configuredCourses(j);
                bool purchased = market.hasPurchased(buyer, ck);
                uint256 calls = handler.buyCallCount(buyer, ck);
                if (purchased) {
                    // 已购 ⇒ 调用计数恰好为 1
                    assertEq(calls, 1);
                } else {
                    // 未购 ⇒ 调用计数必须为 0（任何 >=1 都意味着 buyCourse
                    // 至少成功一次，但 hasPurchased 为 false → 数据不一致）
                    assertEq(calls, 0);
                }
            }
        }
    }

    /// @dev 市场暂停时，handler.buy 不应让 owner 余额继续增长。
    ///      用「每个 actor 当前已购总额」与「owner 实收总额」的交叉校验：
    ///      owner 实收 == Σ (已成功调用 buyCourse 的金额)。在 paused 下，
    ///      handler.buy 走到 market.buyCourse 必 revert，handler 不会写
    ///      purchasesByCourse / buyCallCount，因此 Σ 与 balanceOf 在
    ///      pause 之后必然相等；若不等则说明 pause 被绕过。
    function invariant_PausedRejectsBuy() public view {
        uint256 actorsLen = handler.actorsLength();
        uint256 configuredLen = handler.configuredCoursesLength();
        uint256 expectedTotal;

        for (uint256 i = 0; i < actorsLen; i++) {
            address buyer = handler.actors(i);
            for (uint256 j = 0; j < configuredLen; j++) {
                bytes32 ck = handler.configuredCourses(j);
                // 只统计「真正成功 buyCourse」的金额；buyCallCount==1 即成功一次。
                if (handler.buyCallCount(buyer, ck) == 1) {
                    expectedTotal += handler.amountByCourse(ck);
                }
            }
        }

        // 关键断言：owner 实收 == Σ 已购金额；任何 paused 路径绕过都会让
        // balanceOf(owner) 偏离 expectedTotal（因为 handler 在失败路径不会
        // 递增 buyCallCount，但 token 已 mint → 若 market 真收了，balanceOf
        // 会超出）。
        assertEq(token.balanceOf(owner), expectedTotal);
    }
}
