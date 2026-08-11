import {ConnectKitButton} from "connectkit";
import {useEffect, useState} from "react";
import {Link, NavLink, useLocation} from "react-router-dom";

import {RequirePermission} from "@/auth/RequirePermission";
import {SignInButton} from "@/auth/SignInButton";
import {useSession} from "@/auth/SessionContext";
import {UserMenu} from "@/features/account/UserMenu";
import {TARGET_CHAIN_ID, TARGET_CHAIN_NAME} from "@/chains";

const LINKS = [
    {to: "/courses", label: "Courses"},
    {to: "/swap", label: "Swap"},
    {to: "/account/enrollments", label: "My learning"},
];

function Brand() {
    return (
        <Link to="/" className="nav__brand" aria-label="Web3 University home">
            <span className="nav__mark" aria-hidden="true">◆</span>
            <span className="nav__brand-text"><span className="nav__brand-name">WEB3 UNIVERSITY</span><span className="nav__brand-sub">{TARGET_CHAIN_NAME} · v0.1</span></span>
        </Link>
    );
}

function AccountActions() {
    const {profile} = useSession();
    return (
        <ConnectKitButton.Custom>
            {({isConnected, isConnecting, show, address, truncatedAddress, chain}) => {
                const wrongChain = chain?.id !== undefined && chain.id !== TARGET_CHAIN_ID;
                if (profile) {
                    return (
                        <UserMenu
                            wallet={{
                                connected: isConnected,
                                connecting: isConnecting,
                                address: truncatedAddress ?? address,
                                network: chain?.name ?? "Unknown",
                                wrongChain,
                                manage: () => show?.(),
                            }}
                        />
                    );
                }
                return (
                    <>
                        <button type="button" className="btn btn--primary nav__cta" disabled={isConnecting} onClick={() => show?.()}>
                            {isConnecting ? "Linking…" : isConnected ? "Manage wallet" : "Connect wallet"}
                        </button>
                        <SignInButton className="btn btn--ghost">Sign in</SignInButton>
                    </>
                );
            }}
        </ConnectKitButton.Custom>
    );
}

export function TopNav() {
    const {pathname} = useLocation();
    const [scrolled, setScrolled] = useState(false);
    const [menuOpen, setMenuOpen] = useState(false);
    useEffect(() => {
        const onScroll = () => setScrolled(window.scrollY > 8);
        onScroll();
        window.addEventListener("scroll", onScroll, {passive: true});
        return () => window.removeEventListener("scroll", onScroll);
    }, []);
    useEffect(() => setMenuOpen(false), [pathname]);

    const navClass = ({isActive}: {isActive: boolean}) => `nav__link${isActive ? " is-active" : ""}`;
    return (
        <header className={`nav${scrolled ? " nav--scrolled" : ""}`}>
            <div className="nav__inner">
                <Brand />
                <nav className={`nav__links${menuOpen ? " is-open" : ""}`} aria-label="Primary">
                    {LINKS.map((link) => <NavLink key={link.to} to={link.to} className={navClass}>{link.label}</NavLink>)}
                    <RequirePermission code="COURSE_CREATE"><NavLink to="/studio" className={navClass}>Studio</NavLink></RequirePermission>
                    <RequirePermission code="SYSTEM_ADMIN"><NavLink to="/admin" className={navClass}>Admin</NavLink></RequirePermission>
                </nav>
                <div className="nav__cluster">
                    <AccountActions />
                    <button type="button" className="nav__menu" aria-label="Toggle navigation" aria-expanded={menuOpen} onClick={() => setMenuOpen((value) => !value)}><span /><span /></button>
                </div>
            </div>
        </header>
    );
}
