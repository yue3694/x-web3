/**
 * MyOrders — 账户中心「我的订单」面板。
 *
 * 行为契约（design.md F03 §5）：
 *   - GET /orders?status=&page=  拉取当前用户订单列表；
 *   - 每页 20 条，prev/next 翻页（不暴露总页数）；
 *   - 状态机：loading skeleton → empty CTA / list / error retry；
 *   - status 过滤：all / pending / confirmed / failed / dead；
 *   - 单行 wrong-network 警告在 OrderRow 中处理。
 *
 * 依赖：@tanstack/react-query 的 useQuery；
 *       后端 API 暂未挂载时（404/405）走「API 暂未上线」降级提示。
 */

import {useMemo, useState} from "react";
import {useQuery} from "@tanstack/react-query";

import {apiClient, ApiClientError} from "@/api/client";
import {useSession} from "@/auth/SessionContext";

import {OrderRow} from "./OrderRow";
import type {OrderListResponse, OrderRecord, OrderStatus} from "./MyOrders.types";

const PAGE_SIZE = 50; // 对齐 server default limit (max 100); 单次拉到本地做 status 过滤 + 分页

const STATUS_OPTIONS: {value: OrderStatus; label: string}[] = [
    {value: "all", label: "All"},
    {value: "pending", label: "Pending"},
    {value: "confirmed", label: "Confirmed"},
    {value: "failed", label: "Failed"},
    {value: "dead", label: "Dead"},
];

interface MyOrdersProps {
    className?: string;
}

export function MyOrders({className}: MyOrdersProps) {
    const {profile, loading: sessionLoading} = useSession();
    const [status, setStatus] = useState<OrderStatus>("all");
    const [page, setPage] = useState(1);

    const queryKey = useMemo(() => ["orders", page] as const, [page]);

    const query = useQuery<OrderListResponse>({
        queryKey,
        enabled: !!profile && !sessionLoading,
        queryFn: async ({signal}) => {
            const params = new URLSearchParams();
            params.set("limit", String(PAGE_SIZE));
            return apiClient.get<OrderListResponse>(`/me/orders?${params.toString()}`, {signal});
        },
        staleTime: 30_000,
    });

    if (!profile) {
        return (
            <section className={`my-orders panel${className ? ` ${className}` : ""}`}>
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">Account</span>
                        <h2>My orders</h2>
                        <p>Sign in to view your purchase history.</p>
                    </div>
                </div>
            </section>
        );
    }

    const routeMissing = query.error instanceof ApiClientError
        && (query.error.status === 404 || query.error.status === 405);
    const errorMessage = query.error instanceof ApiClientError
        ? query.error.message
        : query.error
          ? "Unable to load your orders."
          : null;

    const items = query.data?.items ?? [];
    // status 客户端过滤：当前 server 不支持 status 查询参数；
    // limit 命中条数 >= PAGE_SIZE 时视为"可能有下一页"。
    const filtered: OrderRecord[] = status === "all"
        ? items
        : items.filter((o) => o.status === status);
    const hasMore = items.length >= PAGE_SIZE;
    const pageItems = filtered.slice(0, PAGE_SIZE);

    return (
        <section className={`my-orders panel${className ? ` ${className}` : ""}`} aria-labelledby="my-orders-title">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">Account</span>
                    <h2 id="my-orders-title">My orders</h2>
                    <p>Every YD purchase you have made, with on-chain receipts.</p>
                </div>
                <label className="my-orders__filter">
                    <span className="sr-only">Filter by status</span>
                    <select
                        value={status}
                        onChange={(e) => {
                            setStatus(e.target.value as OrderStatus);
                            setPage(1);
                        }}
                    >
                        {STATUS_OPTIONS.map((opt) => (
                            <option key={opt.value} value={opt.value}>{opt.label}</option>
                        ))}
                    </select>
                </label>
            </div>

            {routeMissing ? (
                <div className="notice notice--error" role="alert">
                    The <code>GET /me/orders</code> endpoint is not wired in the current API build.
                    Please ask the backend track to expose the route.
                </div>
            ) : null}
            {errorMessage && !routeMissing ? (
                <div className="notice notice--error" role="alert">
                    {errorMessage}{" "}
                    <button type="button" className="btn--ghost" onClick={() => void query.refetch()}>
                        Retry
                    </button>
                </div>
            ) : null}

            {query.isLoading ? (
                <ol className="my-orders__list" aria-busy="true" aria-label="Loading orders">
                    {[0, 1, 2].map((i) => (
                        <li key={i} className="my-orders__skeleton" />
                    ))}
                </ol>
            ) : pageItems.length === 0 && !routeMissing && !errorMessage ? (
                <div className="empty-state">
                    <span>◇</span>
                    <h3>No orders yet</h3>
                    <p>Browse the catalog to enroll in your first course.</p>
                </div>
            ) : (
                <>
                    <ol className="my-orders__list" aria-label="My orders">
                        {pageItems.map((order) => (
                            <OrderRow key={order.id} order={order} />
                        ))}
                    </ol>

                    <nav className="my-orders__pager" aria-label="Pagination">
                        <button
                            type="button"
                            className="btn--ghost"
                            disabled={page === 1 || query.isFetching}
                            onClick={() => setPage((p) => Math.max(1, p - 1))}
                        >
                            ← Prev
                        </button>
                        <span aria-live="polite">Page {page}</span>
                        <button
                            type="button"
                            className="btn--ghost"
                            disabled={!hasMore || query.isFetching}
                            onClick={() => setPage((p) => p + 1)}
                        >
                            Next →
                        </button>
                    </nav>
                </>
            )}
        </section>
    );
}
