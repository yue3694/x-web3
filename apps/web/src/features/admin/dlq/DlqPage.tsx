import {useCallback, useEffect, useState} from "react";

import {ApiClientError} from "@/api/client";
import {adminApi} from "@/features/admin/adminApi";
import type {DlqEntry, DlqRetryRequest} from "@/features/admin/adminTypes";

function formatDate(value: string) {
    const date = new Date(value);
    return Number.isNaN(date.valueOf()) ? value : new Intl.DateTimeFormat("en-US", {dateStyle: "medium", timeStyle: "short"}).format(date);
}

export function DlqPage() {
    const [items, setItems] = useState<DlqEntry[]>([]);
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState<number | null>(null);
    const [error, setError] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try { setItems((await adminApi.listDlq()).items); }
        catch (cause) { setError(cause instanceof ApiClientError ? `${cause.code}: ${cause.message}` : "Unable to load the dead-letter queue."); }
        finally { setLoading(false); }
    }, []);
    useEffect(() => {void load();}, [load]);
    const resolve = async (item: DlqEntry, resolution: DlqRetryRequest["resolution"]) => {
        setBusy(item.id);
        setError("");
        try { await adminApi.retryDlq(item.id, {resolution}); setItems((current) => current.filter((entry) => entry.id !== item.id)); }
        catch (cause) { setError(cause instanceof ApiClientError ? `${cause.code}: ${cause.message}` : "Unable to resolve this event."); }
        finally { setBusy(null); }
    };
    return (
        <section className="panel admin-panel" aria-labelledby="dlq-title">
            <div className="section-heading"><div><span className="eyebrow">Admin · Recovery</span><h2 id="dlq-title">Dead-letter queue</h2><p>Inspect events that exhausted retries, then record an explicit resolution.</p></div><button type="button" className="btn--ghost" disabled={loading} onClick={() => void load()}>{loading ? "Refreshing…" : "Refresh"}</button></div>
            {error ? <div className="notice notice--error" role="alert">{error}</div> : null}
            {loading ? <div className="route-loader" role="status">Loading unresolved events…</div> : items.length === 0 ? <div className="empty-state"><span>◇</span><h3>Queue is clear</h3><p>No unresolved events require operator attention.</p></div> : <ol className="dlq-list">{items.map((item) => <li key={item.id} className="dlq-card"><header><div><span className={`severity severity--${item.severity}`}>{item.severity}</span><strong>{item.kind}</strong></div><time dateTime={item.createdAt}>{formatDate(item.createdAt)}</time></header><p>{item.summary}</p><dl><div><dt>Consumer</dt><dd>{item.consumer}</dd></div><div><dt>Chain</dt><dd>{item.chainId ?? "—"}</dd></div><div><dt>Retries</dt><dd>{item.retryCount}</dd></div></dl><details><summary>Inspect payload</summary><pre>{JSON.stringify(item.payload, null, 2)}</pre></details><footer><button type="button" className="btn--primary" disabled={busy === item.id} onClick={() => void resolve(item, "replayed")}>Replay</button><button type="button" className="btn--ghost" disabled={busy === item.id} onClick={() => void resolve(item, "manual")}>Resolved manually</button><button type="button" className="btn--danger-ghost" disabled={busy === item.id} onClick={() => void resolve(item, "ignored")}>Ignore</button></footer></li>)}</ol>}
        </section>
    );
}

export default DlqPage;
