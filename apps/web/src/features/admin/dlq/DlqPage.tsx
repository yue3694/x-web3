import {useCallback, useEffect, useState} from "react";

import {ApiClientError} from "@/api/client";
import {adminApi} from "@/features/admin/adminApi";
import type {DlqEntry, DlqRetryRequest} from "@/features/admin/adminTypes";

function formatDate(value: string) {
    const date = new Date(value);
    return Number.isNaN(date.valueOf()) ? value : new Intl.DateTimeFormat("zh-CN", {dateStyle: "medium", timeStyle: "short"}).format(date);
}

const SEVERITY_LABEL: Record<string, string> = {
    info: "提示",
    warn: "警告",
    error: "错误",
    critical: "严重",
};

export function DlqPage() {
    const [items, setItems] = useState<DlqEntry[]>([]);
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState<number | null>(null);
    const [error, setError] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try { setItems((await adminApi.listDlq()).items); }
        catch (cause) { setError(cause instanceof ApiClientError ? `${cause.code}: ${cause.message}` : "无法加载死信队列。"); }
        finally { setLoading(false); }
    }, []);
    useEffect(() => {void load();}, [load]);
    const resolve = async (item: DlqEntry, resolution: DlqRetryRequest["resolution"]) => {
        setBusy(item.id);
        setError("");
        try { await adminApi.retryDlq(item.id, {resolution}); setItems((current) => current.filter((entry) => entry.id !== item.id)); }
        catch (cause) { setError(cause instanceof ApiClientError ? `${cause.code}: ${cause.message}` : "无法处理该事件。"); }
        finally { setBusy(null); }
    };
    return (
        <section className="panel admin-panel" aria-labelledby="dlq-title">
            <div className="section-heading"><div><span className="eyebrow">管理 · 恢复</span><h2 id="dlq-title">死信队列</h2><p>查看重试耗尽的事件，并记录明确的处理方式。</p></div><button type="button" className="btn--ghost" disabled={loading} onClick={() => void load()}>{loading ? "刷新中…" : "刷新"}</button></div>
            {error ? <div className="notice notice--error" role="alert">{error}</div> : null}
            {loading ? <div className="route-loader" role="status">正在加载未处理事件…</div> : items.length === 0 ? <div className="empty-state"><span>◇</span><h3>队列已清空</h3><p>暂无需要人工处理的未解决事件。</p></div> : <ol className="dlq-list">{items.map((item) => <li key={item.id} className="dlq-card"><header><div><span className={`severity severity--${item.severity}`}>{SEVERITY_LABEL[item.severity] ?? item.severity}</span><strong>{item.kind}</strong></div><time dateTime={item.createdAt}>{formatDate(item.createdAt)}</time></header><p>{item.summary}</p><dl><div><dt>消费者</dt><dd>{item.consumer}</dd></div><div><dt>链</dt><dd>{item.chainId ?? "—"}</dd></div><div><dt>已重试</dt><dd>{item.retryCount}</dd></div></dl><details><summary>查看负载</summary><pre>{JSON.stringify(item.payload, null, 2)}</pre></details><footer><button type="button" className="btn--primary" disabled={busy === item.id} onClick={() => void resolve(item, "replayed")}>重放</button><button type="button" className="btn--ghost" disabled={busy === item.id} onClick={() => void resolve(item, "manual")}>已手动处理</button><button type="button" className="btn--danger-ghost" disabled={busy === item.id} onClick={() => void resolve(item, "ignored")}>忽略</button></footer></li>)}</ol>}
        </section>
    );
}

export default DlqPage;
