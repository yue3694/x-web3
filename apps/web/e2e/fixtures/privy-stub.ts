/**
 * Privy stub backend fixture.
 *
 * 设计：
 *   - E2E 阶段不联调真实 Privy 与 Go API；用 page.route 拦截 /api/v1/*；
 *   - 实现最小 endpoint surface，覆盖 F01 验收：
 *       POST /auth/privy/session — 登录（dev-stub 信任任意 token）
 *       GET  /me                — 读取 profile（无 sid → 401）
 *       DELETE /auth/session    — 登出
 *       POST /me/wallets/link   — 钱包绑定（idempotent + 409 冲突）
 *       POST /me/wallets/nonce  — 颁发 nonce
 *       GET  /admin/*           — 永远 401/403（验证隐藏路由不可信）
 *   - session 状态保留在 fixture 闭包内；Set-Cookie 由 route.fulfill 写；
 *   - stub state 可以被测试用例在执行期间通过 mutate() 调整。
 *
 * 注意：浏览器侧 cookie 行为由 Playwright 自带；sid 是测试 cookie，HttpOnly 标记
 * 仅模拟后端行为，不影响 Playwright 的 cookies API（仍可通过 page.cookies 看见）。
 */

import type {BrowserContext, Route} from "@playwright/test";

// —— 与 apps/web/src/api/types.ts 对齐的最小类型 ——
export type RoleCode = "student" | "teacher" | "super_admin";

export interface StubWallet {
    id: string;
    chainId: number;
    address: string;
    isPrimary: boolean;
    boundAt: string;
}

export interface StubProfile {
    id: string;
    displayName: string;
    primaryWallet: StubWallet | null;
    wallets: StubWallet[];
    roles: RoleCode[];
    permissions: string[];
}

const STUB_PROFILE_STUDENT: StubProfile = {
    id: "u_stub_student",
    displayName: "Stub Student",
    primaryWallet: null,
    wallets: [],
    roles: ["student"],
    permissions: ["COURSE_READ", "ORDER_CREATE", "LESSON_PROGRESS_WRITE", "CERTIFICATE_READ"],
};

const STUB_PROFILE_TEACHER: StubProfile = {
    id: "u_stub_teacher",
    displayName: "Stub Teacher",
    primaryWallet: null,
    wallets: [],
    roles: ["teacher"],
    permissions: [
        "COURSE_READ",
        "ORDER_CREATE",
        "LESSON_PROGRESS_WRITE",
        "CERTIFICATE_READ",
        "COURSE_CREATE",
        "COURSE_EDIT",
        "MEDIA_UPLOAD",
    ],
};

const STUB_PROFILE_SUPER: StubProfile = {
    id: "u_stub_super",
    displayName: "Stub Admin",
    primaryWallet: null,
    wallets: [],
    roles: ["super_admin"],
    permissions: ["*"],
};

export const STUB_PROFILES = {
    student: STUB_PROFILE_STUDENT,
    teacher: STUB_PROFILE_TEACHER,
    super_admin: STUB_PROFILE_SUPER,
} as const;

export interface StubState {
    /** 当前已登录 profile；null 表示未登录。 */
    profile: StubProfile | null;
    /** 已签发的 sid；null 表示未签发。 */
    sid: string | null;
    /** 已被其他用户绑定的钱包地址集合 → 触发 409 WALLET_ALREADY_BOUND */
    boundByOthers: Set<string>;
    /** 颁发过的 nonce（防重放；测试可观察） */
    issuedNonces: Set<string>;
    /** /admin/* 的固定状态，默认 401 */
    adminStatus: number;
}

export interface InstallOptions {
    /** 起始 profile：null=未登录；不传=默认 student */
    initialProfile?: StubProfile | null;
    /** 是否一开始就有 sid（让 /me 直接返回 profile） */
    initialSession?: boolean;
    /** /admin/* 默认返回状态，默认 401 */
    adminStatus?: number;
}

export interface StubHandle {
    state: () => Readonly<StubState>;
    /** 强制覆盖 profile（用于「篡改前端角色」之类的负向用例） */
    setProfile: (p: StubProfile | null) => void;
    /** 重置为未登录（不发请求；调用方需 reload） */
    reset: () => void;
}

const SID_COOKIE = "sid";

export async function installPrivyStub(
    target: BrowserContext | {context: () => BrowserContext; route: BrowserContext["route"]},
    opts: InstallOptions = {},
): Promise<StubHandle> {
    // Playwright route helper: prefer explicit BrowserContext.route so page.request
    // (APIRequestContext attached to the same context) is also intercepted.
    const context: BrowserContext =
        "route" in target && typeof target.route === "function"
            ? (target as BrowserContext)
            : (target as {context: () => BrowserContext}).context();
    const routeFn = context.route.bind(context);

    // initialSession=true 时直接 seed sid cookie，
    // 否则页面首次 /me 就会被服务端按未登录拒绝，profile 永远 null。
    const seededProfile = opts.initialProfile !== undefined ? opts.initialProfile : STUB_PROFILE_STUDENT;
    const seededSid = "stub-sid-seed";
    if (opts.initialSession === true && seededProfile) {
        await context.addCookies([
            {
                name: SID_COOKIE,
                value: seededSid,
                domain: "127.0.0.1",
                path: "/",
                httpOnly: false,
                sameSite: "Lax",
            },
        ]);
    }
    const state: StubState = {
        profile: seededProfile,
        sid: opts.initialSession === true && seededProfile ? seededSid : null,
        boundByOthers: new Set<string>(),
        issuedNonces: new Set<string>(),
        adminStatus: opts.adminStatus ?? 401,
    };

    let nextNonce = 1;

    // 兼容重复注册（同一 context 内多次 install 会被覆盖；Playwright 推荐每个测试新 context）
    await context.unrouteAll({behavior: "ignoreErrors"}).catch(() => undefined);
    await routeFn("**/api/v1/**", async (route: Route) => {
        const req = route.request();
        const url = new URL(req.url());
        const path = url.pathname.replace(/^\/api\/v1/, "");
        const method = req.method();

        // —— POST /auth/privy/session ——
        if (path === "/auth/privy/session" && method === "POST") {
            let body: {privyAccessToken?: string} = {};
            try {
                body = JSON.parse(req.postData() ?? "{}");
            } catch {
                // fallthrough to 400
            }
            if (!body.privyAccessToken) {
                return route.fulfill({
                    status: 400,
                    contentType: "application/json",
                    body: JSON.stringify({
                        error: {code: "INVALID_REQUEST", message: "missing privyAccessToken", requestId: "stub"},
                    }),
                });
            }
            // dev-stub 信任任意 token；幂等：相同 token 复用同 profile。
            const sid = `stub-sid-${body.privyAccessToken}`;
            state.sid = sid;
            if (!state.profile) state.profile = STUB_PROFILE_STUDENT;
            return route.fulfill({
                status: 200,
                headers: {
                    "Content-Type": "application/json",
                    // HttpOnly + Lax + Path=/ 模拟后端 cookie 写入
                    "Set-Cookie": `${SID_COOKIE}=${sid}; Path=/; HttpOnly; SameSite=Lax`,
                },
                body: JSON.stringify(state.profile),
            });
        }

        // —— GET /me ——
        if (path === "/me" && method === "GET") {
            const cookie = req.headers().cookie ?? "";
            const hasSid = state.sid !== null && cookie.includes(`${SID_COOKIE}=${state.sid}`);
            if (!hasSid || !state.profile) {
                return route.fulfill({
                    status: 401,
                    contentType: "application/json",
                    body: JSON.stringify({
                        error: {code: "SESSION_EXPIRED", message: "no session", requestId: "stub"},
                    }),
                });
            }
            return route.fulfill({
                status: 200,
                contentType: "application/json",
                body: JSON.stringify(state.profile),
            });
        }

        // —— DELETE /auth/session ——
        if (path === "/auth/session" && method === "DELETE") {
            state.sid = null;
            return route.fulfill({
                status: 204,
                headers: {
                    "Set-Cookie": `${SID_COOKIE}=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax`,
                },
            });
        }

        // —— POST /me/wallets/nonce ——
        if (path === "/me/wallets/nonce" && method === "POST") {
            if (!state.sid) {
                return route.fulfill({
                    status: 401,
                    contentType: "application/json",
                    body: JSON.stringify({
                        error: {code: "SESSION_EXPIRED", message: "no session", requestId: "stub"},
                    }),
                });
            }
            const nonce = `nonce-${nextNonce++}`;
            state.issuedNonces.add(nonce);
            return route.fulfill({
                status: 200,
                contentType: "application/json",
                body: JSON.stringify({
                    nonce,
                    domain: "localhost:4173",
                    expiresAt: new Date(Date.now() + 5 * 60_000).toISOString(),
                }),
            });
        }

        // —— POST /me/wallets/link ——
        if (path === "/me/wallets/link" && method === "POST") {
            if (!state.sid || !state.profile) {
                return route.fulfill({
                    status: 401,
                    contentType: "application/json",
                    body: JSON.stringify({
                        error: {code: "SESSION_EXPIRED", message: "no session", requestId: "stub"},
                    }),
                });
            }
            let body: {chainId?: number; address?: string} = {};
            try {
                body = JSON.parse(req.postData() ?? "{}");
            } catch {
                // fallthrough
            }
            const addr = (body.address ?? "").toLowerCase();
            if (!addr) {
                return route.fulfill({
                    status: 400,
                    contentType: "application/json",
                    body: JSON.stringify({
                        error: {code: "INVALID_REQUEST", message: "address required", requestId: "stub"},
                    }),
                });
            }
            if (state.boundByOthers.has(addr)) {
                return route.fulfill({
                    status: 409,
                    contentType: "application/json",
                    body: JSON.stringify({
                        error: {
                            code: "WALLET_ALREADY_BOUND",
                            message: "wallet is bound to another user",
                            requestId: "stub",
                            details: {address: addr},
                        },
                    }),
                });
            }
            // idempotent: 同地址已绑同 user → 不重复
            const existing = state.profile.wallets.find((w) => w.address.toLowerCase() === addr);
            if (existing) {
                return route.fulfill({
                    status: 200,
                    contentType: "application/json",
                    body: JSON.stringify({wallets: state.profile.wallets}),
                });
            }
            const wallet: StubWallet = {
                id: `w_${addr.slice(2, 10)}`,
                chainId: body.chainId ?? 11155111,
                address: addr,
                isPrimary: state.profile.wallets.length === 0,
                boundAt: new Date().toISOString(),
            };
            state.profile = {
                ...state.profile,
                wallets: [...state.profile.wallets, wallet],
                primaryWallet: state.profile.primaryWallet ?? wallet,
            };
            return route.fulfill({
                status: 200,
                contentType: "application/json",
                body: JSON.stringify({wallets: state.profile.wallets}),
            });
        }

        // —— /admin/* ——
        if (path.startsWith("/admin/")) {
            return route.fulfill({
                status: state.adminStatus,
                contentType: "application/json",
                body: JSON.stringify({
                    error: {code: "FORBIDDEN", message: "admin endpoint hidden", requestId: "stub"},
                }),
            });
        }

        // fallthrough：留给 Playwright 真实放行（默认 404 stub 响应）
        return route.fulfill({
            status: 404,
            contentType: "application/json",
            body: JSON.stringify({error: {code: "NOT_FOUND", message: `stub: no handler for ${method} ${path}`, requestId: "stub"}}),
        });
    });

    return {
        state: () => state,
        setProfile: (p) => {
            state.profile = p;
        },
        reset: () => {
            state.profile = null;
            state.sid = null;
        },
    };
}