import {useCallback, useEffect, useState} from "react";
import {Link, useSearchParams} from "react-router-dom";
import {ApiClientError} from "../../api/client";
import {courseApi, type Course} from "../../api/types";

export function formatCoursePrice(course: Pick<Course, "priceMinor" | "currency">): string {
    if (course.priceMinor === 0) return "免费";
    return new Intl.NumberFormat("zh-CN", {style: "currency", currency: course.currency}).format(course.priceMinor / 100);
}

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

    return (
        <section className="catalog panel" aria-labelledby="catalog-title">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">探索协议</span>
                    <h2 id="catalog-title">已发布的课程</h2>
                    <p>跟随经过认证的大学讲师，学习链上技能。</p>
                </div>
                <form className="catalog-search" role="search" onSubmit={(event) => {event.preventDefault(); const next = new URLSearchParams(searchParams); if (query.trim()) next.set("q", query.trim()); else next.delete("q"); setSearchParams(next);}}>
                    <label className="sr-only" htmlFor="course-search">搜索课程</label>
                    <input id="course-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索课程..." />
                    <button className="btn--primary" type="submit">搜索</button>
                </form>
            </div>

            {error ? <div className="notice notice--error" role="alert">{error}</div> : null}
            {!loading && !error && items.length === 0 ? <div className="empty-state"><span>◇</span><h3>暂无已发布课程</h3><p>新课程审核完成后将出现在这里。</p></div> : null}
            <div className="course-grid">
                {items.map((course, index) => (
                    <Link className="course-card course-card--clickable" key={course.id} to={`/courses/${course.id}`} aria-label={`打开课程《${course.title}》`}>
                        <div className={`course-card__art course-card__art--${index % 3}`}><span>0{index + 1}</span></div>
                        <div className="course-card__body">
                            <div className="course-card__meta"><span className="status-pill">已发布</span><span>{course.teacherName || "大学讲师"}</span></div>
                            <h3>{course.title}</h3>
                            <p>{course.description || "课程大纲稍后公布。"}</p>
                            <div className="course-card__footer"><strong>{formatCoursePrice(course)}</strong><span>{course.currentVersion.toString().padStart(2, "0")} 个模块</span></div>
                        </div>
                    </Link>
                ))}
            </div>
            {nextCursor ? <div className="load-more"><button className="btn--ghost" disabled={loading} onClick={() => void load(nextCursor)}>{loading ? "加载中..." : "加载更多"}</button></div> : null}
            {loading && items.length === 0 ? <div className="loading-grid" aria-label="课程加载中">{[0,1,2].map((item) => <div className="course-skeleton" key={item} />)}</div> : null}
        </section>
    );
}
