// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

/// @notice Sepolia-only test dispenser: SepoliaETH -> YD.
/// Both assets are testnet assets and have no real-world monetary value.
contract SepoliaYDSale is Ownable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    IERC20 public immutable yd;
    uint256 public immutable ydPerEth;

    event Purchased(
        address indexed buyer, address indexed recipient, uint256 sepoliaEthIn, uint256 ydOut
    );

    error WrongChain();
    error ZeroAddress();
    error ZeroAmount();
    error InsufficientInventory();

    constructor(address initialOwner, IERC20 token, uint256 rate) Ownable(initialOwner) {
        if (block.chainid != 11_155_111) revert WrongChain();
        if (initialOwner == address(0) || address(token) == address(0)) revert ZeroAddress();
        if (rate == 0) revert ZeroAmount();
        yd = token;
        ydPerEth = rate;
    }

    function quote(uint256 sepoliaEthAmount) public view returns (uint256) {
        return sepoliaEthAmount * ydPerEth / 1 ether;
    }

    function buy(address recipient) external payable nonReentrant returns (uint256 ydOut) {
        if (recipient == address(0)) revert ZeroAddress();
        if (msg.value == 0) revert ZeroAmount();
        ydOut = quote(msg.value);
        if (yd.balanceOf(address(this)) < ydOut) revert InsufficientInventory();
        yd.safeTransfer(recipient, ydOut);
        emit Purchased(msg.sender, recipient, msg.value, ydOut);
    }

    function withdrawETH(address payable recipient) external onlyOwner {
        if (recipient == address(0)) revert ZeroAddress();
        (bool ok,) = recipient.call{value: address(this).balance}("");
        require(ok, "ETH transfer failed");
    }
}
