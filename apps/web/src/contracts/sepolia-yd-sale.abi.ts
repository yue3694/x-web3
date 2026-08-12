export const sepoliaYdSaleAbi = [
    {type: "function", name: "quote", stateMutability: "view", inputs: [{name: "sepoliaEthAmount", type: "uint256"}], outputs: [{name: "ydOut", type: "uint256"}]},
    {type: "function", name: "ydPerEth", stateMutability: "view", inputs: [], outputs: [{name: "", type: "uint256"}]},
    {type: "function", name: "buy", stateMutability: "payable", inputs: [{name: "recipient", type: "address"}], outputs: [{name: "ydOut", type: "uint256"}]},
] as const;
