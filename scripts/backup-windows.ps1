[CmdletBinding()]
param(
    [string]$EnvFile = ".env.local",
    [string]$OutputDirectory = "backups",
    [switch]$ValidateOnly
)

$ErrorActionPreference = "Stop"
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$compose = Join-Path $root "docker-compose.local.yml"
$controlStateVolume = "018-hai-phase2-control-state"
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

function Wait-ContainerHealthy([string]$Container, [int]$TimeoutSeconds = 90) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $state = (& docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' $Container 2>$null | Out-String).Trim()
        if ($state -eq 'healthy' -or $state -eq 'running') { return }
        if ($state -eq 'unhealthy' -or $state -eq 'exited' -or $state -eq 'dead') {
            throw "$Container entered state '$state' after backup."
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "$Container did not become healthy within $TimeoutSeconds seconds after backup."
}

$envPath = Resolve-RepoPath $EnvFile
$outputPath = Resolve-RepoPath $OutputDirectory
if (-not (Test-Path -LiteralPath $envPath -PathType Leaf)) { throw "Environment file not found: $envPath" }
if (-not (Test-Path -LiteralPath $compose -PathType Leaf)) { throw "Compose file not found: $compose" }
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw "Docker Desktop is required." }

$settings = Read-DotEnv $envPath
$automationDB = Require-Setting $settings "AUTOMATION_DB_NAME"
$identityDB = Require-Setting $settings "IDP_DB_NAME"
$null = Require-Setting $settings "DB_USER"

& docker compose --env-file $envPath -f $compose config --quiet
if ($LASTEXITCODE -ne 0) { throw "Docker Compose validation failed." }
& docker volume inspect $controlStateVolume | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Safety control-state volume is unavailable: $controlStateVolume" }
& docker image inspect $archiveImage | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Local backend image is unavailable: $archiveImage. Run docker compose up --build first." }

if ($ValidateOnly) {
    Write-Host "Backup preflight passed for both Postgres databases, local media, and persisted safety controls."
    exit 0
}

$running = @{}
foreach ($service in @("backend", "idp")) {
    $running[$service] = -not [string]::IsNullOrWhiteSpace((& docker compose --env-file $envPath -f $compose ps -q $service | Out-String))
}

$stamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
$bundle = Join-Path $outputPath "hai-backup-$stamp"
$automationDump = Join-Path $bundle "automation.dump"
$identityDump = Join-Path $bundle "identity.dump"
$mediaArchive = Join-Path $bundle "media.zip"
$controlStateArchive = Join-Path $bundle "phase2-control-state.tar.gz"
$temporaryFiles = @(
    @{ Container = "018-hai-postgres-automation"; Path = "/tmp/hai-automation-$stamp.dump" },
    @{ Container = "018-hai-postgres-idp"; Path = "/tmp/hai-identity-$stamp.dump" }
)

New-Item -ItemType Directory -Path $bundle -Force | Out-Null
try {
    foreach ($service in @("backend", "idp")) {
        if ($running[$service]) {
            & docker compose --env-file $envPath -f $compose stop --timeout 30 $service | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "Could not stop $service for a consistent backup." }
        }
    }

    & docker exec 018-hai-postgres-automation pg_dump -U $settings.DB_USER -d $automationDB -Fc -f $temporaryFiles[0].Path
    if ($LASTEXITCODE -ne 0) { throw "Automation database dump failed." }
    & docker exec 018-hai-postgres-automation pg_restore --list $temporaryFiles[0].Path | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Automation database dump is not readable by pg_restore." }
    & docker cp "$($temporaryFiles[0].Container):$($temporaryFiles[0].Path)" $automationDump
    if ($LASTEXITCODE -ne 0) { throw "Could not copy the automation database dump." }

    & docker exec 018-hai-postgres-idp pg_dump -U $settings.DB_USER -d $identityDB -Fc -f $temporaryFiles[1].Path
    if ($LASTEXITCODE -ne 0) { throw "Identity database dump failed." }
    & docker exec 018-hai-postgres-idp pg_restore --list $temporaryFiles[1].Path | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Identity database dump is not readable by pg_restore." }
    & docker cp "$($temporaryFiles[1].Container):$($temporaryFiles[1].Path)" $identityDump
    if ($LASTEXITCODE -ne 0) { throw "Could not copy the identity database dump." }

    $mediaPath = Join-Path $root "images"
    New-Item -ItemType Directory -Path $mediaPath -Force | Out-Null
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    [IO.Compression.ZipFile]::CreateFromDirectory($mediaPath, $mediaArchive, [IO.Compression.CompressionLevel]::Optimal, $false)

    $modeJson = Read-HaiDockerVolumeDocument $controlStateVolume "background_mode.json" $archiveImage
    $emergencyJson = Read-HaiDockerVolumeDocument $controlStateVolume "emergency_stop.json" $archiveImage
    Assert-HaiControlStateDocuments $modeJson $emergencyJson

    & docker run --rm --network none --read-only --cap-drop ALL --user 10001:10001 `
        -v "${controlStateVolume}:/source:ro" `
        -v "${bundle}:/backup" `
        --entrypoint /bin/tar `
        $archiveImage `
        -czf "/backup/$([IO.Path]::GetFileName($controlStateArchive))" -C /source .
    if ($LASTEXITCODE -ne 0) { throw "Persisted safety control-state backup failed." }
    $controlStateEntries = @(& docker run --rm --network none --read-only --cap-drop ALL `
        -v "${bundle}:/backup:ro" `
        --entrypoint /bin/tar `
        $archiveImage `
        -tzf /backup/phase2-control-state.tar.gz)
    if ($LASTEXITCODE -ne 0) { throw "Persisted safety control-state archive is not readable." }
    Assert-HaiArchiveEntries $controlStateEntries

    $commit = (& git -C $root rev-parse HEAD 2>$null | Out-String).Trim()
    $files = @($automationDump, $identityDump, $mediaArchive, $controlStateArchive) | ForEach-Object {
        $item = Get-Item -LiteralPath $_
        [ordered]@{ name = $item.Name; bytes = $item.Length; sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $_).Hash.ToLowerInvariant() }
    }
    $manifest = [ordered]@{
        formatVersion = 2
        createdAt = (Get-Date).ToUniversalTime().ToString("o")
        gitCommit = $commit
        databases = @($automationDB, $identityDB)
        mediaSource = "images"
        controlStateSource = $controlStateVolume
        files = $files
    }
    Assert-HaiRecoveryManifest ([pscustomobject]$manifest) $bundle
    $manifest | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $bundle "manifest.json") -Encoding utf8
    Write-Host "Backup created: $bundle"
} catch {
    Remove-Item -LiteralPath $bundle -Recurse -Force -ErrorAction SilentlyContinue
    throw
} finally {
    foreach ($temporary in $temporaryFiles) {
        & docker exec $temporary.Container rm -f $temporary.Path 2>$null | Out-Null
    }
    foreach ($service in @("idp", "backend")) {
        if ($running[$service]) {
            & docker compose --env-file $envPath -f $compose up -d --no-build $service | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "Could not restart $service after backup." }
        }
    }
    if ($running.idp) { Wait-ContainerHealthy "018-hai-idp" }
    if ($running.backend) { Wait-ContainerHealthy "018-hai-backend" }
}
