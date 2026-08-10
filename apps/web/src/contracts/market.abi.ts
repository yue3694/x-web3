// TODO(spin): replace stub once CourseMarket is implemented in packages/contracts.
// 当前的 ABI 占位只暴露前端购买链路需要的最小函数签名（purchase + 事件），
// 实际合约方法、事件和 error 应由 `pnpm contracts:export:abi` 自动生成，
// 不要在此文件手写完整 ABI。
//
// Reference: design.md F03 §3 — CourseMarket 入口合约。
// 部署后 ABI 由 packages/contracts/script/export-abi.mjs 生成，
// 地址登记到 apps/web/src/contracts/deployments.ts 的 courseMarketDeployments。

export const marketAbi = [
    {
        type: "function",
        name: "purchase",
        inputs: [
            {name: "courseKey", type: "bytes32", internalType: "bytes32"},
            {name: "recipient", type: "address", internalType: "address"},
        ],
        outputs: [],
        stateMutability: "nonpayable",
    },
] as const;

export type MarketAbi = typeof marketAbi;
