/** Minimal ERC-20 ABI used by checkout and swap allowance flows. */
export const erc20Abi = [
    {
        type: "function",
        name: "balanceOf",
        stateMutability: "view",
        inputs: [{name: "account", type: "address"}],
        outputs: [{name: "balance", type: "uint256"}],
    },
    {
        type: "function",
        name: "allowance",
        stateMutability: "view",
        inputs: [
            {name: "owner", type: "address"},
            {name: "spender", type: "address"},
        ],
        outputs: [{name: "remaining", type: "uint256"}],
    },
    {
        type: "function",
        name: "approve",
        stateMutability: "nonpayable",
        inputs: [
            {name: "spender", type: "address"},
            {name: "amount", type: "uint256"},
        ],
        outputs: [{name: "success", type: "bool"}],
    },
] as const;
