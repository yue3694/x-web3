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

import {afterEach, describe, expect, it, vi} from "vitest";
import {cleanup, render, screen} from "@testing-library/react";
import {QueryClient, QueryClientProvider} from "@tanstack/react-query";

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
        hasRole: (role: string) => Boolean(mocks.profile?.roles.includes(role as any)),
        isAuthenticated: Boolean(mocks.profile),
        refresh: vi.fn(),
        logout: vi.fn(),
    }),
}));

vi.mock("connectkit", () => ({
    ConnectKitButton: {Custom: ({children}: {children: (args: any) => any}) => children({
        isConnected: false, isConnecting: false, show: () => {}, truncatedAddress: "0x", address: "0x", chain: {id: 11155111, name: "Sepolia"},
    })},
}));

vi.mock("@privy-io/react-auth", () => ({
    usePrivy: () => ({ready: true, authenticated: false, login: () => {}, logout: () => {}}),
}));

vi.mock("@/api/client", () => ({
    ApiClientError: class extends Error {},
    apiClient: {post: vi.fn(), put: vi.fn(), get: vi.fn(), delete: vi.fn()},
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

function renderWithProviders() {
    const qc = new QueryClient({defaultOptions: {queries: {retry: false}}});
    return render(<QueryClientProvider client={qc}><App /></QueryClientProvider>);
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
        expect(screen.getAllByText(/Published courses/i).length).toBeGreaterThanOrEqual(1);
        // Swap 在 nav link + section heading 出现两次；用 heading role 更精确
        expect(screen.getAllByRole("heading", {name: /^Swap$/}).length).toBeGreaterThanOrEqual(1);
        expect(screen.getByText(/Sign in to see your account/i)).toBeTruthy();
        expect(screen.getByText(/Sepolia Etherscan/i)).toBeTruthy();
    });

    it("TopNav exposes Catalog / Swap / Account anchors", () => {
        mocks.profile = null;
        renderWithProviders();
        const links = screen.getAllByRole("link", {name: /Catalog|Swap|Account/i});
        const hrefs = links.map((a) => a.getAttribute("href"));
        expect(hrefs.some((h) => h === "#catalog")).toBe(true);
        expect(hrefs.some((h) => h === "#swap")).toBe(true);
        expect(hrefs.some((h) => h === "#account")).toBe(true);
    });

    it("Account center renders sub-panels for authenticated users", () => {
        mocks.profile = {
            id: "u1", displayName: "Alice", roles: ["student"], permissions: [],
        };
        renderWithProviders();
        // 至少存在一个面板标题含这些文字（section h3 + 内部组件可能各有一个）
        expect(screen.getAllByText(/My orders/i).length).toBeGreaterThanOrEqual(1);
        expect(screen.getAllByText(/My enrollments/i).length).toBeGreaterThanOrEqual(1);
        expect(screen.getAllByText(/My certificates/i).length).toBeGreaterThanOrEqual(1);
        expect(screen.getAllByText(/My comments/i).length).toBeGreaterThanOrEqual(1);
    });

    it("Teacher Studio mounts when COURSE_CREATE permission is present", () => {
        mocks.profile = {
            id: "u2", displayName: "Prof", roles: ["teacher"], permissions: ["COURSE_CREATE"],
        };
        renderWithProviders();
        expect(screen.getByText(/Course studio/i)).toBeTruthy();
    });

    it("Admin console mounts when SYSTEM_ADMIN permission is present", () => {
        mocks.profile = {
            id: "u3", displayName: "Root", roles: ["super_admin"],
            // AdminLayout 用 code="admin" 守门；SYSTEM_ADMIN 与 admin 都给到保证嵌套通通放行
            permissions: ["SYSTEM_ADMIN", "admin"],
        };
        renderWithProviders();
        // Admin console 在外层 section heading + AdminLayout 内部 h2 各出现一次
        expect(screen.getAllByText(/Admin console/i).length).toBeGreaterThanOrEqual(1);
        expect(screen.getAllByText(/Users/i).length).toBeGreaterThanOrEqual(1);
        expect(screen.getByText(/Indexer sync/i)).toBeTruthy();
        expect(screen.getByText(/Dead-letter queue/i)).toBeTruthy();
    });

    it("Admin console is hidden for non-admin users", () => {
        mocks.profile = {
            id: "u4", displayName: "Student", roles: ["student"], permissions: [],
        };
        renderWithProviders();
        expect(screen.queryByText(/Admin console/i)).toBeNull();
    });
});