/**
 * 单条用户记录行 + 角色操作。
 *
 * 行为：
 *   - 展示 email / displayName / walletsCount / 角色 chip 列表 / 创建与最近登录时间；
 *   - 角色操作全部走 <ConfirmRequired> 双击确认，避免误授予 super_admin；
 *   - 父组件传入 onChange 回调，触发列表 refetch（不在组件内 useEffect 写副作用）。
 */

import {type CSSProperties} from "react";

import {adminApi} from "@/features/admin/adminApi";
import {ConfirmRequired} from "@/features/admin/ConfirmRequired";
import type {AdminRoleCode, AdminUser} from "@/features/admin/adminTypes";

interface UserRowProps {
    user: AdminUser;
    /** 可授予的角色全集（从后端 /admin/roles 拉取，父组件传入）。 */
    availableRoles: readonly AdminRoleCode[];
    onChanged: () => void;
}

const KNOWN_ROLES: readonly AdminRoleCode[] = ["student", "teacher", "super_admin"];

const rowStyle: CSSProperties = {
    display: "grid",
    gridTemplateColumns: "1.6fr 0.8fr 1.4fr 1.6fr",
    gap: "0.8rem",
    alignItems: "center",
    padding: "0.8rem 0.9rem",
    borderTop: "1px solid var(--border)",
};

const cellMuted: CSSProperties = {
    color: "var(--fg-muted)",
    fontSize: "0.84rem",
};

const rolesStyle: CSSProperties = {
    display: "flex",
    flexWrap: "wrap",
    gap: "0.3rem",
};

const actionsStyle: CSSProperties = {
    display: "flex",
    flexWrap: "wrap",
    gap: "0.35rem",
    justifyContent: "flex-end",
};

const smallBtnStyle: CSSProperties = {
    padding: "0.25rem 0.55rem",
    fontSize: "0.78rem",
};

function roleLabel(code: AdminRoleCode): string {
    if (code === "super_admin") return "超级管理员";
    if (code === "teacher") return "讲师";
    if (code === "student") return "学生";
    return code;
}

function rolePillTone(code: AdminRoleCode): string {
    if (code === "super_admin") return "status-pill status-pill--pending_review";
    if (code === "teacher") return "status-pill status-pill--confirmed";
    return "status-pill status-pill--draft";
}

export function UserRow({user, availableRoles, onChanged}: UserRowProps) {
    const grantable = availableRoles.filter((r) => !user.roles.includes(r));
    const revokable = user.roles.filter((r) => r !== "super_admin" || KNOWN_ROLES.includes(r));

    return (
        <li style={rowStyle} className="user-row">
            <div>
                <div style={{fontWeight: 600}}>{user.displayName || "—"}</div>
                <div style={cellMuted}>
                    <code>{user.email}</code>
                </div>
            </div>

            <div style={cellMuted}>
                <span style={{color: "var(--fg)", fontWeight: 500}}>
                    {user.walletsCount}
                </span>{" "}
                个钱包
            </div>

            <div style={rolesStyle} aria-label="角色">
                {user.roles.length === 0 ? (
                    <span style={cellMuted}>无角色</span>
                ) : (
                    user.roles.map((r) => (
                        <span key={r} className={rolePillTone(r)}>
                            {roleLabel(r)}
                        </span>
                    ))
                )}
            </div>

            <div style={actionsStyle}>
                {grantable.map((r) => (
                    <ConfirmRequired
                        key={`grant-${r}`}
                        title={`授予角色：${roleLabel(r)}`}
                        description={`授予 "${r}" 给 ${user.email} 将写入审计日志，请确认这是有意操作。`}
                        confirmLabel="授予"
                        onConfirm={async () => {
                            await adminApi.grantRole(user.id, {role: r});
                            onChanged();
                        }}
                    >
                        <button type="button" className="btn--ghost" style={smallBtnStyle}>
                            + {roleLabel(r)}
                        </button>
                    </ConfirmRequired>
                ))}

                {revokable.map((r) => (
                    <ConfirmRequired
                        key={`revoke-${r}`}
                        title={`收回角色：${roleLabel(r)}`}
                        description={`从 ${user.email} 收回 "${r}" 将写入审计日志。`}
                        confirmLabel="收回"
                        onConfirm={async () => {
                            await adminApi.revokeRole(user.id, r);
                            onChanged();
                        }}
                    >
                        <button
                            type="button"
                            className="btn--ghost"
                            style={{...smallBtnStyle, color: "var(--accent-rose)"}}
                        >
                            − {roleLabel(r)}
                        </button>
                    </ConfirmRequired>
                ))}
            </div>
        </li>
    );
}
