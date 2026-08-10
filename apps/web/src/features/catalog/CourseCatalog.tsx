import {useCallback, useEffect, useState} from "react";
import {Link, useSearchParams} from "react-router-dom";
import {ApiClientError} from "../../api/client";
import {courseApi, type Course} from "../../api/types";

export function formatCoursePrice(course: Pick<Course, "priceMinor" | "currency">): string {
    if (course.priceMinor === 0) return "Free";
    return new Intl.NumberFormat("en-US", {style: "currency", currency: course.currency}).format(course.priceMinor / 100);
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
            setError(cause instanceof ApiClientError ? cause.message : "Unable to load courses.");
        } finally {
            setLoading(false);
        }
    }, [appliedQuery]);

    useEffect(() => { void load(); }, [load]);

    return (
        <section className="catalog panel" aria-labelledby="catalog-title">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">Explore the protocol</span>
                    <h2 id="catalog-title">Published courses</h2>
                    <p>Learn onchain skills from verified university teachers.</p>
                </div>
                <form className="catalog-search" role="search" onSubmit={(event) => {event.preventDefault(); const next = new URLSearchParams(searchParams); if (query.trim()) next.set("q", query.trim()); else next.delete("q"); setSearchParams(next);}}>
                    <label className="sr-only" htmlFor="course-search">Search courses</label>
                    <input id="course-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search courses..." />
                    <button className="btn--primary" type="submit">Search</button>
                </form>
            </div>

            {error ? <div className="notice notice--error" role="alert">{error}</div> : null}
            {!loading && !error && items.length === 0 ? <div className="empty-state"><span>◇</span><h3>No published courses yet</h3><p>New courses will appear here after review.</p></div> : null}
            <div className="course-grid">
                {items.map((course, index) => (
                    <Link className="course-card course-card--clickable" key={course.id} to={`/courses/${course.id}`} aria-label={`Open course ${course.title}`}>
                        <div className={`course-card__art course-card__art--${index % 3}`}><span>0{index + 1}</span></div>
                        <div className="course-card__body">
                            <div className="course-card__meta"><span className="status-pill">Published</span><span>{course.teacherName || "University faculty"}</span></div>
                            <h3>{course.title}</h3>
                            <p>{course.description || "Course curriculum details coming soon."}</p>
                            <div className="course-card__footer"><strong>{formatCoursePrice(course)}</strong><span>{course.currentVersion.toString().padStart(2, "0")} modules</span></div>
                        </div>
                    </Link>
                ))}
            </div>
            {nextCursor ? <div className="load-more"><button className="btn--ghost" disabled={loading} onClick={() => void load(nextCursor)}>{loading ? "Loading..." : "Load more"}</button></div> : null}
            {loading && items.length === 0 ? <div className="loading-grid" aria-label="Loading courses">{[0,1,2].map((item) => <div className="course-skeleton" key={item} />)}</div> : null}
        </section>
    );
}
