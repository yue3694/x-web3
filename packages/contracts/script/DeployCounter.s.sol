// SPDX-License-Identifier: MIT
// ============================================================================
//  DeployCounter.s.sol  ——  把 Counter 部署到任意 EVM 链
// ----------------------------------------------------------------------------
//  Foundry 的"部署脚本"和"测试合约"一样写在 Solidity 里。但跟 test 不同：
//    - 继承 `Script` 而不是 `Test`（无 cheatcode）；
//    - 用 `vm.startBroadcast / vm.stopBroadcast` 标记"会把接下来的调用真
//      实广播出去"；
//    - 用 `console2.log` 输出到终端（注意：测试里用 `console.log`，脚本里
//      用 `console2.log`——forge-std 的两个 console，console2 适合在
//      production 输出中保留空格）。
//
//  三种跑法：
//    1) 跑本地 anvil：
//         anvil &
//         forge script script/DeployCounter.s.sol:DeployCounter \
//             --rpc-url http://127.0.0.1:8545 --private-key 0xac09...
//    2) 广播到 Sepolia + 自动 Etherscan 验证：
//         forge script script/DeployCounter.s.sol:DeployCounter \
//             --rpc-url $SEPOLIA_RPC_URL --broadcast --verify -vvvv
//    3) 仅模拟（不广播）：省略 --broadcast，会执行脚本但不发送任何交易。
// ============================================================================

pragma solidity ^0.8.24;

// `Script` 是 forge-std 提供的基础类，提供 `vm.startBroadcast` /
// `vm.stopBroadcast` / `console2` 等部署专用工具。`Test` 不行——它带 vm.*
// cheatcode，跑脚本时不能访问。
import {Script, console2} from "forge-std/Script.sol";

import {Counter} from "../src/Counter.sol";

/// @notice 部署 Counter 的最小化脚本。
/// @dev    把 owner 设为部署者本人——部署者即管理员。
contract DeployCounter is Script {
    function run() external {
        // ---------------------------------------------------------------
        //  从环境变量读 deployer 私钥。
        //  `vm.envUint("DEPLOYER_PRIVATE_KEY")` 会自动从 `.env` 文件读
        //  （前提：当前工作目录有 .env，或者你在 shell 里 export）。
        //
        //  也可以用 `vm.envOr` 提供 fallback：`vm.envOr("FOO", 0)`。
        //
        //  替代写法：在 forge script 命令行加 `--private-key 0x...`，会
        //  覆盖 env 里的值。
        // ---------------------------------------------------------------
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");

        // 从私钥反推 EOA 地址。`vm.addr` 是 forge-std 提供的，方便打印。
        address deployer = vm.addr(deployerPrivateKey);

        // ---------------------------------------------------------------
        //  vm.startBroadcast(deployerPrivateKey) —— 从此刻起，所有合约调
        //  用都会被 forge 打包成"由 deployer 签名"的真实交易，并通过 RPC
        //  发送到 --rpc-url 指向的链。
        //
        //  vm.stopBroadcast() —— 结束广播区域。
        //
        //  配对使用：中间的 `new Counter(deployer)` 是一次部署交易，会被
        //  真实广播。
        //
        //  如果只想去模拟（不广播），删除 --broadcast 即可——脚本仍会执
        //  行，但 vm.startBroadcast 改为 dry-run 模式，不实际发送交易。
        // ---------------------------------------------------------------
        vm.startBroadcast(deployerPrivateKey);
        Counter counter = new Counter(deployer);
        vm.stopBroadcast();

        // ---------------------------------------------------------------
        //  console2.log —— 脚本运行时输出到 stdout。注意脚本外用 console2
        //  而非 console，因为 console2 输出对空格 / 换行更友好。
        //
        //  部署后的下一步操作提示也放在这里，方便人工拷贝。
        // ---------------------------------------------------------------
        console2.log("Counter deployed at:", address(counter));
        console2.log("Deployer:           ", deployer);
        console2.log("Network (chainid):  ", block.chainid);
        console2.log("");
        console2.log("Next steps:");
        console2.log("  1. Copy the address above into");
        console2.log("     apps/web/src/contracts/deployments.ts");
        console2.log("     -> counterDeployments.sepolia.address");
        console2.log("  2. Run: pnpm contracts:export:abi");
        console2.log("  3. Run: pnpm dev");
    }
}

// ===========================================================================
//  部署脚本与测试的区别（速查）
// ----------------------------------------------------------------------------
//                       Test                   Script
//   继承基类              Test                   Script
//   cheatcode (vm.*)       有                    仅 env / addr
//   console               console.log            console2.log
//   发送交易              否                    通过 vm.startBroadcast
//   用 anvil / 公共 RPC    可以（anvil 自动）    需要 --rpc-url
//   跑出位置              test/                  script/
//   命名                  *.t.sol                *.s.sol
// ===========================================================================
