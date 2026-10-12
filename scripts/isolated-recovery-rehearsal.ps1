[CmdletBinding()]
param(
    [ValidateSet('Prepare', 'Start', 'Seed', 'Backup', 'Restore', 'Verify', 'Cleanup')][string]$Action = 'Prepare',
    [string]$RecoveryResourceManifest,
    [ValidatePattern('^sha256:[0-9a-f]{64}$')][string]$PostgresImageId
)

# Parent-run transport rehearsal only. Dot-sourcing defines support, never starts resources.
function Invoke-HaiIsolatedRaw([string[]]$Arguments, [string]$InputText) {
    if ($PSBoundParameters.ContainsKey('InputText')) { $InputText | & docker @Arguments 2>$null }
    else { & docker @Arguments 2>$null }
    if ($LASTEXITCODE -ne 0) { throw 'Isolated recovery Docker operation failed; fixtures retained, private output suppressed.' }
}

function Get-HaiIsolatedRoot([string]$Owner) {
    $repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
    return Join-Path (Join-Path $repo 'output') ('recovery-rehearsal-' + $Owner)
}

function Assert-HaiIsolatedFiles($Selection) {
    Assert-HaiIsolatedManifest $Selection.Manifest
    $expected = Get-HaiIsolatedRoot $Selection.Manifest.owner
    if ($Selection.Root -cne $expected -or $Selection.Path -cne (Join-Path $expected 'resource-manifest.json')) {
        throw 'Isolated recovery refuses caller-selected paths.'
    }
    Assert-HaiOwnedDirectory $Selection.Root (Split-Path -Parent $expected) (Split-Path -Leaf $expected)
    if ((Get-FileHash -LiteralPath $Selection.Path -Algorithm SHA256).Hash -cne $Selection.Hash) {
        throw 'Isolated recovery manifest changed during the operation.'
    }
}

function Read-HaiIsolatedSelection([string]$Path) {
    $full = [IO.Path]::GetFullPath($Path)
    # Validate the path before opening it; do not read arbitrary caller files.
    $parent = Split-Path -Parent $full
    $leaf = Split-Path -Leaf $parent
    $match = [regex]::Match($leaf, '^recovery-rehearsal-([0-9a-f]{32})$')
    if ((Split-Path -Leaf $full) -cne 'resource-manifest.json' -or -not $match.Success) {
        throw 'Isolated recovery requires its generated resource-manifest path.'
    }
    $owner = $match.Groups[1].Value
    $expected = Get-HaiIsolatedRoot $owner
    if ($parent -cne $expected) { throw 'Isolated recovery manifest is outside its owned output root.' }
    Assert-HaiOwnedDirectory $parent (Split-Path -Parent $expected) $leaf
    $manifest = Get-Content -LiteralPath $full -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-HaiIsolatedManifest $manifest
    if ($manifest.owner -cne $owner) { throw 'Isolated recovery directory and owner differ.' }
    $selection = [pscustomobject]@{ Manifest = $manifest; Root = $parent; Path = $full; Hash = (Get-FileHash -LiteralPath $full -Algorithm SHA256).Hash }
    Assert-HaiIsolatedFiles $selection
    return $selection
}

function Save-HaiIsolatedSelection($Selection) {
    Assert-HaiIsolatedFiles $Selection
    [IO.File]::WriteAllText($Selection.Path, ($Selection.Manifest | ConvertTo-Json -Depth 30), [Text.UTF8Encoding]::new($false))
    $Selection.Hash = (Get-FileHash -LiteralPath $Selection.Path -Algorithm SHA256).Hash
}

function Assert-HaiIsolatedDaemon($Selection) {
    Assert-HaiIsolatedFiles $Selection
    Assert-HaiLocalRecoveryEngine
    $id = (Invoke-HaiIsolatedRaw @('info', '--format', '{{.ID}}') | Out-String).Trim()
    if (-not $Selection.Manifest.daemonId -or $id -cne $Selection.Manifest.daemonId) { throw 'Isolated recovery Docker engine identity changed.' }
}

function Assert-HaiLocalRecoveryEngine {
    if ($env:DOCKER_HOST) { throw 'Isolated recovery refuses ambient DOCKER_HOST overrides; do not change daemon settings.' }
    $contexts = @(Invoke-HaiIsolatedRaw @('context', 'inspect') | ConvertFrom-Json)
    if ($contexts.Count -ne 1 -or $contexts[0].Endpoints.docker.Host -cnotmatch '^(npipe|unix)://') {
        throw 'Isolated recovery requires the existing local Docker engine, not a remote endpoint/new daemon.'
    }
}

function Get-HaiIsolatedRecord($Selection, [string]$Container) {
    $records = @($Selection.Manifest.containers | Where-Object { $_.id -and -not $_.removed -and $_.id -ceq $Container })
    if ($records.Count -ne 1) { throw 'Isolated recovery refuses an unrecorded container ID.' }
    return $records[0]
}

function Assert-HaiIsolatedTarget($Selection, $Record) {
    Assert-HaiIsolatedDaemon $Selection
    $actual = @(Invoke-HaiIsolatedRaw @('inspect', '--type', 'container', $Record.id) | ConvertFrom-Json)
    if ($actual.Count -ne 1) { throw 'Isolated recovery container inventory changed.' }
    Assert-HaiIsolatedContainer $actual[0] $Record $Selection.Manifest
}

function Get-HaiIsolatedDatabaseInventory($Selection, $Record) {
    Assert-HaiIsolatedTarget $Selection $Record
    $sql = "SELECT json_build_object('name', datname, 'oid', oid::text, 'owner', pg_get_userbyid(datdba), 'marker', shobj_description(oid, 'pg_database'))::text FROM pg_database WHERE datname NOT IN ('template0','template1','postgres') ORDER BY datname;"
    return @(Invoke-HaiIsolatedRaw @('exec', '-i', $Record.id, 'psql', '-X', '--no-password', '-U', 'hai_recovery', '-d', 'postgres', '-Atq', '-v', 'ON_ERROR_STOP=1') -InputText $sql | ForEach-Object { $_ | ConvertFrom-Json })
}

function Assert-HaiIsolatedResources($Selection) {
    Assert-HaiIsolatedDaemon $Selection
    foreach ($record in $Selection.Manifest.containers) {
        if (-not $record.id) { continue }
        if ($record.removed) {
            $remaining = @(Invoke-HaiIsolatedRaw @('container', 'ls', '--all', '--no-trunc', '--filter', "id=$($record.id)", '--format', '{{.ID}}'))
            if ($remaining.Count) { throw 'Recorded fixture removal is not confirmed by the local engine.' }
            continue
        }
        $actual = @(Get-HaiIsolatedDatabaseInventory $Selection $record)
        $expected = @($record.databases)
        Assert-HaiIsolatedDatabaseReceipts $actual $expected
    }
}

function Assert-HaiReadyIsolatedPair($Selection) {
    Assert-HaiIsolatedResources $Selection
    if ($Selection.Manifest.cleaned -or @($Selection.Manifest.containers | Where-Object { $_.id -and -not $_.removed -and @($_.databases).Count -gt 0 }).Count -ne 2) {
        throw 'Both initialized, active synthetic containers are required.'
    }
}

function Assert-HaiRecordedIsolatedTarget($Selection, [string]$Container) {
    $record = Get-HaiIsolatedRecord $Selection $Container
    # This inventory rechecks the daemon and exact container identity/config.
    # Revalidate the target database receipts, not the unrelated peer, before
    # each operation. Whole-pair checks remain at phase boundaries and cleanup.
    $actual = @(Get-HaiIsolatedDatabaseInventory $Selection $record)
    Assert-HaiIsolatedDatabaseReceipts @($record.databases) $actual
}

function Invoke-HaiIsolatedExec($Selection, [string]$Container, [string[]]$Command) {
    Assert-HaiRecordedIsolatedTarget $Selection $Container
    if ($Command[0] -ceq 'psql' -and $Command.Count -gt 2 -and $Command[-2] -ceq '-c') {
        # SQL on stdin avoids Windows PowerShell's native embedded-quote loss.
        Invoke-HaiIsolatedRaw (@('exec', '-i', $Container) + @($Command[0..($Command.Count - 3)])) -InputText $Command[-1]
    } else {
        Invoke-HaiIsolatedRaw (@('exec', $Container) + $Command)
    }
}

function Invoke-HaiIsolatedCopy($Selection, [string]$Container, [string]$ContainerPath, [string]$HostName, [switch]$ToContainer) {
    $null = Get-HaiIsolatedRecord $Selection $Container
    if ($ContainerPath -cnotmatch '^/(tmp|hai-control)/(hai-[a-z-]+\.[a-z.]+)$' -or
        $HostName -cnotin @('automation.dump', 'identity.dump', 'phase2-control-state.tar.gz')) { throw 'Unowned isolated recovery transfer path.' }
    $hostPath = Join-Path (Join-Path $Selection.Root 'bundle') $HostName
    Assert-HaiRecordedIsolatedTarget $Selection $Container
    $remote = "${Container}:$ContainerPath"
    if ($ToContainer) { Invoke-HaiIsolatedRaw @('cp', $hostPath, $remote) | Out-Null }
    else { Invoke-HaiIsolatedRaw @('cp', $remote, $hostPath) | Out-Null }
}

function Add-HaiIsolatedDatabaseReceipt($Selection, $Record, [string]$Name) {
    # Only source initialization and successful exclusive createdb may call this.
    $short = if ($Record.kind -eq 'automation') { 'a' } else { 'i' }
    if ($Name -cnotin @($Record.database, "hai_restore_${short}_$($Selection.Manifest.owner)")) { throw 'Unowned isolated database receipt.' }
    $inventory = @(Get-HaiIsolatedDatabaseInventory $Selection $Record)
    $new = @($inventory | Where-Object { $_.name -ceq $Name -and $_.owner -ceq 'hai_recovery' -and -not $_.marker })
    if ($new.Count -ne 1 -or [string]$new[0].oid -cnotmatch '^[1-9][0-9]*$' -or $inventory.Count -ne (@($Record.databases).Count + 1)) { throw 'Cannot establish exclusive database receipt.' }
    foreach ($prior in @($Record.databases)) {
        if (@($inventory | Where-Object { $_.name -ceq $prior.name -and $_.oid -ceq $prior.oid -and $_.owner -ceq $prior.owner -and $_.marker -ceq $prior.marker }).Count -ne 1) {
            throw 'Prior isolated database receipt changed.'
        }
    }
    $marker = "$($Selection.Manifest.owner):$Name"
    Assert-HaiIsolatedTarget $Selection $Record
    # The OID assertion and ownership marker are applied in the same PostgreSQL transaction.
    $sql = "BEGIN; DO `$guard`$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_database WHERE datname='$Name' AND oid=$($new[0].oid) AND datdba=(SELECT oid FROM pg_roles WHERE rolname='hai_recovery') AND shobj_description(oid,'pg_database') IS NULL) THEN RAISE EXCEPTION 'database identity changed'; END IF; END; `$guard`$; COMMENT ON DATABASE $Name IS '$marker'; COMMIT;"
    Invoke-HaiIsolatedRaw @('exec', '-i', $Record.id, 'psql', '-X', '--no-password', '-U', 'hai_recovery', '-d', 'postgres', '-Atq', '-v', 'ON_ERROR_STOP=1') -InputText $sql | Out-Null
    $receipt = [pscustomobject]@{ name = $Name; oid = [string]$new[0].oid; owner = 'hai_recovery'; marker = $marker }
    $Record.databases = @($Record.databases) + $receipt
    Save-HaiIsolatedSelection $Selection
    Assert-HaiIsolatedResources $Selection
}

function New-HaiIsolatedSelection([string]$Image) {
    if ($Image -cnotmatch '^sha256:[0-9a-f]{64}$') { throw 'Supply an explicitly approved cached PostgreSQL 17 image ID; tags/pulls are forbidden.' }
    $owner = [Guid]::NewGuid().ToString('N')
    $root = Get-HaiIsolatedRoot $owner
    Assert-HaiOwnedDirectory $root (Split-Path -Parent $root) (Split-Path -Leaf $root)
    New-Item -ItemType Directory -Path $root -ErrorAction Stop | Out-Null
    $project = 'hai-recovery-' + $owner
    $containers = foreach ($kind in @('automation', 'identity')) {
        $short = if ($kind -eq 'automation') { 'a' } else { 'i' }
        [pscustomobject]@{ kind = $kind; name = "$project-$kind"; database = "hai_fixture_${short}_$owner"; id = ''; databases = @(); removed = $false }
    }
    $manifest = [pscustomobject]@{ contract = 'hai-isolated-recovery.v1'; createdUtc = [DateTime]::UtcNow.ToString('o'); owner = $owner; project = $project; image = $Image; user = 'hai_recovery'; daemonId = ''; containers = @($containers); seeded = $false; verified = $false; cleaned = $false }
    Assert-HaiIsolatedManifest $manifest
    $path = Join-Path $root 'resource-manifest.json'
    [IO.File]::WriteAllText($path, ($manifest | ConvertTo-Json -Depth 30), [Text.UTF8Encoding]::new($false))
    $media = Join-Path $root 'images'
    New-Item -ItemType Directory -Path $media | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $media 'empty') | Out-Null
    [IO.File]::WriteAllText((Join-Path $media 'first.txt'), "synthetic media one`n", [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllBytes((Join-Path $media 'second.bin'), [byte[]]@(0, 1, 2, 127, 255))
    return Read-HaiIsolatedSelection $path
}

function Start-HaiIsolatedFixture($Selection) {
    Assert-HaiIsolatedFiles $Selection
    if ($Selection.Manifest.daemonId -or @($Selection.Manifest.containers | Where-Object id).Count) { throw 'Fixture start is one-shot; do not reuse partially created resources.' }
    Assert-HaiLocalRecoveryEngine
    $Selection.Manifest.daemonId = (Invoke-HaiIsolatedRaw @('info', '--format', '{{.ID}}') | Out-String).Trim()
    if (-not $Selection.Manifest.daemonId) { throw 'Docker engine identity is unavailable.' }
    Save-HaiIsolatedSelection $Selection
    $image = @(Invoke-HaiIsolatedRaw @('image', 'inspect', $Selection.Manifest.image) | ConvertFrom-Json)
    if ($image.Count -ne 1 -or $image[0].Id -cne $Selection.Manifest.image) { throw 'Approved cached image identity is unavailable.' }
    $volumes = @($image[0].Config.Volumes.PSObject.Properties.Name)
    if ($image[0].Os -cne 'linux' -or $volumes.Count -ne 1 -or $volumes[0] -cne '/var/lib/postgresql/data' -or
        @($image[0].Config.Env | Where-Object { $_ -ceq 'PG_MAJOR=17' }).Count -ne 1 -or
        @($image[0].Config.Env | Where-Object { $_ -match '^(POSTGRES_PASSWORD|POSTGRES_USER_FILE|POSTGRES_DB_FILE|POSTGRES_INITDB_ARGS|PGHOST|PGSERVICE|PGPASSFILE)=' }).Count) {
        throw 'Cached image is not the bounded PostgreSQL 17 fixture contract; no alternative image will be pulled.'
    }
    foreach ($record in $Selection.Manifest.containers) {
        Assert-HaiIsolatedDaemon $Selection
        $existing = @(& docker inspect --type container $record.name 2>$null)
        if ($LASTEXITCODE -eq 0) { throw 'Fixture name is already in use; no existing resource will be reused.' }
        Assert-HaiIsolatedDaemon $Selection
        $id = (Invoke-HaiIsolatedRaw @('create', '--pull', 'never', '--name', $record.name,
            '--label', "hai.recovery.owner=$($Selection.Manifest.owner)", '--label', "com.docker.compose.project=$($Selection.Manifest.project)", '--label', "hai.recovery.kind=$($record.kind)",
            '--network', 'none', '--restart', 'no', '--memory', '268435456', '--memory-swap', '268435456', '--cpus', '0.5', '--pids-limit', '64', '--shm-size', '16777216',
            '--tmpfs', '/var/lib/postgresql/data:rw,size=134217728', '--tmpfs', '/hai-control:rw,size=16777216',
            '--env', 'POSTGRES_USER=hai_recovery', '--env', "POSTGRES_DB=$($record.database)", '--env', 'POSTGRES_HOST_AUTH_METHOD=trust',
            $Selection.Manifest.image, 'postgres', '-c', 'shared_buffers=16MB', '-c', 'work_mem=1MB', '-c', 'max_connections=10', '-c', 'temp_file_limit=32768', '-c', 'log_statement=none') | Out-String).Trim()
        if ($id -cnotmatch '^[0-9a-f]{64}$') { throw 'Container creation returned no exact identity; inspect locally, do not guess cleanup targets.' }
        $record.id = $id
        Save-HaiIsolatedSelection $Selection
        Assert-HaiIsolatedTarget $Selection $record
        Invoke-HaiIsolatedRaw @('start', $record.id) | Out-Null
        $ready = $false
        for ($attempt = 0; $attempt -lt 45; $attempt++) {
            Assert-HaiIsolatedTarget $Selection $record
            $probe = 'test $(cat /proc/1/comm) = postgres && pg_isready -U hai_recovery -d ' + $record.database
            & docker exec $record.id /bin/sh -c $probe 2>$null | Out-Null
            if ($LASTEXITCODE -eq 0) { $ready = $true; break }
            Start-Sleep -Seconds 2
        }
        if (-not $ready) { throw 'Synthetic PostgreSQL did not become ready; resources preserved.' }
        Add-HaiIsolatedDatabaseReceipt $Selection $record $record.database
        $version = @(Invoke-HaiIsolatedExec $Selection $record.id @('psql', '-X', '--no-password', '-U', 'hai_recovery', '-d', $record.database, '-Atq', '-c', 'SHOW server_version_num;'))
        if ($version.Count -ne 1 -or [int]$version[0] -lt 170000 -or [int]$version[0] -ge 180000) { throw 'This bounded fixture requires the approved cached PostgreSQL 17 image.' }
    }
}

function Invoke-HaiIsolatedSql($Selection, $Record, [string]$Database, [string]$Sql) {
    if (@($Record.databases | Where-Object name -CEQ $Database).Count -ne 1) { throw 'Isolated SQL refuses an unrecorded database.' }
    Invoke-HaiIsolatedExec $Selection $Record.id @('psql', '-X', '--no-password', '-U', 'hai_recovery', '-d', $Database, '-Atq', '-v', 'ON_ERROR_STOP=1', '-c', $Sql)
}

function Seed-HaiIsolatedFixture($Selection) {
    Assert-HaiReadyIsolatedPair $Selection
    if ($Selection.Manifest.seeded -or $Selection.Manifest.cleaned) { throw 'Synthetic seed is one-shot.' }
    $sql = @'
BEGIN;
CREATE TYPE public.fixture_status AS ENUM ('paused', 'read_only', 'approval_required');
CREATE TABLE public.context_memories (id bigserial PRIMARY KEY, owner_identity uuid NOT NULL, body text NOT NULL, state public.fixture_status NOT NULL, cost numeric(12,2) NOT NULL, allowed boolean NOT NULL, details jsonb NOT NULL, created_at timestamptz NOT NULL);
INSERT INTO public.context_memories VALUES
(1, '11111111-1111-4111-8111-111111111111', 'first' || chr(10) || chr(233), 'read_only', 0, false, '{"revision":1,"approval":"default_deny"}', '2026-10-01T00:00:00Z'),
(2, '22222222-2222-4222-8222-222222222222', 'second', 'approval_required', 0, false, '{"revision":2,"approval":"default_deny"}', '2026-10-01T00:00:01Z');
SELECT setval('public.context_memories_id_seq', 42, true);
CREATE SEQUENCE public.fixture_uncalled;
SELECT setval('public.fixture_uncalled', 97, false);
'@
    foreach ($name in @('workflow_items', 'workflow_events', 'workflow_decisions', 'life_ledger_commitment_revisions', 'life_ledger_cost_entries', 'users')) {
        $sql += "`nCREATE TABLE public.$name (LIKE public.context_memories INCLUDING ALL); INSERT INTO public.$name SELECT * FROM public.context_memories;"
    }
    $sql += "`nCOMMIT;"
    foreach ($r in $Selection.Manifest.containers) { Invoke-HaiIsolatedSql $Selection $r $r.database $sql | Out-Null }
    $source = @($Selection.Manifest.containers | Where-Object kind -CEQ 'automation')[0]
    $mode = '{"mode":"read_only"}'
    $stop = '{"engaged":true,"reason":"synthetic rehearsal","actor":"fixture-owner","engagedAt":"2026-10-01T00:00:00Z","updatedAt":"2026-10-01T00:00:00Z","revision":7}'
    Assert-HaiControlStateDocuments $mode $stop
    $modeBytes = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($mode))
    $stopBytes = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($stop))
    $command = "set -eu; mkdir /hai-control/source; printf %s $modeBytes | base64 -d > /hai-control/source/background_mode.json; printf %s $stopBytes | base64 -d > /hai-control/source/emergency_stop.json; chmod 0750 /hai-control/source; chmod 0600 /hai-control/source/*.json; chown -R 10001:10001 /hai-control/source"
    Invoke-HaiIsolatedExec $Selection $source.id @('/bin/sh', '-c', $command) | Out-Null
    $Selection.Manifest.seeded = $true
    Save-HaiIsolatedSelection $Selection
    Assert-HaiSyntheticDatabase $Selection $source $source.database
}

function Assert-HaiSyntheticDatabase($Selection, $Record, [string]$Database) {
    foreach ($name in @('context_memories', 'workflow_items', 'workflow_events', 'workflow_decisions', 'life_ledger_commitment_revisions', 'life_ledger_cost_entries', 'users')) {
        $sql = "SELECT count(*)=2 AND count(DISTINCT owner_identity)=2 AND bool_and((id=1 AND owner_identity='11111111-1111-4111-8111-111111111111' AND body='first'||chr(10)||chr(233) AND state='read_only' AND details->>'revision'='1') OR (id=2 AND owner_identity='22222222-2222-4222-8222-222222222222' AND body='second' AND state='approval_required' AND details->>'revision'='2')) AND bool_and(cost=0 AND NOT allowed AND details->>'approval'='default_deny') FROM public.$name;"
        $ok = @(Invoke-HaiIsolatedSql $Selection $Record $Database $sql)
        if ($ok.Count -ne 1 -or $ok[0] -cne 't') { throw 'Independent synthetic row/owner/EUR0/default-deny invariant failed.' }
    }
    $sql = "SELECT (SELECT last_value=42 AND is_called FROM public.context_memories_id_seq) AND (SELECT last_value=97 AND NOT is_called FROM public.fixture_uncalled) AND enum_range(NULL::public.fixture_status)::text='{paused,read_only,approval_required}' AND (SELECT count(*)=7 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r') AND NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','S') AND c.relowner<>(SELECT oid FROM pg_roles WHERE rolname='hai_recovery'));"
    $ok = @(Invoke-HaiIsolatedSql $Selection $Record $Database $sql)
    if ($ok.Count -ne 1 -or $ok[0] -cne 't') { throw 'Independent synthetic sequence/type/relation-owner invariant failed.' }
}

function Get-HaiIsolatedControls($Selection, $Record, [ValidateSet('source', 'restore')][string]$Directory) {
    $documents = @()
    $digests = [ordered]@{}
    foreach ($name in @('background_mode.json', 'emergency_stop.json')) {
        # UID 10001 readability is checked separately from privileged extraction.
        Assert-HaiRecordedIsolatedTarget $Selection $Record.id
        $command = "test -f /hai-control/$Directory/$name && test ! -L /hai-control/$Directory/$name && cat /hai-control/$Directory/$name"
        $content = @(Invoke-HaiIsolatedRaw @('exec', '--user', '10001:10001', $Record.id, '/bin/sh', '-c', $command))
        $documents += $content -join "`n"
        Assert-HaiRecordedIsolatedTarget $Selection $Record.id
        $hash = @(Invoke-HaiIsolatedRaw @('exec', '--user', '10001:10001', $Record.id, 'sha256sum', "/hai-control/$Directory/$name"))
        $pattern = '^([0-9a-f]{64})  ' + [regex]::Escape("/hai-control/$Directory/$name") + '$'
        if ($hash.Count -ne 1 -or $hash[0] -cnotmatch $pattern) { throw 'Invalid synthetic control byte digest.' }
        $digests[$name] = $Matches[1]
    }
    Assert-HaiControlStateDocuments $documents[0] $documents[1]
    $mode = $documents[0] | ConvertFrom-Json
    $stop = $documents[1] | ConvertFrom-Json
    $expectedMode = '{"mode":"read_only"}'
    $expectedStop = '{"engaged":true,"reason":"synthetic rehearsal","actor":"fixture-owner","engagedAt":"2026-10-01T00:00:00Z","updatedAt":"2026-10-01T00:00:00Z","revision":7}'
    if ($mode.mode -cne 'read_only' -or -not $stop.engaged -or $stop.revision -ne 7 -or
        $digests['background_mode.json'] -cne (Get-HaiTextDigest $expectedMode) -or
        $digests['emergency_stop.json'] -cne (Get-HaiTextDigest $expectedStop)) { throw 'Independent synthetic restrictive safety bytes/state invariant failed.' }
    return [pscustomobject]$digests
}

function Invoke-HaiIsolatedBackup($Selection, [switch]$ValidateOnly) {
    Assert-HaiReadyIsolatedPair $Selection
    if (-not $Selection.Manifest.seeded -or $Selection.Manifest.cleaned) { throw 'An initialized synthetic fixture is required.' }
    if ($ValidateOnly) { Write-Output 'Isolated backup ownership preflight only; no dump performed.'; return }
    $bundle = Join-Path $Selection.Root 'bundle'
    New-Item -ItemType Directory -Path $bundle -ErrorAction Stop | Out-Null
    $evidence = @{}
    foreach ($r in $Selection.Manifest.containers) {
        Assert-HaiSyntheticDatabase $Selection $r $r.database
        $evidence[$r.kind] = Get-HaiDatabaseIntegrity $r.id 'hai_recovery' $r.database $r.kind $Selection
        $temp = "/tmp/hai-$($r.kind).dump"
        Invoke-HaiIsolatedExec $Selection $r.id @('/bin/sh', '-c', "test ! -e '$temp' && test ! -L '$temp' && (set -C; : > '$temp')") | Out-Null
        Invoke-HaiIsolatedExec $Selection $r.id @('pg_dump', '--no-password', '-U', 'hai_recovery', '-d', $r.database, '-Fc', '-f', $temp) | Out-Null
        Invoke-HaiIsolatedExec $Selection $r.id @('pg_restore', '--list', $temp) | Out-Null
        Invoke-HaiIsolatedCopy $Selection $r.id $temp ($r.kind + '.dump')
        Assert-HaiRecoveryEvidence $evidence[$r.kind] (Get-HaiDatabaseIntegrity $r.id 'hai_recovery' $r.database $r.kind $Selection) 'Synthetic source during dump'
    }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    Assert-HaiIsolatedFiles $Selection
    $mediaPath = Join-Path $Selection.Root 'images'
    [IO.Compression.ZipFile]::CreateFromDirectory($mediaPath, (Join-Path $bundle 'media.zip'))
    $zip = [IO.Compression.ZipFile]::OpenRead((Join-Path $bundle 'media.zip'))
    try { Assert-HaiMediaContents $zip $mediaPath } finally { $zip.Dispose() }
    $source = @($Selection.Manifest.containers | Where-Object kind -CEQ 'automation')[0]
    $controls = Get-HaiIsolatedControls $Selection $source 'source'
    Invoke-HaiIsolatedExec $Selection $source.id @('/bin/sh', '-c', 'test ! -e /tmp/hai-control.tar.gz && test ! -L /tmp/hai-control.tar.gz && (set -C; : > /tmp/hai-control.tar.gz) && tar -czf /tmp/hai-control.tar.gz -C /hai-control/source .') | Out-Null
    Invoke-HaiIsolatedCopy $Selection $source.id '/tmp/hai-control.tar.gz' 'phase2-control-state.tar.gz'
    $entries = @(Invoke-HaiIsolatedExec $Selection $source.id @('tar', '-tzf', '/tmp/hai-control.tar.gz'))
    $details = @(Invoke-HaiIsolatedExec $Selection $source.id @('tar', '-tvzf', '/tmp/hai-control.tar.gz'))
    Assert-HaiStrictControlArchive $entries $details
    Assert-HaiRecoveryEvidence $controls (Get-HaiIsolatedControls $Selection $source 'source') 'Synthetic safety source during backup'
    $files = foreach ($name in @('automation.dump', 'identity.dump', 'media.zip', 'phase2-control-state.tar.gz')) {
        $path = Join-Path $bundle $name
        [pscustomobject]@{ name = $name; bytes = (Get-Item -LiteralPath $path).Length; sha256 = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() }
    }
    $backup = [pscustomobject][ordered]@{ formatVersion = 2; recoveryOwner = $Selection.Manifest.owner; recoveryProject = $Selection.Manifest.project; databases = @($Selection.Manifest.containers.database); mediaSource = 'images'; controlStateSource = $Selection.Manifest.project + '-control'; integrity = [pscustomobject][ordered]@{ contract = 'hai-recovery-integrity.v1'; automation = $evidence.automation; identity = $evidence.identity; controls = $controls }; files = @($files) }
    Assert-HaiRecoveryManifest $backup $bundle $Selection
    Assert-HaiIsolatedResources $Selection
    [IO.File]::WriteAllText((Join-Path $bundle 'manifest.json'), ($backup | ConvertTo-Json -Depth 30), [Text.UTF8Encoding]::new($false))
    Write-Output 'Synthetic backup created; no application/production recovery acceptance implied.'
}

function Invoke-HaiIsolatedRestore($Selection, [switch]$ValidateOnly) {
    Assert-HaiReadyIsolatedPair $Selection
    $bundle = Join-Path $Selection.Root 'bundle'
    $backup = Get-Content -LiteralPath (Join-Path $bundle 'manifest.json') -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-HaiRecoveryManifest $backup $bundle $Selection
    if ($backup.integrity.contract -cne 'hai-recovery-integrity.v1' -or
        (@($backup.databases) -join ',') -cne (@($Selection.Manifest.containers.database) -join ',')) { throw 'Synthetic backup database/integrity contract changed.' }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead((Join-Path $bundle 'media.zip'))
    try { Assert-HaiIsolatedMediaArchive $zip } finally { $zip.Dispose() }
    if ($ValidateOnly) { Write-Output 'Isolated restore checksums/ownership preflight only; no restoration performed.'; return }
    foreach ($r in $Selection.Manifest.containers) {
        $short = if ($r.kind -eq 'automation') { 'a' } else { 'i' }
        $scratch = "hai_restore_${short}_$($Selection.Manifest.owner)"
        if (@($r.databases | Where-Object name -CEQ $scratch).Count) { throw 'Restore is one-shot; prior/failed scratch evidence will not be overwritten.' }
        $temp = "/tmp/hai-restored-$($r.kind).dump"
        Invoke-HaiIsolatedExec $Selection $r.id @('/bin/sh', '-c', "test ! -e '$temp' && test ! -L '$temp' && (set -C; : > '$temp')") | Out-Null
        Invoke-HaiIsolatedCopy $Selection $r.id $temp ($r.kind + '.dump') -ToContainer
        Invoke-HaiIsolatedExec $Selection $r.id @('createdb', '--no-password', '-U', 'hai_recovery', '--template=template0', '--', $scratch) | Out-Null
        Add-HaiIsolatedDatabaseReceipt $Selection $r $scratch
        Invoke-HaiIsolatedExec $Selection $r.id @('pg_restore', '--no-password', '-U', 'hai_recovery', '--exit-on-error', '--no-owner', '--no-privileges', '-d', $scratch, $temp) | Out-Null
        Assert-HaiRecoveryEvidence $backup.integrity.($r.kind) (Get-HaiDatabaseIntegrity $r.id 'hai_recovery' $scratch $r.kind $Selection) 'Synthetic restored database'
        Assert-HaiSyntheticDatabase $Selection $r $scratch
    }
    $target = @($Selection.Manifest.containers | Where-Object kind -CEQ 'identity')[0]
    Invoke-HaiIsolatedExec $Selection $target.id @('/bin/sh', '-c', 'test ! -e /tmp/hai-restored-control.tar.gz && test ! -L /tmp/hai-restored-control.tar.gz && (set -C; : > /tmp/hai-restored-control.tar.gz)') | Out-Null
    Invoke-HaiIsolatedCopy $Selection $target.id '/tmp/hai-restored-control.tar.gz' 'phase2-control-state.tar.gz' -ToContainer
    $entries = @(Invoke-HaiIsolatedExec $Selection $target.id @('tar', '-tzf', '/tmp/hai-restored-control.tar.gz'))
    $details = @(Invoke-HaiIsolatedExec $Selection $target.id @('tar', '-tvzf', '/tmp/hai-restored-control.tar.gz'))
    Assert-HaiStrictControlArchive $entries $details
    Invoke-HaiIsolatedExec $Selection $target.id @('/bin/sh', '-c', 'set -eu; mkdir /hai-control/restore; tar -oxzf /tmp/hai-restored-control.tar.gz -C /hai-control/restore; chmod 0750 /hai-control/restore; chmod 0600 /hai-control/restore/*.json; chown -R 10001:10001 /hai-control/restore') | Out-Null
    Assert-HaiRecoveryEvidence $backup.integrity.controls (Get-HaiIsolatedControls $Selection $target 'restore') 'Synthetic restored safety controls'
    $media = Join-Path $Selection.Root 'restored-media'
    Assert-HaiIsolatedFiles $Selection
    New-Item -ItemType Directory -Path $media -ErrorAction Stop | Out-Null
    [IO.Compression.ZipFile]::ExtractToDirectory((Join-Path $bundle 'media.zip'), $media)
    $zip = [IO.Compression.ZipFile]::OpenRead((Join-Path $bundle 'media.zip'))
    try { Assert-HaiMediaContents $zip $media } finally { $zip.Dispose() }
    Verify-HaiIsolatedFixture $Selection
}

function Assert-HaiIsolatedMediaArchive($Archive) {
    Assert-HaiMediaEntries $Archive
    if (@($Archive.Entries).Count -ne 3) { throw 'Synthetic media archive must contain exactly its two files and empty directory.' }
    foreach ($entry in $Archive.Entries) {
        $length = switch -CaseSensitive ($entry.FullName) {
            'first.txt' { 20 }
            'second.bin' { 5 }
            'empty/' { 0 }
            default { throw 'Unexpected synthetic media archive entry.' }
        }
        if ($entry.Length -ne $length) { throw 'Synthetic media archive size invariant failed.' }
    }
}

function Verify-HaiIsolatedFixture($Selection) {
    Assert-HaiReadyIsolatedPair $Selection
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $bundle = Join-Path $Selection.Root 'bundle'
    $backup = Get-Content -LiteralPath (Join-Path $bundle 'manifest.json') -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-HaiRecoveryManifest $backup $bundle $Selection
    foreach ($r in $Selection.Manifest.containers) {
        if (@($r.databases).Count -ne 2) { throw 'Both synthetic source and restored databases must exist.' }
        foreach ($db in $r.databases) {
            Assert-HaiSyntheticDatabase $Selection $r $db.name
            Assert-HaiRecoveryEvidence $backup.integrity.($r.kind) (Get-HaiDatabaseIntegrity $r.id 'hai_recovery' $db.name $r.kind $Selection) 'Synthetic source/restored evidence'
        }
    }
    $a = @($Selection.Manifest.containers | Where-Object kind -CEQ 'automation')[0]
    $i = @($Selection.Manifest.containers | Where-Object kind -CEQ 'identity')[0]
    Assert-HaiRecoveryEvidence $backup.integrity.controls (Get-HaiIsolatedControls $Selection $a 'source') 'Synthetic source controls'
    Assert-HaiRecoveryEvidence $backup.integrity.controls (Get-HaiIsolatedControls $Selection $i 'restore') 'Synthetic restored controls'
    foreach ($directory in @('images', 'restored-media')) {
        $media = Join-Path $Selection.Root $directory
        $zip = [IO.Compression.ZipFile]::OpenRead((Join-Path $bundle 'media.zip'))
        try { Assert-HaiMediaContents $zip $media } finally { $zip.Dispose() }
        if ([IO.File]::ReadAllText((Join-Path $media 'first.txt')) -cne "synthetic media one`n" -or
            [Convert]::ToBase64String([IO.File]::ReadAllBytes((Join-Path $media 'second.bin'))) -cne 'AAECf/8=' -or
            -not (Test-Path -LiteralPath (Join-Path $media 'empty') -PathType Container)) { throw 'Independent synthetic media invariant failed.' }
    }
    $Selection.Manifest.verified = $true
    Save-HaiIsolatedSelection $Selection
    $receipt = [ordered]@{ contract = 'hai-synthetic-recovery-receipt.v1'; owner = $Selection.Manifest.owner; project = $Selection.Manifest.project; verifiedUtc = [DateTime]::UtcNow.ToString('o'); rowsSequencesTypesOwners = $true; mediaAndRestrictiveControls = $true; sourceUnchanged = $true; applicationAcceptance = $false; cleanupPerformed = $false }
    $receiptPath = Join-Path $Selection.Root 'receipt.json'
    if (Test-Path -LiteralPath $receiptPath) { $receiptPath = Join-Path $Selection.Root ('receipt-' + [Guid]::NewGuid().ToString('N') + '.json') }
    [IO.File]::WriteAllText($receiptPath, ($receipt | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
    Write-Output 'Synthetic PG dump/restore, independent row/sequence/type/owner, EUR0/default-deny, media and safety invariants passed. Fixtures retained; not HAI production acceptance.'
}

function Remove-HaiIsolatedFixture($Selection) {
    # Never prune, enumerate suffixes, touch volumes, or delete evidence. Explicit parent action only.
    Assert-HaiIsolatedResources $Selection
    foreach ($r in $Selection.Manifest.containers) {
        if (-not $r.id -or $r.removed) { continue }
        Assert-HaiIsolatedResources $Selection
        Invoke-HaiIsolatedRaw @('rm', '--force', $r.id) | Out-Null
        $r.removed = $true
        Save-HaiIsolatedSelection $Selection
        Assert-HaiIsolatedResources $Selection
    }
    $Selection.Manifest.cleaned = $true
    Save-HaiIsolatedSelection $Selection
    $receiptPath = Join-Path $Selection.Root ('cleanup-receipt-' + [Guid]::NewGuid().ToString('N') + '.json')
    $receipt = [ordered]@{ contract = 'hai-synthetic-recovery-cleanup.v1'; owner = $Selection.Manifest.owner; project = $Selection.Manifest.project; removedUtc = [DateTime]::UtcNow.ToString('o'); containers = @($Selection.Manifest.containers | Where-Object removed | Select-Object name, id); hostEvidenceRetained = $true }
    [IO.File]::WriteAllText($receiptPath, ($receipt | ConvertTo-Json -Depth 5), [Text.UTF8Encoding]::new($false))
    Write-Output 'Exact owned fixture containers removed; all host evidence retained. No volume/host cleanup performed.'
}

if ($MyInvocation.InvocationName -eq '.') { return }
$ErrorActionPreference = 'Stop'
$requestedManifest = $RecoveryResourceManifest
. (Join-Path $PSScriptRoot 'windows-recovery-contract.ps1')
. (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
if ($Action -eq 'Prepare') {
    if ($requestedManifest) { throw 'Prepare always generates a fresh owner/project; caller manifest reuse is forbidden.' }
    $selection = New-HaiIsolatedSelection $PostgresImageId
    Write-Output $selection.Path
    return
}
if ($PostgresImageId) { throw 'Image selection is recorded only during Prepare.' }
if (-not $requestedManifest) { throw 'Supply the exact Prepare resource-manifest path.' }
$selection = Read-HaiIsolatedSelection $requestedManifest
switch ($Action) {
    'Start' { Start-HaiIsolatedFixture $selection }
    'Seed' { Seed-HaiIsolatedFixture $selection }
    'Backup' { Invoke-HaiIsolatedBackup $selection }
    'Restore' { Invoke-HaiIsolatedRestore $selection }
    'Verify' { Verify-HaiIsolatedFixture $selection }
    'Cleanup' { Remove-HaiIsolatedFixture $selection }
}
