import {usePrivy} from "@privy-io/react-auth";
import {useState} from "react";
import type {ReactNode} from "react";

import {useSession} from "./SessionContext";

export default function PrivySignInButton({children}: {children?: ReactNode}) {
	const {ready, authenticated, login: openLogin, getAccessToken} = usePrivy();
	const {login: createPlatformSession} = useSession();
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const handle = async () => {
		setError(null);
		if (!authenticated) {
			openLogin();
			return;
		}
		setBusy(true);
		try {
			const token = await getAccessToken();
			if (!token) throw new Error("Privy 访问令牌不可用");
			await createPlatformSession(token);
		} catch (e) {
			setError(e instanceof Error ? e.message : "登录失败");
		} finally {
			setBusy(false);
		}
	};

	return (
		<button type="button" onClick={handle} disabled={!ready || busy}>
			{children ?? (busy ? "登录中…" : authenticated ? "继续" : "登录")}
			{error ? <span role="alert"> — {error}</span> : null}
		</button>
	);
}
