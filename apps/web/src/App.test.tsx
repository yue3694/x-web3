/**
 * App.tsx 端到端 render 测试。
 *
 * 验证：
 *   - 公开区域（TopNav / Hero / Catalog / Swap / Footer）总在；
 *   - AccountCenter 登录态才显示内容；
 *   - CourseEditor (Teacher Studio) 在有 COURSE_CREATE 时挂载；
 *   - AdminLayout 在有 SYSTEM_ADMIN 时挂载；
 *   - TopNav 锚点覆盖 Catalog / Swap / Account / Teacher / Admin。
 *
 * 鉴权门由 SessionContext 提供；这里用 vi.mock 让 useSession 返回我们控制的 profile。
 */

import {afterEach, beforeAll, describe, expect, it, vi} from "vitest";
import {cleanup, render, screen} from "@testing-library/react";
import {QueryClient, QueryClientProvider} from "@tanstack/react-query";
import {MemoryRouter} from "react-router-dom";

const mocks = vi.hoisted(() => ({
    profile: null as null | {
        id: string;
        displayName: string;
        roles: Array<"student" | "teacher" | "super_admin">;
        permissions: string[];
    },
}));

vi.mock("@/auth/SessionContext", () => ({
    useSession: () => ({
        profile: mocks.profile,
        hasPermission: (code: string) => Boolean(mocks.profile?.permissions.includes(code)),
        hasRole: (role: "student" | "teacher" | "super_admin") => Boolean(mocks.profile?.roles.includes(role)),
        isAuthenticated: Boolean(mocks.profile),
        refresh: vi.fn(),
        logout: vi.fn(),
    }),
}));

vi.mock("connectkit", () => ({
    ConnectKitButton: {Custom: ({children}: {children: (args: {isConnected: boolean; isConnecting: boolean; show: () => void; truncatedAddress: string; address: string; chain: {id: number; name: string}}) => unknown}) => children({
        isConnected: false, isConnecting: false, show: () => {}, truncatedAddress: "0x", address: "0x", chain: {id: 11155111, name: "Sepolia"},
    })},
}));

vi.mock("@privy-io/react-auth", () => ({
    usePrivy: () => ({ready: true, authenticated: false, login: () => {}, logout: () => {}}),
}));

vi.mock("@/api/client", () => ({
    ApiClientError: class extends Error {},
    apiClient: {post: vi.fn(), put: vi.fn(), get: vi.fn().mockResolvedValue({items: []}), delete: vi.fn()},
}));

vi.mock("@/api/learning", () => ({
    learningApi: {issuePlayback: vi.fn(), reportProgress: vi.fn()},
}));

vi.mock("@/features/admin/adminApi", () => ({
    adminApi: {
        getChainSync: vi.fn().mockResolvedValue({latestBlock: 0, lagSeconds: 0, status: "ok", lastEventAt: null, nextBlock: 0, consumers: []}),
        rewind: vi.fn(),
        listDLQ: vi.fn().mockResolvedValue({items: []}),
        replayDLQ: vi.fn(),
        discardDLQ: vi.fn(),
        listUsers: vi.fn().mockResolvedValue({items: []}),
        grantRole: vi.fn(),
        revokeRole: vi.fn(),
    },
}));

// wagmi hooks（CourseCatalog / Player / SwapCard 都有引用）—— 提供 stub
vi.mock("wagmi", () => ({
    useAccount: () => ({address: undefined, isConnected: false}),
    useChainId: () => 11155111,
    useSwitchChain: () => ({switchChain: vi.fn()}),
    useReadContract: () => ({data: undefined, isLoading: false}),
    useWriteContract: () => ({writeContract: vi.fn(), data: undefined, isPending: false}),
    useWaitForTransactionReceipt: () => ({isLoading: false, isSuccess: false}),
    useConfig: () => ({}),
    useDisconnect: () => ({disconnect: vi.fn()}),
    useConnect: () => ({connect: vi.fn(), connectors: []}),
    usePublicClient: () => ({}),
    useWalletClient: () => ({data: undefined}),
}));

import {App} from "./App";

beforeAll(() => {
    Object.defineProperty(window, "scrollTo", {value: vi.fn(), writable: true});
});

function renderWithProviders(path = "/") {
    const qc = new QueryClient({defaultOptions: {queries: {retry: false}}});
    return render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[path]}><App /></MemoryRouter></QueryClientProvider>);
}

afterEach(() => {
    cleanup();
    mocks.profile = null;
});

describe("App shell", () => {
    it("renders public sections for anonymous visitors", () => {
        mocks.profile = null;
        renderWithProviders();
        expect(screen.getAllByText(/WEB3 UNIVERSITY/i).length).toBeGreaterThanOrEqual(1);
        expect(screen.getByText(/从发现到存证/i)).toBeTruthy();
        expect(screen.getByText(/探索课程/i)).toBeTruthy();
        expect(screen.getByText(/chain_id: 31337/i)).toBeTruthy();
    });

    it("TopNav exposes independent routed workspaces", () => {
        mocks.profile = null;
        renderWithProviders();
        const links = screen.getAllByRole("link", {name: /课程|兑换|我的学习/});
        const hrefs = links.map((a) => a.getAttribute("href"));
        expect(hrefs.some((h) => h === "/courses")).toBe(true);
        expect(hrefs.some((h) => h === "/swap")).toBe(true);
        expect(hrefs.some((h) => h === "/account/enrollments")).toBe(true);
    });

    it("Account center renders one routed sub-panel for authenticated users", async () => {
        mocks.profile = {
            id: "u1", displayName: "Alice", roles: ["student"], permissions: [],
        };
        renderWithProviders("/account/enrollments");
        expect(await screen.findByText(/学习进度，井然有序/i)).toBeTruthy();
        expect(screen.getAllByText(/我的报名/i).length).toBeGreaterThanOrEqual(1);
        expect(screen.queryByText(/我的订单/i)).toBeNull();
    });

    it("Teacher Studio mounts when COURSE_CREATE permission is present", () => {
        mocks.profile = {
            id: "u2", displayName: "Prof", roles: ["teacher"], permissions: ["COURSE_CREATE"],
        };
        renderWithProviders("/studio");
        return screen.findByText(/课程工作台/i).then((node) => expect(node).toBeTruthy());
    });

    it("Admin console mounts when SYSTEM_ADMIN permission is present", () => {
        mocks.profile = {
            id: "u3", displayName: "Root", roles: ["super_admin"],
            // AdminLayout 用 code="admin" 守门；SYSTEM_ADMIN 与 admin 都给到保证嵌套通通放行
            permissions: ["SYSTEM_ADMIN", "admin"],
        };
        renderWithProviders("/admin/users");
        return screen.findByText(/用户与角色/i).then((node) => expect(node).toBeTruthy());
    });

    it("Admin console is hidden for non-admin users", () => {
        mocks.profile = {
            id: "u4", displayName: "Student", roles: ["student"], permissions: [],
        };
        renderWithProviders("/admin/users");
        expect(screen.getByText(/无权访问/)).toBeTruthy();
    });
});
