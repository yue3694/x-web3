// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title ICertificateNFT
/// @notice 课程结业证书 NFT（Soulbound）的对外接口。证书一旦发放给学员，
///         就被锁在学员地址上不可转让；只有平台（MINTER_ROLE / BURNER_ROLE）
///         可签发或吊销。
/// @dev    实现位于 `src/CertificateNFT.sol`。自定义错误（`Soulbound` /
///         `AlreadyMinted` / `NotMinted` / `AlreadyRevoked` / `ZeroAddress`）
///         在实现合约声明，并通过继承自动暴露给接口调用方。
interface ICertificateNFT {
    // --------------------------------------------------------------------
    //  角色
    // --------------------------------------------------------------------

    /// @notice 可调用 `mintCertificate` 签发证书的 worker 签名账户。
    function MINTER_ROLE() external view returns (bytes32);

    /// @notice 可调用 `revokeCertificate` 吊销证书的运营/合规账户。
    function BURNER_ROLE() external view returns (bytes32);

    // --------------------------------------------------------------------
    //  状态变更
    // --------------------------------------------------------------------

    /// @notice 签发证书：仅 MINTER_ROLE；`certificateId` 必须全网唯一。
    /// @param to            证书接收者（学员）。
    /// @param certificateId 业务侧唯一 ID（可由后端 UUID 截断 / 哈希得到）。
    /// @param uri           元数据 URI（IPFS / Arweave）。
    function mintCertificate(address to, uint256 certificateId, string calldata uri) external;

    /// @notice 吊销证书：仅 BURNER_ROLE；token 会被销毁，`certificateId` 不可复用。
    /// @param certificateId 要吊销的证书 ID。
    function revokeCertificate(uint256 certificateId) external;

    // --------------------------------------------------------------------
    //  视图
    // --------------------------------------------------------------------

    /// @notice 证书 ID 是否已被签发（用于业务侧防重）。
    function isMinted(uint256 certificateId) external view returns (bool);

    // --------------------------------------------------------------------
    //  事件
    // --------------------------------------------------------------------

    /// @notice 证书被签发时触发。
    event CertificateMinted(address indexed to, uint256 indexed certificateId, string uri);

    /// @notice 证书被吊销时触发。
    event CertificateRevoked(
        uint256 indexed certificateId, address indexed from, address indexed by
    );
}
