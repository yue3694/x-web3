/**
 * MyEnrollments — 账户中心「我的报名」面板。
 *
 * 行为契约（live learning.yaml F04）：
 *   - 拉取 GET /me/enrollments?limit=；行：courseTitle / completionPct 进度条 /
 *     hasCompletion（completed / in_progress）/ enrolledAt / 跳到 /learn/{courseId}；
 *   - 状态机：loading skeleton → empty CTA / list / error retry；
 *   - limit 分页（默认 50、上限 50），无 cursor；
 *   - 后端 404/405 走"API 暂未上线"降级提示。
 *
 * UI 层次：
 *   1. section-heading（eyebrow + 标题 + 描述 + 计数）
 *   2. stats summary（in progress / completed / total）
 *   3. filter chips（All / In progress / Completed）
 *   4. list (cards) 或 empty state CTA
 *
 * 排序：在读永远靠前（按 enrolledAt 倒序），已结课沉底（按 completedAt 倒序）。
 * 这样打开页面就能看见下一步要学什么。
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";

import { ApiClientError } from "@/api/client";
import { learningApi } from "@/api/learning";
import { useSession } from "@/auth/SessionContext";

import type { EnrollmentItem } from "./types";

interface MyEnrollmentsProps {
    className?: string;
}

type Filter = "all" | "in-progress" | "completed";

const FILTER_OPTIONS: { value: Filter; label: string }[] = [
    { value: "all", label: "全部" },
    { value: "in-progress", label: "学习中" },
    { value: "completed", label: "已完成" },
];

function formatDate(iso: string): string {
    const date = new Date(iso);
    if (Number.isNaN(date.valueOf())) return iso;
    return new Intl.DateTimeFormat("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
    }).format(date);
}

/** 进度状态文案 —— 不止 0/100 两个端点，给中间态更细的反馈。 */
function progressLabel(pct: number, hasCompletion: boolean): string {
    if (hasCompletion) return "已完成";
    if (pct === 0) return "尚未开始";
    if (pct < 50) return "刚刚开始";
    if (pct < 100) return "学习中";
    return "即将完成";
}

function StatusBadge({ item }: { item: EnrollmentItem }) {
    const variant = item.hasCompletion ? "completed" : "in-progress";
    return (
        <span className={`status-pill status-pill--${variant}`}>
            <span aria-hidden="true" className="status-pill__dot" />
            {progressLabel(item.completionPct, item.hasCompletion)}
        </span>
    );
}

interface EmptyStateProps {
    filter: Filter;
}

/** 三种空态：从未报名 / 全部已结课被切走 / 当前 filter 命中 0 条。 */
function EmptyState({ filter }: EmptyStateProps) {
    if (filter === "completed") {
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
                    <path d="M12 2 4 6v6c0 4.5 3.2 8.6 8 10 4.8-1.4 8-5.5 8-10V6l-8-4Z" />
                    <path d="m9 12 2 2 4-4" />
                </svg>
                <h3>暂无已结课的课程</h3>
                <p>完成一门正在学习的课程，即可解锁你的链上证书。</p>
                <Link to="/account/enrollments?filter=in-progress" className="btn--ghost">
                    查看学习中
                </Link>
            </div>
        );
    }
    if (filter === "in-progress") {
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
                    <circle cx="12" cy="12" r="9" />
                    <path d="M12 7v5l3 2" />
                </svg>
                <h3>全部学完啦</h3>
                <p>当前在读的课程都已完成。看看课程市场，挑下一门感兴趣的课。</p>
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
                <path d="M4 5h12a4 4 0 0 1 4 4v10H8a4 4 0 0 1-4-4V5Z" />
                <path d="M4 5v10a4 4 0 0 0 4 4" />
                <path d="M9 9h6M9 13h4" />
            </svg>
            <h3>暂无报名记录</h3>
            <p>浏览课程目录并购买一门课程，开启你的学习之旅。</p>
            <Link to="/courses" className="btn--primary">
                浏览课程
            </Link>
        </div>
    );
}

export function MyEnrollments({ className }: MyEnrollmentsProps) {
    const { profile, loading: sessionLoading } = useSession();
    const [items, setItems] = useState<EnrollmentItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [routeMissing, setRouteMissing] = useState(false);
    const [filter, setFilter] = useState<Filter>("all");

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

    // 派生：分类计数 + 排序（in-progress 永远在读，completed 沉底）
    const { counts, sorted } = useMemo(() => {
        const inProgress = items.filter((i) => !i.hasCompletion);
        const completed = items.filter((i) => i.hasCompletion);
        const sortedItems: EnrollmentItem[] = [
            ...inProgress.sort((a, b) => b.enrolledAt.localeCompare(a.enrolledAt)),
            ...completed.sort((a, b) =>
                (b.completedAt ?? b.enrolledAt).localeCompare(a.completedAt ?? a.enrolledAt),
            ),
        ];
        return {
            counts: {
                all: items.length,
                "in-progress": inProgress.length,
                completed: completed.length,
            },
            sorted: sortedItems,
        };
    }, [items]);

    const filtered = useMemo(
        () => (filter === "all" ? sorted : sorted.filter((i) => (filter === "completed" ? i.hasCompletion : !i.hasCompletion))),
        [filter, sorted],
    );

    if (!profile) {
        return (
            <section className={`my-enrollments panel${className ? ` ${className}` : ""}`}>
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">账户</span>
                        <h2>我的报名</h2>
                        <p>请先登录，跟踪你的课程进度。</p>
                    </div>
                </div>
            </section>
        );
    }

    const hasItems = items.length > 0;
    const showContent = hasItems || !loading && !routeMissing && !error;

    return (
        <section
            className={`my-enrollments panel${className ? ` ${className}` : ""}`}
            aria-labelledby="my-enrollments-title"
        >
            <header className="section-heading">
                <div>
                    <span className="eyebrow">账户</span>
                    <h2 id="my-enrollments-title">我的报名</h2>
                    <p>你已购买的课程，含观看进度与完课状态。</p>
                </div>
                {hasItems ? (
                    <dl
                        className="my-enrollments__stats"
                        aria-label="报名汇总"
                    >
                        <div className="my-enrollments__stat my-enrollments__stat--progress">
                            <dt>学习中</dt>
                            <dd>{counts["in-progress"]}</dd>
                        </div>
                        <div className="my-enrollments__stat my-enrollments__stat--done">
                            <dt>已完成</dt>
                            <dd>{counts.completed}</dd>
                        </div>
                        <div className="my-enrollments__stat">
                            <dt>合计</dt>
                            <dd>{counts.all}</dd>
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

            {hasItems ? (
                <div
                    className="filter-chips"
                    role="tablist"
                    aria-label="按状态筛选报名"
                >
                    {FILTER_OPTIONS.map((opt) => {
                        const isActive = filter === opt.value;
                        const count = counts[opt.value];
                        return (
                            <button
                                key={opt.value}
                                type="button"
                                role="tab"
                                aria-selected={isActive}
                                aria-controls="my-enrollments-list"
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
                    id="my-enrollments-list"
                    className="my-enrollments__list"
                    aria-busy="true"
                    aria-label="报名加载中"
                >
                    {[0, 1, 2].map((i) => (
                        <li key={i} className="my-enrollments__skeleton">
                            <div className="my-enrollments__skeleton-row" />
                            <div className="my-enrollments__skeleton-bar" />
                        </li>
                    ))}
                </ol>
            ) : showContent && filtered.length === 0 ? (
                <EmptyState filter={filter} />
            ) : (
                <ol
                    id="my-enrollments-list"
                    className="my-enrollments__list"
                    aria-label="我的报名"
                >
                    {filtered.map((e) => {
                        const variant = e.hasCompletion ? "completed" : "in-progress";
                        return (
                            <li
                                key={e.enrollmentId}
                                className={`my-enrollments__item my-enrollments__item--${variant}`}
                            >
                                <header className="my-enrollments__head">
                                    <div className="my-enrollments__headline">
                                        <StatusBadge item={e} />
                                        <h3 className="my-enrollments__title">
                                            <Link to={`/learn/${e.courseId}`}>
                                                {e.courseTitle}
                                            </Link>
                                        </h3>
                                    </div>
                                    <time
                                        className="my-enrollments__time"
                                        dateTime={e.enrolledAt}
                                        title={`报名于 ${formatDate(e.enrolledAt)}`}
                                    >
                                        报名于 {formatDate(e.enrolledAt)}
                                    </time>
                                </header>

                                <div
                                    className="my-enrollments__progress"
                                    role="progressbar"
                                    aria-valuemin={0}
                                    aria-valuemax={100}
                                    aria-valuenow={e.completionPct}
                                    aria-label={`进度：${e.completionPct}%`}
                                >
                                    <div className="my-enrollments__progress-track">
                                        <div
                                            className="my-enrollments__progress-bar"
                                            style={{ width: `${Math.min(100, Math.max(0, e.completionPct))}%` }}
                                        />
                                    </div>
                                    <span className="my-enrollments__progress-meta">
                                        <span className="my-enrollments__progress-count">
                                            {e.completedLessonsTotal}
                                            <span className="muted">/{e.requiredLessonsTotal}</span>
                                        </span>
                                        <span className="my-enrollments__progress-pct">
                                            {e.completionPct}%
                                        </span>
                                    </span>
                                </div>

                                <footer className="my-enrollments__foot">
                                    {e.hasCompletion && e.completedAt ? (
                                        <span className="my-enrollments__completed">
                                            <svg
                                                aria-hidden="true"
                                                viewBox="0 0 24 24"
                                                fill="none"
                                                stroke="currentColor"
                                                strokeWidth="1.8"
                                                strokeLinecap="round"
                                                strokeLinejoin="round"
                                            >
                                                <path d="M20 6 9 17l-5-5" />
                                            </svg>
                                            已完成 {formatDate(e.completedAt)}
                                        </span>
                                    ) : (
                                        <span className="my-enrollments__hint">
                                            {progressLabel(e.completionPct, e.hasCompletion)} · 继续加油
                                        </span>
                                    )}
                                    <Link
                                        to={`/learn/${e.courseId}`}
                                        className="my-enrollments__cta"
                                        aria-label={
                                            e.hasCompletion
                                                ? `回顾课程《${e.courseTitle}》`
                                                : `继续学习《${e.courseTitle}》`
                                        }
                                    >
                                        {e.hasCompletion ? "回顾课程" : "继续学习"}
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
                                    </Link>
                                </footer>
                            </li>
                        );
                    })}
                </ol>
            )}
        </section>
    );
}