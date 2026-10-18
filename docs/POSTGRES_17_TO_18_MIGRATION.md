# PostgreSQL 17 to 18 migration

The canonical instructions are in
[the PostgreSQL 18 upgrade guide](database-upgrade-to-postgresql-18.md).
Follow that guide for the IDP, automation/pgvector, and optional Temporal
databases. It covers preserved volumes, restore verification, migration markers,
application health gates, rollback, and backup retention.

The Windows backup command covers the IDP and automation databases and refuses
when the optional Temporal volume is present. If Temporal is enabled, take and
verify its separate logical dump before changing images; the canonical guide
includes the Temporal dump and restore commands.

This document preserves the earlier pull request's migration-guide link. It does
not establish that a deployed database was migrated or that a restore rehearsal
passed. Never run `docker compose down --volumes` during the migration.
