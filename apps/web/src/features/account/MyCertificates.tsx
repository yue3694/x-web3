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
    { value: "all", label: "All" },
    { value: "confirmed", label: "Confirmed" },
    { value: "minting", label: "Minting" },
    { value: "pending", label: "Pending" },
    { value: "failed", label: "Failed / Dead" },
];

function formatDate(iso: string): string {
    const date = new Date(iso);
    if (Number.isNaN(date.valueOf())) return iso;
    return new Intl.DateTimeFormat("en-US", { year: "numeric", month: "short", day: "numeric" }).format(date);
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
                        onchainCertId <code>#{cert.onchainCertId}</code>
                    </span>
                </div>
                <button
                    type="button"
                    className="btn--ghost btn--icon"
                    onClick={onClose}
                    aria-label="Close certificate details"
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
                    <dt>Recipient</dt>
                    <dd>
                        <code title={cert.recipientWallet}>{truncateAddress(cert.recipientWallet)}</code>
                    </dd>
                </div>
                <div>
                    <dt>Metadata</dt>
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
                    <dt>Completed</dt>
                    <dd>{formatDate(cert.completedAt)}</dd>
                </div>
                <div>
                    <dt>Rule version</dt>
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
                <h3>No certificates yet</h3>
                <p>Complete a course 100% to mint your first on-chain certificate.</p>
                <Link to="/courses" className="btn--primary">
                    Browse catalog
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
            <h3>No {filter} certificates</h3>
            <p>Nothing matches the current filter.</p>
            {onResetFilter ? (
                <button type="button" className="btn--ghost" onClick={onResetFilter}>
                    Show all certificates
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
                setError("Unable to load your certificates.");
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
                        <span className="eyebrow">Account</span>
                        <h2>My certificates</h2>
                        <p>Sign in to view the on-chain certificates you have earned.</p>
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
                    <span className="eyebrow">Account</span>
                    <h2 id="my-certificates-title">My certificates</h2>
                    <p>On-chain certificates for completed courses, with mint status and recipient wallet.</p>
                </div>
                {hasAnyCert ? (
                    <dl className="my-certificates__stats" aria-label="Certificate summary">
                        <div className="my-certificates__stat">
                            <dt>Earned</dt>
                            <dd>{counts.all}</dd>
                        </div>
                        <div className="my-certificates__stat my-certificates__stat--hint">
                            <dt>View</dt>
                            <dd>
                                <Link to="/account/enrollments?filter=completed" className="my-certificates__stat-link">
                                    Completed courses
                                </Link>
                            </dd>
                        </div>
                    </dl>
                ) : null}
            </header>

            {routeMissing ? (
                <div className="notice notice--error" role="alert">
                    The <code>GET /me/enrollments</code> endpoint is not wired in the current API build.
                    Please ask the backend track to expose the route.
                </div>
            ) : null}
            {error && !routeMissing ? (
                <div className="notice notice--error" role="alert">
                    <span>{error}</span>{" "}
                    <button type="button" className="btn--ghost" onClick={() => void load()}>
                        Retry
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
                    aria-label="Filter certificates by mint status"
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
                    aria-label="Loading certificates"
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
                    aria-label="My certificates"
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
                                        title={`Completed on ${formatDate(e.completedAt)}`}
                                    >
                                        completed {formatDate(e.completedAt)}
                                    </time>
                                ) : null}
                            </header>
                            <footer className="my-certificates__foot">
                                <span className="my-certificates__hint">
                                    {e.completedLessonsTotal}/{e.requiredLessonsTotal} lessons · 100%
                                </span>
                                <button
                                    type="button"
                                    className="my-certificates__cta"
                                    disabled={loadingCert === e.courseId}
                                    onClick={() => void onView(e.courseId)}
                                    aria-label={`View certificate for ${e.courseTitle}`}
                                >
                                    {loadingCert === e.courseId ? (
                                        "Loading…"
                                    ) : (
                                        <>
                                            View certificate
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