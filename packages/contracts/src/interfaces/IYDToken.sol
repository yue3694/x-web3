// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title IYDToken
/// @notice YD Token (ERC-20 + Permit + AccessControl + Pausable) 的对外接口。
/// @dev 前端 / 后端 / worker 通过此接口调用；事件 / 错误在这里集中声明以便
///      消费者做单一 ABI 导入。实现位于 `src/YDToken.sol`。
interface IYDToken {
    // --------------------------------------------------------------------
    //  角色
    // --------------------------------------------------------------------

    /// @notice 铸币角色：可调用 `mint` 直到 cap 达到上限。
    function MINTER_ROLE() external view returns (bytes32);

    /// @notice 暂停角色：可调用 `pause` / `unpause` 紧急冻结转账。
    function PAUSER_ROLE() external view returns (bytes32);

    // --------------------------------------------------------------------
    //  元数据 / 视图
    // --------------------------------------------------------------------

    /// @notice 最大可铸造量（含初始供应），decimals = 18。
    function cap() external view returns (uint256);

    // --------------------------------------------------------------------
    //  状态变更
    // --------------------------------------------------------------------

    /// @notice 由 MINTER_ROLE 调用，向 `to` 增发 `amount` YD；触发 Minted 事件。
    function mint(address to, uint256 amount) external;

    /// @notice 暂停所有转账（transfer / transferFrom / approve 等仍可用）。
    function pause() external;

    /// @notice 解除暂停。
    function unpause() external;

    // --------------------------------------------------------------------
    //  事件
    // --------------------------------------------------------------------

    /// @notice 当 `mint` 成功执行时触发。
    event Minted(address indexed to, uint256 amount, address indexed by);

    /// @notice 为未来 burn 预留事件（ADR-0002 暂不实现 burn，但事件先占位）。
    event Burned(address indexed from, uint256 amount, address indexed by);

    /// @notice 当 cap 被调整时触发（MVP 暂不暴露修改 cap 的方法，仅预留事件）。
    event CapSet(uint256 newCap);
}
