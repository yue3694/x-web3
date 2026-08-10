import {Link} from "react-router-dom";

import {Hero} from "@/components/Hero";

const PATHS = [
    {to: "/courses", index: "01", title: "Explore courses", body: "Browse verified curricula, review modules and enroll with an onchain purchase."},
    {to: "/account/enrollments", index: "02", title: "Learn with continuity", body: "Resume lessons, sync progress and keep every course activity in one focused workspace."},
    {to: "/account/certificates", index: "03", title: "Prove completion", body: "Collect verifiable course credentials after completing the required lessons."},
];

export function HomePage() {
    return (
        <>
            <Hero />
            <section className="journey" aria-labelledby="journey-title">
                <div className="section-heading">
                    <div>
                        <span className="eyebrow">One clear learning path</span>
                        <h2 id="journey-title">From discovery to proof</h2>
                        <p>Each stage now has its own route, context and system feedback.</p>
                    </div>
                </div>
                <div className="journey__grid">
                    {PATHS.map((item) => (
                        <Link className="journey-card" to={item.to} key={item.to}>
                            <span className="journey-card__index">{item.index}</span>
                            <h3>{item.title}</h3>
                            <p>{item.body}</p>
                            <span className="journey-card__link">Open workspace <span aria-hidden="true">→</span></span>
                        </Link>
                    ))}
                </div>
            </section>
            <section className="protocol-banner" aria-label="Platform architecture">
                <div>
                    <span className="eyebrow">Web2 speed · Web3 proof</span>
                    <h2>A focused interface over a verifiable protocol.</h2>
                </div>
                <Link className="btn btn--ghost" to="/swap">Open YD swap</Link>
            </section>
        </>
    );
}
