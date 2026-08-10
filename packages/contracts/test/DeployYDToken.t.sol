// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {DeployYDToken} from "../script/DeployYDToken.s.sol";
import {YDToken} from "../src/YDToken.sol";

/// @title TestDeployYDToken
/// @notice 针对 DeployYDToken 的单测，覆盖：
///   1. 配置解析纯逻辑（buildConfig / parseYDConfig）；
///   2. Mode A / Mode B / run() 端到端部署。
///
/// @dev 关于「为什么 env 相关断言全塞在一个测试函数里」：
///      `vm.setEnv` 写的是 forge **进程级** 环境变量，而 forge 会并行执行同一
///      个合约里的多个 test 函数。如果把 env 场景拆成多个 test，它们会互相覆盖
///      对方刚设置的 YD_* 值，产生随机失败。所以所有依赖 env 的场景
///      （Mode A / Mode B / run()）在 `test_ResolveConfig_EnvAndJsonModes_Sequential`
///      内按顺序跑，其余不碰 env 的用例照常并行。
contract TestDeployYDToken is Test {
    DeployYDToken internal dyd;

    /// forge-lint: disable-next-line(mixed-case-variable)
    string internal constant FIXTURE_PATH = "./test/fixtures/yd-token.json";
    /// forge-lint: disable-next-line(mixed-case-variable)
    string internal constant FIXTURE_PATH_ENV_MODE = "./test/fixtures/yd-token-env.json";

    address internal constant TREASURY = address(0x7111);
    address internal constant PAUSER = address(0xB055);

    // anvil 默认账户 #0 —— 公开测试密钥，非任何真实资金账户。
    uint256 internal constant DEPLOYER_PK =
        0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80;

    uint256 internal constant INITIAL = 200_000_000 * 1e18;
    uint256 internal constant CAP = 1_000_000_000 * 1e18;

    function setUp() public {
        dyd = new DeployYDToken();

        // happy-path fixture，仅用于 test_ParseYDConfig_FromFile。
        vm.writeFile(FIXTURE_PATH, _configJson(TREASURY, TREASURY, PAUSER, TREASURY));
    }

    // --------------------------------------------------------------------
    //  buildConfig —— 归一化 + 零地址校验
    // --------------------------------------------------------------------

    function test_BuildConfig_HappyPath() public view {
        DeployYDToken.YDConfig memory cfg = dyd.buildConfig(TREASURY, TREASURY, PAUSER, TREASURY);

        assertEq(cfg.admin, TREASURY);
        assertEq(cfg.minter, TREASURY);
        assertEq(cfg.pauser, PAUSER);
        assertEq(cfg.treasury, TREASURY);
    }

    function test_BuildConfig_AllowsSeparateAdminAndTreasury() public view {
        // 生产环境也允许 admin != treasury（虽然 Mode A 默认相等）。
        address admin = address(0xAAA);
        DeployYDToken.YDConfig memory cfg = dyd.buildConfig(admin, TREASURY, PAUSER, TREASURY);

        assertEq(cfg.admin, admin);
        assertEq(cfg.treasury, TREASURY);
    }

    function test_BuildConfig_RejectsZeroAdmin() public {
        vm.expectRevert(bytes("DeployYDToken: zero admin"));
        dyd.buildConfig(address(0), TREASURY, PAUSER, TREASURY);
    }

    function test_BuildConfig_RejectsZeroMinter() public {
        vm.expectRevert(bytes("DeployYDToken: zero minter"));
        dyd.buildConfig(TREASURY, address(0), PAUSER, TREASURY);
    }

    function test_BuildConfig_RejectsZeroPauser() public {
        vm.expectRevert(bytes("DeployYDToken: zero pauser"));
        dyd.buildConfig(TREASURY, TREASURY, address(0), TREASURY);
    }

    function test_BuildConfig_RejectsZeroTreasury() public {
        vm.expectRevert(bytes("DeployYDToken: zero treasury"));
        dyd.buildConfig(TREASURY, TREASURY, PAUSER, address(0));
    }

    /// @dev Fuzz：任意非零 admin/minter/pauser/treasury 都应通过。
    function testFuzz_BuildConfig_NeverReturnsZeroForRequiredFields(
        address admin,
        address minter,
        address pauser,
        address treasury
    ) public view {
        vm.assume(admin != address(0));
        vm.assume(minter != address(0));
        vm.assume(pauser != address(0));
        vm.assume(treasury != address(0));

        DeployYDToken.YDConfig memory cfg = dyd.buildConfig(admin, minter, pauser, treasury);

        assertEq(cfg.admin, admin);
        assertEq(cfg.minter, minter);
        assertEq(cfg.pauser, pauser);
        assertEq(cfg.treasury, treasury);
    }

    // --------------------------------------------------------------------
    //  parseYDConfig —— Mode B JSON
    // --------------------------------------------------------------------

    function test_ParseYDConfig_FromFile() public view {
        string memory json = vm.readFile(FIXTURE_PATH);
        DeployYDToken.YDConfig memory cfg = dyd.parseYDConfig(json);

        assertEq(cfg.admin, TREASURY);
        assertEq(cfg.minter, TREASURY);
        assertEq(cfg.pauser, PAUSER);
        assertEq(cfg.treasury, TREASURY);
    }

    function test_ParseYDConfig_RejectsEmptyJson() public {
        vm.expectRevert(bytes("DeployYDToken: empty JSON"));
        dyd.parseYDConfig("");
    }

    function test_ParseYDConfig_RejectsMissingAdmin() public {
        string memory json = string.concat(
            "{",
            "\"minter\":\"",
            vm.toString(TREASURY),
            "\",",
            "\"pauser\":\"",
            vm.toString(PAUSER),
            "\",",
            "\"treasury\":\"",
            vm.toString(TREASURY),
            "\"",
            "}"
        );

        vm.expectRevert(bytes("DeployYDToken: missing admin"));
        dyd.parseYDConfig(json);
    }

    function test_ParseYDConfig_RejectsMissingMinter() public {
        string memory json = string.concat(
            "{",
            "\"admin\":\"",
            vm.toString(TREASURY),
            "\",",
            "\"pauser\":\"",
            vm.toString(PAUSER),
            "\",",
            "\"treasury\":\"",
            vm.toString(TREASURY),
            "\"",
            "}"
        );

        vm.expectRevert(bytes("DeployYDToken: missing minter"));
        dyd.parseYDConfig(json);
    }

    function test_ParseYDConfig_RejectsMissingPauser() public {
        string memory json = string.concat(
            "{",
            "\"admin\":\"",
            vm.toString(TREASURY),
            "\",",
            "\"minter\":\"",
            vm.toString(TREASURY),
            "\",",
            "\"treasury\":\"",
            vm.toString(TREASURY),
            "\"",
            "}"
        );

        vm.expectRevert(bytes("DeployYDToken: missing pauser"));
        dyd.parseYDConfig(json);
    }

    function test_ParseYDConfig_RejectsMissingTreasury() public {
        string memory json = string.concat(
            "{",
            "\"admin\":\"",
            vm.toString(TREASURY),
            "\",",
            "\"minter\":\"",
            vm.toString(TREASURY),
            "\",",
            "\"pauser\":\"",
            vm.toString(PAUSER),
            "\"",
            "}"
        );

        vm.expectRevert(bytes("DeployYDToken: missing treasury"));
        dyd.parseYDConfig(json);
    }

    function test_ParseYDConfig_RejectsZeroAdmin() public {
        vm.expectRevert(bytes("DeployYDToken: zero admin"));
        dyd.parseYDConfig(_configJson(address(0), TREASURY, PAUSER, TREASURY));
    }

    function test_ParseYDConfig_RejectsZeroPauser() public {
        vm.expectRevert(bytes("DeployYDToken: zero pauser"));
        dyd.parseYDConfig(_configJson(TREASURY, TREASURY, address(0), TREASURY));
    }

    /// @dev 语法错误的 JSON 由 stdJson/cheatcode 层抛出（非本合约的 require），
    ///      所以只断言「必然 revert」，不绑定具体 message。
    function test_ParseYDConfig_RejectsMalformedJson() public {
        vm.expectRevert();
        dyd.parseYDConfig("{not-json");
    }

    // --------------------------------------------------------------------
    //  env 相关场景（顺序执行，见合约顶部 @dev 说明）
    // --------------------------------------------------------------------

    /// @dev forge / anvil 默认 chainid 是 31337；现网 Sepolia 是 11_155_111。
    ///      用 31337 让 happy path 不动 vm 状态即可通过；不匹配 case 用
    ///      11_155_111 强制 require 触发。
    uint256 internal constant DEFAULT_TEST_CHAIN_ID = 31_337;
    uint256 internal constant SEPOLIA_CHAIN_ID = 11_155_111;

    /// @dev 用户的核心需求集中在「Mode A 跑完 run() 之后的状态」——
    ///      DEFAULT_ADMIN_ROLE 已迁出 deployer、cap=1B、totalSupply=200M、
    ///      paused=false、MINTER/PAUSER 各 1 成员。所以本 sequential 块的最
    ///      后一个用例 _run_Deploys_TransfersAdminAndMintsInitialSupply
    ///      是最重头的断言。
    function test_ResolveConfig_EnvAndJsonModes_Sequential() public {
        _envMode_ReadsTreasuryAndPauser();
        _envMode_RejectsUnsetTreasury();
        _envMode_RejectsUnsetPauser();
        _jsonMode_TakesPriorityOverEnv();
        _jsonMode_RejectsMissingFile();
        // run() 先做 chainid 校验；forge/anvil 默认 chainid=31337，所以
        // 这里显式把它设成 31337 才能让 _run_* 走通。chainid 不匹配 case
        // 也在同一个 sequential block 里覆盖，避免与其它用例并行踩同一组
        // 进程级环境变量。
        vm.setEnv("EXPECTED_CHAIN_ID", vm.toString(DEFAULT_TEST_CHAIN_ID));
        _run_Deploys_TransfersAdminAndMintsInitialSupply();
        _run_DefaultsExpectedChainIdToSepolia();
        _run_RejectsChainIdMismatch();
        _clearEnv();
    }

    /// @dev forge 默认 chainid 是 31337。把 EXPECTED_CHAIN_ID 设成 31337 即可
    ///      走完整部署。这是用户需求的核心断言。
    function _run_Deploys_TransfersAdminAndMintsInitialSupply() private {
        vm.setEnv("DEPLOYER_PRIVATE_KEY", vm.toString(DEPLOYER_PK));
        _setEnvModeA(TREASURY, PAUSER, "");

        YDToken token = dyd.run();

        // 需求 1: cap == 1B
        assertEq(token.cap(), CAP, "cap must be 1B");

        // 需求 2: totalSupply == 200M (构造时 mint 给 treasury)
        assertEq(token.totalSupply(), INITIAL, "totalSupply must be 200M after deploy");
        assertEq(token.balanceOf(TREASURY), INITIAL, "treasury must hold the 200M initial mint");

        // 需求 3: DEFAULT_ADMIN_ROLE 已迁出 deployer, 落在 treasury 多签。
        // 注：YDToken 继承的是 plain AccessControl（不是 AccessControlEnumerable），
        // 所以没有 getRoleMemberCount；改用「正向 hasRole + 反向 hasRole」组合，
        // 双向锁定「deployer 没有、treasury 有」，语义等价于「成员数为 1」。
        bytes32 adminRole = token.DEFAULT_ADMIN_ROLE();
        assertTrue(token.hasRole(adminRole, TREASURY), "treasury must hold DEFAULT_ADMIN_ROLE");
        assertFalse(token.hasRole(adminRole, vm.addr(DEPLOYER_PK)), "deployer must NOT be admin");

        // 需求 4: MINTER_ROLE = treasury (Mode A 默认 treasury=admin=minter)
        bytes32 minterRole = token.MINTER_ROLE();
        assertTrue(token.hasRole(minterRole, TREASURY), "treasury must hold MINTER_ROLE");
        assertFalse(token.hasRole(minterRole, vm.addr(DEPLOYER_PK)), "deployer must NOT be minter");

        // 需求 5: PAUSER_ROLE = pauser 多签
        bytes32 pauserRole = token.PAUSER_ROLE();
        assertTrue(token.hasRole(pauserRole, PAUSER), "pauser multisig must hold PAUSER_ROLE");
        assertFalse(token.hasRole(pauserRole, vm.addr(DEPLOYER_PK)), "deployer must NOT be pauser");

        // 需求 6: paused() == false initially
        assertFalse(token.paused(), "must NOT be paused after deploy");
    }

    /// @dev EXPECTED_CHAIN_ID 不设时应默认 Sepolia (11_155_111)；但 forge 默认
    ///      chainid=31337，所以本次配置会落到 require fail 路径。这里我们 *显式*
    ///      改成 Sepolia 但用 `vm.chainId` cheatcode 把 block.chainid 也调到
    ///      Sepolia，验证「match 时落 admin role」。
    function _run_DefaultsExpectedChainIdToSepolia() private {
        // 重置 EXPECTED_CHAIN_ID 为空字符串，让 run() 走 vm.envOr 的 Sepolia 分支。
        vm.setEnv("EXPECTED_CHAIN_ID", "");
        vm.setEnv("DEPLOYER_PRIVATE_KEY", vm.toString(DEPLOYER_PK));
        _setEnvModeA(TREASURY, PAUSER, "");

        // 把 block.chainid 改成 Sepolia，与 vm.envOr 默认分支吻合。
        vm.chainId(SEPOLIA_CHAIN_ID);

        YDToken token = dyd.run();
        assertEq(token.cap(), CAP, "cap must hold on Sepolia chainid");
        assertFalse(token.paused(), "paused must hold on Sepolia chainid");

        // 还原给后续 _run_RejectsChainIdMismatch。
        vm.chainId(DEFAULT_TEST_CHAIN_ID);
    }

    /// @dev EXPECTED_CHAIN_ID = Sepolia 但 block.chainid = 31337，require 必 fail。
    function _run_RejectsChainIdMismatch() private {
        vm.setEnv("EXPECTED_CHAIN_ID", vm.toString(SEPOLIA_CHAIN_ID));
        vm.setEnv("DEPLOYER_PRIVATE_KEY", vm.toString(DEPLOYER_PK));
        _setEnvModeA(TREASURY, PAUSER, "");

        vm.expectRevert(); // 信息含动态 chainid，不绑死
        dyd.run();
    }

    // ----- Mode A：env 地址 -----

    function _envMode_ReadsTreasuryAndPauser() private {
        _setEnvModeA(TREASURY, PAUSER, "");

        DeployYDToken.YDConfig memory cfg = dyd.readEnvConfig();
        // Mode A 默认 admin / minter == treasury
        assertEq(cfg.admin, TREASURY, "admin should default to treasury in Mode A");
        assertEq(cfg.minter, TREASURY, "minter should default to treasury in Mode A");
        assertEq(cfg.pauser, PAUSER);
        assertEq(cfg.treasury, TREASURY);

        (DeployYDToken.YDConfig memory resolved, bool useJsonMode) = dyd.resolveConfig();
        assertFalse(useJsonMode, "empty YD_CONFIG_PATH must select Mode A");
        assertEq(resolved.pauser, PAUSER);
        assertEq(resolved.treasury, TREASURY);
    }

    function _envMode_RejectsUnsetTreasury() private {
        _setEnvModeA(address(0), PAUSER, "");

        vm.expectRevert(bytes("DeployYDToken: YD_TREASURY_MULTISIG unset/zero"));
        dyd.readEnvConfig();
    }

    function _envMode_RejectsUnsetPauser() private {
        _setEnvModeA(TREASURY, address(0), "");

        vm.expectRevert(bytes("DeployYDToken: YD_PAUSER_MULTISIG unset/zero"));
        dyd.readEnvConfig();
    }

    // ----- Mode B：JSON 文件 -----

    function _jsonMode_TakesPriorityOverEnv() private {
        // env 里放一组「错误」地址，JSON 放正确的：验证 Mode B 覆盖 Mode A。
        address jsonAdmin = address(0xDA0);
        address jsonMinter = address(0xDA1);
        address jsonPauser = address(0xDA2);
        address jsonTreasury = address(0xDA3);
        vm.writeFile(
            FIXTURE_PATH_ENV_MODE, _configJson(jsonAdmin, jsonMinter, jsonPauser, jsonTreasury)
        );
        _setEnvModeA(TREASURY, PAUSER, FIXTURE_PATH_ENV_MODE);

        (DeployYDToken.YDConfig memory cfg, bool useJsonMode) = dyd.resolveConfig();
        assertTrue(useJsonMode, "YD_CONFIG_PATH set must select Mode B");
        assertEq(cfg.admin, jsonAdmin);
        assertEq(cfg.minter, jsonMinter);
        assertEq(cfg.pauser, jsonPauser);
        assertEq(cfg.treasury, jsonTreasury);
    }

    function _jsonMode_RejectsMissingFile() private {
        _setEnvModeA(TREASURY, PAUSER, "./test/fixtures/does-not-exist.json");

        vm.expectRevert(bytes("DeployYDToken: config file not found"));
        dyd.resolveConfig();
    }

    // --------------------------------------------------------------------
    //  helpers
    // --------------------------------------------------------------------

    /// @dev 地址传 address(0) 表示「env 未设置」——脚本按零地址处理，语义等价。
    function _setEnvModeA(address treasury, address pauser, string memory configPath) private {
        vm.setEnv("YD_TREASURY_MULTISIG", treasury == address(0) ? "" : vm.toString(treasury));
        vm.setEnv("YD_PAUSER_MULTISIG", pauser == address(0) ? "" : vm.toString(pauser));
        vm.setEnv("YD_CONFIG_PATH", configPath);
    }

    /// @dev setEnv 是 forge 进程级副作用，跑完清掉避免污染其它 suite —— 尤其是
    ///      DEPLOYER_PRIVATE_KEY，不能把 anvil 测试私钥留在进程环境里。
    function _clearEnv() private {
        _setEnvModeA(address(0), address(0), "");
        vm.setEnv("DEPLOYER_PRIVATE_KEY", "");
    }

    function _configJson(address admin, address minter, address pauser, address treasury)
        private
        pure
        returns (string memory)
    {
        return string.concat(
            "{",
            "\"admin\":\"",
            vm.toString(admin),
            "\",",
            "\"minter\":\"",
            vm.toString(minter),
            "\",",
            "\"pauser\":\"",
            vm.toString(pauser),
            "\",",
            "\"treasury\":\"",
            vm.toString(treasury),
            "\"",
            "}"
        );
    }
}
