export const meta = {
  name: 'phase-5-certificate-pipeline',
  description: 'Phase 5: F04 completion API + metadata + mint consumer + integration tests',
  phases: [
    { title: 'Implement' },
    { title: 'Verify & Test' },
    { title: 'Codex Review' },
    { title: 'Fix Findings' },
  ],
};

const { specsRoot, tasks } = args;

// =====================================================================
//  PHASE 1 — IMPLEMENT (2 parallel streams)
//  Stream A: completion service + completion API + mint job creation
//  Stream B: metadata generation + worker mint consumer + integration tests
// =====================================================================
phase('Implement');

const streamA_prompt = [
  'You are the Go backend engineer for this monorepo (API + Worker).',
  '',
  'PROJECT CONTEXT:',
  '- Repo: /Users/huyi/Documents/Coding/github/x-web3',
  '- Stack: Go 1.25; pgx/v5; viem-go is not used; use github.com/ethereum/go-ethereum for ABI; use github.com/google/uuid for UUIDs',
  '- Existing DB schema: read database/migrations/0003_enrollments.up.sql (enrollments), 0002_course.up.sql (lessons, chapters), 0004_order.up.sql (orders, chain_events)',
  '- Existing API: apps/api/internal/{learning,media,course,handlers,httpkit,audit}',
  '- Existing Worker: apps/worker/internal/{certificate,indexer,order}',
  '- Spec: specs/web3University/features/04-learning-certificate/tasks.md → F04-T07',
  '- Existing learning.go only has playback presign; no completion logic yet',
  '',
  'DELIVERABLES (this turn must produce all + green build + tests):',
  '',
  '1. F04-T07: apps/api/internal/certificate/completion.go',
  '   - POST /courses/{id}/complete endpoint',
  '   - Auth: require enrollment (read enrollments table; if missing → 403)',
  '   - Completion rule: 100% of required lessons (read lessons table for course; required=true) must have progress.pct=100',
  '   - Idempotency: ON CONFLICT (user_id, course_id) DO NOTHING; if completion already exists, return existing record',
  '   - Atomic transaction: INSERT INTO course_completions ... + INSERT INTO certificate_jobs (status=pending, attempt=0) ... — both in same tx; UNIQUE (user_id, course_id) on certificate_jobs to prevent duplicate jobs',
  '   - new migrations: database/migrations/0008_certificates.up.sql (+ .down.sql)',
  '     * course_completions (id, user_id, course_id, completed_at, completed_lessons_count, total_lessons_count)',
  '     * lesson_progress (user_id, lesson_id, pct, updated_at, UNIQUE (user_id, lesson_id))',
  '     * certificate_jobs (id, user_id, course_id, certificate_id, status, attempt, last_error, created_at, updated_at, UNIQUE (user_id, course_id))',
  '   - Tests: apps/api/internal/certificate/completion_test.go covering:',
  '     * happy path: 100% progress → completion + job created',
  '     * not enrolled → 403',
  '     * partial progress → 422 (UNPROCESSABLE_ENTITY, distinct from "already completed" idempotent return)',
  '     * idempotency: 2nd call same user/course → returns existing, no duplicate job',
  '     * atomicity: if INSERT course_completions fails, INSERT certificate_jobs must rollback',
  '',
  '2. F04-T08: apps/api/internal/learning/progress.go',
  '   - POST /lessons/{id}/progress { pct: 0-100 } endpoint',
  '   - Auth: require enrollment (read enrollments by user_id + course_id containing lesson_id; query chapters.lessons JOIN)',
  '   - Non-decreasing rule: existing.pct must not regress; same value OK (idempotent)',
  '   - atomic UPSERT INTO lesson_progress (user_id, lesson_id, pct, updated_at)',
  '   - 200 OK with new pct; 409 if regression attempt',
  '   - Tests: apps/api/internal/learning/progress_test.go covering happy path + regression',
  '',
  '3. F04-T08 (continued): apps/api/internal/learning/queries.go',
  '   - GET /me/enrollments → list with course details + completion pct (computed from lesson_progress)',
  '   - GET /me/certificates → list with status + cert id + tx hash',
  '   - Tests covering: empty list, multi-enrollment, completion counts',
  '',
  'CODE STYLE:',
  '- Match apps/api style: error sentinels, structured logging via slog, RBAC via httpkit.Require',
  '- Use existing audit writer (apps/api/internal/audit/writer.go) for completion + certificate events',
  '- Use existing objectstore.ObjectStore for any media interactions',
  '',
  'VERIFICATION before reporting done:',
  '- cd apps/api && go build ./... → exit 0',
  '- cd apps/api && go vet ./... → clean',
  '- cd apps/api && go test -count=1 ./internal/certificate/... ./internal/learning/... → all pass',
  '- cd packages/contracts && forge fmt --check (should be unchanged) → exit 0',
  '- cd apps/worker && go build ./... → exit 0',
  '',
  'DELIVER REPORT: new files (abs path + line count), modified files (abs path + diff summary), test counts, 1-paragraph summary.',
  '',
  'Do not commit. Do not push.'
].join('\n');

const streamB_prompt = [
  'You are the Go backend engineer for this monorepo (Worker + minor API).',
  '',
  'PROJECT CONTEXT:',
  '- Repo: /Users/huyi/Documents/Coding/github/x-web3',
  '- Stack: Go 1.25; pgx/v5; github.com/ethereum/go-ethereum; AWS SDK v2 (for S3) — add if needed',
  '- Existing: apps/worker/internal/certificate/signer.go (KMS/keystore/anvil) — USE this, do NOT reimplement',
  '- Existing: apps/api/internal/objectstore/objectstore.go (ObjectStore interface, PresignPut, PresignGet) — reuse',
  '- Existing API: apps/api/internal/audit/writer.go (audit.Entry)',
  '- Spec: specs/web3University/features/04-learning-certificate/tasks.md → F04-T09, F04-T11',
  '',
  'DELIVERABLES (this turn):',
  '',
  '1. F04-T09: apps/api/internal/certificate/metadata.go',
  '   - Generate certificate metadata JSON: { name, description, image, attributes: [{trait_type, value}], course_id, completion_date, recipient }',
  '   - Upload via objectstore.PresignPut → resolve to deterministic IPFS CID (use real Pinata/Web3.Storage OR a stub returning sha256(content) → "ipfs://bafk..." — pick whichever is consistent with existing media patterns)',
  '   - Validate: URI scheme must be ipfs:// or https://; reject data: URIs; length check',
  '   - Function signature: GenerateAndUpload(ctx, userID, courseID, courseMeta, recipientAddr) returns (uri string, sha256 string, err error)',
  '   - Tests: unit test that generates metadata, verifies JSON shape, validates URI schemes',
  '',
  '2. F04-T11: apps/worker/internal/certificate/consumer.go',
  '   - Consumer that polls certificate_jobs WHERE status=\'pending\' AND attempt < max_attempts ORDER BY created_at LIMIT batch',
  '   - For each job:',
  '     * Generate metadata via GenerateAndUpload (call API endpoint via HTTP OR call the same package — pick one, document)',
  '     * Sign via signer.SignCertificateMint(ctx, certificateID, recipient, uri) — already implemented',
  '     * Broadcast via ethclient.SendTransaction with retry/backoff (max 3, exp backoff)',
  '     * Wait for receipt with N-confirmation depth (reuse CHAIN_CONFIRMATION_DEPTH env)',
  '     * On success: UPDATE certificate_jobs SET status=\'confirmed\', tx_hash=$2, confirmed_at=now() WHERE id=$1',
  '     * On failure: UPDATE certificate_jobs SET status=\'failed\', attempt=attempt+1, last_error=$2, next_retry_at=now()+exp_backoff(attempt)',
  '     * After max_attempts (default 5): status=\'dead\', write to dlq_events with payload',
  '   - New migration (if needed): database/migrations/0009_cert_jobs_ext.up.sql (+ .down.sql) — add columns certificate_id (uint256 hex), tx_hash, attempt, last_error, next_retry_at if not present',
  '   - Consumer loop with graceful shutdown (ctx.Done + drain)',
  '   - Tests: consumer_test.go covering: success path (with fake signer), retry on broadcast fail, max attempts → DLQ, idempotency (re-running confirmed job is no-op)',
  '',
  '3. F04-T15: apps/worker/internal/certificate/integration_test.go + apps/api/internal/certificate/integration_test.go',
  '   - End-to-end: insert certificate_job → consumer picks up → generates metadata → signs (fake signer) → broadcasts (mock RPC) → receipt confirms → status=confirmed',
  '   - Test the failure scenarios: signature error → retry → eventual dead-letter',
  '   - Test idempotency: pre-confirmed job → consumer skips',
  '   - Use existing testenv helpers (apps/api/internal/integration/testenv)',
  '   - Mark tests with `// integration` build tag or env-gate them via DATABASE_URL_TEST',
  '',
  'CODE STYLE:',
  '- Match apps/worker style: sentinel errors via errors.New, interface + impl pattern, slog for logging, atomic counters for metrics',
  '- Reuse apps/worker/internal/certificate/signer.go — DO NOT reimplement KMS/keystore/anvil',
  '- For HTTP calls to API: use net/http with timeout; consider implementing retry middleware',
  '',
  'VERIFICATION before reporting done:',
  '- cd apps/worker && go build ./... → exit 0',
  '- cd apps/worker && go vet ./... → clean',
  '- cd apps/worker && go test -count=1 ./internal/certificate/... → all pass (existing 19 tests must still pass)',
  '- cd apps/api && go build ./... → exit 0',
  '- cd apps/api && go test ./internal/certificate/... → all pass',
  '- cd packages/contracts && forge fmt --check → exit 0',
  '- cd packages/contracts && forge test → 97+ pass',
  '',
  'DELIVER REPORT same format as Stream A. Note any TODOs.',
  '',
  'Do not commit. Do not push.'
].join('\n');

const implementResult = await parallel([
  () => agent(streamA_prompt, { label: 'Stream A: completion + progress API', phase: 'Implement', agentType: 'm-backend-engineer' }),
  () => agent(streamB_prompt, { label: 'Stream B: metadata + mint consumer + integration', phase: 'Implement', agentType: 'm-backend-engineer' }),
]);

// =====================================================================
//  PHASE 2 — VERIFY & TEST
// =====================================================================
phase('Verify & Test');

const verifyPrompt = [
  'You are the QA / build verifier. Read the two implementation reports.',
  '',
  'Your job:',
  '1. cd packages/contracts && forge fmt --check ; must exit 0',
  '2. cd packages/contracts && forge build ; must exit 0 (warnings OK)',
  '3. cd packages/contracts && forge test ; must be all green',
  '4. cd apps/api && go build ./... ; must exit 0',
  '5. cd apps/api && go vet ./... ; must be clean',
  '6. cd apps/api && go test ./internal/certificate/... ./internal/learning/... ; report total / passed / failed',
  '7. cd apps/worker && go build ./... ; must exit 0',
  '8. cd apps/worker && go vet ./... ; must be clean',
  '9. cd apps/worker && go test ./internal/certificate/... ; report total / passed / failed',
  '10. cd apps/web && pnpm exec tsc --noEmit ; must be 0',
  '',
  'If ANY step fails:',
  '- Read the actual error output',
  '- Decide: implementation side effect or environmental issue',
  '- For side effects: send back to the relevant stream with precise error',
  '- For environmental issues: fix in-place if single-line config — DO NOT modify implementation logic',
  '',
  'Report format:',
  '{',
  '  ok: boolean,',
  '  forge_fmt: "pass" | "fail",',
  '  forge_test: { total, passed, failed },',
  '  api_build: "pass" | "fail",',
  '  api_vet: "pass" | "fail",',
  '  api_test: { total, passed, failed },',
  '  worker_build: "pass" | "fail",',
  '  worker_vet: "pass" | "fail",',
  '  worker_test: { total, passed, failed },',
  '  web_typecheck: "pass" | "fail",',
  '  fixes_applied: [strings]',
  '}'
].join('\n');

const verifyResult = await agent(verifyPrompt, { label: 'Verify build + tests', phase: 'Verify & Test', agentType: 'general-purpose' });

if (!verifyResult || !verifyResult.ok) {
  return {
    status: 'verify_failed',
    verify: verifyResult,
    implementResult,
    note: 'Pipeline halted at Verify & Test.'
  };
}

// =====================================================================
//  PHASE 3 — CODEX REVIEW
// =====================================================================
phase('Codex Review');

const codexPrompt = [
  'You are a senior Go + Web3 backend reviewer. Review the Phase 5 changes.',
  '',
  'SCOPE:',
  '- apps/api/internal/certificate/completion.go + metadata.go',
  '- apps/api/internal/learning/progress.go + queries.go',
  '- apps/worker/internal/certificate/consumer.go',
  '- database/migrations/0008_certificates.up.sql + 0009_cert_jobs_ext.up.sql',
  '- test files for both',
  '',
  'READ: .claude/rules/security.md, coding-style.md, smart-contract.md first.',
  'ALSO READ: apps/worker/internal/certificate/signer.go (existing)',
  '',
  'REVIEW FOR:',
  '- Critical: race conditions on certificate_jobs unique constraint; metadata injection (XSS / JSON escape); URI scheme injection (javascript: / data: / file:); signature replay (job rerun signs twice); receipt hash mismatch; KMS key swap; signer address not asserted against expected MINTER_ROLE',
  '- Important: idempotency correctness; retry backoff exponential bounds; DLQ max-attempts threshold; audit log on completion; non-decreasing progress rule edge cases; completion rule (100% of REQUIRED lessons only); SQL injection in dynamic queries',
  '- Nice-to-have: test coverage; doc completeness; error message clarity',
  '',
  'DO NOT WRITE FILES. Limit to top 15 findings.',
  '',
  'REPORT FORMAT:',
  '## Critical (must fix before merge)',
  '## Important (should fix soon)',
  '## Nice-to-have',
  '## What looks good',
  '## Summary — shippable? top 3 priorities if not.'
].join('\n');

const codexResult = await agent(codexPrompt, { label: 'Codex review Phase 5', phase: 'Codex Review', agentType: 'general-purpose' });

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
    note: 'Phase 5 complete: no Critical or Important findings.'
  };
}

const fixResult = await pipeline(
  findings,
  f => agent(
    'Apply finding from Phase 5 codex:\n\n' +
    f.summary + '\nFile: ' + f.file + '\nLine: ' + f.line + '\nFix: ' + f.fix + '\n\n' +
    'Read file, apply minimum-scope fix, run affected tests, report.',
    { label: 'Fix: ' + (f.summary || '').slice(0, 50), phase: 'Fix Findings', agentType: 'general-purpose' }
  ),
  fix => agent(
    'Verify fix for: ' + (fix.summary || 'unknown') + '\n' +
    'cd packages/contracts && forge fmt --check && forge test\n' +
    'cd apps/api && go build ./... && go test ./internal/certificate/... ./internal/learning/...\n' +
    'cd apps/worker && go build ./... && go test ./internal/certificate/...\n' +
    'Report pass/fail per step.',
    { label: 'Verify fix', phase: 'Fix Findings', agentType: 'general-purpose' }
  )
);

return {
  status: 'fixed',
  implementResult,
  verify: verifyResult,
  codex: codexResult,
  fixes: fixResult,
  note: 'Phase 5 complete: all Critical + Important findings fixed and verified.'
};