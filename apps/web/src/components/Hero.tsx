/**
 * Hero — 主页主视觉。
 *
 * 设计目标：大气 + 可信。
 *   - 大号双行渐变标题；
 *   - 副标题点明价值主张；
 *   - 双 CTA：浏览课程 / 成为讲师；
 *   - 底部统计带，给用户立即的「这站点是活的」信号。
 */

import {Link} from "react-router-dom";
import {TARGET_CHAIN_ID, TARGET_CHAIN_NAME} from "@/chains";

interface Stat {
    label: string;
    value: string;
    hint?: string;
}

const STATS: Stat[] = [
    {value: "12+", label: "已上线课程", hint: "覆盖 4 大方向"},
    {value: TARGET_CHAIN_NAME, label: "测试链", hint: TARGET_CHAIN_ID === 31337 ? "本地 Anvil" : "Etherscan 已验证"},
    {value: "EAS", label: "凭据存证", hint: "链上工作证明"},
    {value: "Open", label: "开源", hint: "MIT 协议"},
];

export function Hero() {
    return (
        <section className="hero" id="top">
            <span className="hero__eyebrow">
                <span className="hero__eyebrow-dot" aria-hidden="true" />
                Web3 大学 · {TARGET_CHAIN_NAME}
            </span>

            <h1 className="hero__title">
                <span className="hero__title-line">链上技能</span>
                <span className="hero__title-line hero__title-line--accent">
                    由已认证师资倾囊相授
                </span>
            </h1>

            <p className="hero__lede">
                一所为开放互联网而生的大学。浏览经过认证的教师打造的课程，
                取得由 EAS 背书的链上凭据；请带好你的钱包 —— 每一笔回执都活在
                协议能够自证的地方。
            </p>

            <div className="hero__actions">
                <Link to="/courses" className="btn btn--primary hero__cta">
                    浏览课程 →
                </Link>
                <Link to="/studio" className="btn btn--ghost hero__cta">
                    成为讲师
                </Link>
            </div>

            <dl className="hero__stats" aria-label="大学概览">
                {STATS.map((stat) => (
                    <div className="hero__stat" key={stat.label}>
                        <dt className="hero__stat-label">{stat.label}</dt>
                        <dd className="hero__stat-value">{stat.value}</dd>
                        {stat.hint ? <dd className="hero__stat-hint">{stat.hint}</dd> : null}
                    </div>
                ))}
            </dl>
        </section>
    );
}
