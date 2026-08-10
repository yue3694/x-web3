/**
 * TopNav — 站点主导航。
 *
 * 结构：
 *   [brand] [nav links] ............ [wallet chip · sign in / user menu]
 *
 * 设计原则：
 *   - 永远 sticky，背景半透明 + blur；
 *   - wallet 连接状态只在这里展示一次（chip），避免与 UserMenu 内容重复；
 *   - nav links 中 Teacher / Admin 由 RequirePermission 控制可见性，
 *     不会绕过服务端鉴权。
 */

import {ConnectKitButton} from "connectkit";
import {useEffect, useState} from "react";

import {useSession} from "@/auth/SessionContext";
import {RequirePermission} from "@/auth/RequirePermission";
import {SignInButton} from "@/auth/SignInButton";
import {UserMenu} from "@/features/account/UserMenu";

interface NavLink {
    href: string;
    label: string;
}

const LINKS: NavLink[] = [
    {href: "#catalog", label: "Catalog"},
    {href: "#swap", label: "Swap"},
    {href: "#account", label: "Account"},
    {href: "#protocol", label: "Protocol"},
    {href: "#contact", label: "Contact"},
];

function Brand() {
    return (
        <a href="#top" className="nav__brand" aria-label="Web3 University home">
            <span className="nav__mark" aria-hidden="true">◆</span>
            <span className="nav__brand-text">
                <span className="nav__brand-name">WEB3 UNIVERSITY</span>
                <span className="nav__brand-sub">Sepolia · v0.1</span>
            </span>
        </a>
    );
}

function WalletChip() {
    return (
        <ConnectKitButton.Custom>
            {({isConnected, isConnecting, show, address, truncatedAddress, chain}) => {
                if (!isConnected) {
                    return (
                        <button
                            type="button"
                            className="btn btn--primary nav__cta"
                            disabled={isConnecting}
                            onClick={show}
                        >
                            {isConnecting ? "Linking Wallet…" : "Connect Wallet"}
                        </button>
                    );
                }
                const wrongChain = chain?.id !== undefined && chain.id !== 11155111;
                return (
                    <button
                        type="button"
                        className={`wallet-chip${wrongChain ? " wallet-chip--warn" : ""}`}
                        onClick={show}
                        title={wrongChain ? "Wrong network — switch to Sepolia" : "Manage wallet"}
                    >
                        <span className="wallet-chip__dot" aria-hidden="true" />
                        <span className="wallet-chip__net">
                            {chain?.name ?? "Unknown"} · {chain?.id ?? "—"}
                        </span>
                        <span className="wallet-chip__addr">{truncatedAddress ?? address}</span>
                    </button>
                );
            }}
        </ConnectKitButton.Custom>
    );
}

export function TopNav() {
    const {profile} = useSession();
    const [scrolled, setScrolled] = useState(false);

    useEffect(() => {
        const onScroll = () => setScrolled(window.scrollY > 8);
        onScroll();
        window.addEventListener("scroll", onScroll, {passive: true});
        return () => window.removeEventListener("scroll", onScroll);
    }, []);

    return (
        <header className={`nav${scrolled ? " nav--scrolled" : ""}`}>
            <div className="nav__inner">
                <Brand />

                <nav className="nav__links" aria-label="Primary">
                    {LINKS.map((link) => (
                        <a key={link.href} href={link.href} className="nav__link">
                            {link.label}
                        </a>
                    ))}
                    <RequirePermission code="COURSE_CREATE">
                        <a href="#studio" className="nav__link nav__link--accent">
                            Teacher Studio
                        </a>
                    </RequirePermission>
                    <RequirePermission code="SYSTEM_ADMIN">
                        <a href="#admin" className="nav__link nav__link--accent">
                            Admin
                        </a>
                    </RequirePermission>
                </nav>

                <div className="nav__cluster">
                    <WalletChip />
                    {profile ? (
                        <UserMenu />
                    ) : (
                        <SignInButton className="btn btn--ghost">
                            Sign in
                        </SignInButton>
                    )}
                </div>
            </div>
        </header>
    );
}