/**
 * useSwapExecute — 封装 swap 的写链路（签名 → 广播 → 等回执 → 结算）。
 *
 * 与 useSwapQuote（读）对称：SwapCard 只负责编排与布局，
 * 交易生命周期这块的状态机放在这里。
 *
 * 状态：idle → signing → confirming → done | failed
 * （quoting 由 SwapCard 合并进来 —— 它属于读链路。）
 *
 * 注意：swap() 只能由用户交互调用，绝不在 useEffect 里触发。
 */

import {useCallback, useEffect, useRef, useState} from "react";
import {useAccount, usePublicClient, useWaitForTransactionReceipt, useWriteContract} from "wagmi";
import type {Address} from "viem";

import {swapRouterAbi} from "@/contracts/swap.abi";
import {erc20Abi} from "@/contracts/erc20.abi";

import {DEFAULT_FEE_TIER, NO_PRICE_LIMIT, TARGET_CHAIN_ID, swapRouterAddress} from "./swapConfig";
import {extractReceivedAmount, isUserRejected, normalizeError} from "./swapErrors";
import {deadlineFromNow} from "./swapUtils";
import type {SwapSettlement, SwapState} from "./swapTypes";

interface ExecuteArgs {
  tokenIn: Address;
  tokenOut: Address;
  recipient: Address;
  amountIn: bigint;
  minAmountOut: bigint;
  deadlineMins: number;
}

interface UseSwapExecuteResult {
  /** 写链路状态；不含 quoting。 */
  state: Exclude<SwapState, "quoting">;
  error: string | null;
  settlement: SwapSettlement | null;
  swap: (args: ExecuteArgs) => Promise<void>;
  reset: () => void;
  setError: (msg: string | null) => void;
}

export function useSwapExecute(
  onSuccess?: (settlement: SwapSettlement) => void,
): UseSwapExecuteResult {
  const {writeContractAsync} = useWriteContract();
  const {address} = useAccount();
  const publicClient = usePublicClient({chainId: TARGET_CHAIN_ID});

  const [state, setState] = useState<Exclude<SwapState, "quoting">>("idle");
  const [error, setError] = useState<string | null>(null);
  const [txHash, setTxHash] = useState<`0x${string}` | null>(null);
  const [settlement, setSettlement] = useState<SwapSettlement | null>(null);
  const onSuccessRef = useRef(onSuccess);
  onSuccessRef.current = onSuccess;
  // 回执解析需要这两个值，但它们属于「提交那一刻」的快照 —— 不能读当前表单，
  // 否则用户在确认期间改了 token/滑点会算错实际到账。
  const [pending, setPending] = useState<{tokenOut: Address; recipient: Address; min: bigint} | null>(
    null,
  );

  const receipt = useWaitForTransactionReceipt({
    hash: txHash ?? undefined,
    chainId: TARGET_CHAIN_ID,
  });

  useEffect(() => {
    if (state !== "confirming" || !receipt.data || !txHash || !pending) return;
    const done: SwapSettlement = {
      txHash,
      minReceived: pending.min,
      actualReceived: extractReceivedAmount(receipt.data.logs, pending.tokenOut, pending.recipient),
    };
    setSettlement(done);
    setState("done");
    onSuccessRef.current?.(done);
  }, [state, receipt.data, txHash, pending]);

  useEffect(() => {
    if (state === "confirming" && receipt.error) {
      setState("failed");
      setError(receipt.error.message);
    }
  }, [state, receipt.error]);

  const swap = useCallback(
    async (args: ExecuteArgs) => {
      setError(null);
      if (!swapRouterAddress) {
        setError("Swap router is not configured on the target chain yet.");
        return;
      }
      if (!address || !publicClient) {
        setError("Wallet or RPC client is unavailable.");
        return;
      }
      setPending({tokenOut: args.tokenOut, recipient: args.recipient, min: args.minAmountOut});
      try {
        setState("checking");
        const [balance, allowance] = await Promise.all([
          publicClient.readContract({
            address: args.tokenIn,
            abi: erc20Abi,
            functionName: "balanceOf",
            args: [address],
          }),
          publicClient.readContract({
            address: args.tokenIn,
            abi: erc20Abi,
            functionName: "allowance",
            args: [address, swapRouterAddress],
          }),
        ]);
        if (balance < args.amountIn) {
          throw new Error("Insufficient token balance for this swap");
        }
        if (allowance < args.amountIn) {
          setState("approving");
          const approvalHash = await writeContractAsync({
            address: args.tokenIn,
            abi: erc20Abi,
            functionName: "approve",
            args: [swapRouterAddress, args.amountIn],
            chainId: TARGET_CHAIN_ID,
          });
          const approvalReceipt = await publicClient.waitForTransactionReceipt({hash: approvalHash});
          if (approvalReceipt.status !== "success") {
            throw new Error("Token approval transaction reverted");
          }
        }

        setState("signing");
        const hash = await writeContractAsync({
          address: swapRouterAddress,
          abi: swapRouterAbi,
          functionName: "exactInputSingle",
          args: [
            {
              tokenIn: args.tokenIn,
              tokenOut: args.tokenOut,
              fee: DEFAULT_FEE_TIER,
              recipient: args.recipient,
              deadline: deadlineFromNow(args.deadlineMins),
              amountIn: args.amountIn,
              amountOutMinimum: args.minAmountOut,
              sqrtPriceLimitX96: NO_PRICE_LIMIT,
            },
          ],
          chainId: TARGET_CHAIN_ID,
        });
        setTxHash(hash);
        setState("confirming");
      } catch (cause) {
        // 用户主动取消不是错误：回到 idle，允许直接再点一次。
        if (isUserRejected(cause)) {
          setState("idle");
          setError("User rejected");
          return;
        }
        setState("failed");
        setError(normalizeError(cause).message);
      }
    },
    [address, publicClient, writeContractAsync],
  );

  const reset = useCallback(() => {
    setState("idle");
    setError(null);
    setTxHash(null);
    setSettlement(null);
    setPending(null);
  }, []);

  return {state, error, settlement, swap, reset, setError};
}
