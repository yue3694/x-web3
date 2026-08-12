import {Link} from "react-router-dom";

import {Hero} from "@/components/Hero";

const PATHS = [
    {to: "/courses", index: "01", title: "探索课程", body: "浏览已认证的课程大纲，查阅模块详情并通过链上购买完成报名。"},
    {to: "/account/enrollments", index: "02", title: "持续学习", body: "继续上次的课时、同步进度，把所有学习活动收拢在同一工作台中。"},
    {to: "/account/certificates", index: "03", title: "完课存证", body: "完成必修课时后，即可领取可验证的链上课程凭据。"},
];

export function HomePage() {
    return (
        <>
            <Hero />
            <section className="journey" aria-labelledby="journey-title">
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">一条清晰的学习路径</span>
                        <h2 id="journey-title">从发现到存证</h2>
                        <p>每个阶段都有专属的页面、上下文与系统反馈。</p>
                    </div>
                </div>
                <div className="journey__grid">
                    {PATHS.map((item) => (
                        <Link className="journey-card" to={item.to} key={item.to}>
                            <span className="journey-card__index">{item.index}</span>
                            <h3>{item.title}</h3>
                            <p>{item.body}</p>
                            <span className="journey-card__link">打开工作区 <span aria-hidden="true">→</span></span>
                        </Link>
                    ))}
                </div>
            </section>
            <section className="protocol-banner" aria-label="平台架构">
                <div>
                    <span className="eyebrow">Web2 的速度 · Web3 的存证</span>
                    <h2>聚焦的界面，构建于可验证的协议之上。</h2>
                </div>
                <Link className="btn btn--ghost" to="/swap">打开 YD 兑换</Link>
            </section>
        </>
    );
}
