// SPDX-License-Identifier: MIT
// ============================================================================
//  DeployNotepad.s.sol  ——  把 Notepad 部署到任意 EVM 链
// ----------------------------------------------------------------------------
//  与 DeployCounter.s.sol 的唯一差别：Notepad 没有构造函数参数。
//
//  跑法（Sepolia）：
//
//      forge script script/DeployNotepad.s.sol:DeployNotepad \
//          --rpc-url $SEPOLIA_RPC_URL --broadcast --verify -vvvv
//
//  或用项目脚本：
//
//      pnpm contracts:deploy:notepad:sepolia
//
//  部署完成后，console 会打印合约地址。把地址粘进：
//      apps/web/src/contracts/deployments.ts
//          notepadDeployments.sepolia.address = '0x...'
// ============================================================================

pragma solidity ^0.8.24;

import {Script, console2} from "forge-std/Script.sol";
import {Notepad} from "../src/Notepad.sol";

contract DeployNotepad is Script {
    function run() external {
        // vm.envUint 从 .env 自动读取（前提：当前工作目录有 .env）。
        // 也可以 `--private-key 0x...` 覆盖。
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);

        // startBroadcast / stopBroadcast 之间的所有合约调用都会被广播成
        // 由 deployer 签名的真实交易。省略 --broadcast 时进入 dry-run 模式。
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
