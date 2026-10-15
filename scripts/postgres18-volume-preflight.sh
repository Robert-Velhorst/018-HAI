#!/bin/sh
set -eu

# PostgreSQL 18 moved PGDATA below the volume root. When upgrading a volume
# that still contains the old `/var/lib/postgresql/data` cluster, refuse to
# start an empty 18 cluster until an operator has restored and marked it.
legacy_pgdata=/var/lib/postgresql/data
target_pgdata=${PGDATA:-/var/lib/postgresql/18/docker}

if [ -f "$legacy_pgdata/PG_VERSION" ]; then
  target_version=''
  if [ -f "$target_pgdata/PG_VERSION" ]; then
    target_version=$(cat "$target_pgdata/PG_VERSION")
  fi

  if [ "$target_version" != 18 ] || [ ! -f "$target_pgdata/.hai-postgres18-migration-complete" ]; then
    if [ "$target_version" != '' ] || [ "${HAI_POSTGRES18_ALLOW_EMPTY_TARGET:-0}" != 1 ]; then
      echo "Legacy PostgreSQL data is present at $legacy_pgdata. Restore and verify it in PostgreSQL 18, then create $target_pgdata/.hai-postgres18-migration-complete before starting applications." >&2
      exit 78
    fi
  fi
fi

exec /usr/local/bin/docker-entrypoint.sh "$@"
