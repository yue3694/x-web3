import {useCallback, useEffect, useRef, useState} from "react";
import {useAccount, useChainId, useSignMessage} from "wagmi";

import {authApi, type WalletLoginChallenge} from "@/api/types";
import {useSession} from "./SessionContext";

function loginMessage(challenge: WalletLoginChallenge, chainId: number, address: string): string {
    return [
        "x-web3 login",
        `nonce: ${challenge.nonce}`,
        `chainId: ${chainId}`,
        `address: ${address.toLowerCase()}`,
        `domain: ${challenge.domain}`,
        `expiry: ${challenge.expiresAt}`,
    ].join("\n");
}

export function WalletAutoSession() {
    const {address, isConnected} = useAccount();
    const chainId = useChainId();
    const {signMessageAsync} = useSignMessage();
    const {profile, setAuthenticatedProfile} = useSession();
    const attempted = useRef<string | null>(null);
    const [challenge, setChallenge] = useState<WalletLoginChallenge | null>(null);
    const [displayName, setDisplayName] = useState("");
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const [retryVersion, setRetryVersion] = useState(0);

    const complete = useCallback(async (activeChallenge: WalletLoginChallenge, name?: string) => {
        if (!address) return;
        setBusy(true);
        setError("");
        try {
            const signature = await signMessageAsync({message: loginMessage(activeChallenge, chainId, address)});
            const next = await authApi.loginWithWallet({
                chainId,
                address,
                nonce: activeChallenge.nonce,
                expiry: activeChallenge.expiresAt,
                domain: activeChallenge.domain,
                signature,
                displayName: name,
            });
            setAuthenticatedProfile(next);
            setChallenge(null);
        } catch (cause) {
            attempted.current = null;
            setChallenge(null);
            setError(cause instanceof Error ? cause.message : "钱包登录失败");
        } finally {
            setBusy(false);
        }
    }, [address, chainId, setAuthenticatedProfile, signMessageAsync]);

    useEffect(() => {
        if (!isConnected || !address || !chainId) {
            attempted.current = null;
            setChallenge(null);
            return;
        }
        const identity = `${chainId}:${address.toLowerCase()}`;
        const alreadyThisWallet = profile?.wallets.some((wallet) => wallet.chainId === chainId && wallet.address.toLowerCase() === address.toLowerCase());
        if (alreadyThisWallet || attempted.current === identity) return;
        attempted.current = identity;
        void authApi.issueWalletLoginNonce(chainId, address).then((next) => {
            if (next.registered) void complete(next);
            else {
                setDisplayName("");
                setChallenge(next);
            }
        }).catch((cause) => {
            attempted.current = null;
            setError(cause instanceof Error ? cause.message : "无法准备钱包登录");
        });
    }, [address, chainId, complete, isConnected, profile?.wallets, retryVersion]);

    if (!challenge && !error) return null;
    return (
        <div className="onboarding-overlay" role="presentation">
            <section className="onboarding-dialog" role="dialog" aria-modal="true" aria-labelledby="wallet-onboarding-title">
                <span className="eyebrow">Wallet account</span>
                <h2 id="wallet-onboarding-title">{challenge ? "创建你的学习身份" : "钱包登录未完成"}</h2>
                {challenge ? (
                    <>
                        <p>这是该钱包第一次登录。设置昵称后，确认一次免费签名即可完成注册和登录。</p>
                        <label><span>昵称</span><input autoFocus maxLength={40} value={displayName} onChange={(event) => setDisplayName(event.target.value)} placeholder="例如：Alice" /></label>
                        <button className="btn--primary" type="button" disabled={busy || displayName.trim().length < 2} onClick={() => void complete(challenge, displayName.trim())}>{busy ? "正在登录…" : "注册并登录"}</button>
                    </>
                ) : (
                    <>
                        <p className="notice notice--error">{error}</p>
                        <button className="btn--primary" type="button" onClick={() => {
                            attempted.current = null;
                            setError("");
                            setRetryVersion((value) => value + 1);
                        }}>重试钱包登录</button>
                    </>
                )}
            </section>
        </div>
    );
}
