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

: "${CF_API_TOKEN:?Set CF_API_TOKEN to a scoped Cloudflare API token}"
: "${CF_ZONE_ID:?Set CF_ZONE_ID to the Cloudflare zone ID}"
: "${DOMAIN_NAME:?Set DOMAIN_NAME to the hostname to publish}"
: "${CLOUDFRONT_DOMAIN:?Set CLOUDFRONT_DOMAIN to the dxxxxx.cloudfront.net hostname}"

api="https://api.cloudflare.com/client/v4"
auth_header="Authorization: Bearer ${CF_API_TOKEN}"
record_name="${DOMAIN_NAME}"

lookup="$(curl --fail-with-body --silent --show-error \
  --get "${api}/zones/${CF_ZONE_ID}/dns_records" \
  -H "${auth_header}" -H "Content-Type: application/json" \
  --data-urlencode "type=CNAME" --data-urlencode "name=${record_name}")"

record_id="$(node -e 'const d=JSON.parse(process.argv[1]); if(!d.success) process.exit(2); process.stdout.write(d.result[0]?.id ?? "")' "${lookup}")"
payload="$(node -e 'process.stdout.write(JSON.stringify({type:"CNAME",name:process.argv[1],content:process.argv[2],ttl:1,proxied:false,comment:"Managed by x-web3 deployment"}))' "${record_name}" "${CLOUDFRONT_DOMAIN}")"

if [[ -n "${record_id}" ]]; then
  method="PUT"
  endpoint="${api}/zones/${CF_ZONE_ID}/dns_records/${record_id}"
else
  method="POST"
  endpoint="${api}/zones/${CF_ZONE_ID}/dns_records"
fi

response="$(curl --fail-with-body --silent --show-error -X "${method}" "${endpoint}" \
  -H "${auth_header}" -H "Content-Type: application/json" --data "${payload}")"
node -e 'const d=JSON.parse(process.argv[1]); if(!d.success){console.error(d.errors);process.exit(2)} console.log(`Cloudflare DNS ready: ${d.result.name} -> ${d.result.content} (DNS only)`) ' "${response}"
