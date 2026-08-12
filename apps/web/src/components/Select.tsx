/**
 * Select — 主题化下拉组件（替代原生 <select>）。
 *
 * 为什么不用原生：原生 select 的弹层由操作系统绘制，无法继承项目深色
 * 主题，在暗色模式下显示为亮色 macOS / Windows 控件，与卡片脱节。
 *
 * 行为契约（按 ui-ux-pro-max §1/§5/§8）：
 *   · WAI-ARIA 1.2 combobox 模式：
 *       trigger role="combobox" aria-expanded + aria-controls + aria-activedescendant
 *       listbox  role="listbox"
 *       option   role="option" aria-selected / aria-disabled
 *   · 全键盘：ArrowUp/Down/Home/End 移动，Enter/Space 选择，Esc 关闭，Tab 关闭并移焦，
 *           输入字符按 prefix 跳到第一匹配项
 *   · 弹出层用 createPortal 渲染到 document.body，避开 transform / overflow 裁剪；
 *     位置通过 getBoundingClientRect 取触发器坐标，剩余空间不够时下翻到上方
 *   · 焦点环保留（全局已有），关闭后还焦点给触发器（a11y §focus-on-route-change）
 *   · 主题遵循 :root CSS 变量；启用 prefers-reduced-motion 时关闭位移动画
 */

import {
    useCallback,
    useEffect,
    useId,
    useLayoutEffect,
    useMemo,
    useRef,
    useState,
} from "react";
import {createPortal} from "react-dom";

export type SelectValue = string | number;

export interface SelectOption<V extends SelectValue> {
    value: V;
    label: string;
    /** 右侧次要文案（如 chain id、status pill 文字）。 */
    hint?: string;
    disabled?: boolean;
}

export interface SelectProps<V extends SelectValue> {
    value: V;
    onChange: (next: V) => void;
    options: readonly SelectOption<V>[];
    disabled?: boolean;
    ariaLabel?: string;
    className?: string;
    /** 触发器宽度策略：fit = 跟当前选项走，min = 至少 88px 与原 select 一致。 */
    width?: "fit" | "min";
    id?: string;
}

const VIEWPORT_GAP = 8;        // 边缘留白
const OPTION_MIN_PX = 44;      // touch target
const FALLBACK_WIDTH = 200;    // SSR / 测量前宽度

function clamp(n: number, min: number, max: number): number {
    return Math.max(min, Math.min(max, n));
}

export function Select<V extends SelectValue>(props: SelectProps<V>) {
    const {value, onChange, options, disabled, ariaLabel, className, width = "min", id} = props;
    const listboxId = useId();
    const triggerId = useId();
    const optionId = (i: number) => `${listboxId}-opt-${i}`;

    const triggerRef = useRef<HTMLButtonElement>(null);
    const listboxRef = useRef<HTMLUListElement>(null);

    const [open, setOpen] = useState(false);
    const [activeIndex, setActiveIndex] = useState<number>(-1);
    const [rect, setRect] = useState<{top: number; left: number; width: number; placeAbove: boolean} | null>(null);

    const selectedIndex = useMemo(() => options.findIndex((o) => o.value === value), [options, value]);

    // 关闭后还焦点给触发器。
    const focusTrigger = useCallback(() => {
        triggerRef.current?.focus({preventScroll: true});
    }, []);

    // 测量并定位。useLayoutEffect 在绘制前同步执行，避免闪烁。
    const reposition = useCallback(() => {
        const el = triggerRef.current;
        if (!el) return;
        const r = el.getBoundingClientRect();
        const vw = window.innerWidth;
        const vh = window.innerHeight;
        const desiredWidth = clamp(r.width, 120, vw - VIEWPORT_GAP * 2);
        // 水平：在触发器右边缘之内，左右溢出则向中夹拢
        const left = clamp(r.left, VIEWPORT_GAP, Math.max(VIEWPORT_GAP, vw - desiredWidth - VIEWPORT_GAP));
        // 垂直：先尝试下方，剩余空间 < 160 时翻到上方
        const spaceBelow = vh - r.bottom - VIEWPORT_GAP;
        const spaceAbove = r.top - VIEWPORT_GAP;
        const placeAbove = spaceBelow < 160 && spaceAbove > spaceBelow;
        setRect({
            top: placeAbove ? Math.max(VIEWPORT_GAP, r.top - Math.min(spaceAbove, 320)) : r.bottom + 4,
            left,
            width: desiredWidth,
            placeAbove,
        });
    }, []);

    useLayoutEffect(() => {
        if (!open) return;
        reposition();
        const onScrollOrResize = () => reposition();
        window.addEventListener("scroll", onScrollOrResize, true);
        window.addEventListener("resize", onScrollOrResize);
        return () => {
            window.removeEventListener("scroll", onScrollOrResize, true);
            window.removeEventListener("resize", onScrollOrResize);
        };
    }, [open, reposition]);

    // 打开时初始化高亮项为已选项；关闭时重置。
    useEffect(() => {
        if (open) setActiveIndex(selectedIndex >= 0 ? selectedIndex : 0);
        else setActiveIndex(-1);
    }, [open, selectedIndex]);

    // 关闭时同时把焦点还给触发器。
    useEffect(() => {
        if (!open) return;
        const wasOpen = open;
        return () => {
            if (wasOpen) focusTrigger();
        };
    }, [focusTrigger, open]);

    const close = useCallback(() => setOpen(false), []);

    const onTriggerKeyDown = (e: React.KeyboardEvent<HTMLButtonElement>) => {
        if (disabled) return;
        if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Enter" || e.key === " " || e.key === "Home" || e.key === "End") {
            e.preventDefault();
            if (!open) setOpen(true);
        }
    };

    const onListKeyDown = (e: React.KeyboardEvent<HTMLUListElement>) => {
        if (e.key === "Escape") {
            e.preventDefault();
            close();
            focusTrigger();
            return;
        }
        if (e.key === "Tab") {
            close();
            return;
        }
        if (!options.length) return;
        // 找到下一个允许项
        const findEnabled = (start: number, dir: 1 | -1) => {
            const n = options.length;
            let i = start;
            for (let k = 0; k < n; k++) {
                if (options[i] && !options[i].disabled) return i;
                i = (i + dir + n) % n;
            }
            return start;
        };
        if (e.key === "ArrowDown") {
            e.preventDefault();
            setActiveIndex((i) => findEnabled((i < 0 ? selectedIndex : i) + 1, 1));
        } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setActiveIndex((i) => findEnabled((i < 0 ? selectedIndex : i) - 1, -1));
        } else if (e.key === "Home") {
            e.preventDefault();
            setActiveIndex(findEnabled(0, 1));
        } else if (e.key === "End") {
            e.preventDefault();
            setActiveIndex(findEnabled(options.length - 1, -1));
        } else if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            const target = options[activeIndex];
            if (target && !target.disabled) {
                onChange(target.value);
                close();
                focusTrigger();
            }
        } else if (e.key.length === 1) {
            // 简单 typeahead：按下字符跳到首个 label 以它开头的可选项
            const ch = e.key.toLowerCase();
            const i = options.findIndex((o) => !o.disabled && o.label.toLowerCase().startsWith(ch));
            if (i >= 0) setActiveIndex(i);
        }
    };

    // 点击外面关闭
    useEffect(() => {
        if (!open) return;
        const onDocPointer = (ev: PointerEvent) => {
            const t = ev.target as Node | null;
            if (!t) return;
            if (triggerRef.current?.contains(t)) return;
            if (listboxRef.current?.contains(t)) return;
            setOpen(false);
        };
        document.addEventListener("pointerdown", onDocPointer);
        return () => document.removeEventListener("pointerdown", onDocPointer);
    }, [open]);

    const selected = options[selectedIndex];
    const triggerStyle: React.CSSProperties = width === "fit" ? {} : {minWidth: 88};

    return (
        <>
            <button
                id={id ?? triggerId}
                ref={triggerRef}
                type="button"
                className={`select__trigger${className ? ` ${className}` : ""}`}
                style={triggerStyle}
                role="combobox"
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-controls={listboxId}
                aria-activedescendant={open && activeIndex >= 0 ? optionId(activeIndex) : undefined}
                aria-label={ariaLabel}
                disabled={disabled}
                onClick={() => !disabled && setOpen((o) => !o)}
                onKeyDown={onTriggerKeyDown}
            >
                <span className="select__trigger-label">{selected ? selected.label : "—"}</span>
                {selected?.hint ? (
                    <span className="select__trigger-hint">{selected.hint}</span>
                ) : null}
                <svg
                    className={`select__caret${open ? " is-open" : ""}`}
                    viewBox="0 0 24 24"
                    width="14"
                    height="14"
                    aria-hidden="true"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                >
                    <path d="m6 9 6 6 6-6" />
                </svg>
            </button>

            {open && rect && typeof document !== "undefined"
                ? createPortal(
                    <ul
                        ref={listboxRef}
                        id={listboxId}
                        role="listbox"
                        tabIndex={-1}
                        aria-labelledby={triggerId}
                        className={`select__listbox${rect.placeAbove ? " is-above" : ""}`}
                        style={{
                            position: "fixed",
                            top: rect.top,
                            left: rect.left,
                            width: rect.width || FALLBACK_WIDTH,
                        }}
                        onKeyDown={onListKeyDown}
                    >
                        {options.map((opt, i) => {
                            const isSelected = opt.value === value;
                            const isActive = i === activeIndex;
                            return (
                                <li
                                    key={String(opt.value)}
                                    id={optionId(i)}
                                    role="option"
                                    aria-selected={isSelected}
                                    aria-disabled={opt.disabled || undefined}
                                    className={
                                        "select__option" +
                                        (isActive ? " is-active" : "") +
                                        (isSelected ? " is-selected" : "") +
                                        (opt.disabled ? " is-disabled" : "")
                                    }
                                    style={{minHeight: OPTION_MIN_PX}}
                                    onMouseEnter={() => setActiveIndex(i)}
                                    onClick={() => {
                                        if (opt.disabled) return;
                                        onChange(opt.value);
                                        close();
                                        focusTrigger();
                                    }}
                                >
                                    <span className="select__option-label">{opt.label}</span>
                                    {opt.hint ? (
                                        <span className="select__option-hint">{opt.hint}</span>
                                    ) : null}
                                    {isSelected ? (
                                        <svg
                                            className="select__option-check"
                                            viewBox="0 0 24 24"
                                            width="16"
                                            height="16"
                                            aria-hidden="true"
                                            fill="none"
                                            stroke="currentColor"
                                            strokeWidth="2.4"
                                            strokeLinecap="round"
                                            strokeLinejoin="round"
                                        >
                                            <path d="M5 12.5 10 17.5 19 7" />
                                        </svg>
                                    ) : null}
                                </li>
                            );
                        })}
                    </ul>,
                    document.body,
                )
                : null}
        </>
    );
}
