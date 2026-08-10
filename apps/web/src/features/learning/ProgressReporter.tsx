/**
 * F02-T13 学习进度上报 hook + 课程完成按钮。
 *
 * 行为契约（与 `packages/shared/openapi/learning.yaml` live spec 对齐）：
 *   - useProgressReporter 每 5s 读 videoRef.currentTime / duration，
 *     发 POST /lessons/{id}/progress，body = { pct: 0..100 整数 }。
 *   - 失败静默：累计 failureCount，UI 可见，不影响播放。
 *   - 单调推进：本地 maxPct 不回退；服务端 409 PROGRESS_REGRESSION 也忽略。
 *   - pct=100 时渲染 "Mark as complete"，调 POST /courses/{id}/complete，
 *     200 后跳转到 /account/certificates。
 *
 * 与 Player.tsx 的分工：Player 持有 onTimeUpdate 毫秒级节流 + 视频元素；
 * 本组件只负责 5s 兜底轮询 + 完成按钮。两者互不冲突。
 */

import {useCallback, useEffect, useRef, useState} from "react";

import {ApiClientError} from "@/api/client";
import {learningApi} from "@/api/learning";

/** 兜底轮询间隔（毫秒）—— 与 F04 design.md 129 行一致。 */
const REPORT_INTERVAL_MS = 5_000;

function computePct(currentTime: number, duration: number): number {
    if (!duration || !isFinite(duration) || duration <= 0) return 0;
    const ratio = Math.max(0, Math.min(1, currentTime / duration));
    return Math.floor(ratio * 100);
}

export interface ProgressReporterHandle {
    /** 当前进度的整数百分比 0..100 */
    pct: number;
    /** 上次成功上报的 wall-clock 毫秒（无上报则为 0） */
    lastReportAt: number;
    /** 上报失败的累计次数（仅展示） */
    failureCount: number;
    /** 强制立即上报一次（脱离节流窗口） */
    reportNow: () => void;
    /** 是否已经达到 100%（用于驱动完成按钮） */
    isComplete: boolean;
}

export interface UseProgressReporterOptions {
    /** 课时 ID（用于上报路径） */
    lessonId: string;
    /** 课程 ID（用于 "Mark as complete" 跳转；缺省时复用 lessonId） */
    courseId: string;
    /** video 元素 ref（Player 持有） */
    videoRef: React.RefObject<HTMLVideoElement | null>;
    /** 已登录用户的 userId（未登录时不上报） */
    userId?: string | null;
}

export function useProgressReporter(opts: UseProgressReporterOptions): ProgressReporterHandle {
    const {lessonId, courseId, videoRef, userId} = opts;
    const [pct, setPct] = useState(0);
    const [lastReportAt, setLastReportAt] = useState(0);
    const [failureCount, setFailureCount] = useState(0);
    /** 本地已上报的最大百分比 —— 单调推进，避免覆盖服务端。 */
    const maxPctRef = useRef(0);

    const reportNow = useCallback(() => {
        const v = videoRef.current;
        if (!v) return;
        const cur = v.currentTime;
        const dur = v.duration;
        if (!dur || !isFinite(dur) || dur <= 0) return;
        const nextPct = computePct(cur, dur);
        setPct(nextPct);
        // 单调推进：低于历史最大值时不上报
        if (nextPct < maxPctRef.current) return;
        maxPctRef.current = nextPct;
        void learningApi.reportLessonProgress(lessonId, {pct: nextPct})
            .then(() => {
                setLastReportAt(Date.now());
            })
            .catch(() => {
                // reportLessonProgress 已吞 404/405/501；这里累计其它罕见错误。
                setFailureCount((n) => n + 1);
            });
        // courseId 仅用于完成跳转，不参与上报
        void courseId;
    }, [courseId, lessonId, videoRef]);

    // 5s 兜底轮询：覆盖视频暂停 / 切 tab / onTimeUpdate 被浏览器节流的场景。
    useEffect(() => {
        if (!userId) return;
        const timer = window.setInterval(() => {
            const v = videoRef.current;
            if (!v || v.paused || !v.duration || !isFinite(v.duration)) return;
            reportNow();
        }, REPORT_INTERVAL_MS);
        return () => window.clearInterval(timer);
    }, [reportNow, userId, videoRef]);

    return {
        pct,
        lastReportAt,
        failureCount,
        reportNow,
        isComplete: pct >= 100,
    };
}

export interface ProgressReporterProps {
    /** useProgressReporter 返回的 handle */
    handle: ProgressReporterHandle;
    /** 课程 ID（用于完成跳转） */
    courseId: string;
    /** 课程标题（成功提示中展示） */
    courseTitle?: string;
    /** 自定义类 */
    className?: string;
}

type CompleteState = "idle" | "submitting" | "success" | "error";

/**
 * 与 hook 配套的 UI 组件：展示当前进度、失败重试计数、
 * 以及 pct=100 时的 "Mark as complete" 按钮。
 */
export function ProgressReporter({handle, courseId, courseTitle, className}: ProgressReporterProps) {
    const [state, setState] = useState<CompleteState>("idle");
    const [errorMsg, setErrorMsg] = useState("");

    const onComplete = useCallback(async () => {
        if (state === "submitting" || state === "success") return;
        setState("submitting");
        setErrorMsg("");
        try {
            await learningApi.markCourseComplete(courseId);
            setState("success");
            // 跳转到证书页（无 react-router；用 location.assign 走全量加载）
            window.location.assign("/account/certificates");
        } catch (cause) {
            setState("error");
            if (cause instanceof ApiClientError) {
                setErrorMsg(`${cause.code}: ${cause.message}`);
            } else {
                setErrorMsg("Failed to mark the course as complete.");
            }
        }
    }, [courseId, state]);

    return (
        <div className={`progress-reporter${className ? ` ${className}` : ""}`}>
            <div className="progress-reporter__bar" role="progressbar"
                 aria-valuemin={0} aria-valuemax={100} aria-valuenow={handle.pct}>
                <div className="progress-reporter__fill" style={{width: `${handle.pct}%`}} />
            </div>
            <div className="progress-reporter__meta">
                <span className="muted">{handle.pct}% watched</span>
                {handle.failureCount > 0 ? (
                    <span className="muted" title="进度上报失败次数（不影响播放）">
                        sync retries: {handle.failureCount}
                    </span>
                ) : null}
            </div>
            {handle.isComplete && state !== "success" ? (
                <div className="progress-reporter__complete">
                    <p>
                        You have watched the entire lesson
                        {courseTitle ? ` “${courseTitle}”` : ""}. Mark this course as complete to mint
                        your certificate.
                    </p>
                    <button
                        type="button"
                        className="btn--primary"
                        disabled={state === "submitting"}
                        onClick={() => void onComplete()}
                    >
                        {state === "submitting" ? "Submitting…" : "Mark as complete"}
                    </button>
                </div>
            ) : null}
            {state === "success" ? (
                <div className="notice notice--ok" role="status">
                    Course marked complete. Redirecting to your certificates…
                </div>
            ) : null}
            {state === "error" ? (
                <div className="notice notice--error" role="alert">
                    {errorMsg}{" "}
                    <button type="button" className="btn--ghost" onClick={() => void onComplete()}>
                        Retry
                    </button>
                </div>
            ) : null}
        </div>
    );
}
