/// <reference types="vitest" />
import {defineConfig} from "vite";

export default defineConfig({
    test: {
        environment: "node",
        include: ["src/**/*.test.{ts,tsx}"],
        globals: false,
    },
    resolve: {
        alias: {
            "@": new URL("./src", import.meta.url).pathname,
        },
    },
});