import {StrictMode} from "react";
import {createRoot} from "react-dom/client";
import {QueryClient, QueryClientProvider} from "@tanstack/react-query";
import {WagmiProvider} from "wagmi";

import {App} from "./App";
import {wagmiConfig} from "./wagmi";
import "./styles.css";

const queryClient = new QueryClient();

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("Root element #root not found");

createRoot(rootElement).render(
    <StrictMode>
        <WagmiProvider config={wagmiConfig}>
            <QueryClientProvider client={queryClient}>
                <App />
            </QueryClientProvider>
        </WagmiProvider>
    </StrictMode>,
);