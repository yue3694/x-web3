/**
 * OrderRow — MyOrders 列表中的单行渲染。
 *
 * 拆分原因：MyOrders.tsx 是容器 + 翻页 + 过滤，单行渲染包含
 * wrong-network 警告，单独成文件以保持各组件 ≤ 200 行。
 */

import {useChainId, useSwitchChain} from "wagmi";

import {etherscanUrl, formatDate, type OrderRecord} from "./MyOrders.types";

interface OrderRowProps {
    order: OrderRecord;
}

export function OrderRow({order}: OrderRowProps) {
    const chainId = useChainId();
    const {switchChain, isPending: isSwitching} = useSwitchChain();

    const wrongNetwork = chainId !== order.chainId;
    const txUrl = order.onchainTxHash ? etherscanUrl(order.chainId, order.onchainTxHash) : null;

    return (
        <li className={`my-orders__item my-orders__item--${order.status}`}>
            <header>
                <div>
                    <span className={`status-pill status-pill--${order.status}`}>
                        {order.status}
                    </span>
                    <strong className="my-orders__title">{order.courseTitle}</strong>
                </div>
                <time className="my-orders__time" dateTime={order.createdAt}>
                    {formatDate(order.createdAt)}
                </time>
            </header>
            <dl className="my-orders__meta">
                <div>
                    <dt>Price</dt>
                    <dd>{order.priceYD} YD</dd>
                </div>
                <div>
                    <dt>Chain</dt>
                    <dd>{order.chainId}</dd>
                </div>
                <div>
                    <dt>Receipt</dt>
                    <dd>
                        {txUrl ? (
                            <a href={txUrl} target="_blank" rel="noreferrer noopener">
                                {order.onchainTxHash?.slice(0, 10)}…
                            </a>
                        ) : (
                            <span className="muted">Pending</span>
                        )}
                    </dd>
                </div>
                <div>
                    <dt>Enrollment</dt>
                    <dd>
                        {order.enrollmentId ? (
                            <a href={`/learn/${order.courseId}`}>Open course</a>
                        ) : (
                            <span className="muted">Awaiting confirmation</span>
                        )}
                    </dd>
                </div>
            </dl>
            {wrongNetwork ? (
                <div className="notice notice--warn" role="status">
                    This order is on chain {order.chainId}, but your wallet is on {chainId}.{" "}
                    <button
                        type="button"
                        className="btn--ghost"
                        disabled={isSwitching}
                        onClick={() => switchChain({chainId: order.chainId})}
                    >
                        {isSwitching ? "Switching…" : "Switch network"}
                    </button>
                </div>
            ) : null}
        </li>
    );
}
