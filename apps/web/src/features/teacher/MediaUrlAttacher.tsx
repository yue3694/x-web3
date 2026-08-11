/**
 * MediaUrlAttacher — 教师课程媒体「HTTP(S) 视频地址」绑定控件。
 *
 * 设计：
 *   - 教师在 lesson 行直接粘贴一个 http(s) 视频 URL（如 CDN / 推流地址 / 第三方存储）；
 *   - 客户端先做基本格式校验（http/https + URL 合法），通过后即时调用
 *     `onUploaded(syntheticMediaAsset)`，沿用现有 `MediaAsset` 类型契约；
 *   - 当前不直接对接后端：等待后端增加"register media by URL"端点时，把
 *     这里的 `submit` 换成 POST 即可，对外回调形态不变；
 *   - 仍保留替换/清除入口；URL 解析出错时给出可读 reason 而不是 alert。
 *
 * 与 `MediaUploader`（走 S3 PUT + finalize 的"真上传"）互不影响；
 * CourseEditor 目前只调用本组件，旧的 MediaUploader 留作未来恢复用。
 */

import { useCallback, useMemo, useState, type FormEvent } from "react";

import type { MediaAsset } from "./teacherTypes";

export interface MediaUrlAttacherProps {
    /** 默认按钮文案（多语言扩展点）。 */
    label?: string;
    /** 占位提示（占满时给个示例 URL）。 */
    placeholder?: string;
    /** 当前已绑定的 URL（再次打开时回填到输入框）。 */
    initialUrl?: string;
    /** 通过校验、生成 `MediaAsset` 后回调；外部可绑定到 lesson.mediaAssetId。 */
    onAttached?: (asset: MediaAsset) => void;
    /** 已有绑定时给出"清除"入口；外部清掉 lesson.mediaAssetId。 */
    onClear?: () => void;
}

const DEFAULT_PLACEHOLDER = "https://cdn.example.com/videos/lesson-01.mp4";

interface ValidationResult {
    ok: boolean;
    reason?: string;
    normalized?: string;
    host?: string;
    filename?: string;
}

function validateUrl(raw: string): ValidationResult {
    const trimmed = raw.trim();
    if (!trimmed) {
        return { ok: false, reason: "URL is required." };
    }
    let url: URL;
    try {
        url = new URL(trimmed);
    } catch {
        return { ok: false, reason: "Not a valid URL — check the format." };
    }
    if (url.protocol !== "http:" && url.protocol !== "https:") {
        return { ok: false, reason: `Unsupported protocol: ${url.protocol.replace(":", "")}. Use http(s).` };
    }
    if (!url.hostname) {
        return { ok: false, reason: "URL is missing a host." };
    }
    const pathname = url.pathname || "/";
    const filename = pathname.split("/").filter(Boolean).pop() ?? pathname;
    return {
        ok: true,
        normalized: url.toString(),
        host: url.host,
        filename,
    };
}

function inferContentType(url: string): string {
    const lower = url.toLowerCase().split("?")[0] ?? "";
    if (lower.endsWith(".mp4")) return "video/mp4";
    if (lower.endsWith(".webm")) return "video/webm";
    if (lower.endsWith(".mov")) return "video/quicktime";
    if (lower.endsWith(".m3u8")) return "application/vnd.apple.mpegurl";
    if (lower.endsWith(".mpd")) return "application/dash+xml";
    return "video/url";
}

function makeSyntheticAsset(url: string): MediaAsset {
    const id = globalThis.crypto?.randomUUID?.() ?? `url-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
    const now = new Date().toISOString();
    return {
        id,
        ownerUserId: "",
        s3Key: url,
        contentType: inferContentType(url),
        sizeBytes: 0,
        status: "ready",
        scanStatus: "clean",
        createdAt: now,
        updatedAt: now,
    };
}

export function MediaUrlAttacher({
    label = "Attach video",
    placeholder = DEFAULT_PLACEHOLDER,
    initialUrl = "",
    onAttached,
    onClear,
}: MediaUrlAttacherProps) {
    const [draft, setDraft] = useState(initialUrl);
    const [touched, setTouched] = useState(false);
    const [error, setError] = useState("");
    const [attached, setAttached] = useState<MediaAsset | null>(
        initialUrl ? makeSyntheticAsset(initialUrl) : null,
    );

    const validation = useMemo(() => validateUrl(draft), [draft]);
    const showError = touched && !validation.ok;

    const handleSubmit = useCallback(
        (event: FormEvent<HTMLFormElement>) => {
            event.preventDefault();
            setTouched(true);
            if (!validation.ok || !validation.normalized) {
                setError(validation.reason ?? "Invalid URL.");
                return;
            }
            setError("");
            const asset = makeSyntheticAsset(validation.normalized);
            setAttached(asset);
            onAttached?.(asset);
        },
        [onAttached, validation],
    );

    const handleClear = useCallback(() => {
        setAttached(null);
        setDraft("");
        setTouched(false);
        setError("");
        onClear?.();
    }, [onClear]);

    if (attached) {
        return (
            <div className="media-url-attacher media-url-attacher--ready" role="status">
                <span className="media-url-attacher__chip" aria-label="Attached video">
                    <svg
                        aria-hidden="true"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="1.8"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                    >
                        <path d="m10 8 6 4-6 4V8Z" />
                        <rect x="3" y="5" width="18" height="14" rx="2" />
                    </svg>
                    <span className="media-url-attacher__chip-text">
                        <span className="media-url-attacher__chip-host">{validation.host ?? new URL(attached.s3Key).host}</span>
                        <span className="media-url-attacher__chip-path" title={attached.s3Key}>
                            {attached.s3Key}
                        </span>
                    </span>
                </span>
                <button
                    type="button"
                    className="btn--ghost"
                    onClick={handleClear}
                    aria-label="Remove attached video"
                >
                    Remove
                </button>
            </div>
        );
    }

    return (
        <form className="media-url-attacher" onSubmit={handleSubmit} noValidate>
            <label className="media-url-attacher__label">
                <span className="media-url-attacher__label-text">{label}</span>
                <span className="media-url-attacher__field">
                    <span className="media-url-attacher__scheme" aria-hidden="true">
                        <svg
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth="1.6"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                        >
                            <path d="M10 13a5 5 0 0 0 7.07 0l1.41-1.41a5 5 0 0 0-7.07-7.07L10 5.93" />
                            <path d="M14 11a5 5 0 0 0-7.07 0l-1.41 1.41a5 5 0 0 0 7.07 7.07L14 18.07" />
                        </svg>
                    </span>
                    <input
                        type="url"
                        inputMode="url"
                        autoComplete="off"
                        spellCheck={false}
                        value={draft}
                        onChange={(event) => {
                            setDraft(event.target.value);
                            if (touched) setError("");
                        }}
                        onBlur={() => setTouched(true)}
                        placeholder={placeholder}
                        aria-invalid={showError}
                        aria-describedby={showError ? "media-url-attacher-error" : undefined}
                        className="media-url-attacher__input"
                    />
                    <button
                        type="submit"
                        className="media-url-attacher__submit"
                        disabled={!validation.ok}
                    >
                        Attach
                    </button>
                </span>
            </label>
            {validation.ok ? (
                <p className="media-url-attacher__hint" aria-live="polite">
                    Will attach <code>{validation.host}{validation.filename ? `/${validation.filename}` : ""}</code>{" "}
                    to this lesson.
                </p>
            ) : null}
            {showError && error ? (
                <p id="media-url-attacher-error" className="media-url-attacher__error" role="alert">
                    {error}
                </p>
            ) : null}
        </form>
    );
}