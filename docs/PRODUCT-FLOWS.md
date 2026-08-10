# Web3 University product flows

## Route map

| Route | Audience | Responsibility | Main integrations |
| --- | --- | --- | --- |
| `/` | Public | Product entry and learning journey | None on initial render |
| `/courses` | Public | Search and paginate published courses | `GET /courses` |
| `/courses/:courseId` | Public / student | Curriculum, enrollment state, checkout, comments | `GET /courses/:id`, comments, purchase intent, transaction submission |
| `/swap` | Wallet user | Quote and execute YD/USDC swap | Sepolia RPC / Uniswap contracts |
| `/account/enrollments` | Student | Resume active courses | `GET /me/enrollments` |
| `/account/orders` | Student | Inspect onchain purchase status | `GET /me/orders` |
| `/account/certificates` | Student | View completion credentials | enrollment/certificate APIs |
| `/account/comments` | Student | Manage personal reviews | `GET /me/comments`, delete comment |
| `/learn/:courseId?lesson=:lessonId` | Enrolled student | Select lesson, request playback, report progress, complete course | course detail, playback, progress, completion APIs |
| `/studio` | Teacher | Create curriculum, upload media, submit review | `/teacher/*` |
| `/admin/users` | System admin | User and role operations | `/admin/users*` |
| `/admin/chain` | System admin | Indexer health | `GET /admin/chain/sync` |
| `/admin/dlq` | System admin | Inspect and resolve failed events | `GET /admin/dlq`, `POST /admin/dlq/:id/retry` |

## Core interaction sequence

1. A visitor searches `/courses`; only the catalog API is loaded.
2. Selecting a course opens a shareable course route rather than a modal.
3. The detail API optionally resolves the session. Anonymous visitors receive public data; authenticated users also receive the authoritative `enrolled` flag.
4. A signed-in user with a bound Sepolia wallet creates a frozen purchase intent, submits the contract transaction, and reports its hash to the API.
5. The worker confirms the event and creates the enrollment idempotently.
6. The student resumes from `/account/enrollments`, selects a lesson under `/learn/:courseId`, obtains a short-lived playback credential, and reports monotonic progress.
7. Completion creates one certificate job; the credential becomes visible under `/account/certificates`.

## Frontend/backend boundary

- Cookie session and RBAC are authoritative on the Go API; route guards are user-experience controls only.
- HTTP requests go through `ApiClient` for credentials, request IDs, and error envelopes.
- Direct contract operations remain in wagmi feature hooks; the API verifies purchase effects from indexed chain events.
- Public course detail uses optional session middleware so the same route supports anonymous discovery and personalized enrollment state.
