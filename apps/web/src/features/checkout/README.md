# Checkout (F03)

Course purchase flow — combines a wagmi v2 `useWriteContract` against the
on-chain `CourseMarket` with a server-side purchase-intent / order
confirmation handshake.

## Files

| File | Purpose |
|------|---------|
| `checkoutTypes.ts` | Shared `CheckoutState`, `CheckoutContextProps`, intent / ack shapes. |
| `CheckoutButton.tsx` | The actual write-on-chain button with full state machine. |
| `CheckoutPanel.tsx` | Wrapper: price breakdown + terms checkbox + button + error banner. |

## State machine (`CheckoutState`)

```
idle → preparing → signing → confirming → done
                                       ↘ failed (→ retry restarts at idle)
```

- **idle** — initial; nothing in flight.
- **preparing** — `POST /purchase-intents` to get server-issued intent.
- **signing** — wallet prompt is up (`useWriteContract.writeContractAsync`).
- **confirming** — tx broadcast; `useWaitForTransactionReceipt` is polling.
- **done** — receipt landed; txHash reported back to `/orders/{intentId}/transactions`.
- **failed** — terminal failure; user can retry.

User rejection resets to **idle** + friendly toast; we never auto-retry.

## Props (`CheckoutContextProps`)

| Prop | Type | Notes |
|------|------|-------|
| `courseId` | `string` | Catalog UUID. |
| `courseTitle` | `string` | Display only. |
| `priceYD` | `string` (bigint) | Wei-string, used for total calc in panel. |
| `courseKey` | `0x${string}` (bytes32) | On-chain course identifier. |
| `recipient` | `0x${string}` | Marketplace treasury (comes from intent). |
| `onSuccess` | `(txHash) => void` | Fires after both chain confirm and backend ack. |

## Chain handling

- Target chain comes from `VITE_TARGET_CHAIN_ID` (Anvil 31337 or Sepolia 11155111).
- Wrong chain → button morphs into a target-chain switch action via `useSwitchChain`.
- After switch, the buy button re-appears; user re-clicks to start intent.

## ABI assumption

- ABI is read **only** from `apps/web/src/contracts/market.abi.ts`.
- The current file is a stub exposing `purchase(bytes32, address)` and the
  matching `MarketAbi` type. Once the CourseMarket contract ships, replace
  the stub by re-running `pnpm contracts:export:abi` (which generates
  `market.abi.ts` from `packages/contracts/src/CourseMarket.sol`).
- If the ABI export is missing, `CheckoutButton` throws
  `"ABI not yet exported"` at render — this is the deliberate guard required
  by the F03 contract.

## Backend endpoints

| Endpoint | When | Body |
|----------|------|------|
| `POST /purchase-intents` | before `writeContract` | `{courseId, chainId}` |
| `POST /orders/{intentId}/transactions` | after txHash available | `{txHash, chainId}` |

Both go through `apiClient` (`@/api/client`) which already handles
credentials, X-Request-ID, and `ApiClientError` parsing.

## Error mapping

| Code | Source | UX |
|------|--------|----|
| `wrong-network` | `useChainId()` mismatch | Hidden buy button + network switch. |
| `user-rejected` | EIP-1193 user rejection | Reset to idle + "User rejected" toast. |
| `abi-missing` | `market.abi.ts` empty | Red notice: contract not yet deployed. |
| `api` | `ApiClientError` | Banner with server `message`. |
| `tx-failed` | `useWaitForTransactionReceipt` error | Banner; button offers "Retry". |
| `unknown` | fallback | Generic banner. |

## TODOs

- `market.abi.ts` — replace stub with real ABI after CourseMarket is
  implemented in `packages/contracts` and exported.
- `recipient` — currently injected by caller; once `/purchase-intents`
  is live, prefer the value returned in the intent response.
- UX copy and styles are minimal; rely on `panel / btn--primary / notice`
  primitives from `styles.css` until F03 design pass lands.
