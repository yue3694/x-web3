/**
 * CourseDetail — 课程详情面板（modal）。
 *
 * 设计：
 *   - 由 CourseCatalog 卡片点击打开；Esc / 遮罩点击关闭；
 *   - 调 /courses/{id} 拿课程 + 章节；enrolled=true 时才显示受保护章节占位；
 *   - 详情内嵌入 <Comments>，由评论组件自己处理登录 / 已购买 / 软删。
 *
 * 注意：当前后端 catalog.Get 返回不带 enrolled 字段（OpenAPI 中有但 Go handler
 * 未透传）；此处按 OpenAPI contract 解析，缺失时退化为 false。
 */

import {useEffect, useState} from "react";

import {ApiClientError} from "@/api/client";
import {courseApi, type CourseDetail as CourseDetailData} from "@/api/types";

import {Comments} from "./Comments";

interface CourseDetailProps {
    courseId: string | null;
    onClose: () => void;
}

interface LoadedDetail extends CourseDetailData {
    enrolled?: boolean;
}

export function CourseDetail({courseId, onClose}: CourseDetailProps) {
    const [data, setData] = useState<LoadedDetail | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");

    useEffect(() => {
        if (!courseId) {
            setData(null);
            setError("");
            return;
        }
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

    useEffect(() => {
        if (!courseId) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [courseId, onClose]);

    if (!courseId) return null;

    const course = data?.course;

    return (
        <div
            className="course-detail"
            role="dialog"
            aria-modal="true"
            aria-labelledby="course-detail-title"
            onClick={(e) => {
                if (e.target === e.currentTarget) onClose();
            }}
        >
            <article className="course-detail__panel panel">
                <button
                    type="button"
                    className="course-detail__close"
                    aria-label="Close course detail"
                    onClick={onClose}
                >
                    ×
                </button>

                {loading ? <p className="muted">Loading course…</p> : null}
                {error ? <div className="notice notice--error" role="alert">{error}</div> : null}

                {course ? (
                    <>
                        <header className="course-detail__header">
                            <span className="eyebrow">Course</span>
                            <h2 id="course-detail-title">{course.title}</h2>
                            <p className="muted">
                                {course.teacherName || "University faculty"} · v{course.currentVersion}
                                {data?.enrolled ? <span className="status-pill status-pill--published">Enrolled</span> : null}
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

                        <Comments courseId={course.id} courseTitle={course.title} />
                    </>
                ) : null}
            </article>
        </div>
    );
}
