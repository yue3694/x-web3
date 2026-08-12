import {SwapCard} from "@/features/swap/SwapCard";
import {TARGET_CHAIN_NAME} from "@/chains";

export default function SwapPage() {
    return (
        <div className="swap-workspace">
            <header className="swap-workspace__intro">
                <div>
                    <span className="eyebrow">DeFi 兑换台</span>
                    <h1>为下一门课程准备 YD。</h1>
                    <p>
                        在 {TARGET_CHAIN_NAME} 上兑换 YD 与 USDC，提供明确的报价、价格影响与滑点保护。
                    </p>
                </div>
                <ul className="swap-workspace__bullets" aria-label="兑换要点">
                    <li>
                        <span aria-hidden="true">·</span>
                        <span>Uniswap V3 · 0.3% 池路由</span>
                    </li>
                    <li>
                        <span aria-hidden="true">·</span>
                        <span>价格影响阈值 10%，超过自动拦截</span>
                    </li>
                    <li>
                        <span aria-hidden="true">·</span>
                        <span>本地签名，不会代扣授权外资产</span>
                    </li>
                </ul>
            </header>

            <SwapCard />
        </div>
    );
}
