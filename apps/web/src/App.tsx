import {ConnectButton} from "./components/ConnectButton";
import {CounterCard} from "./components/CounterCard";

export function App() {
    return (
        <main className="container">
            <header className="header">
                <h1>x-web3</h1>
                <p>Vite + React + wagmi · Sepolia testnet</p>
                <ConnectButton />
            </header>
            <section className="panel">
                <CounterCard />
            </section>
            <footer className="footer">
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