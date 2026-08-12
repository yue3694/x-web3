// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {IAggregatorV3, IPriceOracle} from "./interfaces/IPriceOracle.sol";

/// @notice Chainlink-compatible feed adapter with mandatory freshness checks.
/// @dev This adapter is reference-only in the MVP; CourseMarket accounting remains
///      a frozen ERC-20 amount and does not call the oracle.
contract ChainlinkPriceOracle is IPriceOracle {
    IAggregatorV3 public immutable feed;
    uint256 public immutable maxAge;

    error ZeroAddress();
    error InvalidMaxAge();
    error InvalidAnswer();
    error StalePrice();
    error IncompleteRound();

    constructor(address feedAddress, uint256 maxAgeSeconds) {
        if (feedAddress == address(0)) revert ZeroAddress();
        if (maxAgeSeconds == 0) revert InvalidMaxAge();
        feed = IAggregatorV3(feedAddress);
        maxAge = maxAgeSeconds;
    }

    function latestPrice()
        external
        view
        returns (uint256 price, uint8 decimals_, uint256 updatedAt)
    {
        (uint80 roundId, int256 answer,, uint256 feedUpdatedAt, uint80 answeredInRound) =
            feed.latestRoundData();
        if (answer <= 0) revert InvalidAnswer();
        if (answeredInRound < roundId) revert IncompleteRound();
        if (
            feedUpdatedAt == 0 || feedUpdatedAt > block.timestamp
                || block.timestamp - feedUpdatedAt > maxAge
        ) revert StalePrice();

        return (uint256(answer), feed.decimals(), feedUpdatedAt);
    }

    function description() external view returns (string memory) {
        return feed.description();
    }
}
