/**
 * PriceImpactBadge — 价格影响色标（F05-T06）。
 *
 * 色带：
 *   green   < 1%
 *   yellow  1% – 5%
 *   red     5% – 10%
 *   blocked ≥ 10%（SwapCard 据此禁用提交）
 *
 * 纯展示组件：不做阈值判断以外的任何逻辑，分级函数在 swapUtils 里以便单测。
 */

import {PRICE_IMPACT_BLOCK_PCT} from "./swapConfig";
import {priceImpactTone} from "./swapUtils";
import type {RiskTone} from "./swapTypes";

interface PriceImpactBadgeProps {
  /** 价格影响百分比；null = 尚无报价，显示「—」。 */
  pct: number | null;
  /** 报价请求在飞时显示占位。 */
  loading?: boolean;
}

const TONE_LABEL: Record<RiskTone, string> = {
  ok: "价格影响较低",
  warn: "价格影响中等",
  danger: "价格影响较高",
  blocked: `价格影响 ≥ ${PRICE_IMPACT_BLOCK_PCT}%，兑换已拦截`,
};

export function PriceImpactBadge({pct, loading = false}: PriceImpactBadgeProps) {
  if (loading) {
    return (
      <span className="price-impact-badge price-impact-badge--loading" aria-busy="true">
        价格影响 <strong>…</strong>
      </span>
    );
  }

  if (pct === null) {
    return (
      <span className="price-impact-badge price-impact-badge--ok" title="暂无报价">
        价格影响 <strong>—</strong>
      </span>
    );
  }

  const tone = priceImpactTone(pct);
  const text = `${pct.toFixed(2)}%`;

  return (
    <span
      className={`price-impact-badge price-impact-badge--${tone}`}
      data-tone={tone}
      title={TONE_LABEL[tone]}
      // 高风险才打断读屏用户，低风险保持安静。
      role={tone === "danger" || tone === "blocked" ? "alert" : undefined}
    >
      价格影响 <strong>{text}</strong>
      {tone === "blocked" ? <span className="price-impact-badge__flag"> · 已拦截</span> : null}
    </span>
  );
}
