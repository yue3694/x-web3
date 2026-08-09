/**
 * Hero — 主页主视觉。
 *
 * 设计目标：大气 + 可信。
 *   - 大号双行渐变标题；
 *   - 副标题点明价值主张；
 *   - 双 CTA：浏览课程 / 成为讲师；
 *   - 底部统计带，给用户立即的「这站点是活的」信号。
 */

interface Stat {
    label: string;
    value: string;
    hint?: string;
}

const STATS: Stat[] = [
    {value: "12+", label: "Published courses", hint: "Across 4 tracks"},
    {value: "Sepolia", label: "Testnet deployment", hint: "Verified on Etherscan"},
    {value: "EAS", label: "Credential receipts", hint: "Onchain proof of work"},
    {value: "Open", label: "Source", hint: "MIT licensed"},
];

export function Hero() {
    return (
        <section className="hero" id="top">
            <span className="hero__eyebrow">
                <span className="hero__eyebrow-dot" aria-hidden="true" />
                Web3 University · Sepolia testnet
            </span>

            <h1 className="hero__title">
                <span className="hero__title-line">Master onchain</span>
                <span className="hero__title-line hero__title-line--accent">
                    from verified faculty.
                </span>
            </h1>

            <p className="hero__lede">
                A university for the open internet. Browse courses built by verified
                teachers, earn onchain credentials backed by EAS, and bring your
                wallet — every receipt lives where the protocol can prove it.
            </p>

            <div className="hero__actions">
                <a href="#catalog" className="btn btn--primary hero__cta">
                    Browse courses →
                </a>
                <a href="#studio" className="btn btn--ghost hero__cta">
                    Become a teacher
                </a>
            </div>

            <dl className="hero__stats" aria-label="University at a glance">
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