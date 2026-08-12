/**
 * Playwright E2E 配置 — apps/web。
 *
 * 目标：
 *   - 不依赖真实 Privy SDK：通过 VITE_PRIVY_DEV_STUB=1 让前端直接信任字符串 token；
 *   - 不依赖真实 Go API：所有 /api/v1/* 由 e2e/fixtures/privy-stub.ts 用 page.route 拦截；
 *   - 启动 Vite dev server（同源 4173），配合 strictPort 让 webServer 自检 URL。
 *
 * 跑测试：
 *   pnpm --filter @x-web3/web e2e:install    # 一次性下载 chromium
 *   pnpm --filter @x-web3/web e2e            # headless run
 *   pnpm --filter @x-web3/web e2e:ui         # 调试 UI
 *
 * CI 环境变量 CI=1 时 reporter 切到 github，关闭 reuseExistingServer。
 */
import {defineConfig, devices} from "@playwright/test";

const PORT = Number(process.env.E2E_WEB_PORT ?? 4173);
const HOST = "127.0.0.1";
const BASE_URL = `http://${HOST}:${PORT}`;
const IS_CI = Boolean(process.env.CI);

export default defineConfig({
    testDir: "./e2e",
    // Playwright 默认 30s；F01 auth 流程 mock backend 通常 < 5s，给宽一点。
    timeout: 30_000,
    expect: {timeout: 5_000},
    fullyParallel: true,
    forbidOnly: IS_CI,
    retries: IS_CI ? 2 : 0,
    workers: IS_CI ? 2 : undefined,
    reporter: IS_CI ? [["github"], ["list"]] : "list",
    outputDir: "./test-results",
    use: {
        baseURL: BASE_URL,
        trace: "retain-on-failure",
        screenshot: "only-on-failure",
        video: "retain-on-failure",
        // 同源 + HttpOnly cookie 走 stub 控制；CI 关掉 geolocation 弹窗噪音
        permissions: [],
    },
    projects: [
        {
            name: "chromium",
            use: {
                ...devices["Desktop Chrome"],
                channel: undefined,
                viewport: {width: 1280, height: 800},
            },
        },
    ],
    webServer: {
        // Vite dev server：避免要求先 build；page.route 在浏览器侧拦截 /api/v1/*，
        // 不会真正走到 Vite proxy / backend。
        command: `pnpm exec vite --port ${PORT} --host ${HOST} --strictPort`,
        url: BASE_URL,
        reuseExistingServer: !IS_CI,
        timeout: 60_000,
        stdout: "pipe",
        stderr: "pipe",
        env: {
            // Privy dev stub：跳过真实 SDK 加载（PrivyRuntime 直接返回 children）。
            VITE_PRIVY_DEV_STUB: "1",
            // 同源路径，让 page.route 能稳定拦截 /api/v1/*。
            VITE_API_BASE_URL: "/api/v1",
            // 测试用空值：避免连真 RPC；wagmi/ConnectKit 启动会延迟到点击 "Connect Wallet" 才用到。
            VITE_SEPOLIA_RPC_URL: "",
            VITE_PRIVY_APP_ID: "",
            VITE_WALLETCONNECT_PROJECT_ID: "",
            VITE_APP_URL: BASE_URL,
            VITE_DEPLOYER_ADDRESS: "",
            VITE_COUNTER_CONTRACT_ADDRESS: "",
            VITE_NOTEPAD_CONTRACT_ADDRESS: "",
            VITE_COURSE_MARKET_ADDRESS: "0x2222222222222222222222222222222222222222",
            VITE_YD_TOKEN_ADDRESS: "0x1010101010101010101010101010101010101010",
            VITE_SEPOLIA_YD_SALE_ADDRESS: "0x3030303030303030303030303030303030303030",
        },
    },
});
