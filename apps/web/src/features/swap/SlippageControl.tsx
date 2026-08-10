/**
 * SlippageControl — 滑点容忍度选择器。
 *
 * 3 个预设（0.5% / 1% / 2%）+ 自定义输入，默认 0.5%。
 *   > 5%  黄色警告（可继续）
 *   > 10% 红色拒绝（SwapCard 禁用提交）
 *
 * 受控组件：bps 由父组件持有，本组件只负责编辑与提示。
 * 自定义输入保留用户原始文本（本地 state），避免「输 0.」立刻被格式化打断。
 */

import {useEffect, useState} from "react";

import {SLIPPAGE_MAX_BPS, SLIPPAGE_PRESETS_BPS, SLIPPAGE_WARN_BPS} from "./swapConfig";
import {bpsToPercentText, percentTextToBps, slippageTone} from "./swapUtils";

interface SlippageControlProps {
  /** 当前滑点（基点）。 */
  valueBps: number;
  /** 变更回调；越界值也会上报，由父组件统一做禁用判断。 */
  onChange: (bps: number) => void;
  /** 交易进行中时禁止改滑点。 */
  disabled?: boolean;
}

const WARN_PCT = bpsToPercentText(SLIPPAGE_WARN_BPS);
const MAX_PCT = bpsToPercentText(SLIPPAGE_MAX_BPS);

export function SlippageControl({valueBps, onChange, disabled = false}: SlippageControlProps) {
  const isPreset = SLIPPAGE_PRESETS_BPS.some((p) => p === valueBps);
  const [customText, setCustomText] = useState(() => (isPreset ? "" : bpsToPercentText(valueBps)));

  // 父组件（或预设按钮）改回预设值时清空自定义输入框，避免两处显示打架。
  useEffect(() => {
    if (SLIPPAGE_PRESETS_BPS.some((p) => p === valueBps)) setCustomText("");
  }, [valueBps]);

  const tone = slippageTone(valueBps);
  const invalid = customText !== "" && percentTextToBps(customText) === null;

  const onCustomChange = (raw: string) => {
    setCustomText(raw);
    const bps = percentTextToBps(raw);
    if (bps !== null) onChange(bps);
  };

  return (
    <fieldset className="slippage-control" disabled={disabled}>
      <legend className="slippage-control__legend">Slippage tolerance</legend>

      <div className="slippage-control__row">
        {SLIPPAGE_PRESETS_BPS.map((bps) => (
          <button
            key={bps}
            type="button"
            className={`btn btn--ghost slippage-control__preset${
              isPreset && valueBps === bps ? " is-active" : ""
            }`}
            aria-pressed={isPreset && valueBps === bps}
            onClick={() => {
              setCustomText("");
              onChange(bps);
            }}
          >
            {bpsToPercentText(bps)}%
          </button>
        ))}

        <label className="slippage-control__custom">
          <span className="sr-only">Custom slippage in percent</span>
          <input
            type="text"
            inputMode="decimal"
            placeholder="Custom"
            value={customText}
            onChange={(e) => onCustomChange(e.target.value)}
            aria-invalid={invalid || tone === "blocked"}
            aria-describedby="slippage-control-hint"
          />
          <span aria-hidden="true">%</span>
        </label>
      </div>

      <p
        id="slippage-control-hint"
        className={`slippage-control__hint slippage-control__hint--${tone}`}
        role={tone === "ok" ? undefined : "alert"}
      >
        {invalid
          ? "Enter a valid percentage, e.g. 0.5"
          : tone === "blocked"
            ? `Slippage above ${MAX_PCT}% is rejected. Lower it to continue.`
            : tone === "warn"
              ? `Above ${WARN_PCT}% you may be front-run. Proceed only if you know why.`
              : `Your swap reverts if the price moves more than ${bpsToPercentText(valueBps)}%.`}
      </p>
    </fieldset>
  );
}
