[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$BackupDirectory,
    [string]$EnvFile = ".env.local",
    [switch]$ValidateOnly
)

$ErrorActionPreference = "Stop"
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$compose = Join-Path $root "docker-compose.local.yml"
$archiveImage = "018-hai-backend:local"
. (Join-Path $PSScriptRoot "windows-recovery-contract.ps1")

function Resolve-RepoPath([string]$Path) {
    if ([IO.Path]::IsPathRooted($Path)) { return [IO.Path]::GetFullPath($Path) }
    return [IO.Path]::GetFullPath((Join-Path $root $Path))
}

function Read-DotEnv([string]$Path) {
    $values = @{}
    foreach ($line in [IO.File]::ReadAllLines($Path)) {
        if ($line -match '^\s*#' -or [string]::IsNullOrWhiteSpace($line)) { continue }
        $separator = $line.IndexOf('=')
        if ($separator -lt 1) { continue }
        $name = $line.Substring(0, $separator).Trim()
        $value = $line.Substring($separator + 1).Trim()
        if (($value.StartsWith("'") -and $value.EndsWith("'")) -or
            ($value.StartsWith('"') -and $value.EndsWith('"'))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$name] = $value
    }
    return $values
}

function Require-Setting($Settings, [string]$Name) {
    $value = [string]$Settings[$Name]
    if ([string]::IsNullOrWhiteSpace($value)) { throw "$Name is required in the environment file." }
    return $value
}

$envPath = Resolve-RepoPath $EnvFile
$bundle = Resolve-RepoPath $BackupDirectory
if (-not (Test-Path -LiteralPath $envPath -PathType Leaf)) { throw "Environment file not found: $envPath" }
if (-not (Test-Path -LiteralPath $bundle -PathType Container)) { throw "Backup directory not found: $bundle" }
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw "Docker Desktop is required." }

$manifestPath = Join-Path $bundle "manifest.json"
if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw "Backup manifest is missing." }
$manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding utf8 | ConvertFrom-Json
Assert-HaiRecoveryManifest $manifest $bundle

Add-Type -AssemblyName System.IO.Compression.FileSystem
$media = [IO.Compression.ZipFile]::OpenRead((Join-Path $bundle "media.zip"))
try { $null = $media.Entries.Count } finally { $media.Dispose() }

& docker image inspect $archiveImage | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Local backend image is unavailable: $archiveImage. Run docker compose up --build first." }
$controlStateEntries = @(& docker run --rm --network none --read-only --cap-drop ALL `
    -v "${bundle}:/backup:ro" `
    --entrypoint /bin/tar `
    $archiveImage `
    -tzf /backup/phase2-control-state.tar.gz)
if ($LASTEXITCODE -ne 0) { throw "Safety control-state archive is not readable." }
Assert-HaiArchiveEntries $controlStateEntries

$settings = Read-DotEnv $envPath
$dbUser = Require-Setting $settings "DB_USER"
$liveAutomation = Require-Setting $settings "AUTOMATION_DB_NAME"
$liveIdentity = Require-Setting $settings "IDP_DB_NAME"
& docker compose --env-file $envPath -f $compose config --quiet
if ($LASTEXITCODE -ne 0) { throw "Docker Compose validation failed." }

if ($ValidateOnly) {
    Write-Host "Restore preflight passed: manifest, four checksums, media archive, safety controls, and Compose are valid."
    exit 0
}

$suffix = (Get-Date).ToUniversalTime().ToString("yyyyMMddHHmmss") + (Get-Random -Minimum 1000 -Maximum 9999)
$scratchAutomation = "hai_restore_automation_$suffix".ToLowerInvariant()
$scratchIdentity = "hai_restore_identity_$suffix".ToLowerInvariant()
$scratchControlVolume = "018-hai-phase2-restore-drill-$suffix".ToLowerInvariant()
if ($scratchAutomation -eq $liveAutomation -or $scratchIdentity -eq $liveIdentity) {
    throw "Scratch database name collides with a configured live database."
}

foreach ($target in @(
    @{ Container = "018-hai-postgres-automation"; Database = $scratchAutomation },
    @{ Container = "018-hai-postgres-idp"; Database = $scratchIdentity }
)) {
    $exists = (& docker exec $target.Container psql -U $dbUser -d postgres -Atqc "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = '$($target.Database)')" 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw "Could not verify scratch database is absent: $($target.Database)" }
    if ($exists -ne "f") { throw "Scratch database already exists; refusing to modify it: $($target.Database)" }
}
& docker volume inspect $scratchControlVolume 2>$null | Out-Null
if ($LASTEXITCODE -eq 0) { throw "Scratch control-state volume already exists; refusing to modify it: $scratchControlVolume" }

$automationTemp = "/tmp/$scratchAutomation.dump"
$identityTemp = "/tmp/$scratchIdentity.dump"
$automationCreated = $false
$identityCreated = $false
$controlVolumeCreated = $false
try {
    & docker volume create $scratchControlVolume | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not create the safety-control restore volume." }
    $controlVolumeCreated = $true
    & docker run --rm --network none --read-only --cap-drop ALL --cap-add CHOWN --user 0:0 `
        -v "${scratchControlVolume}:/restore" `
        -v "${bundle}:/backup:ro" `
        --entrypoint /bin/sh `
        $archiveImage `
        -c 'tar -oxzf /backup/phase2-control-state.tar.gz -C /restore && chmod 0750 /restore && chmod 0600 /restore/background_mode.json /restore/emergency_stop.json && chown -R 10001:10001 /restore'
    if ($LASTEXITCODE -ne 0) { throw "Safety control-state restore drill failed." }
    $modeJson = Read-HaiDockerVolumeDocument $scratchControlVolume "background_mode.json" $archiveImage
    $emergencyJson = Read-HaiDockerVolumeDocument $scratchControlVolume "emergency_stop.json" $archiveImage
    Assert-HaiControlStateDocuments $modeJson $emergencyJson

    & docker cp (Join-Path $bundle "automation.dump") "018-hai-postgres-automation:$automationTemp"
    if ($LASTEXITCODE -ne 0) { throw "Could not stage the automation dump." }
    & docker cp (Join-Path $bundle "identity.dump") "018-hai-postgres-idp:$identityTemp"
    if ($LASTEXITCODE -ne 0) { throw "Could not stage the identity dump." }

    & docker exec 018-hai-postgres-automation createdb -U $dbUser $scratchAutomation
    if ($LASTEXITCODE -ne 0) { throw "Could not create the automation scratch database." }
    $automationCreated = $true
    & docker exec 018-hai-postgres-automation pg_restore -U $dbUser --exit-on-error --no-owner --no-privileges -d $scratchAutomation $automationTemp
    if ($LASTEXITCODE -ne 0) { throw "Automation restore drill failed." }

    & docker exec 018-hai-postgres-idp createdb -U $dbUser $scratchIdentity
    if ($LASTEXITCODE -ne 0) { throw "Could not create the identity scratch database." }
    $identityCreated = $true
    & docker exec 018-hai-postgres-idp pg_restore -U $dbUser --exit-on-error --no-owner --no-privileges -d $scratchIdentity $identityTemp
    if ($LASTEXITCODE -ne 0) { throw "Identity restore drill failed." }

    $automationTables = [int]((& docker exec 018-hai-postgres-automation psql -U $dbUser -d $scratchAutomation -Atqc "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname='public'" | Out-String).Trim())
    $identityTables = [int]((& docker exec 018-hai-postgres-idp psql -U $dbUser -d $scratchIdentity -Atqc "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname='public'" | Out-String).Trim())
    if ($automationTables -lt 1) { throw "Automation restore contains no public tables." }
    if ($identityTables -lt 1) { throw "Identity restore contains no public tables." }
    Write-Host "Restore drill passed: automation tables=$automationTables; identity tables=$identityTables; safety controls extracted; live state untouched."
} finally {
    if ($automationCreated) { & docker exec 018-hai-postgres-automation dropdb -U $dbUser $scratchAutomation 2>$null | Out-Null }
    if ($identityCreated) { & docker exec 018-hai-postgres-idp dropdb -U $dbUser $scratchIdentity 2>$null | Out-Null }
    & docker exec 018-hai-postgres-automation rm -f $automationTemp 2>$null | Out-Null
    & docker exec 018-hai-postgres-idp rm -f $identityTemp 2>$null | Out-Null
    if ($controlVolumeCreated) { & docker volume rm $scratchControlVolume 2>$null | Out-Null }
}
