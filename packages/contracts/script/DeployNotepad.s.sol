// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Script, console2} from "forge-std/Script.sol";
import {Notepad} from "../src/Notepad.sol";

/// @notice Deploys Notepad to the configured network (defaults to --rpc-url arg).
///         No constructor args — Notepad is ownerless and self-contained.
///         After broadcast, the address is logged and (when --verify is set)
///         automatically submitted to Etherscan.
contract DeployNotepad is Script {
    function run() external {
        // `vm.envUint` reads from `.env` automatically when Foundry is run
        // from the package root, OR from the `--private-key` flag.
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);

        vm.startBroadcast(deployerPrivateKey);
        Notepad notepad = new Notepad();
        vm.stopBroadcast();

        console2.log("Notepad deployed at: ", address(notepad));
        console2.log("Deployer:            ", deployer);
        console2.log("Network (chainid):   ", block.chainid);
        console2.log("");
        console2.log("Next steps:");
        console2.log("  1. Paste the address above into");
        console2.log("     apps/web/src/contracts/deployments.ts");
        console2.log("     -> notepadDeployments.sepolia.address");
        console2.log("  2. Run: pnpm contracts:export:abi");
        console2.log("  3. Run: pnpm dev");
    }
}