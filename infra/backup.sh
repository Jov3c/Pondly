#!/usr/bin/env bash
set -euo pipefail

: "${PONDLY_DATABASE_URL:?set PONDLY_DATABASE_URL}"
backup_dir="${PONDLY_BACKUP_DIR:-./backups}"
retention_days="${PONDLY_BACKUP_RETENTION_DAYS:-7}"

mkdir -p "$backup_dir"
chmod 700 "$backup_dir"
umask 077

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
pending="$(mktemp -d "$backup_dir/.pending-$stamp.XXXXXXXX")"
trap 'rm -f -- "$pending/data.dump" "$pending/data.sha256"; rmdir -- "$pending"' EXIT
name="pondly-$stamp-${pending##*.}.dump"
file="$backup_dir/$name"
pg_dump --format=custom --no-owner --file="$pending/data.dump" "$PONDLY_DATABASE_URL"
digest="$(sha256sum < "$pending/data.dump")"
printf '%s  %s\n' "${digest%% *}" "$name" > "$pending/data.sha256"
mv -- "$pending/data.dump" "$file"
mv -- "$pending/data.sha256" "$file.sha256"
find "$backup_dir" -type f -name 'pondly-*.dump*' -mtime +"$retention_days" -delete
echo "backup written: $file"
