// TODO(spin): replace stub once the Uniswap V3 SwapRouter ABI is vendored/exported.
// 当前占位只暴露 swap 写链路需要的最小函数签名（exactInputSingle）。
// SwapRouter 是 Uniswap 官方部署的外部合约，不在 packages/contracts 里，
// 所以 `pnpm contracts:export:abi` 不会生成它 —— 上线前请从
// @uniswap/swap-router-contracts 的 artifacts 里拷贝完整 ABI 覆盖本文件。
//
// Reference:
//   v3-periphery/contracts/interfaces/ISwapRouter.sol — ExactInputSingleParams
//   https://docs.uniswap.org/contracts/v3/reference/periphery/SwapRouter
//
// ⚠️ deadline 位置说明（两个 router 不兼容，替换 ABI 时必看）:
//   - SwapRouter   (0xE592…1564)：ExactInputSingleParams **含** deadline 字段，
//     即下面这份 struct 布局。
//   - SwapRouter02 (0x6845…45E4)：IV3SwapRouter 把 deadline 从 struct 里删了，
//     改成用 `multicall(uint256 deadline, bytes[] data)` 在外层带截止时间。
// F05 要求 exactInputSingle 必须带 deadline，所以这里采用含 deadline 的布局。
// 如果最终部署的确实是 02，请把 deadline 从 struct 移除，并让 SwapCard 改走
// multicall 包装（README「Deadline math」一节有迁移说明）。

export const swapRouterAbi = [
  {
    type: "function",
    name: "exactInputSingle",
    inputs: [
      {
        name: "params",
        type: "tuple",
        internalType: "struct ISwapRouter.ExactInputSingleParams",
        components: [
          {name: "tokenIn", type: "address", internalType: "address"},
          {name: "tokenOut", type: "address", internalType: "address"},
          {name: "fee", type: "uint24", internalType: "uint24"},
          {name: "recipient", type: "address", internalType: "address"},
          {name: "deadline", type: "uint256", internalType: "uint256"},
          {name: "amountIn", type: "uint256", internalType: "uint256"},
          {name: "amountOutMinimum", type: "uint256", internalType: "uint256"},
          {name: "sqrtPriceLimitX96", type: "uint160", internalType: "uint160"},
        ],
      },
    ],
    outputs: [{name: "amountOut", type: "uint256", internalType: "uint256"}],
    stateMutability: "payable",
  },
] as const;

export type SwapRouterAbi = typeof swapRouterAbi;
