/**
 * Footer — 站点底部导航。
 *
 * 多列结构：brand、产品、资源、技术。
 * 底部 bar 放状态/版权/外部链接。
 */

import {Link} from "react-router-dom";
import {TARGET_CHAIN_ID, TARGET_CHAIN_NAME} from "@/chains";

interface FooterColumn {
    title: string;
    links: Array<{label: string; href: string; external?: boolean}>;
}

const COLUMNS: FooterColumn[] = [
    {
        title: "产品",
        links: [
            {label: "课程目录", href: "/courses"},
            {label: "讲师工作台", href: "/studio"},
            {label: "我的凭据", href: "/account/certificates"},
        ],
    },
    {
        title: "资源",
        links: [
            ...(TARGET_CHAIN_ID === 11155111 ? [{label: "Sepolia 浏览器", href: "https://sepolia.etherscan.io/", external: true}] : []),
            {label: "EAS 证明服务", href: "https://attest.sh/", external: true},
            {label: "Foundry 手册", href: "https://book.getfoundry.sh/", external: true},
        ],
    },
    {
        title: "技术栈",
        links: [
            {label: "React + wagmi v2", href: "/"},
            {label: "Solidity 0.8.24", href: "/"},
            {label: "OpenZeppelin", href: "/"},
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
                        一所为开放互联网而生的大学。基于 {TARGET_CHAIN_NAME} 构建，
                        为每一笔回执提供可验证的链上凭据。
                    </p>
                </div>

                {COLUMNS.map((column) => (
                    <div className="site-footer__col" key={column.title}>
                        <h3>{column.title}</h3>
                        <ul>
                            {column.links.map((link) => (
                                <li key={link.label}>
                                    {link.external ? <a href={link.href} target="_blank" rel="noreferrer">{link.label}<span aria-hidden="true"> ↗</span></a> : <Link to={link.href}>{link.label}</Link>}
                                </li>
                            ))}
                        </ul>
                    </div>
                ))}
            </div>

            <div className="site-footer__bar">
                <span>// system_status: 在线</span>
                <span>© {year} x-web3 · MIT 协议</span>
                {TARGET_CHAIN_ID === 11155111 ? (
                    <a href="https://sepolia.etherscan.io/" target="_blank" rel="noreferrer">sepolia.etherscan.io ↗</a>
                ) : <span>chain_id: {TARGET_CHAIN_ID}</span>}
            </div>
        </footer>
    );
}
