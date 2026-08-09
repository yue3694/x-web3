// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
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
}
