/**
 * MyComments — 账户中心「我的评论」面板。
 *
 * 行为契约:
 *   - 拉取当前登录用户的所有评论（含 pending / approved / rejected）。
 *   - 软删走 DELETE /courses/comments/{id}；删除后从本地列表移除。
 *   - moderation_status 决定徽章颜色，并附文案说明「为何未公开」。
 *   - 软删的评论后端 ListByCourse 不会再返回，但本组件调用 listMine
 *     在 main.go 尚未挂载路由时可能 404 → 给出明确「API 暂未上线」提示。
 *
 * 设计稿：暂无；按 design.md F02 §2 + Account UX 自定结构。
 */

import {useCallback, useEffect, useState} from "react";

import {ApiClientError} from "@/api/client";
import {commentApi, type Comment, type ModerationStatus} from "@/api/types";
import {useSession} from "@/auth/SessionContext";
import {CommentStatusBadge} from "@/features/catalog/CommentItem";

const STATUS_HINT: Record<ModerationStatus, string> = {
    approved: "已对所有用户公开显示在课程页。",
    pending: "正在等待审核，尚未公开。",
    rejected: "未通过审核。如有权限可编辑后重新提交。",
};

interface MyCommentsProps {
    /** 可选：嵌入到 UserMenu / Settings 等容器中时复用样式。 */
    className?: string;
}

function formatDate(iso: string): string {
    const date = new Date(iso);
    if (Number.isNaN(date.valueOf())) return iso;
    return new Intl.DateTimeFormat("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
    }).format(date);
}

export function MyComments({className}: MyCommentsProps) {
    const {profile, loading: sessionLoading} = useSession();
    const [items, setItems] = useState<Comment[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [routeMissing, setRouteMissing] = useState(false);
    const [deletingId, setDeletingId] = useState<string | null>(null);

    const load = useCallback(async () => {
        if (!profile) return;
        setLoading(true);
        setError("");
        setRouteMissing(false);
        try {
            const page = await commentApi.listMine();
            setItems(page.items);
        } catch (cause) {
            if (cause instanceof ApiClientError) {
                if (cause.status === 404 || cause.status === 405) {
                    // 后端 main.go 暂未挂载 GET /me/comments，优雅降级。
                    setRouteMissing(true);
                } else {
                    setError(cause.message);
                }
            } else {
                setError("无法加载你的评论。");
            }
        } finally {
            setLoading(false);
        }
    }, [profile]);

    useEffect(() => {
        if (sessionLoading) return;
        void load();
    }, [load, sessionLoading]);

    if (!profile) {
        return (
            <section className={`my-comments panel${className ? ` ${className}` : ""}`}>
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">账户</span>
                        <h2>我的评论</h2>
                        <p>请先登录，查看与你账户关联的讨论记录。</p>
                    </div>
                </div>
            </section>
        );
    }

    const onDelete = async (id: string) => {
        if (deletingId) return;
        setDeletingId(id);
        try {
            await commentApi.softDelete(id);
            setItems((current) => current.filter((c) => c.id !== id));
        } catch (cause) {
            setError(cause instanceof ApiClientError ? cause.message : "删除评论失败。");
        } finally {
            setDeletingId(null);
        }
    };

    return (
        <section className={`my-comments panel${className ? ` ${className}` : ""}`} aria-labelledby="my-comments-title">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">账户</span>
                    <h2 id="my-comments-title">我的评论</h2>
                    <p>你在课程下发表过的所有评论，包括仍在审核中的。</p>
                </div>
            </div>

            {routeMissing ? (
                <div className="notice notice--error" role="alert">
                    当前 API 尚未挂载 <code>GET /me/comments</code> 路由。
                    后端 repository 已就绪，请联系后端同学开放该接口。
                </div>
            ) : null}
            {error ? <div className="notice notice--error" role="alert">{error}</div> : null}

            {loading ? (
                <p className="muted">评论加载中…</p>
            ) : items.length === 0 && !routeMissing ? (
                <div className="empty-state">
                    <span>◇</span>
                    <h3>暂无评论</h3>
                    <p>购买课程后，发表第一条评论来开启讨论吧。</p>
                </div>
            ) : (
                <ol className="my-comments__list" aria-label="我的评论">
                    {items.map((comment) => (
                        <li key={comment.id} className={`my-comments__item my-comments__item--${comment.moderationStatus}`}>
                            <header>
                                <div>
                                    <CommentStatusBadge status={comment.moderationStatus} />
                                    <time className="my-comments__time" dateTime={comment.createdAt}>
                                        {formatDate(comment.createdAt)}
                                    </time>
                                </div>
                                <a
                                    className="my-comments__course"
                                    href={`#course-${comment.courseId}`}
                                    title="跳到课程"
                                >
                                    课程 · {comment.courseId.slice(0, 8)}…
                                </a>
                            </header>
                            <p className="my-comments__body">{comment.body}</p>
                            <footer className="my-comments__foot">
                                <span className="muted">{STATUS_HINT[comment.moderationStatus]}</span>
                                <button
                                    type="button"
                                    className="btn--ghost btn--danger-ghost"
                                    disabled={deletingId === comment.id}
                                    onClick={() => void onDelete(comment.id)}
                                >
                                    {deletingId === comment.id ? "删除中…" : "删除"}
                                </button>
                            </footer>
                        </li>
                    ))}
                </ol>
            )}
        </section>
    );
}
