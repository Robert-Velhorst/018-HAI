# PostgreSQL 18 database upgrade gate

The local Compose stack now targets PostgreSQL 18.6, pgvector 0.8.7 on PostgreSQL 18, and Redis 8.10.2. The image tags exist in the upstream registries as checked on 2026-10-15.

## Persistent PostgreSQL data

Three services use durable named volumes:

| Service | Existing volume |
| --- | --- |
| `postgres-idp` | `018-hai-postgres-idp-data` |
| `postgres-automation` | `018-hai-postgres-automation-data` |
| `temporal-postgres` (durability profile) | `018-hai-temporal-postgres-data` |

PostgreSQL 17 stored cluster files at the root of the mounted data directory. The official PostgreSQL 18 images use `/var/lib/postgresql/18/docker` and mount `/var/lib/postgresql`. The Compose mappings now use the PostgreSQL 18 mount location. A PostgreSQL 17 cluster cannot be opened by PostgreSQL 18; changing the image tag alone is not a migration. The image entrypoint detects older cluster files and refuses to start them.

Before applying the PostgreSQL 18 Compose file to a host with existing volumes:

1. Stop application writers and take a separate, verified full-cluster backup from each PostgreSQL 17 service. Store backups outside the Compose volumes in approved encrypted storage. The dump must include roles, databases, schema, and data.
2. Restore each dump into a fresh PostgreSQL 18 cluster and a new volume. Keep the original PostgreSQL 17 named volumes intact and offline for rollback.
3. Restore the automation database with the PostgreSQL 18 pgvector image so the `vector` extension is available. Check extension state, schema migrations, role grants, database names, and application connectivity before starting writers.
4. Keep the old volumes and verified backups until the new deployment has passed the agreed observation period. Do not run `docker compose down -v` or remove the old volumes as part of this upgrade.

The three volumes are independent clusters and must be upgraded separately. `redis` has no persistent volume in this Compose file; its cache/session state is ephemeral and is recreated by the service.

This repository change updates the target images and test fixtures only. It does not copy, migrate, or delete any existing database data, and it does not change a running deployment.

## Sources

- [PostgreSQL 18.6 release](https://www.postgresql.org/about/news/postgresql-186-1711-1615-1519-1424-and-19-beta-3-released-3365/)
- [PostgreSQL versioning and supported branches](https://www.postgresql.org/support/versioning/)
- [Official PostgreSQL Docker image: data directory change in 18+](https://github.com/docker-library/docs/blob/master/postgres/README.md#pgdata)
- [pgvector changelog](https://github.com/pgvector/pgvector/blob/master/CHANGELOG.md)
- [Redis release index](https://download.redis.io/releases/)
