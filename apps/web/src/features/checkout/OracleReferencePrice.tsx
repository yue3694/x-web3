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
    if (result.isLoading) return <p className="muted">Loading oracle reference price…</p>;
    if (result.error) {
        return <p className="notice notice--warn">Oracle reference unavailable or stale.</p>;
    }
    if (!result.data) return null;

    const [price, decimals, updatedAt] = result.data;
    return (
        <p className="muted" data-testid="oracle-reference-price">
            Oracle reference: 1 YD ≈ {formatUnits(price, decimals)} USD · updated {new Date(Number(updatedAt) * 1000).toLocaleString()}
            {" "}(reference only; settlement remains the frozen YD amount)
        </p>
    );
}
