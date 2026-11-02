#!/usr/bin/env bash
set -euo pipefail

: "${PONDLY_DATABASE_URL:?set PONDLY_DATABASE_URL}"
backup_dir="${PONDLY_BACKUP_DIR:-./backups}"
retention_days="${PONDLY_BACKUP_RETENTION_DAYS:-7}"

mkdir -p "$backup_dir"
chmod 700 "$backup_dir"
umask 077

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
file="$backup_dir/pondly-$stamp.dump"
pg_dump --format=custom --no-owner --file="$file" "$PONDLY_DATABASE_URL"
sha256sum "$file" > "$file.sha256"
find "$backup_dir" -type f -name 'pondly-*.dump*' -mtime +"$retention_days" -delete
echo "backup written: $file"
