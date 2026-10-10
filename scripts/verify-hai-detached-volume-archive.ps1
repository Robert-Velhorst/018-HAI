[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet('018-hai-kafka-kraft-data', '018-hai-ollama-local-data', '018-hai-redis-data', '018-hai-redpanda-data')]
    [string]$VolumeName,
    [Parameter(Mandatory)]
    [string]$BundlePath,
    [string]$ArchiveRoot = (Join-Path $env:LOCALAPPDATA 'HAI\volume-recovery'),
    [switch]$LibraryOnly,
    [switch]$ArchiveOnly
)

$ErrorActionPreference = 'Stop'

function Invoke-HaiVolumeVerifierDocker([string[]]$Arguments, [string]$FailureMessage) {
    $output = @(& docker @Arguments 2>$null)
    if ($LASTEXITCODE -ne 0) { throw $FailureMessage }
    return $output
}

function Get-HaiVolumeVerifierMetadata([string]$Name) {
    $output = @(Invoke-HaiVolumeVerifierDocker @('volume', 'inspect', '--format', '{{json .}}', $Name) 'Could not inspect the source Docker volume.')
    if ($output.Count -ne 1) { throw 'Source Docker volume metadata is ambiguous.' }
    try { return $output[0] | ConvertFrom-Json -ErrorAction Stop }
    catch { throw 'Source Docker volume metadata is invalid.' }
}

function Assert-HaiVolumeVerifierDetached([string]$Name, $ExpectedMetadata) {
    $references = @(Invoke-HaiVolumeVerifierDocker @('ps', '-a', '--filter', "volume=$Name", '--format', '{{.ID}}|{{.Names}}') 'Could not inspect source-volume attachments.')
    if (@($references | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) }).Count -gt 0) {
        throw "Source volume '$Name' is attached; removal eligibility is not established."
    }
    $actual = Get-HaiVolumeVerifierMetadata $Name
    if ([string]$actual.Name -cne [string]$ExpectedMetadata.Name -or
        [string]$actual.Driver -cne [string]$ExpectedMetadata.Driver -or
        [string]$actual.CreatedAt -cne [string]$ExpectedMetadata.CreatedAt) {
        throw 'Source volume identity differs from the recovery manifest.'
    }
}

if ($LibraryOnly -or $MyInvocation.InvocationName -eq '.') { return }

. (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw 'Docker Desktop is required.' }
Assert-HaiLocalDockerEngine

$archiveRootFull = [IO.Path]::GetFullPath($ArchiveRoot)
$bundleFull = [IO.Path]::GetFullPath($BundlePath)
$ownerMatch = [regex]::Match([IO.Path]::GetFileName($bundleFull), '^hai-volume-recovery-([0-9a-f]{32})$')
if (-not $ownerMatch.Success -or
    [IO.Path]::GetDirectoryName($bundleFull).TrimEnd('\') -cne $archiveRootFull.TrimEnd('\')) {
    throw 'Bundle path is not an exact generated child of the configured recovery root.'
}
Assert-HaiOwnedDirectory $bundleFull $archiveRootFull ([IO.Path]::GetFileName($bundleFull))
Assert-HaiPrivateEnvironmentAcl $bundleFull -Directory
$manifestPath = Join-Path $bundleFull 'manifest.json'
if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'Completed recovery manifest is missing.' }
Assert-HaiPrivateEnvironmentAcl $manifestPath
$manifestItem = Get-Item -LiteralPath $manifestPath -Force
if (($manifestItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Recovery manifest is a reparse point.' }
try { $manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop }
catch { throw 'Recovery manifest is invalid JSON.' }

$formatVersion = [int]$manifest.formatVersion
$requiredManifestKeys = if ($formatVersion -eq 1) {
    @('formatVersion', 'createdUtc', 'owner', 'sourceVolume', 'archiveImageId', 'artifact', 'restoreDrill', 'sourceUnchangedAfterArchive', 'scratchVolumeRemoved', 'sourceVolumeRemoved')
} elseif ($formatVersion -eq 2) {
    @('formatVersion', 'createdUtc', 'owner', 'sourceVolume', 'archiveImageId', 'artifact', 'restoreDrill', 'restoreTarget', 'restoreTargetDisposed', 'sourceUnchangedAfterArchive', 'sourceVolumeRemoved')
} else {
    throw 'Recovery manifest format version is unsupported.'
}
if (@($manifest.PSObject.Properties.Name | Where-Object { $_ -cnotin $requiredManifestKeys }).Count -gt 0 -or
    @($manifest.PSObject.Properties.Name).Count -ne $requiredManifestKeys.Count -or
    [string]$manifest.owner -cne $ownerMatch.Groups[1].Value -or
    [string]$manifest.restoreDrill -cne 'passed' -or $manifest.sourceUnchangedAfterArchive -ne $true -or
    $manifest.sourceVolumeRemoved -ne $false -or
    ($formatVersion -eq 1 -and $manifest.scratchVolumeRemoved -ne $true) -or
    ($formatVersion -eq 2 -and ([string]$manifest.restoreTarget -cne 'tmpfs-1g' -or $manifest.restoreTargetDisposed -ne $true))) {
    throw 'Recovery manifest does not satisfy the detached-volume archive contract.'
}
if (@($manifest.sourceVolume.PSObject.Properties.Name | Where-Object { $_ -cnotin @('name', 'driver', 'createdAt') }).Count -gt 0 -or
    @($manifest.sourceVolume.PSObject.Properties.Name).Count -ne 3 -or
    [string]$manifest.sourceVolume.name -cne $VolumeName -or [string]$manifest.sourceVolume.driver -cne 'local' -or
    [string]$manifest.artifact.name -cne ($VolumeName + '.tar.gz') -or
    @($manifest.artifact.PSObject.Properties.Name | Where-Object { $_ -cnotin @('name', 'bytes', 'sha256') }).Count -gt 0 -or
    @($manifest.artifact.PSObject.Properties.Name).Count -ne 3 -or
    [long]$manifest.artifact.bytes -le 0 -or [string]$manifest.artifact.sha256 -cnotmatch '^[0-9a-f]{64}$' -or
    [string]$manifest.archiveImageId -cnotmatch '^sha256:[0-9a-f]{64}$') {
    throw 'Recovery manifest volume or artifact metadata is invalid.'
}

$archivePath = Join-Path $bundleFull ([string]$manifest.artifact.name)
if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) { throw 'Recovery archive file is missing.' }
Assert-HaiPrivateEnvironmentAcl $archivePath
$archiveItem = Get-Item -LiteralPath $archivePath -Force
if (($archiveItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
    [long]$archiveItem.Length -ne [long]$manifest.artifact.bytes -or
    (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$manifest.artifact.sha256) {
    throw 'Recovery archive bytes do not match the manifest.'
}

$imageId = (Invoke-HaiVolumeVerifierDocker @('image', 'inspect', '--format', '{{.Id}}', '018-hai-backend:local') 'Pinned local archive image is unavailable.') | Out-String
if ($imageId.Trim() -cne [string]$manifest.archiveImageId) { throw 'Pinned local archive image differs from the image used for the restore drill.' }

if ($ArchiveOnly) {
    $volumeNames = @(Invoke-HaiVolumeVerifierDocker @('volume', 'ls', '--format', '{{.Name}}') 'Could not verify that the archived source volume is absent.')
    if (@($volumeNames | Where-Object { [string]$_ -ceq $VolumeName }).Count -gt 0) {
        throw 'Archive-only verification is valid only after the exact source volume has been removed.'
    }
    $references = @(Invoke-HaiVolumeVerifierDocker @('ps', '-a', '--filter', "volume=$VolumeName", '--format', '{{.ID}}|{{.Names}}') 'Could not verify source-volume references.')
    if (@($references | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) }).Count -gt 0) {
        throw 'A container still references the archived source volume.'
    }
    [pscustomobject][ordered]@{
        result = 'verified_recovery_archive_source_removed'
        sourceVolume = $VolumeName
        archiveBundle = $bundleFull
        archiveSha256 = [string]$manifest.artifact.sha256
        archiveIntegrityVerified = $true
        restoreDrillRecorded = 'passed'
        sourceDetached = $true
        sourceVolumeRemoved = $true
        safeToRemove = $false
        cleanupAuthorized = $false
    } | ConvertTo-Json -Depth 4
    return
}

$sourceMetadata = Get-HaiVolumeVerifierMetadata $VolumeName
if ([string]$sourceMetadata.Driver -cne 'local' -or
    [string]$sourceMetadata.CreatedAt -cne [string]$manifest.sourceVolume.createdAt) {
    throw 'Current source volume identity differs from the archived source.'
}
Assert-HaiVolumeVerifierDetached $VolumeName $sourceMetadata

$compareArgs = @(
    'run', '--rm', '--pull=never', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--cap-add', 'DAC_READ_SEARCH', '--user', '0:0',
    '--mount', "type=bind,source=$bundleFull,target=/backup,readonly",
    '--mount', "type=volume,source=$VolumeName,target=/source,readonly",
    '--entrypoint', '/bin/tar', [string]$manifest.archiveImageId,
    '-dzf', "/backup/$($manifest.artifact.name)", '-C', '/source'
)
$null = Invoke-HaiVolumeVerifierDocker $compareArgs 'Current source volume no longer matches the verified recovery archive.'
Assert-HaiVolumeVerifierDetached $VolumeName $sourceMetadata

[pscustomobject][ordered]@{
    result = 'current_source_matches_verified_recovery_archive'
    sourceVolume = $VolumeName
    archiveBundle = $bundleFull
    archiveSha256 = [string]$manifest.artifact.sha256
    restoreDrill = 'passed'
    restoreTarget = if ($formatVersion -eq 1) { 'legacy-named-scratch-volume' } else { [string]$manifest.restoreTarget }
    sourceDetached = $true
    sourceVolumeRemoved = $false
    safeToRemove = $false
    cleanupAuthorized = $false
} | ConvertTo-Json -Depth 4
