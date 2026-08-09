/**
 * AccountMenu — 当前用户菜单。
 *   - 显示 display name + primary wallet；
 *   - 列出已绑钱包 + 解绑按钮；
 *   - 隐藏超管入口（按 F01 R-ID-006）。
 */

import {useDisconnect} from "wagmi";

import {authApi} from "@/api/types";
import {ApiClientError} from "@/api/client";
import {useSession} from "@/auth/SessionContext";
import {WalletLink} from "@/features/wallet/WalletLink";
import {useState} from "react";

export function AccountMenu() {
    const {profile, refresh, logout, hasRole} = useSession();
	const {disconnect} = useDisconnect();
    const [busy, setBusy] = useState<string | null>(null);

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
    };

    return (
        <section className="account-menu">
            <h2>
                {profile.displayName}
                {hasRole("super_admin") ? (
                    <span style={{marginLeft: 8, opacity: 0.6}}>· super_admin</span>
                ) : null}
            </h2>
            <h3>Wallets</h3>
            <ul>
                {profile.wallets.map((w) => (
                    <li key={w.id}>
                        <code>{w.address}</code> (chainId {w.chainId})
                        <button
                            type="button"
                            onClick={() => onUnbind(w.id)}
                            disabled={busy === w.id}
                        >
                            {busy === w.id ? "Unbinding…" : "Unbind"}
                        </button>
                    </li>
                ))}
            </ul>
            <WalletLink onLinked={refresh} />
            <button type="button" onClick={onLogout}>
                Sign out
            </button>
        </section>
    );
}
