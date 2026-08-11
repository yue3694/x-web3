/**
 * F02-T13 学习播放器外壳（受保护凭证 + 进度上报占位）。
 *
 * 设计要点：
 *   - 进入页面即调 GET /lessons/{id}/playback 拿签名 URL。
 *   - 用 detectPlayback 自动识别 URL 类型：
 *       native   → 原生 <video>（MP4 / WebM）
 *       hls      → <video> + 内置 HLS（Safari 原生 / 其它浏览器 fallback iframe）
 *       dash     → iframe 嵌入（DASH.js 未引入，先走外链）
 *       youtube  → iframe 嵌入
 *       bilibili → iframe 嵌入
 *       iframe   → 兜底 iframe
 *   - 可选 props.overrideSrc / props.previewSrc 直接传入地址，绕过后端签发：
 *       overrideSrc 用于本地 / 测试 / 讲师预览，previewSrc 用于未报名试看。
 *   - onTimeUpdate 节流 5 秒（与 F04 design.md 对齐）上报 POST /lessons/{id}/progress；
 *     上报失败静默退化，避免打断播放。
 *   - 凭证过期前 30s 自动重新签发；reload-on-expire 不打断 UI。
 *   - 鉴权：外层 RequireAuth，未登录直接渲染提示。
 *
 * 已知 TODO（F04 落地后清理）：
 *   - 接入 hls.js / dash.js 后，把 hls / dash 分支切到原生 <video>。
 *   - 接入 HLS/DASH 时引入 hls.js / dash.js，并把 signed cookie 模式加上。
 */

import {useCallback, useEffect, useMemo, useRef, useState} from "react";

import {ApiClientError} from "@/api/client";
import {learningApi, type PlaybackCredential, type ProgressReport} from "@/api/types";
import {useSession} from "@/auth/SessionContext";

import {ProgressReporter, useProgressReporter} from "./ProgressReporter";
import {detectPlayback, describePlayback} from "./playbackRules";

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
    /**
     * 进度上报回调（可选，外部可订阅本地状态做埋点）。
     * 仅对原生 / hls / dash 等带 progressbar 的播放器生效；
     * iframe 嵌入（YouTube/Bilibili）拿不到 timeupdate，靠 ProgressReporter 兜底。
     */
    onProgress?: (report: ProgressReport) => void;
    /**
     * 直接覆盖播放地址（讲师预览 / 本地调试用，绕过后端凭证）。
     * 与 lessonId 互斥：传入 overrideSrc 后会跳过 issuePlayback。
     */
    overrideSrc?: string | null;
    /**
     * 试看地址（未报名时也能看）：与 overrideSrc 区别在于不进入进度上报，
     * 仍在 UI 上标注为「试看」状态。
     */
    previewSrc?: string | null;
}

type FetchState = "idle" | "loading" | "ready" | "error";

function toBps(positionSeconds: number, durationSeconds: number): number {
    if (!durationSeconds || !isFinite(durationSeconds) || durationSeconds <= 0) return 0;
    const ratio = Math.max(0, Math.min(1, positionSeconds / durationSeconds));
    return Math.floor(ratio * 10_000);
}

export function Player({lessonId, courseId, title, className, onProgress, overrideSrc, previewSrc}: PlayerProps) {
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

    /** 是否跳过 issuePlayback（overrideSrc / previewSrc 模式）。 */
    const useOverride = Boolean(overrideSrc) || Boolean(previewSrc);
    const isPreview = !overrideSrc && Boolean(previewSrc);

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
        if (useOverride) {
            // 直接用 override / preview 源，无需后端签发。
            setState("ready");
            return;
        }
        if (sessionLoading) return;
        if (!profile) {
            setError("请先登录后再播放本课时。");
            setState("error");
            return;
        }
        void issueCredential();
    }, [sessionLoading, profile, issueCredential, useOverride]);

    // ----- 凭证快过期前 30s 续签（不打断当前 src） -----
    useEffect(() => {
        if (useOverride || !credential) return;
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
    }, [credential, issueCredential, useOverride]);

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

    // ----- 解析播放地址 -----
    const resolvedUrl = overrideSrc ?? previewSrc ?? credential?.url ?? "";
    const playback = useMemo(() => detectPlayback(resolvedUrl), [resolvedUrl]);
    const videoSrc = useMemo(() => playback?.embedUrl ?? "", [playback]);

    // 标记：当前播放源是否支持 timeupdate（iframe 嵌入拿不到）。
    const supportsTimeUpdate = playback?.kind === "native" || playback?.kind === "hls";

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
                    <p>播放凭证按需签发，五分钟内有效。进度上报会节流处理，不会阻塞播放器。</p>
                </div>
                <div className="learning-player__meta-aside">
                    <span className="learning-player__source-badge" title={describePlayback(playback)}>
                        <span className="learning-player__source-dot" aria-hidden="true" />
                        {describePlayback(playback)}
                    </span>
                    {profile ? (
                        <span className="badge" title="已登录">{profile.displayName}</span>
                    ) : null}
                    {isPreview ? <span className="status-pill status-pill--pending">试看</span> : null}
                </div>
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

                {state === "ready" && playback ? (
                    playback.kind === "iframe" ||
                    playback.kind === "youtube" ||
                    playback.kind === "bilibili" ||
                    playback.kind === "dash" ? (
                        <iframe
                            className="learning-player__iframe"
                            src={videoSrc}
                            title={title ?? "课程视频"}
                            allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
                            allowFullScreen
                            loading="lazy"
                            referrerPolicy="strict-origin-when-cross-origin"
                        />
                    ) : (
                        <video
                            ref={videoRef}
                            className="learning-player__video"
                            controls
                            preload="metadata"
                            playsInline
                            src={videoSrc}
                            onTimeUpdate={supportsTimeUpdate ? onTimeUpdate : undefined}
                            onSeeked={supportsTimeUpdate ? onSeeked : undefined}
                            onError={() => setError("视频播放失败，链接可能已过期。")}
                        />
                    )
                ) : null}
            </div>

            <footer className="learning-player__meta">
                <span className="muted">
                    {state === "ready" && credential
                        ? `签名链接有效期至 ${new Date(credential.expiresAt).toLocaleTimeString("zh-CN")}`
                        : playback?.kind === "native" || playback?.kind === "hls"
                          ? "原生播放器，可拖动进度条 / 倍速"
                          : "外部嵌入，进度由 ProgressReporter 兜底"}
                </span>
                {progressFailures > 0 ? (
                    <span className="muted" title="进度上报失败次数（不影响播放）">
                        进度同步失败：{progressFailures} 次（已跳过重试）
                    </span>
                ) : null}
            </footer>

            {state === "ready" && profile && !isPreview ? (
                <ProgressReporter
                    handle={progressHandle}
                    courseId={courseId ?? lessonId}
                    courseTitle={title}
                />
            ) : null}
        </section>
    );
}