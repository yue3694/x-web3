// SPDX-License-Identifier: MIT
// ============================================================================
//  DeployCourseMarket.s.sol  ——  把 CourseMarket 部署到任意 EVM 链
// ----------------------------------------------------------------------------
//  CourseMarket 是 F03 链上结算的入口。它由 API worker 监听
//  CoursePurchased 事件，再把状态推进到 orders + enrollments。
//
//  部署参数：
//    - token  ：支付 ERC20 的地址（Sepolia = MockYD / 待 F05 决议）；
//    - owner  ：管理员（建议走多签 / timelock）；MVP 阶段 = 部署者。
//
//  三种跑法：
//    1) 本地 anvil：
//         anvil &
//         forge script script/DeployCourseMarket.s.sol:DeployCourseMarket \
//             --rpc-url http://127.0.0.1:8545 --private-key 0xac09...
//    2) 广播到 Sepolia + 自动 Etherscan 验证：
//         forge script script/DeployCourseMarket.s.sol:DeployCourseMarket \
//             --rpc-url $SEPOLIA_RPC_URL --broadcast --verify -vvvv
//    3) 仅模拟：省略 --broadcast。
// ============================================================================

pragma solidity ^0.8.24;

import {Script, console2} from "forge-std/Script.sol";
import {CourseMarket} from "../src/CourseMarket.sol";

/// @notice 部署 CourseMarket。
/// @dev    部署脚本额外校验 token 非零，避免 owner() 在 token 缺席时被错误地
///         设成 0x0。
contract DeployCourseMarket is Script {
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);

        // 支付代币地址：默认从 PAYMENT_TOKEN_ADDRESS 读，缺省 = address(0)，
        // 由部署者在 console 输出后用 configureCourse 单独配置（解耦部署与
        // 代币上线顺序）。
        address paymentToken = vm.envOr("PAYMENT_TOKEN_ADDRESS", address(0));

        vm.startBroadcast(deployerPrivateKey);
        CourseMarket market = new CourseMarket(deployer);
        // 若提供了 token，第一笔配置写一个 priceVersion=1 的占位 course。
        // 真上线课程由后端在 catalogue 上线时再调 configureCourse 写入。
        if (paymentToken != address(0)) {
            // 占位 courseKey = bytes32(0)；正式课程由后端签名写入。
            market.configureCourse(bytes32(0), paymentToken, 0, 1);
        }
        vm.stopBroadcast();

        console2.log("CourseMarket deployed at:", address(market));
        console2.log("Owner:                  ", deployer);
        console2.log("Payment token (or 0):   ", paymentToken);
        console2.log("Network (chainid):      ", block.chainid);
        console2.log("");
        console2.log("Next steps:");
        console2.log("  1. Copy the address above into");
        console2.log("     apps/web/src/contracts/deployments.ts");
        console2.log("     -> courseMarketDeployments.sepolia.address");
        console2.log("  2. Run: pnpm contracts:export:abi CourseMarket");
        console2.log("  3. Run: pnpm dev");
    }
}