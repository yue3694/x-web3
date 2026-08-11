import {useEffect, useState} from "react";
import {Link, Navigate, useParams, useSearchParams} from "react-router-dom";

import {ApiClientError} from "@/api/client";
import {courseApi, type CourseDetail} from "@/api/types";
import {RequireAuth} from "@/auth/RequireAuth";
import {Player} from "@/features/learning/Player";

export default function LearningPage() {
    const {courseId} = useParams();
    const [search, setSearch] = useSearchParams();
    const [detail, setDetail] = useState<CourseDetail | null>(null);
    const [error, setError] = useState("");
    useEffect(() => {
        if (!courseId) return;
        let active = true;
        courseApi.get(courseId).then((value) => {if (active) setDetail(value);}).catch((cause) => {if (active) setError(cause instanceof ApiClientError ? cause.message : "无法加载该课程。");});
        return () => {active = false;};
    }, [courseId]);
    if (!courseId) return <Navigate to="/account/enrollments" replace />;
    const lessons = detail?.chapters.flatMap((chapter) => chapter.lessons.map((lesson) => ({...lesson, chapterTitle: chapter.title}))) ?? [];
    const activeLesson = lessons.find((lesson) => lesson.id === search.get("lesson")) ?? lessons[0];
    return (
        <RequireAuth>
            <div className="learning-layout">
                <aside className="lesson-nav panel" aria-label="课程课时">
                    <Link to="/account/enrollments" className="back-link">← 我的学习</Link>
                    <h1>{detail?.course.title ?? "课程加载中…"}</h1>
                    {error ? <div className="notice notice--error" role="alert">{error}</div> : null}
                    <ol>{lessons.map((lesson) => <li key={lesson.id}><button type="button" className={lesson.id === activeLesson?.id ? "is-active" : ""} onClick={() => setSearch({lesson: lesson.id})}><small>{lesson.chapterTitle}</small><span>{lesson.title}</span></button></li>)}</ol>
                </aside>
                <div>{activeLesson ? <Player lessonId={activeLesson.id} courseId={courseId} title={activeLesson.title} /> : !error ? <div className="route-loader" role="status">课程目录加载中…</div> : null}</div>
            </div>
        </RequireAuth>
    );
}
