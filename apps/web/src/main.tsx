import {StrictMode} from "react";
import {createRoot} from "react-dom/client";
import {QueryClient, QueryClientProvider} from "@tanstack/react-query";
import {WagmiProvider} from "wagmi";
import {ConnectKitProvider} from "connectkit";

import {App} from "./App";
import {wagmiConfig} from "./wagmi";
import {SessionProvider} from "./auth/SessionContext";
import {PrivyRuntime} from "./auth/PrivyRuntime";
import "./styles.css";

const queryClient = new QueryClient();

// Cyberpunk theme overrides ConnectKit's CSS variables.
const cyberpunkTheme = {
    "--ck-font-family":
        "'Inter', system-ui, -apple-system, BlinkMacSystemFont, sans-serif",
    "--ck-border-radius": "2px",
    "--ck-primary-button-color": "#05050f",
    "--ck-primary-button-background": "#ff2e9a",
    "--ck-primary-button-box-shadow": "0 0 18px rgba(255, 46, 154, 0.55)",
    "--ck-primary-button-hover-background": "#ff4baa",
    "--ck-primary-button-hover-color": "#05050f",
    "--ck-primary-button-hover-box-shadow":
        "0 0 24px rgba(255, 46, 154, 0.75)",
    "--ck-secondary-button-background": "transparent",
    "--ck-secondary-button-color": "#00e5ff",
    "--ck-secondary-button-border-color": "rgba(0, 229, 255, 0.55)",
    "--ck-secondary-button-hover-background":
        "rgba(0, 229, 255, 0.10)",
    "--ck-secondary-button-hover-color": "#7df3ff",
    "--ck-secondary-button-hover-border-color": "#00e5ff",
    "--ck-modal-background": "#0a0a18",
    "--ck-modal-box-shadow":
        "0 0 0 1px rgba(255, 46, 154, 0.35), 0 24px 60px rgba(255, 46, 154, 0.25)",
    "--ck-modal-border-radius": "4px",
    "--ck-overlay-background": "rgba(5, 5, 16, 0.85)",
    "--ck-overlay-backdrop-filter": "blur(6px)",
    "--ck-body-color": "#f0f0ff",
    "--ck-body-color-muted": "#8a8aab",
    "--ck-body-color-danger": "#ff3860",
    "--ck-body-color-valid": "#00ff9c",
    "--ck-body-background": "#0a0a18",
    "--ck-body-background-secondary": "#13131f",
    "--ck-body-background-tertiary": "#1f1f33",
    "--ck-qr-background": "#05050f",
    "--ck-qr-color": "#00e5ff",
    "--ck-tooltip-background": "#13131f",
    "--ck-tooltip-color": "#00e5ff",
    "--ck-tooltip-shadow": "0 0 12px rgba(0, 229, 255, 0.45)",
    "--ck-focus-color": "#00e5ff",
    "--ck-spinner-color": "#ff2e9a",
    "--ck-recent-badge-color": "#ffd400",
    "--ck-recent-badge-background": "rgba(255, 212, 0, 0.10)",
    "--ck-recent-badge-border-radius": "2px",
} as const;

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("Root element #root not found");

createRoot(rootElement).render(
    <StrictMode>
		<PrivyRuntime>
			<WagmiProvider config={wagmiConfig}>
				<QueryClientProvider client={queryClient}>
					<ConnectKitProvider
                    mode="dark"
                    customTheme={cyberpunkTheme}
                    options={{
                        hideQuestionMarkCTA: true,
                        disclaimer: undefined,
                        hideRecentBadge: false,
                    }}
                >
						<SessionProvider>
							<App />
						</SessionProvider>
					</ConnectKitProvider>
				</QueryClientProvider>
			</WagmiProvider>
		</PrivyRuntime>
    </StrictMode>,
);
