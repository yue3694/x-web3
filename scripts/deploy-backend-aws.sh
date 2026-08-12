#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
config_file="${DEPLOY_CONFIG_FILE:-${repo_dir}/deploy/config.env}"
set -a
source "${config_file}"
source "${repo_dir}/.env"
set +a

region="${AWS_REGION:-us-east-1}"
stack="${BACKEND_STACK_NAME:-x-web3-backend}"
artifact_bucket="$(aws cloudformation describe-stacks --stack-name "${STACK_NAME:-x-web3-web}" --region "$region" --query "Stacks[0].Outputs[?OutputKey=='BucketName'].OutputValue" --output text)"
release="$(date -u +%Y%m%d%H%M%S)"
api_key="backend/${release}/api"
worker_key="backend/${release}/worker"
migrations_key="backend/${release}/migrations.tar.gz"

mkdir -p "${repo_dir}/.deploy"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "${repo_dir}/.deploy/api" ./apps/api/cmd/api
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "${repo_dir}/.deploy/worker" ./apps/worker/cmd/worker
tar -czf "${repo_dir}/.deploy/migrations.tar.gz" -C "${repo_dir}/database/migrations" .
aws s3 cp "${repo_dir}/.deploy/api" "s3://${artifact_bucket}/${api_key}" --region "$region"
aws s3 cp "${repo_dir}/.deploy/worker" "s3://${artifact_bucket}/${worker_key}" --region "$region"
aws s3 cp "${repo_dir}/.deploy/migrations.tar.gz" "s3://${artifact_bucket}/${migrations_key}" --region "$region"

secret_name="${stack}/runtime"
secret_arn="$(aws secretsmanager describe-secret --secret-id "$secret_name" --region "$region" --query ARN --output text 2>/dev/null || true)"
existing_runtime=""
if [[ -n "$secret_arn" ]]; then
  existing_runtime="$(aws secretsmanager get-secret-value --secret-id "$secret_arn" --region "$region" --query SecretString --output text)"
fi
db_password="$(jq -r '.POSTGRES_PASSWORD // empty' <<<"$existing_runtime" 2>/dev/null || true)"
if [[ -z "$db_password" ]]; then
  db_password="$(openssl rand -hex 24)"
fi
if [[ -n "$existing_runtime" ]]; then
  existing_session_secret="$(jq -r '.SESSION_SECRET // empty' <<<"$existing_runtime")"
  if [[ -n "$existing_session_secret" ]]; then
    SESSION_SECRET="$existing_session_secret"
  fi
fi
runtime_json="$(node -e 'const [dbp,session,privy,jwks,rpc,origin]=process.argv.slice(1); process.stdout.write(JSON.stringify({POSTGRES_PASSWORD:dbp,DATABASE_URL:`postgres://xweb3:${dbp}@127.0.0.1:5432/xweb3?sslmode=disable`,REDIS_URL:"redis://127.0.0.1:6379/0",API_ENV:"prod",API_PORT:"8080",API_BASE_URL:`${origin}/api/v1`,WEB_ORIGIN:origin,PRIVY_APP_ID:privy,PRIVY_JWKS_URL:jwks,PRIVY_AUDIENCE:privy,PRIVY_DEV_STUB:"0",SESSION_SECRET:session,SESSION_TTL_HOURS:"168",SESSION_COOKIE_SECURE:"true",API_DOMAIN:new URL(origin).host,SEPOLIA_RPC_URL:rpc,CHAIN_ID:"11155111",WORKER_CHAIN_ID:"11155111",CHAIN_CONFIRMATION_DEPTH:"12",WORKER_METRICS_ADDR:"127.0.0.1:9090"}))' "$db_password" "$SESSION_SECRET" "$PRIVY_APP_ID" "$PRIVY_JWKS_URL" "${VITE_SEPOLIA_RPC_URL}" "https://${DOMAIN_NAME}")"
runtime_json="$(node -e 'const [raw,market,token,cert,rpc,oracle,sale]=process.argv.slice(1); const x=JSON.parse(raw); Object.assign(x,{COURSE_MARKET_ADDRESS:market,YD_TOKEN_ADDRESS:token,CERT_NFT_ADDRESS:cert,WORKER_MARKET_ADDRESSES:market,WORKER_RPC_URLS:rpc,PRICE_ORACLE_ADDRESS:oracle,YD_SALE_ADDRESS:sale,RECONCILE_ENABLED:"0",WORKER_BATCH_SIZE:"10"}); process.stdout.write(JSON.stringify(x))' "$runtime_json" "${VITE_COURSE_MARKET_ADDRESS:-}" "${VITE_YD_TOKEN_ADDRESS:-}" "${VITE_CERTIFICATE_NFT_ADDRESS:-}" "${VITE_SEPOLIA_RPC_URL}" "${VITE_PRICE_ORACLE_ADDRESS:-}" "${VITE_YD_SALE_ADDRESS:-}")"
if [[ -n "${CERTIFICATE_MINTER_PRIVATE_KEY:-}" ]]; then
  cert_minter_address="$(cast wallet address --private-key "${CERTIFICATE_MINTER_PRIVATE_KEY}")"
  runtime_json="$(CERT_KEY="${CERTIFICATE_MINTER_PRIVATE_KEY}" CERT_ADDR="${cert_minter_address}" node -e 'const x=JSON.parse(process.argv[1]); Object.assign(x,{SIGNER_DRIVER:"anvil",SIGNER_ANVIL_PRIVATE_KEY:process.env.CERT_KEY,CERT_MINTER_ADDRESS:process.env.CERT_ADDR}); process.stdout.write(JSON.stringify(x))' "$runtime_json")"
fi
if [[ -n "$secret_arn" ]]; then
  aws secretsmanager put-secret-value --secret-id "$secret_arn" --secret-string "$runtime_json" --region "$region" >/dev/null
else
  secret_arn="$(aws secretsmanager create-secret --name "$secret_name" --secret-string "$runtime_json" --region "$region" --query ARN --output text)"
fi

aws cloudformation deploy --stack-name "$stack" --template-file "${repo_dir}/infra/aws/backend.yaml" --region "$region" \
  --capabilities CAPABILITY_IAM --parameter-overrides ArtifactBucket="$artifact_bucket" ApiArtifactKey="$api_key" \
  WorkerArtifactKey="$worker_key" MigrationsArtifactKey="$migrations_key" RuntimeSecretArn="$secret_arn"
media_bucket="$(aws cloudformation describe-stacks --stack-name "$stack" --region "$region" --query "Stacks[0].Outputs[?OutputKey=='MediaBucketName'].OutputValue" --output text)"
runtime_json="$(jq -c --arg bucket "$media_bucket" --arg aws_region "$region" '. + {OBJECT_STORE_BUCKET:$bucket,AWS_REGION:$aws_region}' <<<"$runtime_json")"
aws secretsmanager put-secret-value --secret-id "$secret_arn" --secret-string "$runtime_json" --region "$region" >/dev/null
aws cloudformation describe-stacks --stack-name "$stack" --region "$region" --query 'Stacks[0].Outputs' --output json
