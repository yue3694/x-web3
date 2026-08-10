// Filled in by hand after each deploy:
//   pnpm contracts:deploy:sepolia                -> counterDeployments
//   pnpm contracts:deploy:notepad:sepolia         -> notepadDeployments
//   pnpm contracts:deploy:course-market:sepolia   -> courseMarketDeployments
//   pnpm contracts:deploy:yd-token:sepolia        -> ydTokenDeployments
//   pnpm contracts:deploy:certificate-nft:sepolia -> certificateNftDeployments
// The deploy script prints the address — paste it into the matching chain
// entry below. Address validation happens via optionalAddress() so missing
// addresses silently become undefined (UI should fall back to "not deployed").
import type {Address} from "viem";

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
 * 部署后用 PAYMENT_TOKEN_ADDRESS / COURSES_CONFIG_PATH 配置课程。
 * v1 部署槽位占位；Sepolia 上线后回填。
 */
export const courseMarketDeployments = {
    sepolia: {
        address: optionalAddress(import.meta.env.VITE_COURSE_MARKET_ADDRESS),
        chainId: 11155111,
    },
} as const;

/**
 * YDToken — 项目结算 ERC-20（F05）。
 * ADR-0002：cap=1B，initial supply=200M 部署到 treasury 多签。
 */
export const ydTokenDeployments = {
    sepolia: {
        address: optionalAddress(import.meta.env.VITE_YD_TOKEN_ADDRESS),
        chainId: 11155111,
    },
} as const;

/**
 * CertificateNFT — 学习完成证书（F04）。
 * soulbound：默认仅 MINTER_ROLE（Worker signer）可铸造。
 */
export const certificateNftDeployments = {
    sepolia: {
        address: optionalAddress(import.meta.env.VITE_CERTIFICATE_NFT_ADDRESS),
        chainId: 11155111,
    },
} as const;