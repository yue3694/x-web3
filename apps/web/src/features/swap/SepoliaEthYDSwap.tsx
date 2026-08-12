import {useMemo, useState} from "react";
import {formatEther, formatUnits, parseEther, type Address} from "viem";
import {useAccount, useBalance, useChainId, usePublicClient, useReadContract, useSwitchChain, useWriteContract} from "wagmi";

import {useNotify} from "@/components/NotifyProvider";
import {sepoliaYdSaleAbi} from "@/contracts/sepolia-yd-sale.abi";

const SEPOLIA_CHAIN_ID = 11_155_111;
const saleAddress = import.meta.env.VITE_SEPOLIA_YD_SALE_ADDRESS as Address | undefined;

export function SepoliaEthYDSwap() {
    const {address, isConnected} = useAccount();
    const chainId = useChainId();
    const {switchChainAsync} = useSwitchChain();
    const {writeContractAsync} = useWriteContract();
    const publicClient = usePublicClient({chainId: SEPOLIA_CHAIN_ID});
    const {notify} = useNotify();
    const [amount, setAmount] = useState("0.01");
    const [busy, setBusy] = useState(false);
    const amountIn = useMemo(() => {
        try { return parseEther(amount || "0"); } catch { return 0n; }
    }, [amount]);

    const balance = useBalance({address, chainId: SEPOLIA_CHAIN_ID});
    const quote = useReadContract({
        address: saleAddress,
        abi: sepoliaYdSaleAbi,
        functionName: "quote",
        args: [amountIn],
        chainId: SEPOLIA_CHAIN_ID,
        query: {enabled: Boolean(saleAddress && amountIn > 0n)},
    });
    const rate = useReadContract({
        address: saleAddress,
        abi: sepoliaYdSaleAbi,
        functionName: "ydPerEth",
        chainId: SEPOLIA_CHAIN_ID,
        query: {enabled: Boolean(saleAddress)},
    });

    const buy = async () => {
        setBusy(true);
        try {
            if (!address || !isConnected) throw new Error("请先连接 Sepolia 钱包。");
            if (!saleAddress || !publicClient) throw new Error("Sepolia YD 兑换合约尚未配置。");
            if (amountIn <= 0n) throw new Error("请输入有效的 SepoliaETH 数量。");
            if (chainId !== SEPOLIA_CHAIN_ID) await switchChainAsync({chainId: SEPOLIA_CHAIN_ID});
            if (balance.data && balance.data.value < amountIn) throw new Error("SepoliaETH 余额不足。");

            const hash = await writeContractAsync({
                address: saleAddress,
                abi: sepoliaYdSaleAbi,
                functionName: "buy",
                args: [address],
                value: amountIn,
                chainId: SEPOLIA_CHAIN_ID,
            });
            const receipt = await publicClient.waitForTransactionReceipt({hash});
            if (receipt.status !== "success") throw new Error("兑换交易已回滚。");
            notify(`兑换成功：${formatUnits(quote.data ?? 0n, 18)} YD 已到账。`, "success");
            await balance.refetch();
        } catch (cause) {
            notify(cause instanceof Error ? cause.message : "兑换失败。", "error");
        } finally {
            setBusy(false);
        }
    };

    return (
        <section className="swap-card panel" aria-labelledby="sepolia-swap-title">
            <header className="swap-card__header">
                <div className="swap-card__heading"><span className="eyebrow">Sepolia test exchange</span><h2 id="sepolia-swap-title">SepoliaETH → YD</h2></div>
                <span className="swap-card__protocol">仅支持 Ethereum Sepolia</span>
            </header>
            <div className="swap-card__grid">
                <div className="swap-card__main">
                    <label className="swap-card__field"><span>支付 SepoliaETH</span><input inputMode="decimal" value={amount} onChange={(event) => setAmount(event.target.value)} /></label>
                    <div className="swap-card__leg"><span>预计获得</span><strong className="swap-card__output">{quote.isLoading ? "报价中…" : `${formatUnits(quote.data ?? 0n, 18)} YD`}</strong></div>
                    <button className="btn--primary swap-card__submit" type="button" disabled={busy || amountIn <= 0n} onClick={() => void buy()}>{busy ? "等待 Sepolia 确认…" : isConnected ? `用 ${amount || "0"} SepoliaETH 兑换 YD` : "连接 Sepolia 钱包以兑换"}</button>
                </div>
                <aside className="swap-card__aside">
                    <dl className="swap-card__meta">
                        <div className="meta-row"><dt>钱包余额</dt><dd>{balance.data ? formatEther(balance.data.value) : "—"} SepoliaETH</dd></div>
                        <div className="meta-row"><dt>测试汇率</dt><dd>1 SepoliaETH = {formatUnits(rate.data ?? 0n, 18)} YD</dd></div>
                        <div className="meta-row"><dt>Chain ID</dt><dd>11155111</dd></div>
                    </dl>
                    <p className="muted">SepoliaETH 和这里的 YD 都是测试网资产，不具有真实货币价值。</p>
                </aside>
            </div>
        </section>
    );
}
