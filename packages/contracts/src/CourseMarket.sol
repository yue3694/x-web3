// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {Pausable} from "@openzeppelin/contracts/utils/Pausable.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

/// @title ICourseMarket
/// @notice CourseMarket 的对外接口；前端 / worker 用同样的 ABI。
interface ICourseMarket {
    function buyCourse(bytes32 courseKey, uint256 expectedAmount, bytes16 intentId) external;
    function configureCourse(bytes32 courseKey, address token, uint256 amount, uint256 priceVersion)
        external;

    event CourseConfigured(
        bytes32 indexed courseKey, address token, uint256 amount, uint256 priceVersion
    );
    event CoursePurchased(
        bytes32 indexed courseKey,
        address indexed buyer,
        address token,
        uint256 amount,
        bytes16 intentId,
        uint256 priceVersion
    );
}

/// @title CourseMarket
/// @notice YD Token（或任意 ERC20）结算的课程市场合约。
///
/// 设计要点：
///   - configureCourse：仅 owner；同一 (courseKey) 二次配置 → 价格/代币变化
///     即产生新的 price_version；老 price_version 保留可继续被 buyCourse 消费
///     直到其 intent 过期（worker 不依赖 valid_to）。
///   - buyCourse：
///       * CEI：先记录 (buyer, courseKey) → 再 transferFrom → 再 emit；
///       * 防重购：同一 (buyer, courseKey) 只能购买一次；
///       * expectedAmount 防 price tampering；
///       * intentId = bytes16（UUID 高 128 位），由 API 颁发。
///   - 紧急暂停：Pausable 阻止买入，不影响 configure / 查询。
contract CourseMarket is ICourseMarket, Ownable, Pausable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    /// @dev course 配置：当前生效的 (token, amount, priceVersion)。
    struct CourseConfig {
        address token;
        uint256 amount;
        uint256 priceVersion;
        bool configured;
    }

    mapping(bytes32 => CourseConfig) private _configs;
    /// @dev 防重购：(buyer, courseKey) → purchased
    mapping(address => mapping(bytes32 => bool)) private _purchased;

    /// @custom:storage-location erc7201:x-web3.course-market
    error NotConfigured();
    error AlreadyPurchased();
    error AmountMismatch();
    error ZeroAddress();
    error ZeroAmount();

    constructor(address initialOwner) Ownable(initialOwner) {}

    /// @inheritdoc ICourseMarket
    function configureCourse(bytes32 courseKey, address token, uint256 amount, uint256 priceVersion)
        external
        onlyOwner
    {
        if (token == address(0)) revert ZeroAddress();
        if (amount == 0) revert ZeroAmount();
        _configs[courseKey] = CourseConfig({
            token: token, amount: amount, priceVersion: priceVersion, configured: true
        });
        emit CourseConfigured(courseKey, token, amount, priceVersion);
    }

    /// @inheritdoc ICourseMarket
    function buyCourse(bytes32 courseKey, uint256 expectedAmount, bytes16 intentId)
        external
        whenNotPaused
        nonReentrant
    {
        CourseConfig memory cfg = _configs[courseKey];
        if (!cfg.configured) revert NotConfigured();
        if (expectedAmount != cfg.amount) revert AmountMismatch();
        if (_purchased[msg.sender][courseKey]) revert AlreadyPurchased();

        // checks
        // effects
        _purchased[msg.sender][courseKey] = true;
        // interactions
        IERC20(cfg.token).safeTransferFrom(msg.sender, owner(), cfg.amount);

        emit CoursePurchased(
            courseKey, msg.sender, cfg.token, cfg.amount, intentId, cfg.priceVersion
        );
    }

    /// @notice 查询当前配置。
    function getConfig(bytes32 courseKey)
        external
        view
        returns (address token, uint256 amount, uint256 priceVersion)
    {
        CourseConfig memory cfg = _configs[courseKey];
        return (cfg.token, cfg.amount, cfg.priceVersion);
    }

    /// @notice 查询是否已购。
    function hasPurchased(address buyer, bytes32 courseKey) external view returns (bool) {
        return _purchased[buyer][courseKey];
    }

    /// @notice 紧急暂停买入。
    function pause() external onlyOwner {
        _pause();
    }

    function unpause() external onlyOwner {
        _unpause();
    }
}
