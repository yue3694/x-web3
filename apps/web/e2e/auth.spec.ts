/**
 * F01-T14: E2E 骨架（Playwright + Privy stub）。
 *
 * 覆盖 F01 验收：
 *   - AC-001 / R-ID-001 / R-ID-002: 首次登录创建 profile；重复登录幂等（同 user_id）。
 *   - AC-002 / R-ID-003: 钱包绑定冲突时返回 409 + error.code WALLET_ALREADY_BOUND。
 *   - AC-003 / R-ID-004: 角色决定 UI 可见性；student 看不到 Teacher Studio / Admin 入口。
 *   - R-ID-005 / R-ID-006: 客户端篡改 role 不影响 API；/admin/* 无 sid 直接 401/403。
 *
 * 所有 /api/v1/* 由 fixtures/privy-stub.ts 拦截；Privy SDK 通过
 * VITE_PRIVY_DEV_STUB=1 在前端跳过加载（见 playwright.config.ts）。
 *
 * 完整覆盖（RequireAuth 跳转、wallets unbind 路径、role upgrade 审批等）
 * 留在 F01 后续 E2E 任务里补齐——本任务是「骨架」。
 */

import {test, expect} from "@playwright/test";
import {
    STUB_PROFILES,
    installPrivyStub,
    type StubProfile,
} from "./fixtures/privy-stub";

test.describe("F01 / 身份与权限 / auth flow", () => {
    test.beforeEach(async ({context}) => {
        // 每个 test 都从空 cookie 起始，避免 sid 串扰
        await context.clearCookies();
    });

    test("anonymous user sees sign-in CTA and no user menu", async ({page, context}) => {
        await installPrivyStub(context, {initialProfile: null, initialSession: false});
        await page.goto("/");

        await expect(page.getByRole("button", {name: /sign in/i}).first()).toBeVisible();
        // TopNav 中没有 user-menu 节点
        await expect(page.locator(".user-menu")).toHaveCount(0);
        // Teacher Studio / Admin 链接被 RequirePermission 隐藏
        await expect(page.getByRole("link", {name: /^Studio$/})).toHaveCount(0);
        await expect(page.getByRole("link", {name: /^Admin$/})).toHaveCount(0);
    });

    test("dev-stub login returns deterministic profile and renders user menu", async ({page, context}) => {
        const stub = await installPrivyStub(context, {
            initialProfile: STUB_PROFILES.student,
            initialSession: false,
        });

        await page.goto("/");
        await page.getByRole("button", {name: /sign in/i}).first().click();

        // session bootstrap 完成后渲染 user-menu
        const userMenu = page.locator(".user-menu");
        await expect(userMenu).toBeVisible({timeout: 10_000});
        await expect(userMenu.locator(".user-menu__name")).toHaveText(STUB_PROFILES.student.displayName);
        // student 没有 SYSTEM_ADMIN / COURSE_CREATE → nav 不出现入口
        await expect(page.getByRole("link", {name: /^Studio$/})).toHaveCount(0);
        await expect(page.getByRole("link", {name: /^Admin$/})).toHaveCount(0);
        // sid cookie 被 stub 写入
        const cookies = await page.context().cookies();
        expect(cookies.map((c) => c.name)).toContain("sid");

        // fixture 状态：sid 已签发
        expect(stub.state().sid).toMatch(/^stub-sid-/);
    });

    test("re-login with same token is idempotent and returns same user_id", async ({page, context}) => {
        await installPrivyStub(context, {
            initialProfile: STUB_PROFILES.student,
            initialSession: false,
        });

        await page.goto("/");
        const signIn = page.getByRole("button", {name: /sign in/i}).first();
        await signIn.click();
        await expect(page.locator(".user-menu")).toBeVisible();

        // 模拟刷新页面 → /me 仍然拿到同一个 profile
        await page.reload();
        await expect(page.locator(".user-menu")).toBeVisible();
        await expect(page.locator(".user-menu__name")).toHaveText(STUB_PROFILES.student.displayName);
        // AC-001: 同一个 subject 下 user_id 始终为 STUB_PROFILES.student.id
        await expect.poll(async () => {
            const body = await page.evaluate(async () => {
                const r = await fetch("/api/v1/me", {credentials: "include"});
                return r.ok ? await r.json() : null;
            });
            return body?.id ?? null;
        }).toBe(STUB_PROFILES.student.id);
    });

    test("teacher role reveals Teacher Studio link and still hides Admin", async ({page, context}) => {
        await installPrivyStub(context, {
            initialProfile: STUB_PROFILES.teacher,
            initialSession: true,
        });

        await page.goto("/");

        // Teacher Studio 链接可见；Admin 链接（SYSTEM_ADMIN）不可见
        await expect(page.getByRole("link", {name: /^Studio$/})).toBeVisible();
        await expect(page.getByRole("link", {name: /^Admin$/})).toHaveCount(0);
    });

    test("super_admin role reveals both Teacher Studio and Admin links", async ({page, context}) => {
        await installPrivyStub(context, {
            initialProfile: STUB_PROFILES.super_admin,
            initialSession: true,
        });

        await page.goto("/");

        await expect(page.getByRole("link", {name: /^Studio$/})).toBeVisible();
        await expect(page.getByRole("link", {name: /^Admin$/})).toBeVisible();
    });

    test("direct GET /admin/* without sid returns 401 (R-ID-006)", async ({page, context}) => {
        await installPrivyStub(context, {initialProfile: null, initialSession: false});
        await page.goto("/"); // 先落地，让浏览器侧 cookie / route 都就位

        // 通过浏览器 fetch 发起（同源），让 context.route 拦截
        const resp = await page.evaluate(async () => {
            const r = await fetch("/api/v1/admin/audit-logs", {credentials: "include"});
            return {status: r.status, body: await r.json()};
        });
        expect(resp.status).toBe(401);
        expect(resp.body.error?.code).toBeTruthy();
    });

    test("client-side profile.role tampering cannot bypass admin gating (R-ID-005)", async ({page, context}) => {
        // 用户拿到 student profile 后篡改 roles 字段 → 后端仍以原 profile 响应
        const stub = await installPrivyStub(context, {
            initialProfile: STUB_PROFILES.student,
            initialSession: true,
        });

        await page.goto("/");
        await expect(page.locator(".user-menu")).toBeVisible();

        // 客户端尝试把 roles 改成 ["super_admin"]——通过 mutate stub 让下次 /me 返回伪造 profile
        const tampered: StubProfile = {
            ...STUB_PROFILES.student,
            roles: ["super_admin"],
            permissions: ["*"],
        };
        stub.setProfile(tampered);
        await page.reload();

        // UI 由 useSession().profile 决定 → 显式渲染 Admin 入口（证明前端 UX 隐藏只是 UX，
        // 不是安全机制——下面单独验证 server 仍按真实 role 拒绝 admin endpoint）。
        await expect(page.getByRole("link", {name: /^Admin$/})).toBeVisible();

        // 即使前端"以为"自己是 admin，请求真实 admin endpoint 仍被 stub 按 adminStatus=401 拒绝；
        // 注：这是 E2E 骨架的简化演示——真正的 R-ID-005 在 Go API 的 RBAC 中实现，
        // 这里验证「篡改 client profile 不让 admin endpoint 通过」的端到端语义。
        const adminStatus = await page.evaluate(async () => {
            const r = await fetch("/api/v1/admin/system", {credentials: "include"});
            return r.status;
        });
        expect([401, 403]).toContain(adminStatus);
    });

    test("logout clears session and user menu disappears", async ({page, context}) => {
        const stub = await installPrivyStub(context, {
            initialProfile: STUB_PROFILES.student,
            initialSession: true,
        });

        await page.goto("/");
        await expect(page.locator(".user-menu")).toBeVisible();

        // 打开 user menu → 触发 Sign out
        await page.locator(".user-menu__trigger").click();
        await page.getByRole("button", {name: /sign out/i}).click();

        // stub 清 sid → reload 后 /me 401 → profile 清空
        await page.reload();
        await expect(page.locator(".user-menu")).toHaveCount(0);
        await expect(page.getByRole("button", {name: /sign in/i}).first()).toBeVisible();
        expect(stub.state().sid).toBeNull();
    });

    test("wallet link conflict returns 409 WALLET_ALREADY_BOUND (R-ID-003)", async ({page, context}) => {
        // 预置冲突：地址 0xdeadbeef 已被「另一个用户」绑定
        const stub = await installPrivyStub(context, {
            initialProfile: STUB_PROFILES.student,
            initialSession: true,
        });
        stub.state().boundByOthers.add("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef");

        // 通过浏览器 fetch（让 context.route 拦截）
        await page.goto("/");
        const resp = await page.evaluate(async () => {
            const r = await fetch("/api/v1/me/wallets/link", {
                method: "POST",
                credentials: "include",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({
                    chainId: 11155111,
                    address: "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
                    nonce: "nonce-test",
                    expiry: new Date(Date.now() + 60_000).toISOString(),
                    signature: "0x" + "0".repeat(130),
                    domain: "localhost:4173",
                }),
            });
            return {status: r.status, body: await r.json()};
        });
        expect(resp.status).toBe(409);
        expect(resp.body.error?.code).toBe("WALLET_ALREADY_BOUND");
        expect(resp.body.error?.details?.address).toBe("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef");
    });
});
