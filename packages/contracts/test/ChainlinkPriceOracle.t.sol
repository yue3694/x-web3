// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {ChainlinkPriceOracle} from "../src/ChainlinkPriceOracle.sol";
import {MockV3Aggregator} from "../src/mocks/MockV3Aggregator.sol";

contract ChainlinkPriceOracleTest is Test {
    MockV3Aggregator internal feed;
    ChainlinkPriceOracle internal oracle;

    function setUp() public {
        vm.warp(1_000_000);
        feed = new MockV3Aggregator(8, 100_000_000, "YD / USD");
        oracle = new ChainlinkPriceOracle(address(feed), 1 hours);
    }

    function test_LatestPrice() public view {
        (uint256 price, uint8 decimals_, uint256 updatedAt) = oracle.latestPrice();
        assertEq(price, 100_000_000);
        assertEq(decimals_, 8);
        assertEq(updatedAt, block.timestamp);
    }

    function test_RevertsNegativeOrZeroAnswer() public {
        feed.setRoundData(2, 0, block.timestamp, 2);
        vm.expectRevert(ChainlinkPriceOracle.InvalidAnswer.selector);
        oracle.latestPrice();

        feed.setRoundData(3, -1, block.timestamp, 3);
        vm.expectRevert(ChainlinkPriceOracle.InvalidAnswer.selector);
        oracle.latestPrice();
    }

    function test_RevertsStalePrice() public {
        feed.setRoundData(2, 100_000_000, block.timestamp - 1 hours - 1, 2);
        vm.expectRevert(ChainlinkPriceOracle.StalePrice.selector);
        oracle.latestPrice();
    }

    function test_RevertsFutureTimestamp() public {
        feed.setRoundData(2, 100_000_000, block.timestamp + 1, 2);
        vm.expectRevert(ChainlinkPriceOracle.StalePrice.selector);
        oracle.latestPrice();
    }

    function test_RevertsIncompleteRound() public {
        feed.setRoundData(2, 100_000_000, block.timestamp, 1);
        vm.expectRevert(ChainlinkPriceOracle.IncompleteRound.selector);
        oracle.latestPrice();
    }

    function test_ConstructorGuards() public {
        vm.expectRevert(ChainlinkPriceOracle.ZeroAddress.selector);
        new ChainlinkPriceOracle(address(0), 1 hours);
        vm.expectRevert(ChainlinkPriceOracle.InvalidMaxAge.selector);
        new ChainlinkPriceOracle(address(feed), 0);
    }
}
