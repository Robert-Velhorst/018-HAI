[CmdletBinding()]
param(
    [string]$BackupDirectory,
    [string]$EnvFile = ".env.local",
    [switch]$ValidateOnly,
    [switch]$RestoreEnvironmentOnly,
    [string]$RecoveryResourceManifest
)

$ErrorActionPreference = "Stop"
if ($PSBoundParameters.ContainsKey('RecoveryResourceManifest')) {
    if ($RestoreEnvironmentOnly) { throw 'Environment-only recovery is available only for a normal Windows backup bundle.' }
    if ([string]::IsNullOrWhiteSpace($RecoveryResourceManifest)) { throw 'An explicit isolated recovery manifest must not be empty.' }
    if ($PSBoundParameters.ContainsKey('EnvFile') -or $PSBoundParameters.ContainsKey('BackupDirectory')) {
        throw 'Isolated recovery selects its bundle from the owned manifest, not caller paths.'
    }
    $isolatedManifestPath = $RecoveryResourceManifest
    $isolatedValidateOnly = [bool]$ValidateOnly
    . (Join-Path $PSScriptRoot 'windows-recovery-contract.ps1')
    . (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
    . (Join-Path $PSScriptRoot 'isolated-recovery-rehearsal.ps1')
    Invoke-HaiIsolatedRestore (Read-HaiIsolatedSelection $isolatedManifestPath) -ValidateOnly:$isolatedValidateOnly
    return
}
if ([string]::IsNullOrWhiteSpace($BackupDirectory)) { throw 'BackupDirectory is required for normal-installation recovery.' }
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$compose = Join-Path $root "docker-compose.local.yml"
$archiveImage = "018-hai-backend:local"
. (Join-Path $PSScriptRoot "windows-recovery-contract.ps1")
. (Join-Path $PSScriptRoot "backup-windows.ps1") -EnvFile $EnvFile -ValidateOnly:$ValidateOnly -LibraryOnly

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
if (-not (Test-Path -LiteralPath $bundle -PathType Container)) { throw "Backup directory not found: $bundle" }
Assert-HaiOwnedDirectory $bundle ([IO.Path]::GetDirectoryName($bundle)) ([IO.Path]::GetFileName($bundle))

$manifestPath = Join-Path $bundle "manifest.json"
if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw "Backup manifest is missing." }
$manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding utf8 | ConvertFrom-Json
Assert-HaiRecoveryManifest $manifest $bundle
Assert-HaiExtendedRecoveryCoverage $manifest.extendedRecoveryCoverage
if ($null -eq $manifest.integrity -or $manifest.integrity.contract -cne 'hai-recovery-integrity.v1' -or
    $null -eq $manifest.integrity.automation -or $null -eq $manifest.integrity.identity -or $null -eq $manifest.integrity.controls) {
    throw "Backup lacks canonical database and safety integrity evidence; create a new complete backup."
}

if ($RestoreEnvironmentOnly) {
    if ($ValidateOnly) { throw 'Choose either -ValidateOnly or -RestoreEnvironmentOnly, not both.' }
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        throw 'Environment-only recovery requires Windows DPAPI CurrentUser protection.'
    }
    if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) { throw 'The signed-in Windows profile is unavailable.' }
    $expectedEnvironmentPath = [IO.Path]::GetFullPath((Join-Path $env:LOCALAPPDATA 'HAI\hai.env'))
    if (-not [string]::Equals([IO.Path]::GetFullPath($envPath), $expectedEnvironmentPath, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Environment-only recovery may write only %LOCALAPPDATA%\HAI\hai.env.'
    }
    if (Test-Path -LiteralPath $envPath) {
        throw 'Environment target already exists; refusing to overwrite it.'
    }
    if (-not (Test-Path -LiteralPath ([IO.Path]::GetDirectoryName($expectedEnvironmentPath)) -PathType Container)) {
        throw 'The protected HAI data directory is missing; install HAI before restoring its environment. No files or Docker data were changed.'
    }

    Restore-HaiProtectedEnvironmentFile $bundle $manifest $expectedEnvironmentPath
    $restoredEnvironment = Get-Item -LiteralPath $expectedEnvironmentPath -Force -ErrorAction Stop
    if ($restoredEnvironment.PSIsContainer -or ($restoredEnvironment.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'Recovered environment did not produce a regular protected file.'
    }
    Write-Host 'The original protected HAI environment was restored. No Docker commands ran and no containers or volumes were changed. Run Start HAI to perform ownership checks and start the existing stack.'
    return
}

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw "Docker Desktop is required." }
Assert-HaiLocalDockerEngine

$environmentRestored = $false
$environmentPreflightComplete = $false
if (Test-Path -LiteralPath $envPath) {
    $environmentItem = Get-Item -LiteralPath $envPath -Force
    if (-not $environmentItem.PSIsContainer -and ($environmentItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0) {
        # Existing operator configuration is authoritative and is never overwritten.
    } else { throw 'Environment target exists but is not a regular file; refusing to follow or replace it.' }
} else {
    if ($ValidateOnly) {
        $settings = Restore-HaiProtectedEnvironmentFile $bundle $manifest $envPath -ValidateOnly -ValidationAction {
            param($stagedEnvironmentPath)
            & (Join-Path $PSScriptRoot 'backup-windows.ps1') -EnvFile $stagedEnvironmentPath -ValidateOnly
        }
        $environmentPreflightComplete = $true
    } else {
        Restore-HaiProtectedEnvironmentFile $bundle $manifest $envPath
        $environmentRestored = $true
    }
}

if (-not $environmentPreflightComplete) { $settings = Read-DotEnv $envPath }
$dbUser = Require-Setting $settings "DB_USER"
$liveAutomation = Require-Setting $settings "AUTOMATION_DB_NAME"
$liveIdentity = Require-Setting $settings "IDP_DB_NAME"
if (@($manifest.databases).Count -ne 2 -or $manifest.databases[0] -cne $liveAutomation -or $manifest.databases[1] -cne $liveIdentity) {
    throw "Backup database identities do not match the selected environment file."
}
# Use the recovered/provided environment only for a read-only Compose preflight.
if (-not $environmentPreflightComplete) { & (Join-Path $PSScriptRoot 'backup-windows.ps1') -EnvFile $envPath -ValidateOnly }

Add-Type -AssemblyName System.IO.Compression.FileSystem
$media = [IO.Compression.ZipFile]::OpenRead((Join-Path $bundle "media.zip"))
try { Assert-HaiMediaEntries $media } finally { $media.Dispose() }

& docker image inspect $archiveImage | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Local backend image is unavailable: $archiveImage. Run docker compose up --build first." }
$controlStateEntries = @(& docker run --rm --network none --read-only --cap-drop ALL `
    -v "${bundle}:/backup:ro" `
    --entrypoint /bin/tar `
    $archiveImage `
    -tzf /backup/phase2-control-state.tar.gz)
if ($LASTEXITCODE -ne 0) { throw "Safety control-state archive is not readable." }
$controlStateDetails = @(& docker run --rm --network none --read-only --cap-drop ALL `
    -v "${bundle}:/backup:ro" --entrypoint /bin/tar $archiveImage -tvzf /backup/phase2-control-state.tar.gz)
if ($LASTEXITCODE -ne 0) { throw "Safety archive type inspection failed." }
Assert-HaiStrictControlArchive $controlStateEntries $controlStateDetails

if ($ValidateOnly) {
    $environmentMessage = if ($ValidateOnly -and -not (Test-Path -LiteralPath $envPath)) {
        'Missing environment was validated in a protected temporary location and not installed.'
    } elseif ($environmentRestored) { 'Missing environment restored from the current-user protected bundle.' } else { 'Existing environment preserved.' }
    Write-Host "Restore preflight passed: checksums, integrity contract, archive paths/types, and Compose. $environmentMessage Data restore has not been rehearsed."
    exit 0
}

$suffix = [Guid]::NewGuid().ToString('N')
$scratchAutomation = "hai_restore_automation_$suffix".ToLowerInvariant()
$scratchIdentity = "hai_restore_identity_$suffix".ToLowerInvariant()
$scratchControlVolume = "018-hai-phase2-restore-drill-$suffix".ToLowerInvariant()
Assert-HaiScratchTargets $suffix $scratchAutomation $scratchIdentity $scratchControlVolume @($liveAutomation, $liveIdentity)

$automationTemp = "/tmp/$scratchAutomation.dump"
$identityTemp = "/tmp/$scratchIdentity.dump"
$mediaName = "hai-restore-media-$suffix"
$mediaParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$scratchMedia = Join-Path $mediaParent $mediaName
$ownedAutomation = $false
$ownedIdentity = $false
$ownedControl = $false
$ownedMedia = $false
$ownedAutomationTemp = $false
$ownedIdentityTemp = $false
$ownerToken = [Guid]::NewGuid().ToString('N')
$cleanupFailed = $false
$completed = $false
try {
    Assert-HaiOwnedDirectory $scratchMedia $mediaParent $mediaName
    New-Item -ItemType Directory -Path $scratchMedia | Out-Null
    $ownedMedia = $true
    [IO.Compression.ZipFile]::ExtractToDirectory((Join-Path $bundle "media.zip"), $scratchMedia)
    $media = [IO.Compression.ZipFile]::OpenRead((Join-Path $bundle "media.zip"))
    try { Assert-HaiMediaContents $media $scratchMedia } finally { $media.Dispose() }

    & docker volume inspect $scratchControlVolume 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) { throw "Safety-control scratch volume already exists; it is not owned by this drill." }
    & docker volume create --label "hai.recovery.run=$ownerToken" $scratchControlVolume | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not create the safety-control restore volume." }
    $volumeOwner = (& docker volume inspect --format '{{index .Labels "hai.recovery.run"}}' $scratchControlVolume 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $volumeOwner -cne $ownerToken) { throw "Could not establish exclusive scratch-volume ownership." }
    $ownedControl = $true
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
    Assert-HaiRecoveryEvidence $manifest.integrity.controls (Get-HaiControlDigests $scratchControlVolume $archiveImage) 'Safety controls'

    & docker exec 018-hai-postgres-automation /bin/sh -c "test ! -e '$automationTemp' && test ! -L '$automationTemp' && (set -C; : > '$automationTemp')" 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not exclusively reserve automation staging file." }
    $ownedAutomationTemp = $true
    & docker exec 018-hai-postgres-idp /bin/sh -c "test ! -e '$identityTemp' && test ! -L '$identityTemp' && (set -C; : > '$identityTemp')" 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not exclusively reserve identity staging file." }
    $ownedIdentityTemp = $true

    & docker cp (Join-Path $bundle "automation.dump") "018-hai-postgres-automation:$automationTemp"
    if ($LASTEXITCODE -ne 0) { throw "Could not stage the automation dump." }
    & docker cp (Join-Path $bundle "identity.dump") "018-hai-postgres-idp:$identityTemp"
    if ($LASTEXITCODE -ne 0) { throw "Could not stage the identity dump." }

    & docker exec 018-hai-postgres-automation createdb -U $dbUser --template=template0 -- $scratchAutomation 2>$null
    if ($LASTEXITCODE -ne 0) { throw "Could not create the automation scratch database." }
    $ownedAutomation = $true
    & docker exec 018-hai-postgres-automation pg_restore -U $dbUser --exit-on-error --no-owner --no-privileges -d $scratchAutomation $automationTemp 2>$null
    if ($LASTEXITCODE -ne 0) { throw "Automation restore drill failed." }

    & docker exec 018-hai-postgres-idp createdb -U $dbUser --template=template0 -- $scratchIdentity 2>$null
    if ($LASTEXITCODE -ne 0) { throw "Could not create the identity scratch database." }
    $ownedIdentity = $true
    & docker exec 018-hai-postgres-idp pg_restore -U $dbUser --exit-on-error --no-owner --no-privileges -d $scratchIdentity $identityTemp 2>$null
    if ($LASTEXITCODE -ne 0) { throw "Identity restore drill failed." }

    Assert-HaiRecoveryEvidence $manifest.integrity.automation (Get-HaiDatabaseIntegrity '018-hai-postgres-automation' $dbUser $scratchAutomation 'automation') 'Automation database'
    Assert-HaiRecoveryEvidence $manifest.integrity.identity (Get-HaiDatabaseIntegrity '018-hai-postgres-idp' $dbUser $scratchIdentity 'identity') 'Identity database'
    foreach ($target in @(@{ Container = '018-hai-postgres-automation'; Database = $scratchAutomation }, @{ Container = '018-hai-postgres-idp'; Database = $scratchIdentity })) {
        $owners = @(Invoke-HaiRecoveryQuery $target.Container $dbUser $target.Database "SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','S') AND c.relowner <> (SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user);")
        if ($owners.Count -ne 1 -or $owners[0] -ne '0') { throw "Restored tables and sequences are not owned by the selected restore role." }
    }
    $completed = $true
} finally {
    Assert-HaiScratchTargets $suffix $scratchAutomation $scratchIdentity $scratchControlVolume @($liveAutomation, $liveIdentity)
    if ($ownedAutomation) {
        & docker exec 018-hai-postgres-automation dropdb -U $dbUser -- $scratchAutomation 2>$null | Out-Null
        if ($LASTEXITCODE -ne 0) { $cleanupFailed = $true }
    }
    if ($ownedIdentity) {
        & docker exec 018-hai-postgres-idp dropdb -U $dbUser -- $scratchIdentity 2>$null | Out-Null
        if ($LASTEXITCODE -ne 0) { $cleanupFailed = $true }
    }
    if ($ownedAutomationTemp) {
        & docker exec 018-hai-postgres-automation rm -f $automationTemp 2>$null | Out-Null
        if ($LASTEXITCODE -ne 0) { $cleanupFailed = $true }
    }
    if ($ownedIdentityTemp) {
        & docker exec 018-hai-postgres-idp rm -f $identityTemp 2>$null | Out-Null
        if ($LASTEXITCODE -ne 0) { $cleanupFailed = $true }
    }
    if ($ownedControl) {
        $volumeOwner = (& docker volume inspect --format '{{index .Labels "hai.recovery.run"}}' $scratchControlVolume 2>$null | Out-String).Trim()
        if ($LASTEXITCODE -eq 0 -and $volumeOwner -ceq $ownerToken) {
            & docker volume rm $scratchControlVolume 2>$null | Out-Null
            if ($LASTEXITCODE -ne 0) { $cleanupFailed = $true }
        } else { $cleanupFailed = $true }
    }
    if ($ownedMedia) {
        try {
            Assert-HaiOwnedDirectory $scratchMedia $mediaParent $mediaName
            Remove-Item -LiteralPath $scratchMedia -Recurse -Force
        } catch { $cleanupFailed = $true }
    }
    if ($cleanupFailed) { Write-Warning "Owned recovery fixture cleanup failed; manual inspection is required. No success receipt will be emitted." }
}
if ($cleanupFailed -or -not $completed) { throw "Restore drill did not complete with clean owned-fixture cleanup." }
Write-Host "Restore integrity drill passed: canonical rows, columns, sequences, owner fields, media bytes, and safety controls match; owned fixtures removed. Application/runtime acceptance is still required."
