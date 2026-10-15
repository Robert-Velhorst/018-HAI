# PostgreSQL 17 to 18 migration

This guide applies only to the PostgreSQL services in `docker-compose.local.yml`:
the IDP database, automation database with pgvector, and optional Temporal
database. The upgrade changes PostgreSQL's data directory. PostgreSQL 17 files
remain in the existing named volumes under `/var/lib/postgresql/data`; the
PostgreSQL 18 image uses `/var/lib/postgresql/18/docker`. The Compose volume
names stay the same, and the startup preflight blocks application use of a new,
empty cluster while an old cluster remains unconverted.

This is an offline logical migration. It does not migrate a production or
Hetzner database. Do not run `docker compose down --volumes` during this work.

## Before switching the Compose image

1. Stop writes to the local databases and take a recoverable backup. The
   repository's Windows backup command covers the IDP and automation
   databases, but intentionally refuses when the optional Temporal volume is
   present. If Temporal is enabled, also create and verify a separate logical
   dump for it before proceeding.
2. While the PostgreSQL 17 containers are still running, create one custom
   format dump per database and store the files outside the repository. For
   example:

   ```sh
   docker compose exec -T postgres-idp sh -ec 'pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > idp.pg17.dump
   docker compose exec -T postgres-automation sh -ec 'pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > automation.pg17.dump
   docker compose --profile durability exec -T temporal-postgres sh -ec 'pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > temporal.pg17.dump
   ```

   Run the Temporal command only when that service is enabled. Confirm each
   dump is non-empty and keep a second copy before changing images. Retain the
   existing recovery bundle as well.
3. Stop the application services and the PostgreSQL 17 containers. Keep the
   named volumes. Switch to the reviewed PostgreSQL 18 Compose configuration.

## Initialize and restore PostgreSQL 18

For each database whose old volume contains `PG_VERSION`, start only that
database once with `HAI_POSTGRES18_ALLOW_EMPTY_TARGET=1`. This one-time setting
allows PostgreSQL 18 to initialize its new directory beside the untouched
PostgreSQL 17 files. In PowerShell, set the variable only for the database
startup command, then remove it:

```powershell
$env:HAI_POSTGRES18_ALLOW_EMPTY_TARGET = '1'
docker compose up -d postgres-idp postgres-automation
Remove-Item Env:HAI_POSTGRES18_ALLOW_EMPTY_TARGET
```

Include `--profile durability` and `temporal-postgres` only if Temporal was
enabled. Do not start application services during this step. The preflight
refuses later starts until the restore marker has been added.

Restore each dump into its matching database, stopping at the first error:

```sh
docker compose exec -T postgres-idp sh -ec 'pg_restore --exit-on-error --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < idp.pg17.dump
docker compose exec -T postgres-automation sh -ec 'pg_restore --exit-on-error --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < automation.pg17.dump
docker compose --profile durability exec -T temporal-postgres sh -ec 'pg_restore --exit-on-error --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < temporal.pg17.dump
```

Skip Temporal when it was not enabled. Verify application-specific row counts,
required roles, and representative records against the backup before continuing.
For automation, check whether the `vector` extension exists and, if it does,
run `ALTER EXTENSION vector UPDATE;` in that database. After verification,
create the `$PGDATA/.hai-postgres18-migration-complete` marker as root in each
migrated database container. For example:

```sh
docker compose exec -T -u 0 postgres-idp sh -ec 'touch "$PGDATA/.hai-postgres18-migration-complete"'
docker compose exec -T -u 0 postgres-automation sh -ec 'touch "$PGDATA/.hai-postgres18-migration-complete"'
docker compose --profile durability exec -T -u 0 temporal-postgres sh -ec 'touch "$PGDATA/.hai-postgres18-migration-complete"'
```

Run the Temporal command only when enabled. The marker records that the
operator completed restore and verification; it is not a substitute for either.

After all applicable databases pass their checks and have markers, start the
application services and verify their readiness and authentication paths. Keep
the dumps and original data directories until the application has been
accepted and a fresh PostgreSQL 18 recovery bundle has been verified. The old
cluster files remain in the named volumes; do not remove them as cleanup.

## Recovery

If restore or verification fails, stop the PostgreSQL 18 and application
containers. Remove no volumes. Preserve the dumps and logs, then use a separate
PostgreSQL 17 Compose checkout against a copy of the saved volume or restore
the pre-upgrade recovery bundle. Do not point PostgreSQL 17 at the PostgreSQL
18 data directory.
