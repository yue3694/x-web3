import {useAccount, useConnect, useDisconnect} from "wagmi";

export function ConnectButton() {
    const {address, isConnected, chain} = useAccount();
    const {connectors, connect, isPending, error} = useConnect();
    const {disconnect} = useDisconnect();

    if (isConnected) {
        return (
            <div className="connect">
                <span className="badge">
                    {chain?.name ?? "Unknown"} · {chain?.id ?? "—"}
                </span>
                <code className="address">{shorten(address)}</code>
                <button type="button" onClick={() => disconnect()}>
                    Disconnect
                </button>
            </div>
        );
    }

    return (
        <div className="connect">
            {connectors.map((connector) => (
                <button
                    key={connector.uid}
                    type="button"
                    disabled={isPending}
                    onClick={() => connect({connector})}
                >
                    {isPending ? "Connecting…" : `Connect ${connector.name}`}
                </button>
            ))}
            {error && <p className="error">{error.message}</p>}
        </div>
    );
}

function shorten(addr?: string) {
    if (!addr) return "";
    return `${addr.slice(0, 6)}…${addr.slice(-4)}`;
}