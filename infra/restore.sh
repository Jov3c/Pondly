#!/usr/bin/env bash
set -euo pipefail

if [[ "${PONDLY_RESTORE_CONFIRM:-}" != "yes" ]]; then
  echo "refusing restore: set PONDLY_RESTORE_CONFIRM=yes and target an isolated staging database" >&2
  exit 1
fi
: "${PONDLY_RESTORE_DATABASE_URL:?set PONDLY_RESTORE_DATABASE_URL}"
dump="${1:?usage: restore.sh <postgresql custom dump>}"
: "${dump:?dump path required}"

if [[ -f "$dump.sha256" ]]; then
  sha256sum --check "$dump.sha256"
fi
pg_restore --clean --if-exists --no-owner --dbname="$PONDLY_RESTORE_DATABASE_URL" "$dump"
echo "restore complete: $dump"
