# AWS + Cloudflare deployment

This project is a static Vite dApp. The production path is:

```text
Browser -> Cloudflare DNS -> CloudFront -> private S3 bucket
                         \-> Sepolia RPC -> Notepad contract
```

Cloudflare is deliberately **DNS only** for the application record. CloudFront
already provides CDN, TLS, compression, HTTP/3, caching, and security headers;
proxying the record through Cloudflare would create two cache layers and make
invalidations and incident diagnosis less predictable.

## What gets deployed

- A private, encrypted, versioned S3 bucket. Public access is fully blocked.
- A CloudFront distribution with Origin Access Control (OAC), HTTPS redirect,
  compression, SPA fallback, and security headers.
- A bucket policy allowing reads only from that CloudFront distribution.
- A Cloudflare CNAME record pointing the chosen hostname to CloudFront.

The smart contracts are not hosted on AWS. They remain on Sepolia; the browser
talks to them using `VITE_SEPOLIA_RPC_URL`.

Deployment settings live in `deploy/config.env`. This file is intentionally
git-ignored. Copy `deploy/config.env.example` when preparing another machine;
never commit the real file because it can contain the Cloudflare API token.

## Prerequisites

- Node.js 20+, pnpm 10, AWS CLI v2, and `curl`.
- An AWS identity with CloudFormation, S3, and CloudFront permissions.
- For a custom hostname, a domain managed by Cloudflare and an ACM certificate
  in **us-east-1**. CloudFront only accepts ACM certificates from that region.
- A Cloudflare API token limited to `Zone / DNS / Edit` for the target zone.

Check the AWS identity before making changes:

```bash
aws sts get-caller-identity
```

## 1. Prepare frontend configuration

```bash
cp apps/web/.env.example apps/web/.env.production
```

Set these public build-time values:

```dotenv
VITE_SEPOLIA_RPC_URL=https://eth-sepolia.example-rpc.com/v2/PUBLIC_KEY
VITE_WALLETCONNECT_PROJECT_ID=your-public-project-id
VITE_APP_URL=https://app.example.com
VITE_COUNTER_CONTRACT_ADDRESS=
VITE_NOTEPAD_CONTRACT_ADDRESS=
```

Do not put secrets in any `VITE_*` variable. Vite embeds them in browser code.

Before publishing the functional dApp, deploy the contracts and set their
addresses in `apps/web/.env.production`:

```bash
pnpm contracts:test
pnpm contracts:deploy:notepad:sepolia
pnpm contracts:export:abi
```

Use a throwaway funded Sepolia deployer wallet, never a treasury key.

## 2. First AWS deployment without a custom domain

The repository includes `infra/aws/static-site.yaml` and an idempotent deploy
script. Edit the ignored `deploy/config.env`, then run:

```bash
pnpm deploy:aws
```

The script deploys/updates the stack, builds the app, uploads hashed assets with
a one-year immutable cache, uploads `index.html` with no-cache headers, and
creates a CloudFront invalidation. It prints the public URL when done.

Inspect stack outputs at any time:

```bash
aws cloudformation describe-stacks \
  --stack-name x-web3-web \
  --region us-east-1 \
  --query 'Stacks[0].Outputs'
```

## 3. Add a custom hostname

### 3.1 Request and validate the certificate

In ACM **us-east-1**, request a public certificate for the exact hostname (for
example `app.example.com`). Add the ACM validation CNAME shown by AWS to
Cloudflare with proxy status set to DNS only, then wait for status `ISSUED`.

The certificate validation record must remain in DNS so renewal stays automatic.

### 3.2 Attach the hostname to CloudFront

Run the deployment again with both custom-domain parameters:

```bash
pnpm deploy:aws
```

`DOMAIN_NAME` and `ACM_CERTIFICATE_ARN` are an all-or-nothing pair. Omitting
either keeps the CloudFront default certificate and hostname.

### 3.3 Create or update Cloudflare DNS

Set `ACM_CERTIFICATE_ARN`, `CF_ZONE_ID`, `CF_API_TOKEN`, and the distribution
hostname in the ignored configuration file, then run:

```bash
bash scripts/configure-cloudflare-dns.sh
```

The script creates or updates a DNS-only CNAME. Cloudflare supports CNAME
flattening for an apex hostname, but a subdomain such as `app.example.com` is
usually clearer operationally.

Verify DNS and HTTPS:

```bash
dig +short app.example.com
curl -I https://app.example.com
```

## Routine releases

After source or production environment changes, run the same deployment command:

```bash
pnpm deploy:aws
```

CloudFormation is idempotent; an unchanged infrastructure template is not an
error. The asset upload and invalidation still run.

## Automatic releases from GitHub

`.github/workflows/deploy.yml` runs after a commit reaches `main` (including a
merged pull request) and can also be started manually. It uses GitHub's OIDC
identity to assume a short-lived, least-privilege AWS role; no AWS access key is
stored in GitHub.

The bootstrap role lives in `infra/aws/github-actions-role.yaml`. Repository
variables used by the workflow are:

| Variable | Purpose |
| --- | --- |
| `AWS_ROLE_ARN` | OIDC deployment role ARN |
| `AWS_REGION` | Deployment region, currently `us-east-1` |
| `S3_BUCKET` | Site bucket stack output |
| `CLOUDFRONT_DISTRIBUTION_ID` | Distribution stack output |
| `VITE_SEPOLIA_RPC_URL` | Optional public Sepolia RPC URL |
| `VITE_WALLETCONNECT_PROJECT_ID` | Optional WalletConnect public project ID |
| `VITE_COUNTER_CONTRACT_ADDRESS` | Optional deployed Counter address |
| `VITE_NOTEPAD_CONTRACT_ADDRESS` | Optional deployed Notepad address |

Contract variables can remain empty until deployment. Updating a variable does
not itself trigger a workflow; start `Deploy web` manually afterward or merge a
new change to `main`.

## Rollback and removal

- Frontend rollback: restore the desired Git revision and run `pnpm deploy:aws`.
  S3 versioning also preserves old objects, but a Git-based rollback is easier to
  audit and restores the complete release consistently.
- DNS rollback: change the Cloudflare record back to its previous target.
- Stack deletion does **not** delete the S3 bucket because its deletion and
  replacement policies are `Retain`. This prevents accidental loss. Empty and
  delete the retained bucket separately only after confirming it is no longer
  needed.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| CloudFront returns an old release | Wait for the invalidation, then inspect browser cache. |
| `AccessDenied` from S3 | Confirm the bucket policy source ARN matches the current distribution. |
| Custom domain TLS error | Certificate must be `ISSUED`, in us-east-1, and cover the exact hostname. |
| CloudFormation alias error | Create the Cloudflare/ACM validation record first; then attach the issued certificate. |
| Wallet UI loads but contract actions do not | Set deployed addresses in `deployments.ts` and confirm Sepolia RPC access. |
| Cloudflare record resolves but site loops/fails | Keep the record DNS only; do not add a second proxy/cache layer. |

## Security and cost notes

- S3 has no public website endpoint and cannot be read directly.
- Cloudflare tokens and deployer private keys must stay outside Git.
- `PriceClass_100` is the default to limit CloudFront edge locations and cost.
- CloudFront and S3 are usage-priced. Add an AWS Budget before production traffic.
- Source maps are currently enabled by Vite. Disable `build.sourcemap` for a
  private production codebase or publish maps only to an error-tracking service.
