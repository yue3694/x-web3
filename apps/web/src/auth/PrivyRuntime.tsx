import {lazy, Suspense} from "react";
import type {ReactNode} from "react";

const PrivyProviderRuntime = lazy(() => import("./PrivyProviderRuntime"));

const PRIVY_APP_ID = import.meta.env.VITE_PRIVY_APP_ID ?? "";
export const usesPrivyDevStub = import.meta.env.VITE_PRIVY_DEV_STUB === "1";

export function PrivyRuntime({children}: {children: ReactNode}) {
    if (usesPrivyDevStub) {
        return children;
    }
    if (!PRIVY_APP_ID) {
        throw new Error("VITE_PRIVY_APP_ID is required when Privy dev stub is disabled");
    }
	return (
		<Suspense fallback={null}>
			<PrivyProviderRuntime appId={PRIVY_APP_ID}>
				{children}
			</PrivyProviderRuntime>
		</Suspense>
	);
}
