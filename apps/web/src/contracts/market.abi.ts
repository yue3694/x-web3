// CourseMarket ABI 占位（与 `packages/contracts/src/CourseMarket.sol` 一致）。
//
// 当前函数签名：buyCourse(bytes32 courseKey, uint256 expectedAmount, bytes16 intentId)
//   - courseKey：链上课程键，前端从 UUID 用 sha256 计算（与后端
//     api/internal/order/order.go::CourseKey 对齐；worker test fixture
//     courseKeyForTest 同样 sha256）。合约不验证内容，三端 SSOT 是 SHA-256。
//   - expectedAmount：unit256（YD wei，6 decimals）。API 颁发 intent 时锁定；
//     合约端二次校验防 price tampering。
//   - intentId：bytes16 = UUID 高 128 位（worker 用其匹配 purchase_intents.id）。
//
// 真实部署后请用 `pnpm contracts:export:abi` 自动生成；
// 当前 stub 暴露前端购买链路所需的最小函数 + 事件。

import type {Abi} from "viem";

export const marketAbi = [
    {
        type: "function",
        name: "owner",
        inputs: [],
        outputs: [{name: "", type: "address"}],
        stateMutability: "view",
    },
    {
        type: "function",
        name: "buyCourse",
        inputs: [
            {name: "courseKey", type: "bytes32"},
            {name: "expectedAmount", type: "uint256"},
            {name: "intentId", type: "bytes16"},
        ],
        outputs: [],
        stateMutability: "nonpayable",
    },
    {
        type: "function",
        name: "configureCourse",
        inputs: [
            {name: "courseKey", type: "bytes32"},
            {name: "token", type: "address"},
            {name: "amount", type: "uint256"},
            {name: "priceVersion", type: "uint256"},
        ],
        outputs: [],
        stateMutability: "nonpayable",
    },
    {
        type: "event",
        name: "CoursePurchased",
        inputs: [
            {name: "courseKey", type: "bytes32", indexed: true},
            {name: "buyer", type: "address", indexed: true},
            {name: "token", type: "address", indexed: false},
            {name: "amount", type: "uint256", indexed: false},
            {name: "intentId", type: "bytes16", indexed: false},
            {name: "priceVersion", type: "uint256", indexed: false},
        ],
        anonymous: false,
    },
    {
        type: "event",
        name: "CourseConfigured",
        inputs: [
            {name: "courseKey", type: "bytes32", indexed: true},
            {name: "token", type: "address", indexed: false},
            {name: "amount", type: "uint256", indexed: false},
            {name: "priceVersion", type: "uint256", indexed: false},
        ],
        anonymous: false,
    },
] as const satisfies Abi;

export type MarketAbi = typeof marketAbi;
