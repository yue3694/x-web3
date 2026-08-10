/**
 * 危险操作二次确认 modal。
 *
 * 设计动机：MVP 阶段后端没有 Privy 实时鉴权（re-issue access token 不在
 * 当前 sprint），为了不阻断 SYSTEM_ADMIN 工作流，UX 上用「5 秒内双击『I confirm』」
 * 作为人为操作门槛。后端真实权威来自 `rbac.Middleware(PermSystemAdmin)`，前端
 * 这一层只是防误触。
 *
 * 样式策略：本目录受「不修改 apps/web/src 之外」约束，无法新增全局 class；
 * 这里使用内联 style 复用 :root 已有的 CSS 变量（--bg-elev-2 / --border-strong
 * / --fg / --accent / --accent-rose 等），保证视觉与系统一致。
 *
 * 用法：
 *   <ConfirmRequired
 *     title="Grant role: teacher"
 *     description="Granting this role will let the user publish courses."
 *     confirmLabel="Grant"
 *     onConfirm={async () => { await adminApi.grantRole(...) }}
 *   >
 *     <button className="btn--primary">Open confirm</button>
 *   </ConfirmRequired>
 */

import {type CSSProperties, type ReactNode, useEffect, useRef, useState} from "react";

import {ApiClientError} from "@/api/client";

interface ConfirmRequiredProps {
    title: string;
    description: string;
    /** 主按钮文案（如 "Grant" / "Revoke" / "Retry"）。 */
    confirmLabel: string;
    /** 触发控件：必传，单击会打开 modal。 */
    children: ReactNode;
    /** 真正执行的动作；抛错会被 modal 捕获并展示。 */
    onConfirm: () => Promise<void> | void;
    /** 双击窗口（毫秒），默认 5000。 */
    windowMs?: number;
    /** 透传 className 到触发器外层 <span>。 */
    triggerClassName?: string;
}

const backdropStyle: CSSProperties = {
    position: "fixed",
    inset: 0,
    background: "rgba(0, 0, 0, 0.55)",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    zIndex: 50,
};

const panelStyle: CSSProperties = {
    width: "min(100%, 460px)",
    maxHeight: "calc(100vh - 4rem)",
    overflowY: "auto",
    padding: "1.4rem 1.2rem",
    background: "var(--bg-elev-2)",
    border: "1px solid var(--border-strong)",
    borderRadius: "var(--radius-lg)",
    boxShadow: "0 24px 60px rgba(0, 0, 0, 0.55)",
    color: "var(--fg)",
};

const titleStyle: CSSProperties = {
    margin: "0 0 0.4rem",
    fontSize: "1.1rem",
    fontWeight: 600,
};

const descStyle: CSSProperties = {
    margin: "0 0 0.9rem",
    color: "var(--fg-muted)",
    fontSize: "0.92rem",
    lineHeight: 1.5,
};

const hintStyle: CSSProperties = {
    margin: "0.8rem 0 1.2rem",
    padding: "0.6rem 0.8rem",
    border: "1px dashed var(--border-strong)",
    borderRadius: "var(--radius-sm)",
    background: "var(--bg-elev)",
    color: "var(--fg-muted)",
    fontSize: "0.84rem",
};

const actionsStyle: CSSProperties = {
    display: "flex",
    justifyContent: "flex-end",
    gap: "0.6rem",
};

const errorStyle: CSSProperties = {
    margin: "0.4rem 0 0.8rem",
    padding: "0.6rem 0.8rem",
    border: "1px solid rgba(244, 63, 94, 0.3)",
    background: "rgba(244, 63, 94, 0.07)",
    color: "#fda4af",
    borderRadius: "var(--radius-sm)",
    fontSize: "0.86rem",
};

const triggerStyle: CSSProperties = {
    display: "inline-block",
};

export function ConfirmRequired({
    title,
    description,
    confirmLabel,
    children,
    onConfirm,
    windowMs = 5000,
    triggerClassName,
}: ConfirmRequiredProps) {
    const [open, setOpen] = useState(false);
    const [firstClickedAt, setFirstClickedAt] = useState<number | null>(null);
    const [remaining, setRemaining] = useState(0);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const countdownRef = useRef<number | null>(null);

    // Countdown：第一次点击后每 100ms 刷新一次，windowMs 到点重置。
    useEffect(() => {
        if (firstClickedAt === null) {
            setRemaining(0);
            return;
        }
        const tick = () => {
            const left = Math.max(0, windowMs - (Date.now() - firstClickedAt));
            setRemaining(left);
            if (left <= 0) {
                setFirstClickedAt(null);
                if (countdownRef.current !== null) {
                    window.clearInterval(countdownRef.current);
                    countdownRef.current = null;
                }
            }
        };
        tick();
        countdownRef.current = window.setInterval(tick, 100);
        return () => {
            if (countdownRef.current !== null) {
                window.clearInterval(countdownRef.current);
                countdownRef.current = null;
            }
        };
    }, [firstClickedAt, windowMs]);

    // Esc 关闭。
    useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape" && !busy) close();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, busy]);

    function close() {
        setOpen(false);
        setFirstClickedAt(null);
        setError("");
        setBusy(false);
    }

    function handleTriggerClick() {
        setError("");
        setOpen(true);
    }

    async function handleConfirmClick() {
        // 第一次点击 → 进入倒计时，不发请求。
        if (firstClickedAt === null) {
            setFirstClickedAt(Date.now());
            return;
        }
        // 第二次点击（在窗口内）→ 真正执行。
        setBusy(true);
        setError("");
        try {
            await onConfirm();
            close();
        } catch (e) {
            setError(
                e instanceof ApiClientError
                    ? `${e.code}: ${e.message}`
                    : "Action failed.",
            );
        } finally {
            setBusy(false);
        }
    }

    const remainingSec = (remaining / 1000).toFixed(1);
    const armed = firstClickedAt !== null;

    return (
        <>
            <span
                onClick={handleTriggerClick}
                role="button"
                tabIndex={0}
                onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        handleTriggerClick();
                    }
                }}
                className={triggerClassName}
                style={triggerStyle}
            >
                {children}
            </span>

            {open ? (
                <div
                    style={backdropStyle}
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="confirm-required-title"
                    onClick={() => {
                        if (!busy) close();
                    }}
                >
                    <div style={panelStyle} onClick={(e) => e.stopPropagation()}>
                        <h3 id="confirm-required-title" style={titleStyle}>
                            {title}
                        </h3>
                        <p style={descStyle}>{description}</p>

                        {error ? (
                            <div style={errorStyle} role="alert">
                                {error}
                            </div>
                        ) : null}

                        <div style={hintStyle}>
                            {armed ? (
                                <span>
                                    Click <strong>"{confirmLabel}"</strong> again within{" "}
                                    <code>{remainingSec}s</code> to confirm.
                                </span>
                            ) : (
                                <span>
                                    This is a destructive admin action. You will be asked
                                    to click confirm twice within{" "}
                                    {Math.round(windowMs / 1000)}s.
                                </span>
                            )}
                        </div>

                        <footer style={actionsStyle}>
                            <button
                                type="button"
                                className="btn--ghost"
                                onClick={close}
                                disabled={busy}
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                className="btn--primary"
                                onClick={() => void handleConfirmClick()}
                                disabled={busy}
                            >
                                {busy
                                    ? "Working…"
                                    : armed
                                      ? `${confirmLabel} (${remainingSec}s)`
                                      : `I confirm — ${confirmLabel}`}
                            </button>
                        </footer>
                    </div>
                </div>
            ) : null}
        </>
    );
}
