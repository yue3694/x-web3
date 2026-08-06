// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

/// @title Counter
/// @notice Minimal example contract used as the smoke-test target for
///         Sepolia deployment & frontend wiring. Replace with real logic.
contract Counter is Ownable {
    uint256 public count;

    event Increment(address indexed by, uint256 newCount);
    event Decrement(address indexed by, uint256 newCount);
    event Reset(address indexed by);

    error Underflow();

    constructor(address initialOwner) Ownable(initialOwner) {}

    function increment() external {
        count += 1;
        emit Increment(msg.sender, count);
    }

    function decrement() external {
        if (count == 0) revert Underflow();
        count -= 1;
        emit Decrement(msg.sender, count);
    }

    function reset() external onlyOwner {
        count = 0;
        emit Reset(msg.sender);
    }
}