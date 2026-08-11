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
db_password="$(openssl rand -hex 24)"
runtime_json="$(node -e 'const [dbp,session,privy,jwks,rpc,origin]=process.argv.slice(1); process.stdout.write(JSON.stringify({POSTGRES_PASSWORD:dbp,DATABASE_URL:`postgres://xweb3:${dbp}@127.0.0.1:5432/xweb3?sslmode=disable`,REDIS_URL:"redis://127.0.0.1:6379/0",API_ENV:"prod",API_PORT:"8080",API_BASE_URL:`${origin}/api/v1`,WEB_ORIGIN:origin,PRIVY_APP_ID:privy,PRIVY_JWKS_URL:jwks,PRIVY_AUDIENCE:privy,PRIVY_DEV_STUB:"0",SESSION_SECRET:session,SESSION_TTL_HOURS:"168",SESSION_COOKIE_SECURE:"true",API_DOMAIN:new URL(origin).host,SEPOLIA_RPC_URL:rpc,CHAIN_ID:"11155111",WORKER_CHAIN_ID:"11155111",CHAIN_CONFIRMATION_DEPTH:"12",WORKER_METRICS_ADDR:"127.0.0.1:9090"}))' "$db_password" "$SESSION_SECRET" "$PRIVY_APP_ID" "$PRIVY_JWKS_URL" "${VITE_SEPOLIA_RPC_URL}" "https://${DOMAIN_NAME}")"
secret_arn="$(aws secretsmanager describe-secret --secret-id "$secret_name" --region "$region" --query ARN --output text 2>/dev/null || true)"
if [[ -n "$secret_arn" ]]; then
  aws secretsmanager put-secret-value --secret-id "$secret_arn" --secret-string "$runtime_json" --region "$region" >/dev/null
else
  secret_arn="$(aws secretsmanager create-secret --name "$secret_name" --secret-string "$runtime_json" --region "$region" --query ARN --output text)"
fi

aws cloudformation deploy --stack-name "$stack" --template-file "${repo_dir}/infra/aws/backend.yaml" --region "$region" \
  --capabilities CAPABILITY_IAM --parameter-overrides ArtifactBucket="$artifact_bucket" ApiArtifactKey="$api_key" \
  WorkerArtifactKey="$worker_key" MigrationsArtifactKey="$migrations_key" RuntimeSecretArn="$secret_arn"
aws cloudformation describe-stacks --stack-name "$stack" --region "$region" --query 'Stacks[0].Outputs' --output json
