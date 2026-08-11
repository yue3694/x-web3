export const priceOracleAbi = [
    {
        type: "function",
        name: "latestPrice",
        stateMutability: "view",
        inputs: [],
        outputs: [
            {name: "price", type: "uint256"},
            {name: "decimals", type: "uint8"},
            {name: "updatedAt", type: "uint256"},
        ],
    },
] as const;
