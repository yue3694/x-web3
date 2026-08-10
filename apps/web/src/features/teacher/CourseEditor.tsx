/**
 * CourseEditor — 教师课程编辑器顶层组件。
 *
 * 结构：
 *   - 元数据表单 (title / description / price) → courseApi.create / update
 *   - 章节拖拽排序 (ChapterReorderList) + 课时增删 + 媒体上传 (MediaUploader)
 *   - 章节草稿整体保存：courseApi.replaceCurriculum（PUT + If-Match 乐观锁）
 *
 * 数据流：
 *   create → 返回 Course（拿到 id + currentVersion）→ 草稿 curriculum 进入编辑器
 *   addChapter / addLesson / removeChapter / removeLesson → setChapters
 *   reorderChapter → setChapters（基于 drag list）
 *   uploadLessonMedia(chapterId, lessonId, asset) → setChapters（写 lesson.mediaAssetId）
 *   saveCurriculum → courseApi.replaceCurriculum；版本号回填到 Course.currentVersion
 *     STALE_VERSION → 友好提示 + 引导 reload（不做强制刷新，避免丢失草稿）
 *
 * F02-T12 完成度：
 *   - [x] 章节拖拽（ChapterReorderList，原生 HTML5 dnd + 键盘）
 *   - [x] 章节 / 课时增删 + 标题编辑
 *   - [x] 课时媒体上传（绑定到 lesson.mediaAssetId）
 *   - [x] 整体 curriculum 保存（PUT + 乐观锁 + STALE_VERSION UX）
 *   - [x] 保存冲突提示（F02-T16 联动：STALE_VERSION 时弹窗，offer reload）
 */

import {useEffect, useMemo, useState, type FormEvent} from "react";

import {ApiClientError} from "@/api/client";
import {courseApi, type Course} from "@/api/types";

import {ChapterReorderList, type ChapterReorderItem} from "./ChapterReorderList";
import {MediaUploader} from "./MediaUploader";
import type {DraftChapter, DraftLesson, MediaAsset} from "./teacherTypes";
import {createDraftChapter, createDraftLesson, isCurriculumValid, toCurriculumInput} from "./teacherTypes";

function seedChapters(): DraftChapter[] {
    return [
        {...createDraftChapter(), title: "Welcome & orientation"},
        {...createDraftChapter(), title: "Core concepts"},
        {...createDraftChapter(), title: "Hands-on project"},
    ];
}

function lessonKey(chapter: DraftChapter, lesson: DraftLesson): string {
    return `${chapter.clientId}::${lesson.clientId}`;
}

export function CourseEditor() {
    const [title, setTitle] = useState("");
    const [description, setDescription] = useState("");
    const [price, setPrice] = useState("0");
    const [course, setCourse] = useState<Course | null>(null);
    const [chapters, setChapters] = useState<DraftChapter[]>(seedChapters);
    const [busy, setBusy] = useState(false);
    const [savingCurriculum, setSavingCurriculum] = useState(false);
    const [message, setMessage] = useState("");
    const [error, setError] = useState("");
    /** stale-version 冲突信号：true 时下方出现 "Discard / Reload latest" 双按钮。 */
    const [staleConflict, setStaleConflict] = useState(false);

    const valid = useMemo(() => title.trim().length > 0 && Number(price) >= 0, [title, price]);
    const curriculumValid = useMemo(() => isCurriculumValid(chapters), [chapters]);

    const reorderItems: ChapterReorderItem<DraftChapter>[] = useMemo(
        () => chapters.map((c) => ({id: c.clientId, title: c.title || "(untitled chapter)", payload: c})),
        [chapters],
    );

    // 创建草稿课程（POST /teacher/courses）→ 拿到 id + currentVersion
    async function create(event: FormEvent) {
        event.preventDefault();
        if (!valid) return;
        setBusy(true); setMessage(""); setError(""); setStaleConflict(false);
        try {
            const slug = `${title.toLowerCase().trim().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}-${Date.now().toString(36)}`;
            const created = await courseApi.create({slug, title, description, priceMinor: Math.round(Number(price) * 100), currency: "USD"});
            setCourse(created);
            setMessage("Draft saved. Add curriculum below and click 'Save curriculum' before submitting for review.");
        } catch (cause) {
            setError(cause instanceof ApiClientError ? cause.message : "Could not create the draft.");
        } finally { setBusy(false); }
    }

    // 提交审批（仅 draft → pending_review；state 由后端决定）
    async function submit() {
        if (!course) return;
        setBusy(true); setMessage(""); setError("");
        try {
            const updated = await courseApi.submit(course.id);
            setCourse(updated);
            setMessage("Course submitted for admin review.");
        } catch (cause) {
            setError(cause instanceof ApiClientError ? cause.message : "Could not submit the course.");
        } finally { setBusy(false); }
    }

    // curriculum 整体保存（PUT + If-Match）→ 成功回填版本；失败若是 STALE_VERSION 提示冲突
    async function saveCurriculum() {
        if (!course) return;
        if (!curriculumValid) {
            setError("Every chapter and lesson needs a non-empty title.");
            return;
        }
        setSavingCurriculum(true); setMessage(""); setError(""); setStaleConflict(false);
        try {
            const body = {chapters: toCurriculumInput(chapters)};
            const resp = await courseApi.replaceCurriculum(course.id, course.currentVersion, body);
            setCourse({...course, currentVersion: resp.currentVersion});
            setMessage(`Curriculum saved (v${resp.currentVersion}).`);
        } catch (cause) {
            if (cause instanceof ApiClientError && cause.code === "STALE_VERSION") {
                setStaleConflict(true);
                setError("This course was updated elsewhere. Reload the latest version or discard your changes.");
            } else {
                setError(cause instanceof ApiClientError ? cause.message : "Could not save curriculum.");
            }
        } finally { setSavingCurriculum(false); }
    }

    // STALE_VERSION → 用户选 "Reload latest" 时丢弃本地草稿、回填最新版本
    async function reloadLatest() {
        if (!course) return;
        setBusy(true); setError(""); setMessage(""); setStaleConflict(false);
        try {
            const fresh = await courseApi.get(course.id);
            setCourse(fresh.course);
            setChapters(
                fresh.chapters.map((chapter) => ({
                    clientId: `chapter-${chapter.id}`,
                    title: chapter.title,
                    lessons: chapter.lessons.map((lesson) => ({
                        clientId: `lesson-${lesson.id}`,
                        title: lesson.title,
                        required: lesson.required,
                        durationSeconds: lesson.durationSeconds,
                    })),
                })),
            );
            setMessage(`Reloaded v${fresh.course.currentVersion} from server.`);
        } catch (cause) {
            setError(cause instanceof ApiClientError ? cause.message : "Could not reload the course.");
        } finally { setBusy(false); }
    }

    function discardLocalEdits() {
        setStaleConflict(false);
        setError("");
        setMessage("Local edits kept. Re-save will overwrite the latest server version.");
    }

    // --- 章节操作 ---
    function handleReorder(next: ChapterReorderItem<DraftChapter>[]) {
        setChapters(next.map((item) => item.payload));
    }
    function addChapter() {
        setChapters((current) => [...current, createDraftChapter()]);
    }
    function removeChapter(clientId: string) {
        setChapters((current) => current.filter((c) => c.clientId !== clientId));
    }
    function updateChapterTitle(clientId: string, value: string) {
        setChapters((current) => current.map((c) => (c.clientId === clientId ? {...c, title: value} : c)));
    }

    // --- 课时操作 ---
    function addLesson(chapterClientId: string) {
        setChapters((current) => current.map((c) => (c.clientId === chapterClientId ? {...c, lessons: [...c.lessons, createDraftLesson()]} : c)));
    }
    function removeLesson(chapterClientId: string, lessonClientId: string) {
        setChapters((current) => current.map((c) => (c.clientId === chapterClientId ? {...c, lessons: c.lessons.filter((l) => l.clientId !== lessonClientId)} : c)));
    }
    function updateLesson(chapterClientId: string, lessonClientId: string, patch: Partial<DraftLesson>) {
        setChapters((current) => current.map((c) => (
            c.clientId === chapterClientId
                ? {...c, lessons: c.lessons.map((l) => (l.clientId === lessonClientId ? {...l, ...patch} : l))}
                : c
        )));
    }
    function uploadLessonMedia(chapterClientId: string, lessonClientId: string, asset: MediaAsset) {
        updateLesson(chapterClientId, lessonClientId, {mediaAssetId: asset.id});
        setMessage(`Attached ${asset.s3Key.split("/").pop()} to lesson.`);
    }

    // 若用户在 idle 状态下已经有 dirty 草稿，按下 enter 不会误触发 create
    useEffect(() => { setStaleConflict(false); }, [chapters, title, description, price]);

    return (
        <section className="teacher-studio panel" aria-labelledby="studio-title">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">Teacher workspace</span>
                    <h2 id="studio-title">Course studio</h2>
                    <p>Build a versioned course draft and send it through onchain university review.</p>
                </div>
                {course ? <span className={`status-pill status-pill--${course.status}`}>{course.status.replace("_", " ")}</span> : null}
            </div>

            <form className="editor-grid card" onSubmit={create}>
                <label><span>Course title</span><input required maxLength={160} value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Smart Contract Security" /></label>
                <label><span>Price (USD)</span><input required min="0" step="0.01" type="number" value={price} onChange={(event) => setPrice(event.target.value)} /></label>
                <label className="editor-grid__wide"><span>Description</span><textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder="What will students be able to build?" rows={5} /></label>
                <div className="editor-actions editor-grid__wide">
                    <button className="btn--primary" disabled={busy || !valid || course?.status === "pending_review"} type="submit">{busy ? "Saving..." : course ? "Create another draft" : "Create draft"}</button>
                    {course?.status === "draft" ? <button className="btn--ghost" disabled={busy || savingCurriculum} onClick={() => void submit()} type="button">Submit review</button> : null}
                    <span>{message}</span>
                </div>
            </form>

            <div className="editor-curriculum card" aria-label="Curriculum">
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">Curriculum</span>
                        <h3>Chapters</h3>
                        <p>Drag to reorder, add lessons, and upload media. Save before submitting for review.</p>
                    </div>
                    {course ? <span className="muted">v{course.currentVersion}</span> : null}
                </div>

                <ChapterReorderList
                    items={reorderItems}
                    onReorder={handleReorder}
                    renderItem={() => null}
                />

                {chapters.length === 0 ? <div className="empty-state"><span>◇</span><h3>No chapters yet</h3><p>Click "Add chapter" to start.</p></div> : null}

                <ol className="editor-curriculum__chapters" aria-label="Chapter list">
                    {chapters.map((chapter, index) => (
                        <li key={chapter.clientId} className="editor-chapter">
                            <div className="editor-chapter__head">
                                <span className="editor-chapter__index">{index + 1}</span>
                                <input
                                    className="editor-chapter__title"
                                    value={chapter.title}
                                    placeholder={`Chapter ${index + 1} title`}
                                    onChange={(event) => updateChapterTitle(chapter.clientId, event.target.value)}
                                    aria-label={`Chapter ${index + 1} title`}
                                />
                                <button className="btn--ghost btn--danger" type="button" onClick={() => removeChapter(chapter.clientId)} aria-label={`Remove chapter ${index + 1}`}>Remove</button>
                            </div>
                            <ol className="editor-chapter__lessons">
                                {chapter.lessons.map((lesson) => (
                                    <li key={lesson.clientId} className="editor-lesson">
                                        <input
                                            className="editor-lesson__title"
                                            value={lesson.title}
                                            placeholder="Lesson title"
                                            onChange={(event) => updateLesson(chapter.clientId, lesson.clientId, {title: event.target.value})}
                                            aria-label={`Lesson title in chapter ${index + 1}`}
                                        />
                                        <label className="editor-lesson__required">
                                            <input
                                                type="checkbox"
                                                checked={lesson.required}
                                                onChange={(event) => updateLesson(chapter.clientId, lesson.clientId, {required: event.target.checked})}
                                            />
                                            <span>Required</span>
                                        </label>
                                        <label className="editor-lesson__duration">
                                            <span>Sec</span>
                                            <input
                                                type="number"
                                                min="0"
                                                value={lesson.durationSeconds}
                                                onChange={(event) => updateLesson(chapter.clientId, lesson.clientId, {durationSeconds: Math.max(0, Number(event.target.value) || 0)})}
                                            />
                                        </label>
                                        <button className="btn--ghost btn--danger" type="button" onClick={() => removeLesson(chapter.clientId, lesson.clientId)} aria-label="Remove lesson">×</button>
                                    </li>
                                ))}
                            </ol>
                            <div className="editor-lesson__add">
                                <button className="btn--ghost" type="button" onClick={() => addLesson(chapter.clientId)}>+ Add lesson</button>
                            </div>
                            {/* media upload per lesson */}
                            <div className="editor-lesson__media">
                                <h4>Media</h4>
                                {chapter.lessons.map((lesson) => (
                                    <div key={lessonKey(chapter, lesson)} className="editor-lesson__media-row">
                                        <span className="editor-lesson__media-label">{lesson.title || "(untitled lesson)"}</span>
                                        {lesson.mediaAssetId ? (
                                            <span className="status-pill status-pill--ready">attached · {lesson.mediaAssetId.slice(0, 8)}</span>
                                        ) : (
                                            <MediaUploader
                                                label="Attach video"
                                                onUploaded={(asset) => uploadLessonMedia(chapter.clientId, lesson.clientId, asset)}
                                            />
                                        )}
                                    </div>
                                ))}
                            </div>
                        </li>
                    ))}
                </ol>

                <div className="editor-actions">
                    <button className="btn--ghost" type="button" onClick={addChapter}>+ Add chapter</button>
                    {course ? (
                        <button className="btn--primary" type="button" disabled={savingCurriculum || !curriculumValid || course.status !== "draft"} onClick={() => void saveCurriculum()}>
                            {savingCurriculum ? "Saving curriculum..." : "Save curriculum"}
                        </button>
                    ) : (
                        <span className="muted">Create a draft first to save curriculum.</span>
                    )}
                </div>

                {error ? <div className="notice notice--error" role="alert">{error}</div> : null}
                {staleConflict ? (
                    <div className="notice notice--warn" role="alert">
                        <strong>Save conflict.</strong> Another tab updated this course. Reload latest to drop your edits, or keep editing and force-save.
                        <div className="editor-actions">
                            <button className="btn--ghost" type="button" onClick={() => void reloadLatest()}>Reload latest</button>
                            <button className="btn--ghost" type="button" onClick={discardLocalEdits}>Keep my edits</button>
                        </div>
                    </div>
                ) : null}
            </div>
        </section>
    );
}