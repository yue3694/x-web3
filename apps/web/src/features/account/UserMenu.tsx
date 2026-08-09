/**
 * UserMenu — 当前登录用户的下拉菜单。
 *
 * 与 TopNav 中的 wallet chip 配合：钱包地址/网络仅在 TopNav 显示一次，
 * 这里只展示「账户级别的」信息：displayName、角色、所有绑定钱包、解绑、登出。
 */

import {useEffect, useRef, useState} from "react";
import {useDisconnect} from "wagmi";

import {authApi} from "@/api/types";
import {ApiClientError} from "@/api/client";
import {useSession} from "@/auth/SessionContext";
import {WalletLink} from "@/features/wallet/WalletLink";

interface UserMenuProps {
    /** 触发按钮的 aria-label */
    label?: string;
}

export function UserMenu({label = "Open account menu"}: UserMenuProps) {
    const {profile, refresh, logout, hasRole} = useSession();
    const {disconnect} = useDisconnect();
    const [open, setOpen] = useState(false);
    const [busy, setBusy] = useState<string | null>(null);
    const rootRef = useRef<HTMLDivElement | null>(null);

    // 点击外部或 Esc 关闭。
    useEffect(() => {
        if (!open) return;
        const onDocClick = (e: MouseEvent) => {
            if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
        };
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") setOpen(false);
        };
        document.addEventListener("mousedown", onDocClick);
        document.addEventListener("keydown", onKey);
        return () => {
            document.removeEventListener("mousedown", onDocClick);
            document.removeEventListener("keydown", onKey);
        };
    }, [open]);

    if (!profile) return null;

    const onUnbind = async (id: string) => {
        setBusy(id);
        try {
            await authApi.unbindWallet(id);
            await refresh();
        } catch (e) {
            const msg =
                e instanceof ApiClientError
                    ? `${e.code}: ${e.message}`
                    : "unbind failed";
            // 静默失败时也提示（这里只 console；UI 弹窗可在后续 PR 加）
            console.error(msg);
        } finally {
            setBusy(null);
        }
    };

    const onLogout = async () => {
        await logout();
        disconnect();
        setOpen(false);
    };

    const initial = (profile.displayName?.[0] ?? "?").toUpperCase();
    const isSuper = hasRole("super_admin");

    return (
        <div className="user-menu" ref={rootRef}>
            <button
                type="button"
                className="user-menu__trigger"
                aria-haspopup="menu"
                aria-expanded={open}
                aria-label={label}
                onClick={() => setOpen((v) => !v)}
            >
                <span className="user-menu__avatar" aria-hidden="true">{initial}</span>
                <span className="user-menu__name">{profile.displayName}</span>
                <span className="user-menu__caret" aria-hidden="true">▾</span>
            </button>

            {open ? (
                <div className="user-menu__panel" role="menu">
                    <header className="user-menu__header">
                        <span className="user-menu__title">{profile.displayName}</span>
                        {isSuper ? (
                            <span className="badge badge--warn">super_admin</span>
                        ) : profile.roles.length ? (
                            <span className="badge">{profile.roles.join(" · ")}</span>
                        ) : null}
                    </header>

                    <section className="user-menu__section">
                        <h3>Wallets</h3>
                        {profile.wallets.length === 0 ? (
                            <p className="muted">No wallets bound yet.</p>
                        ) : (
                            <ul className="user-menu__wallets">
                                {profile.wallets.map((w) => (
                                    <li key={w.id}>
                                        <code>{w.address}</code>
                                        <span className="user-menu__chain">chain {w.chainId}</span>
                                        <button
                                            type="button"
                                            className="btn--ghost"
                                            onClick={() => void onUnbind(w.id)}
                                            disabled={busy === w.id}
                                        >
                                            {busy === w.id ? "Unbinding…" : "Unbind"}
                                        </button>
                                    </li>
                                ))}
                            </ul>
                        )}
                        <WalletLink onLinked={refresh} />
                    </section>

                    <footer className="user-menu__footer">
                        <button type="button" className="btn--danger-ghost" onClick={() => void onLogout()}>
                            Sign out
                        </button>
                    </footer>
                </div>
            ) : null}
        </div>
    );
}