// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice 业务层只依赖这个稳定接口，不直接依赖某个预言机供应商。
interface IPriceOracle {
    /// @return price 正数价格
    /// @return decimals price 的小数位
    /// @return updatedAt feed 最后更新时间
    function latestPrice() external view returns (uint256 price, uint8 decimals, uint256 updatedAt);
}

/// @notice Chainlink AggregatorV3 的最小兼容接口。
interface IAggregatorV3 {
    function decimals() external view returns (uint8);
    function description() external view returns (string memory);
    function latestRoundData()
        external
        view
        returns (
            uint80 roundId,
            int256 answer,
            uint256 startedAt,
            uint256 updatedAt,
            uint80 answeredInRound
        );
}
