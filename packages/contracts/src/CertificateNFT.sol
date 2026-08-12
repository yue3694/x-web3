// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {ERC721} from "@openzeppelin/contracts/token/ERC721/ERC721.sol";
import {AccessControl} from "@openzeppelin/contracts/access/AccessControl.sol";
import {ICertificateNFT} from "./interfaces/ICertificateNFT.sol";

/// @title CertificateNFT
/// @notice 课程结业证书 NFT（Soulbound）。一旦签发即锁在学员地址上不可
///         转让，仅平台（MINTER_ROLE / BURNER_ROLE）可签发或吊销。
///
/// 设计要点：
///   - 业务侧 `certificateId` 由后端颁发，签发时校验唯一性（`AlreadyMinted`）；
///     与链上自增 tokenId 解耦，方便后端按业务键查询。
///   - 链上 tokenId 直接取 `certificateId`（uint256），简化索引与事件过滤。
///   - URI 通过自有 `_tokenURIs` 持久化（避免 ERC721URIStorage 与 AccessControl
///     diamond 继承问题）；`tokenURI()` 返回签发时的元数据链接；burn 时清空。
///   - 全部 transfer / transferFrom / safeTransferFrom 都会 revert `Soulbound`；
///     只有 `mintCertificate`（from=0）与 `revokeCertificate`（burn）会成功。
///   - revoke 后 `certificateId` 不可再被 mint（`_minted` 标记不撤销）；URI
///     同步清理，避免吊销后仍返回元数据。
contract CertificateNFT is ICertificateNFT, ERC721, AccessControl {
    /// @notice 可调用 `mintCertificate` 签发证书的 worker 签名账户。
    bytes32 public constant MINTER_ROLE = keccak256("MINTER_ROLE");

    /// @notice 可调用 `revokeCertificate` 吊销证书的运营/合规账户。
    bytes32 public constant BURNER_ROLE = keccak256("BURNER_ROLE");

    /// @dev certificateId → 是否已被签发；吊销不重置。
    mapping(uint256 => bool) private _minted;

    /// @dev certificateId → 元数据 URI。burn 后清空。
    mapping(uint256 => string) private _tokenURIs;

    /// @custom:storage-location erc7201:x-web3.certificate-nft
    error Soulbound();
    error AlreadyMinted();
    error NotMinted();
    error AlreadyRevoked();
    error ZeroAddress();

    /// @param initialAdmin  DEFAULT_ADMIN_ROLE 持有者（多签）。
    /// @param initialMinter MINTER_ROLE 持有者（worker signer）。
    constructor(address initialAdmin, address initialMinter) ERC721("YD Certificate", "YDC") {
        if (initialAdmin == address(0)) revert ZeroAddress();
        if (initialMinter == address(0)) revert ZeroAddress();

        _grantRole(DEFAULT_ADMIN_ROLE, initialAdmin);
        _grantRole(MINTER_ROLE, initialMinter);
    }

    // --------------------------------------------------------------------
    //  核心：签发 / 吊销
    // --------------------------------------------------------------------

    /// @inheritdoc ICertificateNFT
    function mintCertificate(address to, uint256 certificateId, string calldata uri)
        external
        onlyRole(MINTER_ROLE)
    {
        if (to == address(0)) revert ZeroAddress();
        if (_minted[certificateId]) revert AlreadyMinted();

        _minted[certificateId] = true;
        _tokenURIs[certificateId] = uri;
        _mint(to, certificateId);
        emit CertificateMinted(to, certificateId, uri);
    }

    /// @inheritdoc ICertificateNFT
    function revokeCertificate(uint256 certificateId) external onlyRole(BURNER_ROLE) {
        // 区分三种状态：从未签发（NotMinted）、已吊销（AlreadyRevoked）、活跃可吊销。
        if (!_minted[certificateId]) revert NotMinted();
        // _minted 标记吊销后保留，所以这里用「URI 是否被清掉」判断是否已吊销。
        if (bytes(_tokenURIs[certificateId]).length == 0) revert AlreadyRevoked();

        address from = ownerOf(certificateId);
        // burn 之前先清 URI，避免吊销后元数据仍可读取。
        delete _tokenURIs[certificateId];
        _burn(certificateId);
        emit CertificateRevoked(certificateId, from, msg.sender);
    }

    // --------------------------------------------------------------------
    //  视图
    // --------------------------------------------------------------------

    /// @inheritdoc ICertificateNFT
    function isMinted(uint256 certificateId) external view returns (bool) {
        return _minted[certificateId];
    }

    /// @dev 自实现 URI 持久化：直接返回签发时写入的 URI。
    function tokenURI(uint256 certificateId) public view override(ERC721) returns (string memory) {
        _requireOwned(certificateId);
        return _tokenURIs[certificateId];
    }

    /// @dev ERC-165 接口支持：合并 ERC721 与 AccessControl。
    function supportsInterface(bytes4 interfaceId)
        public
        view
        override(ERC721, AccessControl)
        returns (bool)
    {
        return super.supportsInterface(interfaceId);
    }

    /// @dev 强制 Soulbound：禁止所有非铸造路径的转账。
    ///      ERC721 的 _update 在 mint（from=0）、burn（to=0）、transfer 三
    ///      类场景都会被调用；这里只放行 from=0（铸造），其余一律 revert。
    function _update(address to, uint256 tokenId, address auth)
        internal
        override(ERC721)
        returns (address)
    {
        address from = _ownerOf(tokenId);
        if (from != address(0) && to != address(0)) revert Soulbound();
        return super._update(to, tokenId, auth);
    }
}
