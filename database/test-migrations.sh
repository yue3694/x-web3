#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
provided_test_url="${DATABASE_URL_TEST:-}"
if [[ -f "${repo_root}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${repo_root}/.env"
  set +a
fi

test_url="${provided_test_url:-${DATABASE_URL_TEST:-}}"
if [[ -z "${test_url}" ]]; then
  echo "DATABASE_URL_TEST is required" >&2
  exit 1
fi
if [[ "${test_url}" == "${DATABASE_URL:-}" ]]; then
  echo "DATABASE_URL_TEST must not equal DATABASE_URL" >&2
  exit 1
fi

"${repo_root}/database/migrate.sh" --database "${test_url}" up
"${repo_root}/database/migrate.sh" --database "${test_url}" down
"${repo_root}/database/migrate.sh" --database "${test_url}" up
psql "${test_url}" -v ON_ERROR_STOP=1 -f "${repo_root}/database/seed/0001_roles.sql" >/dev/null

version="$("${repo_root}/database/migrate.sh" --database "${test_url}" version 2>&1)"
expected_version="$(find "${repo_root}/database/migrations" -name '*.up.sql' -exec basename {} \; | sed 's/_.*//' | sort -n | tail -1 | sed 's/^0*//')"
if [[ "${version}" != "${expected_version}" ]]; then
  echo "unexpected migration version: ${version}" >&2
  exit 1
fi
echo "migration smoke passed at version ${version}"
