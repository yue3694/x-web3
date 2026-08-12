import {useCallback, useEffect, useState, type FormEvent} from "react";
import {Link, useSearchParams} from "react-router-dom";
import {ApiClientError} from "../../api/client";
import {courseApi, type Course} from "../../api/types";

export function formatCoursePrice(course: Pick<Course, "priceMinor" | "currency">): string {
    if (course.priceMinor === 0) return "免费";
    return new Intl.NumberFormat("zh-CN", {style: "currency", currency: course.currency}).format(course.priceMinor / 100);
}

/** 课时总时长转成人类可读文案；不足 1 小时只显示分钟，避免出现「0.0 小时」。 */
export function formatCourseDuration(durationSeconds: number): string {
    if (durationSeconds <= 0) return "";
    const minutes = Math.round(durationSeconds / 60);
    if (minutes < 60) return `${minutes} 分钟`;
    return `${(minutes / 60).toFixed(1).replace(/\.0$/, "")} 小时`;
}

/** 卡片底部的「N 章 · M 课时 · X 小时」；后端未返回聚合值时静默省略该段。 */
export function formatCourseStats(course: Course): string {
    const parts: string[] = [];
    if (course.chapterCount) parts.push(`${course.chapterCount} 章`);
    if (course.lessonCount) parts.push(`${course.lessonCount} 课时`);
    const duration = formatCourseDuration(course.durationSeconds ?? 0);
    if (duration) parts.push(duration);
    return parts.join(" · ");
}

const ART_VARIANTS = 3;

export function CourseCatalog() {
    const [items, setItems] = useState<Course[]>([]);
    const [searchParams, setSearchParams] = useSearchParams();
    const [query, setQuery] = useState(searchParams.get("q") ?? "");
    const appliedQuery = searchParams.get("q") ?? "";
    const [nextCursor, setNextCursor] = useState("");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");

    const load = useCallback(async (before?: string) => {
        setLoading(true);
        setError("");
        try {
            const page = await courseApi.list({q: appliedQuery, before, limit: 9});
            setItems((current) => before ? [...current, ...page.items] : page.items);
            setNextCursor(page.nextCursor);
        } catch (cause) {
            setError(cause instanceof ApiClientError ? cause.message : "无法加载课程列表。");
        } finally {
            setLoading(false);
        }
    }, [appliedQuery]);

    useEffect(() => { void load(); }, [load]);

    const submitSearch = (event: FormEvent) => {
        event.preventDefault();
        const next = new URLSearchParams(searchParams);
        const trimmed = query.trim();
        if (trimmed) next.set("q", trimmed); else next.delete("q");
        setSearchParams(next);
    };

    const clearSearch = () => {
        setQuery("");
        const next = new URLSearchParams(searchParams);
        next.delete("q");
        setSearchParams(next);
    };

    const initialLoading = loading && items.length === 0;
    const showEmpty = !loading && !error && items.length === 0;

    return (
        <section className="catalog panel" aria-labelledby="catalog-title">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">探索协议</span>
                    <h2 id="catalog-title">已发布的课程</h2>
                    <p>跟随经过认证的大学讲师，学习链上技能。</p>
                </div>
                <form className="catalog-search" role="search" onSubmit={submitSearch}>
                    <label className="sr-only" htmlFor="course-search">搜索课程</label>
                    <input id="course-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索课程..." />
                    <button className="btn--primary" type="submit">搜索</button>
                </form>
            </div>

            {appliedQuery ? (
                <div className="catalog-filters">
                    <span className="filter-chip">
                        关键词：{appliedQuery}
                        <button type="button" onClick={clearSearch} aria-label="清除搜索条件">×</button>
                    </span>
                    {!loading ? <span className="catalog-count">{items.length} 门课程{nextCursor ? "+" : ""}</span> : null}
                </div>
            ) : null}

            {error ? <div className="notice notice--error" role="alert">{error}</div> : null}

            {initialLoading ? (
                <div className="course-grid" aria-label="课程加载中" aria-busy="true">
                    {[0, 1, 2, 3, 4, 5].map((item) => <div className="course-skeleton" key={item} />)}
                </div>
            ) : null}

            {showEmpty ? (
                <div className="empty-state">
                    <span aria-hidden="true">◇</span>
                    <h3>{appliedQuery ? "没有匹配的课程" : "暂无已发布课程"}</h3>
                    <p>{appliedQuery ? "换个关键词，或清除筛选查看全部课程。" : "新课程审核完成后将出现在这里。"}</p>
                    {appliedQuery ? <button className="btn--ghost" type="button" onClick={clearSearch}>清除筛选</button> : null}
                </div>
            ) : null}

            {items.length > 0 ? (
                <ul className="course-grid" role="list">
                    {items.map((course, index) => {
                        const stats = formatCourseStats(course);
                        const isFree = course.priceMinor === 0;
                        return (
                            <li key={course.id}>
                                <Link className="course-card course-card--clickable" to={`/courses/${course.id}`} aria-label={`打开课程《${course.title}》`}>
                                    <div className={`course-card__art course-card__art--${index % ART_VARIANTS}`}>
                                        <span aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
                                        <span className={`price-tag${isFree ? " price-tag--free" : ""}`}>{formatCoursePrice(course)}</span>
                                    </div>
                                    <div className="course-card__body">
                                        <h3>{course.title}</h3>
                                        <p>{course.description || "课程大纲稍后公布。"}</p>
                                        <div className="course-card__spacer" />
                                        <div className="course-card__teacher">
                                            <span className="avatar" aria-hidden="true">{(course.teacherName || "讲").slice(0, 1)}</span>
                                            <span className="course-card__teacher-name">{course.teacherName || "大学讲师"}</span>
                                        </div>
                                        {stats ? <div className="course-card__footer">{stats}</div> : null}
                                    </div>
                                </Link>
                            </li>
                        );
                    })}
                </ul>
            ) : null}

            {nextCursor ? (
                <div className="load-more">
                    <button className="btn--ghost" disabled={loading} onClick={() => void load(nextCursor)}>
                        {loading ? "加载中..." : "加载更多"}
                    </button>
                </div>
            ) : null}
        </section>
    );
}
