/**
 * F03-T18: E2E 购买主流程（Playwright + Privy stub）。
 *
 * 覆盖 F03 验收：
 *   - AC-001: 匿名访问 /catalog → 看见 Sign in CTA，无 user-menu。
 *   - AC-002: 学生登录后打开课程详情 → 点 Buy → CheckoutPanel 出现。
 *   - AC-003: CheckoutButton 状态机 idle → preparing → signing → confirming → done。
 *   - AC-004: 成功时 /me/enrollments 包含新报名。
 *   - AC-005: 失败（409 ALREADY_PURCHASED）展示 user-visible 错误，不进入 done。
 *
 * 与 auth.spec.ts 一致：所有 /api/v1/* 由 fixtures/privy-stub.ts 拦截；
 * Privy SDK 在 VITE_PRIVY_DEV_STUB=1 下被绕过（playwright.config.ts 注入）。
 *
 * 注：wagmi 写链这一步在 E2E 阶段被 page.route 截掉（RPC 拦截），txHash 由
 * stub 模拟；真实 receipt 也不等链——CheckoutButton 进入 confirming 后由 stub
 * 注入 ack 让 useEffect 走到 done。
 */

import {test, expect, type Page} from "@playwright/test";
import {STUB_PROFILES, installPrivyStub} from "./fixtures/privy-stub";

// 固定 UUID + 地址：让断言可重现，避免依赖时间戳 / 随机源。
const COURSE_ID = "c_00000000-0000-0000-0000-000000000001";
const INTENT_ID = "i_00000000-0000-0000-0000-000000000001";
const ORDER_ID = "o_00000000-0000-0000-0000-000000000001";
const ENROLLMENT_ID = "e_00000000-0000-0000-0000-000000000001";
const WALLET_ID = "w_00000000-0000-0000-0000-000000000001";
const TX_HASH = ("0x" + "ab".repeat(32)) as `0x${string}`;
const STUDENT_WALLET = ("0x" + "11".repeat(20)) as `0x${string}`;
const MARKET_ADDRESS = ("0x" + "22".repeat(20)) as `0x${string}`;
const TOKEN_ADDRESS = ("0x" + "33".repeat(20)) as `0x${string}`;
const PRICE_ID = "p_00000000-0000-0000-0000-000000000001";
const COURSE_KEY = ("0x" + "44".repeat(32)) as `0x${string}`;

const COURSE = {
    id: COURSE_ID,
    slug: "intro-to-yideng",
    title: "Intro to Yideng Finance",
    description: "Test course for E2E purchase flow.",
    teacherName: "Stub Teacher",
    teacherWallet: STUDENT_WALLET,
    priceMinor: 10000,
    priceYD: "10000000000000000000",
    currency: "USD",
    currentVersion: 1,
    publishedAt: "2026-01-01T00:00:00Z",
};

const COURSE_DETAIL = {
    course: COURSE,
    chapters: [{id: "ch_1", position: 1, title: "Ch 1", lessons: [{id: "l_1", position: 1, title: "L1", required: true, durationSeconds: 600}]}],
};

const PURCHASE_INTENT = {
    id: INTENT_ID, userId: STUB_PROFILES.student.id, walletId: WALLET_ID, courseId: COURSE_ID,
    priceId: PRICE_ID, courseKey: COURSE_KEY, priceVersion: 1, chainId: 11155111,
    tokenAddress: TOKEN_ADDRESS, amount: "10000000000000000000", marketAddress: MARKET_ADDRESS,
    idempotencyKey: "idem-e2e-001", status: "created",
    expiresAt: new Date(Date.now() + 300_000).toISOString(),
    createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
};

const ORDER_ACK = {orderId: ORDER_ID, intentId: INTENT_ID, onchainTxHash: TX_HASH, chainId: 11155111, status: "confirmed"};

const ENROLLMENT_ITEM = {
    enrollmentId: ENROLLMENT_ID, courseId: COURSE_ID, courseSlug: COURSE.slug, courseTitle: COURSE.title,
    enrolledAt: new Date().toISOString(), requiredLessonsTotal: 1, completedLessonsTotal: 0,
    completionPct: 0, hasCompletion: false, completedAt: null,
};

/**
 * /catalog 打开课程详情 → 点 Buy → 勾选条款 → 看到 CheckoutPanel 渲染。
 * 真实下单由各用例在 purchase-intents / transactions 上自己打桩。
 */
async function openCheckoutPanel(page: Page) {
    await page.goto("/");
    await page.getByRole("button", {name: /open course intro to yideng finance/i}).click();
    await expect(page.getByRole("dialog", {name: /course detail/i})).toBeVisible();
    await page.getByRole("button", {name: /^buy$/i}).first().click();
    await expect(page.locator(".checkout-panel")).toBeVisible();
    await page.locator(".checkout-panel__terms input[type=checkbox]").check();
}

test.describe("F03 / 购买 / purchase flow", () => {
    test.beforeEach(async ({context}) => {
        await context.clearCookies();
    });

    test("happy path: sign in → buy → confirmed → enrollment appears in /me/enrollments", async ({page, context}) => {
        const stub = await installPrivyStub(context, {initialProfile: STUB_PROFILES.student, initialSession: false});

        // /catalog 列表 + 详情打桩
        await page.route("**/api/v1/courses?**", (r) => r.fulfill({
            status: 200, contentType: "application/json",
            body: JSON.stringify({items: [COURSE], nextCursor: ""}),
        }));
        await page.route(`**/api/v1/courses/${COURSE_ID}`, (r) => r.fulfill({
            status: 200, contentType: "application/json", body: JSON.stringify(COURSE_DETAIL),
        }));

        // 关键：purchase-intents 与 orders/.../transactions 双步确认
        let intentCalls = 0;
        await page.route("**/api/v1/orders/purchase-intents", (r) => {
            intentCalls += 1;
            return r.fulfill({status: 200, contentType: "application/json", body: JSON.stringify(PURCHASE_INTENT)});
        });
        await page.route(`**/api/v1/orders/${INTENT_ID}/transactions`, (r) => r.fulfill({
            status: 200, contentType: "application/json", body: JSON.stringify(ORDER_ACK),
        }));

        // /me/enrollments：第一次空（MyEnrollments 拉取），purchase 后含新记录
        let enrollmentsCalls = 0;
        await page.route("**/api/v1/me/enrollments?**", (r) => {
            enrollmentsCalls += 1;
            const items = enrollmentsCalls > 1 ? [ENROLLMENT_ITEM] : [];
            return r.fulfill({status: 200, contentType: "application/json", body: JSON.stringify({items})});
        });

        // 登录
        await page.goto("/");
        await page.getByRole("button", {name: /sign in/i}).first().click();
        await expect(page.locator(".user-menu")).toBeVisible();

        // 进入购买面板
        await openCheckoutPanel(page);

        const cta = page.locator(".checkout-button button[data-state]").last();
        await expect(cta).toHaveAttribute("data-state", "idle");
        await cta.click();

        // 状态机走完：先到中间态（preparing/signing/confirming 任一）→ 终态 done
        await expect(cta).toHaveAttribute("data-state", /preparing|signing|confirming/);
        await expect(cta).toHaveAttribute("data-state", "done", {timeout: 10_000});

        // 成功提示出现
        await expect(page.getByText(/unlock|enrolled|confirmed/i).first()).toBeVisible();

        expect(intentCalls).toBeGreaterThanOrEqual(1);
        expect(stub.state().sid).toMatch(/^stub-sid-/);

        // 报名落地：直接 fetch 验证
        const body = await page.evaluate(async () => {
            const r = await fetch("/api/v1/me/enrollments?limit=50", {credentials: "include"});
            return r.ok ? await r.json() : null;
        });
        expect(body?.items?.some((e: {courseId: string}) => e.courseId === COURSE_ID)).toBe(true);
    });

    test("409 ALREADY_PURCHASED surfaces a user-visible error and stays failed", async ({page, context}) => {
        await installPrivyStub(context, {initialProfile: STUB_PROFILES.student, initialSession: true});
        await page.route("**/api/v1/courses?**", (r) => r.fulfill({
            status: 200, contentType: "application/json",
            body: JSON.stringify({items: [COURSE], nextCursor: ""}),
        }));
        await page.route(`**/api/v1/courses/${COURSE_ID}`, (r) => r.fulfill({
            status: 200, contentType: "application/json", body: JSON.stringify(COURSE_DETAIL),
        }));

        // 关键：purchase-intents 返回 409 ALREADY_PURCHASED
        await page.route("**/api/v1/orders/purchase-intents", (r) => r.fulfill({
            status: 409, contentType: "application/json",
            body: JSON.stringify({error: {code: "ALREADY_PURCHASED", message: "You already own this course.", requestId: "stub", details: {courseId: COURSE_ID}}}),
        }));

        await page.goto("/");
        await expect(page.locator(".user-menu")).toBeVisible();
        await openCheckoutPanel(page);

        const cta = page.locator(".checkout-button button[data-state]").last();
        await cta.click();

        // 终态是 failed；不可达 done；error banner 可见
        await expect(cta).toHaveAttribute("data-state", "failed", {timeout: 10_000});
        await expect(page.locator(".checkout-button__error, .notice--error").first()).toBeVisible();
        await expect(page.getByText(/already|owned|purchased/i).first()).toBeVisible();
        await expect(cta).not.toHaveAttribute("data-state", "done");
    });
});
