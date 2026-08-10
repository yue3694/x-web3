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

import {CheckoutButton} from "./CheckoutButton";
import type {CheckoutContextProps} from "./checkoutTypes";

interface PriceBreakdown {
    base: string;
    platformFee: string;
    total: string;
}

interface CheckoutPanelProps extends CheckoutContextProps {
    /** 自定义平台费率（基点），默认 250 = 2.5%。 */
    platformFeeBps?: number;
    /** 初始条款勾选状态（默认 false）。 */
    defaultTermsAccepted?: boolean;
}

function computeBreakdown(priceYD: string, feeBps: number): PriceBreakdown {
    // priceYD 是 wei 字符串（base unit），前端用 bigint 计算。
    const base = BigInt(priceYD);
    const fee = (base * BigInt(feeBps)) / BigInt(10000);
    return {
        base: base.toString(),
        platformFee: fee.toString(),
        total: (base + fee).toString(),
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
    platformFeeBps = 250,
    defaultTermsAccepted = false,
}: CheckoutPanelProps) {
    const [accepted, setAccepted] = useState(defaultTermsAccepted);
    const [errorBanner, setErrorBanner] = useState<string | null>(null);

    const breakdown = useMemo(
        () => computeBreakdown(priceYD, platformFeeBps),
        [priceYD, platformFeeBps],
    );

    return (
        <section className="checkout-panel panel" aria-labelledby="checkout-panel-title">
            <header className="section-heading">
                <div>
                    <span className="eyebrow">Checkout</span>
                    <h2 id="checkout-panel-title">Buy {courseTitle}</h2>
                    <p>Settle with YD on Sepolia. Your enrollment unlocks immediately after on-chain confirmation.</p>
                </div>
            </header>

            <dl className="checkout-panel__breakdown">
                <div>
                    <dt>Course price</dt>
                    <dd>{breakdown.base} YD</dd>
                </div>
                <div>
                    <dt>Platform fee</dt>
                    <dd>{breakdown.platformFee} YD</dd>
                </div>
                <div className="checkout-panel__total">
                    <dt>Total</dt>
                    <dd>{breakdown.total} YD</dd>
                </div>
            </dl>

            <label className="checkout-panel__terms">
                <input
                    type="checkbox"
                    checked={accepted}
                    onChange={(e) => setAccepted(e.target.checked)}
                />
                <span>
                    I understand that on-chain purchases are final and non-refundable. The course unlock
                    is tied to my connected wallet.
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
                />
                {!accepted ? (
                    <p className="muted" role="note">
                        Accept the terms to enable purchase.
                    </p>
                ) : null}
            </div>
        </section>
    );
}
