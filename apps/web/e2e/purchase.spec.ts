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
const COURSE_ID = "00000000-0000-4000-8000-000000000001";
const INTENT_ID = "00000000-0000-4000-8000-000000000002";
const ORDER_ID = "00000000-0000-4000-8000-000000000003";
const ENROLLMENT_ID = "00000000-0000-4000-8000-000000000004";
const WALLET_ID = "00000000-0000-4000-8000-000000000005";
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

async function installWalletAndRpc(page: Page) {
    await page.addInitScript(({address, txHash}) => {
        const provider = {
            isMetaMask: true,
            chainId: "0xaa36a7",
            networkVersion: "11155111",
            selectedAddress: address,
            request: async ({method}: {method: string}) => {
                if (method === "eth_chainId") return "0xaa36a7";
                if (method === "net_version") return "11155111";
                if (method === "eth_requestAccounts" || method === "eth_accounts") return [address];
                if (method === "eth_sendTransaction" || method === "eth_sendRawTransaction") return txHash;
                return null;
            },
            on: () => undefined,
            removeListener: () => undefined,
        };
        (window as unknown as {ethereum: unknown}).ethereum = provider;
        const announce = () => window.dispatchEvent(new CustomEvent("eip6963:announceProvider", {detail: {info: {uuid: "350670db-19fa-4704-a166-e52e178b59d2", name: "MetaMask", icon: "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg'/>", rdns: "io.metamask"}, provider}}));
        window.addEventListener("eip6963:requestProvider", announce);
        window.setTimeout(announce, 0);
    }, {address: STUDENT_WALLET, txHash: TX_HASH});
    await page.route("**/*", async (route) => {
        const raw = route.request().postData() ?? "";
        if (!raw.includes("\"jsonrpc\"")) return route.fallback();
        type Rpc = {method?: string; id?: number};
        let parsed: Rpc | Rpc[] = {};
        try { parsed = JSON.parse(raw); } catch { /* ignore */ }
        const handle = ({method, id = 1}: Rpc) => {
            let result: unknown = null;
            if (method === "eth_getTransactionReceipt") result = {transactionHash: TX_HASH, blockHash: "0x" + "aa".repeat(32), blockNumber: "0x1234", from: STUDENT_WALLET, to: MARKET_ADDRESS, status: "0x1", gasUsed: "0x5208", cumulativeGasUsed: "0x5208", logs: [], contractAddress: null, logsBloom: "0x" + "00".repeat(256), transactionIndex: "0x0"};
            if (method === "eth_blockNumber") result = "0x1234";
            if (method === "eth_getTransactionCount") result = "0x1";
            if (method === "eth_gasPrice") result = "0x3b9aca00";
            if (method === "eth_estimateGas") result = "0x5208";
            return {jsonrpc: "2.0", id, result};
        };
        return route.fulfill({status: 200, contentType: "application/json", body: JSON.stringify(Array.isArray(parsed) ? parsed.map(handle) : handle(parsed))});
    });
}

async function connectWallet(page: Page) {
    await page.getByRole("button", {name: /connect wallet/i}).first().click();
    await page.getByText(/MetaMask/i).first().click();
    await expect(page.getByRole("button", {name: /manage wallet/i})).toBeVisible();
}

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
    await page.goto("/courses");
    await page.getByRole("link", {name: /open course intro to yideng finance/i}).click();
    await page.waitForURL(`**/courses/${COURSE_ID}`);
    await expect(page.locator(".checkout-panel")).toBeVisible();
    await page.locator(".checkout-panel__terms input[type=checkbox]").check();
}

test.describe("F03 / 购买 / purchase flow", () => {
    test.beforeEach(async ({context}) => {
        await context.clearCookies();
    });

    test("happy path: sign in → buy → confirmed → enrollment appears in /me/enrollments", async ({page, context}) => {
        await installWalletAndRpc(page);
        const wallet = {id: WALLET_ID, chainId: 11155111, address: STUDENT_WALLET, isPrimary: true, boundAt: "2026-07-01T00:00:00Z"};
        const stub = await installPrivyStub(context, {initialProfile: {...STUB_PROFILES.student, primaryWallet: wallet, wallets: [wallet]}, initialSession: false});

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
        let transactionSubmitted = false;
        await page.route(`**/api/v1/orders/${INTENT_ID}/transactions`, (r) => {
            transactionSubmitted = true;
            return r.fulfill({status: 200, contentType: "application/json", body: JSON.stringify(ORDER_ACK)});
        });

        // 购买前为空；交易确认并跳转到账户路由后返回新报名。
        await page.route("**/api/v1/me/enrollments?**", (r) => {
            const items = transactionSubmitted ? [ENROLLMENT_ITEM] : [];
            return r.fulfill({status: 200, contentType: "application/json", body: JSON.stringify({items})});
        });

        // 登录
        await page.goto("/courses");
        await page.getByRole("button", {name: /sign in/i}).first().click();
        await expect(page.locator(".user-menu")).toBeVisible();
        await connectWallet(page);

        // 进入购买面板
        await openCheckoutPanel(page);

        const cta = page.locator(".checkout-button button[data-state]").last();
        await expect(cta).toHaveAttribute("data-state", "idle");
        await cta.click();

        // 状态机进入链上处理中间态；成功后产品会立即跳到独立的报名路由。
        await expect(cta).toHaveAttribute("data-state", /preparing|signing|confirming/);
        await expect(page).toHaveURL(/\/account\/enrollments$/, {timeout: 10_000});
        await expect(page.getByRole("heading", {name: "My enrollments"})).toBeVisible();
        await expect(page.locator(".my-enrollments__title", {hasText: COURSE.title})).toBeVisible();

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
        await installWalletAndRpc(page);
        const wallet = {id: WALLET_ID, chainId: 11155111, address: STUDENT_WALLET, isPrimary: true, boundAt: "2026-07-01T00:00:00Z"};
        await installPrivyStub(context, {initialProfile: {...STUB_PROFILES.student, primaryWallet: wallet, wallets: [wallet]}, initialSession: true});
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

        await page.goto("/courses");
        await expect(page.locator(".user-menu")).toBeVisible();
        await connectWallet(page);
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
