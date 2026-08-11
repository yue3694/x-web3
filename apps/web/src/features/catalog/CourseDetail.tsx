/**
 * CourseDetail — 独立课程详情页。
 *
 * 设计：
 *   - 由 CourseCatalog 卡片进入独立、可分享的详情路由；
 *   - 调 /courses/{id} 拿课程 + 章节；enrolled=true 时才显示受保护章节占位；
 *   - 详情内嵌入 <Comments>，由评论组件自己处理登录 / 已购买 / 软删；
 *   - 未 enrolled 时把 <CheckoutPanel> 嵌在 curriculum 之下，购买成功
 *     （订单 submitted 后）跳转到 My Enrollments，形成明确的购买闭环。
 *
 * 注意：当前后端 catalog.Get 返回不带 enrolled 字段（OpenAPI 中有但 Go handler
 * 未透传）；此处按 OpenAPI contract 解析，缺失时退化为 false。
 */

import {useCallback, useEffect, useState} from "react";
import {Link, useNavigate} from "react-router-dom";

import {ApiClientError} from "@/api/client";
import {courseApi, type CourseDetail as CourseDetailData} from "@/api/types";
import {useSession} from "@/auth/SessionContext";
import {courseMarketDeployments} from "@/contracts/deployments";

import {CheckoutPanel} from "@/features/checkout/CheckoutPanel";
import {courseKeyFromUuid} from "@/features/checkout/derive";

import {Comments} from "./Comments";

interface CourseDetailProps {
    courseId: string;
}

type LoadedDetail = CourseDetailData;

/** 1 YD = 10^18 wei；USD ↔ YD 1:1 占位（OQ-004 决议后再切换价格预言）。 */
function priceMinorToYDWei(priceMinor: number): string {
    if (!Number.isFinite(priceMinor) || priceMinor <= 0) return "0";
    // priceMinor 是 cents；1 USD = 100 cents = 1 YD = 10^18 wei
    // → YD wei = (priceMinor / 100) * 10^18 = priceMinor * 10^16
    const wei = BigInt(priceMinor) * 10n ** 16n;
    return wei.toString();
}

export function CourseDetail({courseId}: CourseDetailProps) {
    const navigate = useNavigate();
    const {profile, loading: sessionLoading} = useSession();
    const [data, setData] = useState<LoadedDetail | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [courseKey, setCourseKey] = useState<`0x${string}` | null>(null);
    const [keyError, setKeyError] = useState<string | null>(null);

    useEffect(() => {
        let cancelled = false;
        setLoading(true);
        setError("");
        (async () => {
            try {
                const raw = await courseApi.get(courseId);
                if (!cancelled) {
                    // 后端 detail 暂未带 enrolled 字段；用 as 兼容 OpenAPI 形态。
                    setData(raw as LoadedDetail);
                }
            } catch (cause) {
                if (!cancelled) {
                    setError(cause instanceof ApiClientError ? cause.message : "Unable to load course detail.");
                }
            } finally {
                if (!cancelled) setLoading(false);
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [courseId]);

    // 计算课程链上 key（sha256(uuid)）。
    useEffect(() => {
        let cancelled = false;
        (async () => {
            try {
                const key = await courseKeyFromUuid(courseId);
                if (!cancelled) setCourseKey(key);
            } catch (cause) {
                if (!cancelled) {
                    setKeyError(cause instanceof Error ? cause.message : "courseKey derivation failed");
                }
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [courseId]);

    const generateIdempotencyKey = useCallback(() => {
        // 32 hex chars from window.crypto.randomUUID + a fallback for SSR/test env.
        const id = typeof globalThis.crypto?.randomUUID === "function"
            ? globalThis.crypto.randomUUID()
            : Math.random().toString(36).slice(2);
        return id.replace(/-/g, "");
    }, []);

    const course = data?.course;
    const enrolled = data?.enrolled === true;

    // wallet 选择：primary wallet 必须与 chain 匹配；否则从同一 chain 的 wallet 里挑第一个。
    const marketChainId = courseMarketDeployments.target.chainId;
    const walletForChain = profile?.wallets.find((w) => w.chainId === marketChainId) ?? null;

    const priceYD = course ? priceMinorToYDWei(course.priceMinor) : "0";
    const marketAddress = courseMarketDeployments.target.address;

    return (
        <div className="course-detail-page page-stack" aria-labelledby="course-detail-title">
            <Link className="back-link" to="/courses">← Back to courses</Link>
            <article className="course-detail__panel panel">

                {loading ? <p className="muted">Loading course…</p> : null}
                {error ? <div className="notice notice--error" role="alert">{error}</div> : null}

                {course ? (
                    <>
                        <header className="course-detail__header">
                            <span className="eyebrow">Course</span>
                            <h2 id="course-detail-title">{course.title}</h2>
                            <p className="muted">
                                {course.teacherName || "University faculty"} · v{course.currentVersion}
                                {enrolled ? <span className="status-pill status-pill--published">Enrolled</span> : null}
                            </p>
                        </header>

                        <section className="course-detail__about">
                            <h3>About this course</h3>
                            <p>{course.description || "Course curriculum details coming soon."}</p>
                        </section>

                        <section className="course-detail__chapters">
                            <h3>Curriculum</h3>
                            {data?.chapters.length ? (
                                <ol className="course-detail__chapter-list">
                                    {data.chapters.map((chapter) => (
                                        <li key={chapter.id} className="course-detail__chapter">
                                            <strong>{chapter.position}. {chapter.title}</strong>
                                            <ul>
                                                {chapter.lessons.map((lesson) => (
                                                    <li key={lesson.id}>
                                                        {lesson.position}. {lesson.title}
                                                        {lesson.required ? <span className="muted"> · required</span> : null}
                                                    </li>
                                                ))}
                                            </ul>
                                        </li>
                                    ))}
                                </ol>
                            ) : (
                                <p className="muted">No chapters published yet.</p>
                            )}
                        </section>

                        {!enrolled ? (
                            sessionLoading ? (
                                <p className="muted" role="status">Loading session…</p>
                            ) : !profile ? (
                                <div className="notice notice--info" role="status">
                                    Sign in to purchase this course.
                                </div>
                            ) : !walletForChain ? (
                                <div className="notice notice--warn" role="alert">
                                    No wallet bound for chain {marketChainId}.{" "}
                                    Open your account menu and link one before buying.
                                </div>
                            ) : !marketAddress ? (
                                <div className="notice notice--warn" role="alert">
                                    CourseMarket contract is not deployed on Sepolia yet.
                                </div>
                            ) : !courseKey || keyError ? (
                                <div className="notice notice--error" role="alert">
                                    {keyError ?? "Failed to derive courseKey."}
                                </div>
                            ) : course.priceMinor <= 0 ? (
                                <div className="notice notice--info" role="status">
                                    This course is free; no checkout required.
                                </div>
                            ) : (
                                <CheckoutPanel
                                    courseId={course.id}
                                    courseTitle={course.title}
                                    priceYD={priceYD}
                                    courseKey={courseKey}
                                    walletId={walletForChain.id}
                                    generateIdempotencyKey={generateIdempotencyKey}
                                    onSuccess={(hash) => {
                                        void hash;
                                        navigate("/account/enrollments");
                                    }}
                                />
                            )
                        ) : (
                            <div className="notice notice--ok" role="status">
                                You are enrolled. Open the course player from My Enrollments.
                            </div>
                        )}

                        <Comments courseId={course.id} courseTitle={course.title} />
                    </>
                ) : null}
            </article>
        </div>
    );
}
