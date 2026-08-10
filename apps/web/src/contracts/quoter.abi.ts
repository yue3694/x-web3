// TODO(spin): replace stub once the Uniswap V3 QuoterV2 ABI is vendored/exported.
// 当前占位只暴露 swap 报价链路需要的最小函数签名（quoteExactInputSingle）。
// QuoterV2 是 Uniswap 官方部署的外部合约，不在 packages/contracts 里，
// 所以 `pnpm contracts:export:abi` 不会生成它 —— 上线前请从
// @uniswap/v3-periphery 的 artifacts 里拷贝完整 ABI 覆盖本文件。
//
// Reference:
//   v3-periphery/contracts/lens/QuoterV2.sol — IQuoterV2.QuoteExactInputSingleParams
//   https://docs.uniswap.org/contracts/v3/reference/periphery/lens/QuoterV2
//
// ⚠️ stateMutability 说明:
// 链上 QuoterV2.quoteExactInputSingle 被声明为 `nonpayable`（它靠 swap 回调
// revert 再解码结果，所以不能标 view），但它**没有**任何持久化写入，用
// eth_call 调用完全安全。wagmi 的 useReadContract 只接受 view/pure 函数，
// 因此这里刻意标注为 `view`，这是 Uniswap 前端通用做法。
// 替换成官方 ABI 时要保留这一处覆盖，否则 useSwapQuote 会类型报错。

export const quoterAbi = [
  {
    type: "function",
    name: "quoteExactInputSingle",
    inputs: [
      {
        name: "params",
        type: "tuple",
        internalType: "struct IQuoterV2.QuoteExactInputSingleParams",
        components: [
          {name: "tokenIn", type: "address", internalType: "address"},
          {name: "tokenOut", type: "address", internalType: "address"},
          {name: "amountIn", type: "uint256", internalType: "uint256"},
          {name: "fee", type: "uint24", internalType: "uint24"},
          {name: "sqrtPriceLimitX96", type: "uint160", internalType: "uint160"},
        ],
      },
    ],
    outputs: [
      {name: "amountOut", type: "uint256", internalType: "uint256"},
      {name: "sqrtPriceX96After", type: "uint160", internalType: "uint160"},
      {name: "initializedTicksCrossed", type: "uint32", internalType: "uint32"},
      {name: "gasEstimate", type: "uint256", internalType: "uint256"},
    ],
    // 见文件头 ⚠️：链上是 nonpayable，这里为了 useReadContract 覆盖成 view。
    stateMutability: "view",
  },
] as const;

export type QuoterAbi = typeof quoterAbi;
