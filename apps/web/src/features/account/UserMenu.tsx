/**
 * UserMenu — 当前登录用户的下拉菜单。
 *
 * 钱包连接与网络管理统一交给 TopNav 的 ConnectKit。
 * 这里只展示平台账户信息：displayName、角色和登出。
 */

import {useEffect, useRef, useState} from "react";
import {useDisconnect} from "wagmi";

import {useSession} from "@/auth/SessionContext";

interface UserMenuProps {
    /** 触发按钮的 aria-label */
    label?: string;
    wallet: {
        connected: boolean;
        connecting: boolean;
        address?: string;
        network: string;
        wrongChain: boolean;
        manage: () => void;
    };
}

export function UserMenu({label = "Open account menu", wallet}: UserMenuProps) {
    const {profile, logout, hasRole} = useSession();
    const {disconnect} = useDisconnect();
    const [open, setOpen] = useState(false);
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

    const onLogout = async () => {
        await logout();
        disconnect();
        setOpen(false);
    };

    const initial = (profile.displayName?.[0] ?? "?").toUpperCase();
    const isSuper = hasRole("super_admin");
    const walletStatus = wallet.connecting ? "Connecting…" : wallet.connected ? wallet.network : "Wallet disconnected";

    const manageWallet = () => {
        setOpen(false);
        wallet.manage();
    };

    return (
        <div className="user-menu" ref={rootRef}>
            <button
                type="button"
                className={`user-menu__trigger${wallet.wrongChain ? " user-menu__trigger--warn" : ""}${!wallet.connected ? " user-menu__trigger--offline" : ""}`}
                aria-haspopup="menu"
                aria-expanded={open}
                aria-label={label}
                onClick={() => setOpen((v) => !v)}
            >
                <span className="user-menu__avatar" aria-hidden="true">{initial}</span>
                <span className="user-menu__summary">
                    <span className="user-menu__name">{profile.displayName}</span>
                    <span className="user-menu__wallet-summary">
                        <span className="wallet-chip__dot" aria-hidden="true" />
                        <span className="wallet-chip__net">{walletStatus}</span>
                        {wallet.connected && wallet.address ? <span className="wallet-chip__addr">{wallet.address}</span> : null}
                    </span>
                </span>
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
                        <h3>Wallet</h3>
                        <button type="button" className="user-menu__wallet-action" onClick={manageWallet}>
                            <span className="wallet-chip__dot" aria-hidden="true" />
                            <span>
                                <strong>{walletStatus}</strong>
                                <small>{wallet.connected ? wallet.address : "Connect to continue with onchain actions"}</small>
                            </span>
                            <span aria-hidden="true">→</span>
                        </button>
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
