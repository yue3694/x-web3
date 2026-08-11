import {SwapCard} from "@/features/swap/SwapCard";
import {TARGET_CHAIN_NAME} from "@/chains";

export default function SwapPage() {
    return (
        <div className="page-stack page-stack--narrow">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">DeFi 兑换台</span>
                    <h1>为下一门课程准备 YD。</h1>
                    <p>在 {TARGET_CHAIN_NAME} 上兑换 YD 与 USDC，提供明确的报价、价格影响与滑点保护。</p>
                </div>
            </div>
            <section className="panel page-panel" aria-label="YD 代币兑换"><SwapCard /></section>
        </div>
    );
}
