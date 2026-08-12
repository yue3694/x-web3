// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {YDToken, IYDToken} from "../src/YDToken.sol";
import {IAccessControl} from "@openzeppelin/contracts/access/IAccessControl.sol";

/// @title YDTokenTest
/// @notice 覆盖 YDToken 的核心路径：铸造 / 暂停 / 角色 / cap / 错误。
contract YDTokenTest is Test {
    YDToken internal token;
    address internal admin = makeAddr("admin");
    address internal minter = makeAddr("minter");
    address internal pauser = makeAddr("pauser");
    address internal treasury = makeAddr("treasury");
    address internal alice = makeAddr("alice");
    address internal bob = makeAddr("bob");

    uint256 internal constant INITIAL = 200_000_000 * 1e18;
    uint256 internal constant CAP = 1_000_000_000 * 1e18;

    function setUp() public {
        token = new YDToken(admin, minter, pauser, treasury);
    }

    // --------------------------------------------------------------------
    //  构造函数 / 初始状态
    // --------------------------------------------------------------------

    function test_Constructor_MintsInitialSupplyToTreasury() public view {
        assertEq(token.totalSupply(), INITIAL);
        assertEq(token.balanceOf(treasury), INITIAL);
        assertEq(token.cap(), CAP);
    }

    function test_Constructor_GrantsRoles() public view {
        assertTrue(token.hasRole(token.DEFAULT_ADMIN_ROLE(), admin));
        assertTrue(token.hasRole(token.MINTER_ROLE(), minter));
        assertTrue(token.hasRole(token.PAUSER_ROLE(), pauser));
    }

    function test_Constructor_RejectsZeroAddresses() public {
        vm.expectRevert(YDToken.ZeroAddress.selector);
        new YDToken(address(0), minter, pauser, treasury);

        vm.expectRevert(YDToken.ZeroAddress.selector);
        new YDToken(admin, address(0), pauser, treasury);

        vm.expectRevert(YDToken.ZeroAddress.selector);
        new YDToken(admin, minter, address(0), treasury);

        vm.expectRevert(YDToken.ZeroAddress.selector);
        new YDToken(admin, minter, pauser, address(0));
    }

    // --------------------------------------------------------------------
    //  mint 路径
    // --------------------------------------------------------------------

    function test_Mint_HappyPath() public {
        uint256 amount = 1000e18;
        vm.prank(minter);
        vm.expectEmit(true, true, false, true);
        emit IYDToken.Minted(alice, amount, minter);
        token.mint(alice, amount);

        assertEq(token.balanceOf(alice), amount);
        assertEq(token.totalSupply(), INITIAL + amount);
    }

    function test_Mint_RevertsForNonMinter() public {
        vm.startPrank(alice);
        vm.expectRevert(
            abi.encodeWithSelector(
                IAccessControl.AccessControlUnauthorizedAccount.selector, alice, token.MINTER_ROLE()
            )
        );
        token.mint(alice, 1000e18);
        vm.stopPrank();
    }

    function test_Mint_RevertsOnZeroAddress() public {
        vm.prank(minter);
        vm.expectRevert(YDToken.ZeroAddress.selector);
        token.mint(address(0), 1000e18);
    }

    function test_Mint_RevertsOnZeroAmount() public {
        vm.prank(minter);
        vm.expectRevert(YDToken.ZeroAmount.selector);
        token.mint(alice, 0);
    }

    function test_Mint_RevertsWhenExceedingCap() public {
        uint256 remaining = CAP - INITIAL;
        vm.prank(minter);
        // 第一次铸到 cap 上限
        token.mint(alice, remaining);

        // 再铸任何正数都会超 cap
        vm.prank(minter);
        vm.expectRevert(YDToken.CapExceeded.selector);
        token.mint(alice, 1);
    }

    /// @notice 模糊：任意 amount 都不能让 totalSupply 越过 cap。
    function test_Fuzz_MintRespectsCap(uint256 amount) public {
        // 限制 amount 上界，避免 totalSupply + amount 溢出。
        amount = bound(amount, 0, CAP);

        vm.startPrank(minter);
        if (amount == 0) {
            vm.expectRevert(YDToken.ZeroAmount.selector);
            token.mint(alice, amount);
        } else if (amount > CAP - INITIAL) {
            vm.expectRevert(YDToken.CapExceeded.selector);
            token.mint(alice, amount);
        } else {
            token.mint(alice, amount);
            assertLe(token.totalSupply(), CAP);
        }
        vm.stopPrank();
    }

    // --------------------------------------------------------------------
    //  pause 路径
    // --------------------------------------------------------------------

    function test_Pause_BlocksTransfer() public {
        vm.prank(pauser);
        token.pause();

        vm.prank(treasury);
        vm.expectRevert();
        token.transfer(alice, 1e18);
    }

    function test_Pause_DoesNotBlockApprove() public {
        vm.prank(pauser);
        token.pause();

        vm.prank(treasury);
        // approve 在 OZ v5 ERC20Pausable 中不被 pause 阻塞。
        token.approve(alice, 1e18);
        assertEq(token.allowance(treasury, alice), 1e18);
    }

    function test_Unpause_ResumesTransfer() public {
        vm.startPrank(pauser);
        token.pause();
        token.unpause();
        vm.stopPrank();

        vm.prank(treasury);
        token.transfer(alice, 1e18);
        assertEq(token.balanceOf(alice), 1e18);
    }

    function test_Pause_RevertsForNonPauser() public {
        bytes32 pauserRole = token.PAUSER_ROLE();
        vm.startPrank(alice);
        vm.expectRevert(
            abi.encodeWithSelector(
                IAccessControl.AccessControlUnauthorizedAccount.selector, alice, pauserRole
            )
        );
        token.pause();
        vm.stopPrank();
    }

    // --------------------------------------------------------------------
    //  角色管理
    // --------------------------------------------------------------------

    function test_GrantRole_RevertsForNonAdmin() public {
        bytes32 adminRole = token.DEFAULT_ADMIN_ROLE();
        bytes32 minterRole = token.MINTER_ROLE();
        vm.startPrank(alice);
        vm.expectRevert(
            abi.encodeWithSelector(
                IAccessControl.AccessControlUnauthorizedAccount.selector, alice, adminRole
            )
        );
        token.grantRole(minterRole, alice);
        vm.stopPrank();
    }

    function test_AdminCanGrantMinter() public {
        vm.startPrank(admin);
        token.grantRole(token.MINTER_ROLE(), alice);
        vm.stopPrank();

        vm.prank(alice);
        token.mint(bob, 1e18);
        assertEq(token.balanceOf(bob), 1e18);
    }

    function test_Mint_RevertsAfterRevokingMinter() public {
        bytes32 minterRole = token.MINTER_ROLE();
        vm.startPrank(admin);
        token.revokeRole(minterRole, minter);
        vm.stopPrank();

        vm.startPrank(minter);
        vm.expectRevert(
            abi.encodeWithSelector(
                IAccessControl.AccessControlUnauthorizedAccount.selector, minter, minterRole
            )
        );
        token.mint(alice, 1e18);
        vm.stopPrank();
    }

    // --------------------------------------------------------------------
    //  invariant：总供应永不超过 cap
    // --------------------------------------------------------------------

    function test_Invariant_TotalSupplyNeverExceedsCap() public {
        assertLe(token.totalSupply(), CAP);

        // 铸到 cap 上限
        vm.prank(minter);
        token.mint(alice, CAP - INITIAL);
        assertEq(token.totalSupply(), CAP);
        assertLe(token.totalSupply(), CAP);
    }
}
