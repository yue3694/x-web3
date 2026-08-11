/**
 * OrderRow — MyOrders 列表中的单行渲染。
 *
 * 拆分原因：MyOrders.tsx 是容器 + 翻页 + 过滤，单行渲染包含
 * wrong-network 警告，单独成文件以保持各组件 ≤ 200 行。
 */

import { Link } from "react-router-dom";
import { useChainId, useSwitchChain } from "wagmi";

import { etherscanUrl, formatDate, type OrderRecord } from "./MyOrders.types";

interface OrderRowProps {
    order: OrderRecord;
}

const STATUS_LABEL: Record<Exclude<OrderRecord["status"], "pending"> | "pending", string> = {
    pending: "Pending",
    confirmed: "Confirmed",
    failed: "Failed",
    dead: "Dead",
};

function StatusBadge({ status }: { status: OrderRecord["status"] }) {
    return (
        <span className={`status-pill status-pill--${status}`}>
            <span aria-hidden="true" className="status-pill__dot" />
            {STATUS_LABEL[status]}
        </span>
    );
}

function ExternalLink({ href, children }: { href: string; children: React.ReactNode }) {
    return (
        <a href={href} target="_blank" rel="noreferrer noopener" className="my-orders__link">
            {children}
            <svg
                aria-hidden="true"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.8"
                strokeLinecap="round"
                strokeLinejoin="round"
            >
                <path d="M15 3h6v6" />
                <path d="M10 14 21 3" />
                <path d="M21 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5" />
            </svg>
        </a>
    );
}

export function OrderRow({ order }: OrderRowProps) {
    const chainId = useChainId();
    const { switchChain, isPending: isSwitching } = useSwitchChain();

    const wrongNetwork = chainId !== order.chainId;
    const txUrl = order.onchainTxHash ? etherscanUrl(order.chainId, order.onchainTxHash) : null;

    return (
        <li className={`my-orders__item my-orders__item--${order.status}`}>
            <header className="my-orders__head">
                <div className="my-orders__headline">
                    <StatusBadge status={order.status} />
                    <h3 className="my-orders__title">
                        {order.enrollmentId ? (
                            <Link to={`/learn/${order.courseId}`}>{order.courseTitle ?? "Untitled course"}</Link>
                        ) : (
                            <span>{order.courseTitle ?? "Untitled course"}</span>
                        )}
                    </h3>
                </div>
                <time className="my-orders__time" dateTime={order.createdAt} title={formatDate(order.createdAt)}>
                    {formatDate(order.createdAt)}
                </time>
            </header>
            <dl className="my-orders__meta">
                <div>
                    <dt>Price</dt>
                    <dd>
                        <span className="my-orders__price">
                            {order.priceYD ? order.priceYD : "—"}
                            <span className="my-orders__price-unit">YD</span>
                        </span>
                    </dd>
                </div>
                <div>
                    <dt>Chain</dt>
                    <dd>
                        <span className="my-orders__chain">{order.chainId}</span>
                    </dd>
                </div>
                <div>
                    <dt>Receipt</dt>
                    <dd>
                        {txUrl ? (
                            <ExternalLink href={txUrl}>
                                {order.onchainTxHash?.slice(0, 10)}…
                            </ExternalLink>
                        ) : (
                            <span className="muted">Awaiting confirmation</span>
                        )}
                    </dd>
                </div>
                <div>
                    <dt>Enrollment</dt>
                    <dd>
                        {order.enrollmentId ? (
                            <Link to={`/learn/${order.courseId}`} className="my-orders__link">
                                Open course
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
                            </Link>
                        ) : (
                            <span className="muted">Awaiting confirmation</span>
                        )}
                    </dd>
                </div>
            </dl>
            {wrongNetwork ? (
                <div className="notice notice--warn" role="status">
                    <span>
                        This order is on chain <strong>{order.chainId}</strong>, but your wallet is on{" "}
                        <strong>{chainId}</strong>.
                    </span>{" "}
                    <button
                        type="button"
                        className="btn--ghost"
                        disabled={isSwitching}
                        onClick={() => switchChain({ chainId: order.chainId })}
                    >
                        {isSwitching ? "Switching…" : "Switch network"}
                    </button>
                </div>
            ) : null}
        </li>
    );
}