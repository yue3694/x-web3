// SPDX-License-Identifier: MIT
// ============================================================================
//  DeployCertificateNFT.s.sol  ——  把 CertificateNFT 部署到任意 EVM 链
// ----------------------------------------------------------------------------
//  CertificateNFT 是 F04「学习证书」的链上载体（Soulbound ERC-721 +
//  AccessControl）。铸造由 worker 的 mint signer 触发，吊销由运营/合规账户
//  触发，所以部署时需要一次把三个角色对齐：
//
//    - admin  (DEFAULT_ADMIN_ROLE)：角色管理员，生产建议多签 / timelock；
//    - minter (MINTER_ROLE)       ：worker mint signer（KMS / keystore）；
//    - burner (BURNER_ROLE)       ：吊销证书的运营账户，默认 = admin。
//
//  三种跑法：
//    1) 本地 anvil：
//         anvil &
//         CERT_NFT_ADMIN_ADDRESS=0x... CERT_NFT_MINTER_ADDRESS=0x... \
//         forge script script/DeployCertificateNFT.s.sol:DeployCertificateNFT \
//             --rpc-url http://127.0.0.1:8545 --private-key 0xac09...
//    2) 广播到 Sepolia + 自动 Etherscan 验证：
//         forge script script/DeployCertificateNFT.s.sol:DeployCertificateNFT \
//             --rpc-url $SEPOLIA_RPC_URL --broadcast --verify -vvvv
//    3) 仅模拟：省略 --broadcast。
//
//  两种配置模式：
//    Mode A（默认）：从 env 读三个地址
//        CERT_NFT_ADMIN_ADDRESS   （必填）
//        CERT_NFT_MINTER_ADDRESS  （必填）
//        CERT_NFT_BURNER_ADDRESS  （选填，缺省 = admin）
//    Mode B：CERT_NFT_CONFIG_PATH 指向 JSON 文件。Mode B 优先级更高：只要
//            设置了 CERT_NFT_CONFIG_PATH，上面三个地址 env 即被忽略。
//
//  JSON 形状（单个对象；burner 可省略）：
//    {
//      "admin":  "0x<address>",
//      "minter": "0x<address>",
//      "burner": "0x<address>"
//    }
//
//  关于 BURNER_ROLE 授权：
//    构造函数只授予 DEFAULT_ADMIN_ROLE / MINTER_ROLE，BURNER_ROLE 必须在部署
//    后由 admin 显式 grant。因此脚本只有在「部署者本人就是 admin」时才能在
//    同一笔脚本里补上这一步；admin 是多签时，脚本会打印待执行的 grantRole
//    命令，由多签自行提案执行（不做「部署者临时持有 admin 再移交」的
//    bootstrap，避免出现一个部署者可越权的时间窗）。
// ============================================================================

pragma solidity ^0.8.24;

import {Script, console2} from "forge-std/Script.sol";
import {stdJson} from "forge-std/StdJson.sol";
import {CertificateNFT} from "../src/CertificateNFT.sol";

/// @notice 部署 CertificateNFT，支持 Mode A（env 地址）与 Mode B（JSON 配置）。
/// @dev    配置解析 / 校验的纯逻辑拆成 `buildConfig` / `parseCertificateConfig`
///         / `readEnvConfig` / `resolveConfig`，便于单测在不广播的情况下覆盖。
contract DeployCertificateNFT is Script {
    using stdJson for string;

    /// @notice 部署所需的三个角色地址。
    /// @dev    burner 在 `buildConfig` 里做过缺省填充，落到这里一定非零。
    struct CertificateConfig {
        address admin;
        address minter;
        address burner;
    }

    // --------------------------------------------------------------------
    //  配置构建与校验（纯逻辑，可单测）
    // --------------------------------------------------------------------

    /// @notice 校验并归一化三个角色地址；两种模式共用同一套规则。
    /// @dev    burner 传 address(0) 视为「未配置」而非非法值 —— env 模式下
    ///         `vm.envOr(..., address(0))` 无法区分「没设」和「设成零地址」，
    ///         所以两种模式统一按「零地址 = 未配置 → 回落到 admin」处理。
    /// @param admin  DEFAULT_ADMIN_ROLE 持有者，必填。
    /// @param minter MINTER_ROLE 持有者（worker signer），必填。
    /// @param burner BURNER_ROLE 持有者，选填；零地址时回落到 admin。
    /// @return cfg   归一化后的配置，三个字段均非零。
    function buildConfig(address admin, address minter, address burner)
        public
        pure
        returns (CertificateConfig memory cfg)
    {
        require(admin != address(0), "DeployCertificateNFT: zero admin");
        require(minter != address(0), "DeployCertificateNFT: zero minter");

        cfg = CertificateConfig({
            admin: admin, minter: minter, burner: burner == address(0) ? admin : burner
        });
    }

    /// @notice Mode B：从 JSON 字符串解析角色配置。供单测与生产共用。
    /// @dev    不做 vm.readFile，由调用者负责 IO；仅做校验与反序列化。
    ///         view 而非 pure：stdJson.keyExists 走 vm cheatcode。
    /// @param json 配置文件内容，形如 {"admin":"0x..","minter":"0x..","burner":"0x.."}。
    /// @return cfg 归一化后的配置。
    function parseCertificateConfig(string memory json)
        public
        view
        returns (CertificateConfig memory cfg)
    {
        require(bytes(json).length > 0, "DeployCertificateNFT: empty JSON");
        // 先探 key 再 read：stdJson 读缺失 key 会抛底层解析错误，信息对运维不友好。
        require(json.keyExists(".admin"), "DeployCertificateNFT: missing admin");
        require(json.keyExists(".minter"), "DeployCertificateNFT: missing minter");

        address admin = json.readAddress(".admin");
        address minter = json.readAddress(".minter");
        address burner = json.keyExists(".burner") ? json.readAddress(".burner") : address(0);

        cfg = buildConfig(admin, minter, burner);
    }

    /// @notice Mode A：从环境变量读角色配置。
    /// @dev    统一用 `vm.envOr(..., address(0))` + 显式检查，这样「变量没设」
    ///         和「设成零地址」都会命中同一条可读的报错，而不是 envAddress 的
    ///         底层 cheatcode 报错。
    /// @return cfg 归一化后的配置。
    function readEnvConfig() public view returns (CertificateConfig memory cfg) {
        address admin = vm.envOr("CERT_NFT_ADMIN_ADDRESS", address(0));
        address minter = vm.envOr("CERT_NFT_MINTER_ADDRESS", address(0));
        address burner = vm.envOr("CERT_NFT_BURNER_ADDRESS", address(0));

        require(admin != address(0), "DeployCertificateNFT: CERT_NFT_ADMIN_ADDRESS unset/zero");
        require(minter != address(0), "DeployCertificateNFT: CERT_NFT_MINTER_ADDRESS unset/zero");

        cfg = buildConfig(admin, minter, burner);
    }

    /// @notice 按优先级选模式并返回最终配置：CERT_NFT_CONFIG_PATH 非空 → Mode B。
    /// @return cfg         归一化后的配置。
    /// @return useJsonMode 是否走了 Mode B（JSON），仅用于日志展示。
    function resolveConfig() public view returns (CertificateConfig memory cfg, bool useJsonMode) {
        string memory configPath = vm.envOr("CERT_NFT_CONFIG_PATH", string(""));
        useJsonMode = bytes(configPath).length > 0;

        if (useJsonMode) {
            require(vm.exists(configPath), "DeployCertificateNFT: config file not found");
            cfg = parseCertificateConfig(vm.readFile(configPath));
        } else {
            cfg = readEnvConfig();
        }
    }

    /// @notice 判断脚本能否在部署交易之后直接 grant BURNER_ROLE。
    /// @dev    BURNER_ROLE 的 admin role 是 DEFAULT_ADMIN_ROLE，构造函数把它
    ///         发给了 `admin`；只有部署者本人 == admin 时脚本才有权限。
    /// @param deployer 广播交易的 EOA。
    /// @param admin    DEFAULT_ADMIN_ROLE 持有者。
    /// @return 是否可以由脚本直接授予 BURNER_ROLE。
    function canGrantBurnerRole(address deployer, address admin) public pure returns (bool) {
        return deployer == admin;
    }

    // --------------------------------------------------------------------
    //  部署入口
    // --------------------------------------------------------------------

    /// @notice 部署 CertificateNFT 并（在权限允许时）授予 BURNER_ROLE。
    /// @dev    返回值方便单测直接断言角色状态；`forge script` 会忽略它。
    /// @return nft 新部署的 CertificateNFT。
    function run() external returns (CertificateNFT nft) {
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);

        // chainid 安全网：脚本侧默认按 Sepolia 部署（11155111）。如果运维把
        // RPC 指向 Mainnet / 其他网络却用着 Sepolia 的测试地址，本应在播报前
        // 立即失败，而不是真的花真金把合约部署上链。
        // 覆盖方式：EXPECTED_CHAIN_ID 环境变量，缺省 Sepolia。
        // 此检查按 .claude/rules/security.md「不允许软警告」原则写成 require —— fail loud。
        uint256 expectedChainId = vm.envOr("EXPECTED_CHAIN_ID", uint256(11_155_111));
        require(
            block.chainid == expectedChainId,
            string.concat(
                "DeployCertificateNFT: chainid mismatch (got ",
                vm.toString(block.chainid),
                ", expected ",
                vm.toString(expectedChainId),
                "). Refusing to broadcast."
            )
        );

        (CertificateConfig memory cfg, bool useJsonMode) = resolveConfig();
        bool grantsBurner = canGrantBurnerRole(deployer, cfg.admin);

        vm.startBroadcast(deployerPrivateKey);
        nft = new CertificateNFT(cfg.admin, cfg.minter);
        if (grantsBurner) {
            nft.grantRole(nft.BURNER_ROLE(), cfg.burner);
        }
        vm.stopBroadcast();

        // ========== Console summary ==========
        console2.log("CertificateNFT deployed at:", address(nft));
        console2.log("Admin  (DEFAULT_ADMIN):   ", cfg.admin);
        console2.log("Minter (MINTER_ROLE):     ", cfg.minter);
        console2.log("Burner (BURNER_ROLE):     ", cfg.burner);
        console2.log("Deployer:                 ", deployer);
        console2.log("Network (chainid):        ", block.chainid);
        console2.log(
            "Mode:                     ", useJsonMode ? "B (JSON)" : "A (CERT_NFT_*_ADDRESS)"
        );
        console2.log("BURNER_ROLE granted:      ", grantsBurner ? "yes (by this script)" : "NO");
        if (!grantsBurner) {
            console2.log("---------------------------------------------------------------");
            console2.log("Deployer is not the admin, so BURNER_ROLE was NOT granted.");
            console2.log("The admin must execute:");
            console2.log(
                string.concat(
                    "  cast send ",
                    vm.toString(address(nft)),
                    " \"grantRole(bytes32,address)\" ",
                    vm.toString(nft.BURNER_ROLE()),
                    " ",
                    vm.toString(cfg.burner)
                )
            );
            console2.log("---------------------------------------------------------------");
        }
        console2.log("");
        console2.log("Next steps:");
        console2.log("  1. Copy the address above into");
        console2.log("     apps/web/src/contracts/deployments.ts");
        console2.log("     -> certificateNftDeployments.sepolia.address");
        console2.log("  2. Run: pnpm contracts:export:abi CertificateNFT");
        console2.log("  3. Run: pnpm dev");
    }
}
