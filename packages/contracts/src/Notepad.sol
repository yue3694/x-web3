// SPDX-License-Identifier: MIT
// ============================================================================
//  Notepad.sol  ——  链上记事本（项目核心合约）
// ----------------------------------------------------------------------------
//  每个人（每个 EVM 地址）拥有自己的笔记集合。CRUD 只能由 `msg.sender`
//  对自己的笔记执行；读操作对所有人开放。
//
//  阅读路径建议：
//
//    §1   限制常量（公开给前端的"协议规则"）
//    §2   数据结构（Note + 存储布局）
//    §3   事件（链下索引维度）
//    §4   自定义错误（gas-最优的失败信号）
//    §5   写操作：createNote / updateNote / deleteNote
//    §6   读操作：getNote / getNotes / getNoteCount
//    §7   内部辅助函数（_checkTitle / _checkBody / _checkCapacity /
//          _loadOwned / _indexOf）
//
//  完整设计文档：仓库根 `docs/ARCHITECTURE.md` §5。
// ============================================================================

// 版本锁。0.8.24 与 `foundry.toml` 的 `solc_version` 严格一致——不要单独
// 改动其中一个。
pragma solidity ^0.8.24;

/// @title  Notepad
/// @notice 每地址隔离的链上记事本。
///         - CRUD 仅本人可调（CRUD 写入均带 `msg.sender` 校验）；
///         - 任意 `owner` 的笔记可被任意人读取（公共 view 函数）；
///         - 单条 ≤ 1KB 字节，每地址 ≤ 50 条，标题 ≤ 64 字节。
/// @dev    设计原则：
///
///   1. **存储布局**：`mapping(address => Note[])`。每个用户拥有独立的动
///      态数组；删除使用 swap-and-pop 保持数组紧凑。
///
///   2. **id 不变量**：1-based，单调递增，**永不复用**。删除 id=k 时，把
///      数组尾元素搬到 slot k-1，但保留其原 id。前端必须按 id 升序展示。
///      详见 `docs/ARCHITECTURE.md` §5.2 及 §7 `_indexOf` 的注释。
///
///   3. **时间戳**：用 `uint64` 而不是 `uint256`。EVM 字面常量是 256 位，
///      写 uint64 编译器会 mask 一次，但读取与比较的逻辑成本一样；存储与
///      calldata 更紧凑，且够用到公元 584,000,000,000 年（uint64 秒）。
///
///   4. **自定义错误**优先于 `require(string, ...)`：每个错误 4 字节
///      selector + 静态 ABI 编码，省 gas 且链下解码确定。
///
///   5. **CEI（checks-effects-interactions）**：本合约无 external call，所
///      有写函数在写存储后才 emit 事件——但仍然先校验，再修改，再 emit，
///      与可能的 future external-call 兼容。
///
/// @custom:security-contact  none (示例合约)
contract Notepad {
    // =========================================================================
    //  §1 限制常量
    // =========================================================================
    //
    //  常量在合约字节码里内联，不占 storage。`public` 自动生成同名的 getter
    //  函数（`MAX_TITLE_LEN()` 等），前端可以链上读到这些数字，避免硬编码。
    //
    //  调参注意：
    //    - 改小：已部署合约不受影响（常量已内联）。
    //    - 改大：必须重新部署才能生效。
    // =========================================================================

    /// @notice 单条笔记标题最大字节数（UTF-8 字节，不是字符数）。
    uint256 public constant MAX_TITLE_LEN = 64;

    /// @notice 单条笔记正文最大字节数（1 KB）。
    uint256 public constant MAX_BODY_LEN = 1024;

    /// @notice 每个地址最多持有多少条笔记。决定 `getNotes` 返回的最大体积。
    uint256 public constant MAX_NOTES_PER_USER = 50;

    // =========================================================================
    //  §2 数据结构
    // =========================================================================

    /// @notice 单条笔记的全部链上表示。
    /// @param  id        1-based，单调递增；删除时不复用。
    /// @param  title     标题，最长 64 字节。
    /// @param  body      正文，最长 1024 字节。
    /// @param  createdAt 创建时间戳（秒，uint64 防未来溢出）。
    /// @param  updatedAt 最近一次 update 时间戳。删除事件里再带 `at` 字段。
    struct Note {
        uint256 id;
        string title;
        string body;
        uint64 createdAt;
        uint64 updatedAt;
    }

    // -------------------------------------------------------------------------
    //  存储布局
    //
    //  `mapping(address => Note[])` —— 每地址持有独立的动态数组。
    //
    //  `private` vs 默认 `internal`：选 `private` 是因为没有任何子合约需要
    //  直接访问它，所有读都走 public getter。这同时也是"代码层封装"——
    //  注意：**链上数据是公开的**，任何人能通过 `eth_getStorageAt` 读到
    //  原始 slot 内容。
    //
    //  gas 提示：
    //    - 读整个数组（`getNotes`）：每元素 ~`200+bytes`（含动态字符串），
    //      50 条 ≈ 11KB，公共 RPC 通常一次能返回；
    //    - 单条读：固定成本 ~`2.1k gas`。
    // -------------------------------------------------------------------------
    mapping(address => Note[]) private _notes;

    // =========================================================================
    //  §3 事件
    // =========================================================================
    //
    //  indexed 字段（最多 3 个）：写入 topics，subgraph / RPC 都可按值过滤。
    //  非 indexed：作为 ABI 编码写入 data，更省 gas 但不能按值过滤。
    //
    //  这里把 `owner` 和 `id` 都标 indexed——这两个是"我要找 Alice 的第 3
    // 条笔记变更历史"这种查询的天然维度。
    // =========================================================================

    /// @notice 新笔记创建。
    event NoteCreated(address indexed owner, uint256 indexed id, uint64 at);

    /// @notice 笔记更新（标题 / 正文任一变化都触发）。
    event NoteUpdated(address indexed owner, uint256 indexed id, uint64 at);

    /// @notice 笔记删除。
    event NoteDeleted(address indexed owner, uint256 indexed id, uint64 at);

    // =========================================================================
    //  §4 自定义错误
    // =========================================================================

    error TitleTooLong();
    error BodyTooLong();
    error TooManyNotes();
    error NoteNotFound();

    // =========================================================================
    //  §5 写操作
    // =========================================================================

    /// @notice 创建新笔记。
    /// @param  title 标题（≤ 64 字节）。允许空字符串。
    /// @param  body  正文（≤ 1024 字节）。允许空字符串。
    /// @return id    新笔记的 id（1-based）。
    /// @dev    CEI 顺序：
    ///           1. checks：长度校验、上限校验（皆 pure / view，零存储代价）；
    ///           2. effects：push 进 `_notes[msg.sender]`；
    ///           3. emit：事件跟随状态变更。
    ///         本合约无 external call，所以"interactions"为空——CEI 仍然写成
    ///         上述顺序，未来引入转账 / hook 时不需重构。
    function createNote(string calldata title, string calldata body)
        external
        returns (uint256 id)
    {
        // ---- checks（校验失败抛自定义错误，省 gas 且链下可读）----
        _checkTitle(title);
        _checkBody(body);
        _checkCapacity(msg.sender);

        // ---- effects（仅修改本合约 storage）----
        Note[] storage list = _notes[msg.sender];
        id = list.length + 1; // 1-based，与存储下标差 1
        uint64 ts = uint64(block.timestamp); // cast: 秒为单位远小于 uint64 上限
        list.push(Note({id: id, title: title, body: body, createdAt: ts, updatedAt: ts}));

        // ---- emit（放在状态变更之后，确保 indexer 一致性）----
        emit NoteCreated(msg.sender, id, ts);
    }

    /// @notice 更新已有笔记。
    /// @dev    `createdAt` 不变；`updatedAt` 推到 `block.timestamp`。
    ///         没有改 title / body 长度（如果想精确判定是否真的变了，需要
    ///         引入短哈希或旧值字段——本合约选择不优化，每次都按"有变更"处
    ///         理，事件仍然发出。
    function updateNote(uint256 id, string calldata title, string calldata body)
        external
    {
        // 同样走 checks（length）→ effects（写 storage）→ emit
        _checkTitle(title);
        _checkBody(body);
        // _loadOwned 内部对 `_indexOf` 失败会抛 `NoteNotFound()`。
        Note storage n = _loadOwned(msg.sender, id);

        // 注意：**先**做完所有校验，**再**开始写存储——这是 CEI 的关键。
        n.title = title;
        n.body = body;
        n.updatedAt = uint64(block.timestamp);
        emit NoteUpdated(msg.sender, id, n.updatedAt);
    }

    /// @notice 删除指定 id 的笔记，使用 swap-and-pop。
    /// @dev    swap-and-pop 图示（删除 id=2，3 条笔记）：
    ///
    ///             之前:  [{id:1}, {id:2}, {id:3}]
    ///                              ↓
    ///             1. 把尾部 {id:3} 搬到 idx=1（= id-1）的位置
    ///             2. pop() 尾部
    ///
    ///             之后:  [{id:1}, {id:3}]   length = 2
    ///
    ///         **关键**：被搬移的 {id:3} 笔记的 `id` 字段保持不变（它就
    ///         是结构体的整个赋值，不是只赋值 `body`），因此后续
    ///         `updateNote(3, ...)` 仍然找得到它。
    ///
    ///         数组里下标 = id-1 是巧合：因为本合约不重用 id，所以稳定。
    ///         **任何重排操作都必须保留 `id` 不变**——前端依赖它。
    function deleteNote(uint256 id) external {
        Note[] storage list = _notes[msg.sender];
        uint256 idx = _indexOf(list, id);
        uint256 last = list.length - 1;

        // 仅当要删除的不是尾元素时，才需要 swap。
        // 删尾是 O(1) 最常见情况——避免一次多余 SSTORE。
        if (idx != last) {
            list[idx] = list[last]; // 整体赋值：`id` / `title` / `body` / 时间戳 都搬过来
        }
        list.pop();

        emit NoteDeleted(msg.sender, id, uint64(block.timestamp));
    }

    // =========================================================================
    //  §6 读操作
    // =========================================================================
    //
    //  这些函数全部 `view`——链下调用零 gas（通过 `eth_call`）；链上调它们
    //  会消耗 gas 但不会写存储，因此即便 EOA 直接调也不会留痕。
    //
    //  ABI 返回 `Note[] memory` 给前端：wagmi / viem 会自动把字符串字段
    //  解码成 JS string。
    // =========================================================================

    /// @notice 返回某地址的笔记数量。
    /// @dev    公开给任意调用方——没有任何敏感信息。
    function getNoteCount(address owner) external view returns (uint256) {
        return _notes[owner].length;
    }

    /// @notice 按 id 读取某条笔记。`NoteNotFound()` 当 id 不存在。
    function getNote(address owner, uint256 id) external view returns (Note memory) {
        return _loadOwned(owner, id);
    }

    /// @notice 一次性返回某地址所有笔记。
    /// @dev    受 `MAX_NOTES_PER_USER = 50` 限制，response 体积约 ≤ 11KB，
    ///         适合一次性拉到前端本地渲染。如果未来放开上限，应改成游标
    ///         分页（`getNotesPaginated(address, uint256 offset, uint256 limit)`）
    ///         避免单次 response 超过 RPC 节点限制（Alchemy 免费档 10MB，
    ///         公共 RPC 通常 100KB）。
    function getNotes(address owner) external view returns (Note[] memory) {
        return _notes[owner];
    }

    // =========================================================================
    //  §7 内部辅助
    // =========================================================================

    /// @dev 通过 owner + id 解析出 storage 引用；找不到抛 `NoteNotFound()`。
    ///      `_indexOf` 是 O(N) 线性扫，但 N ≤ 50，可接受。
    function _loadOwned(address owner, uint256 id) internal view returns (Note storage) {
        Note[] storage list = _notes[owner];
        return list[_indexOf(list, id)];
    }

    /// @dev 在 `list`（长度 ≤ 50）里按 id 找下标。找不到抛 `NoteNotFound()`。
    ///      用 `storage` 而不是 `memory` 是因为我们要把 `_indexOf` 的结果
    ///      立即用在 `list[idx]` 上——`storage` 引用指回原数组。
    function _indexOf(Note[] storage list, uint256 id) internal view returns (uint256) {
        unchecked {
            // unchecked：循环上限 `list.length` 是 storage 长度，本身已经
            // 受 `MAX_NOTES_PER_USER` 约束。循环变量 `i` 不会溢出 uint256。
            // unchecked-safety: `i <= list.length < 2^256`，循环安全。
            for (uint256 i = 0; i < list.length; i++) {
                if (list[i].id == id) return i;
            }
        }
        revert NoteNotFound();
    }

    /// @dev 用 `bytes(s).length` 而不是 `s.length`：`s.length` 是 UTF-8 字符
    ///      数（不一定等于字节数，emoji / 中日韩字符每个占 3 字节），
    ///      `bytes(s).length` 才是字节数。我们限额是字节，所以取后者。
    function _checkTitle(string calldata s) internal pure {
        if (bytes(s).length > MAX_TITLE_LEN) revert TitleTooLong();
    }

    /// @dev 同 `_checkTitle`，作用于 body。
    function _checkBody(string calldata s) internal pure {
        if (bytes(s).length > MAX_BODY_LEN) revert BodyTooLong();
    }

    /// @dev 检查 `_notes[owner].length < MAX_NOTES_PER_USER`。
    ///      注意 `>=` 而非 `>`：满 50 条再调就会抛错。
    function _checkCapacity(address owner) internal view {
        if (_notes[owner].length >= MAX_NOTES_PER_USER) revert TooManyNotes();
    }
}

// ============================================================================
//  已知安全检查清单（自查用，CI 可加 `slither` / `mythril` 配合）
// ----------------------------------------------------------------------------
//   [x] 无 external call —— 无重入面
//   [x] 无 selfdestruct / delegatecall —— 无升级 / 自毁面
//   [x] 0.8.24 checked 算术 —— 无溢出面
//   [x] 字符串长度显式校验 —— 无超大 calldata DoS
//   [x] 自定义错误 —— 无错误信息泄露隐私
//   [x] 事件带 owner + id indexed —— 前端 / indexer 可追踪
//   [x] 没有 tx.origin 鉴权 / block.timestamp 随机源
// ============================================================================