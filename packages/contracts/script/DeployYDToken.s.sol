// SPDX-License-Identifier: MIT
// ============================================================================
//  DeployYDToken.s.sol  ——  把 YDToken (F05) 部署到任意 EVM 链
// ----------------------------------------------------------------------------
//  YDToken 是 YD 平台的 ERC-20 积分：固定 cap = 1e9 * 1e18，构造时一次性
//  mint 2e8 * 1e18 给 treasury 余额；剩余 8e8 留给后续 MINTER_ROLE 走治理
//  路线释放。详见 ADR-0002。
//
//  部署参数（四个地址）：
//    - admin    (DEFAULT_ADMIN_ROLE)  ：角色管理员；生产 = treasury 多签。
//    - minter   (MINTER_ROLE)         ：可铸币；生产 = treasury 多签（同一多签）。
//    - pauser   (PAUSER_ROLE)         ：紧急暂停；生产 = pauser 多签（独立）。
//    - treasury (构造时 2 亿接收方)     ：与 admin 通常是同一地址。
//
//  注意：admin / minter 在生产环境通常**是** treasury 多签（一份多签同时管
//  角色和资金），pauser 拆出独立多签避免「金库多签一锅端」风险。
//
//  YDToken 当前只有 MINTER_ROLE / PAUSER_ROLE 两个业务角色；BURNER_ROLE 与
//  burn 方法在 MVP 中未启用（ADR-0002 暂留接口位），所以本脚本不处理
//  BURNER_ROLE；如未来开启，参照 DeployCertificateNFT 的 burner 模式扩展。
//
//  三种跑法：
//    1) 本地 anvil：
//         anvil &
//         YD_TREASURY_MULTISIG=0x... YD_PAUSER_MULTISIG=0x... \
//         forge script script/DeployYDToken.s.sol:DeployYDToken \
//             --rpc-url http://127.0.0.1:8545 --private-key 0xac09...
//    2) 广播到 Sepolia + 自动 Etherscan 验证：
//         forge script script/DeployYDToken.s.sol:DeployYDToken \
//             --rpc-url $SEPOLIA_RPC_URL --broadcast --verify -vvvv
//    3) 仅模拟：省略 --broadcast。
//
//  两种配置模式：
//    Mode A（默认）：从 env 读两个地址
//        YD_TREASURY_MULTISIG  （必填；脚本会同时把它设为 admin / minter / treasury）
//        YD_PAUSER_MULTISIG    （必填）
//    Mode B：YD_CONFIG_PATH 指向 JSON 文件。Mode B 优先级更高：只要设置了
//            YD_CONFIG_PATH，上面两个 env 即被忽略。
//
//  JSON 形状（单个对象）：
//    {
//      "admin":    "0x<address>",
//      "minter":   "0x<address>",
//      "pauser":   "0x<address>",
//      "treasury": "0x<address>"
//    }
//
//  关于 ADMIN_ROLE 的转移：
//    构造函数会把 DEFAULT_ADMIN_ROLE 发给 deployer；脚本部署完成后立刻
//    `revokeRole(DEFAULT_ADMIN_ROLE, deployer)` + `grantRole(DEFAULT_ADMIN_ROLE, admin)`
//    把 admin 移交走。「移交到 deployer 控制的临时地址，再二次移交」的方案
//    会留一段 deployer 可越权的窗口，ADR-0002 已否决。
//    （YDToken 当前继承的是 plain AccessControl，未启用 AccessControlDefaultAdminRules
//    的 beginDefaultAdminTransfer 2-step；如未来切到该扩展，可在这里替换为
//    beginDefaultAdminTransfer + acceptDefaultAdminTransfer 模式。）
// ============================================================================

pragma solidity ^0.8.24;

import {Script, console2} from "forge-std/Script.sol";
import {stdJson} from "forge-std/StdJson.sol";
import {YDToken} from "../src/YDToken.sol";

/// @notice 部署 YDToken，支持 Mode A（env 地址）与 Mode B（JSON 配置）。
/// @dev    配置解析 / 校验的纯逻辑拆成 `buildConfig` / `parseYDConfig` /
///         `readEnvConfig` / `resolveConfig`，便于单测在不广播的情况下覆盖。
contract DeployYDToken is Script {
    using stdJson for string;

    /// @notice 部署所需的角色地址 + 资金接收地址。
    /// @dev    treasury 在 Mode A 下会同时作为 admin / minter。
    struct YDConfig {
        address admin;
        address minter;
        address pauser;
        address treasury;
    }

    // --------------------------------------------------------------------
    //  配置构建与校验（纯逻辑，可单测）
    // --------------------------------------------------------------------

    /// @notice 校验并归一化四个角色地址 + treasury；两种模式共用同一套规则。
    /// @dev    四个地址字段均为必填且非零；无可选字段。
    /// @param admin     DEFAULT_ADMIN_ROLE 持有者，必填。
    /// @param minter    MINTER_ROLE 持有者，必填。
    /// @param pauser    PAUSER_ROLE 持有者，必填。
    /// @param treasury  构造时 2 亿 YD 的接收方，必填（通常 == admin 多签）。
    /// @return cfg      归一化后的配置。
    function buildConfig(address admin, address minter, address pauser, address treasury)
        public
        pure
        returns (YDConfig memory cfg)
    {
        require(admin != address(0), "DeployYDToken: zero admin");
        require(minter != address(0), "DeployYDToken: zero minter");
        require(pauser != address(0), "DeployYDToken: zero pauser");
        require(treasury != address(0), "DeployYDToken: zero treasury");

        cfg = YDConfig({admin: admin, minter: minter, pauser: pauser, treasury: treasury});
    }

    /// @notice Mode B：从 JSON 字符串解析角色配置。供单测与生产共用。
    /// @dev    不做 vm.readFile，由调用者负责 IO；仅做校验与反序列化。
    ///         view 而非 pure：stdJson.keyExists 走 vm cheatcode。
    /// @param json 配置文件内容，形如 {"admin":"0x..","minter":"0x..","pauser":"0x..","treasury":"0x.."}。
    /// @return cfg 归一化后的配置。
    function parseYDConfig(string memory json) public view returns (YDConfig memory cfg) {
        require(bytes(json).length > 0, "DeployYDToken: empty JSON");
        // 先探 key 再 read：stdJson 读缺失 key 会抛底层解析错误，信息对运维不友好。
        require(json.keyExists(".admin"), "DeployYDToken: missing admin");
        require(json.keyExists(".minter"), "DeployYDToken: missing minter");
        require(json.keyExists(".pauser"), "DeployYDToken: missing pauser");
        require(json.keyExists(".treasury"), "DeployYDToken: missing treasury");

        address admin = json.readAddress(".admin");
        address minter = json.readAddress(".minter");
        address pauser = json.readAddress(".pauser");
        address treasury = json.readAddress(".treasury");

        cfg = buildConfig(admin, minter, pauser, treasury);
    }

    /// @notice Mode A：从环境变量读角色配置。
    /// @dev    统一用 `vm.envOr(..., address(0))` + 显式检查，这样「变量没设」
    ///         和「设成零地址」都会命中同一条可读的报错，而不是 envAddress 的
    ///         底层 cheatcode 报错。
    /// @return cfg 归一化后的配置。
    function readEnvConfig() public view returns (YDConfig memory cfg) {
        address treasury = vm.envOr("YD_TREASURY_MULTISIG", address(0));
        address pauser = vm.envOr("YD_PAUSER_MULTISIG", address(0));

        require(treasury != address(0), "DeployYDToken: YD_TREASURY_MULTISIG unset/zero");
        require(pauser != address(0), "DeployYDToken: YD_PAUSER_MULTISIG unset/zero");

        // Mode A 默认：admin / minter = treasury（同一多签同时管角色和资金），
        // 详见 ADR-0002 与合约顶部注释。
        cfg = buildConfig({admin: treasury, minter: treasury, pauser: pauser, treasury: treasury});
    }

    /// @notice 按优先级选模式并返回最终配置：YD_CONFIG_PATH 非空 → Mode B。
    /// @return cfg         归一化后的配置。
    /// @return useJsonMode 是否走了 Mode B（JSON），仅用于日志展示。
    function resolveConfig() public view returns (YDConfig memory cfg, bool useJsonMode) {
        string memory configPath = vm.envOr("YD_CONFIG_PATH", string(""));
        useJsonMode = bytes(configPath).length > 0;

        if (useJsonMode) {
            require(vm.exists(configPath), "DeployYDToken: config file not found");
            cfg = parseYDConfig(vm.readFile(configPath));
        } else {
            cfg = readEnvConfig();
        }
    }

    // --------------------------------------------------------------------
    //  部署入口
    // --------------------------------------------------------------------

    /// @notice 部署 YDToken 并把 admin 收尾到目标多签。
    /// @dev    返回值方便单测直接断言角色状态；`forge script` 会忽略它。
    /// @return token 新部署的 YDToken。
    function run() external returns (YDToken token) {
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
                "DeployYDToken: chainid mismatch (got ",
                vm.toString(block.chainid),
                ", expected ",
                vm.toString(expectedChainId),
                "). Refusing to broadcast."
            )
        );

        (YDConfig memory cfg, bool useJsonMode) = resolveConfig();

        // 构造时 mint 2e8 给 treasury。Mode A 默认 cfg.treasury = cfg.minter =
        // cfg.admin = deployer-bootstrap-on-constructor，构造结束时 deployer
        // 拥有 admin，但 treasury 资金接收地址通常是独立多签。真实部署场景
        // 下 cfg.treasury 是多签、cfg.admin 也是同一多签，deployer 在脚本
        // 完成后立刻失去 admin。
        uint256 initialSupply = 200_000_000 * 1e18;

        vm.startBroadcast(deployerPrivateKey);
        token = new YDToken(deployer, cfg.minter, cfg.pauser, cfg.treasury);

        // admin 移交：deployer 在构造时拿到 DEFAULT_ADMIN_ROLE，部署完成后
        // 立即把 admin 转交给 cfg.admin（多签），再让 deployer 放弃剩余的 admin。
        // 必须**先 grant 再 revoke**：grantRole 需要 `onlyRole(DEFAULT_ADMIN_ROLE)`，
        // 一旦 deployer 撤销自己就失去该权限，后续 grant 会 revert。
        if (cfg.admin != deployer) {
            token.grantRole(token.DEFAULT_ADMIN_ROLE(), cfg.admin);
            token.revokeRole(token.DEFAULT_ADMIN_ROLE(), deployer);
        }
        vm.stopBroadcast();

        // ========== Console summary ==========
        console2.log("YDToken deployed at:            ", address(token));
        console2.log("Cap:                            ", token.cap());
        console2.log("Initial supply (minted in ctor):", initialSupply);
        console2.log("Total supply after deploy:      ", token.totalSupply());
        console2.log("Treasury balance:               ", token.balanceOf(cfg.treasury));
        console2.log("");
        console2.log("Admin  (DEFAULT_ADMIN_ROLE):    ", cfg.admin);
        console2.log("Minter (MINTER_ROLE):           ", cfg.minter);
        console2.log("Pauser (PAUSER_ROLE):           ", cfg.pauser);
        console2.log("Treasury (initial mint target): ", cfg.treasury);
        console2.log("Deployer:                       ", deployer);
        console2.log("Network (chainid):              ", block.chainid);
        console2.log(
            "Mode:                           ", useJsonMode ? "B (JSON)" : "A (YD_*_MULTISIG)"
        );
        console2.log("Paused:                         ", token.paused());
        if (cfg.admin != deployer) {
            console2.log(
                "DEFAULT_ADMIN_ROLE:             ", "transferred from deployer to ", cfg.admin
            );
        } else {
            console2.log(
                "DEFAULT_ADMIN_ROLE:             ", "kept on deployer (cfg.admin == deployer)"
            );
        }
        console2.log("");
        console2.log("Next steps:");
        console2.log("  1. Copy the address above into");
        console2.log("     apps/web/src/contracts/deployments.ts");
        console2.log("     -> ydTokenDeployments.target.address");
        console2.log("  2. Run: pnpm contracts:export:abi YDToken");
        console2.log("  3. Run: pnpm dev");
    }
}
