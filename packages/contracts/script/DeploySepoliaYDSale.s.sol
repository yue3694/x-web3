// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Script, console2} from "forge-std/Script.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SepoliaYDSale} from "../src/SepoliaYDSale.sol";

contract DeploySepoliaYDSale is Script {
    function run() external returns (SepoliaYDSale sale) {
        require(block.chainid == 11_155_111, "DeploySepoliaYDSale: Sepolia only");
        uint256 key = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address owner = vm.addr(key);
        IERC20 yd = IERC20(vm.envAddress("YD_TOKEN_ADDRESS"));
        uint256 rate = vm.envOr("SEPOLIA_YD_PER_ETH", uint256(1000 ether));
        uint256 inventory = vm.envOr("SEPOLIA_YD_INVENTORY", uint256(1_000_000 ether));

        vm.startBroadcast(key);
        sale = new SepoliaYDSale(owner, yd, rate);
        yd.transfer(address(sale), inventory);
        vm.stopBroadcast();

        console2.log("Sepolia YD sale:", address(sale));
        console2.log("YD token:", address(yd));
        console2.log("1 SepoliaETH -> YD wei:", rate);
    }
}
