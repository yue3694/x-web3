// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {CertificateNFT, ICertificateNFT} from "../src/CertificateNFT.sol";
import {IAccessControl} from "@openzeppelin/contracts/access/IAccessControl.sol";

/// @title CertificateNFTTest
/// @notice 覆盖 CertificateNFT 的核心路径：签发 / Soulbound / 吊销 / 角色。
contract CertificateNFTTest is Test {
    CertificateNFT internal nft;
    address internal admin = makeAddr("admin");
    address internal minter = makeAddr("minter");
    address internal burner = makeAddr("burner");
    address internal alice = makeAddr("alice");
    address internal bob = makeAddr("bob");

    uint256 internal constant CERT_A = 1001;
    uint256 internal constant CERT_B = 1002;

    function setUp() public {
        nft = new CertificateNFT(admin, minter);
        // 给 burner 角色，用于 revokeCertificate 测试
        bytes32 burnerRole = nft.BURNER_ROLE();
        vm.startPrank(admin);
        nft.grantRole(burnerRole, burner);
        vm.stopPrank();
    }

    // --------------------------------------------------------------------
    //  构造函数 / 角色
    // --------------------------------------------------------------------

    function test_Constructor_GrantsRoles() public view {
        assertTrue(nft.hasRole(nft.DEFAULT_ADMIN_ROLE(), admin));
        assertTrue(nft.hasRole(nft.MINTER_ROLE(), minter));
    }

    function test_Constructor_RejectsZeroAddresses() public {
        vm.expectRevert(CertificateNFT.ZeroAddress.selector);
        new CertificateNFT(address(0), minter);

        vm.expectRevert(CertificateNFT.ZeroAddress.selector);
        new CertificateNFT(admin, address(0));
    }

    // --------------------------------------------------------------------
    //  mintCertificate
    // --------------------------------------------------------------------

    function test_Mint_HappyPath() public {
        string memory uri = "ipfs://QmXxx/cert-1001.json";

        vm.prank(minter);
        vm.expectEmit(true, true, false, true);
        emit ICertificateNFT.CertificateMinted(alice, CERT_A, uri);
        nft.mintCertificate(alice, CERT_A, uri);

        assertEq(nft.ownerOf(CERT_A), alice);
        assertTrue(nft.isMinted(CERT_A));
        assertEq(nft.tokenURI(CERT_A), uri);
    }

    /// @notice tokenURI 在签发后可通过 ERC721Metadata 检索（前端 / indexer 必用）。
    function test_TokenURI_StoredAndRetrievable() public {
        string memory uri = "ipfs://bafy.../cert.json";
        vm.prank(minter);
        nft.mintCertificate(alice, CERT_A, uri);

        assertEq(nft.tokenURI(CERT_A), uri);
    }

    /// @notice 吊销后 tokenURI 必须清空，避免 metadata 仍可读取。
    function test_TokenURI_ClearedAfterRevoke() public {
        string memory uri = "ipfs://bafy.../cert.json";
        vm.prank(minter);
        nft.mintCertificate(alice, CERT_A, uri);

        vm.prank(burner);
        nft.revokeCertificate(CERT_A);

        // ownerOf 已经 revert；tokenURI 同样会 revert (ERC721 内部 _requireOwned)。
        vm.expectRevert();
        nft.tokenURI(CERT_A);
    }

    /// @notice 未签发的 certificateId 调 tokenURI 必须 revert。
    function test_TokenURI_RevertsForUnminted() public {
        vm.expectRevert();
        nft.tokenURI(CERT_A);
    }

    function test_Mint_RevertsForNonMinter() public {
        bytes32 minterRole = nft.MINTER_ROLE();
        vm.startPrank(alice);
        vm.expectRevert(
            abi.encodeWithSelector(
                IAccessControl.AccessControlUnauthorizedAccount.selector, alice, minterRole
            )
        );
        nft.mintCertificate(alice, CERT_A, "ipfs://x");
        vm.stopPrank();
    }

    function test_Mint_RevertsOnZeroAddress() public {
        vm.prank(minter);
        vm.expectRevert(CertificateNFT.ZeroAddress.selector);
        nft.mintCertificate(address(0), CERT_A, "ipfs://x");
    }

    function test_Mint_RevertsOnDuplicateId() public {
        vm.startPrank(minter);
        nft.mintCertificate(alice, CERT_A, "ipfs://a");
        vm.expectRevert(CertificateNFT.AlreadyMinted.selector);
        nft.mintCertificate(bob, CERT_A, "ipfs://b");
        vm.stopPrank();
    }

    /// @notice 模糊：任意 certificateId 都不会重复铸造。
    function test_Fuzz_MintIdUnique(uint256 id) public {
        vm.assume(id != 0); // tokenId 0 在 OZ ERC721 中按惯例从 1 起
        vm.prank(minter);
        nft.mintCertificate(alice, id, "ipfs://x");
        assertTrue(nft.isMinted(id));
        assertEq(nft.ownerOf(id), alice);

        vm.prank(minter);
        vm.expectRevert(CertificateNFT.AlreadyMinted.selector);
        nft.mintCertificate(bob, id, "ipfs://y");
    }

    // --------------------------------------------------------------------
    //  Soulbound
    // --------------------------------------------------------------------

    function test_TransferFrom_RevertsSoulbound() public {
        vm.prank(minter);
        nft.mintCertificate(alice, CERT_A, "ipfs://a");

        vm.prank(alice);
        vm.expectRevert(CertificateNFT.Soulbound.selector);
        nft.transferFrom(alice, bob, CERT_A);
    }

    function test_SafeTransferFrom_RevertsSoulbound() public {
        vm.prank(minter);
        nft.mintCertificate(alice, CERT_A, "ipfs://a");

        vm.prank(alice);
        vm.expectRevert(CertificateNFT.Soulbound.selector);
        nft.safeTransferFrom(alice, bob, CERT_A, "");
    }

    function test_Approve_DoesNotEnableTransfer() public {
        vm.prank(minter);
        nft.mintCertificate(alice, CERT_A, "ipfs://a");

        vm.prank(alice);
        nft.approve(bob, CERT_A);

        // approve 自身不 revert，但真正 transferFrom 仍因 soulbound 而 revert。
        vm.prank(bob);
        vm.expectRevert(CertificateNFT.Soulbound.selector);
        nft.transferFrom(alice, bob, CERT_A);
    }

    // --------------------------------------------------------------------
    //  revokeCertificate
    // --------------------------------------------------------------------

    function test_Revoke_HappyPath() public {
        vm.prank(minter);
        nft.mintCertificate(alice, CERT_A, "ipfs://a");

        vm.prank(burner);
        vm.expectEmit(true, true, true, false);
        emit ICertificateNFT.CertificateRevoked(CERT_A, alice, burner);
        nft.revokeCertificate(CERT_A);

        // _minted 标记保持，certificateId 永不复用。
        assertTrue(nft.isMinted(CERT_A));
        // ownerOf 应当 revert（token 已被 burn）
        vm.expectRevert();
        nft.ownerOf(CERT_A);
    }

    function test_Revoke_RevertsForNonBurner() public {
        vm.prank(minter);
        nft.mintCertificate(alice, CERT_A, "ipfs://a");

        bytes32 burnerRole = nft.BURNER_ROLE();
        vm.startPrank(alice);
        vm.expectRevert(
            abi.encodeWithSelector(
                IAccessControl.AccessControlUnauthorizedAccount.selector, alice, burnerRole
            )
        );
        nft.revokeCertificate(CERT_A);
        vm.stopPrank();
    }

    function test_Revoke_RevertsOnUnminted() public {
        vm.prank(burner);
        vm.expectRevert(CertificateNFT.NotMinted.selector);
        nft.revokeCertificate(CERT_A);
    }

    /// @notice 重复吊销同一 ID 必须用专属错误 `AlreadyRevoked`，便于运维区分。
    function test_Revoke_RevertsOnAlreadyRevoked() public {
        vm.prank(minter);
        nft.mintCertificate(alice, CERT_A, "ipfs://a");

        vm.prank(burner);
        nft.revokeCertificate(CERT_A);

        vm.prank(burner);
        vm.expectRevert(CertificateNFT.AlreadyRevoked.selector);
        nft.revokeCertificate(CERT_A);
    }

    function test_Revoke_IdCannotBeReused() public {
        vm.startPrank(minter);
        nft.mintCertificate(alice, CERT_A, "ipfs://a");
        vm.stopPrank();

        vm.prank(burner);
        nft.revokeCertificate(CERT_A);

        // 重新铸造同一 ID 应被 _minted 拦截
        vm.prank(minter);
        vm.expectRevert(CertificateNFT.AlreadyMinted.selector);
        nft.mintCertificate(bob, CERT_A, "ipfs://b");
    }

    // --------------------------------------------------------------------
    //  invariant：所有 token 余额之和 = 签发数 - 吊销数
    // --------------------------------------------------------------------

    function test_Invariant_OwnershipCountMatchesMintedMinusBurned() public {
        // 签发 3 张
        vm.startPrank(minter);
        nft.mintCertificate(alice, 1, "ipfs://1");
        nft.mintCertificate(alice, 2, "ipfs://2");
        nft.mintCertificate(bob, 3, "ipfs://3");
        vm.stopPrank();

        // 吊销 1 张
        vm.prank(burner);
        nft.revokeCertificate(2);

        // 已知 ID 范围 (1..3) 中活着的 token 总数 = 3 - 1 = 2
        uint256 alive = _aliveCount(1, 3);
        assertEq(alive, 2);
    }

    /// @dev 用 try/catch 探测 token 是否存在（ownerOf 会对不存在的 token revert）。
    function _aliveCount(uint256 fromId, uint256 toId) internal view returns (uint256 n) {
        for (uint256 i = fromId; i <= toId; i++) {
            try nft.ownerOf(i) returns (address owner) {
                if (owner != address(0)) n++;
            } catch {
                // ownerOf revert 表示 token 不存在，跳过。
            }
        }
    }
}
