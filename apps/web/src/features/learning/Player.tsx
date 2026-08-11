/**
 * F02-T13 学习播放器外壳（受保护凭证 + 进度上报占位）。
 *
 * 设计要点：
 *   - 进入页面即调 GET /lessons/{id}/playback 拿 S3 presigned GET / CloudFront 凭证。
 *   - 用原生 <video> 接入；不引入 video.js（MVP 先用浏览器自带控件，HLS/DASH 留待 F04）。
 *   - onTimeupdate 节流 5 秒（与 F04 design.md 129 行对齐）上报 POST /lessons/{id}/progress；
 *     上报失败静默退化，避免打断播放。
 *   - 凭证过期前 30s 自动重新签发；reload-on-expire 不打断 UI。
 *   - 鉴权：外层 RequireAuth，未登录直接渲染提示。
 *
 * 已知 TODO（F04 落地后清理）：
 *   - 后端 progress 接口实现后，去掉"404/405 静默"分支。
 *   - 接入 HLS/DASH 时引入 hls.js / dash.js，并把 signed cookie 模式加上。
 */

import {useCallback, useEffect, useMemo, useRef, useState} from "react";

import {ApiClientError} from "@/api/client";
import {learningApi, type PlaybackCredential, type ProgressReport} from "@/api/types";
import {useSession} from "@/auth/SessionContext";

import {ProgressReporter, useProgressReporter} from "./ProgressReporter";

/** 进度上报节流间隔（毫秒）—— 与 F04 design.md 一致。 */
const PROGRESS_THROTTLE_MS = 5_000;

/** 凭证快过期时主动续签的提前量。 */
const REFRESH_LEAD_MS = 30_000;

export interface PlayerProps {
    /** 课时 ID（UUID，由上游 CourseDetail 传入） */
    lessonId: string;
    /** 课程 ID（用于 "Mark as complete" 跳转；缺省时复用 lessonId） */
    courseId?: string;
    /** 课时标题（仅展示） */
    title?: string;
    /** 自定义类 */
    className?: string;
    /** 进度上报回调（可选，外部可订阅本地状态做埋点） */
    onProgress?: (report: ProgressReport) => void;
}

type FetchState = "idle" | "loading" | "ready" | "error";

function toBps(positionSeconds: number, durationSeconds: number): number {
    if (!durationSeconds || !isFinite(durationSeconds) || durationSeconds <= 0) return 0;
    const ratio = Math.max(0, Math.min(1, positionSeconds / durationSeconds));
    return Math.floor(ratio * 10_000);
}

export function Player({lessonId, courseId, title, className, onProgress}: PlayerProps) {
    const {profile, loading: sessionLoading} = useSession();
    const videoRef = useRef<HTMLVideoElement | null>(null);

    const [state, setState] = useState<FetchState>("idle");
    const [error, setError] = useState<string>("");
    const [credential, setCredential] = useState<PlaybackCredential | null>(null);

    /** 上次成功上报的时间戳（ms）—— 用于节流。 */
    const lastReportAt = useRef<number>(0);
    /** 已上报的最大 progressBps —— 本地单调推进，避免回退。 */
    const lastReportedBps = useRef<number>(0);
    /** 进度上报失败的次数（仅展示用） */
    const [progressFailures, setProgressFailures] = useState(0);

    const progressHandle = useProgressReporter({
        lessonId,
        courseId: courseId ?? lessonId,
        videoRef,
        userId: profile?.id ?? null,
    });

    // ----- 拉取播放凭证 -----
    const issueCredential = useCallback(async (): Promise<PlaybackCredential | null> => {
        if (!profile) return null;
        setState((s) => (s === "ready" ? s : "loading"));
        setError("");
        try {
            const cred = await learningApi.issuePlayback(lessonId);
            setCredential(cred);
            setState("ready");
            return cred;
        } catch (cause) {
            if (cause instanceof ApiClientError) {
                if (cause.status === 401) {
                    setError("请先登录后再播放本课时。");
                } else if (cause.status === 403) {
                    setError("请先购买本课程后再观看。");
                } else {
                    setError(cause.message);
                }
            } else {
                setError("无法获取播放凭证。");
            }
            setState("error");
            return null;
        }
    }, [lessonId, profile]);

    useEffect(() => {
        if (sessionLoading) return;
        if (!profile) {
            setError("请先登录后再播放本课时。");
            setState("error");
            return;
        }
        void issueCredential();
    }, [sessionLoading, profile, issueCredential]);

    // ----- 凭证快过期前 30s 续签（不打断当前 src） -----
    useEffect(() => {
        if (!credential) return;
        const exp = new Date(credential.expiresAt).getTime();
        const wait = exp - Date.now() - REFRESH_LEAD_MS;
        if (wait <= 0) {
            // 已经快过期或已过期，立刻续
            void issueCredential();
            return;
        }
        const timer = window.setTimeout(() => {
            void issueCredential();
        }, wait);
        return () => window.clearTimeout(timer);
    }, [credential, issueCredential]);

    // ----- 进度上报（节流 + 单调推进） -----
    const reportNow = useCallback(
        async (position: number, duration: number) => {
            const report: ProgressReport = {
                positionSeconds: position,
                durationSeconds: duration,
                progressBps: toBps(position, duration),
                reportedAt: new Date().toISOString(),
            };
            // 单调推进：本地上报过更高的 bps，就不再发更低的，避免覆盖服务端
            if (report.progressBps < lastReportedBps.current) return;
            lastReportedBps.current = report.progressBps;
            onProgress?.(report);
            try {
                await learningApi.reportProgress(lessonId, report);
            } catch {
                // learningApi.reportProgress 已经吞掉 404/405；这里只统计其它罕见错误
                setProgressFailures((n) => n + 1);
            }
        },
        [lessonId, onProgress],
    );

    const onTimeUpdate = useCallback(
        (event: React.SyntheticEvent<HTMLVideoElement>) => {
            const v = event.currentTarget;
            const now = Date.now();
            if (now - lastReportAt.current < PROGRESS_THROTTLE_MS) return;
            lastReportAt.current = now;
            progressHandle.reportNow();
            void reportNow(v.currentTime, v.duration || 0);
        },
        [progressHandle, reportNow],
    );

    // 用户主动 seek 后立即上报一次（不算节流窗口）
    const onSeeked = useCallback(
        (event: React.SyntheticEvent<HTMLVideoElement>) => {
            const v = event.currentTarget;
            lastReportAt.current = Date.now();
            progressHandle.reportNow();
            void reportNow(v.currentTime, v.duration || 0);
        },
        [progressHandle, reportNow],
    );

    // 离开页面 / 卸载前最后一次上报
    useEffect(() => {
        return () => {
            const v = videoRef.current;
            if (!v) return;
            // 这里不需要 await；fire-and-forget 即可
            void reportNow(v.currentTime, v.duration || 0);
        };
    }, [reportNow]);

    const videoSrc = useMemo(() => credential?.url ?? "", [credential]);

    // 5s 兜底轮询上报 + 100% 完成按钮（与 Player.onTimeUpdate 节流互补）
    return (
        <section
            className={`learning-player panel${className ? ` ${className}` : ""}`}
            aria-labelledby="learning-player-title"
        >
            <div className="section-heading">
                <div>
                    <span className="eyebrow">课时</span>
                    <h2 id="learning-player-title">
                        {title || (state === "ready" ? "正在播放" : "课时加载中…")}
                    </h2>
                    <p>
                        播放凭证按需签发，五分钟内有效。进度上报会节流处理，不会阻塞播放器。
                    </p>
                </div>
                {profile ? (
                    <span className="badge" title="已登录">{profile.displayName}</span>
                ) : null}
            </div>

            {error ? (
                <div className="notice notice--error" role="alert">
                    {error}
                </div>
            ) : null}

            <div className="learning-player__stage">
                {state === "loading" ? (
                    <div className="learning-player__placeholder" role="status" aria-live="polite">
                        <span className="learning-player__spinner" aria-hidden="true" />
                        <span>正在请求播放凭证…</span>
                    </div>
                ) : null}

                {state === "ready" && videoSrc ? (
                    <video
                        ref={videoRef}
                        className="learning-player__video"
                        controls
                        preload="metadata"
                        playsInline
                        // HLS/DASH 留待 F04；当前仅直连 MP4/WebM
                        src={videoSrc}
                        onTimeUpdate={onTimeUpdate}
                        onSeeked={onSeeked}
                        onError={() => setError("视频播放失败，链接可能已过期。")}
                    />
                ) : null}
            </div>

            <footer className="learning-player__meta">
                <span className="muted">
                    {state === "ready" && credential
                        ? `签名链接有效期至 ${new Date(credential.expiresAt).toLocaleTimeString()}`
                        : "等待凭证中"}
                </span>
                {progressFailures > 0 ? (
                    <span className="muted" title="进度上报失败次数（不影响播放）">
                        进度同步失败：{progressFailures} 次（已跳过重试）
                    </span>
                ) : null}
            </footer>

            {state === "ready" && profile ? (
                <ProgressReporter
                    handle={progressHandle}
                    courseId={courseId ?? lessonId}
                    courseTitle={title}
                />
            ) : null}
        </section>
    );
}
