// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test, console2} from "forge-std/Test.sol";
import {DeployCourseMarket} from "../script/DeployCourseMarket.s.sol";

/// @title TestDeployCourseMarketScript
/// @notice 针对 DeployCourseMarket.parseCoursesConfig 的纯逻辑单测。
///         验证逻辑通过把 JSON 字符串直接传入 parseCoursesConfig；
///         仅 happy path 走一次 vm.readFile 来验证 IO 端到端。
contract TestDeployCourseMarketScript is Test {
    DeployCourseMarket internal dcm;

    /// forge-lint: disable-next-line(mixed-case-variable)
    string internal constant FIXTURE_PATH = "./test/fixtures/courses.json";

    function setUp() public {
        dcm = new DeployCourseMarket();

        // 写一份 happy-path fixture 到 ./test/fixtures/courses.json，
        // 仅用于 test_ParseCoursesConfig_FromFile。
        string memory json = string.concat(
            "[",
            "{",
            "\"courseKey\":\"0xabc0000000000000000000000000000000000000000000000000000000000001\",",
            "\"token\":\"0x1111111111111111111111111111111111111111\",",
            "\"amount\":\"1000000000000000000\",",
            "\"priceVersion\":1",
            "},",
            "{",
            "\"courseKey\":\"0xabc0000000000000000000000000000000000000000000000000000000000002\",",
            "\"token\":\"0x2222222222222222222222222222222222222222\",",
            "\"amount\":\"5000000000000000000\",",
            "\"priceVersion\":2",
            "}",
            "]"
        );
        vm.writeFile(FIXTURE_PATH, json);
    }

    function test_ParseCoursesConfig_FromFile() public view {
        string memory json = vm.readFile(FIXTURE_PATH);
        DeployCourseMarket.CourseInput[] memory courses = dcm.parseCoursesConfig(json);

        assertEq(courses.length, 2);

        assertEq(
            uint256(courses[0].courseKey),
            0xabc0000000000000000000000000000000000000000000000000000000000001
        );
        assertEq(courses[0].token, address(0x1111111111111111111111111111111111111111));
        assertEq(courses[0].amount, 1e18);
        assertEq(courses[0].priceVersion, 1);

        assertEq(courses[1].token, address(0x2222222222222222222222222222222222222222));
        assertEq(courses[1].amount, 5e18);
        assertEq(courses[1].priceVersion, 2);
    }

    function test_ParseCoursesConfig_RejectsEmptyJson() public {
        vm.expectRevert(bytes("DeployCourseMarket: empty JSON"));
        dcm.parseCoursesConfig("");
    }

    function test_ParseCoursesConfig_RejectsZeroCourseKey() public {
        string memory json = string.concat(
            "[",
            "{",
            "\"courseKey\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",",
            "\"token\":\"0x1111111111111111111111111111111111111111\",",
            "\"amount\":\"1000\",",
            "\"priceVersion\":1",
            "}",
            "]"
        );
        vm.expectRevert(bytes("DeployCourseMarket: zero courseKey"));
        dcm.parseCoursesConfig(json);
    }

    function test_ParseCoursesConfig_RejectsZeroToken() public {
        string memory json = string.concat(
            "[",
            "{",
            "\"courseKey\":\"0xabc0000000000000000000000000000000000000000000000000000000000099\",",
            "\"token\":\"0x0000000000000000000000000000000000000000\",",
            "\"amount\":\"1000\",",
            "\"priceVersion\":1",
            "}",
            "]"
        );
        vm.expectRevert(bytes("DeployCourseMarket: zero token"));
        dcm.parseCoursesConfig(json);
    }

    function test_ParseCoursesConfig_RejectsZeroAmount() public {
        string memory json = string.concat(
            "[",
            "{",
            "\"courseKey\":\"0xabc0000000000000000000000000000000000000000000000000000000000099\",",
            "\"token\":\"0x1111111111111111111111111111111111111111\",",
            "\"amount\":\"0\",",
            "\"priceVersion\":1",
            "}",
            "]"
        );
        vm.expectRevert(bytes("DeployCourseMarket: zero amount"));
        dcm.parseCoursesConfig(json);
    }

    function test_ParseCoursesConfig_RejectsZeroPriceVersion() public {
        string memory json = string.concat(
            "[",
            "{",
            "\"courseKey\":\"0xabc0000000000000000000000000000000000000000000000000000000000099\",",
            "\"token\":\"0x1111111111111111111111111111111111111111\",",
            "\"amount\":\"1000\",",
            "\"priceVersion\":0",
            "}",
            "]"
        );
        vm.expectRevert(bytes("DeployCourseMarket: zero priceVersion"));
        dcm.parseCoursesConfig(json);
    }

    function test_ParseCoursesConfig_SingleEntry_InMemory() public view {
        string memory json = string.concat(
            "[",
            "{",
            "\"courseKey\":\"0xabc0000000000000000000000000000000000000000000000000000000000042\",",
            "\"token\":\"0x3333333333333333333333333333333333333333\",",
            "\"amount\":\"42\",",
            "\"priceVersion\":7",
            "}",
            "]"
        );
        DeployCourseMarket.CourseInput[] memory courses = dcm.parseCoursesConfig(json);
        assertEq(courses.length, 1);
        assertEq(courses[0].priceVersion, 7);
        assertEq(courses[0].amount, 42);
    }
}
