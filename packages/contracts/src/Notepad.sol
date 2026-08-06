// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title  Notepad
/// @notice Per-wallet on-chain notepad. Each `msg.sender` owns an isolated
///         list of notes that only they can mutate; anyone can read any
///         owner's notes via the public view functions.
/// @dev    Storage layout is `mapping(address => Note[])`. `id` is 1-based
///         and never reused — deleting swaps the tail into the freed slot
///         but preserves each remaining note's original `id`.
contract Notepad {
    // ---------- Limits (public for off-chain tooling) ----------
    uint256 public constant MAX_TITLE_LEN = 64; // bytes
    uint256 public constant MAX_BODY_LEN = 1024; // 1 KB
    uint256 public constant MAX_NOTES_PER_USER = 50;

    // ---------- Types ----------
    struct Note {
        uint256 id; // 1-based, monotonically increasing per owner
        string title;
        string body;
        uint64 createdAt;
        uint64 updatedAt;
    }

    // ---------- Storage ----------
    mapping(address => Note[]) private _notes;

    // ---------- Events ----------
    /// @notice Emitted when `owner` creates a new note.
    event NoteCreated(address indexed owner, uint256 indexed id, uint64 at);
    /// @notice Emitted when `owner` updates an existing note.
    event NoteUpdated(address indexed owner, uint256 indexed id, uint64 at);
    /// @notice Emitted when `owner` deletes a note.
    event NoteDeleted(address indexed owner, uint256 indexed id, uint64 at);

    // ---------- Errors ----------
    error TitleTooLong();
    error BodyTooLong();
    error TooManyNotes();
    error NoteNotFound();

    // ============================================================
    //                       Mutations
    // ============================================================

    /// @notice Create a new note owned by `msg.sender`.
    /// @param  title Headline text, max 64 bytes.
    /// @param  body  Body text, max 1024 bytes.
    /// @return id    Newly assigned id (1-based).
    function createNote(string calldata title, string calldata body)
        external
        returns (uint256 id)
    {
        _checkTitle(title);
        _checkBody(body);
        _checkCapacity(msg.sender);

        Note[] storage list = _notes[msg.sender];
        id = list.length + 1; // 1-based, monotonic
        uint64 ts = uint64(block.timestamp);
        list.push(Note({id: id, title: title, body: body, createdAt: ts, updatedAt: ts}));

        emit NoteCreated(msg.sender, id, ts);
    }

    /// @notice Replace title and body of note `id` owned by `msg.sender`.
    ///         `createdAt` is preserved; `updatedAt` is bumped.
    function updateNote(uint256 id, string calldata title, string calldata body)
        external
    {
        _checkTitle(title);
        _checkBody(body);
        Note storage n = _loadOwned(msg.sender, id);
        n.title = title;
        n.body = body;
        n.updatedAt = uint64(block.timestamp);
        emit NoteUpdated(msg.sender, id, n.updatedAt);
    }

    /// @notice Delete note `id` owned by `msg.sender`. Uses swap-and-pop to
    ///         keep the underlying array compact; the surviving tail note's
    ///         original `id` is preserved.
    function deleteNote(uint256 id) external {
        Note[] storage list = _notes[msg.sender];
        uint256 idx = _indexOf(list, id);
        uint256 last = list.length - 1;
        if (idx != last) {
            // Move the tail note into the freed slot, preserving its own id.
            list[idx] = list[last];
        }
        list.pop();
        emit NoteDeleted(msg.sender, id, uint64(block.timestamp));
    }

    // ============================================================
    //                          Views
    // ============================================================

    /// @notice Number of notes currently owned by `owner`.
    function getNoteCount(address owner) external view returns (uint256) {
        return _notes[owner].length;
    }

    /// @notice Return a single note by id. Reverts with `NoteNotFound` if absent.
    function getNote(address owner, uint256 id) external view returns (Note memory) {
        return _loadOwned(owner, id);
    }

    /// @notice Return every note owned by `owner` in storage order.
    /// @dev    Bounded to `MAX_NOTES_PER_USER` items; intended for off-chain
    ///         consumption where the caller pays no gas.
    function getNotes(address owner) external view returns (Note[] memory) {
        return _notes[owner];
    }

    // ============================================================
    //                       Internals
    // ============================================================

    function _loadOwned(address owner, uint256 id) internal view returns (Note storage) {
        Note[] storage list = _notes[owner];
        return list[_indexOf(list, id)];
    }

    /// @dev Linear scan — acceptable because `list.length` is bounded to 50.
    function _indexOf(Note[] storage list, uint256 id) internal view returns (uint256) {
        for (uint256 i = 0; i < list.length; i++) {
            if (list[i].id == id) return i;
        }
        revert NoteNotFound();
    }

    function _checkTitle(string calldata s) internal pure {
        if (bytes(s).length > MAX_TITLE_LEN) revert TitleTooLong();
    }

    function _checkBody(string calldata s) internal pure {
        if (bytes(s).length > MAX_BODY_LEN) revert BodyTooLong();
    }

    function _checkCapacity(address owner) internal view {
        if (_notes[owner].length >= MAX_NOTES_PER_USER) revert TooManyNotes();
    }
}