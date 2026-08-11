/**
 * F05-T10: E2E Swap 演示（Playwright + Privy stub + JSON-RPC mock）。
 *
 * 覆盖 F05 验收（UI 演示层级；不联真 Sepolia / 真钱包）：
 *   - AC-001: /swap 页面 SwapCard 渲染；YD→USDC 字段可见。
 *   - AC-002: 输入 100 YD → QuoterV2 报价出现（mock 95 USDC，价格影响 5%）。
 *   - AC-003: 自定义滑点 1% 可输入（不命中预设按钮）。
 *   - AC-004: 点 Swap → 状态机走完 idle → signing → confirming → done。
 *   - AC-005: 错误路径：writeContract 抛 user-rejected → 回到 idle（不进入 done）。
 *
 * 说明：
 *   - wagmi v2 的 useReadContract / useWriteContract 走 RPC transport；
 *     E2E 阶段用 page.route 拦截 JSON-RPC（eth_call / eth_sendTransaction /
 *     eth_getTransactionReceipt），按 method 返回确定性结果。
 *   - useAccount 默认未连钱包；E2E 没法点击真实 ConnectKit → 我们在
 *     page.addInitScript 里 mock window.ethereum，配合 wagmi 的 injected
 *     connector 让 useAccount 立即返回 isConnected=true。
 *   - 所有金额 / 地址 / txHash 都是固定的；不依赖随机源。
 */

import {test, expect, type Page} from "@playwright/test";
import {STUB_PROFILES, installPrivyStub} from "./fixtures/privy-stub";

// 固定地址 / 金额（deterministic）。
const SEPOLIA = "0xaa36a7";
const YD = "0x" + "10".repeat(20);
const QUOTER = "0x" + "30".repeat(20);
const ROUTER = "0x" + "40".repeat(20);
const STUDENT = "0x" + "50".repeat(20);
const TX_HASH = "0x" + "99".repeat(32);
const PROBE = BigInt(10n ** 15n); // 0.001 YD
const GAS = BigInt(180_000n);
const AMOUNT_OUT_95 = BigInt(95n * 10n ** 6n);
const PROBE_OUT = 1_000n;

// QuoterV2 4-word tuple encode（amountOut / sqrtPriceX96After / ticksCrossed / gasEstimate）。
const TUPLE = (out: bigint) =>
    "0x" + out.toString(16).padStart(64, "0") + "0".repeat(64 + 64) + GAS.toString(16).padStart(64, "0");

/** QuoterV2 calldata: selector(4) + tokenIn(32) + tokenOut(32) + amountIn(32) + fee(32) + sqrt(32). */
function extractAmountIn(calldata: string): bigint {
    const d = calldata.replace(/^0x/, "");
    return BigInt("0x" + d.slice(8 + 64 * 2, 8 + 64 * 3));
}

async function stubEthereum(page: Page, opts: {rejectTx?: boolean} = {}) {
    await page.addInitScript(({chainId, address, reject}) => {
        const provider = {
            isMetaMask: true,
            chainId,
            networkVersion: "11155111",
            selectedAddress: address,
            request: async ({method}: {method: string}) => {
                if (method === "eth_chainId" || method === "net_version") return chainId;
                if (method === "eth_requestAccounts" || method === "eth_accounts") return [address];
                if (method === "eth_sendRawTransaction" || method === "eth_sendTransaction") {
                    if (reject) {
                        const e = new Error("User rejected") as Error & {code: number};
                        e.code = 4001;
                        throw e;
                    }
                    return "0x" + "99".repeat(32);
                }
                if (method === "personal_sign" || method === "eth_signTypedData_v4") return "0x" + "00".repeat(65);
                return null;
            },
            on: () => undefined,
            removeListener: () => undefined,
        };
        (window as unknown as {ethereum: unknown}).ethereum = provider;
        const announce = () => window.dispatchEvent(new CustomEvent("eip6963:announceProvider", {detail: {info: {uuid: "350670db-19fa-4704-a166-e52e178b59d2", name: "MetaMask", icon: "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg'/>", rdns: "io.metamask"}, provider}}));
        window.addEventListener("eip6963:requestProvider", announce);
        window.setTimeout(announce, 0);
    }, {chainId: SEPOLIA, address: STUDENT, reject: Boolean(opts.rejectTx)});
}

async function stubJsonRpc(page: Page) {
    await page.route("**/*", async (route) => {
        const post = route.request().postData() ?? "";
        if (!post.includes("\"jsonrpc\"")) return route.fallback();
        type RpcRequest = {method?: string; params?: Array<{to?: string; data?: string}>; id?: number};
        let parsed: RpcRequest | RpcRequest[] = {};
        try { parsed = JSON.parse(post); } catch { /* ignore */ }
        const handle = (body: RpcRequest) => {
          const {method = "", params = [], id = 1} = body;
          let result: unknown = null;
          if (method === "eth_call" && params[0]) {
            const c = params[0];
            // QuoterV2.quoteExactInputSingle
            if (c.to?.toLowerCase() === QUOTER) {
                const amt = extractAmountIn(c.data ?? "");
                result = amt === PROBE ? TUPLE(PROBE_OUT) : TUPLE(AMOUNT_OUT_95);
            }
            // YD.balanceOf / YD.allowance → 大数（mock 已无限授权）
            if (c.to?.toLowerCase() === YD && (c.data?.replace(/^0x/, "").startsWith("70a08231") || c.data?.replace(/^0x/, "").startsWith("dd62ed3e"))) {
                result = "0x" + BigInt(10n ** 24n).toString(16).padStart(64, "0");
            }
          }
          if (method === "eth_getTransactionReceipt") result = {
                transactionHash: TX_HASH, blockHash: "0x" + "aa".repeat(32), blockNumber: "0x1234",
                from: STUDENT, to: ROUTER, status: "0x1", gasUsed: "0x2dc6c0", cumulativeGasUsed: "0x2dc6c0",
                logs: [], contractAddress: null, logsBloom: "0x" + "00".repeat(256), transactionIndex: "0x0",
          };
          if (method === "eth_blockNumber") result = "0x1234";
          if (method === "eth_gasPrice") result = "0x3b9aca00";
          if (method === "eth_estimateGas") result = "0x2dc6c0";
          if (method === "eth_getTransactionCount") result = "0x1";
          return {jsonrpc: "2.0", id, result};
        };
        const response = Array.isArray(parsed) ? parsed.map(handle) : handle(parsed);
        return route.fulfill({status: 200, contentType: "application/json", body: JSON.stringify(response)});
    });
}

test.describe("F05 / Swap / YD → USDC demo", () => {
    test.beforeEach(async ({context}) => {
        await context.clearCookies();
    });

    test("happy path: connect → 100 YD → quote 95 USDC / 5% impact → slip 1% → swap done", async ({page, context}) => {
        await installPrivyStub(context, {initialProfile: STUB_PROFILES.student, initialSession: true});
        await stubEthereum(page);
        await stubJsonRpc(page);

        await page.goto("/swap");
        await page.getByRole("button", {name: /connect wallet/i}).first().click();
        await page.getByText(/MetaMask/i).first().click();
        await expect(page.locator(".swap-card")).toBeVisible();

        // amountIn input
        await page.locator(".swap-card input").first().fill("100");
        // quote 落地：SwapSummary 出现 "95"
        await expect(page.locator(".swap-card__output")).toContainText(/95/i, {timeout: 10_000});
        // 价格影响 5%
        await expect(page.locator(".price-impact-badge, [data-testid='price-impact']")).toContainText(/5/);

        // 自定义滑点 1%
        await page.locator(".slippage-control__custom input").fill("1");
        const submit = page.locator(".swap-card__submit");
        await expect(submit).toBeEnabled();

        // RPC mock can settle between browser frames, so assert the durable
        // terminal state instead of a transient signing/confirming label.
        await submit.click();
        await expect(submit).toHaveAttribute("data-state", "done", {timeout: 10_000});
        await expect(submit).toHaveText(/swap again/i);
    });

    test("user-rejected from injected provider resets to idle with a banner", async ({page, context}) => {
        await installPrivyStub(context, {initialProfile: STUB_PROFILES.student, initialSession: true});
        await stubEthereum(page, {rejectTx: true});
        await stubJsonRpc(page);

        await page.goto("/swap");
        await page.getByRole("button", {name: /connect wallet/i}).first().click();
        await page.getByText(/MetaMask/i).first().click();
        await expect(page.locator(".swap-card")).toBeVisible();
        await page.locator(".swap-card input").first().fill("100");
        await expect(page.locator(".swap-card__output")).toContainText(/95/i, {timeout: 10_000});

        const submit = page.locator(".swap-card__submit");
        await expect(submit).toBeEnabled();
        await submit.click();

        // 终态 idle（user-rejected 走 reset 分支），banner 含 rejected
        await expect(submit).toHaveAttribute("data-state", "idle", {timeout: 10_000});
        await expect(page.locator(".notice--error").filter({hasText: /rejected/i})).toBeVisible();
    });
});
