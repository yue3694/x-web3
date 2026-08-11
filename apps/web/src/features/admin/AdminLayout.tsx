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

import {type CSSProperties} from "react";
import {NavLink, Outlet} from "react-router-dom";

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
        label: "用户",
        description: "查看用户、授予 / 收回角色。",
        path: "/admin/users",
        icon: "U",
    },
    {
        key: "courses",
        label: "课程",
        description: "审核并发布提交上来的课程。",
        path: "/admin/courses",
        icon: "C",
        sensitive: true,
    },
    {
        key: "chain",
        label: "链状态",
        description: "索引同步状态与手动回滚。",
        path: "/admin/chain",
        icon: "⌬",
        sensitive: true,
    },
    {
        key: "dlq",
        label: "死信队列",
        description: "未处理的死信事件与重试。",
        path: "/admin/dlq",
        icon: "D",
        sensitive: true,
    },
];

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

export function AdminLayout() {
    const {profile} = useSession();

    return (
        <RequirePermission
            code="SYSTEM_ADMIN"
            fallback={
                <div className="panel" style={deniedStyle} role="alert">
                    <strong>无权访问。</strong>你当前缺少 <code>SYSTEM_ADMIN</code> 权限，无法查看该页面。
                    {profile ? (
                        <div style={{marginTop: "0.4rem", color: "var(--fg-muted)"}}>
                            当前登录身份：<code>{profile.displayName}</code> — 请联系超级管理员授予 <code>SYSTEM_ADMIN</code> 权限。
                        </div>
                    ) : null}
                </div>
            }
        >
            <div style={layoutStyle} className="admin-layout">
                <aside style={asideStyle} aria-label="Admin navigation">
                    <h2 style={asideTitleStyle}>管理控制台</h2>
                    <ul style={navListStyle}>
                        {NAV_ITEMS.map((item) => (
                                <li key={item.key}>
                                    <NavLink to={item.path} style={({isActive}) => ({...navItemBase, background: isActive ? "var(--bg-elev)" : "transparent", borderColor: isActive ? "var(--border-strong)" : "transparent"})}>
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
                                                高敏感
                                            </span>
                                        ) : null}
                                    </NavLink>
                                </li>
                        ))}
                    </ul>
                </aside>

                <main style={mainStyle} className="admin-layout__main">
                    <Outlet />
                </main>
            </div>
        </RequirePermission>
    );
}

export default AdminLayout;
