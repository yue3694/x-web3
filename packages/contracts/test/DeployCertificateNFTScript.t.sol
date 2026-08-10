// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {DeployCertificateNFT} from "../script/DeployCertificateNFT.s.sol";
import {CertificateNFT} from "../src/CertificateNFT.sol";

/// @title TestDeployCertificateNFTScript
/// @notice 针对 DeployCertificateNFT 配置解析 / 校验逻辑的单测。
///         纯逻辑（buildConfig / parseCertificateConfig / canGrantBurnerRole）
///         直接传参断言；仅 happy path 走一次 vm.readFile 验证 IO 端到端。
///
/// @dev 关于「为什么 env 相关断言全塞在一个测试函数里」：
///      `vm.setEnv` 写的是 forge **进程级** 环境变量，而 forge 会并行执行同一
///      个合约里的多个 test 函数。如果把 env 场景拆成多个 test，它们会互相覆盖
///      对方刚设置的 CERT_NFT_* 值，产生随机失败。所以所有依赖 env 的场景
///      （Mode A / Mode B / run()）在 `test_ResolveConfig_EnvAndJsonModes_Sequential`
///      内按顺序跑，其余不碰 env 的用例照常并行。
contract TestDeployCertificateNFTScript is Test {
    DeployCertificateNFT internal dcn;

    // fixture 路径按用例区分：同名文件被并行用例写入不同内容会互相打架。
    /// forge-lint: disable-next-line(mixed-case-variable)
    string internal constant FIXTURE_PATH = "./test/fixtures/certificate-nft.json";
    /// forge-lint: disable-next-line(mixed-case-variable)
    string internal constant FIXTURE_PATH_ENV_MODE = "./test/fixtures/certificate-nft-env.json";

    address internal constant ADMIN = address(0xA11CE);
    address internal constant MINTER = address(0x711117E4);
    address internal constant BURNER = address(0xB0B);

    // anvil 默认账户 #0 —— 公开测试密钥，非任何真实资金账户。
    uint256 internal constant DEPLOYER_PK =
        0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80;

    function setUp() public {
        dcn = new DeployCertificateNFT();

        // happy-path fixture，仅用于 test_ParseCertificateConfig_FromFile。
        vm.writeFile(FIXTURE_PATH, _configJson(ADMIN, MINTER, BURNER));
    }

    // --------------------------------------------------------------------
    //  buildConfig —— 归一化 + 零地址校验
    // --------------------------------------------------------------------

    function test_BuildConfig_HappyPath() public view {
        DeployCertificateNFT.CertificateConfig memory cfg = dcn.buildConfig(ADMIN, MINTER, BURNER);

        assertEq(cfg.admin, ADMIN);
        assertEq(cfg.minter, MINTER);
        assertEq(cfg.burner, BURNER);
    }

    function test_BuildConfig_DefaultsBurnerToAdmin() public view {
        DeployCertificateNFT.CertificateConfig memory cfg =
            dcn.buildConfig(ADMIN, MINTER, address(0));

        assertEq(cfg.burner, ADMIN, "burner should fall back to admin");
    }

    function test_BuildConfig_RejectsZeroAdmin() public {
        vm.expectRevert(bytes("DeployCertificateNFT: zero admin"));
        dcn.buildConfig(address(0), MINTER, BURNER);
    }

    function test_BuildConfig_RejectsZeroMinter() public {
        vm.expectRevert(bytes("DeployCertificateNFT: zero minter"));
        dcn.buildConfig(ADMIN, address(0), BURNER);
    }

    /// @dev Fuzz：任意非零 admin/minter 都应通过，且 burner 缺省一定回落到 admin。
    function testFuzz_BuildConfig_NeverReturnsZeroAddress(
        address admin,
        address minter,
        address burner
    ) public view {
        vm.assume(admin != address(0));
        vm.assume(minter != address(0));

        DeployCertificateNFT.CertificateConfig memory cfg = dcn.buildConfig(admin, minter, burner);

        assertEq(cfg.admin, admin);
        assertEq(cfg.minter, minter);
        assertTrue(cfg.burner != address(0), "burner must never be zero");
        assertEq(cfg.burner, burner == address(0) ? admin : burner);
    }

    // --------------------------------------------------------------------
    //  parseCertificateConfig —— Mode B JSON
    // --------------------------------------------------------------------

    function test_ParseCertificateConfig_FromFile() public view {
        string memory json = vm.readFile(FIXTURE_PATH);
        DeployCertificateNFT.CertificateConfig memory cfg = dcn.parseCertificateConfig(json);

        assertEq(cfg.admin, ADMIN);
        assertEq(cfg.minter, MINTER);
        assertEq(cfg.burner, BURNER);
    }

    function test_ParseCertificateConfig_OmittedBurnerDefaultsToAdmin() public view {
        string memory json = string.concat(
            "{",
            "\"admin\":\"",
            vm.toString(ADMIN),
            "\",",
            "\"minter\":\"",
            vm.toString(MINTER),
            "\"",
            "}"
        );

        DeployCertificateNFT.CertificateConfig memory cfg = dcn.parseCertificateConfig(json);
        assertEq(cfg.burner, ADMIN, "missing burner key should fall back to admin");
    }

    function test_ParseCertificateConfig_ZeroBurnerDefaultsToAdmin() public view {
        DeployCertificateNFT.CertificateConfig memory cfg =
            dcn.parseCertificateConfig(_configJson(ADMIN, MINTER, address(0)));

        assertEq(cfg.burner, ADMIN, "explicit zero burner should fall back to admin");
    }

    function test_ParseCertificateConfig_RejectsEmptyJson() public {
        vm.expectRevert(bytes("DeployCertificateNFT: empty JSON"));
        dcn.parseCertificateConfig("");
    }

    function test_ParseCertificateConfig_RejectsMissingAdmin() public {
        string memory json = string.concat("{\"minter\":\"", vm.toString(MINTER), "\"}");

        vm.expectRevert(bytes("DeployCertificateNFT: missing admin"));
        dcn.parseCertificateConfig(json);
    }

    function test_ParseCertificateConfig_RejectsMissingMinter() public {
        string memory json = string.concat("{\"admin\":\"", vm.toString(ADMIN), "\"}");

        vm.expectRevert(bytes("DeployCertificateNFT: missing minter"));
        dcn.parseCertificateConfig(json);
    }

    function test_ParseCertificateConfig_RejectsZeroAdmin() public {
        vm.expectRevert(bytes("DeployCertificateNFT: zero admin"));
        dcn.parseCertificateConfig(_configJson(address(0), MINTER, BURNER));
    }

    function test_ParseCertificateConfig_RejectsZeroMinter() public {
        vm.expectRevert(bytes("DeployCertificateNFT: zero minter"));
        dcn.parseCertificateConfig(_configJson(ADMIN, address(0), BURNER));
    }

    /// @dev 语法错误的 JSON 由 stdJson/cheatcode 层抛出（非本合约的 require），
    ///      所以只断言「必然 revert」，不绑定具体 message。
    function test_ParseCertificateConfig_RejectsMalformedJson() public {
        vm.expectRevert();
        dcn.parseCertificateConfig("{not-json");
    }

    /// @dev admin 字段类型不是地址字符串时，parseJsonAddress 会 revert。
    function test_ParseCertificateConfig_RejectsNonAddressAdmin() public {
        string memory json =
            string.concat("{\"admin\":123,\"minter\":\"", vm.toString(MINTER), "\"}");

        vm.expectRevert();
        dcn.parseCertificateConfig(json);
    }

    // --------------------------------------------------------------------
    //  canGrantBurnerRole
    // --------------------------------------------------------------------

    function test_CanGrantBurnerRole_TrueWhenDeployerIsAdmin() public view {
        assertTrue(dcn.canGrantBurnerRole(ADMIN, ADMIN));
    }

    function test_CanGrantBurnerRole_FalseWhenAdminIsMultisig() public view {
        assertFalse(dcn.canGrantBurnerRole(vm.addr(DEPLOYER_PK), ADMIN));
    }

    // --------------------------------------------------------------------
    //  env 相关场景（顺序执行，见合约顶部 @dev 说明）
    // --------------------------------------------------------------------

    /// @dev forge / anvil 默认 chainid 是 31337；现网 Sepolia 是 11_155_111。
    ///      用 31337 让 happy path 不动 vm 状态即可通过；不匹配 case 用
    ///      11_155_111 强制 require 触发。
    uint256 internal constant DEFAULT_TEST_CHAIN_ID = 31_337;
    uint256 internal constant SEPOLIA_CHAIN_ID = 11_155_111;

    function test_ResolveConfig_EnvAndJsonModes_Sequential() public {
        _envMode_ReadsAllThreeAddresses();
        _envMode_DefaultsBurnerToAdmin();
        _envMode_RejectsUnsetAdmin();
        _envMode_RejectsUnsetMinter();
        _jsonMode_TakesPriorityOverEnv();
        _jsonMode_RejectsMissingFile();
        // run() 现在先做 chainid 校验；forge/anvil 默认 chainid=31337，所以
        // 这里显式把它设成 31337 才能让 _run_* 走通。codex #15 新增的
        // 「不匹配」/「默认 Sepolia」场景也在同一个 sequential block 里覆盖，
        // 避免与其它用例并行踩同一组进程级环境变量。
        vm.setEnv("EXPECTED_CHAIN_ID", vm.toString(DEFAULT_TEST_CHAIN_ID));
        _run_DeploysAndGrantsBurnerWhenDeployerIsAdmin();
        _run_SkipsBurnerGrantWhenAdminIsExternal();
        _run_AcceptsMatchingChainId();
        _run_DefaultsExpectedChainIdToSepolia();
        _run_RejectsChainIdMismatch();
        _clearEnv();
    }

    /// @dev forge 默认 chainid 是 31337。把 EXPECTED_CHAIN_ID 设成 31337 即可
    ///      走完整部署；admin 角色应落在 ADMIN 常量上。
    function _run_AcceptsMatchingChainId() private {
        // EXPEXTED_CHAIN_ID 已经在 sequential 顶部设成 DEFAULT_TEST_CHAIN_ID；
        // 这里再覆盖一次防御性，确保这条用例单独跑也能过。
        vm.setEnv("EXPECTED_CHAIN_ID", vm.toString(DEFAULT_TEST_CHAIN_ID));
        vm.setEnv("DEPLOYER_PRIVATE_KEY", vm.toString(DEPLOYER_PK));
        _setEnv(ADMIN, MINTER, BURNER, "");

        CertificateNFT nft = dcn.run();
        assertTrue(nft.hasRole(nft.DEFAULT_ADMIN_ROLE(), ADMIN), "admin role on matching chain");
    }

    /// @dev EXPECTED_CHAIN_ID 不设时应默认 Sepolia (11_155_111)；但 forge 默认
    ///      chainid=31337，所以本次配置会落到 require fail 路径。这里我们 *显式*
    ///      改成 Sepolia 但用 `vm.chainId` cheatcode 把 block.chainid 也调到
    ///      Sepolia，验证「match 时落 admin role」。这条路径给真实 Sepolia 部署做
    ///      smoke 用。
    function _run_DefaultsExpectedChainIdToSepolia() private {
        // 重置 EXPECTED_CHAIN_ID 为空字符串，让 run() 走 vm.envOr 的 Sepolia 分支。
        vm.setEnv("EXPECTED_CHAIN_ID", "");
        vm.setEnv("DEPLOYER_PRIVATE_KEY", vm.toString(DEPLOYER_PK));
        address deployer = vm.addr(DEPLOYER_PK);
        _setEnv(deployer, MINTER, BURNER, "");

        // 把 block.chainid 改成 Sepolia，与 vm.envOr 默认分支吻合。
        vm.chainId(SEPOLIA_CHAIN_ID);

        CertificateNFT nft = dcn.run();
        assertTrue(
            nft.hasRole(nft.DEFAULT_ADMIN_ROLE(), deployer), "admin role when default Sepolia"
        );

        // 还原给后续 _run_AcceptsMatchingChainId。
        vm.chainId(DEFAULT_TEST_CHAIN_ID);
    }

    /// @dev EXPECTED_CHAIN_ID = Sepolia 但 block.chainid = 31337，require 必 fail。
    function _run_RejectsChainIdMismatch() private {
        vm.setEnv("EXPECTED_CHAIN_ID", vm.toString(SEPOLIA_CHAIN_ID));
        vm.setEnv("DEPLOYER_PRIVATE_KEY", vm.toString(DEPLOYER_PK));
        _setEnv(ADMIN, MINTER, BURNER, "");

        vm.expectRevert(); // 信息含动态 chainid，不绑死
        dcn.run();
    }

    // ----- Mode A：env 地址 -----

    function _envMode_ReadsAllThreeAddresses() private {
        _setEnv(ADMIN, MINTER, BURNER, "");

        DeployCertificateNFT.CertificateConfig memory cfg = dcn.readEnvConfig();
        assertEq(cfg.admin, ADMIN);
        assertEq(cfg.minter, MINTER);
        assertEq(cfg.burner, BURNER);

        (DeployCertificateNFT.CertificateConfig memory resolved, bool useJsonMode) =
            dcn.resolveConfig();
        assertFalse(useJsonMode, "empty CERT_NFT_CONFIG_PATH must select Mode A");
        assertEq(resolved.burner, BURNER);
    }

    function _envMode_DefaultsBurnerToAdmin() private {
        _setEnv(ADMIN, MINTER, address(0), "");

        DeployCertificateNFT.CertificateConfig memory cfg = dcn.readEnvConfig();
        assertEq(cfg.burner, ADMIN, "unset CERT_NFT_BURNER_ADDRESS should fall back to admin");
    }

    function _envMode_RejectsUnsetAdmin() private {
        _setEnv(address(0), MINTER, BURNER, "");

        vm.expectRevert(bytes("DeployCertificateNFT: CERT_NFT_ADMIN_ADDRESS unset/zero"));
        dcn.readEnvConfig();
    }

    function _envMode_RejectsUnsetMinter() private {
        _setEnv(ADMIN, address(0), BURNER, "");

        vm.expectRevert(bytes("DeployCertificateNFT: CERT_NFT_MINTER_ADDRESS unset/zero"));
        dcn.readEnvConfig();
    }

    // ----- Mode B：JSON 文件 -----

    function _jsonMode_TakesPriorityOverEnv() private {
        // env 里放一组「错误」地址，JSON 放正确的：验证 Mode B 覆盖 Mode A。
        address jsonAdmin = address(0xDA0);
        address jsonMinter = address(0xDA1);
        address jsonBurner = address(0xDA2);
        vm.writeFile(FIXTURE_PATH_ENV_MODE, _configJson(jsonAdmin, jsonMinter, jsonBurner));
        _setEnv(ADMIN, MINTER, BURNER, FIXTURE_PATH_ENV_MODE);

        (DeployCertificateNFT.CertificateConfig memory cfg, bool useJsonMode) = dcn.resolveConfig();
        assertTrue(useJsonMode, "CERT_NFT_CONFIG_PATH set must select Mode B");
        assertEq(cfg.admin, jsonAdmin);
        assertEq(cfg.minter, jsonMinter);
        assertEq(cfg.burner, jsonBurner);
    }

    function _jsonMode_RejectsMissingFile() private {
        _setEnv(ADMIN, MINTER, BURNER, "./test/fixtures/does-not-exist.json");

        vm.expectRevert(bytes("DeployCertificateNFT: config file not found"));
        dcn.resolveConfig();
    }

    // ----- run()：端到端部署 -----

    function _run_DeploysAndGrantsBurnerWhenDeployerIsAdmin() private {
        address deployer = vm.addr(DEPLOYER_PK);
        vm.setEnv("DEPLOYER_PRIVATE_KEY", vm.toString(DEPLOYER_PK));
        // admin == deployer：脚本有 DEFAULT_ADMIN_ROLE，可以直接补 BURNER_ROLE。
        _setEnv(deployer, MINTER, BURNER, "");

        CertificateNFT nft = dcn.run();

        assertTrue(nft.hasRole(nft.DEFAULT_ADMIN_ROLE(), deployer), "admin role");
        assertTrue(nft.hasRole(nft.MINTER_ROLE(), MINTER), "minter role");
        assertTrue(nft.hasRole(nft.BURNER_ROLE(), BURNER), "burner role granted by script");
    }

    function _run_SkipsBurnerGrantWhenAdminIsExternal() private {
        vm.setEnv("DEPLOYER_PRIVATE_KEY", vm.toString(DEPLOYER_PK));
        // admin 是多签（!= deployer）：脚本无权 grant，BURNER_ROLE 必须留空。
        _setEnv(ADMIN, MINTER, BURNER, "");

        CertificateNFT nft = dcn.run();

        assertTrue(nft.hasRole(nft.DEFAULT_ADMIN_ROLE(), ADMIN), "admin role");
        assertTrue(nft.hasRole(nft.MINTER_ROLE(), MINTER), "minter role");
        assertFalse(nft.hasRole(nft.BURNER_ROLE(), BURNER), "script must not grant without admin");
    }

    // --------------------------------------------------------------------
    //  helpers
    // --------------------------------------------------------------------

    /// @dev 地址传 address(0) 表示「env 未设置」——脚本按零地址处理，语义等价。
    function _setEnv(address admin, address minter, address burner, string memory configPath)
        private
    {
        vm.setEnv("CERT_NFT_ADMIN_ADDRESS", admin == address(0) ? "" : vm.toString(admin));
        vm.setEnv("CERT_NFT_MINTER_ADDRESS", minter == address(0) ? "" : vm.toString(minter));
        vm.setEnv("CERT_NFT_BURNER_ADDRESS", burner == address(0) ? "" : vm.toString(burner));
        vm.setEnv("CERT_NFT_CONFIG_PATH", configPath);
    }

    /// @dev setEnv 是 forge 进程级副作用，跑完清掉避免污染其它 suite —— 尤其是
    ///      DEPLOYER_PRIVATE_KEY，不能把 anvil 测试私钥留在进程环境里。
    function _clearEnv() private {
        _setEnv(address(0), address(0), address(0), "");
        vm.setEnv("DEPLOYER_PRIVATE_KEY", "");
    }

    function _configJson(address admin, address minter, address burner)
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
            "\"burner\":\"",
            vm.toString(burner),
            "\"",
            "}"
        );
    }
}
