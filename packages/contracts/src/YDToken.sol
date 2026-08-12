// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {ERC20Pausable} from "@openzeppelin/contracts/token/ERC20/extensions/ERC20Pausable.sol";
import {ERC20Permit} from "@openzeppelin/contracts/token/ERC20/extensions/ERC20Permit.sol";
import {AccessControl} from "@openzeppelin/contracts/access/AccessControl.sol";
import {IYDToken} from "./interfaces/IYDToken.sol";

/// @title YDToken
/// @notice YD 平台积分（ERC-20 + Permit + AccessControl + Pausable）。经济模型
///         见 `docs/adr/0002-yd-tokenomics.md`。
///
/// 设计要点：
///   - 固定 cap = 1e9 * 1e18，decimals = 18；铸造不可超 cap。
///   - 初始供应 2e8 * 1e18 在构造时发给 treasury；剩余 8e8 留作后续空投/生态。
///   - 三角色模型：
///       * DEFAULT_ADMIN_ROLE：授予 / 撤销其他角色；
///       * MINTER_ROLE：可铸币直到 cap；
///       * PAUSER_ROLE：紧急暂停 / 恢复转账。
///   - 角色管理靠 AccessControl（v5）；部署脚本会把 admin / minter / pauser
///     转交给 Gnosis Safe 多签，详见 ADR-0002。
///   - 继承 ERC20Pausable：pause 后所有 transfer / transferFrom 都会 revert，
///     approve / permit 不受影响，方便链下做救援性回收。
contract YDToken is IYDToken, ERC20, ERC20Permit, ERC20Pausable, AccessControl {
    // --------------------------------------------------------------------
    //  常量 / 角色
    // --------------------------------------------------------------------

    /// @dev 1e9 * 1e18，硬编码 cap（参见 ADR-0002）。
    uint256 private constant _CAP = 1_000_000_000 * 1e18;

    /// @dev 2e8 * 1e18，构造时一次性铸造给 treasury。
    uint256 private constant _INITIAL_SUPPLY = 200_000_000 * 1e18;

    /// @notice 铸币角色：可调用 `mint` 直到 cap 达到上限。
    bytes32 public constant MINTER_ROLE = keccak256("MINTER_ROLE");

    /// @notice 暂停角色：可调用 `pause` / `unpause` 紧急冻结转账。
    bytes32 public constant PAUSER_ROLE = keccak256("PAUSER_ROLE");

    // --------------------------------------------------------------------
    //  自定义错误
    // --------------------------------------------------------------------

    /// @custom:storage-location erc7201:x-web3.yd-token
    error CapExceeded();
    error ZeroAddress();
    error ZeroAmount();

    // --------------------------------------------------------------------
    //  构造函数
    // --------------------------------------------------------------------

    /// @notice 部署并铸造初始供应。
    /// @param initialAdmin   DEFAULT_ADMIN_ROLE 持有者（通常为多签）。
    /// @param initialMinter  MINTER_ROLE 持有者（通常为多签）。
    /// @param initialPauser  PAUSER_ROLE 持有者（通常为多签）。
    /// @param initialTreasury 初始 2 亿 YD 接收方（通常为多签 treasury）。
    constructor(
        address initialAdmin,
        address initialMinter,
        address initialPauser,
        address initialTreasury
    ) ERC20("YD Token", "YD") ERC20Permit("YD Token") {
        if (initialAdmin == address(0)) revert ZeroAddress();
        if (initialMinter == address(0)) revert ZeroAddress();
        if (initialPauser == address(0)) revert ZeroAddress();
        if (initialTreasury == address(0)) revert ZeroAddress();

        _grantRole(DEFAULT_ADMIN_ROLE, initialAdmin);
        _grantRole(MINTER_ROLE, initialMinter);
        _grantRole(PAUSER_ROLE, initialPauser);

        // 构造时即铸造初始供应给 treasury；
        // 不会触及 cap 上限（_INITIAL_SUPPLY < _CAP），故无需 cap 校验。
        _mint(initialTreasury, _INITIAL_SUPPLY);
    }

    // --------------------------------------------------------------------
    //  视图
    // --------------------------------------------------------------------

    /// @inheritdoc IYDToken
    function cap() external pure returns (uint256) {
        return _CAP;
    }

    // --------------------------------------------------------------------
    //  核心：铸币
    // --------------------------------------------------------------------

    /// @inheritdoc IYDToken
    function mint(address to, uint256 amount) external onlyRole(MINTER_ROLE) {
        if (to == address(0)) revert ZeroAddress();
        if (amount == 0) revert ZeroAmount();
        if (totalSupply() + amount > _CAP) revert CapExceeded();

        _mint(to, amount);
        emit Minted(to, amount, msg.sender);
    }

    // --------------------------------------------------------------------
    //  核心：暂停
    // --------------------------------------------------------------------

    /// @inheritdoc IYDToken
    function pause() external onlyRole(PAUSER_ROLE) {
        _pause();
    }

    /// @inheritdoc IYDToken
    function unpause() external onlyRole(PAUSER_ROLE) {
        _unpause();
    }

    // --------------------------------------------------------------------
    //  OZ v5 hook
    // --------------------------------------------------------------------

    /// @dev ERC20Pausable 要求实现此 hook；mint / burn 内部也走它，
    ///      但我们在 mint 中已显式检查 ZeroAddress，故此处仅 pause 透传。
    function _update(address from, address to, uint256 value)
        internal
        override(ERC20, ERC20Pausable)
    {
        super._update(from, to, value);
    }
}
