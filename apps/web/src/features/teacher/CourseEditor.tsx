/**
 * CourseEditor — 教师课程编辑器顶层组件。
 *
 * 结构：
 *   - 元数据表单 (title / description / price)
 *   - 章节拖拽排序 (ChapterReorderList)
 *   - 课时媒体上传 (MediaUploader)
 *
 * 当前版本仍走纯前端草稿状态；保存提交走 courseApi.create / submit。
 * F02 阶段后端 PUT /teacher/courses/:id/curriculum 尚未对接前端，本组件
 * 在 UI 层提供 reorder + media flow，后续接入只需把 reorder/onReorder 透传。
 */

import {useMemo, useState, type FormEvent} from "react";

import {ApiClientError} from "@/api/client";
import {courseApi, type Course} from "@/api/types";

import {ChapterReorderList, type ChapterReorderItem} from "./ChapterReorderList";
import {MediaUploader} from "./MediaUploader";
import type {MediaAsset} from "./teacherTypes";

interface DraftChapterSeed {
    id: string;
    title: string;
    asset?: MediaAsset;
}

function seedChapters(): DraftChapterSeed[] {
    return [
        {id: cryptoId("ch"), title: "Welcome & orientation"},
        {id: cryptoId("ch"), title: "Core concepts"},
        {id: cryptoId("ch"), title: "Hands-on project"},
    ];
}

function cryptoId(prefix: string): string {
    const uuid = globalThis.crypto?.randomUUID?.();
    return uuid ? `${prefix}-${uuid}` : `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

export function CourseEditor() {
    const [title, setTitle] = useState("");
    const [description, setDescription] = useState("");
    const [price, setPrice] = useState("0");
    const [course, setCourse] = useState<Course | null>(null);
    const [busy, setBusy] = useState(false);
    const [message, setMessage] = useState("");
    const [chapters, setChapters] = useState<DraftChapterSeed[]>(seedChapters);

    const reorderItems: ChapterReorderItem<DraftChapterSeed>[] = useMemo(
        () => chapters.map((c) => ({id: c.id, title: c.title, payload: c})),
        [chapters],
    );

    async function create(event: FormEvent) {
        event.preventDefault();
        setBusy(true); setMessage("");
        try {
            const slug = `${title.toLowerCase().trim().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}-${Date.now().toString(36)}`;
            const created = await courseApi.create({slug, title, description, priceMinor: Math.round(Number(price) * 100), currency: "USD"});
            setCourse(created); setMessage("Draft saved. Add curriculum before submitting for review.");
        } catch (cause) {
            setMessage(cause instanceof ApiClientError ? cause.message : "Could not create the draft.");
        } finally { setBusy(false); }
    }

    async function submit() {
        if (!course) return;
        setBusy(true); setMessage("");
        try { const updated = await courseApi.submit(course.id); setCourse(updated); setMessage("Course submitted for admin review."); }
        catch (cause) { setMessage(cause instanceof ApiClientError ? cause.message : "Could not submit the course."); }
        finally { setBusy(false); }
    }

    function handleReorder(next: ChapterReorderItem<DraftChapterSeed>[]) {
        setChapters(next.map((item) => item.payload));
    }

    function handleMediaUploaded(chapterId: string, asset: MediaAsset) {
        setChapters((current) => current.map((c) => (c.id === chapterId ? {...c, asset} : c)));
        setMessage(`Uploaded ${asset.s3Key.split("/").pop()} (${asset.contentType}).`);
    }

    return (
        <section className="teacher-studio panel" aria-labelledby="studio-title">
            <div className="section-heading"><div><span className="eyebrow">Teacher workspace</span><h2 id="studio-title">Course studio</h2><p>Build a versioned course draft and send it through onchain university review.</p></div>{course ? <span className={`status-pill status-pill--${course.status}`}>{course.status.replace("_", " ")}</span> : null}</div>
            <form className="editor-grid card" onSubmit={create}>
                <label><span>Course title</span><input required maxLength={160} value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Smart Contract Security" /></label>
                <label><span>Price (USD)</span><input required min="0" step="0.01" type="number" value={price} onChange={(event) => setPrice(event.target.value)} /></label>
                <label className="editor-grid__wide"><span>Description</span><textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder="What will students be able to build?" rows={5} /></label>
                <div className="editor-actions editor-grid__wide"><button className="btn--primary" disabled={busy || course?.status === "pending_review"} type="submit">{busy ? "Saving..." : course ? "Create another draft" : "Create draft"}</button>{course?.status === "draft" ? <button className="btn--ghost" disabled={busy} onClick={() => void submit()} type="button">Submit review</button> : null}<span>{message}</span></div>
            </form>

            <div className="editor-curriculum card" aria-label="Curriculum">
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">Curriculum</span>
                        <h3>Chapters</h3>
                        <p>Drag to reorder. Each chapter can hold media uploads.</p>
                    </div>
                </div>
                <ChapterReorderList items={reorderItems} onReorder={handleReorder} renderItem={(item) => (
                    item.payload.asset ? (
                        <span className="chapter-reorder__asset" title={item.payload.asset.s3Key}>
                            {item.payload.asset.s3Key.split("/").pop()} · ready
                        </span>
                    ) : (
                        <span className="muted">No media</span>
                    )
                )} />
                <ul className="editor-curriculum__upload" aria-label="Media uploads per chapter">
                    {chapters.map((chapter) => (
                        <li key={chapter.id} className="editor-curriculum__upload-row">
                            <span className="editor-curriculum__upload-label">{chapter.title}</span>
                            <MediaUploader
                                label={chapter.asset ? "Replace media" : "Upload media"}
                                onUploaded={(asset) => handleMediaUploaded(chapter.id, asset)}
                            />
                        </li>
                    ))}
                </ul>
            </div>
        </section>
    );
}