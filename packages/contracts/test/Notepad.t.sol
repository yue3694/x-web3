// SPDX-License-Identifier: MIT
// ============================================================================
//  Notepad.t.sol  ——  Notepad.sol 的 forge-std 单元测试 + fuzz
// ----------------------------------------------------------------------------
//  12 个测试用例 + 1 个 fuzz：
//
//   createNote             (5 个)
//     test_CreateNote_AssignsIdOne
//     test_CreateNote_AssignsSequentialIdsPerOwner
//     test_CreateNote_RevertsTitleTooLong
//     test_CreateNote_RevertsBodyTooLong
//     test_CreateNote_RevertsTooManyNotes
//
//   updateNote             (3 个)
//     test_UpdateNote_ChangesFieldsAndBumpsUpdatedAt
//     test_UpdateNote_RevertsNoteNotFound
//     test_UpdateNote_RevertsBodyTooLong
//
//   deleteNote             (3 个)
//     test_DeleteNote_SwapsAndPops_PreservesOtherIds
//     test_DeleteNote_RevertsNoteNotFound
//     test_DeleteNote_EmitsNoteDeleted
//
//   views                  (2 个)
//     test_GetNotes_ReturnsAllInOrder
//     test_GetNoteCount_ReflectsCreatesAndDeletes
//
//   fuzz                   (1 个)
//     testFuzz_CreateAndUpdate
//
//  覆盖率目标：≥ 90% 行覆盖（项目规则 .claude/rules/testing.md）。
// ============================================================================

pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {Notepad} from "../src/Notepad.sol";

contract NotepadTest is Test {
    // -------------------------------------------------------------------------
    //  测试夹具（fixture）
    //  `internal` + `setUp` 模式：每个 test_* 拿到全新合约实例，状态互不
    //  污染。
    //
    //  `alice` / `bob` 用 makeAddr 分配确定性的、可读的伪地址。比硬编码
    //  address(0x...) 友好得多。
    // -------------------------------------------------------------------------
    Notepad internal notepad;
    address internal alice = makeAddr("alice");
    address internal bob = makeAddr("bob");

    // 测试常量：避免每次重复字面量。
    string constant TITLE = "Hello";
    string constant BODY = "World";

    // 与 Notepad.sol 中的 `public constant` 镜像。Solidity 不允许从外部合约
    // 用 `Notepad.MAX_TITLE_LEN` 直接读 `public constant`（那只是 ABI getter，
    // 不是编译期成员），所以这里再声明一遍；改动源文件常量时记得同步。
    uint256 internal constant TITLE_MAX = 64;
    uint256 internal constant BODY_MAX = 1024;
    uint256 internal constant PER_USER_MAX = 50;

    function setUp() public {
        // Notepad 没有构造函数参数——部署即可。
        notepad = new Notepad();
    }

    // -------------------------------------------------------------------------
    //  内部 helper：避免每个测试重复 `vm.prank(...)` + `notepad.createNote(...)`。
    //  `internal` 函数在测试里只是 DRY 工具，不计入测试用例（forge 只发现
    //  test_ / testFuzz_ / testRevert_ / invariant_ 前缀的函数）。
    // -------------------------------------------------------------------------
    function _createAs(address who, string memory title, string memory body)
        internal
        returns (uint256 id)
    {
        vm.prank(who);
        id = notepad.createNote(title, body);
    }

    /// @dev 构造长度为 n 的纯 'a' 字符串。用于"长度刚好越界"的边界测试。
    function _bytesOfLength(uint256 n) internal pure returns (string memory) {
        bytes memory buf = new bytes(n);
        for (uint256 i = 0; i < n; i++) {
            buf[i] = "a";
        }
        return string(buf);
    }

    // =========================================================================
    //                          createNote (5 个)
    // =========================================================================

    /// @notice 第一条创建的笔记应得 id=1。
    /// @dev    本测试验证两个不变量：
    ///           1) id 从 1 开始（不是 0）；
    ///           2) createdAt 与 updatedAt 在创建瞬间相等（语义：刚创建就
    ///              没人改过它）。
    function test_CreateNote_AssignsIdOne() public {
        vm.prank(alice);
        uint256 id = notepad.createNote(TITLE, BODY);
        assertEq(id, 1);

        Notepad.Note memory n = notepad.getNote(alice, 1);
        assertEq(n.id, 1);
        assertEq(n.title, TITLE);
        assertEq(n.body, BODY);
        // 同一笔交易的 timestamp 是常量——刚创建与"刚更新"应当相同。
        assertEq(n.createdAt, n.updatedAt);
        // timestamp 应当 > 0（0 表示 1970-01-01，实际不可能发生，但 sanity check）。
        assertGt(n.createdAt, 0);
    }

    /// @notice id 在每个 owner 内部单调递增，但不同 owner 之间**独立计数**。
    /// @dev    这是 Notepad 设计的关键：Alice 的 id=3 与 Bob 的 id=1 在
    ///         同一地址空间共存（但因为 storage 是 per-address 的，根本不
    ///         存在冲突）。
    function test_CreateNote_AssignsSequentialIdsPerOwner() public {
        uint256 id1 = _createAs(alice, "a", "1");
        uint256 id2 = _createAs(alice, "b", "2");
        uint256 id3 = _createAs(alice, "c", "3");
        assertEq(id1, 1);
        assertEq(id2, 2);
        assertEq(id3, 3);

        // Bob 的 id 独立计数。
        uint256 bid = _createAs(bob, "x", "y");
        assertEq(bid, 1);
    }

    /// @notice 标题超过 MAX_TITLE_LEN 时应 revert `TitleTooLong`。
    /// @dev    边界值测试：传 MAX+1 字节，恰好越界。
    function test_CreateNote_RevertsTitleTooLong() public {
        string memory tooLong = _bytesOfLength(TITLE_MAX + 1);
        vm.prank(alice);
        vm.expectRevert(Notepad.TitleTooLong.selector);
        notepad.createNote(tooLong, BODY);
    }

    /// @notice 正文超过 MAX_BODY_LEN 时应 revert `BodyTooLong`。
    function test_CreateNote_RevertsBodyTooLong() public {
        string memory tooLong = _bytesOfLength(BODY_MAX + 1);
        vm.prank(alice);
        vm.expectRevert(Notepad.BodyTooLong.selector);
        notepad.createNote(TITLE, tooLong);
    }

    /// @notice 第 51 次创建应当 revert `TooManyNotes`。
    /// @dev    上限测试：循环创建到上限，再次创建必须失败。
    ///         跑得慢一些（51 次合约调用 + 断言），但属于关键不变量。
    function test_CreateNote_RevertsTooManyNotes() public {
        for (uint256 i = 0; i < PER_USER_MAX; i++) {
            _createAs(alice, "t", "b");
        }
        // 第 51 次创建应失败。
        vm.prank(alice);
        vm.expectRevert(Notepad.TooManyNotes.selector);
        notepad.createNote(TITLE, BODY);
    }

    // =========================================================================
    //                          updateNote (3 个)
    // =========================================================================

    /// @notice update 后 title / body 应更新，updatedAt 应推进，createdAt 不变。
    /// @dev    用 `vm.warp(ts)` 把 `block.timestamp` 拨到任意时刻（cheatcode
    ///         只在测试里生效）。然后做 update，再读——验证 `updatedAt` 跟
    ///         `block.timestamp` 而不是 `createdAt`。
    function test_UpdateNote_ChangesFieldsAndBumpsUpdatedAt() public {
        uint256 id = _createAs(alice, TITLE, BODY);
        Notepad.Note memory before = notepad.getNote(alice, id);

        // 时间戳前进 100 秒。
        vm.warp(before.createdAt + 100);

        vm.prank(alice);
        notepad.updateNote(id, "new title", "new body");

        Notepad.Note memory after_ = notepad.getNote(alice, id);
        assertEq(after_.id, id);
        assertEq(after_.title, "new title");
        assertEq(after_.body, "new body");
        // createdAt 必须保持不变（这是历史不变量）。
        assertEq(after_.createdAt, before.createdAt, "createdAt unchanged");
        // updatedAt 必须前进到当前 block.timestamp。
        assertEq(after_.updatedAt, before.createdAt + 100, "updatedAt bumped");
    }

    /// @notice update 一个不存在的 id 应 revert `NoteNotFound`。
    function test_UpdateNote_RevertsNoteNotFound() public {
        vm.prank(alice);
        vm.expectRevert(Notepad.NoteNotFound.selector);
        notepad.updateNote(999, TITLE, BODY);
    }

    /// @notice update 时传超过 MAX_BODY_LEN 的 body 也应 revert `BodyTooLong`。
    /// @dev    校验的顺序问题：理论上 `NoteNotFound` 与 `BodyTooLong` 哪个
    ///         先抛？当前合约先长度校验、再 id 校验——所以本测试即使 id=1
    ///         存在也仍然抛 `BodyTooLong`，证明顺序符合预期。
    function test_UpdateNote_RevertsBodyTooLong() public {
        uint256 id = _createAs(alice, TITLE, BODY);
        string memory tooLong = _bytesOfLength(BODY_MAX + 1);
        vm.prank(alice);
        vm.expectRevert(Notepad.BodyTooLong.selector);
        notepad.updateNote(id, TITLE, tooLong);
    }

    // =========================================================================
    //                          deleteNote (3 个)
    // =========================================================================

    /// @notice swap-and-pop 后剩余笔记的 id 必须保持原值。
    /// @dev    这是整个 Notepad 最重要的不变量——所有"id 不复用"的设计都依
    ///         赖它。详细图示见 `docs/ARCHITECTURE.md` §5.2 与
    ///         `src/Notepad.sol::deleteNote` 的注释。
    ///
    ///         测试场景：
    ///             创建 id=1, 2, 3
    ///             删除 id=2
    ///             验证 getNote(alice, 2) revert（NoteNotFound）
    ///             验证 getNote(alice, 1) 与 getNote(alice, 3) 都仍然在
    ///             且字段未被破坏
    function test_DeleteNote_SwapsAndPops_PreservesOtherIds() public {
        uint256 id1 = _createAs(alice, "one", "1");
        uint256 id2 = _createAs(alice, "two", "2");
        uint256 id3 = _createAs(alice, "three", "3");

        // 删中间一条。
        vm.prank(alice);
        notepad.deleteNote(id2);

        // 长度变为 2。
        assertEq(notepad.getNoteCount(alice), 2);

        // id=1 与 id=3 都还在，字段未被破坏。
        Notepad.Note memory n1 = notepad.getNote(alice, id1);
        Notepad.Note memory n3 = notepad.getNote(alice, id3);
        assertEq(n1.id, id1);
        assertEq(n1.title, "one");
        assertEq(n3.id, id3);
        assertEq(n3.title, "three");

        // id=2 已不存在。
        vm.expectRevert(Notepad.NoteNotFound.selector);
        notepad.getNote(alice, id2);
    }

    /// @notice 删除不存在的 id 应 revert `NoteNotFound`。
    function test_DeleteNote_RevertsNoteNotFound() public {
        vm.prank(alice);
        vm.expectRevert(Notepad.NoteNotFound.selector);
        notepad.deleteNote(1);
    }

    /// @notice delete 必须 emit `NoteDeleted` 事件——indexer / subgraph 依赖此事件。
    /// @dev    `vm.expectEmit(topic1, topic2, topic3, data)` 把接下来 1 次
    ///         调用的事件 emit 与本测试期望的事件比对：
    ///           - `true, true, false, true` —— 比对 topics[0..2] + data；
    ///           - `false` 表示对应槽位不比对（不关心它的值）。
    ///
    ///         这里 NoteDeleted 有 (topic0=event sig, topic1=owner indexed,
    ///         topic2=id indexed, data=at 非 indexed)，所以比对 t0+t1+t2+data。
    function test_DeleteNote_EmitsNoteDeleted() public {
        uint256 id = _createAs(alice, TITLE, BODY);

        // 期望：emit NoteDeleted(alice, id, block.timestamp)
        vm.expectEmit(true, true, false, true);
        emit Notepad.NoteDeleted(alice, id, uint64(block.timestamp));

        vm.prank(alice);
        notepad.deleteNote(id);
    }

    // =========================================================================
    //                              Views (2 个)
    // =========================================================================

    /// @notice getNotes 按存储顺序返回所有笔记；空 owner 返回空数组而非 revert。
    /// @dev    ABI 解码时 Solidity 的动态数组在 EVM 里是 ABI 编码的：
    ///         [offset][len][elements...]。Solidity 测试里返回 `Note[] memory`，
    ///         索引访问正常。
    function test_GetNotes_ReturnsAllInOrder() public {
        _createAs(alice, "a", "1");
        _createAs(alice, "b", "2");
        _createAs(alice, "c", "3");

        Notepad.Note[] memory all = notepad.getNotes(alice);
        assertEq(all.length, 3);
        assertEq(all[0].id, 1);
        assertEq(all[0].title, "a");
        assertEq(all[1].id, 2);
        assertEq(all[1].title, "b");
        assertEq(all[2].id, 3);
        assertEq(all[2].title, "c");

        // 空 owner：返回空数组，**不是** revert。
        assertEq(notepad.getNotes(bob).length, 0);
    }

    /// @notice getNoteCount 必须跟随创建 / 删除变化。
    /// @dev    顺带验证：删 id=1 后，id=2 仍可被 getNote 取到。
    function test_GetNoteCount_ReflectsCreatesAndDeletes() public {
        assertEq(notepad.getNoteCount(alice), 0);
        uint256 id1 = _createAs(alice, "a", "1");
        uint256 id2 = _createAs(alice, "b", "2");
        assertEq(notepad.getNoteCount(alice), 2);

        vm.prank(alice);
        notepad.deleteNote(id1);
        assertEq(notepad.getNoteCount(alice), 1);

        // id2 还在。
        Notepad.Note memory n = notepad.getNote(alice, id2);
        assertEq(n.id, id2);
    }

    // =========================================================================
    //                              Fuzz (1 个)
    // =========================================================================

    /// @notice 任意 title / body（受长度约束）都应能在 create + update 往返
    ///         中保留下来。
    /// @dev    `testFuzz_` 前缀让 forge 自动用随机输入跑 256 次（默认）。
    ///     改高：`forge test --fuzz-runs 10000`。
    ///
    ///         `vm.assume(cond)`：剔除不满足前置条件的输入（这里是长度）。
    ///         长度约束的边界用例已经被 `test_CreateNote_RevertsTitleTooLong`
    ///         等专门覆盖——fuzz 只关注"合法区间内的属性"。
    ///
    ///         invariant 待证：create 后读 title/body == 写入；update
    ///         后同上（甚至做 no-op update 也不破坏字段）。
    function testFuzz_CreateAndUpdate(string calldata title, string calldata body) public {
        // 长度越界的输入由专门的 revert 测试覆盖——这里只 fuzz 合法区间。
        vm.assume(bytes(title).length <= TITLE_MAX);
        vm.assume(bytes(body).length <= BODY_MAX);

        vm.prank(alice);
        uint256 id = notepad.createNote(title, body);
        Notepad.Note memory n0 = notepad.getNote(alice, id);
        assertEq(n0.title, title);
        assertEq(n0.body, body);

        // no-op update（同样字段值）后字段仍保持。
        vm.prank(alice);
        notepad.updateNote(id, title, body);
        Notepad.Note memory n1 = notepad.getNote(alice, id);
        assertEq(n1.title, title);
        assertEq(n1.body, body);
        assertEq(n1.id, id);
    }
}

// ===========================================================================
//  调试技巧
// ----------------------------------------------------------------------------
//   - 单跑：`forge test --match-contract NotepadTest -vvv`
//   - 单跑某测试：`forge test --match-test test_DeleteNote -vvvv`
//   - 覆盖率：`forge coverage`
//   - fuzz 增跑：`forge test --match-test testFuzz --fuzz-runs 10000`
//   - 失败时优先看：
//       (a) revert reason（断言错误？业务 revert？）
//       (b) trace（哪个 cheatcode 设置的状态导致问题？）
//       (c) 期望写错了还是合约 bug？
// ===========================================================================