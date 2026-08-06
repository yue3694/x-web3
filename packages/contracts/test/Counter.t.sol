// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {Counter} from "../src/Counter.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

contract CounterTest is Test {
    Counter internal counter;
    address internal owner = makeAddr("owner");
    address internal alice = makeAddr("alice");

    function setUp() public {
        counter = new Counter(owner);
    }

    function test_InitialCountIsZero() public view {
        assertEq(counter.count(), 0);
    }

    function test_Increment() public {
        counter.increment();
        assertEq(counter.count(), 1);

        counter.increment();
        assertEq(counter.count(), 2);
    }

    function test_Decrement() public {
        counter.increment();
        counter.increment();
        counter.decrement();
        assertEq(counter.count(), 1);
    }

    function test_DecrementRevertsOnUnderflow() public {
        vm.expectRevert(Counter.Underflow.selector);
        counter.decrement();
    }

    function test_ResetOnlyOwner() public {
        counter.increment();
        vm.prank(alice);
        vm.expectRevert(
            abi.encodeWithSelector(Ownable.OwnableUnauthorizedAccount.selector, alice)
        );
        counter.reset();

        vm.prank(owner);
        counter.reset();
        assertEq(counter.count(), 0);
    }
}