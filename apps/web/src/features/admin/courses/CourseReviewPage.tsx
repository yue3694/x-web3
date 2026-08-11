import {useCallback, useEffect, useState} from "react";
import {getAddress} from "viem";
import {useAccount, useChainId, usePublicClient, useSwitchChain, useWriteContract} from "wagmi";

import {ApiClientError} from "@/api/client";
import {TARGET_CHAIN_ID, TARGET_CHAIN_NAME} from "@/chains";
import {courseMarketDeployments, ydTokenDeployments} from "@/contracts/deployments";
import {marketAbi} from "@/contracts/market.abi";
import {adminApi} from "@/features/admin/adminApi";
import type {AdminCourseReviewItem} from "@/features/admin/adminTypes";
import {courseKeyFromUuid} from "@/features/checkout/derive";

function priceMinorToYDWei(priceMinor: number): bigint {
    return BigInt(priceMinor) * 10n ** 16n;
}

export function CourseReviewPage() {
    const [items, setItems] = useState<AdminCourseReviewItem[]>([]);
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState<string | null>(null);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const {address, isConnected} = useAccount();
    const chainId = useChainId();
    const {switchChainAsync} = useSwitchChain();
    const {writeContractAsync} = useWriteContract();
    const publicClient = usePublicClient({chainId: TARGET_CHAIN_ID});

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const response = await adminApi.listCoursesForReview();
            setItems(response.items);
        } catch (cause) {
            setError(cause instanceof ApiClientError ? `${cause.code}: ${cause.message}` : "Unable to load review queue.");
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => { void load(); }, [load]);

    const approve = async (course: AdminCourseReviewItem) => {
        setBusy(course.id);
        setError("");
        setMessage("");
        try {
            if (course.priceMinor > 0) {
                if (!isConnected || !address) throw new Error("Connect the CourseMarket owner wallet first.");
                if (chainId !== TARGET_CHAIN_ID) await switchChainAsync({chainId: TARGET_CHAIN_ID});
                const marketAddress = courseMarketDeployments.target.address;
                const tokenAddress = ydTokenDeployments.target.address;
                if (!marketAddress || !tokenAddress || !publicClient) throw new Error("Local market, token or RPC configuration is missing.");

                const owner = await publicClient.readContract({address: marketAddress, abi: marketAbi, functionName: "owner"});
                if (getAddress(owner) !== getAddress(address)) {
                    throw new Error(`Publishing requires the CourseMarket owner wallet (${owner}).`);
                }
                const courseKey = await courseKeyFromUuid(course.id);
                const hash = await writeContractAsync({
                    address: marketAddress,
                    abi: marketAbi,
                    functionName: "configureCourse",
                    args: [courseKey, tokenAddress, priceMinorToYDWei(course.priceMinor), BigInt(course.currentVersion)],
                    chainId: TARGET_CHAIN_ID,
                });
                const receipt = await publicClient.waitForTransactionReceipt({hash});
                if (receipt.status !== "success") throw new Error("CourseMarket configuration reverted.");
            }

            await adminApi.reviewCourse(course.id, "approve");
            setMessage(`Published “${course.title}” on ${TARGET_CHAIN_NAME}.`);
            await load();
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "Could not publish the course.");
        } finally {
            setBusy(null);
        }
    };

    const reject = async (course: AdminCourseReviewItem) => {
        setBusy(course.id);
        setError("");
        setMessage("");
        try {
            await adminApi.reviewCourse(course.id, "reject", "Return to Studio for curriculum edits.");
            setMessage(`Returned “${course.title}” to draft. It can now be reopened in Studio.`);
            await load();
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "Could not return the course to draft.");
        } finally {
            setBusy(null);
        }
    };

    return (
        <section className="panel" aria-labelledby="course-review-title">
            <header className="section-heading">
                <div>
                    <span className="eyebrow">Admin · Courses</span>
                    <h2 id="course-review-title">Course review</h2>
                    <p>Publishing configures the paid course on {TARGET_CHAIN_NAME}, then opens it in the catalog.</p>
                </div>
            </header>
            {error ? <div className="notice notice--error" role="alert">{error}</div> : null}
            {message ? <div className="notice notice--ok" role="status">{message}</div> : null}
            {TARGET_CHAIN_ID === 31337 && !isConnected ? (
                <div className="notice notice--info" role="status">
                    Local publishing needs an injected browser wallet using the Anvil deployment account. A mobile WalletConnect/Reown wallet cannot reach your computer&apos;s localhost RPC.
                </div>
            ) : null}
            {loading ? <p className="muted" role="status">Loading review queue…</p> : null}
            {!loading && items.length === 0 ? <div className="empty-state"><span>◇</span><h3>Review queue is clear</h3><p>Submitted courses will appear here.</p></div> : null}
            <div className="page-stack">
                {items.map((course) => (
                    <article className="card" key={course.id}>
                        <span className="status-pill status-pill--pending_review">pending review</span>
                        <h3>{course.title}</h3>
                        <p>{course.description || "No description."}</p>
                        <p className="muted">{course.teacherName} · {(course.priceMinor / 100).toFixed(2)} {course.currency} · v{course.currentVersion}</p>
                        {course.lessonCount === 0 ? <div className="notice notice--warn">This version has no lessons. Return it to draft before publishing.</div> : null}
                        <button className="btn--primary" type="button" disabled={busy !== null || course.lessonCount === 0} onClick={() => void approve(course)}>
                            {busy === course.id ? "Publishing…" : `Publish on ${TARGET_CHAIN_NAME}`}
                        </button>
                        <button className="btn--ghost" type="button" disabled={busy !== null} onClick={() => void reject(course)}>
                            Return to draft
                        </button>
                    </article>
                ))}
            </div>
        </section>
    );
}

export default CourseReviewPage;
