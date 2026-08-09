/**
 * Footer — 站点底部导航。
 *
 * 多列结构：brand、产品、资源、技术。
 * 底部 bar 放状态/版权/外部链接。
 */

interface FooterColumn {
    title: string;
    links: Array<{label: string; href: string; external?: boolean}>;
}

const COLUMNS: FooterColumn[] = [
    {
        title: "Product",
        links: [
            {label: "Course catalog", href: "#catalog"},
            {label: "Teacher studio", href: "#studio"},
            {label: "Credential receipts", href: "#protocol"},
        ],
    },
    {
        title: "Resources",
        links: [
            {label: "Sepolia Etherscan", href: "https://sepolia.etherscan.io/", external: true},
            {label: "EAS attestations", href: "https://attest.sh/", external: true},
            {label: "Foundry book", href: "https://book.getfoundry.sh/", external: true},
        ],
    },
    {
        title: "Tech",
        links: [
            {label: "React + wagmi v2", href: "#top"},
            {label: "Solidity 0.8.24", href: "#top"},
            {label: "OpenZeppelin", href: "#top"},
        ],
    },
];

export function Footer() {
    const year = new Date().getFullYear();
    return (
        <footer className="site-footer" id="contact">
            <div className="site-footer__grid">
                <div className="site-footer__brand">
                    <span className="site-footer__logo">◆ WEB3 UNIVERSITY</span>
                    <p>
                        A university for the open internet. Built on Sepolia with
                        verifiable credentials for every receipt.
                    </p>
                </div>

                {COLUMNS.map((column) => (
                    <div className="site-footer__col" key={column.title}>
                        <h3>{column.title}</h3>
                        <ul>
                            {column.links.map((link) => (
                                <li key={link.label}>
                                    <a
                                        href={link.href}
                                        target={link.external ? "_blank" : undefined}
                                        rel={link.external ? "noreferrer" : undefined}
                                    >
                                        {link.label}
                                        {link.external ? <span aria-hidden="true"> ↗</span> : null}
                                    </a>
                                </li>
                            ))}
                        </ul>
                    </div>
                ))}
            </div>

            <div className="site-footer__bar">
                <span>// system_status: online</span>
                <span>© {year} x-web3 · MIT license</span>
                <a
                    href="https://sepolia.etherscan.io/"
                    target="_blank"
                    rel="noreferrer"
                >
                    sepolia.etherscan.io ↗
                </a>
            </div>
        </footer>
    );
}