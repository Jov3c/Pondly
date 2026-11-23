#!/usr/bin/env bash
set -euo pipefail

if [[ "${PONDLY_RESTORE_CONFIRM:-}" != "yes" ]]; then
  echo "refusing restore: set PONDLY_RESTORE_CONFIRM=yes and target an isolated staging database" >&2
  exit 1
fi
: "${PONDLY_RESTORE_DATABASE_URL:?set PONDLY_RESTORE_DATABASE_URL}"
dump="${1:?usage: restore.sh <postgresql custom dump>}"
: "${dump:?dump path required}"

if [[ ! -f "$dump.sha256" ]]; then
  echo "refusing restore: missing checksum for $dump" >&2
  exit 1
fi
read -r expected _ < "$dump.sha256"
actual="$(sha256sum < "$dump")"
if [[ ! "$expected" =~ ^[0-9a-f]{64}$ || "${actual%% *}" != "$expected" ]]; then
  echo "refusing restore: checksum mismatch for $dump" >&2
  exit 1
fi
pg_restore --single-transaction --exit-on-error --clean --if-exists --no-owner --dbname="$PONDLY_RESTORE_DATABASE_URL" "$dump"
echo "restore complete: $dump"
