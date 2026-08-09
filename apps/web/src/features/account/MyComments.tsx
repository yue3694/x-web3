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
    approved: "Visible to everyone on the course page.",
    pending: "Awaiting moderation — not yet public.",
    rejected: "Did not pass moderation. Edit and resubmit if you have access.",
};

interface MyCommentsProps {
    /** 可选：嵌入到 UserMenu / Settings 等容器中时复用样式。 */
    className?: string;
}

function formatDate(iso: string): string {
    const date = new Date(iso);
    if (Number.isNaN(date.valueOf())) return iso;
    return new Intl.DateTimeFormat("en-US", {
        year: "numeric",
        month: "short",
        day: "numeric",
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
                setError("Unable to load your comments.");
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
                        <span className="eyebrow">Account</span>
                        <h2>My comments</h2>
                        <p>Sign in to view the discussion history tied to your profile.</p>
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
            setError(cause instanceof ApiClientError ? cause.message : "Failed to delete the comment.");
        } finally {
            setDeletingId(null);
        }
    };

    return (
        <section className={`my-comments panel${className ? ` ${className}` : ""}`} aria-labelledby="my-comments-title">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">Account</span>
                    <h2 id="my-comments-title">My comments</h2>
                    <p>Every comment you have left on a course, including ones still in review.</p>
                </div>
            </div>

            {routeMissing ? (
                <div className="notice notice--error" role="alert">
                    The <code>GET /me/comments</code> endpoint is not wired in the current API build.
                    The backend repository function is ready; please ask the backend track to expose the route.
                </div>
            ) : null}
            {error ? <div className="notice notice--error" role="alert">{error}</div> : null}

            {loading ? (
                <p className="muted">Loading your comments…</p>
            ) : items.length === 0 && !routeMissing ? (
                <div className="empty-state">
                    <span>◇</span>
                    <h3>No comments yet</h3>
                    <p>After purchasing a course, leave a comment to start the conversation.</p>
                </div>
            ) : (
                <ol className="my-comments__list" aria-label="My comments">
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
                                    title="Jump to course"
                                >
                                    course · {comment.courseId.slice(0, 8)}…
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
                                    {deletingId === comment.id ? "Deleting…" : "Delete"}
                                </button>
                            </footer>
                        </li>
                    ))}
                </ol>
            )}
        </section>
    );
}
