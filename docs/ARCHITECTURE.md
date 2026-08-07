# x-web3 Architecture

> End-to-end guide to the monorepo, the contract ↔ frontend bridge, and the
> Sepolia deploy pipeline. Read this top-to-bottom once and you'll understand
> every moving part.

---

## 1. What is this project?

A working **dApp reference**: a Vite + React + wagmi frontend that talks to a
Foundry smart contract on the **Sepolia** testnet. The shipped feature is an
**on-chain notepad** — each MetaMask wallet owns its own list of notes that
anyone can read but only the owner can mutate.

Beyond the demo, the monorepo is structured so you can:

- drop in a new contract (`packages/contracts/src/Foo.sol`) and have it
  picked up by the ABI-export pipeline + frontend with minimal ceremony;
- iterate on the UI without touching the contract code, and vice versa;
- redeploy cleanly to any EVM chain by editing one config file.

---

## 2. Monorepo layout

```
x-web3/
├── apps/
│   └── web/                       # Vite + React + wagmi frontend
│       ├── src/
│       │   ├── components/        # UI components (Notepad, ConnectButton)
│       │   ├── contracts/         # Auto-generated ABI + hand-written addresses
│       │   ├── App.tsx · main.tsx · wagmi.ts · styles.css · vite-env.d.ts
│       │   └── ...
│       ├── vite.config.ts · tsconfig*.json
│       └── .env.example
│
├── packages/
│   └── contracts/                 # Foundry smart contracts
│       ├── src/                   # *.sol  (production)
│       ├── test/                  # *.t.sol  (forge tests)
│       ├── script/                # *.s.sol (deploy), *.mjs (ABI export)
│       ├── node_modules/          # OZ + forge-std (pnpm-managed, no git submodules)
│       ├── foundry.toml · remappings.txt
│       └── .env.example
│
├── docs/                          # ← you are here
├── .claude/                       # Claude Code rules & project facade
├── .env.example                   # ← copy to .env
├── package.json                   # root workspace manifest
├── pnpm-workspace.yaml
└── README.md
```

**Why pnpm workspaces?** Fast installs, `pnpm --filter @x-web3/web dev`
to run just one package, hoisted dependency dedup.

---

## 3. The contract ↔ frontend bridge

This is the key idea — **the frontend never invents the ABI; it is always
copied from the on-chain source of truth at compile time**.

```
┌─────────────────────┐                  ┌─────────────────────┐
│ packages/contracts  │                  │ apps/web            │
│                     │                  │                     │
│  src/Notepad.sol    │  ─── forge build  │  src/contracts/     │
│         │           │        ↓         │   notepad.abi.ts    │ ← JS bundle
│         ▼           │   out/Notepad.sol │   (typed `as const`)│
│  out/Notepad.sol/   │        │         │         │           │
│  Notepad.json       │        │         │         ▼           │
│         │           │        │         │  components/        │
│         ▼           │        │         │   Notepad.tsx       │
│  script/            │  export-abi.mjs   │   useReadContract() │
│  export-abi.mjs     │ ─────────────────►│   useWriteContract()│
└─────────────────────┘                  └─────────────────────┘
```

### 3.1 Forge compile

`pnpm contracts:compile` → `forge build` in `packages/contracts/`:

- Reads `src/*.sol`, resolves imports via `remappings.txt`:
  ```
  forge-std/=node_modules/forge-std/src/
  @openzeppelin/contracts/=node_modules/@openzeppelin/contracts/contracts/
  ```
  These two packages are installed by pnpm (`package.json: dependencies`)
  — no `forge install`, no `git submodule`, no `lib/` directory.
- Produces `out/<ContractName>.sol/<ContractName>.json` — each artifact
  contains the bytecode, deployed bytecode, and the **ABI** (function
  signatures + event signatures + error signatures).

### 3.2 ABI export

`pnpm contracts:export:abi` → `node script/export-abi.mjs Counter Notepad`:

For each contract name passed in, the script:

1. reads `out/<Name>.sol/<Name>.json`;
2. extracts `artifact.abi`;
3. writes a TypeScript module to `apps/web/src/contracts/<name>.abi.ts` of the
   shape:
   ```ts
   export const notepadAbi = [...] as const;
   export type NotepadAbi = typeof notepadAbi;
   ```

The `as const` is critical: it makes the array literal-typed, so wagmi can
infer function names, argument types, and return types from it. The whole
pipeline is type-safe end-to-end — calling a function that doesn't exist
in the contract is a TypeScript error.

> **Why not auto-detect all contracts?** The export script is parameterized
> (`node export-abi.mjs Foo Bar`). Default = `[Counter, Notepad]`. Pass
> names to export a subset, e.g. `node export-abi.mjs Notepad`.

> **Why does the script NOT touch `deployments.ts`?** That's hand-edited
> because deployments happen infrequently and we don't want a script to
> silently wipe the address you pasted in last deploy.

### 3.3 Hand-written deployment addresses

`apps/web/src/contracts/deployments.ts` is the **only** place that knows
which on-chain address corresponds to which contract:

```ts
export const notepadDeployments = {
    sepolia: {
        address: '0xYourDeployedAddress...',
        chainId: 11155111,
    },
} as const;
```

The frontend reads `notepadDeployments.sepolia.address` and passes it to
every wagmi hook. To deploy to a new chain: run `forge script` against that
chain's RPC, paste the printed address into a new entry.

### 3.4 wagmi hooks do the rest

In `apps/web/src/components/Notepad.tsx`:

```tsx
const {data: notes} = useReadContract({
    abi: notepadAbi,
    address: notepadDeployments.sepolia.address,
    functionName: 'getNotes',
    args: [address],
    chainId: notepadDeployments.sepolia.chainId,
});
```

wagmi turns this into an `eth_call` JSON-RPC against the configured
transport (Alchemy / Infura / public), and TypeScript checks that
`getNotes` exists in the ABI and takes a single `address` argument.

---

## 4. Sepolia deploy pipeline

```
.env (git-ignored)              packages/contracts/                Sepolia
─────────────────               ────────────────────               ────────
SEPOLIA_RPC_URL ───────────►   forge script ──broadcast──►        tx mined
ETHERSCAN_API_KEY ─────────►   forge script ──verify ──────►     Etherscan
DEPLOYER_PRIVATE_KEY ──────►   script/DeployNotepad.s.sol         contract
                                                                       │
                                                                       ▼
                                                            broadcast/11155111/
                                                              run-latest.json
                                                                       │
                                                                       ▼
                                          apps/web/src/contracts/
                                            deployments.ts  ← (paste manually)
```

The contract auto-verifies on Etherscan because `forge script --verify`
calls Etherscan's `verifysourcecode` endpoint with the Solidity source
**and** the exact compiler settings used to build it. Result: a green
"Verified" badge on the contract page, and any user can read / decompile
the deployed bytecode to confirm it matches the source.

### 4.1 Per-network configuration

`foundry.toml` only pins Solidity version + optimizer. RPC URLs come from
`.env` at runtime. To deploy to a different chain:

```bash
forge script script/DeployNotepad.s.sol:DeployNotepad \
    --rpc-url https://mainnet.infura.io/v3/$INFURA_KEY \
    --broadcast --verify -vvvv
```

### 4.2 Why `run-latest.json` matters

Foundry writes a complete deployment receipt to
`packages/contracts/broadcast/<chainid>/run-latest.json` after every
broadcast. **Commit this file.** It's the audit trail: which script ran,
which txs were sent, the addresses, the contract args. If you redeploy,
rename the old file (e.g. `run-2026-08-06.json`) before the next broadcast.

---

## 5. The Notepad feature in detail

### 5.1 Contract surface (`packages/contracts/src/Notepad.sol`)

```solidity
struct Note {
    uint256 id;          // 1-based, monotonic per owner
    string  title;       // max 64 bytes
    string  body;        // max 1024 bytes
    uint64  createdAt;
    uint64  updatedAt;
}

function createNote(string title, string body) external returns (uint256 id);
function updateNote(uint256 id, string title, string body) external;
function deleteNote(uint256 id) external;

function getNoteCount(address owner) external view returns (uint256);
function getNote(address owner, uint256 id) external view returns (Note memory);
function getNotes(address owner) external view returns (Note[] memory);

event NoteCreated(address indexed owner, uint256 indexed id, uint64 at);
event NoteUpdated(address indexed owner, uint256 indexed id, uint64 at);
event NoteDeleted(address indexed owner, uint256 indexed id, uint64 at);

error TitleTooLong();
error BodyTooLong();
error TooManyNotes();
error NoteNotFound();
```

**Limits** (public constants so the frontend can read them off-chain):

| Constant | Value | Why |
|----------|-------|-----|
| `MAX_TITLE_LEN` | 64 bytes | Comfortable headline length; cheap gas. |
| `MAX_BODY_LEN` | 1024 bytes (1 KB) | ~50 KB total per user at 50 notes — well under one RPC response. |
| `MAX_NOTES_PER_USER` | 50 | Bounds `getNotes` return size; linear scans stay O(50). |

**Storage layout**: `mapping(address => Note[])`. Each owner has their own
array. No shared index, no cross-owner leakage.

### 5.2 The `id` invariant — read this before editing the contract

- `id` is **1-based and monotonic per owner**. The first note any user
  creates gets `id=1`. The next gets `id=2`. Etc.
- `id` is **never reused**, even after deletion. So `id=3` after a delete
  is unambiguously the third note ever created, not "the third note that
  currently exists".
- Deletion uses **swap-and-pop**: deleting note `id=k` swaps the **last**
  array element into slot `k-1` (1-based index → 0-based array slot),
  then `pop()`s the tail. The surviving tail note's `id` is preserved
  across the move.
- Frontends must sort by `id` before rendering, because the array is in
  storage order (which changes when you swap-and-pop).

```
Before deleteNote(2):  [{id:1}, {id:2}, {id:3}]
After deleteNote(2):   [{id:1}, {id:3}]      (id:3 swapped into slot 1)
                       array.length == 2
                       NoteNotFound reverts on getNote(_, 2)
```

### 5.3 Why no Ownable?

A single deployer-admin role would be misleading here: **the contract has
no admin**. Each user is their own admin via `msg.sender` checks. Anyone
can read anyone's notes (public view functions take an arbitrary `owner`
arg).

### 5.4 Frontend behavior (`apps/web/src/components/Notepad.tsx`)

**Three render modes**, switched via a local `useState`:

| Mode | What shows in the editor pane |
|------|-------------------------------|
| `view` | Either "select a note" empty state, or the selected note's title/body + Edit/Delete buttons |
| `create` | Empty title + body inputs + Save/Cancel |
| `edit` | Pre-filled title + body inputs + Save/Cancel |

**State machine** (selected note = `N`, mode = `M`):

```
              click +New           click N        click Edit
   (any) ───────────────► create ───────► view ───────────► edit
     ▲                       │       ▲                       │
     │            cancel     │       │             save      │
     └───────────────────────┘       └───────────────────────┘
                                       ↓ (tx confirmed)
                                  reset to (any)
```

**Hooks wiring** (verbatim pattern from the wagmi docs):

```tsx
const { data: notes, refetch }   = useReadContract({ /* getNotes */ });
const { data: hash, writeContract, isPending, error } = useWriteContract();
const { isLoading: isConfirming, isSuccess: isConfirmed } =
    useWaitForTransactionReceipt({ hash });

useEffect(() => {
    if (isConfirmed) { refetch(); /* reset editor */ }
}, [isConfirmed, refetch]);
```

This means: send a write → wait for receipt → on success, refresh reads
and clear the editor. No manual polling, no event listeners, no
subgraph needed.

**Guards** (in render order, like `CounterCard.tsx`):

1. `hasAddress` — `apps/web/src/contracts/deployments.ts` must be filled.
2. `isConnected` — `useAccount().isConnected`.
3. `isOnSepolia` — `useAccount().chainId === 11155111`.

If any guard fails, the user sees a single-line instruction telling them
exactly what to do — not a blank screen.

---

## 6. Local development workflow

```
   ┌──────────────────────────────────────────────────────────────────┐
   │                                                                  │
   │   edit Solidity                                                  │
   │       │                                                          │
   │       ▼                                                          │
   │   forge build ──► forge test ──► (deploy script dry-run on anvil)│
   │                                          │                       │
   │                                          ▼                       │
   │   export-abi.mjs ──► notepad.abi.ts   ────►   Vite HMR (dev)     │
   │                                          │                       │
   │                                          ▼                       │
   │                                    MetaMask pop-up               │
   │                                                                  │
   └──────────────────────────────────────────────────────────────────┘
```

Concrete commands:

```bash
# Contracts — fast inner loop
cd packages/contracts
forge build                         # compile
forge test                          # unit tests
forge test --match-contract Notepad # one suite
forge coverage                      # line coverage
forge fmt --check                   # CI-format guard

# Frontend — fast inner loop
cd ../..
pnpm dev                            # http://localhost:5173, HMR on save
pnpm --filter @x-web3/web typecheck # strict TS check

# Bridge
pnpm contracts:export:abi           # forge artifacts → TS ABI

# Deploy
pnpm contracts:deploy:notepad:sepolia  # broadcasts + verifies
```

---

## 7. Adding a new contract (cookbook)

Let's say you want to add a `TodoList` contract.

1. **Write it**:
   ```bash
   touch packages/contracts/src/TodoList.sol
   ```
   Write the contract. Follow `packages/contracts/src/Notepad.sol` as a
   template — NatSpec, custom errors, indexed event params.

2. **Write tests**:
   ```bash
   touch packages/contracts/test/TodoList.t.sol
   ```

3. **Write deploy script**:
   ```bash
   touch packages/contracts/script/DeployTodoList.s.sol
   ```
   Copy `DeployNotepad.s.sol`; change the contract reference.

4. **Add deployment script to package.json**:
   ```json
   "deploy:todo:sepolia": "forge script script/DeployTodoList.s.sol:DeployTodoList --rpc-url $SEPOLIA_RPC_URL --broadcast --verify -vvvv"
   ```

5. **Add to ABI export** (in `packages/contracts/package.json`):
   ```json
   "export:abi": "node ./script/export-abi.mjs Counter Notepad TodoList"
   ```

6. **Add deployments entry** in `apps/web/src/contracts/deployments.ts`:
   ```ts
   export const todoListDeployments = {
       sepolia: { address: undefined as Address | undefined, chainId: 11155111 },
   } as const;
   ```

7. **Build the UI**. Either reuse `Notepad.tsx`'s hook wiring as a
   template, or split components per-feature.

That's it. No bundler config, no alias, no global registration — pnpm +
wagmi + the ABI-export script pick everything up.

---

## 8. Security posture

| Surface | Posture | Reference |
|---------|---------|-----------|
| Private keys | In `.env` (gitignored); deployer should be a throwaway hot wallet | `.claude/rules/security.md` |
| RPC URLs | Frontend may only see `VITE_*` vars; deployer key never reaches the browser | `.env.example` |
| Reentrancy | Notepad has no external calls; no `ReentrancyGuard` needed | `smart-contract.md` CEI section |
| Integer overflow | Solidity 0.8 checked by default; no `unchecked` blocks | `foundry.toml solc_version = "0.8.24"` |
| Upgradeability | Notepad is non-upgradable by design | `smart-contract.md` upgradeability section |
| On-chain privacy | Public by design — Notepad stores plaintext | `smart-contract.md` known anti-patterns |

---

## 9. Glossary

| Term | Meaning |
|------|---------|
| **ABI** | Application Binary Interface — JSON description of a contract's functions, events, errors. The frontend needs it to know how to encode calls. |
| **RPC URL** | HTTP endpoint that proxies JSON-RPC requests to an EVM node (Alchemy / Infura / public). |
| **wagmi** | React hooks library for EVM (v2 of wagmi uses viem under the hood). |
| **viem** | Lower-level TypeScript library for EVM (replaces ethers.js). |
| **Etherscan** | Block explorer. Sepolia variant: <https://sepolia.etherscan.io/>. |
| **forge / cast / anvil** | Foundry's three CLI tools (compile+test+deploy / inspect / local node). |
| **Foundry** | Rust-based Solidity toolchain, alternative to Hardhat. |
| **`as const`** | TypeScript assertion that narrows an array to its literal type — lets wagmi infer exact function signatures from the ABI. |
| **swap-and-pop** | O(1) array deletion: move the tail into the freed slot, then pop the (now duplicate) tail. |

---

## 10. Where to look next

| If you want to… | Read |
|-----------------|------|
| Add a new contract | §7 above, then `packages/contracts/src/Notepad.sol` |
| Add a new UI feature | `apps/web/src/components/Notepad.tsx` (full CRUD as a template) |
| Deploy to mainnet | §4 — just swap `--rpc-url` and re-paste the address |
| Understand a wagmi hook | <https://wagmi.sh/react/api/hooks> |
| Understand a forge command | <https://book.getfoundry.sh/> |
| Tweak the project rules | `.claude/rules/*.md` |