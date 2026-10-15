# PostgreSQL 18 database upgrade gate

The local Compose stack now targets PostgreSQL 18.6, pgvector 0.8.7 on PostgreSQL 18, and Redis 8.10.2. The image tags exist in the upstream registries as checked on 2026-10-15.

## Persistent PostgreSQL data

Three services use durable named volumes. The updated Compose file assigns new physical volume names ending in `-v18`; the PostgreSQL 17 volumes with the original names remain unmounted and available for rollback:

| Service | Existing PostgreSQL 17 volume | New PostgreSQL 18 volume |
| --- | --- | --- |
| `postgres-idp` | `018-hai-postgres-idp-data` | `018-hai-postgres-idp-data-v18` |
| `postgres-automation` | `018-hai-postgres-automation-data` | `018-hai-postgres-automation-data-v18` |
| `temporal-postgres` (durability profile) | `018-hai-temporal-postgres-data` | `018-hai-temporal-postgres-data-v18` |

PostgreSQL 17 stored cluster files at the root of the mounted data directory. The official PostgreSQL 18 images use `/var/lib/postgresql/18/docker` and mount `/var/lib/postgresql`. A PostgreSQL 17 cluster cannot be opened by PostgreSQL 18; changing the image tag alone is not a migration. The image entrypoint detects older cluster files and refuses to start them.

Before applying the PostgreSQL 18 Compose file to a host with existing volumes:

1. Stop application writers and take verified backups from each PostgreSQL 17 service. Preserve cluster roles and passwords with a `pg_dumpall --globals-only` export, and create a separate `pg_dump -Fc` archive for every database. Store these files outside the Compose volumes in approved encrypted storage; treat them as sensitive.
2. Start only the PostgreSQL 18 database services with application writers stopped. Restore each cluster into its new `-v18` volume. Initialize the new cluster with a temporary migration superuser whose name is not present in the source roles, then restore global roles and each database archive. This avoids collisions with the image's bootstrap role and database.
3. Restore the automation database with the PostgreSQL 18 pgvector image so the `vector` extension is available. Update the extension to 0.8.7, then check extension state, schema migrations, role grants, database names, and application connectivity before starting writers.
4. Keep the old volumes and verified backups until the new deployment has passed the agreed observation period. Do not run `docker compose down -v` or remove the old volumes as part of this upgrade.

The three volumes are independent clusters and must be upgraded separately. The versioned volume names are declared at the end of `docker-compose.local.yml`; do not switch app services to the new clusters until all restores and validation have succeeded. `redis` has no persistent volume in this Compose file; its cache/session state is ephemeral and is recreated by the service.

This repository change updates the target images and test fixtures only. It does not copy, migrate, or delete any existing database data, and it does not change a running deployment.

## Sources

- [PostgreSQL 18.6 release](https://www.postgresql.org/about/news/postgresql-186-1711-1615-1519-1424-and-19-beta-3-released-3365/)
- [PostgreSQL versioning and supported branches](https://www.postgresql.org/support/versioning/)
- [Official PostgreSQL Docker image: data directory change in 18+](https://github.com/docker-library/docs/blob/master/postgres/README.md#pgdata)
- [pgvector changelog](https://github.com/pgvector/pgvector/blob/master/CHANGELOG.md)
- [Redis release index](https://download.redis.io/releases/)
