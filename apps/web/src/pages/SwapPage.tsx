import {SwapCard} from "@/features/swap/SwapCard";
import {TARGET_CHAIN_NAME} from "@/chains";

export default function SwapPage() {
    return (
        <div className="page-stack page-stack--narrow">
            <header className="page-hero">
                <span className="eyebrow">DeFi desk</span>
                <h1>Get YD for your next course.</h1>
                <p>Swap YD and USDC on {TARGET_CHAIN_NAME} with an explicit quote, price impact and slippage protection.</p>
            </header>
            <section className="panel page-panel" aria-label="YD token swap"><SwapCard /></section>
        </div>
    );
}
