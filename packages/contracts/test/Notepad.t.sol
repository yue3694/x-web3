// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {Notepad} from "../src/Notepad.sol";

/// @notice Tests for the Notepad contract. Counterpart to Counter.t.sol —
///         uses forge-std's Test, expectRevert selectors, makeAddr helpers,
///         and one fuzz test.
contract NotepadTest is Test {
    Notepad internal notepad;
    address internal alice = makeAddr("alice");
    address internal bob = makeAddr("bob");

    string constant TITLE = "Hello";
    string constant BODY = "World";

    function setUp() public {
        notepad = new Notepad();
    }

    // ---------- helpers ----------
    function _createAs(address who, string memory title, string memory body)
        internal
        returns (uint256 id)
    {
        vm.prank(who);
        id = notepad.createNote(title, body);
    }

    function _bytesOfLength(uint256 n) internal pure returns (string memory) {
        bytes memory buf = new bytes(n);
        for (uint256 i = 0; i < n; i++) {
            buf[i] = "a";
        }
        return string(buf);
    }

    // ============================================================
    //                        createNote
    // ============================================================

    function test_CreateNote_AssignsIdOne() public {
        vm.prank(alice);
        uint256 id = notepad.createNote(TITLE, BODY);
        assertEq(id, 1);

        Notepad.Note memory n = notepad.getNote(alice, 1);
        assertEq(n.id, 1);
        assertEq(n.title, TITLE);
        assertEq(n.body, BODY);
        assertEq(n.createdAt, n.updatedAt);
        assertGt(n.createdAt, 0);
    }

    function test_CreateNote_AssignsSequentialIdsPerOwner() public {
        uint256 id1 = _createAs(alice, "a", "1");
        uint256 id2 = _createAs(alice, "b", "2");
        uint256 id3 = _createAs(alice, "c", "3");
        assertEq(id1, 1);
        assertEq(id2, 2);
        assertEq(id3, 3);

        // Bob's ids restart at 1 — ownership is per-address.
        uint256 bid = _createAs(bob, "x", "y");
        assertEq(bid, 1);
    }

    function test_CreateNote_RevertsTitleTooLong() public {
        string memory tooLong = _bytesOfLength(Notepad.MAX_TITLE_LEN + 1);
        vm.prank(alice);
        vm.expectRevert(Notepad.TitleTooLong.selector);
        notepad.createNote(tooLong, BODY);
    }

    function test_CreateNote_RevertsBodyTooLong() public {
        string memory tooLong = _bytesOfLength(Notepad.MAX_BODY_LEN + 1);
        vm.prank(alice);
        vm.expectRevert(Notepad.BodyTooLong.selector);
        notepad.createNote(TITLE, tooLong);
    }

    function test_CreateNote_RevertsTooManyNotes() public {
        for (uint256 i = 0; i < Notepad.MAX_NOTES_PER_USER; i++) {
            _createAs(alice, "t", "b");
        }
        vm.prank(alice);
        vm.expectRevert(Notepad.TooManyNotes.selector);
        notepad.createNote(TITLE, BODY);
    }

    // ============================================================
    //                        updateNote
    // ============================================================

    function test_UpdateNote_ChangesFieldsAndBumpsUpdatedAt() public {
        uint256 id = _createAs(alice, TITLE, BODY);
        Notepad.Note memory before = notepad.getNote(alice, id);

        vm.warp(before.createdAt + 100);
        vm.prank(alice);
        notepad.updateNote(id, "new title", "new body");

        Notepad.Note memory after_ = notepad.getNote(alice, id);
        assertEq(after_.id, id);
        assertEq(after_.title, "new title");
        assertEq(after_.body, "new body");
        assertEq(after_.createdAt, before.createdAt, "createdAt unchanged");
        assertEq(after_.updatedAt, before.createdAt + 100, "updatedAt bumped");
    }

    function test_UpdateNote_RevertsNoteNotFound() public {
        vm.prank(alice);
        vm.expectRevert(Notepad.NoteNotFound.selector);
        notepad.updateNote(999, TITLE, BODY);
    }

    function test_UpdateNote_RevertsBodyTooLong() public {
        uint256 id = _createAs(alice, TITLE, BODY);
        string memory tooLong = _bytesOfLength(Notepad.MAX_BODY_LEN + 1);
        vm.prank(alice);
        vm.expectRevert(Notepad.BodyTooLong.selector);
        notepad.updateNote(id, TITLE, tooLong);
    }

    // ============================================================
    //                        deleteNote
    // ============================================================

    function test_DeleteNote_SwapsAndPops_PreservesOtherIds() public {
        uint256 id1 = _createAs(alice, "one", "1");
        uint256 id2 = _createAs(alice, "two", "2");
        uint256 id3 = _createAs(alice, "three", "3");

        // Delete the middle note (#2). After swap-and-pop the array length
        // is 2, and the surviving note ids should be {1, 3} (or {1, 2} if
        // id3's slot was overwritten by id2 — but we explicitly preserve id).
        vm.prank(alice);
        notepad.deleteNote(id2);

        assertEq(notepad.getNoteCount(alice), 2);

        // The remaining notes are id1 and id3 — both still resolvable.
        Notepad.Note memory n1 = notepad.getNote(alice, id1);
        Notepad.Note memory n3 = notepad.getNote(alice, id3);
        assertEq(n1.id, id1);
        assertEq(n1.title, "one");
        assertEq(n3.id, id3);
        assertEq(n3.title, "three");

        // id2 must be gone.
        vm.expectRevert(Notepad.NoteNotFound.selector);
        notepad.getNote(alice, id2);
    }

    function test_DeleteNote_RevertsNoteNotFound() public {
        vm.prank(alice);
        vm.expectRevert(Notepad.NoteNotFound.selector);
        notepad.deleteNote(1);
    }

    function test_DeleteNote_EmitsNoteDeleted() public {
        uint256 id = _createAs(alice, TITLE, BODY);

        vm.expectEmit(true, true, false, true);
        emit Notepad.NoteDeleted(alice, id, uint64(block.timestamp));

        vm.prank(alice);
        notepad.deleteNote(id);
    }

    // ============================================================
    //                           Views
    // ============================================================

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

        // Empty owner returns empty array, not revert.
        assertEq(notepad.getNotes(bob).length, 0);
    }

    function test_GetNoteCount_ReflectsCreatesAndDeletes() public {
        assertEq(notepad.getNoteCount(alice), 0);
        uint256 id1 = _createAs(alice, "a", "1");
        uint256 id2 = _createAs(alice, "b", "2");
        assertEq(notepad.getNoteCount(alice), 2);

        vm.prank(alice);
        notepad.deleteNote(id1);
        assertEq(notepad.getNoteCount(alice), 1);

        // id2 is still there.
        Notepad.Note memory n = notepad.getNote(alice, id2);
        assertEq(n.id, id2);
    }

    // ============================================================
    //                           Fuzz
    // ============================================================

    /// @notice Round-trip: create then update, ensure fields round-trip
    ///         correctly across a fuzzed input range.
    function testFuzz_CreateAndUpdate(string calldata title, string calldata body) public {
        // Skip over-length inputs — those should revert at create time and
        // are exercised by the dedicated unit tests above.
        vm.assume(bytes(title).length <= Notepad.MAX_TITLE_LEN);
        vm.assume(bytes(body).length <= Notepad.MAX_BODY_LEN);

        vm.prank(alice);
        uint256 id = notepad.createNote(title, body);
        Notepad.Note memory n0 = notepad.getNote(alice, id);
        assertEq(n0.title, title);
        assertEq(n0.body, body);

        vm.prank(alice);
        notepad.updateNote(id, title, body); // no-op update keeps invariants
        Notepad.Note memory n1 = notepad.getNote(alice, id);
        assertEq(n1.title, title);
        assertEq(n1.body, body);
        assertEq(n1.id, id);
    }
}