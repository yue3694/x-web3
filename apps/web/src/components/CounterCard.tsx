import {useEffect} from "react";
import {
    useAccount,
    useReadContract,
    useWaitForTransactionReceipt,
    useWriteContract,
} from "wagmi";

import {counterAbi} from "../contracts/counter.abi";
import {counterDeployments} from "../contracts/deployments";

export function CounterCard() {
    const {isConnected, chainId} = useAccount();

    const sepoliaDeployment = counterDeployments.sepolia;
    const isOnSepolia = chainId === sepoliaDeployment.chainId;
    const hasAddress = Boolean(sepoliaDeployment.address);

    const {data: count, refetch} = useReadContract({
        abi: counterAbi,
        address: sepoliaDeployment.address,
        functionName: "count",
        chainId: sepoliaDeployment.chainId,
        query: {enabled: hasAddress},
    });

    const {data: hash, writeContract, isPending, error} = useWriteContract();

    const {isLoading: isConfirming, isSuccess: isConfirmed} =
        useWaitForTransactionReceipt({hash});

    // Refresh counter once the tx is mined.
    useEffect(() => {
        if (isConfirmed) refetch();
    }, [isConfirmed, refetch]);

    if (!hasAddress) {
        return (
            <article className="card">
                <h2>Counter</h2>
                <p className="muted">
                    No deployment address found. Run
                    <code> pnpm contracts:deploy:sepolia </code>
                    and update <code>apps/web/src/contracts/deployments.ts</code>.
                </p>
            </article>
        );
    }

    if (!isConnected) {
        return (
            <article className="card">
                <h2>Counter</h2>
                <p className="muted">Connect a wallet to interact.</p>
            </article>
        );
    }

    if (!isOnSepolia) {
        return (
            <article className="card">
                <h2>Counter</h2>
                <p className="warn">
                    Wrong network. Switch to <strong>Sepolia</strong> in your
                    wallet.
                </p>
            </article>
        );
    }

    return (
        <article className="card">
            <h2>Counter</h2>
            <p className="count">{count?.toString() ?? "—"}</p>
            <div className="actions">
                <button
                    type="button"
                    disabled={isPending || isConfirming}
                    onClick={() =>
                        writeContract({
                            abi: counterAbi,
                            address: sepoliaDeployment.address,
                            functionName: "increment",
                            chainId: sepoliaDeployment.chainId,
                        })
                    }
                >
                    {isPending ? "Confirm in wallet…" : "+1"}
                </button>
                <button
                    type="button"
                    disabled={isPending || isConfirming}
                    onClick={() =>
                        writeContract({
                            abi: counterAbi,
                            address: sepoliaDeployment.address,
                            functionName: "decrement",
                            chainId: sepoliaDeployment.chainId,
                        })
                    }
                >
                    {isPending ? "Confirm in wallet…" : "−1"}
                </button>
            </div>
            {hash && (
                <p className="muted">
                    Tx:{" "}
                    <a
                        href={`https://sepolia.etherscan.io/tx/${hash}`}
                        target="_blank"
                        rel="noreferrer"
                    >
                        {hash.slice(0, 10)}…
                    </a>{" "}
                    {isConfirming && "(mining)"}
                    {isConfirmed && "(✓)"}
                </p>
            )}
            {error && <p className="error">{error.message}</p>}
        </article>
    );
}