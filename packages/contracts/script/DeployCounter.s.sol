// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Script, console2} from "forge-std/Script.sol";
import {Counter} from "../src/Counter.sol";

/// @notice Deploys Counter to the configured network (defaults to --rpc-url arg).
///         Writes the deployed address & ABI to broadcast/ for the frontend
///         to consume via `pnpm contracts:export:abi`.
contract DeployCounter is Script {
    function run() external {
        // The deployer is `msg.sender` of this script — set by `--private-key`
        // or `$DEPLOYER_PRIVATE_KEY` (forge auto-loads it).
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);

        vm.startBroadcast(deployerPrivateKey);
        Counter counter = new Counter(deployer);
        vm.stopBroadcast();

        console2.log("Counter deployed at:", address(counter));
        console2.log("Deployer:           ", deployer);
        console2.log("Network:            ", block.chainid);
    }
}