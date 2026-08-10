/**
 * MyCertificates — 账户中心「我的证书」面板（live learning.yaml F04）。
 *
 * 完课证书走 `/courses/{id}/complete`（幂等返回 CompletionRecord）；
 * 列表从 `GET /me/enrollments` 过滤 `hasCompletion=true` 派生。
 * 状态机：loading skeleton → empty CTA / list / error retry。
 */

import {useCallback, useEffect, useState} from "react";

import {ApiClientError} from "@/api/client";
import {learningApi, type CompletionRecord} from "@/api/learning";
import {useSession} from "@/auth/SessionContext";

import {COMPLETION_STATUS_LABEL, truncateAddress, type EnrollmentItem} from "./types";

interface MyCertificatesProps {
    className?: string;
}

function formatDate(iso: string): string {
    const date = new Date(iso);
    if (Number.isNaN(date.valueOf())) return iso;
    return new Intl.DateTimeFormat("en-US", {year: "numeric", month: "short", day: "numeric"}).format(date);
}

function CertStatusBadge({status}: {status: CompletionRecord["status"]}) {
    return <span className={`status-pill status-pill--${status}`}>{COMPLETION_STATUS_LABEL[status]}</span>;
}

export function MyCertificates({className}: MyCertificatesProps) {
    const {profile, loading: sessionLoading} = useSession();
    const [items, setItems] = useState<EnrollmentItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [routeMissing, setRouteMissing] = useState(false);
    const [loadingCert, setLoadingCert] = useState<string | null>(null);
    const [activeCert, setActiveCert] = useState<CompletionRecord | null>(null);
    const [certError, setCertError] = useState("");

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

    return (
        <section
            className={`my-certificates panel${className ? ` ${className}` : ""}`}
            aria-labelledby="my-certificates-title"
        >
            <div className="section-heading">
                <div>
                    <span className="eyebrow">Account</span>
                    <h2 id="my-certificates-title">My certificates</h2>
                    <p>On-chain certificates for completed courses, with mint status and recipient wallet.</p>
                </div>
            </div>

            {routeMissing ? (
                <div className="notice notice--error" role="alert">
                    The <code>GET /me/enrollments</code> endpoint is not wired in the current API build.
                    Please ask the backend track to expose the route.
                </div>
            ) : null}
            {error && !routeMissing ? (
                <div className="notice notice--error" role="alert">
                    {error}{" "}
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
                <aside className="my-certificates__detail" aria-live="polite">
                    <header>
                        <CertStatusBadge status={activeCert.status} />
                        <span className="muted">onchainCertId #{activeCert.onchainCertId}</span>
                    </header>
                    <dl>
                        <div>
                            <dt>Recipient</dt>
                            <dd><code title={activeCert.recipientWallet}>{truncateAddress(activeCert.recipientWallet)}</code></dd>
                        </div>
                        <div>
                            <dt>Metadata</dt>
                            <dd><a href={activeCert.metadataUri} target="_blank" rel="noreferrer noopener">IPFS</a></dd>
                        </div>
                        <div>
                            <dt>Completed</dt>
                            <dd>{formatDate(activeCert.completedAt)}</dd>
                        </div>
                    </dl>
                </aside>
            ) : null}
            {loading ? (
                <ol className="my-certificates__list" aria-busy="true" aria-label="Loading certificates">
                    {[0, 1, 2].map((i) => (
                        <li key={i} className="my-certificates__skeleton" />
                    ))}
                </ol>
            ) : items.length === 0 && !routeMissing && !error ? (
                <div className="empty-state">
                    <span>◇</span>
                    <h3>No certificates yet</h3>
                    <p>Complete a course 100% to mint your first on-chain certificate.</p>
                </div>
            ) : (
                <ol className="my-certificates__list" aria-label="My certificates">
                    {items.map((e) => (
                        <li key={e.enrollmentId} className="my-certificates__item">
                            <header>
                                <div>
                                    <strong className="my-certificates__title">{e.courseTitle}</strong>
                                </div>
                                {e.completedAt ? (
                                    <time className="my-certificates__time" dateTime={e.completedAt}>
                                        completed {formatDate(e.completedAt)}
                                    </time>
                                ) : null}
                            </header>
                            <footer className="my-certificates__foot">
                                <button
                                    type="button"
                                    className="btn--ghost"
                                    disabled={loadingCert === e.courseId}
                                    onClick={() => void onView(e.courseId)}
                                >
                                    {loadingCert === e.courseId ? "Loading…" : "View certificate"}
                                </button>
                                <button type="button" className="btn--ghost" disabled>
                                    Share
                                </button>
                            </footer>
                        </li>
                    ))}
                </ol>
            )}
        </section>
    );
}
