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
//
//  两种配置模式：
//    Mode A（默认）：仅读取 PAYMENT_TOKEN_ADDRESS env，写一个 priceVersion=1
//                    的占位 course；正式课程由后端签名写入。
//    Mode B：通过 COURSES_CONFIG_PATH 指向 JSON 文件，部署后逐条
//            configureCourse。Mode B 优先级更高：只要设置了
//            COURSES_CONFIG_PATH，PAYMENT_TOKEN_ADDRESS 即被忽略。
//
//  JSON 形状（数组，每条字段：courseKey / token / amount / priceVersion）：
//    [
//      {
//        "courseKey":    "0x<32-byte hex>",
//        "token":        "0x<address>",
//        "amount":       "<uint256 string>",
//        "priceVersion": <uint256>
//      },
//      ...
//    ]
// ============================================================================

pragma solidity ^0.8.24;

import {Script, console2} from "forge-std/Script.sol";
import {stdJson} from "forge-std/StdJson.sol";
import {CourseMarket} from "../src/CourseMarket.sol";

/// @notice 部署 CourseMarket，支持 Mode A（token-only）与 Mode B（JSON 配置）。
/// @dev    JSON 解析的纯逻辑由 `parseCoursesConfig` 暴露，便于单测。
contract DeployCourseMarket is Script {
    using stdJson for string;

    struct CourseInput {
        bytes32 courseKey;
        address token;
        uint256 amount;
        uint256 priceVersion;
    }

    /// @notice 从 JSON 字符串解析课程配置。供单测与生产共用。
    /// @dev    不做 vm.readFile，由调用者负责 IO；仅做校验与反序列化。
    ///         view 而非 pure：stdJson.keyExists 走 vm cheatcode。
    function parseCoursesConfig(string memory json)
        public
        view
        returns (CourseInput[] memory courses)
    {
        require(bytes(json).length > 0, "DeployCourseMarket: empty JSON");

        // stdJson 不提供顶层数组长度 API；用 keyExists 按 [i] 探测直到不命中。
        uint256 length;
        while (json.keyExists(string.concat("[", vm.toString(length), "]"))) {
            unchecked {
                length++;
            }
            // 防御性上限：防止解析错误时无限循环
            require(length < 1024, "DeployCourseMarket: too many entries");
        }
        courses = new CourseInput[](length);

        for (uint256 i = 0; i < length; i++) {
            string memory base = string.concat("[", vm.toString(i), "]");
            CourseInput memory c;
            c.courseKey = json.readBytes32(string.concat(base, ".courseKey"));
            c.token = json.readAddress(string.concat(base, ".token"));
            c.amount = json.readUint(string.concat(base, ".amount"));
            c.priceVersion = json.readUint(string.concat(base, ".priceVersion"));

            require(c.courseKey != bytes32(0), "DeployCourseMarket: zero courseKey");
            require(c.token != address(0), "DeployCourseMarket: zero token");
            require(c.amount > 0, "DeployCourseMarket: zero amount");
            require(c.priceVersion > 0, "DeployCourseMarket: zero priceVersion");

            courses[i] = c;
        }
    }

    function run() external {
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);

        // Mode A：PAYMENT_TOKEN_ADDRESS 单一占位 course
        address paymentToken = vm.envOr("PAYMENT_TOKEN_ADDRESS", address(0));
        // Mode B：JSON 配置文件
        string memory configPath = vm.envOr("COURSES_CONFIG_PATH", string(""));
        bool useJsonMode = bytes(configPath).length > 0;

        CourseInput[] memory courses;
        if (useJsonMode) {
            require(vm.exists(configPath), "DeployCourseMarket: config file not found");
            string memory json = vm.readFile(configPath);
            courses = parseCoursesConfig(json);
        }

        vm.startBroadcast(deployerPrivateKey);
        CourseMarket market = new CourseMarket(deployer);

        uint256 configured;
        if (useJsonMode) {
            for (uint256 i = 0; i < courses.length; i++) {
                CourseInput memory c = courses[i];
                market.configureCourse(c.courseKey, c.token, c.amount, c.priceVersion);
                configured++;
            }
        } else if (paymentToken != address(0)) {
            // 占位 courseKey = bytes32(0)、amount = 0：仅为让 `getConfig(bytes32(0))`
            // 在生产环境返回一个有效 (token, amount=0, version=1) 条目，方便后端
            // worker 用同一调用探测"市场是否已部署 + payment token 是什么"。
            // 任何对 bytes32(0) 的真实 buyCourse 都会被 CourseMarket.AmountMismatch
            // revert（amount==0 与客户端 amount>0 不匹配），不会造成资金损失。
            // 后端 worker 真正使用的课程 key 由 `courseKey` 表（API 业务层）维护，
            // 不依赖该占位条目；后续运维 `configureCourse(realKey, ...)` 即可覆盖。
            market.configureCourse(bytes32(0), paymentToken, 0, 1);
            configured = 1;
        }
        vm.stopBroadcast();

        // ========== Console summary ==========
        console2.log("CourseMarket deployed at:", address(market));
        console2.log("Owner:                  ", deployer);
        console2.log("Network (chainid):      ", block.chainid);
        console2.log(
            "Mode:                   ", useJsonMode ? "B (JSON)" : "A (PAYMENT_TOKEN_ADDRESS)"
        );
        console2.log("Courses configured:     ", configured);
        if (configured > 0) {
            console2.log("---------------------------------------------------------------");
            console2.log(
                "courseKey           | token                                   | amount | version"
            );
            if (useJsonMode) {
                for (uint256 i = 0; i < courses.length; i++) {
                    CourseInput memory c = courses[i];
                    console2.log(
                        string.concat(
                            vm.toString(c.courseKey),
                            " | ",
                            vm.toString(c.token),
                            " | ",
                            vm.toString(c.amount),
                            " | ",
                            vm.toString(c.priceVersion)
                        )
                    );
                }
            } else {
                console2.log(
                    string.concat(
                        vm.toString(bytes32(0)), " | ", vm.toString(paymentToken), " | 0 | 1"
                    )
                );
            }
            console2.log("---------------------------------------------------------------");
        }
        console2.log("");
        console2.log("Next steps:");
        console2.log("  1. Copy the address above into");
        console2.log("     apps/web/src/contracts/deployments.ts");
        console2.log("     -> courseMarketDeployments.sepolia.address");
        console2.log("  2. Run: pnpm contracts:export:abi CourseMarket");
        console2.log("  3. Run: pnpm dev");
    }
}
