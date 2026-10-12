#!/bin/sh
set -eu

for name in PGHOST PGUSER PGPASSWORD PGDATABASE HAI_RUNTIME_DB_USER HAI_RUNTIME_DB_PASSWORD; do
  eval "value=\${$name:-}"
  if [ -z "$value" ]; then
    echo "missing required runtime-role setting: $name" >&2
    exit 64
  fi
done

if ! printf '%s\n' "$PGUSER" | grep -Eq '^[A-Za-z_][A-Za-z0-9_]{0,62}$'; then
  echo "PGUSER must be a PostgreSQL identifier" >&2
  exit 64
fi
if ! printf '%s\n' "$HAI_RUNTIME_DB_USER" | grep -Eq '^[A-Za-z_][A-Za-z0-9_]{0,62}$'; then
  echo "HAI_RUNTIME_DB_USER must be a PostgreSQL identifier" >&2
  exit 64
fi

if [ "$PGUSER" = "$HAI_RUNTIME_DB_USER" ]; then
  echo "runtime database user must differ from the schema owner" >&2
  exit 64
fi
if [ "$PGPASSWORD" = "$HAI_RUNTIME_DB_PASSWORD" ]; then
  echo "runtime database password must differ from the schema owner password" >&2
  exit 64
fi
case "$PGPASSWORD" in
  change-this-*)
    echo "schema owner password must not be an example placeholder" >&2
    exit 64
    ;;
esac
case "$HAI_RUNTIME_DB_PASSWORD" in
  change-this-*)
    echo "runtime database password must not be an example placeholder" >&2
    exit 64
    ;;
esac

# The migration owner creates application objects in public. This reconciler
# does not rewrite unrelated schemas or other creators' defaults: it rejects
# relevant PUBLIC or non-owner defaults that could expand runtime access.
psql -X -v ON_ERROR_STOP=1 \
  --set=runtime_user="$HAI_RUNTIME_DB_USER" \
  --set=runtime_password="$HAI_RUNTIME_DB_PASSWORD" \
  --set=owner_user="$PGUSER" \
  --set=database_name="$PGDATABASE" <<'SQL'
BEGIN;

SELECT format(
  'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS',
  :'runtime_user', :'runtime_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'runtime_user')
\gexec

-- Ownership must remain with the migration account. Refuse legacy states that
-- would require changing ownership, including shared database ownership.
SELECT CASE WHEN EXISTS (
  SELECT 1
  FROM pg_shdepend
  WHERE refclassid = 'pg_authid'::regclass
    AND refobjid = (SELECT oid FROM pg_roles WHERE rolname = :'runtime_user')
    AND deptype = 'o'
) THEN 'true' ELSE 'false' END AS runtime_role_owns_objects
\gset
\if :runtime_role_owns_objects
\echo "runtime database role already owns database objects; refusing automatic downgrade"
DO $$ BEGIN
  RAISE EXCEPTION 'runtime database role already owns database objects; refusing automatic downgrade';
END $$;
\endif

-- Defaults in public (or global defaults that apply to public) must not give
-- PUBLIC or a non-owner creator's future objects to the runtime role.
SELECT CASE WHEN EXISTS (
  SELECT 1
  FROM pg_default_acl AS defaults
  CROSS JOIN LATERAL aclexplode(defaults.defaclacl) AS default_acl
  WHERE defaults.defaclnamespace IN (0::oid, 'public'::regnamespace::oid)
    AND (
      (defaults.defaclrole <> (SELECT oid FROM pg_roles WHERE rolname = :'owner_user')
        AND default_acl.grantee IN (0::oid, (SELECT oid FROM pg_roles WHERE rolname = :'runtime_user')))
    )
) THEN 'true' ELSE 'false' END AS unsupported_application_default_acl
\gset
\if :unsupported_application_default_acl
\echo "unsupported PUBLIC or non-owner default ACL in application scope"
DO $$ BEGIN
  RAISE EXCEPTION 'unsupported PUBLIC or non-owner default ACL in application scope';
END $$;
\endif

SELECT format(
  'ALTER ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS',
  :'runtime_user', :'runtime_password'
)
\gexec

-- Remove both inherited capabilities and role grants that let other logins
-- assume the runtime identity.
SELECT format('REVOKE %I FROM %I', granted_role.rolname, :'runtime_user')
FROM pg_auth_members AS membership
JOIN pg_roles AS granted_role ON granted_role.oid = membership.roleid
WHERE membership.member = (SELECT oid FROM pg_roles WHERE rolname = :'runtime_user')
\gexec

SELECT format('REVOKE %I FROM %I', :'runtime_user', member_role.rolname)
FROM pg_auth_members AS membership
JOIN pg_roles AS member_role ON member_role.oid = membership.member
WHERE membership.roleid = (SELECT oid FROM pg_roles WHERE rolname = :'runtime_user')
\gexec

SELECT format('REVOKE ALL PRIVILEGES ON DATABASE %I FROM %I', :'database_name', :'runtime_user')
\gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO %I', :'database_name', :'runtime_user')
\gexec

-- Older PostgreSQL databases can retain the historical PUBLIC CREATE grant on
-- public. Remove it so it cannot bypass the runtime role's direct revocation.
REVOKE CREATE ON SCHEMA public FROM PUBLIC;

SELECT format('REVOKE ALL PRIVILEGES ON SCHEMA %I FROM %I', schema_name.nspname, :'runtime_user')
FROM pg_namespace AS schema_name
WHERE schema_name.nspname NOT IN ('pg_catalog', 'information_schema')
  AND schema_name.nspname NOT LIKE 'pg_toast%'
  AND schema_name.nspname NOT LIKE 'pg_temp_%'
\gexec

SELECT format('REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA %I FROM %I', schema_name.nspname, :'runtime_user')
FROM pg_namespace AS schema_name
WHERE schema_name.nspname NOT IN ('pg_catalog', 'information_schema')
  AND schema_name.nspname NOT LIKE 'pg_toast%'
  AND schema_name.nspname NOT LIKE 'pg_temp_%'
\gexec

SELECT format('REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA %I FROM %I', schema_name.nspname, :'runtime_user')
FROM pg_namespace AS schema_name
WHERE schema_name.nspname NOT IN ('pg_catalog', 'information_schema')
  AND schema_name.nspname NOT LIKE 'pg_toast%'
  AND schema_name.nspname NOT LIKE 'pg_temp_%'
\gexec

SELECT format('REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA %I FROM %I', schema_name.nspname, :'runtime_user')
FROM pg_namespace AS schema_name
WHERE schema_name.nspname NOT IN ('pg_catalog', 'information_schema')
  AND schema_name.nspname NOT LIKE 'pg_toast%'
  AND schema_name.nspname NOT LIKE 'pg_temp_%'
\gexec

SELECT format('GRANT USAGE ON SCHEMA public TO %I', :'runtime_user')
\gexec
SELECT format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %I', :'runtime_user')
\gexec
SELECT format('GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO %I', :'runtime_user')
\gexec

-- Clear stale owner defaults before installing the supported future-object grants.
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I REVOKE ALL PRIVILEGES ON TABLES FROM %I', :'owner_user', :'runtime_user')
\gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I REVOKE ALL PRIVILEGES ON SEQUENCES FROM %I', :'owner_user', :'runtime_user')
\gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I REVOKE ALL PRIVILEGES ON FUNCTIONS FROM %I', :'owner_user', :'runtime_user')
\gexec

-- HAI's migration owner is the sole supported application-object creator.
-- Remove PUBLIC defaults only from objects that this owner creates; require
-- explicit grants for routines instead of inheriting PostgreSQL's PUBLIC
-- EXECUTE default. Other creators' defaults are validated above, not changed.
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I REVOKE ALL PRIVILEGES ON TABLES FROM PUBLIC', :'owner_user')
\gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I REVOKE ALL PRIVILEGES ON SEQUENCES FROM PUBLIC', :'owner_user')
\gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC', :'owner_user')
\gexec

SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE ALL PRIVILEGES ON TABLES FROM PUBLIC', :'owner_user')
\gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE ALL PRIVILEGES ON SEQUENCES FROM PUBLIC', :'owner_user')
\gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC', :'owner_user')
\gexec

SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA %I REVOKE ALL PRIVILEGES ON TABLES FROM %I', :'owner_user', schema_name.nspname, :'runtime_user')
FROM pg_namespace AS schema_name
WHERE schema_name.nspname NOT IN ('pg_catalog', 'information_schema')
  AND schema_name.nspname NOT LIKE 'pg_toast%'
  AND schema_name.nspname NOT LIKE 'pg_temp_%'
\gexec

SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA %I REVOKE ALL PRIVILEGES ON SEQUENCES FROM %I', :'owner_user', schema_name.nspname, :'runtime_user')
FROM pg_namespace AS schema_name
WHERE schema_name.nspname NOT IN ('pg_catalog', 'information_schema')
  AND schema_name.nspname NOT LIKE 'pg_toast%'
  AND schema_name.nspname NOT LIKE 'pg_temp_%'
\gexec

SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA %I REVOKE ALL PRIVILEGES ON FUNCTIONS FROM %I', :'owner_user', schema_name.nspname, :'runtime_user')
FROM pg_namespace AS schema_name
WHERE schema_name.nspname NOT IN ('pg_catalog', 'information_schema')
  AND schema_name.nspname NOT LIKE 'pg_toast%'
  AND schema_name.nspname NOT LIKE 'pg_temp_%'
\gexec

SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %I', :'owner_user', :'runtime_user')
\gexec
SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO %I', :'owner_user', :'runtime_user')
\gexec

SELECT CASE WHEN (
  NOT runtime_role.rolcanlogin
  OR runtime_role.rolsuper
  OR runtime_role.rolcreatedb
  OR runtime_role.rolcreaterole
  OR runtime_role.rolinherit
  OR runtime_role.rolreplication
  OR runtime_role.rolbypassrls
  OR has_database_privilege(runtime_role.rolname, :'database_name', 'CREATE')
  OR has_schema_privilege(runtime_role.rolname, 'public', 'CREATE')
  OR EXISTS (
    SELECT 1
    FROM pg_namespace AS schema_name
    WHERE schema_name.nspname NOT IN ('pg_catalog', 'information_schema', 'public')
      AND schema_name.nspname NOT LIKE 'pg_toast%'
      AND schema_name.nspname NOT LIKE 'pg_temp_%'
      AND (
        has_schema_privilege(runtime_role.rolname, schema_name.nspname, 'USAGE')
        OR has_schema_privilege(runtime_role.rolname, schema_name.nspname, 'CREATE')
      )
  )
  OR EXISTS (
    SELECT 1
    FROM pg_class AS relation
    JOIN pg_namespace AS schema_name ON schema_name.oid = relation.relnamespace
    CROSS JOIN LATERAL aclexplode(COALESCE(
      relation.relacl,
      acldefault(CASE WHEN relation.relkind = 'S' THEN 'S'::"char" ELSE 'r'::"char" END, relation.relowner)
    )) AS relation_acl
    WHERE schema_name.nspname = 'public'
      AND relation.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
      AND relation_acl.grantee = 0::oid
  )
  OR EXISTS (
    SELECT 1
    FROM pg_proc AS routine
    JOIN pg_namespace AS schema_name ON schema_name.oid = routine.pronamespace
    CROSS JOIN LATERAL aclexplode(COALESCE(
      routine.proacl,
      acldefault('f', routine.proowner)
    )) AS routine_acl
    WHERE schema_name.nspname = 'public'
      AND routine.prosecdef
      AND routine_acl.grantee = 0::oid
      AND routine_acl.privilege_type = 'EXECUTE'
  )
  OR EXISTS (
    SELECT 1
    FROM pg_default_acl AS defaults
    CROSS JOIN LATERAL aclexplode(defaults.defaclacl) AS default_acl
    WHERE defaults.defaclrole = (SELECT oid FROM pg_roles WHERE rolname = :'owner_user')
      AND defaults.defaclnamespace IN (0::oid, 'public'::regnamespace::oid)
      AND default_acl.grantee = 0::oid
  )
  OR EXISTS (
    SELECT 1 FROM pg_auth_members AS membership
    WHERE membership.member = runtime_role.oid OR membership.roleid = runtime_role.oid
  )
  OR EXISTS (
    SELECT 1 FROM pg_shdepend AS dependency
    WHERE dependency.refclassid = 'pg_authid'::regclass
      AND dependency.refobjid = runtime_role.oid
      AND dependency.deptype = 'o'
  )
) THEN 'true' ELSE 'false' END AS runtime_role_is_unsafe
FROM pg_roles AS runtime_role
WHERE runtime_role.rolname = :'runtime_user'
\gset
\if :runtime_role_is_unsafe
\echo "runtime database role retains privileges outside the DML contract"
DO $$ BEGIN
  RAISE EXCEPTION 'runtime database role retains privileges outside the DML contract';
END $$;
\endif

COMMIT;
SQL

echo "runtime database role provisioned"
