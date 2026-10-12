$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$provisionerPath = Join-Path $repositoryRoot "services\postgres-runtime-role\provision-runtime-role.sh"
$containerName = "hai-runtime-role-test-$PID-$([guid]::NewGuid().ToString('N').Substring(0, 8))"
$databaseName = "hai_role_test"
$ownerPassword = "owner-$([guid]::NewGuid().ToString('N'))"
$runtimePassword = "runtime-$([guid]::NewGuid().ToString('N'))"
$rotatedRuntimePassword = "rotated-$([guid]::NewGuid().ToString('N'))"
$publicFailurePassword = "public-failure-$([guid]::NewGuid().ToString('N'))"
$databaseFailurePassword = "database-failure-$([guid]::NewGuid().ToString('N'))"
$schemaFailurePassword = "schema-failure-$([guid]::NewGuid().ToString('N'))"
$defaultFailurePassword = "default-failure-$([guid]::NewGuid().ToString('N'))"
$containerStarted = $false
$testFailure = $null
$cleanupFailure = $null

function Invoke-ContainerSql {
    param(
        [string]$User,
        [string]$Password,
        [string]$Sql,
        [string]$ExpectedErrorPattern,
        [switch]$ExpectFailure
    )

    $dockerArguments = @(
        "exec", "--env", "PGHOST=127.0.0.1", "--env", "PGPASSWORD=$Password", "--env", "LC_ALL=C",
        $containerName, "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-A", "-t",
        "-U", $User, "-d", $databaseName, "-c", $Sql
    )
    $output = @(& docker @dockerArguments 2>&1)
    $exitCode = $LASTEXITCODE
    if ($ExpectFailure) {
        if ($exitCode -eq 0) {
            throw "SQL unexpectedly succeeded for role '$User': $Sql"
        }
        if ($ExpectedErrorPattern -and (($output -join "`n") -notmatch $ExpectedErrorPattern)) {
            throw "SQL failed for role '$User' for an unexpected reason. Expected '$ExpectedErrorPattern'; output: $($output -join "`n")"
        }
    } elseif ($exitCode -ne 0) {
        throw "Disposable PostgreSQL query failed for SQL [$Sql]: $($output -join "`n")"
    }
    return (($output | ForEach-Object { [string]$_ }) -join "`n").Trim()
}

function Invoke-RoleProvisioner {
    param(
        [string]$RuntimeUser = "hai_runtime",
        [string]$RuntimeSecret = $runtimePassword,
        [string]$ExpectedErrorPattern,
        [string]$PsqlRcPath,
        [switch]$ExpectFailure
    )

    $dockerArguments = @(
        "exec",
        "--env", "PGHOST=127.0.0.1",
        "--env", "PGPORT=5432",
        "--env", "PGDATABASE=$databaseName",
        "--env", "PGUSER=hai_owner",
        "--env", "PGPASSWORD=$ownerPassword",
        "--env", "HAI_RUNTIME_DB_USER=$RuntimeUser",
        "--env", "HAI_RUNTIME_DB_PASSWORD=$RuntimeSecret"
    )
    if ($PsqlRcPath) {
        $dockerArguments += @("--env", "PSQLRC=$PsqlRcPath")
    }
    $dockerArguments += @($containerName, "sh", "/tmp/provision-runtime-role.sh")
    $output = @(& docker @dockerArguments 2>&1)
    $exitCode = $LASTEXITCODE
    if ($ExpectFailure) {
        if ($exitCode -eq 0) {
            throw "Role provisioning unexpectedly succeeded for '$RuntimeUser': $($output -join "`n")"
        }
        if ($ExpectedErrorPattern -and (($output -join "`n") -notmatch $ExpectedErrorPattern)) {
            throw "Role provisioning failed for '$RuntimeUser' for an unexpected reason. Expected '$ExpectedErrorPattern'; output: $($output -join "`n")"
        }
    } elseif ($exitCode -ne 0) {
        throw "Runtime-role provisioning failed: $($output -join "`n")"
    }
    return (($output | ForEach-Object { [string]$_ }) -join "`n").Trim()
}

function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

try {
    $dockerArguments = @(
        "run", "--detach", "--rm", "--name", $containerName, "--network", "none",
        "--env", "POSTGRES_USER=hai_owner",
        "--env", "POSTGRES_PASSWORD=$ownerPassword",
        "--env", "POSTGRES_DB=$databaseName",
        "--env", "POSTGRES_HOST_AUTH_METHOD=scram-sha-256",
        "postgres:17-alpine"
    )
    # Set before launch so the finally block also checks for a partially
    # created container if Docker reports a startup error.
    $containerStarted = $true
    $output = @(& docker @dockerArguments 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Could not start the isolated PostgreSQL 17 test instance: $($output -join "`n")"
    }
    $output = @(& docker cp $provisionerPath "${containerName}:/tmp/provision-runtime-role.sh" 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Could not copy the role provisioner into the disposable database: $($output -join "`n")"
    }

    $ready = $false
    for ($attempt = 0; $attempt -lt 45; $attempt++) {
        & docker exec $containerName pg_isready -h 127.0.0.1 -U hai_owner -d $databaseName 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) {
            $ready = $true
            break
        }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) { throw "The disposable PostgreSQL 17 instance did not become ready." }

    # The official image keeps a loopback trust rule ahead of its generated
    # host-auth fallback. Replace that rule in this disposable fixture so bad
    # passwords are actually tested over TCP rather than silently bypassed.
    $authOutput = @(& docker exec --user postgres $containerName sh -c "sed -i 's/trust/scram-sha-256/g' /var/lib/postgresql/data/pg_hba.conf && pg_ctl reload -D /var/lib/postgresql/data" 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Could not enable SCRAM authentication in the disposable PostgreSQL fixture: $($authOutput -join "`n")"
    }
    [void](Invoke-ContainerSql -User "hai_owner" -Password "hai-intentionally-wrong-auth-probe" -ExpectFailure -ExpectedErrorPattern "(?i)password authentication failed" -Sql "SELECT 1;")

    $rcSetup = @(& docker exec $containerName sh -c "printf '%s\n' '\set ON_ERROR_STOP off' '\! touch /tmp/hai-runtime-role-test-psqlrc-executed' > /tmp/hai-runtime-role-test.psqlrc" 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Could not create the hostile psqlrc fixture: $($rcSetup -join "`n")"
    }

    $initialSchema = @'
CREATE TABLE public.existing_records (
  id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
  owner_id text NOT NULL,
  payload text NOT NULL
);
INSERT INTO public.existing_records (owner_id, payload) VALUES ('owner-a', 'before-provisioning');
CREATE TABLE public.owner_scoped_records (
  id bigserial PRIMARY KEY,
  owner_id text NOT NULL,
  payload text NOT NULL
);
ALTER TABLE public.owner_scoped_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.owner_scoped_records FORCE ROW LEVEL SECURITY;
CREATE POLICY owner_scoped_records_policy ON public.owner_scoped_records
  TO PUBLIC
  USING (owner_id = current_setting('app.owner_id', true))
  WITH CHECK (owner_id = current_setting('app.owner_id', true));
INSERT INTO public.owner_scoped_records (owner_id, payload)
VALUES ('owner-a', 'visible'), ('owner-b', 'private');
'@
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql $initialSchema)

    # First use models the documented fresh-install order: migrations as owner,
    # then runtime-role provisioning before the API is allowed to start.
    [void](Invoke-RoleProvisioner -PsqlRcPath "/tmp/hai-runtime-role-test.psqlrc")
    $rcMarkerCheck = @(& docker exec $containerName sh -c "test ! -e /tmp/hai-runtime-role-test-psqlrc-executed" 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "psql executed the hostile startup file despite the provisioner's -X option."
    }
    $roleFlags = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
SELECT rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
       AND NOT rolinherit AND NOT rolreplication AND NOT rolbypassrls
FROM pg_roles WHERE rolname = 'hai_runtime';
'@
    Assert-True ($roleFlags -eq "t") "Fresh runtime role flags are not least-privilege."

    $privileges = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
SELECT has_table_privilege('hai_runtime', 'public.existing_records', 'SELECT')
   AND has_table_privilege('hai_runtime', 'public.existing_records', 'INSERT')
   AND has_table_privilege('hai_runtime', 'public.existing_records', 'UPDATE')
   AND has_table_privilege('hai_runtime', 'public.existing_records', 'DELETE')
   AND NOT has_table_privilege('hai_runtime', 'public.existing_records', 'TRUNCATE')
   AND NOT has_table_privilege('hai_runtime', 'public.existing_records', 'REFERENCES')
   AND NOT has_table_privilege('hai_runtime', 'public.existing_records', 'TRIGGER')
   AND has_sequence_privilege('hai_runtime', 'public.existing_records_id_seq', 'USAGE')
   AND NOT has_database_privilege('hai_runtime', current_database(), 'CREATE')
   AND NOT has_schema_privilege('hai_runtime', 'public', 'CREATE');
'@
    Assert-True ($privileges -eq "t") "Fresh runtime grants exceed or fall short of the DML contract."

    $runtimeDml = Invoke-ContainerSql -User "hai_runtime" -Password $runtimePassword -Sql @'
INSERT INTO public.existing_records (owner_id, payload) VALUES ('owner-a', 'runtime-write') RETURNING id;
UPDATE public.existing_records SET payload = 'runtime-update' WHERE payload = 'runtime-write';
DELETE FROM public.existing_records WHERE payload = 'runtime-update';
'@
    Assert-True (-not [string]::IsNullOrWhiteSpace($runtimeDml)) "Runtime role could not use existing table and identity-sequence grants."

    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
CREATE TABLE public.future_records (
  id bigserial PRIMARY KEY,
  payload text NOT NULL
);
CREATE FUNCTION public.future_security_definer() RETURNS integer
LANGUAGE sql SECURITY DEFINER AS 'SELECT 1';
CREATE ROLE legacy_group NOLOGIN CREATEROLE CREATEDB;
CREATE ROLE legacy_member NOLOGIN;
'@)
    $futureFunctionAcl = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
SELECT NOT EXISTS (
  SELECT 1
  FROM pg_proc AS routine
  CROSS JOIN LATERAL aclexplode(COALESCE(routine.proacl, acldefault('f', routine.proowner))) AS routine_acl
  WHERE routine.oid = 'public.future_security_definer()'::regprocedure
    AND routine_acl.grantee = 0::oid
    AND routine_acl.privilege_type = 'EXECUTE'
)
AND NOT has_function_privilege('hai_runtime', 'public.future_security_definer()', 'EXECUTE');
'@
    Assert-True ($futureFunctionAcl -eq "t") "Migration-owner default privileges left PUBLIC or the runtime role able to execute a future SECURITY DEFINER routine."
    $futureDml = Invoke-ContainerSql -User "hai_runtime" -Password $runtimePassword -Sql @'
INSERT INTO public.future_records (payload) VALUES ('owner-default-grant') RETURNING id;
'@
    Assert-True ($futureDml -match "1") "Owner default privileges did not cover a future table and sequence."

    $visibleRows = Invoke-ContainerSql -User "hai_runtime" -Password $runtimePassword -Sql @'
SET app.owner_id = 'owner-a';
SELECT string_agg(owner_id, ',' ORDER BY owner_id) FROM public.owner_scoped_records;
'@
    Assert-True ($visibleRows -eq "owner-a") "The runtime identity did not preserve row-level owner isolation; result was '$visibleRows'."
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $runtimePassword -ExpectFailure -Sql @'
SET app.owner_id = 'owner-a';
INSERT INTO public.owner_scoped_records (owner_id, payload) VALUES ('owner-b', 'must-be-rejected');
'@)
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $runtimePassword -ExpectFailure -Sql "CREATE TABLE public.runtime_ddl_forbidden (id integer);")
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql "CREATE TABLE public.owner_migration_check (id integer);")

    # Upgrade-like state: the existing login has accumulated unsafe attributes,
    # memberships, ACLs, and default ACLs, while schema ownership remains with
    # the documented migration owner.
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
ALTER ROLE hai_runtime SUPERUSER CREATEDB CREATEROLE INHERIT REPLICATION BYPASSRLS;
GRANT legacy_group TO hai_runtime;
GRANT hai_runtime TO legacy_member;
GRANT CREATE ON DATABASE hai_role_test TO hai_runtime;
GRANT CREATE ON SCHEMA public TO hai_runtime;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO hai_runtime;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO hai_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE hai_owner IN SCHEMA public GRANT ALL ON TABLES TO hai_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE hai_owner IN SCHEMA public GRANT ALL ON SEQUENCES TO hai_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE hai_owner IN SCHEMA public GRANT ALL ON FUNCTIONS TO hai_runtime;
GRANT CREATE ON SCHEMA public TO PUBLIC;
'@)
    $upgradeOutput = Invoke-RoleProvisioner -RuntimeSecret $rotatedRuntimePassword
    Assert-True ($upgradeOutput -match "runtime database role provisioned") "Upgrade-like role reconciliation did not complete."

    $upgradeFlags = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
SELECT rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
       AND NOT rolinherit AND NOT rolreplication AND NOT rolbypassrls
       AND NOT has_database_privilege('hai_runtime', current_database(), 'CREATE')
       AND NOT has_schema_privilege('hai_runtime', 'public', 'CREATE')
       AND NOT has_table_privilege('hai_runtime', 'public.existing_records', 'TRUNCATE')
       AND NOT has_table_privilege('hai_runtime', 'public.existing_records', 'REFERENCES')
       AND NOT has_table_privilege('hai_runtime', 'public.existing_records', 'TRIGGER')
       AND NOT EXISTS (
         SELECT 1 FROM pg_auth_members
         WHERE member = (SELECT oid FROM pg_roles WHERE rolname = 'hai_runtime')
            OR roleid = (SELECT oid FROM pg_roles WHERE rolname = 'hai_runtime')
       )
       AND NOT EXISTS (
         SELECT 1 FROM pg_shdepend
         WHERE refclassid = 'pg_authid'::regclass
           AND refobjid = (SELECT oid FROM pg_roles WHERE rolname = 'hai_runtime')
           AND deptype = 'o'
       )
FROM pg_roles WHERE rolname = 'hai_runtime';
'@
    Assert-True ($upgradeFlags -eq "t") "Upgrade reconciliation left elevated attributes, memberships, ACLs, or ownership."

    $publicCreate = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql "SELECT NOT has_schema_privilege('hai_runtime', 'public', 'CREATE');"
    Assert-True ($publicCreate -eq "t") "Legacy PUBLIC schema CREATE still reaches the runtime role."
    $rotatedDml = Invoke-ContainerSql -User "hai_runtime" -Password $rotatedRuntimePassword -Sql @'
SET app.owner_id = 'owner-a';
SELECT string_agg(owner_id, ',' ORDER BY owner_id) FROM public.owner_scoped_records;
INSERT INTO public.future_records (payload) VALUES ('after-upgrade') RETURNING id;
'@
    Assert-True ($rotatedDml -match "owner-a") "Rotated runtime credentials lost DML or RLS behavior."
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $runtimePassword -ExpectFailure -ExpectedErrorPattern "(?i)password authentication failed" -Sql "SELECT 1;")
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $rotatedRuntimePassword -ExpectFailure -Sql "CREATE TABLE public.runtime_ddl_after_upgrade (id integer);")
    [void](Invoke-RoleProvisioner -RuntimeSecret $rotatedRuntimePassword)

    # This failure occurs after role/password and ACL mutations in the
    # transaction. The provisioner must roll all of them back rather than
    # silently removing an unexpected PUBLIC grant.
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
ALTER ROLE hai_runtime SUPERUSER CREATEDB CREATEROLE INHERIT REPLICATION BYPASSRLS;
GRANT SELECT ON public.existing_records TO PUBLIC;
'@)
    [void](Invoke-RoleProvisioner -RuntimeSecret $publicFailurePassword -ExpectFailure -ExpectedErrorPattern "outside the DML contract")
    $publicRollbackState = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
SELECT role_state.rolsuper
       AND role_state.rolcreatedb
       AND role_state.rolcreaterole
       AND role_state.rolinherit
       AND role_state.rolreplication
       AND role_state.rolbypassrls
       AND has_table_privilege('hai_runtime', 'public.existing_records', 'SELECT')
       AND EXISTS (
         SELECT 1
         FROM pg_class AS relation
         CROSS JOIN LATERAL aclexplode(COALESCE(relation.relacl, acldefault('r', relation.relowner))) AS relation_acl
         WHERE relation.oid = 'public.existing_records'::regclass
           AND relation_acl.grantee = 0::oid
           AND relation_acl.privilege_type = 'SELECT'
       )
FROM pg_roles AS role_state
WHERE role_state.rolname = 'hai_runtime';
'@
    Assert-True ($publicRollbackState -eq "t") "The post-mutation PUBLIC failure did not preserve the original role state and grant through rollback."
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $rotatedRuntimePassword -Sql "SELECT 1;")
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $publicFailurePassword -ExpectFailure -ExpectedErrorPattern "(?i)password authentication failed" -Sql "SELECT 1;")
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql "REVOKE SELECT ON public.existing_records FROM PUBLIC;")
    [void](Invoke-RoleProvisioner -RuntimeSecret $rotatedRuntimePassword)

    # Database-level PUBLIC CREATE is also outside the runtime contract. It
    # must fail independently of the relation-level PUBLIC-grant probe above.
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql "GRANT CREATE ON DATABASE hai_role_test TO PUBLIC;")
    [void](Invoke-RoleProvisioner -RuntimeSecret $databaseFailurePassword -ExpectFailure -ExpectedErrorPattern "outside the DML contract")
    $databaseGrantPreserved = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
SELECT has_database_privilege('hai_runtime', current_database(), 'CREATE')
   AND EXISTS (
     SELECT 1
     FROM pg_database AS database
     CROSS JOIN LATERAL aclexplode(COALESCE(database.datacl, acldefault('d', database.datdba))) AS database_acl
     WHERE database.datname = current_database()
       AND database_acl.grantee = 0::oid
       AND database_acl.privilege_type = 'CREATE'
   );
'@
    Assert-True ($databaseGrantPreserved -eq "t") "The unexpected database-level PUBLIC CREATE grant was not preserved after fail-closed rejection."
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $rotatedRuntimePassword -Sql "SELECT 1;")
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $databaseFailurePassword -ExpectFailure -ExpectedErrorPattern "(?i)password authentication failed" -Sql "SELECT 1;")
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql "REVOKE CREATE ON DATABASE hai_role_test FROM PUBLIC;")
    [void](Invoke-RoleProvisioner -RuntimeSecret $rotatedRuntimePassword)

    # Public access on a separate application schema is rejected, but the
    # reconciler must not rewrite that unrelated schema's ACL as a side effect.
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
CREATE SCHEMA hai_unrelated_scope;
GRANT USAGE ON SCHEMA hai_unrelated_scope TO PUBLIC;
'@)
    [void](Invoke-RoleProvisioner -RuntimeSecret $schemaFailurePassword -ExpectFailure -ExpectedErrorPattern "outside the DML contract")
    $schemaGrantPreserved = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
SELECT has_schema_privilege('hai_runtime', 'hai_unrelated_scope', 'USAGE')
   AND EXISTS (
     SELECT 1
     FROM pg_namespace AS schema_name
     CROSS JOIN LATERAL aclexplode(COALESCE(schema_name.nspacl, acldefault('n', schema_name.nspowner))) AS schema_acl
     WHERE schema_name.nspname = 'hai_unrelated_scope'
       AND schema_acl.grantee = 0::oid
       AND schema_acl.privilege_type = 'USAGE'
   );
'@
    Assert-True ($schemaGrantPreserved -eq "t") "The unrelated schema PUBLIC grant was not preserved after fail-closed rejection."
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $rotatedRuntimePassword -Sql "SELECT 1;")
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $schemaFailurePassword -ExpectFailure -ExpectedErrorPattern "(?i)password authentication failed" -Sql "SELECT 1;")
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql "DROP SCHEMA hai_unrelated_scope;")

    # A non-owner creator's PUBLIC or runtime-role defaults are unsupported.
    # They are rejected without changing that creator's default ACLs.
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
CREATE ROLE hai_secondary_creator NOLOGIN;
ALTER DEFAULT PRIVILEGES FOR ROLE hai_secondary_creator IN SCHEMA public
  GRANT SELECT ON TABLES TO PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE hai_secondary_creator IN SCHEMA public
  GRANT INSERT ON TABLES TO hai_runtime;
'@)
    [void](Invoke-RoleProvisioner -RuntimeSecret $defaultFailurePassword -ExpectFailure -ExpectedErrorPattern "unsupported PUBLIC or non-owner default ACL")
    $nonOwnerDefaultsPreserved = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
SELECT count(*) = 2
FROM pg_default_acl AS defaults
CROSS JOIN LATERAL aclexplode(defaults.defaclacl) AS default_acl
WHERE defaults.defaclrole = (SELECT oid FROM pg_roles WHERE rolname = 'hai_secondary_creator')
  AND defaults.defaclnamespace = 'public'::regnamespace
  AND default_acl.grantee IN (0::oid, (SELECT oid FROM pg_roles WHERE rolname = 'hai_runtime'));
'@
    Assert-True ($nonOwnerDefaultsPreserved -eq "t") "The non-owner PUBLIC/runtime default ACL fixture did not remain intact after rejection."
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $rotatedRuntimePassword -Sql "SELECT 1;")
    [void](Invoke-ContainerSql -User "hai_runtime" -Password $defaultFailurePassword -ExpectFailure -ExpectedErrorPattern "(?i)password authentication failed" -Sql "SELECT 1;")
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
ALTER DEFAULT PRIVILEGES FOR ROLE hai_secondary_creator IN SCHEMA public
  REVOKE SELECT ON TABLES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE hai_secondary_creator IN SCHEMA public
  REVOKE INSERT ON TABLES FROM hai_runtime;
DROP ROLE hai_secondary_creator;
'@)
    [void](Invoke-RoleProvisioner -RuntimeSecret $rotatedRuntimePassword)

    # Existing runtime ownership is not silently reassigned across the cluster.
    [void](Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
CREATE ROLE hai_stale_runtime LOGIN SUPERUSER PASSWORD 'stale-role-password';
CREATE TABLE public.stale_runtime_owned (id integer);
ALTER TABLE public.stale_runtime_owned OWNER TO hai_stale_runtime;
'@)
    $staleOwnershipCount = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
SELECT count(*)
FROM pg_shdepend
WHERE refclassid = 'pg_authid'::regclass
  AND refobjid = (SELECT oid FROM pg_roles WHERE rolname = 'hai_stale_runtime')
  AND deptype = 'o';
'@
    Assert-True ([int]$staleOwnershipCount -gt 0) "The disposable upgrade fixture did not register its runtime-owned object in pg_shdepend."
    $ownershipFailure = Invoke-RoleProvisioner -RuntimeUser "hai_stale_runtime" -RuntimeSecret "stale-new-password" -ExpectFailure
    Assert-True ($ownershipFailure -match "refusing automatic downgrade") "Provisioning did not fail closed on legacy runtime ownership."
    $staleRoleState = Invoke-ContainerSql -User "hai_owner" -Password $ownerPassword -Sql @'
SELECT rolsuper AND (SELECT relowner = (SELECT oid FROM pg_roles WHERE rolname = 'hai_stale_runtime')
                     FROM pg_class WHERE oid = 'public.stale_runtime_owned'::regclass)
FROM pg_roles WHERE rolname = 'hai_stale_runtime';
'@
    Assert-True ($staleRoleState -eq "t") "Ownership refusal partially changed the legacy role or object."

    Write-Host "Disposable PostgreSQL role provisioning passed: psqlrc isolation, fresh install, upgrade reconciliation, password rotation, RLS, database/table/schema PUBLIC rejection, default-ACL rejection, transaction rollback, and ownership guard."
} catch {
    $testFailure = $_
} finally {
    if ($containerStarted) {
        try {
            $stopOutput = @(& docker stop --time 5 $containerName 2>&1)
            $stopExitCode = $LASTEXITCODE
            if ($stopExitCode -eq 0) {
                $containerStarted = $false
            } else {
                $inspectOutput = @(& docker inspect $containerName 2>&1)
                $inspectExitCode = $LASTEXITCODE
                if ($inspectExitCode -eq 0) {
                    $cleanupFailure = "docker stop failed with exit code $stopExitCode and the disposable container still exists: $($stopOutput -join "`n")"
                } elseif (($inspectOutput -join "`n") -match "(?i)(no such object|no such container)") {
                    $containerStarted = $false
                } else {
                    $cleanupFailure = "docker stop failed with exit code $stopExitCode and container removal could not be verified. Stop output: $($stopOutput -join "`n"); inspect output: $($inspectOutput -join "`n")"
                }
            }
        } catch {
            $cleanupFailure = "Exception while stopping or verifying disposable container '$containerName': $_"
        }
    }
}

if ($cleanupFailure) {
    [Console]::Error.WriteLine("CLEANUP FAILURE: $cleanupFailure")
}
if ($testFailure) {
    throw $testFailure
}
if ($cleanupFailure) {
    throw $cleanupFailure
}
