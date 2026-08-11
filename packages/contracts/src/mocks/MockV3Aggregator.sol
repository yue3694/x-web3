// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice Anvil/test-only Chainlink-compatible feed. Never deploy as a trusted production feed.
contract MockV3Aggregator {
    uint8 public immutable decimals;
    string public description;

    uint80 private _roundId;
    int256 private _answer;
    uint256 private _updatedAt;
    uint80 private _answeredInRound;

    constructor(uint8 decimals_, int256 initialAnswer, string memory description_) {
        decimals = decimals_;
        description = description_;
        setRoundData(1, initialAnswer, block.timestamp, 1);
    }

    function setRoundData(uint80 roundId, int256 answer, uint256 updatedAt, uint80 answeredInRound)
        public
    {
        _roundId = roundId;
        _answer = answer;
        _updatedAt = updatedAt;
        _answeredInRound = answeredInRound;
    }

    function latestRoundData() external view returns (uint80, int256, uint256, uint256, uint80) {
        return (_roundId, _answer, _updatedAt, _updatedAt, _answeredInRound);
    }
}
