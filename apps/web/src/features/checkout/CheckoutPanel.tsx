/**
 * CheckoutPanel — 课程购买聚合面板。
 *
 * 组合：
 *   - 价格明细（base + 平台费 + Gas 估算占位）
 *   - 服务条款勾选
 *   - CheckoutButton（实际写链 + 上报后端）
 *
 * 条款未勾选时禁用购买按钮；条款勾选状态由本组件持有，
 * 后续可拆出独立的 CheckoutTermsCheckbox 复用。
 */

import {useMemo, useState} from "react";
import {formatUnits} from "viem";

import {CheckoutButton} from "./CheckoutButton";
import {OracleReferencePrice} from "./OracleReferencePrice";
import type {CheckoutContextProps} from "./checkoutTypes";

interface PriceBreakdown {
    base: string;
    total: string;
}

interface CheckoutPanelProps extends CheckoutContextProps {
    /** 初始条款勾选状态（默认 false）。 */
    defaultTermsAccepted?: boolean;
}

function computeBreakdown(priceYD: string): PriceBreakdown {
    const base = BigInt(priceYD);
    const display = formatUnits(base, 18);
    return {
        base: display,
        total: display,
    };
}

export function CheckoutPanel({
    courseId,
    courseTitle,
    priceYD,
    courseKey,
    recipient,
    walletId,
    generateIdempotencyKey,
    onSuccess,
    defaultTermsAccepted = false,
}: CheckoutPanelProps) {
    const [accepted, setAccepted] = useState(defaultTermsAccepted);
    const [errorBanner, setErrorBanner] = useState<string | null>(null);

    const breakdown = useMemo(
        () => computeBreakdown(priceYD),
        [priceYD],
    );

    return (
        <section className="checkout-panel panel" aria-labelledby="checkout-panel-title">
            <header className="section-heading">
                <div>
                    <span className="eyebrow">结算</span>
                    <h2 id="checkout-panel-title">购买 {courseTitle}</h2>
                    <p>在已配置的测试链上以 YD 完成支付；Worker 确认后即可解锁学习。</p>
                </div>
            </header>

            <dl className="checkout-panel__breakdown">
                <div>
                    <dt>课程价格</dt>
                    <dd>{breakdown.base} YD</dd>
                </div>
                <div className="checkout-panel__total">
                    <dt>合计</dt>
                    <dd>{breakdown.total} YD</dd>
                </div>
            </dl>

            <OracleReferencePrice />

            <label className="checkout-panel__terms">
                <input
                    type="checkbox"
                    checked={accepted}
                    onChange={(e) => setAccepted(e.target.checked)}
                />
                <span>
                    我已知晓链上购买不可撤销且不予退款，课程解锁与当前连接的钱包绑定。
                </span>
            </label>

            {errorBanner ? (
                <div className="notice notice--error" role="alert">
                    {errorBanner}
                </div>
            ) : null}

            <div className="checkout-panel__cta" aria-disabled={!accepted}>
                <CheckoutButton
                    courseId={courseId}
                    courseTitle={courseTitle}
                    priceYD={priceYD}
                    courseKey={courseKey}
                    recipient={recipient}
                    walletId={walletId}
                    generateIdempotencyKey={generateIdempotencyKey}
                    onSuccess={(hash) => {
                        setErrorBanner(null);
                        onSuccess?.(hash);
                    }}
                    disabled={!accepted}
                />
                {!accepted ? (
                    <p className="muted" role="note">
                        请先勾选条款后再启用购买按钮。
                    </p>
                ) : null}
            </div>
        </section>
    );
}
