[CmdletBinding()]
param()

# Pure inspection fixtures: no Docker command, process, container or service is invoked.
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'windows-recovery-contract.ps1')
. (Join-Path $PSScriptRoot 'backup-windows.ps1')
. (Join-Path $PSScriptRoot 'isolated-recovery-rehearsal.ps1')

function Assert-IsolatedRefusal([string]$Name, [scriptblock]$Action) {
    $refused = $false
    try { & $Action } catch { $refused = $true }
    if (-not $refused) { throw "Isolated recovery unexpectedly accepted: $Name" }
}

$owner = 'a' * 32
$project = 'hai-recovery-' + $owner
$manifest = [pscustomobject]@{
    contract = 'hai-isolated-recovery.v1'; createdUtc = [DateTime]::UtcNow.ToString('o')
    owner = $owner; project = $project; user = 'hai_recovery'; image = 'sha256:' + ('c' * 64)
    containers = @(
        [pscustomobject]@{ kind = 'automation'; name = "$project-automation"; id = 'd' * 64; database = "hai_fixture_a_$owner"; databases = @([pscustomobject]@{ name = "hai_fixture_a_$owner"; oid = '16384'; owner = 'hai_recovery'; marker = "${owner}:hai_fixture_a_$owner" }) },
        [pscustomobject]@{ kind = 'identity'; name = "$project-identity"; id = 'e' * 64; database = "hai_fixture_i_$owner"; databases = @() }
    )
}
Assert-HaiIsolatedManifest $manifest
$secretManifest = $manifest | ConvertTo-Json -Depth 30 | ConvertFrom-Json
$secretManifest | Add-Member -NotePropertyName password -NotePropertyValue 'must-not-be-serialized'
Assert-IsolatedRefusal 'secret manifest property' { Assert-HaiIsolatedManifest $secretManifest }
foreach ($change in @('project', 'owner', 'image', 'expired', 'container', 'database', 'oid', 'marker', 'duplicate')) {
    $bad = $manifest | ConvertTo-Json -Depth 30 | ConvertFrom-Json
    switch ($change) {
        'project' { $bad.project = '018-hai' }
        'owner' { $bad.owner = 'guess' }
        'image' { $bad.image = 'postgres:17' }
        'expired' { $bad.createdUtc = [DateTime]::UtcNow.AddHours(-13).ToString('o') }
        'container' { $bad.containers[0].name = '018-hai-postgres-automation' }
        'database' { $bad.containers[0].database = 'personal_hai' }
        'oid' { $bad.containers[0].databases[0].oid = '0' }
        'marker' { $bad.containers[0].databases[0].marker = 'another-owner' }
        'duplicate' { $bad.containers[1].id = $bad.containers[0].id }
    }
    Assert-IsolatedRefusal $change { Assert-HaiIsolatedManifest $bad }
}
$record = $manifest.containers[0]
Assert-HaiIsolatedDatabaseReceipts @($record.databases) @($record.databases)
foreach ($change in @('name', 'oid', 'owner', 'marker', 'extra', 'duplicate', 'missing')) {
    $bad = @($record.databases | ConvertTo-Json -Depth 10 | ConvertFrom-Json)
    switch ($change) {
        'name' { $bad[0].name = 'personal_hai' }
        'oid' { $bad[0].oid = '16385' }
        'owner' { $bad[0].owner = 'another_owner' }
        'marker' { $bad[0].marker = 'another_run' }
        'extra' { $bad += [pscustomobject]@{ name = 'private_database' } }
        'duplicate' { $bad += $bad[0] }
        'missing' { $bad = @() }
    }
    Assert-IsolatedRefusal "database $change" { Assert-HaiIsolatedDatabaseReceipts $bad @($record.databases) }
}
$container = [pscustomobject]@{
    Id = $record.id; Name = '/' + $record.name; Image = $manifest.image
    Config = [pscustomobject]@{
        Labels = [pscustomobject]@{ 'hai.recovery.owner' = $owner; 'com.docker.compose.project' = $project; 'hai.recovery.kind' = 'automation' }
        Env = @('POSTGRES_USER=hai_recovery', "POSTGRES_DB=$($record.database)", 'POSTGRES_HOST_AUTH_METHOD=trust', 'PGDATA=/var/lib/postgresql/data')
        Cmd = @('postgres', '-c', 'shared_buffers=16MB', '-c', 'work_mem=1MB', '-c', 'max_connections=10', '-c', 'temp_file_limit=32768', '-c', 'log_statement=none')
    }
    HostConfig = [pscustomobject]@{
        NetworkMode = 'none'; Privileged = $false; PublishAllPorts = $false; PortBindings = [pscustomobject]@{}
        Binds = @(); Devices = @(); CapAdd = @(); VolumesFrom = @(); PidMode = ''; IpcMode = 'private'
        RestartPolicy = [pscustomobject]@{ Name = 'no' }; Memory = 268435456; MemorySwap = 268435456; NanoCpus = 500000000; PidsLimit = 64; ShmSize = 16777216
        Tmpfs = [pscustomobject]@{ '/var/lib/postgresql/data' = 'rw,size=134217728'; '/hai-control' = 'rw,size=16777216' }
    }
    Mounts = @([pscustomobject]@{ Type = 'tmpfs'; Destination = '/var/lib/postgresql/data' }, [pscustomobject]@{ Type = 'tmpfs'; Destination = '/hai-control' })
}
Assert-HaiIsolatedContainer $container $record $manifest
foreach ($change in @('id', 'name', 'image', 'owner', 'project', 'kind', 'ports', 'network', 'bind', 'volume', 'tmpfs', 'memory', 'cpu', 'pids', 'pgdata', 'password', 'command')) {
    $bad = $container | ConvertTo-Json -Depth 30 | ConvertFrom-Json
    switch ($change) {
        'id' { $bad.Id = 'f' * 64 }
        'name' { $bad.Name = '/018-hai-postgres-automation' }
        'image' { $bad.Image = 'sha256:' + ('f' * 64) }
        'owner' { $bad.Config.Labels.'hai.recovery.owner' = 'f' * 32 }
        'project' { $bad.Config.Labels.'com.docker.compose.project' = '018-hai' }
        'kind' { $bad.Config.Labels.'hai.recovery.kind' = 'identity' }
        'ports' { $bad.HostConfig.PublishAllPorts = $true }
        'network' { $bad.HostConfig.NetworkMode = 'host' }
        'bind' { $bad.HostConfig.Binds = @('C:\Users\NO:/personal') }
        'volume' { $bad.Mounts[0].Type = 'volume' }
        'tmpfs' { $bad.HostConfig.Tmpfs.'/hai-control' = 'rw,size=999999999' }
        'memory' { $bad.HostConfig.Memory = 0 }
        'cpu' { $bad.HostConfig.NanoCpus = 0 }
        'pids' { $bad.HostConfig.PidsLimit = 0 }
        'pgdata' { $bad.Config.Env += 'PGDATA=/personal' }
        'password' { $bad.Config.Env += 'POSTGRES_PASSWORD=must-not-be-serialized' }
        'command' { $bad.Config.Cmd = @('postgres', '-c', 'shared_buffers=1GB') }
    }
    Assert-IsolatedRefusal $change { Assert-HaiIsolatedContainer $bad $record $manifest }
}
Assert-IsolatedRefusal 'unrecorded container' { Get-HaiIsolatedRecord ([pscustomobject]@{ Manifest = $manifest }) '018-hai-postgres-automation' }
Assert-IsolatedRefusal 'caller manifest path' { Read-HaiIsolatedSelection 'C:\Users\NO\personal\resource-manifest.json' }
Assert-IsolatedRefusal 'unsafe transfer' { Invoke-HaiIsolatedCopy ([pscustomobject]@{ Manifest = $manifest }) $record.id '/etc/passwd' 'automation.dump' }
$media = [pscustomobject]@{ Entries = @(
    [pscustomobject]@{ FullName = 'first.txt'; Length = 20; ExternalAttributes = 0 },
    [pscustomobject]@{ FullName = 'second.bin'; Length = 5; ExternalAttributes = 0 },
    [pscustomobject]@{ FullName = 'empty/'; Length = 0; ExternalAttributes = 0 }
) }
Assert-HaiIsolatedMediaArchive $media
$media.Entries[0].Length = 2147483647
Assert-IsolatedRefusal 'oversized synthetic media' { Assert-HaiIsolatedMediaArchive $media }
# A forwarding-only stub demonstrates stdin argument selection, not resource safety or PG behavior.
$script:guardCalls = 0
$script:forwarded = $null
function Assert-HaiRecordedIsolatedTarget($Selection, [string]$Container) {
    if ($Container -cne $record.id) { throw 'Unexpected SQL forwarding target.' }
    $script:guardCalls++
}
function Invoke-HaiIsolatedRaw([string[]]$Arguments, [string]$InputText) {
    $script:forwarded = [pscustomobject]@{ Arguments = $Arguments; InputText = $InputText }
    return 't'
}
$sql = 'SELECT ''quoted'' COLLATE "C";'
Invoke-HaiIsolatedExec ([pscustomobject]@{ Manifest = $manifest }) $record.id @('psql', '-X', '-c', $sql) | Out-Null
if ($script:guardCalls -ne 1 -or $script:forwarded.InputText -cne $sql -or
    ($script:forwarded.Arguments -join ',') -cne "exec,-i,$($record.id),psql,-X") { throw 'Guarded stdin SQL forwarding contract failed.' }
Write-Output 'Pure isolated recovery manifest/container/path refusal fixtures passed; no real restoration proof.'
