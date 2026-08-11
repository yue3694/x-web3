// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Script, console2} from "forge-std/Script.sol";
import {ChainlinkPriceOracle} from "../src/ChainlinkPriceOracle.sol";

/// @notice Deploys the guarded adapter around a Chainlink-compatible testnet feed.
contract DeployPriceOracle is Script {
    function run() external returns (ChainlinkPriceOracle oracle) {
        uint256 expectedChainId = vm.envOr("EXPECTED_CHAIN_ID", uint256(11_155_111));
        require(block.chainid == expectedChainId, "DeployPriceOracle: unexpected chain");
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address feed = vm.envAddress("PRICE_FEED_ADDRESS");
        uint256 maxAge = vm.envOr("ORACLE_MAX_AGE_SECONDS", uint256(1 hours));

        vm.startBroadcast(deployerPrivateKey);
        oracle = new ChainlinkPriceOracle(feed, maxAge);
        vm.stopBroadcast();

        console2.log("Price oracle deployed at:", address(oracle));
        console2.log("Underlying feed:", feed);
        console2.log("Max age seconds:", maxAge);
    }
}
