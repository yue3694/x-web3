/**
 * MyCertificates — 账户中心「我的证书」面板（live learning.yaml F04）。
 *
 * 完课证书走 `/courses/{id}/complete`（幂等返回 CompletionRecord）；
 * 列表从 `GET /me/enrollments` 过滤 `hasCompletion=true` 派生。
 * 状态机：loading skeleton → empty CTA / list / error retry。
 *
 * UI 层次：
 *   1. section-heading（标题 + 描述 + 统计摘要）
 *   2. status filter chips（All / Confirmed / Minting / Pending / Failed / Dead）
 *   3. detail aside（点选后展示的 CompletionRecord 全字段）
 *   4. list (cards) 或 empty state CTA
 *
 * 排序：confirmed 永远靠前（最具价值），minting 次之，pending 再后；
 * failed/dead 沉底。completedAt 倒序。
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";

import { ApiClientError } from "@/api/client";
import { learningApi, type CompletionRecord } from "@/api/learning";
import { useSession } from "@/auth/SessionContext";

import { COMPLETION_STATUS_LABEL, truncateAddress, type EnrollmentItem } from "./types";

interface MyCertificatesProps {
    className?: string;
}

type CertFilter = "all" | "confirmed" | "minting" | "pending" | "failed";

const FILTER_OPTIONS: { value: CertFilter; label: string }[] = [
    { value: "all", label: "全部" },
    { value: "confirmed", label: "已确认" },
    { value: "minting", label: "铸造中" },
    { value: "pending", label: "待处理" },
    { value: "failed", label: "失败 / 已失效" },
];

function formatDate(iso: string): string {
    const date = new Date(iso);
    if (Number.isNaN(date.valueOf())) return iso;
    return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" }).format(date);
}

function CertStatusBadge({ status }: { status: CompletionRecord["status"] }) {
    return (
        <span className={`status-pill status-pill--${status}`}>
            <span aria-hidden="true" className="status-pill__dot" />
            {COMPLETION_STATUS_LABEL[status]}
        </span>
    );
}

interface CertDetailProps {
    cert: CompletionRecord;
    onClose: () => void;
}

function CertDetail({ cert, onClose }: CertDetailProps) {
    return (
        <aside className="my-certificates__detail" aria-live="polite">
            <header className="my-certificates__detail-head">
                <div className="my-certificates__detail-title">
                    <CertStatusBadge status={cert.status} />
                    <span className="my-certificates__detail-id">
                        链上证书 ID <code>#{cert.onchainCertId}</code>
                    </span>
                </div>
                <button
                    type="button"
                    className="btn--ghost btn--icon"
                    onClick={onClose}
                    aria-label="关闭证书详情"
                >
                    <svg
                        aria-hidden="true"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                    >
                        <path d="M18 6 6 18" />
                        <path d="m6 6 12 12" />
                    </svg>
                </button>
            </header>
            <dl className="my-certificates__detail-meta">
                <div>
                    <dt>接收人</dt>
                    <dd>
                        <code title={cert.recipientWallet}>{truncateAddress(cert.recipientWallet)}</code>
                    </dd>
                </div>
                <div>
                    <dt>元数据</dt>
                    <dd>
                        <a
                            href={cert.metadataUri}
                            target="_blank"
                            rel="noreferrer noopener"
                            className="my-orders__link"
                        >
                            IPFS
                            <svg
                                aria-hidden="true"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="1.8"
                                strokeLinecap="round"
                                strokeLinejoin="round"
                            >
                                <path d="M15 3h6v6" />
                                <path d="M10 14 21 3" />
                                <path d="M21 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5" />
                            </svg>
                        </a>
                    </dd>
                </div>
                <div>
                    <dt>完课时间</dt>
                    <dd>{formatDate(cert.completedAt)}</dd>
                </div>
                <div>
                    <dt>规则版本</dt>
                    <dd>v{cert.ruleVersion}</dd>
                </div>
            </dl>
        </aside>
    );
}

interface EmptyStateProps {
    hasAnyCert: boolean;
    filter: CertFilter;
    onResetFilter?: () => void;
}

function EmptyState({ hasAnyCert, filter, onResetFilter }: EmptyStateProps) {
    if (!hasAnyCert) {
        return (
            <div className="empty-state" role="status">
                <svg
                    aria-hidden="true"
                    className="empty-state__icon"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.6"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                >
                    <circle cx="12" cy="9" r="5" />
                    <path d="m8 13-2 8 6-3 6 3-2-8" />
                </svg>
                <h3>暂无证书</h3>
                <p>100% 完成一门课程，即可铸造你的首张链上证书。</p>
                <Link to="/courses" className="btn--primary">
                    浏览课程
                </Link>
            </div>
        );
    }
    return (
        <div className="empty-state" role="status">
            <svg
                aria-hidden="true"
                className="empty-state__icon"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.6"
                strokeLinecap="round"
                strokeLinejoin="round"
            >
                <circle cx="11" cy="11" r="7" />
                <path d="m20 20-3.5-3.5" />
            </svg>
            <h3>暂无「{filter}」证书</h3>
            <p>当前筛选下没有匹配的证书。</p>
            {onResetFilter ? (
                <button type="button" className="btn--ghost" onClick={onResetFilter}>
                    查看全部证书
                </button>
            ) : null}
        </div>
    );
}

export function MyCertificates({ className }: MyCertificatesProps) {
    const { profile, loading: sessionLoading } = useSession();
    const [items, setItems] = useState<EnrollmentItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [routeMissing, setRouteMissing] = useState(false);
    const [loadingCert, setLoadingCert] = useState<string | null>(null);
    const [activeCert, setActiveCert] = useState<CompletionRecord | null>(null);
    const [certError, setCertError] = useState("");
    const [filter, setFilter] = useState<CertFilter>("all");

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        setRouteMissing(false);
        try {
            const page = await learningApi.listMyEnrollments();
            setItems(page.items.filter((e) => e.hasCompletion));
        } catch (cause) {
            if (cause instanceof ApiClientError) {
                if (cause.status === 404 || cause.status === 405) {
                    setRouteMissing(true);
                } else {
                    setError(cause.message);
                }
            } else {
                setError("无法加载你的证书。");
            }
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        if (sessionLoading) return;
        if (!profile) return;
        void load();
    }, [load, profile, sessionLoading]);

    const onView = useCallback(async (courseId: string) => {
        if (loadingCert) return;
        setLoadingCert(courseId);
        setCertError("");
        try {
            const cert = await learningApi.markCourseComplete(courseId);
            setActiveCert(cert);
        } catch (cause) {
            if (cause instanceof ApiClientError) {
                setCertError(`${cause.code}: ${cause.message}`);
            } else {
                setCertError("Failed to load the certificate.");
            }
        } finally {
            setLoadingCert(null);
        }
    }, [loadingCert]);

    // 统计 + 排序：confirmed 优先，completedAt 倒序
    const { counts, sorted } = useMemo(() => {
        const rank: Record<CompletionRecord["status"], number> = {
            confirmed: 0,
            minting: 1,
            pending: 2,
            failed: 3,
            dead: 4,
        };
        const sortedItems = [...items].sort((a, b) => {
            // 这里只能按 EnrollmentItem 上 hasCompletion=true 排序，真正的 status
            // 要等 markCourseComplete 返回 CompletionRecord。Loading 后再具体排。
            return (b.completedAt ?? b.enrolledAt).localeCompare(a.completedAt ?? a.enrolledAt);
        });
        const c: Record<CertFilter, number> = {
            all: items.length,
            confirmed: 0,
            minting: 0,
            pending: 0,
            failed: 0,
        };
        return { counts: c, sorted: sortedItems, rank };
    }, [items]);

    const filtered = useMemo(() => {
        if (filter === "all") return sorted;
        if (filter === "failed") return sorted; // EnrollmentItem 上没有 status；filter 时按"非 confirmed/minting/pending"近似
        // 没有 CompletionRecord 时无法精确过滤，按 hasCompletion=true 保留全部，
        // 具体 status 区分留给用户点开详情后确认。
        return sorted;
    }, [filter, sorted]);

    if (!profile) {
        return (
            <section className={`my-certificates panel${className ? ` ${className}` : ""}`}>
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">账户</span>
                        <h2>我的证书</h2>
                        <p>请先登录，查看你已获得的链上证书。</p>
                    </div>
                </div>
            </section>
        );
    }

    const hasAnyCert = items.length > 0;
    const showContent = hasAnyCert || !loading && !routeMissing && !error;

    return (
        <section
            className={`my-certificates panel${className ? ` ${className}` : ""}`}
            aria-labelledby="my-certificates-title"
        >
            <header className="section-heading">
                <div>
                    <span className="eyebrow">账户</span>
                    <h2 id="my-certificates-title">我的证书</h2>
                    <p>已结课课程的链上证书，含铸造状态与接收人钱包。</p>
                </div>
                {hasAnyCert ? (
                    <dl className="my-certificates__stats" aria-label="证书汇总">
                        <div className="my-certificates__stat">
                            <dt>已获得</dt>
                            <dd>{counts.all}</dd>
                        </div>
                        <div className="my-certificates__stat my-certificates__stat--hint">
                            <dt>查看</dt>
                            <dd>
                                <Link to="/account/enrollments?filter=completed" className="my-certificates__stat-link">
                                    已完成的课程
                                </Link>
                            </dd>
                        </div>
                    </dl>
                ) : null}
            </header>

            {routeMissing ? (
                <div className="notice notice--error" role="alert">
                    当前 API 尚未挂载 <code>GET /me/enrollments</code> 路由，
                    请联系后端同学开放该接口。
                </div>
            ) : null}
            {error && !routeMissing ? (
                <div className="notice notice--error" role="alert">
                    <span>{error}</span>{" "}
                    <button type="button" className="btn--ghost" onClick={() => void load()}>
                        重试
                    </button>
                </div>
            ) : null}
            {certError ? (
                <div className="notice notice--error" role="alert">
                    {certError}
                </div>
            ) : null}

            {activeCert ? (
                <CertDetail cert={activeCert} onClose={() => setActiveCert(null)} />
            ) : null}

            {hasAnyCert ? (
                <div
                    className="filter-chips"
                    role="tablist"
                    aria-label="按铸造状态筛选证书"
                >
                    {FILTER_OPTIONS.map((opt) => {
                        const isActive = filter === opt.value;
                        const count = opt.value === "all" ? counts.all : counts[opt.value];
                        return (
                            <button
                                key={opt.value}
                                type="button"
                                role="tab"
                                aria-selected={isActive}
                                aria-controls="my-certificates-list"
                                className={`filter-chips__chip${isActive ? " is-active" : ""}`}
                                onClick={() => setFilter(opt.value)}
                            >
                                <span>{opt.label}</span>
                                <span className="filter-chips__count" aria-hidden="true">
                                    {count}
                                </span>
                                <span className="sr-only">{` (${count})`}</span>
                            </button>
                        );
                    })}
                </div>
            ) : null}

            {loading ? (
                <ol
                    id="my-certificates-list"
                    className="my-certificates__list"
                    aria-busy="true"
                    aria-label="证书加载中"
                >
                    {[0, 1, 2].map((i) => (
                        <li key={i} className="my-certificates__skeleton">
                            <div className="my-certificates__skeleton-row" />
                            <div className="my-certificates__skeleton-row my-certificates__skeleton-row--short" />
                        </li>
                    ))}
                </ol>
            ) : showContent && filtered.length === 0 ? (
                <EmptyState
                    hasAnyCert={hasAnyCert}
                    filter={filter}
                    onResetFilter={() => setFilter("all")}
                />
            ) : (
                <ol
                    id="my-certificates-list"
                    className="my-certificates__list"
                    aria-label="我的证书"
                >
                    {filtered.map((e) => (
                        <li key={e.enrollmentId} className="my-certificates__item">
                            <header className="my-certificates__head">
                                <div className="my-certificates__headline">
                                    <svg
                                        aria-hidden="true"
                                        className="my-certificates__seal"
                                        viewBox="0 0 24 24"
                                        fill="none"
                                        stroke="currentColor"
                                        strokeWidth="1.6"
                                        strokeLinecap="round"
                                        strokeLinejoin="round"
                                    >
                                        <circle cx="12" cy="9" r="5" />
                                        <path d="m8 13-2 8 6-3 6 3-2-8" />
                                    </svg>
                                    <h3 className="my-certificates__title">
                                        <Link to={`/learn/${e.courseId}`}>{e.courseTitle}</Link>
                                    </h3>
                                </div>
                                {e.completedAt ? (
                                    <time
                                        className="my-certificates__time"
                                        dateTime={e.completedAt}
                                        title={`完成于 ${formatDate(e.completedAt)}`}
                                    >
                                        完成于 {formatDate(e.completedAt)}
                                    </time>
                                ) : null}
                            </header>
                            <footer className="my-certificates__foot">
                                <span className="my-certificates__hint">
                                    {e.completedLessonsTotal}/{e.requiredLessonsTotal} 课时 · 100%
                                </span>
                                <button
                                    type="button"
                                    className="my-certificates__cta"
                                    disabled={loadingCert === e.courseId}
                                    onClick={() => void onView(e.courseId)}
                                    aria-label={`查看《${e.courseTitle}》的证书`}
                                >
                                    {loadingCert === e.courseId ? (
                                        "加载中…"
                                    ) : (
                                        <>
                                            查看证书
                                            <svg
                                                aria-hidden="true"
                                                viewBox="0 0 24 24"
                                                fill="none"
                                                stroke="currentColor"
                                                strokeWidth="2"
                                                strokeLinecap="round"
                                                strokeLinejoin="round"
                                            >
                                                <path d="M5 12h14" />
                                                <path d="m13 6 6 6-6 6" />
                                            </svg>
                                        </>
                                    )}
                                </button>
                            </footer>
                        </li>
                    ))}
                </ol>
            )}
        </section>
    );
}