/**
 * F01 切片的 API 客户端：
 *   - loginWithPrivy → POST /auth/privy/session
 *   - logout → DELETE /auth/session
 *   - getMe → GET /me
 *   - linkWallet → POST /me/wallets/link
 *   - unbindWallet → DELETE /me/wallets/{id}
 *
 * 这些函数与 packages/shared/openapi/auth.yaml 一一对应。
 */

import {apiClient} from "./client";

export type RoleCode = "student" | "teacher" | "super_admin";

export interface Wallet {
    id: string;
    chainId: number;
    address: string;
    isPrimary: boolean;
    boundAt: string;
}

export interface Profile {
    id: string;
    displayName: string;
    primaryWallet: Wallet | null;
    wallets: Wallet[];
    roles: RoleCode[];
    permissions: string[];
}

export interface WalletLinkRequest {
    chainId: number;
    address: string;
    nonce: string;
    expiry: string;
    signature: string;
    domain?: string;
}

export interface WalletNonce {
	nonce: string;
	domain: string;
	expiresAt: string;
}

export const authApi = {
    async loginWithPrivy(privyAccessToken: string): Promise<Profile> {
        return apiClient.post<Profile>("/auth/privy/session", {
            privyAccessToken,
        });
    },
    async logout(): Promise<void> {
        await apiClient.delete<void>("/auth/session");
    },
    async getMe(): Promise<Profile | null> {
        try {
            return await apiClient.get<Profile>("/me");
        } catch (e: unknown) {
            // 401 = not logged in → return null instead of throwing
            if (
                e instanceof Error &&
                "status" in e &&
                (e as {status: number}).status === 401
            ) {
                return null;
            }
            throw e;
        }
    },
	async linkWallet(req: WalletLinkRequest): Promise<{wallets: Wallet[]}> {
        return apiClient.post<{wallets: Wallet[]}>("/me/wallets/link", req);
	},
	async issueWalletNonce(): Promise<WalletNonce> {
		return apiClient.post<WalletNonce>("/me/wallets/nonce");
	},
    async unbindWallet(walletId: string): Promise<void> {
        await apiClient.delete<void>(`/me/wallets/${walletId}`);
    },
};
