/**
 * 权限门组件：缺权限时隐藏 UI（仅 UX，权威在后端）。
 *
 * 命名注意：后端对应 Permission 表里的 code：
 *   COURSE_CREATE / COURSE_EDIT / COURSE_APPROVE / SYSTEM_ADMIN / ...
 */

import type {ReactNode} from "react";
import {useSession} from "./SessionContext";

interface RequirePermissionProps {
    code: string;
    children: ReactNode;
    fallback?: ReactNode;
}

export function RequirePermission({code, children, fallback}: RequirePermissionProps) {
    const {hasPermission} = useSession();
    if (!hasPermission(code)) {
        return fallback ? <>{fallback}</> : null;
    }
    return <>{children}</>;
}