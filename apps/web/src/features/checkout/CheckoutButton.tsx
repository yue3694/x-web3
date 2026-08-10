/**
 * CheckoutButton — F03 课程购买主按钮。
 *
 * 状态机：idle → preparing → signing → confirming → done | failed
 *
 * 流程：
 *   1. 校验 chainId（错链就调 useSwitchChain 提示切链）。
 *   2. POST /purchase-intents 拿后端签发的 intent（携带冻结的 amount + courseKey + intentId）。
 *   3. useWriteContract 调用 CourseMarket.buyCourse(courseKey, expectedAmount, intentId)。
 *      expectedAmount 与 intentId 由合约二次校验防 price tampering / 重复 intent。
 *   4. 拿到 txHash 后 useWaitForTransactionReceipt 等待确认。
 *   5. POST /orders/{intentId}/transactions 上报 txHash。
 *   6. 回调 onSuccess(txHash)；后端推进 orders.status=submitting；worker 拉事件后
 *      推到 confirmed + 派生 enrollment。
 *
 * 注意：useWriteContract 不会在 useEffect 里调用；只有点击按钮才触发。
 * 错误归一与文案见 checkoutUtils.ts。
 */

import {useEffect, useState} from "react";
import {sepolia} from "wagmi/chains";
import {useAccount, useChainId, useSwitchChain, useWaitForTransactionReceipt, useWriteContract} from "wagmi";

import {apiClient} from "@/api/client";
import {marketAbi} from "@/contracts/market.abi";
import {courseMarketDeployments} from "@/contracts/deployments";

import {uuidToBytes16} from "./derive";
import {buttonLabel, isUserRejected, normalizeError} from "./checkoutUtils";
import type {CheckoutContextProps, CheckoutState, OrderTransactionAck, PurchaseIntent} from "./checkoutTypes";

const TARGET_CHAIN_ID = sepolia.id;

export function CheckoutButton({
    courseId,
    priceYD,
    courseKey,
    recipient: _recipient,
    walletId,
    generateIdempotencyKey,
    onSuccess,
}: CheckoutContextProps) {
    const {isConnected, address} = useAccount();
    const chainId = useChainId();
    const {switchChain, isPending: isSwitching} = useSwitchChain();
    const {writeContractAsync} = useWriteContract();

    const [state, setState] = useState<CheckoutState>("idle");
    const [error, setError] = useState<string | null>(null);
    const [intent, setIntent] = useState<PurchaseIntent | null>(null);
    const [txHash, setTxHash] = useState<`0x${string}` | null>(null);

    const marketAddress = courseMarketDeployments.sepolia.address;
    const onWrongChain = isConnected && chainId !== TARGET_CHAIN_ID;

    const receipt = useWaitForTransactionReceipt({
        hash: txHash ?? undefined,
        chainId: TARGET_CHAIN_ID,
    });

    useEffect(() => {
        if (state !== "confirming" || !receipt.data || !intent || !txHash) return;
        let cancelled = false;
        (async () => {
            try {
                const ack = await apiClient.post<OrderTransactionAck>(
                    `/orders/${intent.id}/transactions`,
                    {txHash, chainId: TARGET_CHAIN_ID},
                );
                if (cancelled) return;
                setState("done");
                onSuccess?.(ack.onchainTxHash ?? txHash);
            } catch (cause) {
                if (cancelled) return;
                setState("failed");
                setError(normalizeError(cause).message);
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [state, receipt.data, intent, txHash, onSuccess]);

    useEffect(() => {
        if (state !== "confirming") return;
        if (receipt.error) {
            setState("failed");
            setError(receipt.error.message);
        }
    }, [state, receipt.error]);

    const onSwitch = () => {
        try {
            switchChain({chainId: TARGET_CHAIN_ID});
        } catch (cause) {
            setError(normalizeError(cause).message);
        }
    };

    const onClick = async () => {
        setError(null);
        if (!isConnected || !address) {
            setError("Connect a wallet first");
            return;
        }
        if (onWrongChain) {
            setError("Switch to Sepolia to continue.");
            return;
        }
        if (!marketAddress) {
            setError("Market contract not deployed on Sepolia yet.");
            return;
        }

        setState("preparing");
        try {
            const fresh = await apiClient.post<PurchaseIntent>("/orders/purchase-intents", {
                courseId,
                chainId: TARGET_CHAIN_ID,
                walletId,
                idempotencyKey: generateIdempotencyKey(),
            });
            setIntent(fresh);

            setState("signing");
            // expectedAmount 用 intent.amount（API 在 CreateIntent 时已按 course_prices 锁定）；
            // intentId 取 purchase_intents.id 的高 128 位 big-endian，对齐合约 bytes16。
            const expectedAmount = BigInt(fresh.amount);
            const intentIdBytes16 = uuidToBytes16(fresh.id);
            const hash = await writeContractAsync({
                address: marketAddress,
                abi: marketAbi,
                functionName: "buyCourse",
                args: [courseKey, expectedAmount, intentIdBytes16],
                chainId: TARGET_CHAIN_ID,
            });
            setTxHash(hash);
            setState("confirming");
        } catch (cause) {
            if (isUserRejected(cause)) {
                setState("idle");
                setError("User rejected");
                setIntent(null);
                setTxHash(null);
                return;
            }
            setState("failed");
            setError(normalizeError(cause).message);
        }
    };

    const label = buttonLabel({
        isConnected,
        onWrongChain,
        isSwitching,
        state,
        priceYD,
        receiptLoading: receipt.isLoading,
    });

    const disabled =
        !isConnected ||
        isSwitching ||
        state === "preparing" ||
        state === "signing" ||
        state === "confirming" ||
        state === "done";

    return (
        <div className="checkout-button">
            <button
                type="button"
                className="btn--primary"
                onClick={onSwitch}
                hidden={!onWrongChain}
                disabled={isSwitching}
                aria-label="Switch network to Sepolia"
            >
                {isSwitching ? "Switching…" : "Switch to Sepolia"}
            </button>

            <button
                type="button"
                className="btn--primary"
                onClick={onClick}
                disabled={disabled}
                aria-busy={state !== "idle" && state !== "done" && state !== "failed"}
                data-state={state}
                hidden={onWrongChain}
            >
                {label}
            </button>

            {error ? (
                <p className="checkout-button__error" role="alert">{error}</p>
            ) : null}
        </div>
    );
}
