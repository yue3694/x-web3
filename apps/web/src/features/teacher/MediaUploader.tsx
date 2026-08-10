/**
 * MediaUploader — 教师课程媒体上传控件。
 *
 * 流程 (apps/api/internal/media/media.go):
 *   1) POST /teacher/media/upload-intent {fileName, contentType, sizeBytes}
 *      ← {mediaAssetId, uploadUrl, expiresAt, s3Key}
 *   2) PUT uploadUrl with file body
 *   3) POST /teacher/media/{id}/finalize {checksumSha256}
 *      ← {status: 'draft'|'ready', scanStatus, ...}
 *
 * 进度条使用浏览器内置 progress 事件；上传到 S3 走 XHR（fetch 进度在
 * 主流环境也是非标准）。失败时暴露 ApiClientError.message，便于上层捕获。
 */

import {useCallback, useRef, useState, type ChangeEvent} from "react";

import {ApiClientError, apiClient} from "@/api/client";
import type {MediaAsset} from "./teacherTypes";

export type UploadPhase = "idle" | "intent" | "uploading" | "finalize" | "ready" | "invalid" | "error";

interface UploadIntentResponse {
    mediaAssetId: string;
    s3Key: string;
    uploadUrl: string;
    expiresAt: string;
}

interface ProgressState {
    loaded: number;
    total: number;
}

export interface MediaUploaderProps {
    /** MIME 白名单（与后端 media.allowList 同步）；前端过滤给出即时反馈。 */
    accept?: string[];
    /** 单文件最大字节数；超过则拒绝。 */
    maxBytes?: number;
    /** 完成 finalize 且返回 ready 后回调；外部可绑定到 lesson.mediaAssetId。 */
    onUploaded?: (asset: MediaAsset) => void;
    /** 默认按钮文案（多语言扩展点）。 */
    label?: string;
}

const DEFAULT_ACCEPT = ["video/mp4", "video/webm", "video/quicktime", "application/pdf"];
const DEFAULT_MAX = 2 * 1024 * 1024 * 1024; // 与后端 DefaultMaxBytes 对齐

function computeSha256(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
        const cryptoObj = globalThis.crypto;
        if (!cryptoObj?.subtle) {
            reject(new Error("SubtleCrypto not available in this browser."));
            return;
        }
        const reader = new FileReader();
        reader.onerror = () => reject(reader.error ?? new Error("Failed to read file."));
        reader.onload = () => {
            const buf = reader.result;
            if (!(buf instanceof ArrayBuffer)) {
                reject(new Error("Unexpected FileReader result."));
                return;
            }
            cryptoObj.subtle.digest("SHA-256", buf).then((hash) => {
                const bytes = new Uint8Array(hash);
                let hex = "";
                for (let i = 0; i < bytes.length; i += 1) hex += bytes[i]!.toString(16).padStart(2, "0");
                resolve(hex);
            }, reject);
        };
        reader.readAsArrayBuffer(file);
    });
}

function uploadWithProgress(url: string, file: File, contentType: string, signal: AbortSignal, onProgress: (loaded: number, total: number) => void): Promise<void> {
    return new Promise((resolve, reject) => {
        const xhr = new XMLHttpRequest();
        xhr.open("PUT", url);
        xhr.setRequestHeader("Content-Type", contentType);
        xhr.upload.onprogress = (event) => {
            if (event.lengthComputable) onProgress(event.loaded, event.total);
        };
        xhr.onload = () => {
            if (xhr.status >= 200 && xhr.status < 300) {
                onProgress(file.size, file.size);
                resolve();
            } else {
                reject(new Error(`Upload failed: ${xhr.status} ${xhr.statusText || ""}`.trim()));
            }
        };
        xhr.onerror = () => reject(new Error("Network error during upload."));
        xhr.onabort = () => reject(new DOMException("Upload aborted", "AbortError"));
        signal.addEventListener("abort", () => xhr.abort(), {once: true});
        xhr.send(file);
    });
}

export function MediaUploader({accept = DEFAULT_ACCEPT, maxBytes = DEFAULT_MAX, onUploaded, label = "Upload media"}: MediaUploaderProps) {
    const [phase, setPhase] = useState<UploadPhase>("idle");
    const [progress, setProgress] = useState<ProgressState>({loaded: 0, total: 0});
    const [error, setError] = useState("");
    const [fileName, setFileName] = useState("");
    const abortRef = useRef<AbortController | null>(null);

    const reset = useCallback(() => {
        abortRef.current?.abort();
        abortRef.current = null;
        setPhase("idle");
        setProgress({loaded: 0, total: 0});
        setError("");
        setFileName("");
    }, []);

    const handleChange = useCallback(async (event: ChangeEvent<HTMLInputElement>) => {
        const file = event.target.files?.[0];
        event.target.value = "";
        if (!file) return;
        if (!accept.includes(file.type)) {
            setPhase("error");
            setError(`Unsupported file type: ${file.type || "unknown"}.`);
            return;
        }
        if (file.size > maxBytes) {
            setPhase("error");
            setError(`File exceeds the ${Math.round(maxBytes / (1024 * 1024))} MB limit.`);
            return;
        }
        setFileName(file.name);
        setError("");
        setProgress({loaded: 0, total: file.size});
        const controller = new AbortController();
        abortRef.current = controller;
        try {
            setPhase("intent");
            const intent = await apiClient.post<UploadIntentResponse>("/teacher/media/upload-intent", {
                fileName: file.name,
                contentType: file.type,
                sizeBytes: file.size,
            });
            setPhase("uploading");
            await uploadWithProgress(intent.uploadUrl, file, file.type, controller.signal, (loaded, total) => setProgress({loaded, total}));
            setPhase("finalize");
            const checksum = await computeSha256(file).catch(() => "");
            const asset = await apiClient.post<MediaAsset>(
                `/teacher/media/${intent.mediaAssetId}/finalize`,
                {checksumSha256: checksum},
            );
            if (asset.status === "ready") {
                setPhase("ready");
                onUploaded?.(asset);
            } else {
                setPhase("invalid");
                setError("Checksum mismatch. Please retry the upload.");
            }
        } catch (cause) {
            if (cause instanceof DOMException && cause.name === "AbortError") {
                reset();
                return;
            }
            setPhase("error");
            setError(cause instanceof ApiClientError ? cause.message : "Upload failed.");
        } finally {
            abortRef.current = null;
        }
    }, [accept, maxBytes, onUploaded, reset]);

    const cancel = useCallback(() => {
        abortRef.current?.abort();
    }, []);

    const busy = phase === "intent" || phase === "uploading" || phase === "finalize";
    const percent = phase === "ready"
        ? 100
        : progress.total > 0
            ? Math.min(100, Math.round((progress.loaded / progress.total) * 100))
            : 0;

    return (
        <div className="media-uploader card">
            <label className="media-uploader__field">
                <span>{label}</span>
                <input
                    type="file"
                    accept={accept.join(",")}
                    onChange={(event) => void handleChange(event)}
                    disabled={busy}
                />
            </label>
            {fileName ? <p className="media-uploader__file">{fileName} · {phase}</p> : null}
            {busy || phase === "ready" ? <div className="media-uploader__progress" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent}><div className="media-uploader__bar" style={{width: `${percent}%`}} /></div> : null}
            {busy ? (
                <button type="button" className="btn--ghost" onClick={cancel}>Cancel upload</button>
            ) : null}
            {error ? <div className="notice notice--error" role="alert">{error}</div> : null}
            {phase === "ready" ? <button type="button" className="btn--ghost" onClick={reset}>Upload another</button> : null}
        </div>
    );
}