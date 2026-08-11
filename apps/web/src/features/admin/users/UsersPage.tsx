/**
 * UsersPage — Admin → Users 子页。
 *
 * 行为：
 *   - 拉取 GET /admin/users?page=&pageSize=；
 *   - 列表展示 email / 钱包数 / 角色 chip，每行嵌 <UserRow> 内的 ConfirmRequired 弹窗；
 *   - 分页走 prev / next + 直接跳页；服务端 404/405 时显示"接口未上线"降级。
 *
 * 设计取舍：
 *   - 角色全集（availableRoles）暂时硬编码在常量中，等后端 /admin/roles 落地后
 *     替换为 fetch；当前不阻塞用户故事。
 *   - 鉴权：RequirePermission 已经在 AdminLayout 里包过一层；本组件依然调用
 *     `useSession().hasPermission('admin')` 进一步保护渲染。
 */

import {useCallback, useEffect, useState} from "react";

import {ApiClientError} from "@/api/client";
import {useSession} from "@/auth/SessionContext";
import {adminApi} from "@/features/admin/adminApi";
import type {AdminRoleCode, AdminUser} from "@/features/admin/adminTypes";

import {UserRow} from "./UserRow";

const KNOWN_ROLES: readonly AdminRoleCode[] = ["student", "teacher", "super_admin"];

const DEFAULT_PAGE_SIZE = 20;

const sectionStyle = {marginTop: 0} as const;

const headerStyle = {
    marginBottom: "0.8rem",
} as const;

const tableHeadStyle = {
    display: "grid",
    gridTemplateColumns: "1.6fr 0.8fr 1.4fr 1.6fr",
    gap: "0.8rem",
    padding: "0.5rem 0.9rem",
    color: "var(--fg-muted)",
    font: "500 0.74rem/1 var(--font-mono)",
    letterSpacing: "0.08em",
    textTransform: "uppercase",
    borderBottom: "1px solid var(--border)",
} as const;

const listStyle = {
    listStyle: "none",
    margin: 0,
    padding: 0,
} as const;

const pagerStyle = {
    display: "flex",
    alignItems: "center",
    gap: "0.6rem",
    marginTop: "0.8rem",
    justifyContent: "flex-end",
    color: "var(--fg-muted)",
    fontSize: "0.84rem",
} as const;

const errorBoxStyle = {
    margin: "0.6rem 0",
    padding: "0.6rem 0.8rem",
    border: "1px solid rgba(244, 63, 94, 0.3)",
    background: "rgba(244, 63, 94, 0.07)",
    color: "#fda4af",
    borderRadius: "var(--radius-sm)",
} as const;

const noticeStyle = {
    margin: "0.6rem 0",
    padding: "0.6rem 0.8rem",
    border: "1px dashed var(--border-strong)",
    background: "var(--bg-elev)",
    color: "var(--fg-muted)",
    borderRadius: "var(--radius-sm)",
    fontSize: "0.86rem",
} as const;

export function UsersPage() {
    const {hasPermission} = useSession();
    const [page, setPage] = useState(1);
    const [items, setItems] = useState<AdminUser[]>([]);
    const [total, setTotal] = useState(0);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [routeMissing, setRouteMissing] = useState(false);

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        setRouteMissing(false);
        try {
            const resp = await adminApi.listUsers(page, DEFAULT_PAGE_SIZE);
            setItems(resp.items);
            setTotal(resp.total);
        } catch (cause) {
            if (cause instanceof ApiClientError) {
                if (cause.status === 404 || cause.status === 405) {
                    setRouteMissing(true);
                } else {
                    setError(`${cause.code}: ${cause.message}`);
                }
            } else {
                setError("Unable to load users.");
            }
        } finally {
            setLoading(false);
        }
    }, [page]);

    useEffect(() => {
        if (!hasPermission("SYSTEM_ADMIN")) return;
        void load();
    }, [load, hasPermission]);

    if (!hasPermission("SYSTEM_ADMIN")) {
        // AdminLayout 已经渲染了"无权访问"，这里直接 null 即可。
        return null;
    }

    const lastPage = Math.max(1, Math.ceil(total / DEFAULT_PAGE_SIZE));
    const canPrev = page > 1;
    const canNext = page < lastPage;

    return (
            <section className="panel" style={sectionStyle} aria-labelledby="users-title">
                <header style={headerStyle}>
                    <span className="eyebrow">管理 · 用户</span>
                    <h2 id="users-title">用户与角色</h2>
                    <p style={{color: "var(--fg-muted)", margin: 0}}>
                        授予或收回系统角色，所有变更都会写入审计日志。
                    </p>
                </header>

                {routeMissing ? (
                    <div className="notice notice--error" role="alert" style={noticeStyle}>
                        当前 API 版本尚未挂载 <code>GET /admin/users</code> 接口，请联系后端在
                        （handler：<code>apps/api/internal/admin/handlers/users.go</code>）暴露该路由。
                    </div>
                ) : null}

                {error && !routeMissing ? (
                    <div role="alert" style={errorBoxStyle}>
                        {error}{" "}
                        <button
                            type="button"
                            className="btn--ghost"
                            onClick={() => void load()}
                        >
                            重试
                        </button>
                    </div>
                ) : null}

                <div role="table" aria-rowcount={total}>
                    <div role="row" style={tableHeadStyle}>
                        <span role="columnheader">用户</span>
                        <span role="columnheader">钱包</span>
                        <span role="columnheader">角色</span>
                        <span role="columnheader" style={{textAlign: "right"}}>
                            操作
                        </span>
                    </div>

                    {loading ? (
                        <ol style={listStyle} aria-busy="true" aria-label="正在加载用户">
                            {[0, 1, 2].map((i) => (
                                <li
                                    key={i}
                                    style={{
                                        height: 56,
                                        borderTop: "1px solid var(--border)",
                                    }}
                                />
                            ))}
                        </ol>
                    ) : items.length === 0 && !routeMissing && !error ? (
                        <div className="empty-state" style={noticeStyle}>
                            <span>◇</span>
                            <h3>暂无用户</h3>
                            <p>还没有账号登录过。</p>
                        </div>
                    ) : (
                        <ol style={listStyle} aria-label="用户列表">
                            {items.map((u) => (
                                <UserRow
                                    key={u.id}
                                    user={u}
                                    availableRoles={KNOWN_ROLES}
                                    onChanged={() => void load()}
                                />
                            ))}
                        </ol>
                    )}
                </div>

                <footer style={pagerStyle} aria-label="分页">
                    <span>
                        第 {page} / {lastPage} 页 · 共 {total} 条
                    </span>
                    <button
                        type="button"
                        className="btn--ghost"
                        onClick={() => setPage((p) => Math.max(1, p - 1))}
                        disabled={!canPrev || loading}
                    >
                        ← 上一页
                    </button>
                    <button
                        type="button"
                        className="btn--ghost"
                        onClick={() => setPage((p) => p + 1)}
                        disabled={!canNext || loading}
                    >
                        下一页 →
                    </button>
                </footer>
            </section>
    );
}

export default UsersPage;
