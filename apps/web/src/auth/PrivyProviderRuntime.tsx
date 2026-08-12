import {PrivyProvider} from "@privy-io/react-auth";
import type {ReactNode} from "react";

export default function PrivyProviderRuntime({
	appId,
	children,
}: {
	appId: string;
	children: ReactNode;
}) {
	return (
		<PrivyProvider appId={appId} config={{appearance: {theme: "dark"}}}>
			{children}
		</PrivyProvider>
	);
}
