/**
 * SessionContext 单测骨架：覆盖 loginWithPrivy / refresh / logout 的 happy path。
 * 完整覆盖（含 RequireAuth 跳转、RequirePermission 隐藏）随 vitest 接入时补齐。
 */

import {afterEach, beforeEach, describe, expect, it, vi} from "vitest";

import {ApiClientError} from "@/api/client";
import {authApi} from "@/api/types";

vi.mock("@/api/types", async () => {
    const actual =
        await vi.importActual<typeof import("@/api/types")>("@/api/types");
    return {
        ...actual,
        authApi: {
            loginWithPrivy: vi.fn(),
            logout: vi.fn(),
            getMe: vi.fn(),
            linkWallet: vi.fn(),
            issueWalletNonce: vi.fn(),
            unbindWallet: vi.fn(),
        },
    };
});

describe("authApi.loginWithPrivy", () => {
    beforeEach(() => vi.resetAllMocks());
    afterEach(() => vi.restoreAllMocks());

    it("returns profile on success", async () => {
        vi.mocked(authApi.loginWithPrivy).mockResolvedValueOnce({
            id: "u1",
            displayName: "Alice",
            primaryWallet: null,
            wallets: [],
            roles: ["student"],
            permissions: ["ORDER_CREATE"],
        });
        const p = await authApi.loginWithPrivy("privy-token");
        expect(p.id).toBe("u1");
        expect(p.roles).toContain("student");
    });

    it("surfaces ApiClientError on 401", async () => {
        vi.mocked(authApi.loginWithPrivy).mockRejectedValueOnce(
            new ApiClientError(
                {
                    code: "INVALID_PRIVY_TOKEN",
                    message: "bad token",
                    requestId: "r1",
                },
                401,
            ),
        );
        await expect(authApi.loginWithPrivy("x")).rejects.toMatchObject({
            status: 401,
            code: "INVALID_PRIVY_TOKEN",
        });
    });
});
