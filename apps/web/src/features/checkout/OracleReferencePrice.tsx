import {formatUnits} from "viem";
import {useReadContract} from "wagmi";

import {TARGET_CHAIN_ID} from "@/chains";
import {priceOracleDeployments} from "@/contracts/deployments";
import {priceOracleAbi} from "@/contracts/price-oracle.abi";

export function OracleReferencePrice() {
    const address = priceOracleDeployments.target.address;
    const result = useReadContract({
        address,
        abi: priceOracleAbi,
        functionName: "latestPrice",
        chainId: TARGET_CHAIN_ID,
        query: {enabled: Boolean(address)},
    });

    if (!address) return null;
    if (result.isLoading) return <p className="muted">正在加载预言机参考价…</p>;
    if (result.error) {
        return <p className="notice notice--warn">预言机参考价不可用或已过期。</p>;
    }
    if (!result.data) return null;

    const [price, decimals, updatedAt] = result.data;
    return (
        <p className="muted" data-testid="oracle-reference-price">
            预言机参考：1 YD ≈ {formatUnits(price, decimals)} 美元 · 更新于 {new Date(Number(updatedAt) * 1000).toLocaleString("zh-CN")}
            {" "}（仅作参考，实际仍按冻结的 YD 数量结算）
        </p>
    );
}
