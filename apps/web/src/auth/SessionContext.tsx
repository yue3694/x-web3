/**
 * SessionContext: 当前登录用户 profile / 权限。
 *
 * 设计：
 *   - 启动时调 GET /me；如果返回 401，profile = null；
 *   - login() 调用 Privy SDK 拿 access token，发 /auth/privy/session；
 *     成功后刷新 profile；
 *   - logout() 调 /auth/session DELETE，再清本地 profile；
 *
 * 关键约束：
 *   - 权限判定永远在服务端，前端只用作 UX 隐藏；
 *   - 不在 useEffect 里调 login（按 .claude/rules/frontend.md）。
 */

import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useState,
    type ReactNode,
} from "react";

import {authApi, type Profile, type RoleCode} from "@/api/types";
import {ApiClientError} from "@/api/client";

export interface SessionState {
    profile: Profile | null;
    loading: boolean;
    /** 触发一次 /me 重新拉取（登录后或服务端状态变更） */
    refresh: () => Promise<void>;
    /** 登录入口（参数来自 Privy；dev stub 模式可传任意字符串） */
    login: (privyAccessToken: string) => Promise<Profile>;
    setAuthenticatedProfile: (profile: Profile) => void;
    logout: () => Promise<void>;
    /** 客户端权限检查：永远只是 UX 隐藏，权威在服务端 */
    hasPermission: (code: string) => boolean;
    hasRole: (code: RoleCode) => boolean;
}

const SessionContext = createContext<SessionState | null>(null);

interface SessionProviderProps {
    children: ReactNode;
    /** 自定义 fetch 实现（测试用） */
    initialLoading?: boolean;
}

export function SessionProvider({
    children,
    initialLoading = true,
}: SessionProviderProps) {
    const [profile, setProfile] = useState<Profile | null>(null);
    const [loading, setLoading] = useState<boolean>(initialLoading);

    const refresh = useCallback(async () => {
        setLoading(true);
        try {
            const p = await authApi.getMe();
            setProfile(p);
        } finally {
            setLoading(false);
        }
    }, []);

    const login = useCallback(async (privyAccessToken: string) => {
        const p = await authApi.loginWithPrivy(privyAccessToken);
        setProfile(p);
        return p;
    }, []);

    const logout = useCallback(async () => {
        try {
            await authApi.logout();
        } catch (e) {
            // 即使后端失败也清本地状态
            if (!(e instanceof ApiClientError)) {
                throw e;
            }
        }
        setProfile(null);
    }, []);

    const hasPermission = useCallback(
        (code: string) => {
            const roles = profile?.roles ?? [];
            const perms = profile?.permissions ?? [];
            if (roles.includes("super_admin")) return true;
            return perms.includes(code);
        },
        [profile],
    );

    const hasRole = useCallback(
        (code: RoleCode) => (profile?.roles ?? []).includes(code),
        [profile],
    );

    useEffect(() => {
        void refresh();
    }, [refresh]);

    const value = useMemo<SessionState>(
        () => ({profile, loading, refresh, login, setAuthenticatedProfile: setProfile, logout, hasPermission, hasRole}),
        [profile, loading, refresh, login, logout, hasPermission, hasRole],
    );

    return (
        <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
    );
}

export function useSession(): SessionState {
    const ctx = useContext(SessionContext);
    if (!ctx) {
        throw new Error("useSession must be used inside <SessionProvider>");
    }
    return ctx;
}
