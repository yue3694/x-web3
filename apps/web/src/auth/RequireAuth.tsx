/**
 * 受保护路由包装：
 *   - 未登录 → 渲染 children 的占位 + "请登录"提示（或自定义 fallback）；
 *   - 已登录 → 渲染 children；
 *
 * 真正的鉴权由后端 RBAC 决定；这里仅控制是否展示 UI。
 */

import type {ReactNode} from "react";
import {useSession} from "./SessionContext";

interface RequireAuthProps {
    children: ReactNode;
    /** 自定义未登录占位 */
    fallback?: ReactNode;
}

export function RequireAuth({children, fallback}: RequireAuthProps) {
    const {profile, loading} = useSession();

    if (loading) {
        return <div role="status">Loading…</div>;
    }
    if (!profile) {
        return (
            fallback ?? (
                <div role="alert">Please sign in to access this page.</div>
            )
        );
    }
    return <>{children}</>;
}