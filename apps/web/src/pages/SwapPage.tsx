import {SepoliaEthYDSwap} from "@/features/swap/SepoliaEthYDSwap";

export default function SwapPage() {
    return (
        <div className="swap-workspace">
            <header className="swap-workspace__intro">
                <div>
                    <span className="eyebrow">DeFi 兑换台</span>
                    <h1>为下一门课程准备 YD。</h1>
                    <p>
                        连接 Ethereum Sepolia 钱包，使用 SepoliaETH 兑换课程支付所需的测试 YD。
                    </p>
                </div>
                <ul className="swap-workspace__bullets" aria-label="兑换要点">
                    <li>
                        <span aria-hidden="true">·</span>
                        <span>唯一兑换方向 · SepoliaETH → YD</span>
                    </li>
                    <li>
                        <span aria-hidden="true">·</span>
                        <span>仅供 Sepolia 测试，不代表市场价格</span>
                    </li>
                    <li>
                        <span aria-hidden="true">·</span>
                        <span>本地签名，不会代扣授权外资产</span>
                    </li>
                </ul>
            </header>

            <SepoliaEthYDSwap />
        </div>
    );
}
