/**
 * F04-T17: E2E 完课证书主流程（Playwright + Privy stub）。
 *
 * 覆盖 F04 验收：
 *   - AC-001: 已报名学生访问 /learn/{courseId} → 调 GET /lessons/{id}/playback 拿 presigned URL。
 *   - AC-002: ProgressReporter 在 pct=100 时触发 POST /courses/{id}/complete（带回执）。
 *   - AC-003: 跳转到 /account/certificates → 列表派生自 /me/enrollments，hasCompletion=true 的条目出现。
 *   - AC-004: 证书条目带 courseTitle + 状态徽章（confirmed）。
 *
 * 与 auth.spec.ts 一致：所有 /api/v1/* 由 fixtures/privy-stub.ts 拦截。
 * 视频元素在 E2E 里直接通过 evaluate 设置 currentTime/duration，绕过真实媒体解码；
 * onTimeUpdate 通过手动派发触发，避免依赖浏览器音视频管线。
 */

import {test, expect} from "@playwright/test";
import {STUB_PROFILES, installPrivyStub} from "./fixtures/privy-stub";

// 固定 UUID + 地址：让断言可重现。
const COURSE_ID = "c_00000000-0000-0000-0000-000000000002";
const LESSON_ID = "l_00000000-0000-0000-0000-000000000002";
const ENROLLMENT_ID = "e_00000000-0000-0000-0000-000000000002";
const COMPLETION_ID = "cmp_00000000-0000-0000-0000-000000000002";
const ONCHAIN_CERT_ID = "cert_00000000-0000-0000-0000-000000000002";
const RECIPIENT = ("0x" + "ee".repeat(20)) as `0x${string}`;
const COMPLETED_AT = "2026-08-10T12:00:00Z";

const COURSE = {
    id: COURSE_ID,
    slug: "advanced-defi",
    title: "Advanced DeFi",
    description: "Course for E2E certificate flow.",
    teacherName: "Stub Teacher",
    teacherWallet: RECIPIENT,
    priceMinor: 20000,
    priceYD: "20000000000000000000",
    currency: "USD",
    currentVersion: 1,
    publishedAt: "2026-02-01T00:00:00Z",
};

const PLAYBACK_CRED = {
    url: "https://cdn.example.com/lessons/l_2/master.m3u8?signature=stub",
    expiresAt: new Date(Date.now() + 5 * 60_000).toISOString(),
    posterUrl: "https://cdn.example.com/posters/l_2.jpg",
    durationSeconds: 600,
};

const ENROLLMENT_IN_PROGRESS = {
    enrollmentId: ENROLLMENT_ID, courseId: COURSE_ID, courseSlug: COURSE.slug, courseTitle: COURSE.title,
    enrolledAt: "2026-08-01T00:00:00Z",
    requiredLessonsTotal: 1, completedLessonsTotal: 0, completionPct: 0,
    hasCompletion: false, completedAt: null,
};

const ENROLLMENT_COMPLETED = {
    ...ENROLLMENT_IN_PROGRESS,
    completedLessonsTotal: 1, completionPct: 100, hasCompletion: true, completedAt: COMPLETED_AT,
};

const COMPLETION_RECORD = {
    id: COMPLETION_ID, enrollmentId: ENROLLMENT_ID, userId: STUB_PROFILES.student.id, courseId: COURSE_ID,
    ruleVersion: 1, completedAt: COMPLETED_AT, completedLessonsCount: 1, totalLessonsCount: 1,
    certificateId: null, onchainCertId: ONCHAIN_CERT_ID, status: "confirmed",
    recipientWallet: RECIPIENT,
    metadataUri: "ipfs://bafy.example/metadata.json",
    metadataSha256: "0x" + "ff".repeat(32),
};

/**
 * 在已挂载的 <video> 上直接设置 currentTime / duration 并派发 timeupdate，
 * 避免依赖真实媒体加载（Playwright headless 无音视频解码）。
 */
async function fastForwardVideoToEnd(page: import("@playwright/test").Page) {
    await page.waitForSelector(".learning-player__video");
    await page.evaluate(() => {
        const v = document.querySelector<HTMLVideoElement>(".learning-player__video");
        if (!v) throw new Error("video element not found");
        // duration 在没有真实媒体时为 NaN；强行覆盖以驱动 computePct 走到 100
        Object.defineProperty(v, "duration", {configurable: true, value: 100});
        v.currentTime = 100;
        v.dispatchEvent(new Event("timeupdate"));
        v.dispatchEvent(new Event("seeked"));
    });
}

test.describe("F04 / 完课证书 / certificate flow", () => {
    test.beforeEach(async ({context}) => {
        await context.clearCookies();
    });

    test("enrolled student watches lesson → marks complete → certificate appears with confirmed badge", async ({page, context}) => {
        // student 一开始就登录 + 有钱包
        const wallet = {id: "w_student_1", chainId: 11155111, address: RECIPIENT, isPrimary: true, boundAt: "2026-07-01T00:00:00Z"};
        const stub = await installPrivyStub(context, {
            initialProfile: {...STUB_PROFILES.student, primaryWallet: wallet, wallets: [wallet]},
            initialSession: true,
        });
        expect(stub.state().sid).toMatch(/^stub-sid-/);

        // playback 凭证
        await page.route(`**/api/v1/lessons/${LESSON_ID}/playback`, (r) => r.fulfill({
            status: 200, contentType: "application/json", body: JSON.stringify(PLAYBACK_CRED),
        }));

        // 进度上报：收集 pct 数组
        const progressCalls: Array<{pct: number}> = [];
        await page.route("**/api/v1/lessons/*/progress", async (r) => {
            let body: {pct?: number} = {};
            try { body = JSON.parse(r.request().postData() ?? "{}"); } catch { /* ignore */ }
            progressCalls.push({pct: body.pct ?? 0});
            await r.fulfill({status: 200, contentType: "application/json", body: JSON.stringify({lessonId: LESSON_ID, pct: body.pct ?? 0})});
        });

        // POST /courses/{id}/complete → CompletionRecord
        await page.route(`**/api/v1/courses/${COURSE_ID}/complete`, (r) => r.fulfill({
            status: 200, contentType: "application/json", body: JSON.stringify(COMPLETION_RECORD),
        }));

        // /me/enrollments：第一次 in-progress，第二次 completed
        let enrollmentsCalls = 0;
        await page.route("**/api/v1/me/enrollments?**", (r) => {
            enrollmentsCalls += 1;
            const items = enrollmentsCalls >= 2 ? [ENROLLMENT_COMPLETED] : [ENROLLMENT_IN_PROGRESS];
            return r.fulfill({status: 200, contentType: "application/json", body: JSON.stringify({items})});
        });

        await page.goto(`/learn/${COURSE_ID}`);
        await expect(page.locator(".learning-player")).toBeVisible();
        await expect(page.locator(".learning-player__video")).toHaveAttribute("src", /cdn\.example\.com/);

        // 推到 pct=100
        await fastForwardVideoToEnd(page);
        const completeBtn = page.getByRole("button", {name: /mark as complete/i});
        await expect(completeBtn).toBeVisible({timeout: 10_000});
        await completeBtn.click();

        expect(progressCalls.some((c) => c.pct === 100)).toBe(true);

        // 跳转后落地 /account/certificates
        await page.waitForURL(/\/account\/certificates/);
        await expect(page.locator(".my-certificates")).toBeVisible();
        const certItem = page.locator(".my-certificates__item").filter({hasText: COURSE.title});
        await expect(certItem).toBeVisible();
        await expect(certItem.locator(".status-pill")).toHaveText(/confirmed/i);
    });

    test("completion endpoint 422 partial-completion surfaces error and stays in-progress", async ({page, context}) => {
        const wallet = {id: "w_student_2", chainId: 11155111, address: RECIPIENT, isPrimary: true, boundAt: "2026-07-01T00:00:00Z"};
        await installPrivyStub(context, {
            initialProfile: {...STUB_PROFILES.student, primaryWallet: wallet, wallets: [wallet]},
            initialSession: true,
        });

        await page.route(`**/api/v1/lessons/${LESSON_ID}/playback`, (r) => r.fulfill({
            status: 200, contentType: "application/json", body: JSON.stringify(PLAYBACK_CRED),
        }));
        await page.route("**/api/v1/lessons/*/progress", (r) => r.fulfill({
            status: 200, contentType: "application/json", body: JSON.stringify({lessonId: LESSON_ID, pct: 100}),
        }));
        // 关键：complete 返回 422（required lessons 未全部 pct=100）
        await page.route(`**/api/v1/courses/${COURSE_ID}/complete`, (r) => r.fulfill({
            status: 422, contentType: "application/json",
            body: JSON.stringify({error: {code: "PARTIAL_COMPLETION", message: "1 lesson below required threshold.", requestId: "stub", details: {requiredLessonsTotal: 3, completedLessonsCount: 1}}}),
        }));
        await page.route("**/api/v1/me/enrollments?**", (r) => r.fulfill({
            status: 200, contentType: "application/json", body: JSON.stringify({items: [ENROLLMENT_IN_PROGRESS]}),
        }));

        await page.goto(`/learn/${COURSE_ID}`);
        await expect(page.locator(".learning-player")).toBeVisible();
        await fastForwardVideoToEnd(page);

        const completeBtn = page.getByRole("button", {name: /mark as complete/i});
        await expect(completeBtn).toBeVisible({timeout: 10_000});
        await completeBtn.click();

        // 错误展示；不跳转
        await expect(page.locator(".progress-reporter .notice--error")).toBeVisible();
        await expect(page.getByText(/partial|PARTIAL/i).first()).toBeVisible();
        expect(page.url()).toContain(`/learn/${COURSE_ID}`);
    });
});
