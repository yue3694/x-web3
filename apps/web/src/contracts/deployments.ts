// Filled in by hand after each deploy:
//   pnpm contracts:deploy:sepolia                 -> counterDeployments
//   pnpm contracts:deploy:notepad:sepolia         -> notepadDeployments
//   pnpm contracts:deploy:course-market:sepolia   -> courseMarketDeployments
//   pnpm contracts:deploy:yd-token:sepolia        -> ydTokenDeployments
//   pnpm contracts:deploy:certificate-nft:sepolia -> certificateNftDeployments
// The deploy script prints the address — paste it into the matching chain
// entry below. Address validation happens via optionalAddress() so missing
// addresses silently become undefined (UI should fall back to "not deployed").
import type {Address} from "viem";

import {TARGET_CHAIN_ID} from "@/chains";

function optionalAddress(value: string | undefined): Address | undefined {
    return value?.match(/^0x[0-9a-fA-F]{40}$/) ? (value as Address) : undefined;
}

export const counterDeployments = {
    sepolia: {
        address: optionalAddress(import.meta.env.VITE_COUNTER_CONTRACT_ADDRESS),
        chainId: 11155111,
    },
} as const;

export const notepadDeployments = {
    sepolia: {
        address: optionalAddress(import.meta.env.VITE_NOTEPAD_CONTRACT_ADDRESS),
        chainId: 11155111,
    },
} as const;

/**
 * CourseMarket — 课程链上购买入口（F03）。
 * 先部署市场，再用 COURSES_CONFIG_PATH 执行配置模式写入课程和支付 Token。
 * 地址由 VITE_TARGET_CHAIN_ID 所选的 Anvil 或 Sepolia 环境回填。
 */
export const courseMarketDeployments = {
    target: {
        address: optionalAddress(import.meta.env.VITE_COURSE_MARKET_ADDRESS),
        chainId: TARGET_CHAIN_ID,
    },
} as const;

/**
 * YDToken — 项目结算 ERC-20（F05）。
 * ADR-0002：cap=1B，initial supply=200M 部署到 treasury 多签。
 */
export const ydTokenDeployments = {
    target: {
        address: optionalAddress(import.meta.env.VITE_YD_TOKEN_ADDRESS),
        chainId: TARGET_CHAIN_ID,
    },
} as const;

/**
 * CertificateNFT — 学习完成证书（F04）。
 * soulbound：默认仅 MINTER_ROLE（Worker signer）可铸造。
 */
export const certificateNftDeployments = {
    target: {
        address: optionalAddress(import.meta.env.VITE_CERTIFICATE_NFT_ADDRESS),
        chainId: TARGET_CHAIN_ID,
    },
} as const;

/** Guarded Chainlink-compatible reference price adapter. */
export const priceOracleDeployments = {
    target: {
        address: optionalAddress(import.meta.env.VITE_PRICE_ORACLE_ADDRESS),
        chainId: TARGET_CHAIN_ID,
    },
} as const;
