-- 0002_web3_courses.sql: Web3 技术栈课程种子数据（讲师 + 分类 + 课程 + 版本 + 章节 + 课时）。
-- 顺序：teachers → user_roles → course_categories → courses → course_versions →
--       course_category_map → chapters → lessons。
-- 设计要点：
--   1. 全部幂等：users / courses 用自然键（privy_user_id / slug）走 ON CONFLICT，
--      关联数据走子查询取 id；
--   2. 课程 status='published' + published_at 非空 + deleted_at NULL，满足目录查询；
--   3. course_versions.version = courses.current_version，描述写在版本表上；
--   4. chapters.position / lessons.position 从 0 开始连续；
--   5. media_asset_id 一律 NULL，duration_seconds 用真实感秒数。

BEGIN;

-- ============================================================
-- 讲师用户（teacher）
-- ============================================================
INSERT INTO users (privy_user_id, display_name, status) VALUES
  ('seed:teacher:lin-yuanzhou', '林远舟', 'active'),
  ('seed:teacher:su-jingxing',  '苏景行', 'active')
ON CONFLICT (privy_user_id) DO NOTHING;

-- ============================================================
-- user_roles：授予两位讲师 'teacher' 角色
-- 写法对齐 0001_roles.sql：SELECT ... FROM roles 子查询
-- ============================================================
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id
FROM users u, roles r
WHERE u.privy_user_id IN ('seed:teacher:lin-yuanzhou', 'seed:teacher:su-jingxing')
  AND r.code = 'teacher'
ON CONFLICT DO NOTHING;

-- ============================================================
-- course_categories: Web3 课程分类
-- ============================================================
INSERT INTO course_categories (slug, title) VALUES
  ('evm-core',           'EVM 内核与合约开发'),
  ('defi',               'DeFi 协议机制'),
  ('security',           '合约安全与审计'),
  ('zk',                 '零知识证明与扩容'),
  ('infra',              '链上数据与节点基础设施'),
  ('account-abstraction','账户抽象与智能账户')
ON CONFLICT (slug) DO NOTHING;

-- ============================================================
-- courses: 7 门课程，published_at 错开便于目录排序层次
-- ============================================================
INSERT INTO courses (teacher_id, slug, title, status, current_version, price_minor, currency, published_at)
SELECT t.id, v.slug, v.title, 'published', 1, v.price_minor, 'USD', v.published_at
FROM (VALUES
  ('seed:teacher:lin-yuanzhou',
   'evm-storage-layout-and-solidity-advanced',
   'EVM 存储布局与 Solidity 进阶',
   12900,
   now() - interval '12 days'),
  ('seed:teacher:lin-yuanzhou',
   'solidity-gas-optimization-and-yul',
   'Solidity Gas 优化与 Yul 内联汇编',
   9900,
   now() - interval '9 days'),
  ('seed:teacher:su-jingxing',
   'evm-proxy-patterns-and-storage-collision-defense',
   '代理合约与存储槽碰撞防御',
   8900,
   now() - interval '17 days'),
  ('seed:teacher:su-jingxing',
   'smart-contract-security-audit-in-practice',
   '智能合约安全审计实战',
   14900,
   now() - interval '6 days'),
  ('seed:teacher:lin-yuanzhou',
   'amm-and-concentrated-liquidity-mechanics',
   'AMM 与集中流动性协议机制',
   12900,
   now() - interval '21 days'),
  ('seed:teacher:su-jingxing',
   'optimistic-vs-zk-rollup-and-data-availability',
   'Optimistic 与 ZK Rollup 对比及数据可用性',
   0,
   now() - interval '3 days'),
  ('seed:teacher:lin-yuanzhou',
   'account-abstraction-erc-4337',
   '账户抽象 ERC-4337：EntryPoint、Paymaster 与 Bundler',
   10900,
   now() - interval '14 days')
) AS v(privy_user_id, slug, title, price_minor, published_at)
JOIN users t ON t.privy_user_id = v.privy_user_id
ON CONFLICT (slug) DO NOTHING;

-- ============================================================
-- course_versions: 每门课的 v1 描述（关键：version 必须等于 courses.current_version）
-- ============================================================
INSERT INTO course_versions (course_id, version, description)
SELECT c.id, c.current_version, v.description
FROM (VALUES
  ('evm-storage-layout-and-solidity-advanced',
   '围绕 EVM 状态树、存储槽打包、Solidity 内部类型映射与字节布局展开。学完后能独立审计一个中型 DeFi 项目的存储模型，定位 upgrade 风险点，并用 foundry-chisel 现场验证猜测。'),
  ('solidity-gas-optimization-and-yul',
   '从 opcode 成本表出发讲 storage packing、短字符串优化与 immutables，再落地到 Yul 与 assembly 内联实战。配套 forge gas-report 基准与 PR 守护脚本，确保优化不回退。'),
  ('evm-proxy-patterns-and-storage-collision-defense',
   '系统讲透明代理、UUPS、Beacon 与 Diamond 的 delegatecall 模型与 storage layout 风险。动手部署一个生产级 UUPS + EIP-1967 槽位隔离，并接入 storage layout 检查到 CI。'),
  ('smart-contract-security-audit-in-practice',
   '以真实历史事件为线索，覆盖重入变体、价格预言机操纵、签名重放、permit 风险、抢跑与三明治攻击。配合 Foundry invariant、fuzz 与 differential test，给出可复现的 PoC 报告模板。'),
  ('amm-and-concentrated-liquidity-mechanics',
   '从恒定乘积 x*y=k 出发，延伸到 Uniswap V3 集中流动性、tick 与费率档位，再深入 v4 的 hook 与 singleton 架构。配套在 mainnet fork 上推算 PnL 与 IL。'),
  ('optimistic-vs-zk-rollup-and-data-availability',
   '系统对比 Optimistic 与 ZK Rollup 的安全模型、终局性、欺诈证明与有效性证明，回看 data availability 与 DAS。配套 Celestia / EigenDA 的工程权衡与最小 DAS 客户端 demo。'),
  ('account-abstraction-erc-4337',
   '从 EOA 的局限讲到 ERC-4337 的 UserOperation 生命周期、EntryPoint 合约、Paymaster 与 Bundler。在本地 stack（bundler + paymaster + account factory）上发出第一笔 sponsored 用户操作。')
) AS v(slug, description)
JOIN courses c ON c.slug = v.slug
ON CONFLICT (course_id, version) DO NOTHING;

-- ============================================================
-- course_category_map: 课程与分类的多对多关联（显式展开避免 LATERAL 列名歧义）
-- ============================================================
INSERT INTO course_category_map (course_id, category_id)
SELECT c.id, cat.id
FROM (VALUES
  ('evm-storage-layout-and-solidity-advanced',       'evm-core'),
  ('evm-storage-layout-and-solidity-advanced',       'security'),
  ('solidity-gas-optimization-and-yul',              'evm-core'),
  ('evm-proxy-patterns-and-storage-collision-defense','evm-core'),
  ('evm-proxy-patterns-and-storage-collision-defense','security'),
  ('smart-contract-security-audit-in-practice',      'security'),
  ('amm-and-concentrated-liquidity-mechanics',       'defi'),
  ('optimistic-vs-zk-rollup-and-data-availability',  'zk'),
  ('optimistic-vs-zk-rollup-and-data-availability',  'infra'),
  ('account-abstraction-erc-4337',                   'account-abstraction'),
  ('account-abstraction-erc-4337',                   'evm-core')
) AS v(course_slug, cat_slug)
JOIN courses c ON c.slug = v.course_slug
JOIN course_categories cat ON cat.slug = v.cat_slug
ON CONFLICT DO NOTHING;

-- ============================================================
-- chapters + lessons
-- 通过 course_slug 关联到 course_versions(chapters) 与 chapters(lessons)
-- 下面每个 INSERT 一次只处理一门课，避免 SQL 过长
-- ============================================================

-- -------------------- 课程 1：EVM 存储布局与 Solidity 进阶 --------------------
INSERT INTO chapters (course_version_id, position, title)
SELECT cv.id, ch.pos, ch.title
FROM course_versions cv
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 'EVM 状态机与账户模型'),
  (1::int, '存储槽打包（Storage Packing）'),
  (2::int, '内存、调用数据与栈'),
  (3::int, 'ABI 编码、字节序与 type(uint256).max 陷阱'),
  (4::int, '实战：定位一个真实合约的存储风险')
) AS ch(pos, title)
WHERE c.slug = 'evm-storage-layout-and-solidity-advanced'
  AND cv.version = c.current_version
ON CONFLICT (course_version_id, position) DO NOTHING;

INSERT INTO lessons (chapter_id, position, title, required, duration_seconds)
SELECT ch.id, l.pos, l.title, true, l.dur
FROM chapters ch
JOIN course_versions cv ON cv.id = ch.course_version_id
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 0::int, '以太坊世界状态：账户、nonce、storage root 与 Merkle Patricia Trie', 1100),
  (0::int, 1::int, 'EOA 与合约账户的 code / storage 字段语义',                            900),
  (0::int, 2::int, 'SSTORE 0→非 0 / 非 0→0 的 gas 语义与 EIP-2200 refund',                1300),
  (0::int, 3::int, '用 cast storage 解读一个 ERC20 的真实槽位',                            1200),
  (0::int, 4::int, '练习：在 anvil 上部署合约并追踪 storage 变化',                          1500),
  (1::int, 0::int, 'Solidity 变量在 storage 中的连续布局规则',                              1000),
  (1::int, 1::int, 'uint128 / uint64 / address 共用一个 32 字节槽的工程实践',              1100),
  (1::int, 2::int, 'struct 与数组：定长 / 动态的偏移计算',                                  1400),
  (1::int, 3::int, 'bytes32 与 string 的底层存储方式',                                      900),
  (1::int, 4::int, '用 forge inspect 与 slither 静态对比布局',                             1300),
  (2::int, 0::int, 'EVM 内存线性分配与 64/32 字节规则',                                     950),
  (2::int, 1::int, 'calldata 与 memory 的边界：何时选 calldata',                            850),
  (2::int, 2::int, 'free memory pointer 与 scratch space',                                  900),
  (2::int, 3::int, '栈深度限制 (1024) 与 tail call 的隐性代价',                            1100),
  (3::int, 0::int, 'abi.encode / encodePacked / encodeWithSignature 的差异',                1200),
  (3::int, 1::int, '哈希签名中的紧凑编码 vs 标准编码',                                      1000),
  (3::int, 2::int, 'type(uint256).max 在 approval 中的滥用与 permit 风险',                  1300),
  (4::int, 0::int, '复盘 EIP-4626 vault 早期实现的 share 通胀漏洞',                        1500),
  (4::int, 1::int, '案例：Proxy 升级导致存储布局错位的 hack 还原',                          1600),
  (4::int, 2::int, '编写 foundry invariant：share 总和守恒',                                1500),
  (4::int, 3::int, '用 chisel 现场推算 slot 偏移并验证',                                   1300)
) AS l(ch_pos, pos, title, dur)
WHERE c.slug = 'evm-storage-layout-and-solidity-advanced'
  AND cv.version = c.current_version
  AND ch.position = l.ch_pos
ON CONFLICT (chapter_id, position) DO NOTHING;

-- -------------------- 课程 2：Solidity Gas 优化与 Yul 内联汇编 --------------------
INSERT INTO chapters (course_version_id, position, title)
SELECT cv.id, ch.pos, ch.title
FROM course_versions cv
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 'Gas 经济学与 opcode 成本'),
  (1::int, 'Solidity 层省 gas 的通用模式'),
  (2::int, '短字符串、Custom Error 与字节优化'),
  (3::int, '内联汇编与 Yul 入门'),
  (4::int, '实战基准与 CI 集成')
) AS ch(pos, title)
WHERE c.slug = 'solidity-gas-optimization-and-yul'
  AND cv.version = c.current_version
ON CONFLICT (course_version_id, position) DO NOTHING;

INSERT INTO lessons (chapter_id, position, title, required, duration_seconds)
SELECT ch.id, l.pos, l.title, true, l.dur
FROM chapters ch
JOIN course_versions cv ON cv.id = ch.course_version_id
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 0::int, 'EVM opcode 基础成本表与黄皮书推导',                                    1200),
  (0::int, 1::int, 'SSTORE / SLOAD / CALL / LOG 的差异化定价',                              1100),
  (0::int, 2::int, '事务内 intrinsic gas、calldata 字节成本与 EIP-2028',                    1300),
  (1::int, 0::int, 'storage 变量打包：从 5 个 SSTORE 减到 1 个',                            1100),
  (1::int, 1::int, 'immutable / constant 的代码层语义与节省',                              900),
  (1::int, 2::int, '短路与 unchecked 算术的安全边界',                                      1000),
  (1::int, 3::int, '把 require 顺序按失败概率排：真实工程数据',                            800),
  (2::int, 0::int, 'bytes32 自定义短字符串的 layout',                                       950),
  (2::int, 1::int, '自定义错误 (Custom Error) 替代 require string',                        900),
  (2::int, 2::int, '使用自定义错误后 trace 体积与 gas 的变化',                              850),
  (3::int, 0::int, 'assembly 块的安全边界与内存破坏',                                       1100),
  (3::int, 1::int, 'Yul：mstore / sstore / tstore（EIP-1153）基础',                        1300),
  (3::int, 2::int, '在 assembly 里直接读 msg.data 与解码 calldata',                        1400),
  (3::int, 3::int, '用 Yul 手写 ERC20 transfer 并对比 gas',                                1800),
  (3::int, 4::int, 'transient storage（EIP-1153）的重入锁替代方案',                       1500),
  (4::int, 0::int, 'forge 配置 gas-report 与 snapshot',                                     900),
  (4::int, 1::int, '写最小 ERC20 基准（pure Solidity vs Yul）',                            1500),
  (4::int, 2::int, '引入 gas-snapshot 守卫 PR 不回退',                                     1200),
  (4::int, 3::int, '何时不值得优化：边际收益与代码可读性 trade-off',                        950)
) AS l(ch_pos, pos, title, dur)
WHERE c.slug = 'solidity-gas-optimization-and-yul'
  AND cv.version = c.current_version
  AND ch.position = l.ch_pos
ON CONFLICT (chapter_id, position) DO NOTHING;

-- -------------------- 课程 3：代理合约与存储槽碰撞防御 --------------------
INSERT INTO chapters (course_version_id, position, title)
SELECT cv.id, ch.pos, ch.title
FROM course_versions cv
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 'Delegatecall 语义'),
  (1::int, '透明代理（Transparent Proxy）'),
  (2::int, 'UUPS 与 EIP-1967 槽位'),
  (3::int, 'Beacon 与 Diamond（EIP-2535）'),
  (4::int, '实战：写一个生产级 UUPS')
) AS ch(pos, title)
WHERE c.slug = 'evm-proxy-patterns-and-storage-collision-defense'
  AND cv.version = c.current_version
ON CONFLICT (course_version_id, position) DO NOTHING;

INSERT INTO lessons (chapter_id, position, title, required, duration_seconds)
SELECT ch.id, l.pos, l.title, true, l.dur
FROM chapters ch
JOIN course_versions cv ON cv.id = ch.course_version_id
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 0::int, 'CALL vs DELEGATECALL：代码执行与 storage 归属',                        1100),
  (0::int, 1::int, 'msg.sender 与 msg.value 在 delegatecall 下的保留规则',                  950),
  (0::int, 2::int, 'storage 引用同 address 的 storage tree',                                1000),
  (1::int, 0::int, 'TransparentUpgradeableProxy 的 admin 转发逻辑',                        1300),
  (1::int, 1::int, 'function selector 冲突：管理员调用 vs 用户调用',                        1100),
  (1::int, 2::int, '不能用 constructor 的实现合约与 Initializable',                         1100),
  (2::int, 0::int, 'EIP-1967：固定 storage slot 的工程意义',                              1100),
  (2::int, 1::int, '自毁漏洞历史与 EIP-6780 影响',                                          900),
  (2::int, 2::int, 'UUPSUpgradeable 的 _authorizeUpgrade 与 onlyOwner',                   1200),
  (3::int, 0::int, 'Beacon Proxy：多实现共享一份逻辑地址',                                  1100),
  (3::int, 1::int, 'EIP-2535 Diamond：facet + selector table + storage 隔离',            1400),
  (3::int, 2::int, 'Diamond 存储隔离：AppStorage 模式与 hash 命名空间',                    1500),
  (4::int, 0::int, '写 Initializable + OwnableUpgradeable + UUPSUpgradeable',             1500),
  (4::int, 1::int, '实现一个最小 ERC1967Proxy',                                            1400),
  (4::int, 2::int, '部署脚本：anvil 上 upgrade V1 → V2',                                   1500),
  (4::int, 3::int, '编写测试：未授权升级必须 revert',                                       1100),
  (4::int, 4::int, '集成 storage layout 检查到 CI（forge inspect）',                       1300)
) AS l(ch_pos, pos, title, dur)
WHERE c.slug = 'evm-proxy-patterns-and-storage-collision-defense'
  AND cv.version = c.current_version
  AND ch.position = l.ch_pos
ON CONFLICT (chapter_id, position) DO NOTHING;

-- -------------------- 课程 4：智能合约安全审计实战 --------------------
INSERT INTO chapters (course_version_id, position, title)
SELECT cv.id, ch.pos, ch.title
FROM course_versions cv
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, '审计方法论与攻击面建模'),
  (1::int, '重入与它的所有变体'),
  (2::int, '价格预言机与操纵'),
  (3::int, '签名与 EIP-712 / EIP-2612'),
  (4::int, 'MEV 与抢跑'),
  (5::int, 'Foundry 测试武器库与 PoC 报告')
) AS ch(pos, title)
WHERE c.slug = 'smart-contract-security-audit-in-practice'
  AND cv.version = c.current_version
ON CONFLICT (course_version_id, position) DO NOTHING;

INSERT INTO lessons (chapter_id, position, title, required, duration_seconds)
SELECT ch.id, l.pos, l.title, true, l.dur
FROM chapters ch
JOIN course_versions cv ON cv.id = ch.course_version_id
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 0::int, '威胁建模与资产流图 (asset flow)',                                       1100),
  (0::int, 1::int, '攻击面清单：ETH / ERC20 / NFT / 治理 / Oracle',                       1000),
  (0::int, 2::int, 'CodeTrace 与 Slither / Aderyn 静态扫描',                               1300),
  (1::int, 0::int, '单函数重入：The DAO 还原',                                              1500),
  (1::int, 1::int, '跨函数重入与 read-only reentrancy',                                     1400),
  (1::int, 2::int, 'cross-contract reentrancy 与 ERC777 hook',                              1300),
  (1::int, 3::int, 'CEI 在 Solidity 0.8 之后的隐性破窗',                                  1200),
  (2::int, 0::int, 'Uniswap V2 TWAP 的攻击面',                                              1200),
  (2::int, 1::int, '低流动性池预言机的闪电贷操纵',                                          1400),
  (2::int, 2::int, 'Chainlink 延迟 / 故障与多源 fallback',                                  1300),
  (3::int, 0::int, 'ecrecover 零地址签名漏洞',                                              1200),
  (3::int, 1::int, 'nonce 复用与签名重放',                                                  1100),
  (3::int, 2::int, 'permit 与 permit2 的钓鱼面',                                            1300),
  (3::int, 3::int, 'EIP-712 domain separator 跨链隔离',                                    1100),
  (4::int, 0::int, '抢跑 / 尾随 / 三明治攻击机理',                                          1200),
  (4::int, 1::int, '滑点与 deadline 的现实工程',                                            1000),
  (4::int, 2::int, '私有 mempool 与 MEV-blocker',                                           1300),
  (5::int, 0::int, 'invariant 测试 + handler 模式',                                         1500),
  (5::int, 1::int, 'fuzz 配置与失败用例去重',                                              1300),
  (5::int, 2::int, 'differential test：Solidity vs Vyper 实现',                            1400),
  (5::int, 3::int, '撰写一份可复现的 PoC 报告',                                            1500)
) AS l(ch_pos, pos, title, dur)
WHERE c.slug = 'smart-contract-security-audit-in-practice'
  AND cv.version = c.current_version
  AND ch.position = l.ch_pos
ON CONFLICT (chapter_id, position) DO NOTHING;

-- -------------------- 课程 5：AMM 与集中流动性协议机制 --------------------
INSERT INTO chapters (course_version_id, position, title)
SELECT cv.id, ch.pos, ch.title
FROM course_versions cv
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 'AMM 的数学骨架'),
  (1::int, 'Uniswap V2 与 fee tier'),
  (2::int, 'Uniswap V3 集中流动性'),
  (3::int, 'Uniswap V4 与 Hooks'),
  (4::int, '实战：在 anvil fork 上推算 PnL')
) AS ch(pos, title)
WHERE c.slug = 'amm-and-concentrated-liquidity-mechanics'
  AND cv.version = c.current_version
ON CONFLICT (course_version_id, position) DO NOTHING;

INSERT INTO lessons (chapter_id, position, title, required, duration_seconds)
SELECT ch.id, l.pos, l.title, true, l.dur
FROM chapters ch
JOIN course_versions cv ON cv.id = ch.course_version_id
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 0::int, '恒定乘积 x*y=k 的不变式推导',                                          1100),
  (0::int, 1::int, '滑点、冲击成本与路径独立性',                                            1000),
  (0::int, 2::int, 'LP 收益 = 手续费 - 无常损失',                                          1300),
  (1::int, 0::int, 'Router / Factory / Pair 的角色拆分',                                    1000),
  (1::int, 1::int, 'TWAP 价格预言机：过去 30 分钟累积价格',                                 1200),
  (1::int, 2::int, '0.3% fee tier 的取舍',                                                  900),
  (2::int, 0::int, '区间 [Pa, Pb] 内的虚拟流动性',                                          1300),
  (2::int, 1::int, 'tick bitmap 与位运算的 gas 优化',                                       1500),
  (2::int, 2::int, '多档位 LP 策略：range order',                                           1200),
  (2::int, 3::int, 'rebalance 成本 vs fee 收益',                                            1400),
  (3::int, 0::int, 'Singleton + PoolManager 与 EIP-1153 transient',                        1400),
  (3::int, 1::int, '写一个 beforeAddLiquidity / afterSwap hook',                            1600),
  (3::int, 2::int, '自定义费率与链上限价单',                                                1300),
  (4::int, 0::int, '配置 mainnet fork 与 block number 锁定',                                1000),
  (4::int, 1::int, '用 foundry 模拟 LP 区间策略',                                           1600),
  (4::int, 2::int, '输出 PnL / IL / fee 三项分解',                                          1400)
) AS l(ch_pos, pos, title, dur)
WHERE c.slug = 'amm-and-concentrated-liquidity-mechanics'
  AND cv.version = c.current_version
  AND ch.position = l.ch_pos
ON CONFLICT (chapter_id, position) DO NOTHING;

-- -------------------- 课程 6：Optimistic 与 ZK Rollup + 数据可用性 --------------------
INSERT INTO chapters (course_version_id, position, title)
SELECT cv.id, ch.pos, ch.title
FROM course_versions cv
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 'L2 的基本模型'),
  (1::int, 'Optimistic Rollup'),
  (2::int, 'ZK Rollup'),
  (3::int, 'zkEVM 的等级与工程路径'),
  (4::int, '数据可用性与 DA 层')
) AS ch(pos, title)
WHERE c.slug = 'optimistic-vs-zk-rollup-and-data-availability'
  AND cv.version = c.current_version
ON CONFLICT (course_version_id, position) DO NOTHING;

INSERT INTO lessons (chapter_id, position, title, required, duration_seconds)
SELECT ch.id, l.pos, l.title, true, l.dur
FROM chapters ch
JOIN course_versions cv ON cv.id = ch.course_version_id
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 0::int, 'Rollup 状态根 + calldata 的最小方案',                                   1100),
  (0::int, 1::int, 'Sequencer 中心化与公平交易排序',                                        1300),
  (0::int, 2::int, '出块周期、终局性与 finality 的差异',                                    1100),
  (1::int, 0::int, '欺诈证明 (fraud proof) 与挑战期',                                       1400),
  (1::int, 1::int, 'State root bond 与经济安全',                                            1200),
  (1::int, 2::int, 'Optimism Bedrock / Arbitrum BoLD 架构对比',                            1500),
  (2::int, 0::int, '有效性证明 vs 欺诈证明',                                                1200),
  (2::int, 1::int, 'zk-SNARK / zk-STARK 的取舍',                                           1400),
  (2::int, 2::int, '递归证明与 proof aggregation',                                          1500),
  (3::int, 0::int, 'Vitalik 四类 zkEVM 谱系',                                               1300),
  (3::int, 1::int, 'Polygon zkEVM / Scroll / Linea 的工程路径',                            1500),
  (3::int, 2::int, '字节码级别兼容 vs 区块重建',                                            1200),
  (4::int, 0::int, 'data availability 问题与 1-of-N 假设',                                  1300),
  (4::int, 1::int, 'Celestia 数据可用性采样 (DAS)',                                        1500),
  (4::int, 2::int, 'EigenDA restaking 与 L1 锚定',                                         1400),
  (4::int, 3::int, '实战：在本地跑一个 minimal DAS 客户端',                                1800)
) AS l(ch_pos, pos, title, dur)
WHERE c.slug = 'optimistic-vs-zk-rollup-and-data-availability'
  AND cv.version = c.current_version
  AND ch.position = l.ch_pos
ON CONFLICT (chapter_id, position) DO NOTHING;

-- -------------------- 课程 7：账户抽象 ERC-4337 --------------------
INSERT INTO chapters (course_version_id, position, title)
SELECT cv.id, ch.pos, ch.title
FROM course_versions cv
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, '为什么需要账户抽象'),
  (1::int, 'UserOperation 数据结构'),
  (2::int, 'EntryPoint 合约'),
  (3::int, 'Paymaster 模式'),
  (4::int, 'Bundler 与本地 stack')
) AS ch(pos, title)
WHERE c.slug = 'account-abstraction-erc-4337'
  AND cv.version = c.current_version
ON CONFLICT (course_version_id, position) DO NOTHING;

INSERT INTO lessons (chapter_id, position, title, required, duration_seconds)
SELECT ch.id, l.pos, l.title, true, l.dur
FROM chapters ch
JOIN course_versions cv ON cv.id = ch.course_version_id
JOIN courses c ON c.id = cv.course_id
CROSS JOIN (VALUES
  (0::int, 0::int, 'EOA 的私钥丢失与单签风险',                                              1000),
  (0::int, 1::int, '智能账户 (smart account) 的目标',                                       900),
  (0::int, 2::int, 'EIP-2938 / EIP-4337 的设计取舍',                                       1200),
  (1::int, 0::int, 'UserOperation 字段逐项解读',                                           1500),
  (1::int, 1::int, 'gas 估算与 prefund',                                                   1300),
  (1::int, 2::int, 'paymasterAndData / signature 编码',                                    1400),
  (2::int, 0::int, 'handleOps 的执行流程',                                                  1600),
  (2::int, 1::int, 'deposit 与 withdrawTo 的资金流',                                       1200),
  (2::int, 2::int, '内置签名聚合 (EIP-7702 关联)',                                          1100),
  (3::int, 0::int, 'verifying paymaster 的链下签',                                         1400),
  (3::int, 1::int, 'token paymaster 的汇率与限额',                                          1500),
  (3::int, 2::int, 'sponsoring UX 与 anti-abuse',                                           1300),
  (4::int, 0::int, 'bundler 选 UserOperation 的 gas auction',                              1400),
  (4::int, 1::int, 'stack：bundler + paymaster + account factory',                          1500),
  (4::int, 2::int, '本地 stack 发一笔 sponsored 用户操作',                                  1800)
) AS l(ch_pos, pos, title, dur)
WHERE c.slug = 'account-abstraction-erc-4337'
  AND cv.version = c.current_version
  AND ch.position = l.ch_pos
ON CONFLICT (chapter_id, position) DO NOTHING;

COMMIT;