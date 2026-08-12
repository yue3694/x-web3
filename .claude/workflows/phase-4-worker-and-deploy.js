export const meta = {
  name: 'phase-4-worker-and-deploy',
  description: 'Phase 4: F03 worker init/reorg/reconcile + F04 deploy script + F04 worker signer abstraction',
  phases: [
    { title: 'Implement' },
    { title: 'Verify & Test' },
    { title: 'Codex Review' },
    { title: 'Fix Findings' },
  ],
};

const { specsRoot, tasks } = args;

// =====================================================================
//  PHASE 1 — IMPLEMENT (3 parallel streams)
// =====================================================================
phase('Implement');

const streamA_prompt = [
  'You are the Go backend engineer (worker side) for this monorepo.',
  '',
  'PROJECT CONTEXT:',
  '- Repo: /Users/huyi/Documents/Coding/github/x-web3',
  '- Stack: pnpm workspaces monorepo; Go 1.22; pgx/v5; use github.com/ethereum/go-ethereum v1.13+ (ethclient)',
  '- Worker lives at apps/worker/; currently has cmd/worker/main.go (105 lines), internal/chain/decoder.go (CoursePurchased decoder + ValidateReceipt), internal/order/confirmer.go (Apply tx atomicity)',
  '- Spec: specs/web3University/features/03-order-onchain/tasks.md → F03-T09, F03-T12, F03-T13; read all three sections first',
  '- Existing DB schema: read database/migrations/0004_order.up.sql for chain_events / chain_checkpoints / outbox_events',
  '- Layered architecture: cmd/ → internal/{chain,order,indexer,reconcile}/ — keep cmd thin; internal packages pure',
  '',
  'DELIVERABLES (this turn must produce all three + green build):',
  '1. F03-T09: apps/worker/internal/indexer/',
  '   - ethclient.WS subscription with HTTP polling fallback (env: WORKER_WS_URL, WORKER_HTTP_URL, WORKER_CHAIN_ID)',
  '   - checkpoint table driver (read/write chain_checkpoints); on startup resume from last persisted checkpoint',
  '   - multi-RPC fallback: 2+ HTTP URLs cycled with backoff; unhealthy URL skipped until next window',
  '   - graceful shutdown via ctx.Done(): drain in-flight txs, flush checkpoint, close WS',
  '   - RPC health metric (atomic counter via expvar or slog)',
  '2. F03-T12: apps/worker/internal/indexer/reorg.go',
  '   - detect reorg by comparing block hash with N-confirmation checkpoint (configurable depth, default = CHAIN_CONFIRMATION_DEPTH env)',
  '   - on reorg: rollback chain_events rows tagged with the orphaned block, revert order state, emit reorg event; persist in chain_reorgs table (new migration alongside)',
  '   - admin API for manual rewind: apps/api/internal/admin/handlers/chain_rewind.go (POST /admin/chain/rewind {blockNumber}); admin-only via existing RBAC middleware; audit log via existing audit writer',
  '   - tests: reorg_test.go covering happy path + missed depth',
  '3. F03-T13: apps/worker/internal/reconcile/',
  '   - periodic scanner (default 30 min) re-pulls logs in [lastConfirmed-ConfirmDepth, lastIndexed] range',
  '   - gap detection: if chain_events.last_block_processed < checkpoint.head - ConfirmDepth, raise alert + DLQ entry',
  '   - DLQ writer: apps/worker/internal/reconcile/dlq.go writing to dlq_events table (new migration)',
  '   - DLQ admin endpoint: GET /admin/dlq + POST /admin/dlq/{id}/retry (re-enqueue)',
  '',
  'DATABASE MIGRATIONS:',
  '- Add database/migrations/0007_reorg_reconcile.up.sql (+ .down.sql) for chain_reorgs + dlq_events; CREATE TABLE IF NOT EXISTS; indexes',
  '',
  'TESTING (mandatory):',
  '- Unit tests for indexer (subscription mock, fallback); reorg (synthetic chain reorg); reconcile (gap detection)',
  '- Integration: stub ethclient interface — match existing test patterns in apps/api/internal/integration/',
  '- cd apps/worker && go test ./... — all pass',
  '- go vet ./... — clean',
  '',
  'CODE STYLE:',
  '- Match existing apps/worker style: lower_snake vars, error wrapping with %w, sentinel errors via errors.New, exhaustive switch',
  '- All public funcs: godoc first line = symbol name; explanation paragraph; edge cases in note',
  '- Use slog (consistent with main.go); never log secrets (redact by name)',
  '- pkg-level interface for ethclient used by indexer; concrete impl injected in main',
  '',
  'DEPENDENCIES:',
  '- Add to apps/worker/go.mod if needed: github.com/ethereum/go-ethereum',
  '- Run go mod tidy after changes',
  '',
  'VERIFICATION before reporting done:',
  '- pnpm --filter @x-web3/worker typecheck OR go build ./... — must succeed',
  '- go test ./... — all pass',
  '- All file paths absolute',
  '- List every new file with absolute path + line count',
  '- List every modified file with absolute path + diff summary',
  '- Note any TODOs or known limitations explicitly',
  '',
  'DELIVER REPORT: file list (new + modified), test count + result, deviations, 1-paragraph summary suitable for codex review.',
  'Do not commit. Do not push.'
].join('\n');

const streamB_prompt = [
  'You are the Solidity / Foundry engineer for this monorepo.',
  '',
  'PROJECT CONTEXT:',
  '- Repo: /Users/huyi/Documents/Coding/github/x-web3',
  '- Foundry + Solidity 0.8.24; OpenZeppelin v5; forge fmt / forge test required',
  '- Patterns to mirror:',
  '  - packages/contracts/script/DeployCourseMarket.s.sol (JSON mode + console summary)',
  '  - packages/contracts/script/DeployCounter.s.sol (simple script)',
  '- Existing contract: packages/contracts/src/CertificateNFT.sol (Soulbound ERC721 + AccessControl)',
  '- Existing interface: packages/contracts/src/interfaces/ICertificateNFT.sol',
  '- Spec: specs/web3University/features/04-learning-certificate/tasks.md → F04-T03',
  '- Existing tests in packages/contracts/test/CertificateNFT.t.sol (do NOT modify)',
  '',
  'DELIVERABLE: F04-T03 — packages/contracts/script/DeployCertificateNFT.s.sol',
  '',
  'MUST SUPPORT:',
  '- Two deployer modes (mirror DeployCourseMarket):',
  '  - Mode A: simple — initialAdmin + initialMinter via env (CERT_NFT_ADMIN_ADDRESS, CERT_NFT_MINTER_ADDRESS)',
  '  - Mode B: JSON — CERT_NFT_CONFIG_PATH pointing to { admin, minter, burner? } (burner defaults to admin)',
  '- Constructor: new CertificateNFT(admin, minter); after deploy grant BURNER_ROLE to CERT_NFT_BURNER_ADDRESS if provided; default burner = admin',
  '- Console summary mirroring DeployCourseMarket format: address / admin / minter / burner; next steps (copy to deployments.ts; run pnpm contracts:export:abi)',
  '- Validate: revert descriptive errors if any address zero or env unset (use vm.envOr with address(0) then check)',
  '- NatSpec on every public function',
  '- StdJson parsing helpers as in DeployCourseMarket',
  '',
  'TESTING (mandatory):',
  '- packages/contracts/test/DeployCertificateNFTScript.t.sol — mirror DeployCourseMarketScript.t.sol structure',
  '- Tests: env mode (admin/minter/burner), JSON mode happy path, zero-address rejection, JSON parse errors',
  '- forge fmt --check: clean',
  '- forge test --match-path "test/DeployCertificateNFTScript.t.sol": all pass',
  '',
  'VERIFICATION before reporting done:',
  '- cd packages/contracts && forge fmt --check: exit 0',
  '- forge build: exit 0 (warnings OK, errors NOT)',
  '- forge test --match-path "test/DeployCertificateNFTScript.t.sol": all pass',
  '- forge test: 78+ pass',
  '- All file paths absolute; new file with line count; modified files with diff summary; deviations',
  '',
  'DELIVER REPORT same format. Do not commit. Do not push.'
].join('\n');

const streamC_prompt = [
  'You are the Go backend engineer (worker side, certificate module).',
  '',
  'PROJECT CONTEXT:',
  '- Repo: /Users/huyi/Documents/Coding/github/x-web3',
  '- Stack: Go 1.22; pgx/v5; github.com/ethereum/go-ethereum (ethclient, accounts/abi/bind, common, crypto); AWS SDK v2 (for KMS) — add if needed',
  '- Existing: apps/worker/internal/{chain,order}/ — sentinel errors via errors.New, interface + impl pattern',
  '- Existing CertificateNFT: packages/contracts/src/CertificateNFT.sol (mintCertificate(address to, uint256 certificateId, string uri))',
  '- ABI access: packages/contracts/out/CertificateNFT.sol/CertificateNFT.json generated by forge build',
  '- Spec: specs/web3University/features/04-learning-certificate/tasks.md → F04-T10',
  '',
  'DELIVERABLE: F04-T10 — apps/worker/internal/certificate/signer.go',
  '',
  'MUST SUPPORT 3 driver backends selected by env SIGNER_DRIVER=keystore|kms|anvil:',
  '',
  '1. KeystoreDriver (default for staging):',
  '   - Reads SIGNER_KEYSTORE_PATH + SIGNER_KEYSTORE_PASSWORD',
  '   - Loads ECDSA key via ethereum/go-ethereum/accounts/keystore',
  '   - Wraps as bind.TransactOptsBackend or constructs TransactOpts per call',
  '   - Constant Address() cached at construction',
  '',
  '2. KMSDriver (production target):',
  '   - Reads SIGNER_KMS_KEY_ID (AWS KMS key ARN or alias)',
  '   - Uses AWS SDK v2 kms with Sign API (ECDSA_SHA_256 spec)',
  '   - Address derived once from public key (cached)',
  '   - Note: KMS does not give raw secp256k1 signature; secp256k1 support requires aws/kms-developer-guide pattern; if too complex, leave as TODO but interface must be defined and KMSDriver constructor must error clearly when not configured.',
  '   - For MVP, implement a thin KMSDriver that calls kms.Sign + converts to [R||S] + adjusts v via on-chain getChainId; document AWS-side setup in code comments',
  '',
  '3. AnvilDriver (local dev / tests):',
  '   - Reads SIGNER_ANVIL_PRIVATE_KEY (hex, with or without 0x prefix)',
  '   - Constructs transactor via bind.NewKeyedTransactorWithChainID',
  '   - Used by integration tests + anvil dev',
  '',
  'INTERFACE (define first):',
  '  type MintSigner interface {',
  '      Address() common.Address',
  '      SignCertificateMint(ctx context.Context, certID *big.Int, to common.Address, uri string) (*types.Transaction, error)',
  '  }',
  '- SignCertificateMint builds + signs the mintCertificate(to, certID, uri) tx bound to CertificateNFT address (CERT_NFT_ADDRESS env)',
  '- Returns the signed *types.Transaction (caller broadcasts via its own RPC client)',
  '- Errors: ErrSignerUnavailable, ErrInvalidAddress, ErrSignFailed (wrapped via fmt.Errorf %w)',
  '',
  'FACTORY:',
  '  func NewMintSigner(ctx context.Context, cfg SignerConfig) (MintSigner, error)',
  '- cfg: Driver string, plus driver-specific fields',
  '- Validates driver name, returns ErrUnsupportedDriver for unknown values',
  '',
  'TESTING:',
  '- apps/worker/internal/certificate/signer_test.go',
  '- AnvilDriver test: signs a known fixture, recovers address from sig, asserts == Address()',
  '- KeystoreDriver test: generates temp keystore, signs, recovers address, deletes temp',
  '- KMSDriver test: skip in unit test (requires AWS); verify constructor returns kms-not-configured error when env unset',
  '- NewMintSigner test: env-mode dispatch (keystore/kms/anvil)',
  '- cd apps/worker && go test ./internal/certificate/... — all pass',
  '',
  'VERIFICATION before reporting done:',
  '- cd apps/worker && go build ./... — exit 0',
  '- go test ./internal/certificate/... — all pass',
  '- go vet ./... — clean',
  '- All file paths absolute; new file with line count; modified files with diff summary; KMS limitations explicitly noted',
  '',
  'DELIVER REPORT same format. Do not commit. Do not push.'
].join('\n');

const implementResult = await parallel([
  () => agent(streamA_prompt, { label: 'Stream A: worker init/reorg/reconcile', phase: 'Implement', agentType: 'm-backend-engineer' }),
  () => agent(streamB_prompt, { label: 'Stream B: DeployCertificateNFT', phase: 'Implement', agentType: 'm-contract-engineer' }),
  () => agent(streamC_prompt, { label: 'Stream C: mint signer abstraction', phase: 'Implement', agentType: 'm-backend-engineer' }),
]);

// =====================================================================
//  PHASE 2 — VERIFY & TEST
// =====================================================================
phase('Verify & Test');

const verifyPrompt = [
  'You are the QA / build verifier. Read the three implementation reports from the previous phase (Stream A: worker init/reorg/reconcile, Stream B: DeployCertificateNFT, Stream C: mint signer).',
  '',
  'Your job:',
  '1. cd packages/contracts && forge fmt --check ; must exit 0',
  '2. cd packages/contracts && forge build ; must exit 0 (warnings OK)',
  '3. cd packages/contracts && forge test ; must be all green; report total count',
  '4. cd apps/worker && go build ./... ; must exit 0',
  '5. cd apps/worker && go vet ./... ; must be clean',
  '6. cd apps/worker && go test ./... ; report total / passed / failed',
  '7. cd packages/shared && pnpm exec tsc --noEmit ; must be 0',
  '8. cd apps/web && pnpm exec tsc --noEmit ; must be 0',
  '',
  'If ANY step fails:',
  '- Read the actual error output',
  '- Decide: implementation side effect or environmental issue',
  '- For side effects: send back to relevant stream with precise error (return verification_failed=true flag in your report)',
  '- For environmental issues: fix in-place if single-line config (add go.mod require, run go mod tidy) — DO NOT modify implementation logic',
  '',
  'Report format:',
  '{',
  '  ok: boolean,',
  '  forge_fmt: "pass" | "fail",',
  '  forge_build: "pass" | "fail",',
  '  forge_test: { total, passed, failed },',
  '  go_build: "pass" | "fail",',
  '  go_vet: "pass" | "fail",',
  '  go_test: { total, passed, failed },',
  '  shared_typecheck: "pass" | "fail",',
  '  web_typecheck: "pass" | "fail",',
  '  fixes_applied: [list of strings describing what you fixed in-place]',
  '}'
].join('\n');

const verifyResult = await agent(verifyPrompt, { label: 'Verify build + tests', phase: 'Verify & Test', agentType: 'general-purpose' });

if (!verifyResult.ok) {
  return {
    status: 'verify_failed',
    verify: verifyResult,
    implementResult,
    note: 'Pipeline halted at Verify & Test. Please review verify report and re-run after fixes.'
  };
}

// =====================================================================
//  PHASE 3 — CODEX REVIEW
// =====================================================================
phase('Codex Review');

const codexPrompt = [
  'You are a senior Go + Solidity + Web3 backend reviewer. Do a focused review of the Phase 4 changes in this monorepo.',
  '',
  'SCOPE OF REVIEW:',
  '- apps/worker/internal/indexer/ (worker init + WS/HTTP fallback + checkpoint + RPC rotation + graceful shutdown)',
  '- apps/worker/internal/indexer/reorg.go (reorg detection + rollback)',
  '- apps/worker/internal/reconcile/ (gap detection + DLQ)',
  '- apps/api/internal/admin/handlers/chain_rewind.go (admin rewind API)',
  '- packages/contracts/script/DeployCertificateNFT.s.sol',
  '- packages/contracts/test/DeployCertificateNFTScript.t.sol',
  '- apps/worker/internal/certificate/signer.go (KMS / keystore / anvil driver)',
  '- database/migrations/0007_reorg_reconcile.up.sql + .down.sql',
  '',
  'READ FIRST:',
  '- .claude/rules/security.md (EVM + key management rules)',
  '- .claude/rules/coding-style.md',
  '- .claude/rules/smart-contract.md',
  '- packages/contracts/src/CertificateNFT.sol',
  '- apps/worker/internal/{chain/decoder.go,order/confirmer.go}',
  '',
  'REVIEW FOR:',
  '- Critical: chain-reorg correctness, key material leakage (PK in env, logged signatures, KMS ARN leakage), RPC URL SSRF, rewind API auth bypass, SQL injection, unsigned tx handling, race on checkpoint writes',
  '- Important: graceful shutdown correctness, WS reconnect backoff, DLQ retry idempotency, signer dispatch env parsing',
  '- Nice-to-have: test coverage gaps, error message clarity, doc completeness',
  '',
  'DO NOT WRITE TO FILES. Produce a structured report.',
  '',
  'REPORT FORMAT (limit top 15 findings max, ranked most-severe first):',
  '',
  '## Critical (must fix before merge)',
  '- **[N]** File:line — one-sentence problem — concrete fix (or "no findings")',
  '',
  '## Important (should fix soon)',
  '- **[N]** File:line — one-sentence problem — concrete fix',
  '',
  '## Nice-to-have',
  '- **[N]** File:line — one-sentence problem — concrete fix',
  '',
  '## What looks good',
  '- (1-3 bullet points)',
  '',
  '## Summary',
  '- One paragraph: is this batch shippable? Top 3 priorities if not.'
].join('\n');

const codexResult = await agent(codexPrompt, { label: 'Codex review Phase 4', phase: 'Codex Review', agentType: 'general-purpose' });

// =====================================================================
//  PHASE 4 — FIX FINDINGS
// =====================================================================
phase('Fix Findings');

const findings = [].concat(codexResult.critical || [], codexResult.important || []);

if (findings.length === 0) {
  return {
    status: 'all_clean',
    implementResult,
    verify: verifyResult,
    codex: codexResult,
    note: 'Phase 4 complete: no Critical or Important findings from codex review.'
  };
}

const fixResult = await pipeline(
  findings,
  f => agent(
    'Apply this exact finding from the Phase 4 codex review:\n\n' +
    f.summary + '\n\n' +
    'File: ' + f.file + '\n' +
    'Line: ' + f.line + '\n' +
    'Suggested fix: ' + f.fix + '\n\n' +
    'Steps:\n' +
    '1. Read the file at ' + f.file + ' (and context files if needed)\n' +
    '2. Apply minimum-scope change to resolve the finding\n' +
    '3. If fix touches code with tests, run affected tests; fix any fallout\n' +
    '4. Report: file modified, line numbers changed, before/after snippet (5 lines each), test result\n\n' +
    'Do NOT introduce unrelated changes. Do NOT commit. Do NOT push.',
    { label: 'Fix finding: ' + (f.summary || '').slice(0, 50), phase: 'Fix Findings', agentType: 'general-purpose' }
  ),
  fix => agent(
    'Verify the fix for: ' + (fix.summary || fix.finding || 'unknown') + '\n\n' +
    'Run:\n' +
    '- cd packages/contracts && forge fmt --check && forge test (if Solidity touched)\n' +
    '- cd apps/worker && go build ./... && go test ./... (if Go touched)\n' +
    '- cd apps/api && go build ./... && go test ./... (if Go API touched)\n' +
    '- cd apps/web && pnpm exec tsc --noEmit (if TS touched)\n\n' +
    'Report pass/fail per step. If anything broke, send back to fix-agent with the error message.',
    { label: 'Verify fix', phase: 'Fix Findings', agentType: 'general-purpose' }
  )
);

return {
  status: 'fixed',
  implementResult,
  verify: verifyResult,
  codex: codexResult,
  fixes: fixResult,
  note: 'Phase 4 complete: all Critical + Important findings fixed and verified.'
};