import {ConnectButton} from "./components/ConnectButton";
import {Notepad} from "./components/Notepad";
import {AccountMenu} from "./features/account/AccountMenu";
import {SignInButton} from "./auth/SignInButton";
import {RequireAuth} from "./auth/RequireAuth";
import {RequirePermission} from "./auth/RequirePermission";
import {useSession} from "./auth/SessionContext";

export function App() {
    const {profile, hasRole} = useSession();

    return (
        <main className="container">
            <header className="header">
                <h1>
                    x-web3{" "}
                    <span className="glitch">// WEB3 UNIVERSITY</span>
                </h1>
                <p>
                    <span className="blink">█</span> Sepolia testnet · Vite +
                    React + wagmi v2 · ConnectKit
                </p>
                <ConnectButton />
                {!profile ? <SignInButton /> : null}
            </header>

            <RequireAuth
                fallback={
                    <section className="panel">
                        <p>Sign in to access the dashboard.</p>
                    </section>
                }
            >
                <section className="panel">
                    <AccountMenu />
                </section>
            </RequireAuth>

            {/* 隐藏超管入口 — 公开导航中不出现。
                URL 仍可直达 /admin/*，但 API 会 401/403。
                F02/F06 接入后这里换成 admin 路由 + 鉴权。 */}
            <RequirePermission code="SYSTEM_ADMIN">
                <section className="panel admin-slot" hidden={!hasRole("super_admin")}>
                    <Notepad />
                </section>
            </RequirePermission>

            <footer className="footer">
                <span>// system_status: online</span>
                <a
                    href="https://sepolia.etherscan.io/"
                    target="_blank"
                    rel="noreferrer"
                >
                    sepolia.etherscan.io ↗
                </a>
            </footer>
        </main>
    );
}