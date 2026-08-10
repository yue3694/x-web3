/// <reference types="vitest" />
import {defineConfig} from "vite";

export default defineConfig({
    test: {
        // jsdom for *.tsx (rendering); pure helper *.ts still works in jsdom too.
        environment: "jsdom",
        include: ["src/**/*.test.{ts,tsx}"],
        globals: false,
    },
    resolve: {
        alias: {
            "@": new URL("./src", import.meta.url).pathname,
        },
    },
});