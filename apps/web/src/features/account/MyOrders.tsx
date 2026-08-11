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
 *
 * UI 层次：
 *   1. section-heading（标题 + 描述 + 统计摘要）
 *   2. filter chips（All / Pending / Confirmed / Failed / Dead）
 *   3. list (cards) 或 empty state CTA
 *   4. pager（prev/next + 当前页指示）
 */

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { apiClient, ApiClientError } from "@/api/client";
import { useSession } from "@/auth/SessionContext";

import { OrderRow } from "./OrderRow";
import type { OrderListResponse, OrderRecord, OrderStatus } from "./MyOrders.types";

const PAGE_SIZE = 50; // 对齐 server default limit (max 100); 单次拉到本地做 status 过滤 + 分页

const STATUS_OPTIONS: { value: OrderStatus; label: string }[] = [
    { value: "all", label: "全部" },
    { value: "pending", label: "待处理" },
    { value: "confirmed", label: "已确认" },
    { value: "failed", label: "失败" },
    { value: "dead", label: "已失效" },
];

interface MyOrdersProps {
    className?: string;
}

export function MyOrders({ className }: MyOrdersProps) {
    const { profile, loading: sessionLoading } = useSession();
    const [status, setStatus] = useState<OrderStatus>("all");
    const [page, setPage] = useState(1);

    const queryKey = useMemo(() => ["orders", page] as const, [page]);

    const query = useQuery<OrderListResponse>({
        queryKey,
        enabled: !!profile && !sessionLoading,
        queryFn: async ({ signal }) => {
            const params = new URLSearchParams();
            params.set("limit", String(PAGE_SIZE));
            return apiClient.get<OrderListResponse>(`/me/orders?${params.toString()}`, { signal });
        },
        staleTime: 30_000,
    });

    if (!profile) {
        return (
            <section className={`my-orders panel${className ? ` ${className}` : ""}`}>
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">账户</span>
                        <h2>我的订单</h2>
                        <p>请先登录，查看你的购买记录。</p>
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
            ? "无法加载你的订单。"
            : null;

    const items = query.data?.items ?? [];
    // status 客户端过滤：当前 server 不支持 status 查询参数；
    // limit 命中条数 >= PAGE_SIZE 时视为"可能有下一页"。
    const counts = useMemo(() => {
        const c: Record<OrderStatus, number> = {
            all: items.length,
            pending: 0,
            confirmed: 0,
            failed: 0,
            dead: 0,
        };
        for (const o of items) c[o.status]++;
        return c;
    }, [items]);

    const filtered: OrderRecord[] = status === "all"
        ? items
        : items.filter((o) => o.status === status);
    const hasMore = items.length >= PAGE_SIZE;
    const pageItems = filtered.slice(0, PAGE_SIZE);

    return (
        <section
            className={`my-orders panel${className ? ` ${className}` : ""}`}
            aria-labelledby="my-orders-title"
        >
            <header className="section-heading">
                <div>
                    <span className="eyebrow">账户</span>
                    <h2 id="my-orders-title">我的订单</h2>
                    <p>你用 YD 完成的每一笔购买，都附有链上回执。</p>
                </div>
                {items.length > 0 ? (
                    <dl className="my-orders__stats" aria-label="订单状态汇总">
                        <div className="my-orders__stat my-orders__stat--all">
                            <dt>合计</dt>
                            <dd>{counts.all}</dd>
                        </div>
                        <div className="my-orders__stat my-orders__stat--confirmed">
                            <dt>已确认</dt>
                            <dd>{counts.confirmed}</dd>
                        </div>
                        <div className="my-orders__stat my-orders__stat--pending">
                            <dt>待处理</dt>
                            <dd>{counts.pending}</dd>
                        </div>
                        {counts.failed + counts.dead > 0 ? (
                            <div className="my-orders__stat my-orders__stat--failed">
                                <dt>失败 / 已失效</dt>
                                <dd>{counts.failed + counts.dead}</dd>
                            </div>
                        ) : null}
                    </dl>
                ) : null}
            </header>

            {routeMissing ? (
                <div className="notice notice--error" role="alert">
                    当前 API 尚未挂载 <code>GET /me/orders</code> 路由，
                    请联系后端同学开放该接口。
                </div>
            ) : null}
            {errorMessage && !routeMissing ? (
                <div className="notice notice--error" role="alert">
                    <span>{errorMessage}</span>{" "}
                    <button type="button" className="btn--ghost" onClick={() => void query.refetch()}>
                        重试
                    </button>
                </div>
            ) : null}

            {items.length > 0 ? (
                <div
                    className="filter-chips"
                    role="tablist"
                    aria-label="按状态筛选订单"
                >
                    {STATUS_OPTIONS.map((opt) => {
                        const isActive = status === opt.value;
                        const count = counts[opt.value];
                        return (
                            <button
                                key={opt.value}
                                type="button"
                                role="tab"
                                aria-selected={isActive}
                                aria-controls="my-orders-list"
                                className={`filter-chips__chip${isActive ? " is-active" : ""}`}
                                onClick={() => {
                                    setStatus(opt.value);
                                    setPage(1);
                                }}
                            >
                                <span>{opt.label}</span>
                                <span className="filter-chips__count" aria-hidden="true">
                                    {count}
                                </span>
                                <span className="sr-only">{` (${count})`}</span>
                            </button>
                        );
                    })}
                </div>
            ) : null}

            {query.isLoading ? (
                <ol
                    id="my-orders-list"
                    className="my-orders__list"
                    aria-busy="true"
                    aria-label="订单加载中"
                >
                    {[0, 1, 2].map((i) => (
                        <li key={i} className="my-orders__skeleton">
                            <div className="my-orders__skeleton-row" />
                            <div className="my-orders__skeleton-row my-orders__skeleton-row--short" />
                            <div className="my-orders__skeleton-grid" />
                        </li>
                    ))}
                </ol>
            ) : pageItems.length === 0 && !routeMissing && !errorMessage ? (
                <div className="empty-state" role="status">
                    {status === "all" ? (
                        <>
                            <svg
                                aria-hidden="true"
                                className="empty-state__icon"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="1.6"
                                strokeLinecap="round"
                                strokeLinejoin="round"
                            >
                                <path d="M3 7h18l-1.5 11a2 2 0 0 1-2 1.7H6.5a2 2 0 0 1-2-1.7L3 7Z" />
                                <path d="M8 7V5a4 4 0 0 1 8 0v2" />
                            </svg>
                            <h3>暂无订单</h3>
                            <p>去课程市场挑选第一门课，开启学习之旅。</p>
                            <a href="/courses" className="btn--primary">浏览课程</a>
                        </>
                    ) : (
                        <>
                            <svg
                                aria-hidden="true"
                                className="empty-state__icon"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="1.6"
                                strokeLinecap="round"
                                strokeLinejoin="round"
                            >
                                <circle cx="11" cy="11" r="7" />
                                <path d="m20 20-3.5-3.5" />
                            </svg>
                            <h3>暂无「{status}」订单</h3>
                            <p>当前筛选下没有匹配的订单。</p>
                            <button
                                type="button"
                                className="btn--ghost"
                                onClick={() => setStatus("all")}
                            >
                                查看全部订单
                            </button>
                        </>
                    )}
                </div>
            ) : (
                <>
                    <ol
                        id="my-orders-list"
                        className="my-orders__list"
                        aria-label="我的订单"
                    >
                        {pageItems.map((order) => (
                            <OrderRow key={order.id} order={order} />
                        ))}
                    </ol>

                    <nav className="my-orders__pager" aria-label="分页">
                        <button
                            type="button"
                            className="btn--ghost"
                            disabled={page === 1 || query.isFetching}
                            onClick={() => setPage((p) => Math.max(1, p - 1))}
                        >
                            <svg
                                aria-hidden="true"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="2"
                                strokeLinecap="round"
                                strokeLinejoin="round"
                            >
                                <path d="M19 12H5" />
                                <path d="m11 18-6-6 6-6" />
                            </svg>
                            上一页
                        </button>
                        <span aria-live="polite" className="my-orders__pager-page">
                            第 {page} 页
                        </span>
                        <button
                            type="button"
                            className="btn--ghost"
                            disabled={!hasMore || query.isFetching}
                            onClick={() => setPage((p) => p + 1)}
                        >
                            下一页
                            <svg
                                aria-hidden="true"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="2"
                                strokeLinecap="round"
                                strokeLinejoin="round"
                            >
                                <path d="M5 12h14" />
                                <path d="m13 6 6 6-6 6" />
                            </svg>
                        </button>
                    </nav>
                </>
            )}
        </section>
    );
}