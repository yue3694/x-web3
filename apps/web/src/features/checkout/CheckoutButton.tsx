/**
 * CheckoutButton — F03 课程购买主按钮。
 *
 * 状态机：idle → preparing → checking → approving? → signing → confirming → done | failed
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

import {useEffect, useRef, useState} from "react";
import {useAccount, useChainId, usePublicClient, useSwitchChain, useWaitForTransactionReceipt, useWriteContract} from "wagmi";
import {getAddress} from "viem";

import {apiClient} from "@/api/client";
import {TARGET_CHAIN_ID, TARGET_CHAIN_NAME} from "@/chains";
import {erc20Abi} from "@/contracts/erc20.abi";
import {marketAbi} from "@/contracts/market.abi";
import {courseMarketDeployments} from "@/contracts/deployments";
import {useNotify} from "@/components/NotifyProvider";

import {uuidToBytes16} from "./derive";
import {buttonLabel, isUserRejected, normalizeError} from "./checkoutUtils";
import type {CheckoutContextProps, CheckoutState, OrderTransactionAck, PurchaseIntent} from "./checkoutTypes";

export function CheckoutButton({
    courseId,
    priceYD,
    courseKey,
    walletId,
    generateIdempotencyKey,
    onSuccess,
    disabled: externallyDisabled = false,
}: CheckoutContextProps & {disabled?: boolean}) {
    const {isConnected, address} = useAccount();
    const chainId = useChainId();
    const {switchChain, isPending: isSwitching} = useSwitchChain();
    const {writeContractAsync} = useWriteContract();
    const publicClient = usePublicClient({chainId: TARGET_CHAIN_ID});
    const {notify} = useNotify();

    const [state, setState] = useState<CheckoutState>("idle");
    const [error, setError] = useState<string | null>(null);
    const [intent, setIntent] = useState<PurchaseIntent | null>(null);
    const [txHash, setTxHash] = useState<`0x${string}` | null>(null);
    const onSuccessRef = useRef(onSuccess);
    onSuccessRef.current = onSuccess;

    const marketAddress = courseMarketDeployments.target.address;
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
                onSuccessRef.current?.(ack.onchainTxHash ?? txHash);
            } catch (cause) {
                if (cancelled) return;
                setState("failed");
                setError(normalizeError(cause).message);
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [state, receipt.data, intent, txHash]);

    useEffect(() => {
        if (state !== "confirming") return;
        if (receipt.error) {
            setState("failed");
            setError(receipt.error.message);
        }
    }, [state, receipt.error]);

    useEffect(() => {
        if (error) notify(error, "error");
    }, [error, notify]);

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
            setError("请先连接钱包");
            return;
        }
        if (onWrongChain) {
            setError(`请切换到 ${TARGET_CHAIN_NAME} 后继续。`);
            return;
        }
        if (!marketAddress) {
            setError(`${TARGET_CHAIN_NAME} 上尚未配置 Market 合约。`);
            return;
        }
        if (!publicClient) {
            setError(`${TARGET_CHAIN_NAME} 的 RPC 客户端不可用。`);
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

            if (fresh.chainId !== TARGET_CHAIN_ID) {
                throw new Error(`Intent chain ${fresh.chainId} does not match ${TARGET_CHAIN_ID}`);
            }
            if (fresh.courseKey.toLowerCase() !== courseKey.toLowerCase()) {
                throw new Error("Intent courseKey does not match the selected course");
            }
            if (getAddress(fresh.marketAddress) !== getAddress(marketAddress)) {
                throw new Error("Intent market address does not match the configured market");
            }

            const expectedAmount = BigInt(fresh.amount);
            const tokenAddress = getAddress(fresh.tokenAddress);

            setState("checking");
            const [balance, allowance] = await Promise.all([
                publicClient.readContract({
                    address: tokenAddress,
                    abi: erc20Abi,
                    functionName: "balanceOf",
                    args: [address],
                }),
                publicClient.readContract({
                    address: tokenAddress,
                    abi: erc20Abi,
                    functionName: "allowance",
                    args: [address, marketAddress],
                }),
            ]);
            if (balance < expectedAmount) {
                throw new Error("YD 余额不足以完成本次购买");
            }
            if (allowance < expectedAmount) {
                setState("approving");
                const approvalHash = await writeContractAsync({
                    address: tokenAddress,
                    abi: erc20Abi,
                    functionName: "approve",
                    args: [marketAddress, expectedAmount],
                    chainId: TARGET_CHAIN_ID,
                });
                const approvalReceipt = await publicClient.waitForTransactionReceipt({
                    hash: approvalHash,
                });
                if (approvalReceipt.status !== "success") {
                    throw new Error("YD 授权交易已回滚");
                }
            }

            setState("signing");
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
                setError("已取消");
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
        externallyDisabled ||
        isSwitching ||
        state === "preparing" ||
        state === "checking" ||
        state === "approving" ||
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
                aria-label={`切换到 ${TARGET_CHAIN_NAME}`}
            >
                {isSwitching ? "切换中…" : `切换到 ${TARGET_CHAIN_NAME}`}
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

        </div>
    );
}
