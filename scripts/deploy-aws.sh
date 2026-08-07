#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
config_file="${DEPLOY_CONFIG_FILE:-${repo_dir}/deploy/config.env}"
if [[ -f "${config_file}" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "${config_file}"
  set +a
fi

STACK_NAME="${STACK_NAME:-x-web3-web}"
AWS_REGION="${AWS_REGION:-us-east-1}"
DOMAIN_NAME="${DOMAIN_NAME:-}"
ACM_CERTIFICATE_ARN="${ACM_CERTIFICATE_ARN:-}"
PRICE_CLASS="${PRICE_CLASS:-PriceClass_100}"

template_file="${repo_dir}/infra/aws/static-site.yaml"

command -v aws >/dev/null || { echo "aws CLI is required" >&2; exit 1; }
command -v pnpm >/dev/null || { echo "pnpm is required" >&2; exit 1; }

aws sts get-caller-identity --region "${AWS_REGION}" >/dev/null

parameters=(
  "DomainName=${DOMAIN_NAME}"
  "AcmCertificateArn=${ACM_CERTIFICATE_ARN}"
  "PriceClass=${PRICE_CLASS}"
)

aws cloudformation deploy \
  --stack-name "${STACK_NAME}" \
  --template-file "${template_file}" \
  --region "${AWS_REGION}" \
  --parameter-overrides "${parameters[@]}" \
  --no-fail-on-empty-changeset

bucket_name="$(aws cloudformation describe-stacks --stack-name "${STACK_NAME}" --region "${AWS_REGION}" --query "Stacks[0].Outputs[?OutputKey=='BucketName'].OutputValue" --output text)"
distribution_id="$(aws cloudformation describe-stacks --stack-name "${STACK_NAME}" --region "${AWS_REGION}" --query "Stacks[0].Outputs[?OutputKey=='DistributionId'].OutputValue" --output text)"
site_url="$(aws cloudformation describe-stacks --stack-name "${STACK_NAME}" --region "${AWS_REGION}" --query "Stacks[0].Outputs[?OutputKey=='SiteUrl'].OutputValue" --output text)"

cd "${repo_dir}"
pnpm install --frozen-lockfile
pnpm build

aws s3 sync apps/web/dist "s3://${bucket_name}" \
  --region "${AWS_REGION}" \
  --delete \
  --exclude "index.html" \
  --cache-control "public,max-age=31536000,immutable"

aws s3 cp apps/web/dist/index.html "s3://${bucket_name}/index.html" \
  --region "${AWS_REGION}" \
  --cache-control "no-cache,no-store,must-revalidate" \
  --content-type "text/html"

aws cloudfront create-invalidation --distribution-id "${distribution_id}" --paths "/*" >/dev/null
echo "Deployment complete: ${site_url}"
