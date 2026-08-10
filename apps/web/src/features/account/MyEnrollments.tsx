/**
 * MyEnrollments — 账户中心「我的报名」面板。
 *
 * 行为契约（live learning.yaml F04）：
 *   - 拉取 GET /me/enrollments?limit=；行：courseTitle / completionPct 进度条 /
 *     hasCompletion（completed / in_progress）/ enrolledAt / 跳到 /learn/{courseId}；
 *   - 状态机：loading skeleton → empty CTA / list / error retry；
 *   - limit 分页（默认 50、上限 50），无 cursor；
 *   - 后端 404/405 走"API 暂未上线"降级提示。
 */

import {useCallback, useEffect, useState} from "react";

import {ApiClientError} from "@/api/client";
import {learningApi} from "@/api/learning";
import {useSession} from "@/auth/SessionContext";

import type {EnrollmentItem} from "./types";

interface MyEnrollmentsProps {
    className?: string;
}

function formatDate(iso: string): string {
    const date = new Date(iso);
    if (Number.isNaN(date.valueOf())) return iso;
    return new Intl.DateTimeFormat("en-US", {
        year: "numeric",
        month: "short",
        day: "numeric",
    }).format(date);
}

function StatusBadge({item}: {item: EnrollmentItem}) {
    const done = item.hasCompletion;
    return (
        <span className={`status-pill status-pill--${done ? "confirmed" : "pending"}`}>
            {done ? "completed" : "in progress"}
        </span>
    );
}

export function MyEnrollments({className}: MyEnrollmentsProps) {
    const {profile, loading: sessionLoading} = useSession();
    const [items, setItems] = useState<EnrollmentItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [routeMissing, setRouteMissing] = useState(false);

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        setRouteMissing(false);
        try {
            const page = await learningApi.listMyEnrollments();
            setItems(page.items);
        } catch (cause) {
            if (cause instanceof ApiClientError) {
                if (cause.status === 404 || cause.status === 405) {
                    setRouteMissing(true);
                } else {
                    setError(cause.message);
                }
            } else {
                setError("Unable to load your enrollments.");
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

    if (!profile) {
        return (
            <section className={`my-enrollments panel${className ? ` ${className}` : ""}`}>
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">Account</span>
                        <h2>My enrollments</h2>
                        <p>Sign in to track your course progress.</p>
                    </div>
                </div>
            </section>
        );
    }

    return (
        <section className={`my-enrollments panel${className ? ` ${className}` : ""}`} aria-labelledby="my-enrollments-title">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">Account</span>
                    <h2 id="my-enrollments-title">My enrollments</h2>
                    <p>Courses you have purchased, with watch progress and completion status.</p>
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

            {loading ? (
                <ol className="my-enrollments__list" aria-busy="true" aria-label="Loading enrollments">
                    {[0, 1, 2].map((i) => (
                        <li key={i} className="my-enrollments__skeleton" />
                    ))}
                </ol>
            ) : items.length === 0 && !routeMissing && !error ? (
                <div className="empty-state">
                    <span>◇</span>
                    <h3>No enrollments yet</h3>
                    <p>Browse the catalog and purchase a course to start learning.</p>
                </div>
            ) : (
                <ol className="my-enrollments__list" aria-label="My enrollments">
                    {items.map((e) => (
                        <li
                            key={e.enrollmentId}
                            className={`my-enrollments__item my-enrollments__item--${e.hasCompletion ? "completed" : "in-progress"}`}
                        >
                            <header>
                                <div>
                                    <StatusBadge item={e} />
                                    <strong className="my-enrollments__title">{e.courseTitle}</strong>
                                </div>
                                <time className="my-enrollments__time" dateTime={e.enrolledAt}>
                                    enrolled {formatDate(e.enrolledAt)}
                                </time>
                            </header>
                            <div
                                className="my-enrollments__progress"
                                role="progressbar"
                                aria-valuemin={0}
                                aria-valuemax={100}
                                aria-valuenow={e.completionPct}
                            >
                                <div className="my-enrollments__bar" style={{width: `${e.completionPct}%`}} />
                                <span className="my-enrollments__pct">
                                    {e.completedLessonsTotal}/{e.requiredLessonsTotal} · {e.completionPct}%
                                </span>
                            </div>
                            <footer className="my-enrollments__foot">
                                <a className="btn--ghost" href={`/learn/${e.courseId}`}>
                                    {e.hasCompletion ? "Review course" : "Continue learning"}
                                </a>
                            </footer>
                        </li>
                    ))}
                </ol>
            )}
        </section>
    );
}
