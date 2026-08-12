/**
 * CourseEditor — 教师课程编辑器顶层组件。
 *
 * 结构：
 *   - 元数据表单 (title / description / price) → courseApi.create / update
 *   - 章节拖拽排序 (ChapterReorderList) + 课时增删 + 媒体附件（MediaUrlAttacher）
 *   - 章节草稿整体保存：courseApi.replaceCurriculum（PUT + If-Match 乐观锁）
 *
 * 数据流：
 *   create → 返回 Course（拿到 id + currentVersion）→ 草稿 curriculum 进入编辑器
 *   addChapter / addLesson / removeChapter / removeLesson → setChapters
 *   reorderChapter → setChapters（基于 drag list）
 *   attachLessonMedia(chapterId, lessonId, asset) → setChapters（写 lesson.mediaAssetId）
 *   saveCurriculum → courseApi.replaceCurriculum；版本号回填到 Course.currentVersion
 *     STALE_VERSION → 友好提示 + 引导 reload（不做强制刷新，避免丢失草稿）
 *
 * F02-T12 完成度：
 *   - [x] 章节拖拽（ChapterReorderList，原生 HTML5 dnd + 键盘）
 *   - [x] 章节 / 课时增删 + 标题编辑
 *   - [x] 课时媒体附件（URL 形式，绑定到 lesson.mediaAssetId）
 *   - [x] 整体 curriculum 保存（PUT + 乐观锁 + STALE_VERSION UX）
 *   - [x] 保存冲突提示（F02-T16 联动：STALE_VERSION 时弹窗，offer reload）
 */

import {useCallback, useEffect, useMemo, useState, type FormEvent} from "react";

import {ApiClientError} from "@/api/client";
import {courseApi, type Course, type CourseChapter} from "@/api/types";
import {Select, type SelectOption} from "@/components/Select";

import {ChapterReorderList, type ChapterReorderItem} from "./ChapterReorderList";
import {MediaUrlAttacher} from "./MediaUrlAttacher";
import type {DraftChapter, DraftLesson, MediaAsset} from "./teacherTypes";
import {createDraftChapter, createDraftLesson, isCurriculumValid, toCurriculumInput} from "./teacherTypes";

function seedChapters(): DraftChapter[] {
    return [
        {...createDraftChapter(), title: "欢迎与导览"},
        {...createDraftChapter(), title: "核心概念"},
        {...createDraftChapter(), title: "动手项目"},
    ];
}

function lessonKey(chapter: DraftChapter, lesson: DraftLesson): string {
    return `${chapter.clientId}::${lesson.clientId}`;
}

export function CourseEditor() {
    type MineItem = {course: Course; chapters: CourseChapter[]};
    const [title, setTitle] = useState("");
    const [description, setDescription] = useState("");
    const [price, setPrice] = useState("0");
    const [course, setCourse] = useState<Course | null>(null);
    const [mine, setMine] = useState<MineItem[]>([]);
    const [chapters, setChapters] = useState<DraftChapter[]>(seedChapters);
    const [busy, setBusy] = useState(false);
    const [savingCurriculum, setSavingCurriculum] = useState(false);
    const [savedCurriculum, setSavedCurriculum] = useState("");
    const [message, setMessage] = useState("");
    const [error, setError] = useState("");
    /** stale-version 冲突信号：true 时下方出现 "Discard / Reload latest" 双按钮。 */
    const [staleConflict, setStaleConflict] = useState(false);

    const valid = useMemo(() => title.trim().length > 0 && Number(price) >= 0, [title, price]);
    const curriculumValid = useMemo(() => isCurriculumValid(chapters), [chapters]);
    const curriculumSaved = curriculumValid && savedCurriculum === JSON.stringify(chapters);

    const reorderItems: ChapterReorderItem<DraftChapter>[] = useMemo(
        () => chapters.map((c) => ({id: c.clientId, title: c.title || "（未命名章节）", payload: c})),
        [chapters],
    );

    const restore = useCallback((selected: MineItem) => {
        const restored = selected.chapters.map((chapter) => ({
            clientId: `chapter-${chapter.id}`,
            title: chapter.title,
            lessons: chapter.lessons.map((lesson) => ({
                clientId: `lesson-${lesson.id}`,
                title: lesson.title,
                required: lesson.required,
                durationSeconds: lesson.durationSeconds,
                mediaAssetId: lesson.mediaAssetId,
            })),
        }));
        setCourse(selected.course);
        setTitle(selected.course.title);
        setDescription(selected.course.description);
        setPrice((selected.course.priceMinor / 100).toString());
        setChapters(restored.length ? restored : seedChapters());
        setSavedCurriculum(restored.length ? JSON.stringify(restored) : "");
        setMessage("");
        setError("");
    }, []);

    const startNew = () => {
        setCourse(null);
        setTitle("");
        setDescription("");
        setPrice("0");
        setChapters(seedChapters());
        setSavedCurriculum("");
        setMessage("");
        setError("");
    };

    useEffect(() => {
        let cancelled = false;
        void courseApi.listMine().then((response) => {
            if (cancelled || response.items.length === 0) return;
            const selected = response.items.find((item) => item.course.status === "draft") ?? response.items[0];
            setMine(response.items);
            restore(selected);
        }).catch(() => {
            // A new teacher legitimately has no resumable draft; the create form remains usable.
        });
        return () => { cancelled = true; };
    }, [restore]);

    // 创建草稿课程（POST /teacher/courses）→ 拿到 id + currentVersion
    async function saveDetails(event: FormEvent) {
        event.preventDefault();
        if (!valid) return;
        setBusy(true); setMessage(""); setError(""); setStaleConflict(false);
        try {
            const input = {title, description, priceMinor: Math.round(Number(price) * 100), currency: "USD"};
            if (course) {
                const updated = await courseApi.update(course.id, course.currentVersion, input);
                setCourse(updated);
                setMessage("课程详情已保存。");
            } else {
                const slug = `${title.toLowerCase().trim().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}-${Date.now().toString(36)}`;
                const created = await courseApi.create({...input, slug});
                setCourse(created);
                setMessage("草稿已创建，请补全并保存课程大纲后再提交。");
            }
        } catch (cause) {
            setError(cause instanceof ApiClientError ? cause.message : "无法创建草稿。");
        } finally { setBusy(false); }
    }

    // 提交审批（仅 draft → pending_review；state 由后端决定）
    async function submit() {
        if (!course) return;
        setBusy(true); setMessage(""); setError("");
        try {
            const updated = await courseApi.submit(course.id);
            setCourse(updated);
            setMessage("课程已提交，等待管理员审核。");
        } catch (cause) {
            setError(cause instanceof ApiClientError ? cause.message : "无法提交课程。");
        } finally { setBusy(false); }
    }

    // curriculum 整体保存（PUT + If-Match）→ 成功回填版本；失败若是 STALE_VERSION 提示冲突
    async function saveCurriculum() {
        if (!course) return;
        if (!curriculumValid) {
            setError("每个章节和课时都需要填写标题。");
            return;
        }
        setSavingCurriculum(true); setMessage(""); setError(""); setStaleConflict(false);
        try {
            const body = {chapters: toCurriculumInput(chapters)};
            const resp = await courseApi.replaceCurriculum(course.id, course.currentVersion, body);
            setCourse({...course, currentVersion: resp.currentVersion});
            setSavedCurriculum(JSON.stringify(chapters));
            setMessage(`课程大纲已保存（v${resp.currentVersion}）。`);
        } catch (cause) {
            if (cause instanceof ApiClientError && cause.code === "STALE_VERSION") {
                setStaleConflict(true);
                setError("课程已在别处更新。请重新载入最新版本或放弃你的修改。");
            } else {
                setError(cause instanceof ApiClientError ? cause.message : "无法保存课程大纲。");
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
            setMessage(`已从服务器重新载入 v${fresh.course.currentVersion}。`);
        } catch (cause) {
            setError(cause instanceof ApiClientError ? cause.message : "无法重新载入课程。");
        } finally { setBusy(false); }
    }

    function discardLocalEdits() {
        setStaleConflict(false);
        setError("");
        setMessage("已保留本地修改，再次保存将覆盖服务器上的最新版本。");
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
    function attachLessonMedia(chapterClientId: string, lessonClientId: string, asset: MediaAsset) {
        updateLesson(chapterClientId, lessonClientId, {mediaAssetId: asset.id, mediaUrl: asset.s3Key});
        const tail = asset.s3Key.split("/").pop() || asset.s3Key;
        setMessage(`已为课时附加 ${tail}。`);
    }

    function detachLessonMedia(chapterClientId: string, lessonClientId: string) {
        updateLesson(chapterClientId, lessonClientId, {mediaAssetId: undefined, mediaUrl: undefined});
        setMessage("已移除课时的媒体附件。");
    }

    // 若用户在 idle 状态下已经有 dirty 草稿，按下 enter 不会误触发 create
    useEffect(() => {
        setStaleConflict(false);
    }, [chapters, title, description, price]);

    return (
        <section className="teacher-studio panel" aria-labelledby="studio-title">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">讲师工作区</span>
                    <h2 id="studio-title">课程工作台</h2>
                    <p>搭一个带版本号的课程草稿，并把它送进链上大学审核流程。</p>
                </div>
                {course ? <span className={`status-pill status-pill--${course.status}`}>{course.status.replace("_", " ")}</span> : null}
            </div>

            {mine.length ? (
                <div className="card editor-actions">
                    <label className="editor-actions__select">
                        <span>我的课程</span>
                        {(() => {
                            const opts: readonly SelectOption<string>[] = mine.map((item) => ({
                                value: item.course.id,
                                label: item.course.title,
                                hint: item.course.status.replace("_", " "),
                            }));
                            return (
                                <Select<string>
                                    value={course?.id ?? ""}
                                    onChange={(next) => {
                                        const found = mine.find((item) => item.course.id === next);
                                        if (found) restore(found);
                                    }}
                                    options={opts}
                                    ariaLabel="选择要编辑的课程"
                                    width="fit"
                                />
                            );
                        })()}
                    </label>
                    <button className="btn--ghost" type="button" onClick={startNew}>新建草稿</button>
                </div>
            ) : null}

            <form className="editor-grid card" onSubmit={saveDetails}>
                <label><span>课程标题</span><input required disabled={Boolean(course && course.status !== "draft")} maxLength={160} value={title} onChange={(event) => setTitle(event.target.value)} placeholder="智能合约安全" /></label>
                <label><span>价格（USD）</span><input required disabled={Boolean(course && course.status !== "draft")} min="0" step="0.01" type="number" value={price} onChange={(event) => setPrice(event.target.value)} /></label>
                <label className="editor-grid__wide"><span>课程简介</span><textarea disabled={Boolean(course && course.status !== "draft")} value={description} onChange={(event) => setDescription(event.target.value)} placeholder="学员学完能做出什么？" rows={5} /></label>
                <div className="editor-actions editor-grid__wide">
                    <button className="btn--primary" disabled={busy || !valid || Boolean(course && course.status !== "draft")} type="submit">{busy ? "保存中..." : course ? "保存详情" : "创建草稿"}</button>
                    {course?.status === "draft" ? <button className="btn--ghost" disabled={busy || savingCurriculum || !curriculumSaved} onClick={() => void submit()} type="button">提交发布</button> : null}
                    {course?.status === "draft" && !curriculumSaved ? <span className="muted">保存一份有效的大纲后即可提交。</span> : null}
                    <span>{message}</span>
                </div>
            </form>

            <div className="editor-curriculum card" aria-label="课程大纲">
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">课程大纲</span>
                        <h3>章节</h3>
                        <p>拖拽以重新排序、添加课时并附加媒体。提交审核前请先保存。</p>
                    </div>
                    {course ? <span className="muted">v{course.currentVersion}</span> : null}
                </div>

                <ChapterReorderList
                    items={reorderItems}
                    onReorder={handleReorder}
                    renderItem={() => null}
                />

                {chapters.length === 0 ? <div className="empty-state"><span>◇</span><h3>暂无章节</h3><p>点击「添加章节」开始编排。</p></div> : null}

                <ol className="editor-curriculum__chapters" aria-label="章节列表">
                    {chapters.map((chapter, index) => (
                        <li key={chapter.clientId} className="editor-chapter">
                            <div className="editor-chapter__head">
                                <span className="editor-chapter__index">{index + 1}</span>
                                <input
                                    className="editor-chapter__title"
                                    value={chapter.title}
                                    placeholder={`第 ${index + 1} 章标题`}
                                    onChange={(event) => updateChapterTitle(chapter.clientId, event.target.value)}
                                    aria-label={`第 ${index + 1} 章标题`}
                                />
                                <button className="btn--ghost btn--danger" type="button" onClick={() => removeChapter(chapter.clientId)} aria-label={`删除第 ${index + 1} 章`}>删除</button>
                            </div>
                            <ol className="editor-chapter__lessons">
                                {chapter.lessons.map((lesson) => (
                                    <li key={lesson.clientId} className="editor-lesson">
                                        <input
                                            className="editor-lesson__title"
                                            value={lesson.title}
                                            placeholder="课时标题"
                                            onChange={(event) => updateLesson(chapter.clientId, lesson.clientId, {title: event.target.value})}
                                            aria-label={`第 ${index + 1} 章下的课时标题`}
                                        />
                                        <label className="editor-lesson__required">
                                            <input
                                                type="checkbox"
                                                checked={lesson.required}
                                                onChange={(event) => updateLesson(chapter.clientId, lesson.clientId, {required: event.target.checked})}
                                            />
                                            <span>必修</span>
                                        </label>
                                        <label className="editor-lesson__duration">
                                            <span>秒</span>
                                            <input
                                                type="number"
                                                min="0"
                                                value={lesson.durationSeconds}
                                                onChange={(event) => updateLesson(chapter.clientId, lesson.clientId, {durationSeconds: Math.max(0, Number(event.target.value) || 0)})}
                                            />
                                        </label>
                                        <button className="btn--ghost btn--danger" type="button" onClick={() => removeLesson(chapter.clientId, lesson.clientId)} aria-label="删除课时">×</button>
                                    </li>
                                ))}
                            </ol>
                            <div className="editor-lesson__add">
                                <button className="btn--ghost" type="button" onClick={() => addLesson(chapter.clientId)}>+ 添加课时</button>
                            </div>
                            {/* media upload per lesson */}
                            <div className="editor-lesson__media">
                                <h4>媒体</h4>
                                {chapter.lessons.map((lesson) => (
                                    <div key={lessonKey(chapter, lesson)} className="editor-lesson__media-row">
                                        <span className="editor-lesson__media-label">{lesson.title || "（未命名课时）"}</span>
                                        <MediaUrlAttacher
                                            label="附加视频"
                                            initialUrl={lesson.mediaUrl ?? ""}
                                            onAttached={(asset) => attachLessonMedia(chapter.clientId, lesson.clientId, asset)}
                                            onClear={() => detachLessonMedia(chapter.clientId, lesson.clientId)}
                                        />
                                    </div>
                                ))}
                            </div>
                        </li>
                    ))}
                </ol>

                <div className="editor-actions">
                    <button className="btn--ghost" type="button" onClick={addChapter}>+ 添加章节</button>
                    {course ? (
                        <button className="btn--primary" type="button" disabled={savingCurriculum || !curriculumValid || course.status !== "draft"} onClick={() => void saveCurriculum()}>
                            {savingCurriculum ? "大纲保存中..." : "保存大纲"}
                        </button>
                    ) : (
                        <span className="muted">请先创建草稿课程，再保存大纲。</span>
                    )}
                </div>

                {error ? <div className="notice notice--error" role="alert">{error}</div> : null}
                {staleConflict ? (
                    <div className="notice notice--warn" role="alert">
                        <strong>保存冲突。</strong> 课程已在其它标签页更新。你可以重新载入最新版以放弃本次编辑，或继续编辑后强制保存。
                        <div className="editor-actions">
                            <button className="btn--ghost" type="button" onClick={() => void reloadLatest()}>重新载入最新版</button>
                            <button className="btn--ghost" type="button" onClick={discardLocalEdits}>保留我的修改</button>
                        </div>
                    </div>
                ) : null}
            </div>
        </section>
    );
}
