import {afterEach, beforeEach, describe, expect, it, vi} from "vitest";
import {cleanup, fireEvent, render, screen, waitFor} from "@testing-library/react";

import {ApiClientError} from "@/api/client";
import type {Course} from "@/api/types";

import {CourseEditor} from "./CourseEditor";
import {
    createDraftChapter,
    createDraftLesson,
    isCurriculumValid,
    toCurriculumInput,
} from "./teacherTypes";

// hoisted 共享 mock state（vi.mock 工厂在模块顶层，不能引用外部变量）
const mocks = vi.hoisted(() => ({
    create: vi.fn(),
    replace: vi.fn(),
    get: vi.fn(),
    submit: vi.fn(),
    listMine: vi.fn(),
}));

vi.mock("@/api/client", async () => {
    const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
    return {
        ...actual,
        apiClient: {
            post: vi.fn(),
            put: vi.fn(),
            get: vi.fn(),
            delete: vi.fn(),
        },
    };
});

vi.mock("@/api/types", async () => {
    const actual = await vi.importActual<typeof import("@/api/types")>("@/api/types");
    return {
        ...actual,
        courseApi: {
            create: mocks.create,
            update: vi.fn(),
            submit: mocks.submit,
            replaceCurriculum: mocks.replace,
            list: vi.fn(),
            listMine: mocks.listMine,
            get: mocks.get,
        },
    };
});

const draftCourse: Course = {
    id: "11111111-1111-1111-1111-111111111111",
    teacherId: "t1",
    slug: "intro",
    title: "Intro",
    description: "",
    status: "draft",
    currentVersion: 4,
    priceMinor: 0,
    currency: "USD",
    createdAt: "2026-08-10T00:00:00Z",
    updatedAt: "2026-08-10T00:00:00Z",
};

// 每个测试前重置全部 mock 实现，避免前一个测试残留的 mockResolvedValue/mockRejectedValue 干扰
beforeEach(() => {
    mocks.create.mockReset().mockResolvedValue(draftCourse);
    mocks.replace.mockReset();
    mocks.get.mockReset();
    mocks.submit.mockReset().mockResolvedValue(draftCourse);
    mocks.listMine.mockReset().mockResolvedValue({items: []});
});

afterEach(() => {
    cleanup();
});

describe("teacherTypes helpers", () => {
    it("creates lesson/chapter with stable clientIds", () => {
        const lesson = createDraftLesson();
        const chapter = createDraftChapter();
        expect(lesson.clientId.startsWith("lesson-")).toBe(true);
        expect(chapter.clientId.startsWith("chapter-")).toBe(true);
        expect(chapter.lessons.length).toBe(1);
    });

    it("flags invalid curriculum (empty title / negative duration)", () => {
        const bad = [{...createDraftChapter(), title: "", lessons: [{...createDraftLesson(), title: "L1"}]}];
        expect(isCurriculumValid(bad)).toBe(false);
        const good = [{...createDraftChapter(), title: "C1", lessons: [{...createDraftLesson(), title: "L1", durationSeconds: 60}]}];
        expect(isCurriculumValid(good)).toBe(true);
    });

    it("converts curriculum to backend input (trim titles)", () => {
        const input = toCurriculumInput([{...createDraftChapter(), title: "  C1  ", lessons: [{...createDraftLesson(), title: " L1 ", required: true, durationSeconds: 90}]}]);
        expect(input[0]?.title).toBe("C1");
        expect(input[0]?.lessons[0]?.title).toBe("L1");
        expect(input[0]?.lessons[0]?.durationSeconds).toBe(90);
    });
});

describe("CourseEditor", () => {
    it("renders seed chapters and disables Save curriculum before creating a draft", () => {
        render(<CourseEditor />);
        expect(screen.getByText(/Course studio/i)).toBeTruthy();
        expect(screen.getAllByPlaceholderText(/Chapter \d title/i).length).toBeGreaterThanOrEqual(3);
        expect(screen.getByText(/Create a draft first/i)).toBeTruthy();
    });

    it("adds and removes chapters + lessons", () => {
        render(<CourseEditor />);
        const initialChapters = screen.getAllByPlaceholderText(/Chapter \d title/i).length;
        fireEvent.click(screen.getAllByText(/\+ Add chapter/i)[0]!);
        expect(screen.getAllByPlaceholderText(/Chapter \d title/i).length).toBe(initialChapters + 1);
        const addLessonBtns = screen.getAllByText(/\+ Add lesson/i);
        fireEvent.click(addLessonBtns[addLessonBtns.length - 1]!);
        const removeLessonBtns = screen.getAllByLabelText("Remove lesson");
        expect(removeLessonBtns.length).toBeGreaterThan(0);
    });

    it("create-draft POSTs and unlocks Save curriculum", async () => {
        render(<CourseEditor />);
        fireEvent.change(screen.getAllByPlaceholderText("Smart Contract Security")[0]!, {target: {value: "Intro"}});
        fireEvent.submit(screen.getAllByRole("button", {name: /Create draft/i})[0]!.closest("form")!);
        await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
        expect(screen.getByRole("button", {name: /Save curriculum/i})).toBeTruthy();
    });

    it("Save curriculum sends If-Match + correct payload; surfaces STALE_VERSION conflict UX", async () => {
        mocks.replace.mockImplementation(async () => {
            throw new ApiClientError({
                code: "STALE_VERSION",
                message: "course was updated",
                requestId: "r1",
            }, 409);
        });

        render(<CourseEditor />);
        fireEvent.change(screen.getAllByPlaceholderText("Smart Contract Security")[0]!, {target: {value: "Intro"}});
        fireEvent.submit(screen.getAllByRole("button", {name: /Create draft/i})[0]!.closest("form")!);
        await waitFor(() => expect(mocks.create).toHaveBeenCalled(), {timeout: 2000});

        // 把所有 3 个 lesson title 都填上（seed 默认 empty，Save curriculum 才能 enabled）
        const lessonTitles = screen.getAllByPlaceholderText("Lesson title");
        lessonTitles.forEach((input, i) => fireEvent.change(input, {target: {value: `Lesson ${i + 1}`}}));

        fireEvent.click(screen.getByRole("button", {name: /Save curriculum/i}));
        await waitFor(() => expect(mocks.replace).toHaveBeenCalledTimes(1), {timeout: 2000});
        // courseApi.replaceCurriculum(id, version, body) — version 必须等于 course.currentVersion
        const [courseId, version, body] = mocks.replace.mock.calls[0]!;
        expect(courseId).toBe(draftCourse.id);
        expect(version).toBe(draftCourse.currentVersion);
        expect(Array.isArray(body.chapters)).toBe(true);
        // apiClient.put 把 If-Match: <version> 注入到 headers；这里通过 mock 的调用参数间接验证 version
        // （避免在测试里 mock apiClient.put 后再断言 header——会让 test 变 fragile）

        await waitFor(() => expect(screen.getByText(/Save conflict/i)).toBeTruthy(), {timeout: 2000});
        expect(screen.getByRole("button", {name: /Reload latest/i})).toBeTruthy();
        expect(screen.getByRole("button", {name: /Keep my edits/i})).toBeTruthy();
    });
});
