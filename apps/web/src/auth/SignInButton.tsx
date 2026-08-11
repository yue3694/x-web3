/**
 * SignInButton — 触发 Privy 登录。
 *
 * 注：本仓前端使用 ConnectKit + wagmi 进行钱包交互。
 * Privy access token 的获取方式（MVP 占位）：
 *   - 真实环境：通过 @privy-io/react-auth 调 usePrivy().getAccessToken()；
 *   - 本地 dev（PRIVY_DEV_STUB=1）：前端直接传字符串 "stub" 给后端，
 *     后端 dev stub 验证器签发固定 subject 的 session。
 *
 * 如果要切换到真 Privy SDK，把 loginPrivy 替换成 usePrivy().login() →
 * useAccessToken() 即可。其它代码（loginWithPrivy 调用）不变。
 */

import {lazy, Suspense, useState} from "react";
import {useSession} from "./SessionContext";
import {usesPrivyDevStub} from "./PrivyRuntime";

const PrivySignInButton = lazy(() => import("./PrivySignInButton"));

interface SignInButtonProps {
    /** 自定义登录触发器（默认按钮） */
    children?: React.ReactNode;
    /** 自定义 className（用于在不同容器内复用样式） */
    className?: string;
}

export function SignInButton({children, className}: SignInButtonProps) {
	if (!usesPrivyDevStub) {
		return (
			<Suspense fallback={<button disabled>登录加载中…</button>}>
				<PrivySignInButton>{children}</PrivySignInButton>
			</Suspense>
		);
	}
	return (
		<DevSignInButton className={className}>{children}</DevSignInButton>
	);
}

function DevSignInButton({children, className}: SignInButtonProps) {
    const {login} = useSession();
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const handle = async () => {
        setBusy(true);
        setError(null);
        try {
            // 真实环境替换为：
            //   const {getAccessToken} = usePrivy();
            //   const privyAccessToken = await getAccessToken();
			await login("stub");
        } catch (e) {
            setError(e instanceof Error ? e.message : "登录失败");
        } finally {
            setBusy(false);
        }
    };

    return (
        <button
            type="button"
            className={className}
            onClick={handle}
            disabled={busy}
        >
            {children ?? (busy ? "登录中…" : "登录")}
            {error ? <span role="alert"> — {error}</span> : null}
        </button>
    );
}
