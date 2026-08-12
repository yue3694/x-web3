import {useCallback, useEffect, useState} from "react";
import {getAddress} from "viem";
import {useAccount, useChainId, usePublicClient, useSwitchChain, useWriteContract} from "wagmi";

import {ApiClientError} from "@/api/client";
import {TARGET_CHAIN_ID, TARGET_CHAIN_NAME} from "@/chains";
import {useNotify} from "@/components/NotifyProvider";
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
    const {notify} = useNotify();

    useEffect(() => { if (error) notify(error, "error"); }, [error, notify]);
    useEffect(() => { if (message) notify(message, "success"); }, [message, notify]);

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const response = await adminApi.listCoursesForReview();
            setItems(response.items);
        } catch (cause) {
            setError(cause instanceof ApiClientError ? `${cause.code}: ${cause.message}` : "无法加载审核队列。");
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
                if (!isConnected || !address) throw new Error("请先连接 CourseMarket 所有者钱包。");
                if (chainId !== TARGET_CHAIN_ID) await switchChainAsync({chainId: TARGET_CHAIN_ID});
                const marketAddress = courseMarketDeployments.target.address;
                const tokenAddress = ydTokenDeployments.target.address;
                if (!marketAddress || !tokenAddress || !publicClient) throw new Error("本地缺少 Market、代币或 RPC 配置。");

                const owner = await publicClient.readContract({address: marketAddress, abi: marketAbi, functionName: "owner"});
                if (getAddress(owner) !== getAddress(address)) {
                    throw new Error(`发布需要使用 CourseMarket 所有者钱包（${owner}）。`);
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
                if (receipt.status !== "success") throw new Error("CourseMarket 配置交易已回滚。");
            }

            await adminApi.reviewCourse(course.id, "approve");
            setMessage(`已在 ${TARGET_CHAIN_NAME} 上发布 "${course.title}"。`);
            await load();
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "课程发布失败。");
        } finally {
            setBusy(null);
        }
    };

    const reject = async (course: AdminCourseReviewItem) => {
        setBusy(course.id);
        setError("");
        setMessage("");
        try {
            await adminApi.reviewCourse(course.id, "reject", "退回 Studio 修改课程内容。");
            setMessage(`已将 "${course.title}" 退回草稿，可在 Studio 中继续编辑。`);
            await load();
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "退回课程草稿失败。");
        } finally {
            setBusy(null);
        }
    };

    return (
        <section className="panel" aria-labelledby="course-review-title">
            <header className="section-heading">
                <div>
                    <span className="eyebrow">管理 · 课程</span>
                    <h2 id="course-review-title">课程审核</h2>
                    <p>发布流程会先在 {TARGET_CHAIN_NAME} 上配置付费课程，然后再上架到课程市场。</p>
                </div>
            </header>
            {loading ? <p className="muted" role="status">正在加载审核队列…</p> : null}
            {!loading && items.length === 0 ? <div className="empty-state"><span>◇</span><h3>审核队列已清空</h3><p>新提交的课程会出现在这里。</p></div> : null}
            <div className="page-stack">
                {items.map((course) => (
                    <article className="card" key={course.id}>
                        <span className="status-pill status-pill--pending_review">待审核</span>
                        <h3>{course.title}</h3>
                        <p>{course.description || "暂无简介。"}</p>
                        <p className="muted">{course.teacherName} · {(course.priceMinor / 100).toFixed(2)} {course.currency} · v{course.currentVersion}</p>
                        {course.lessonCount === 0 ? <div className="notice notice--warn">该版本暂无课时，请先退回草稿后再发布。</div> : null}
                        <button className="btn--primary" type="button" disabled={busy !== null || course.lessonCount === 0} onClick={() => void approve(course)}>
                            {busy === course.id ? "正在发布…" : `在 ${TARGET_CHAIN_NAME} 上发布`}
                        </button>
                        <button className="btn--ghost" type="button" disabled={busy !== null} onClick={() => void reject(course)}>
                            退回草稿
                        </button>
                    </article>
                ))}
            </div>
        </section>
    );
}

export default CourseReviewPage;
