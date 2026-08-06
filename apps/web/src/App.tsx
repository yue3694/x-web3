import {ConnectButton} from "./components/ConnectButton";
import {Notepad} from "./components/Notepad";

export function App() {
    return (
        <main className="container">
            <header className="header">
                <h1>
                    x-web3{" "}
                    <span className="glitch">// ON-CHAIN NOTEPAD</span>
                </h1>
                <p>
                    <span className="blink">█</span> Sepolia testnet · Vite +
                    React + wagmi v2 · ConnectKit
                </p>
                <ConnectButton />
            </header>
            <section className="panel">
                <Notepad />
            </section>
            <footer className="footer">
                <span>// system_status: online</span>
                <a
                    href="https://sepolia.etherscan.io/"
                    target="_blank"
                    rel="noreferrer"
                >
                    sepolia.etherscan.io ↗
                </a>
            </footer>
        </main>
    );
}
