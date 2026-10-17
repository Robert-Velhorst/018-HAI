# PostgreSQL 17 to 18 migration

This guide applies only to the PostgreSQL services in `docker-compose.local.yml`: the IDP database, automation database with pgvector, and optional Temporal database. The upgrade changes PostgreSQL's data directory. PostgreSQL 17 files remain in the existing named volumes under `/var/lib/postgresql/data`; PostgreSQL 18 uses `/var/lib/postgresql/18/docker`. Compose keeps the existing volume names, and the startup preflight plus health checks prevent application services from using an unverified PostgreSQL 18 cluster beside PostgreSQL 17 data.

This is an offline logical migration for the local Compose stack. It does not migrate a production or Hetzner database. Do not run `docker compose down --volumes` during this work.

## Before switching the Compose image

1. Stop writes to the local databases and take a recoverable backup. Create one custom-format dump per database while PostgreSQL 17 is still running:

   ```sh
   docker compose exec -T postgres-idp sh -ec 'pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > idp.pg17.dump
   docker compose exec -T postgres-automation sh -ec 'pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > automation.pg17.dump
   docker compose --profile durability exec -T temporal-postgres sh -ec 'pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > temporal.pg17.dump
   ```

   Run the Temporal command only if that service is enabled. Confirm each dump is non-empty, verify a second copy, and store both copies outside the repository. Treat the dumps as sensitive. Retain the existing recovery bundle.
2. Stop the application services and PostgreSQL 17 containers. Keep all named volumes. Switch to the reviewed PostgreSQL 18 Compose configuration.

## Initialize and restore PostgreSQL 18

For each existing volume that contains a PostgreSQL 17 `PG_VERSION`, start only that database once with `HAI_POSTGRES18_ALLOW_EMPTY_TARGET=1`. The one-time setting lets PostgreSQL 18 initialize its new data directory beside the untouched PostgreSQL 17 files. In PowerShell, set it only for this database-only startup, then remove it:

```powershell
$env:HAI_POSTGRES18_ALLOW_EMPTY_TARGET = '1'
docker compose up -d postgres-idp postgres-automation
Remove-Item Env:HAI_POSTGRES18_ALLOW_EMPTY_TARGET
```

Include `--profile durability` and `temporal-postgres` only if Temporal was enabled. Do not start application services during initialization. The databases may accept local `docker compose exec` commands while their health checks remain unhealthy; dependent applications stay blocked until each migration marker is created.

The new PostgreSQL cluster runs the configured `init.sql`, so recreate its initially seeded database before restoring the old dump. Run this separately for each database:

```sh
docker compose exec -T postgres-idp sh -ec 'dropdb --if-exists -U "$POSTGRES_USER" "$POSTGRES_DB"; createdb -U "$POSTGRES_USER" -T template0 "$POSTGRES_DB"'
docker compose exec -T postgres-automation sh -ec 'dropdb --if-exists -U "$POSTGRES_USER" "$POSTGRES_DB"; createdb -U "$POSTGRES_USER" -T template0 "$POSTGRES_DB"'
docker compose --profile durability exec -T temporal-postgres sh -ec 'dropdb --if-exists -U "$POSTGRES_USER" "$POSTGRES_DB"; createdb -U "$POSTGRES_USER" -T template0 "$POSTGRES_DB"'
```

Skip the Temporal command when it was not enabled. Restore each dump into its matching empty database, stopping at the first error:

```sh
docker compose exec -T postgres-idp sh -ec 'pg_restore --exit-on-error --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < idp.pg17.dump
docker compose exec -T postgres-automation sh -ec 'pg_restore --exit-on-error --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < automation.pg17.dump
docker compose --profile durability exec -T temporal-postgres sh -ec 'pg_restore --exit-on-error --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < temporal.pg17.dump
```

Skip the Temporal restore when it was not enabled. Verify application-specific row counts, representative records, required roles, and grants against the backups. For automation, confirm the `vector` extension version and run `ALTER EXTENSION vector UPDATE;` if an update is needed. Re-run the repository migrations and database-role provisioning before restoring application writers.

Only after each database passes its checks, create the completion marker as root in that container:

```sh
docker compose exec -T -u 0 postgres-idp sh -ec 'touch "$PGDATA/.hai-postgres18-migration-complete"'
docker compose exec -T -u 0 postgres-automation sh -ec 'touch "$PGDATA/.hai-postgres18-migration-complete"'
docker compose --profile durability exec -T -u 0 temporal-postgres sh -ec 'touch "$PGDATA/.hai-postgres18-migration-complete"'
```

Skip the Temporal command when it was not enabled. The marker records that the operator completed restore and verification; it is not a substitute for either. After all applicable markers exist, confirm the databases become healthy, then start the application services and verify readiness and authentication paths.

Keep the dumps and original PostgreSQL 17 data directories until the upgraded applications have been accepted and a fresh PostgreSQL 18 recovery bundle has been verified. The old cluster files remain in their named volumes; do not delete them as cleanup.

## Recovery

If restore or verification fails, stop the PostgreSQL 18 and application containers. Remove no volumes. Preserve the dumps and logs, then use a separate PostgreSQL 17 Compose checkout against the unchanged volume or restore the verified recovery bundle. Do not point PostgreSQL 17 at the PostgreSQL 18 data directory.

## References

- [PostgreSQL versioning policy and supported releases](https://www.postgresql.org/support/versioning/)
- [PostgreSQL 18 release notes](https://www.postgresql.org/docs/18/release.html)
- [Official PostgreSQL Docker image: data directory change in 18+](https://github.com/docker-library/docs/blob/master/postgres/README.md#pgdata)
- [pgvector supported Docker tags](https://github.com/pgvector/pgvector#docker)
