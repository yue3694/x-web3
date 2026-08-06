import {ConnectKitButton} from "connectkit";

/**
 * Cyberpunk-themed wallet button. ConnectKit owns the modal/connector list,
 * but the trigger button is fully rendered here so it can match the rest of
 * the HUD: chamfered border, neon pink glow on hover, monospace address.
 */
export function ConnectButton() {
    return (
        <ConnectKitButton.Custom>
            {({
                isConnected,
                isConnecting,
                show,
                hide,
                address,
                truncatedAddress,
                chain,
                ensName,
            }) => {
                if (isConnected) {
                    const wrongChain =
                        chain?.id !== undefined && chain.id !== 11155111;
                    return (
                        <div className="connect connect--online">
                            <span
                                className={
                                    wrongChain
                                        ? "badge badge--warn"
                                        : "badge badge--net"
                                }
                                title={
                                    wrongChain
                                        ? "Wrong network — switch to Sepolia"
                                        : "Sepolia testnet"
                                }
                            >
                                {wrongChain ? "!" : "●"}{" "}
                                {chain?.name ?? "Unknown"} ·{" "}
                                {chain?.id ?? "—"}
                            </span>
                            <code className="address" onClick={show}>
                                {ensName ?? truncatedAddress ?? address}
                            </code>
                            <button
                                type="button"
                                className="btn btn--ghost"
                                onClick={show}
                            >
                                Manage
                            </button>
                            <button
                                type="button"
                                className="btn btn--danger-ghost"
                                onClick={hide}
                            >
                                Disconnect
                            </button>
                        </div>
                    );
                }
                return (
                    <div className="connect">
                        <button
                            type="button"
                            className="btn btn--primary"
                            disabled={isConnecting}
                            onClick={show}
                        >
                            {isConnecting ? (
                                <>
                                    <span className="blink">●</span>{" "}
                                    Linking Wallet…
                                </>
                            ) : (
                                <>
                                    <span className="blink">▶</span>{" "}
                                    Connect Wallet
                                </>
                            )}
                        </button>
                    </div>
                );
            }}
        </ConnectKitButton.Custom>
    );
}
