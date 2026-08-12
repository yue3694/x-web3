/**
 * CommentItem — 单条评论渲染（状态徽章 + 作者 + 时间 + 软删按钮）。
 *
 * 由 Comments / MyComments 复用。Moderation 颜色由全局 .comment-status--{status} 控制。
 */

import type {Comment, ModerationStatus} from "@/api/types";

const STATUS_LABEL: Record<ModerationStatus, string> = {
    approved: "已通过",
    pending: "待审核",
    rejected: "未通过",
};

export function formatCommentTime(iso: string): string {
    const date = new Date(iso);
    if (Number.isNaN(date.valueOf())) return iso;
    return new Intl.DateTimeFormat("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
    }).format(date);
}

export function CommentStatusBadge({status}: {status: ModerationStatus}) {
    return (
        <span className={`comment-status comment-status--${status}`}>
            {STATUS_LABEL[status]}
        </span>
    );
}

interface CommentItemProps {
    comment: Comment;
    isAuthor: boolean;
    deleting?: boolean;
    onDelete?: (id: string) => void;
    /** 列表项类名，覆盖默认 .comment-item（用于 MyComments 等不同容器）。 */
    className?: string;
}

export function CommentItem({comment, isAuthor, deleting, onDelete, className}: CommentItemProps) {
    const itemClass = className ?? "comment-item";
    return (
        <li className={itemClass}>
            <header className="comment-item__head">
                <div>
                    <span className="comment-item__author">
                        {comment.userDisplayName || (isAuthor ? "我" : "匿名用户")}
                    </span>
                    <time className="comment-item__time" dateTime={comment.createdAt}>
                        {formatCommentTime(comment.createdAt)}
                    </time>
                </div>
                <CommentStatusBadge status={comment.moderationStatus} />
            </header>
            <p className="comment-item__body">{comment.body}</p>
            {isAuthor && onDelete ? (
                <footer className="comment-item__foot">
                    <button
                        type="button"
                        className="btn--ghost btn--danger-ghost comment-item__delete"
                        disabled={deleting}
                        onClick={() => onDelete(comment.id)}
                    >
                        {deleting ? "删除中…" : "删除"}
                    </button>
                </footer>
            ) : null}
        </li>
    );
}
