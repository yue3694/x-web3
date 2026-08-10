/**
 * AdminLayout — /admin/* 顶层布局。
 *
 * 鉴权策略：
 *   - RequirePermission('admin') 仅做 UX 隐藏；后端 `rbac.Middleware(PermSystemAdmin)`
 *     是权威。前端少一层时显示 fallback 提示，*不*在客户端判断 super_admin。
 *   - 当前项目的 RequirePermission 把 super_admin 一律放行，与本组件假设一致。
 *
 * 路由：
 *   - 当前项目没有 router 库；AdminLayout 暴露一个 `currentPath` prop 让宿主
 *     决定激活态。`/admin` 默认指向 Users 子页。
 *
 * 侧栏导航：Users / Roles / Courses Review / Chain / DLQ / Audit / Certificates Retry。
 * 后三个是高敏感操作（链上 / DLQ / 证书重发 / 审计），无强隔离，UI 上以图标 + 文字
 * 提示用户「进入前确认自己有 SYSTEM_ADMIN 权限」（已经在 RequirePermission 之外
 * 拦截不到的情况下）。
 */

import {type CSSProperties, type ReactNode} from "react";

import {RequirePermission} from "@/auth/RequirePermission";
import {useSession} from "@/auth/SessionContext";

interface NavItem {
    key: string;
    label: string;
    description: string;
    path: string;
    icon: string;
    /** 标 true：进入后端默认会做 SYSTEM_ADMIN 二次校验。 */
    sensitive?: boolean;
}

const NAV_ITEMS: NavItem[] = [
    {
        key: "users",
        label: "Users",
        description: "List users, grant / revoke roles.",
        path: "/admin/users",
        icon: "U",
    },
    {
        key: "roles",
        label: "Roles",
        description: "Role catalog & permission matrix.",
        path: "/admin/roles",
        icon: "R",
    },
    {
        key: "courses-review",
        label: "Courses Review",
        description: "Approve / reject teacher course submissions.",
        path: "/admin/courses-review",
        icon: "C",
    },
    {
        key: "chain",
        label: "Chain",
        description: "Indexing sync status & manual rewind.",
        path: "/admin/chain",
        icon: "⌬",
        sensitive: true,
    },
    {
        key: "dlq",
        label: "DLQ",
        description: "Unresolved dead-letter events & retry.",
        path: "/admin/dlq",
        icon: "D",
        sensitive: true,
    },
    {
        key: "audit",
        label: "Audit",
        description: "Search admin action log.",
        path: "/admin/audit",
        icon: "A",
        sensitive: true,
    },
    {
        key: "certificates-retry",
        label: "Certificates Retry",
        description: "Re-enqueue failed certificate jobs.",
        path: "/admin/certificates-retry",
        icon: "✓",
        sensitive: true,
    },
];

interface AdminLayoutProps {
    children: ReactNode;
    /** 当前路径（用于高亮侧栏）。如 "/admin/users?page=2"。 */
    currentPath: string;
}

const layoutStyle: CSSProperties = {
    display: "grid",
    gridTemplateColumns: "240px 1fr",
    gap: "1.4rem",
    alignItems: "flex-start",
};

const asideStyle: CSSProperties = {
    position: "sticky",
    top: "1.2rem",
    padding: "1rem 0.8rem",
    background: "var(--bg-panel)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-md)",
};

const asideTitleStyle: CSSProperties = {
    margin: "0 0 0.6rem",
    padding: "0 0.4rem",
    color: "var(--accent-2)",
    font: "500 0.72rem/1 var(--font-mono)",
    letterSpacing: "0.12em",
    textTransform: "uppercase",
};

const navListStyle: CSSProperties = {
    listStyle: "none",
    margin: 0,
    padding: 0,
    display: "flex",
    flexDirection: "column",
    gap: "0.15rem",
};

const navItemBase: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "0.6rem",
    padding: "0.55rem 0.6rem",
    borderRadius: "var(--radius-sm)",
    color: "var(--fg)",
    textDecoration: "none",
    fontSize: "0.92rem",
    border: "1px solid transparent",
    transition: "background 160ms ease, border-color 160ms ease",
};

const navIconStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    width: "1.4rem",
    height: "1.4rem",
    borderRadius: "var(--radius-sm)",
    background: "var(--bg-elev)",
    color: "var(--accent-2)",
    font: "600 0.78rem/1 var(--font-mono)",
};

const navLabelStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
};

const navSubStyle: CSSProperties = {
    color: "var(--fg-muted)",
    fontSize: "0.74rem",
};

const mainStyle: CSSProperties = {
    minWidth: 0,
};

const deniedStyle: CSSProperties = {
    padding: "1.4rem",
    border: "1px solid rgba(244, 63, 94, 0.3)",
    background: "rgba(244, 63, 94, 0.07)",
    borderRadius: "var(--radius-md)",
    color: "#fda4af",
};

const sensitiveBadgeStyle: CSSProperties = {
    marginLeft: "auto",
    padding: "0.1rem 0.4rem",
    border: "1px solid rgba(245, 158, 11, 0.4)",
    background: "rgba(245, 158, 11, 0.08)",
    color: "var(--accent-amber)",
    borderRadius: "999px",
    font: "500 0.62rem/1 var(--font-mono)",
    textTransform: "uppercase",
};

function isActive(itemPath: string, current: string): boolean {
    // `/admin/users` 应当高亮 `/admin/users?page=2`。
    if (current === itemPath) return true;
    if (current.startsWith(`${itemPath}/`)) return true;
    return false;
}

export function AdminLayout({children, currentPath}: AdminLayoutProps) {
    const {profile} = useSession();

    return (
        <RequirePermission
            code="admin"
            fallback={
                <div className="panel" style={deniedStyle} role="alert">
                    <strong>Access denied.</strong> You need{" "}
                    <code>SYSTEM_ADMIN</code> permission to view this page.
                    {profile ? (
                        <div style={{marginTop: "0.4rem", color: "var(--fg-muted)"}}>
                            Signed in as <code>{profile.displayName}</code> — ask a super
                            admin to grant <code>admin</code> in Users → Roles.
                        </div>
                    ) : null}
                </div>
            }
        >
            <div style={layoutStyle} className="admin-layout">
                <aside style={asideStyle} aria-label="Admin navigation">
                    <h2 style={asideTitleStyle}>Admin Console</h2>
                    <ul style={navListStyle}>
                        {NAV_ITEMS.map((item) => {
                            const active = isActive(item.path, currentPath);
                            return (
                                <li key={item.key}>
                                    <a
                                        href={item.path}
                                        style={{
                                            ...navItemBase,
                                            background: active
                                                ? "var(--bg-elev)"
                                                : "transparent",
                                            borderColor: active
                                                ? "var(--border-strong)"
                                                : "transparent",
                                        }}
                                        aria-current={active ? "page" : undefined}
                                    >
                                        <span style={navIconStyle} aria-hidden="true">
                                            {item.icon}
                                        </span>
                                        <span style={navLabelStyle}>
                                            <span>{item.label}</span>
                                            <span style={navSubStyle}>
                                                {item.description}
                                            </span>
                                        </span>
                                        {item.sensitive ? (
                                            <span style={sensitiveBadgeStyle}>
                                                sensitive
                                            </span>
                                        ) : null}
                                    </a>
                                </li>
                            );
                        })}
                    </ul>
                </aside>

                <main style={mainStyle} className="admin-layout__main">
                    {children}
                </main>
            </div>
        </RequirePermission>
    );
}
