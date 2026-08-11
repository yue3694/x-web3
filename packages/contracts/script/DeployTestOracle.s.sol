// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Script, console2} from "forge-std/Script.sol";
import {ChainlinkPriceOracle} from "../src/ChainlinkPriceOracle.sol";
import {MockV3Aggregator} from "../src/mocks/MockV3Aggregator.sol";

/// @notice Deploys an Anvil-only mock feed and the same adapter used with Chainlink feeds.
contract DeployTestOracle is Script {
    function run() external returns (MockV3Aggregator feed, ChainlinkPriceOracle oracle) {
        require(block.chainid == 31_337, "DeployTestOracle: Anvil chain required");
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        uint8 decimals_ = uint8(vm.envOr("ORACLE_DECIMALS", uint256(8)));
        int256 answer = vm.envOr("ORACLE_INITIAL_ANSWER", int256(100_000_000));
        uint256 maxAge = vm.envOr("ORACLE_MAX_AGE_SECONDS", uint256(1 hours));

        vm.startBroadcast(deployerPrivateKey);
        feed = new MockV3Aggregator(decimals_, answer, "YD / USD test feed");
        oracle = new ChainlinkPriceOracle(address(feed), maxAge);
        vm.stopBroadcast();

        console2.log("Mock feed deployed at:", address(feed));
        console2.log("Price oracle deployed at:", address(oracle));
        console2.log("WARNING: mock feed is for Anvil/test use only");
    }
}
