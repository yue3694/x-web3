/**
 * WalletLink — 钱包绑定交互组件。
 *
 * 流程：
 *   1. 用户已通过 wagmi/ConnectKit 连接钱包；
 *   2. 调 wagmi signMessage 签名 canonical message：
 *        x-web3 bind wallet
 *        nonce: <server-issued>
 *        chainId: <chainId>
 *        address: <lowercased>
 *        domain: <API_DOMAIN>
 *        expiry: <RFC3339>
 *   3. POST /me/wallets/link；
 *   4. 成功后 refresh session。
 *
 * 注意：
 *   - nonce 由后端 /auth/wallets/nonce 颁发（M0 简化：本地生成占位）；
 *     等 F01 service 完善后补 endpoint。
 *   - signature 必须用 personal_sign（EIP-191），与 backend VerifyEIP191 一致。
 */

import {useState} from "react";
import {useAccount, useSignMessage, useChainId} from "wagmi";

import {authApi} from "@/api/types";
import {ApiClientError} from "@/api/client";
import {useSession} from "@/auth/SessionContext";

const CANONICAL_TEMPLATE = (params: {
    nonce: string;
    chainId: number;
    address: string;
    domain: string;
    expiry: string;
}) =>
    [
        "x-web3 bind wallet",
        `nonce: ${params.nonce}`,
        `chainId: ${params.chainId}`,
        `address: ${params.address.toLowerCase()}`,
        `domain: ${params.domain}`,
        `expiry: ${params.expiry}`,
    ].join("\n");

interface WalletLinkProps {
    /** API 域名；用于 EIP-191 签名（防跨站滥用）。空时从 import.meta.env 推断。 */
    domain?: string;
    onLinked?: () => void;
}

export function WalletLink({domain, onLinked}: WalletLinkProps) {
    const {address, isConnected} = useAccount();
    const chainId = useChainId();
    const {signMessageAsync} = useSignMessage();
    const {refresh} = useSession();
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);

    if (!isConnected || !address) {
        return (
            <div className="wallet-link">
                <p>Connect a wallet first to link it to your account.</p>
            </div>
        );
    }

    const handleLink = async () => {
        if (!chainId) {
            setError("wallet chainId unknown");
            return;
        }
        setBusy(true);
        setError(null);
        try {
			const challenge = await authApi.issueWalletNonce();
			const nonce = challenge.nonce;
			const expiry = challenge.expiresAt;
			const challengeDomain = domain ?? challenge.domain;
            const message = CANONICAL_TEMPLATE({
                nonce,
                chainId,
                address,
				domain: challengeDomain,
                expiry,
            });
            const signature = await signMessageAsync({message});

            await authApi.linkWallet({
                chainId,
                address,
                nonce,
                expiry,
                signature,
				domain: challengeDomain,
            });

            await refresh();
            onLinked?.();
        } catch (e) {
            const msg =
                e instanceof ApiClientError
                    ? `${e.code}: ${e.message}`
                    : e instanceof Error
                      ? e.message
                      : "link failed";
            setError(msg);
        } finally {
            setBusy(false);
        }
    };

    return (
        <div className="wallet-link">
            <p>
                Connected: <code>{address}</code> (chainId {chainId})
            </p>
            <button type="button" onClick={handleLink} disabled={busy}>
                {busy ? "Linking…" : "Link this wallet to my account"}
            </button>
            {error ? (
                <p role="alert" style={{color: "var(--ck-body-color-danger)"}}>
                    {error}
                </p>
            ) : null}
        </div>
    );
}
