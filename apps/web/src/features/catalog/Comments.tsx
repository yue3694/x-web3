/**
 * Comments — 课程详情页评论区。
 *
 * 行为契约 (F02-T09 / packages/shared/openapi/course.yaml):
 *   - 列表永远可拉（未登录也返回 approved 列表；后端天然不返回他人的 pending）。
 *   - 写评论需登录 + 已购买；未登录 → 引导登录；未购买 → 展示
 *     "purchase required" 提示。
 *   - 渲染时按 moderation_status 给出徽章：approved / pending / rejected。
 *     自己写的 pending / rejected 仍展示（与后端 ListByCourse 一致）。
 *   - 作者可对自己未删除的评论做软删（DELETE /courses/comments/{id}）。
 *
 * 设计稿：暂无；按 design.md F02 §2 的"已购买校验 + 审核状态 + 软删除"实现。
 */

import {useCallback, useEffect, useState, type FormEvent} from "react";

import {ApiClientError} from "@/api/client";
import {commentApi, type Comment} from "@/api/types";
import {useSession} from "@/auth/SessionContext";

import {CommentItem} from "./CommentItem";

const BODY_MAX = 2000;

interface CommentsProps {
    courseId: string;
    courseTitle?: string;
}

interface SubmitState {
    busy: boolean;
    error: string;
}

export function Comments({courseId, courseTitle}: CommentsProps) {
    const {profile} = useSession();
    const [items, setItems] = useState<Comment[]>([]);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const [draft, setDraft] = useState("");
    const [submit, setSubmit] = useState<SubmitState>({busy: false, error: ""});
    const [deletingId, setDeletingId] = useState<string | null>(null);

    const viewerId = profile?.id ?? "";

    const load = useCallback(async () => {
        setLoading(true);
        setLoadError("");
        try {
            const page = await commentApi.listByCourse(courseId);
            setItems(page.items);
        } catch (cause) {
            setLoadError(cause instanceof ApiClientError ? cause.message : "无法加载评论。");
        } finally {
            setLoading(false);
        }
    }, [courseId]);

    useEffect(() => {
        void load();
    }, [load]);

    const onSubmit = async (event: FormEvent) => {
        event.preventDefault();
        if (submit.busy) return;
        const body = draft.trim();
        if (body.length === 0) {
            setSubmit({busy: false, error: "评论内容不能为空。"});
            return;
        }
        if (body.length > BODY_MAX) {
            setSubmit({busy: false, error: `评论最多 ${BODY_MAX} 个字符。`});
            return;
        }
        setSubmit({busy: true, error: ""});
        try {
            const created = await commentApi.create(courseId, body);
            setItems((current) => [created, ...current]);
            setDraft("");
        } catch (cause) {
            if (cause instanceof ApiClientError) {
                if (cause.code === "COMMENT_NOT_PURCHASED") {
                    setSubmit({busy: false, error: "只有购买过的学员才能在本课程留言。"});
                } else if (cause.status === 401) {
                    setSubmit({busy: false, error: "请先登录后再留言。"});
                } else {
                    setSubmit({busy: false, error: cause.message});
                }
            } else {
                setSubmit({busy: false, error: "提交评论失败。"});
            }
        }
    };

    const onDelete = async (commentId: string) => {
        if (deletingId) return;
        setDeletingId(commentId);
        try {
            await commentApi.softDelete(commentId);
            setItems((current) => current.filter((c) => c.id !== commentId));
        } catch (cause) {
            setLoadError(cause instanceof ApiClientError ? cause.message : "删除评论失败。");
        } finally {
            setDeletingId(null);
        }
    };

    return (
        <section className="comments" aria-labelledby="comments-title">
            <header className="comments__header">
                <div>
                    <span className="eyebrow">讨论区</span>
                    <h3 id="comments-title">
                        {courseTitle ? `《${courseTitle}》的评论` : "评论"}
                    </h3>
                    <p className="comments__hint">
                        只有购买过的学员可以留言。新评论需经审核后才会公开展示。
                    </p>
                </div>
            </header>

            <form className="comments__compose card" onSubmit={onSubmit}>
                <label className="sr-only" htmlFor="comment-body">你的评论</label>
                <textarea
                    id="comment-body"
                    value={draft}
                    onChange={(event) => setDraft(event.target.value)}
                    placeholder={profile ? "分享你的学习心得..." : "请先登录并购买课程后再留言。"}
                    rows={3}
                    maxLength={BODY_MAX}
                    disabled={!profile || submit.busy}
                />
                <div className="comments__compose-foot">
                    <span className="comments__count">{draft.length} / {BODY_MAX}</span>
                    <button
                        type="submit"
                        className="btn--primary"
                        disabled={!profile || submit.busy || draft.trim().length === 0}
                    >
                        {submit.busy ? "提交中…" : "发表评论"}
                    </button>
                </div>
                {submit.error ? (
                    <div className="notice notice--error" role="alert">{submit.error}</div>
                ) : null}
            </form>

            {loadError ? (
                <div className="notice notice--error" role="alert">{loadError}</div>
            ) : null}

            {loading ? (
                <p className="comments__empty">评论加载中…</p>
            ) : items.length === 0 ? (
                <div className="empty-state">
                    <span>◇</span>
                    <h3>暂无评论</h3>
                    <p>完成报名后，欢迎第一个分享你的看法。</p>
                </div>
            ) : (
                <ol className="comments__list" aria-label="评论列表">
                    {items.map((comment) => (
                        <CommentItem
                            key={comment.id}
                            comment={comment}
                            isAuthor={viewerId !== "" && comment.userId === viewerId}
                            deleting={deletingId === comment.id}
                            onDelete={onDelete}
                        />
                    ))}
                </ol>
            )}
        </section>
    );
}
