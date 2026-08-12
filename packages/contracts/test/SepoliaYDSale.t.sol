// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {YDToken} from "../src/YDToken.sol";
import {SepoliaYDSale} from "../src/SepoliaYDSale.sol";

contract SepoliaYDSaleTest is Test {
    YDToken token;
    SepoliaYDSale sale;
    address buyer = address(0xB0B);

    function setUp() external {
        vm.chainId(11_155_111);
        token = new YDToken(address(this), address(this), address(this), address(this));
        sale = new SepoliaYDSale(address(this), token, 1000 ether);
        token.transfer(address(sale), 1_000_000 ether);
        vm.deal(buyer, 1 ether);
    }

    function testBuySepoliaETHForYD() external {
        vm.prank(buyer);
        sale.buy{value: 0.1 ether}(buyer);
        assertEq(token.balanceOf(buyer), 100 ether);
    }

    function testRejectsNonSepoliaDeployment() external {
        vm.chainId(31_337);
        vm.expectRevert(SepoliaYDSale.WrongChain.selector);
        new SepoliaYDSale(address(this), token, 1000 ether);
    }
}
