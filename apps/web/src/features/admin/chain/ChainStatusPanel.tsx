/**
 * ChainStatusPanel — Admin → Chain 同步状态面板。
 *
 * 行为：
 *   - 拉取 GET /admin/chain/sync?chainId=；
 *   - 三个指标：nextBlock / lagSeconds / lastUpdatedAt；
 *   - 颜色档位（按需求）：
 *       绿 lagSeconds < 30
 *       黄 30 ≤ lagSeconds ≤ 300
 *       红 lagSeconds > 300
 *   - 顶部 chain id 选择器（预填当前 wagmi chainId；fallback 11155111 Sepolia）。
 *   - 鉴权由 AdminLayout 完成；这里不重复 gate。
 */

import {useCallback, useEffect, useState} from "react";
import {useAccount} from "wagmi";

import {ApiClientError} from "@/api/client";
import {TARGET_CHAIN_ID, TARGET_CHAIN_NAME} from "@/chains";
import {adminApi} from "@/features/admin/adminApi";
import type {ChainSyncStatus} from "@/features/admin/adminTypes";

const FALLBACK_CHAIN_ID = TARGET_CHAIN_ID;

const KNOWN_CHAINS: ReadonlyArray<{id: number; label: string}> = [
    {id: TARGET_CHAIN_ID, label: TARGET_CHAIN_NAME},
    ...(TARGET_CHAIN_ID === 11155111 ? [] : [{id: 11155111, label: "Sepolia"}]),
    {id: 1, label: "Ethereum mainnet"},
    {id: 137, label: "Polygon"},
    {id: 421614, label: "Arbitrum Sepolia"},
];

type LagLevel = "ok" | "warn" | "danger";

function lagLevel(seconds: number | null): LagLevel {
    if (seconds === null) return "warn";
    if (seconds < 30) return "ok";
    if (seconds <= 300) return "warn";
    return "danger";
}

function lagColor(level: LagLevel): string {
    if (level === "ok") return "var(--accent-mint)";
    if (level === "warn") return "var(--accent-amber)";
    return "var(--accent-rose)";
}

function formatTimestamp(iso: string | null): string {
    if (!iso) return "Unavailable";
    const d = new Date(iso);
    if (Number.isNaN(d.valueOf())) return iso;
    return `${new Intl.DateTimeFormat("en-US", {
        year: "numeric",
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
    }).format(d)} (${Math.max(0, Math.floor((Date.now() - d.valueOf()) / 1000))}s ago)`;
}

const sectionStyle = {marginTop: 0} as const;
const headerStyle = {marginBottom: "0.8rem"} as const;
const toolbarStyle = {
    display: "flex",
    alignItems: "center",
    gap: "0.6rem",
    margin: "0.6rem 0 1rem",
} as const;
const selectStyle = {
    width: "auto",
    minWidth: "12rem",
} as const;
const gridStyle = {
    display: "grid",
    gridTemplateColumns: "repeat(3, minmax(0, 1fr))",
    gap: "0.8rem",
} as const;

const tileBase = {
    padding: "0.9rem 1rem",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-md)",
    background: "var(--bg-elev)",
} as const;

const tileLabelStyle = {
    color: "var(--fg-muted)",
    font: "500 0.7rem/1 var(--font-mono)",
    letterSpacing: "0.1em",
    textTransform: "uppercase" as const,
};
const tileValueStyle = {
    marginTop: "0.35rem",
    font: "600 1.4rem/1.2 var(--font-mono)",
    color: "var(--fg)",
};
const tileSubStyle = {
    marginTop: "0.3rem",
    color: "var(--fg-muted)",
    fontSize: "0.78rem",
};

const gaugeTrackStyle = {
    marginTop: "0.55rem",
    position: "relative" as const,
    height: "6px",
    borderRadius: "999px",
    background: "var(--bg-elev-2)",
    overflow: "hidden",
};
const gaugeFillBase = {
    height: "100%",
    borderRadius: "999px",
    transition: "width 220ms ease",
} as const;

const errorBoxStyle = {
    margin: "0.6rem 0",
    padding: "0.6rem 0.8rem",
    border: "1px solid rgba(244, 63, 94, 0.3)",
    background: "rgba(244, 63, 94, 0.07)",
    color: "#fda4af",
    borderRadius: "var(--radius-sm)",
} as const;

export function ChainStatusPanel() {
    const {chainId: wagmiChainId} = useAccount();
    const [chainId, setChainId] = useState<number>(wagmiChainId ?? FALLBACK_CHAIN_ID);
    const [status, setStatus] = useState<ChainSyncStatus | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const resp = await adminApi.getChainSync(chainId);
            setStatus(resp);
        } catch (cause) {
            if (cause instanceof ApiClientError) {
                setError(`${cause.code}: ${cause.message}`);
            } else {
                setError("Unable to load chain sync status.");
            }
        } finally {
            setLoading(false);
        }
    }, [chainId]);

    useEffect(() => {
        if (wagmiChainId && wagmiChainId !== chainId) {
            setChainId(wagmiChainId);
        }
        // 只在 wagmi chain 改变时同步一次，避免覆盖用户手动选择。
    }, [wagmiChainId]);

    useEffect(() => {
        void load();
    }, [load]);

    // 30s 自动刷新一次（hook 顺序：先 setInterval 再 render）。
    useEffect(() => {
        const id = window.setInterval(() => {
            void load();
        }, 30_000);
        return () => window.clearInterval(id);
    }, [load]);

    const level: LagLevel = status ? lagLevel(status.lagSeconds) : "ok";
    const fillColor = lagColor(level);
    // 进度：把 0..600s 映射到 0..100%，封顶 100。
    const fillPct = status?.lagSeconds != null ? Math.min(100, (status.lagSeconds / 600) * 100) : 0;

    return (
            <section className="panel" style={sectionStyle} aria-labelledby="chain-title">
                <header style={headerStyle}>
                    <span className="eyebrow">Admin · Chain</span>
                    <h2 id="chain-title">Indexer sync</h2>
                    <p style={{color: "var(--fg-muted)", margin: 0}}>
                        Live status of the chain indexer. Threshold:{" "}
                        <strong style={{color: lagColor("ok")}}>&lt; 30s</strong>{" "}
                        healthy,{" "}
                        <strong style={{color: lagColor("warn")}}>30–300s</strong>{" "}
                        warning,{" "}
                        <strong style={{color: lagColor("danger")}}>&gt; 300s</strong>{" "}
                        lagging.
                    </p>
                </header>

                <div style={toolbarStyle}>
                    <label
                        htmlFor="chain-select"
                        style={{color: "var(--fg-muted)", fontSize: "0.84rem"}}
                    >
                        Chain
                    </label>
                    <select
                        id="chain-select"
                        value={chainId}
                        onChange={(e) => setChainId(Number(e.target.value))}
                        style={selectStyle}
                        disabled={loading}
                    >
                        {KNOWN_CHAINS.map((c) => (
                            <option key={c.id} value={c.id}>
                                {c.label} ({c.id})
                            </option>
                        ))}
                    </select>
                    <button
                        type="button"
                        className="btn--ghost"
                        onClick={() => void load()}
                        disabled={loading}
                    >
                        {loading ? "Refreshing…" : "Refresh"}
                    </button>
                </div>

                {error ? (
                    <div role="alert" style={errorBoxStyle}>
                        {error}{" "}
                        <button
                            type="button"
                            className="btn--ghost"
                            onClick={() => void load()}
                        >
                            Retry
                        </button>
                    </div>
                ) : null}

                <div style={gridStyle}>
                    <article style={tileBase} aria-label="Next block">
                        <div style={tileLabelStyle}>nextBlock</div>
                        <div style={tileValueStyle}>
                            {status ? status.nextBlock.toLocaleString() : "—"}
                        </div>
                        <div style={tileSubStyle}>
                            Block the indexer will process next.
                        </div>
                    </article>

                    <article style={tileBase} aria-label="Lag seconds">
                        <div style={tileLabelStyle}>lagSeconds</div>
                        <div style={{...tileValueStyle, color: fillColor}}>
                            {status?.lagSeconds != null ? `${status.lagSeconds.toFixed(0)}s` : "—"}
                        </div>
                        <div style={gaugeTrackStyle} aria-hidden="true">
                            <div
                                style={{
                                    ...gaugeFillBase,
                                    width: `${fillPct}%`,
                                    background: fillColor,
                                }}
                            />
                        </div>
                        <div style={tileSubStyle}>
                            Level:{" "}
                            <strong style={{color: fillColor}}>
                                {level === "ok"
                                    ? "healthy"
                                    : level === "warn"
                                      ? "warning"
                                      : "lagging"}
                            </strong>
                        </div>
                    </article>

                    <article style={tileBase} aria-label="Last updated">
                        <div style={tileLabelStyle}>lastUpdatedAt</div>
                        <div style={{...tileValueStyle, fontSize: "1rem"}}>
                            {status ? formatTimestamp(status.lastUpdatedAt) : "—"}
                        </div>
                        <div style={tileSubStyle}>
                            Server clock; frontend renders relative age.
                        </div>
                    </article>
                </div>
            </section>
    );
}

export default ChainStatusPanel;
