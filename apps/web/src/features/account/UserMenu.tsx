/**
 * UserMenu — 当前登录用户的下拉菜单。
 *
 * 钱包连接与网络管理统一交给 TopNav 的 ConnectKit。
 * 这里只展示平台账户信息：displayName、角色和登出。
 */

import {useEffect, useRef, useState} from "react";
import {formatUnits, type Address} from "viem";
import {useDisconnect, useReadContract} from "wagmi";

import {useSession} from "@/auth/SessionContext";
import {authApi} from "@/api/types";
import {useNotify} from "@/components/NotifyProvider";
import {erc20Abi} from "@/contracts/erc20.abi";
import {ydTokenDeployments} from "@/contracts/deployments";

interface UserMenuProps {
    /** 触发按钮的 aria-label */
    label?: string;
    wallet: {
        connected: boolean;
        connecting: boolean;
        address?: Address;
        displayAddress?: string;
        network: string;
        wrongChain: boolean;
        manage: () => void;
    };
}

export function UserMenu({label = "打开账户菜单", wallet}: UserMenuProps) {
    const {profile, logout, hasRole, setAuthenticatedProfile} = useSession();
    const {disconnect} = useDisconnect();
    const [open, setOpen] = useState(false);
    const [editingName, setEditingName] = useState(false);
    const [name, setName] = useState(profile?.displayName ?? "");
    const [savingName, setSavingName] = useState(false);
    const {notify} = useNotify();
    const rootRef = useRef<HTMLDivElement | null>(null);
    const ydAddress = ydTokenDeployments.target.address;
    const ydBalance = useReadContract({
        address: ydAddress,
        abi: erc20Abi,
        functionName: "balanceOf",
        args: wallet.address ? [wallet.address] : undefined,
        chainId: ydTokenDeployments.target.chainId,
        query: {
            enabled: Boolean(wallet.connected && wallet.address && ydAddress),
            refetchInterval: 12_000,
        },
    });

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

    useEffect(() => {
        if (open && wallet.connected && wallet.address) void ydBalance.refetch();
    }, [open, wallet.connected, wallet.address]);

    if (!profile) return null;

    const onLogout = async () => {
        await logout();
        disconnect();
        setOpen(false);
    };

    const initial = (profile.displayName?.[0] ?? "?").toUpperCase();
    const isSuper = hasRole("super_admin");
    const walletStatus = wallet.connecting ? "连接中…" : wallet.connected ? wallet.network : "钱包未连接";
    const ydAmount = ydBalance.data === undefined ? "—" : formatYDBalance(ydBalance.data);

    const manageWallet = () => {
        setOpen(false);
        wallet.manage();
    };

    const saveName = async () => {
        const nextName = name.trim();
        if (nextName.length < 2) return;
        setSavingName(true);
        try {
            setAuthenticatedProfile(await authApi.updateProfile(nextName));
            setEditingName(false);
            notify("昵称修改成功。", "success");
        } catch (cause) {
            notify(cause instanceof Error ? cause.message : "昵称修改失败", "error");
        } finally {
            setSavingName(false);
        }
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
                        {wallet.connected && wallet.address ? <span className="wallet-chip__addr">{wallet.displayAddress ?? wallet.address}</span> : null}
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
                        <h3>Profile</h3>
                        {editingName ? (
                            <div className="user-menu__name-editor">
                                <input maxLength={40} value={name} onChange={(event) => setName(event.target.value)} aria-label="昵称" />
                                <button className="btn--primary" type="button" disabled={savingName || name.trim().length < 2} onClick={() => void saveName()}>{savingName ? "保存中…" : "保存"}</button>
                                <button className="btn--ghost" type="button" onClick={() => setEditingName(false)}>取消</button>
                            </div>
                        ) : (
                            <button className="user-menu__profile-action" type="button" onClick={() => { setName(profile.displayName); setEditingName(true); }}>
                                <span><strong>{profile.displayName}</strong><small>修改昵称</small></span><span aria-hidden="true">→</span>
                            </button>
                        )}
                    </section>

                    <section className="user-menu__section">
                        <h3>钱包</h3>
                        <div className="user-menu__token-balance" aria-label="YD 余额">
                            <span><small>YD 余额</small><strong>{ydAmount} YD</strong></span>
                            <span className="user-menu__token-mark" aria-hidden="true">YD</span>
                        </div>
                        <button type="button" className="user-menu__wallet-action" onClick={manageWallet}>
                            <span className="wallet-chip__dot" aria-hidden="true" />
                            <span>
                                <strong>{walletStatus}</strong>
                                <small>{wallet.connected ? (wallet.displayAddress ?? wallet.address) : "连接钱包以继续链上操作"}</small>
                            </span>
                            <span aria-hidden="true">→</span>
                        </button>
                    </section>

                    <footer className="user-menu__footer">
                        <button type="button" className="btn--danger-ghost" onClick={() => void onLogout()}>
                            退出登录
                        </button>
                    </footer>
                </div>
            ) : null}
        </div>
    );
}

function formatYDBalance(value: bigint): string {
    const raw = formatUnits(value, 18);
    const [whole, fraction = ""] = raw.split(".");
    const visibleFraction = fraction.slice(0, 4).replace(/0+$/, "");
    return visibleFraction ? `${whole}.${visibleFraction}` : whole;
}
