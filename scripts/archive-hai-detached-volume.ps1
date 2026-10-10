[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet('018-hai-kafka-kraft-data', '018-hai-ollama-local-data', '018-hai-redis-data', '018-hai-redpanda-data')]
    [string]$VolumeName,
    [string]$ArchiveRoot = (Join-Path $env:LOCALAPPDATA 'HAI\volume-recovery'),
    [switch]$LibraryOnly
)

$ErrorActionPreference = 'Stop'

function Get-HaiDetachedVolumeInspect([string]$Name) {
    $output = @(& docker volume inspect --format '{{json .}}' $Name 2>$null)
    if ($LASTEXITCODE -ne 0 -or $output.Count -ne 1) {
        throw 'Could not inspect the selected Docker volume; no source volume was changed.'
    }
    try { return $output[0] | ConvertFrom-Json -ErrorAction Stop }
    catch { throw 'Selected Docker volume metadata was invalid; no source volume was changed.' }
}

function Assert-HaiDetachedVolume([string]$Name, $ExpectedCreatedAt, $ExpectedDriver) {
    $references = @(& docker ps -a --filter "volume=$Name" --format '{{.ID}}|{{.Names}}' 2>$null)
    if ($LASTEXITCODE -ne 0) { throw 'Could not inventory volume attachments; refusing an online or ambiguous archive.' }
    if (@($references | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) }).Count -gt 0) {
        throw "Docker volume '$Name' is attached to a container; only detached legacy volumes can be archived by this tool."
    }
    $current = Get-HaiDetachedVolumeInspect $Name
    if ([string]$current.Name -cne $Name -or [string]$current.Driver -cne $ExpectedDriver -or
        [string]$current.CreatedAt -cne [string]$ExpectedCreatedAt) {
        throw 'Docker volume identity changed during archive; the recovery bundle is incomplete.'
    }
    return $current
}

function Get-HaiDockerImageId([string]$Image) {
    $output = @(& docker image inspect --format '{{.Id}}' $Image 2>$null)
    if ($LASTEXITCODE -ne 0 -or $output.Count -ne 1 -or [string]$output[0] -cnotmatch '^sha256:[0-9a-f]{64}$') {
        throw 'The pinned local HAI archive image is unavailable; refusing to pull or substitute another image.'
    }
    return [string]$output[0]
}

function Assert-HaiNoPathReparsePoints([string]$Path) {
    $cursor = [IO.DirectoryInfo]::new([IO.Path]::GetFullPath($Path))
    while ($null -ne $cursor) {
        if (Test-Path -LiteralPath $cursor.FullName) {
            $item = Get-Item -LiteralPath $cursor.FullName -Force
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or -not $item.PSIsContainer) {
                throw 'Archive path must not traverse a reparse point or non-directory.'
            }
        }
        $cursor = $cursor.Parent
    }
}

function Invoke-HaiArchiveContainer([string[]]$Arguments, [string]$FailureMessage) {
    $null = & docker @Arguments 2>$null
    if ($LASTEXITCODE -ne 0) { throw $FailureMessage }
}

function Assert-HaiScratchVolumeOwnership([string]$Name, [string]$Owner) {
    $metadata = Get-HaiDetachedVolumeInspect $Name
    if ($metadata.Name -cne $Name -or
        [string]$metadata.Labels.'hai.cleanup.owner' -cne $Owner -or
        [string]$metadata.Labels.'hai.cleanup.kind' -cne 'restore-drill') {
        throw 'Restore-drill volume ownership could not be proven; it was left untouched.'
    }
    $references = @(& docker ps -a --filter "volume=$Name" --format '{{.ID}}' 2>$null)
    if ($LASTEXITCODE -ne 0 -or @($references | Where-Object { $_ }).Count -gt 0) {
        throw 'Restore-drill volume is attached or its attachment inventory failed; it was left untouched.'
    }
}

if ($LibraryOnly -or $MyInvocation.InvocationName -eq '.') { return }

. (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
Assert-HaiLocalDockerEngine

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw 'Docker Desktop is required.' }
$archiveImage = Get-HaiDockerImageId '018-hai-backend:local'
$source = Get-HaiDetachedVolumeInspect $VolumeName
if ([string]$source.Name -cne $VolumeName -or [string]$source.Driver -cne 'local') {
    throw 'Only the exact known local HAI legacy volumes are supported.'
}
$sourceCreatedAt = [string]$source.CreatedAt
$sourceDriver = [string]$source.Driver
Assert-HaiDetachedVolume $VolumeName $sourceCreatedAt $sourceDriver | Out-Null

$archiveFullRoot = [IO.Path]::GetFullPath($ArchiveRoot)
Assert-HaiNoPathReparsePoints $archiveFullRoot
if (-not (Test-Path -LiteralPath $archiveFullRoot -PathType Container)) {
    New-Item -ItemType Directory -Path $archiveFullRoot -Force | Out-Null
}
$owner = [Guid]::NewGuid().ToString('N')
$bundleName = 'hai-volume-recovery-' + $owner
$bundle = Join-Path $archiveFullRoot $bundleName
Assert-HaiOwnedDirectory $bundle $archiveFullRoot $bundleName
$sid = [Security.Principal.SecurityIdentifier]::new((Get-HaiWindowsUserSid))
$bundleSecurity = New-HaiPrivateDirectorySecurity $sid
$null = New-HaiPrivateEnvironmentDirectory $bundle $bundleSecurity
Assert-HaiPrivateEnvironmentAcl $bundle -Directory

$artifact = $VolumeName + '.tar.gz'
$archivePath = Join-Path $bundle $artifact
$scratchVolume = '018-hai-restore-drill-' + $owner
$scratchCreated = $false
$restoreVerified = $false
$cleanupFailed = $false
try {
    $existingVolumes = @(& docker volume ls --format '{{.Name}}' 2>$null)
    if ($LASTEXITCODE -ne 0) { throw 'Could not inventory Docker volumes before creating an isolated restore target.' }
    if (@($existingVolumes | Where-Object { [string]$_ -ceq $scratchVolume }).Count -gt 0) {
        throw 'Unique restore-drill volume name already exists; no existing volume will be reused.'
    }

    $archiveArgs = @(
        'run', '--rm', '--pull=never', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--user', '0:0',
        '--mount', "type=volume,source=$VolumeName,target=/source,readonly",
        '--mount', "type=bind,source=$bundle,target=/backup",
        '--entrypoint', '/bin/tar', $archiveImage,
        '-czf', "/backup/$artifact", '-C', '/source', '.'
    )
    Invoke-HaiArchiveContainer $archiveArgs 'Detached volume archive failed; the source volume was not modified.'
    if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) { throw 'Docker reported success without creating the expected archive.' }

    $listArgs = @(
        'run', '--rm', '--pull=never', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--user', '0:0',
        '--mount', "type=bind,source=$bundle,target=/backup,readonly",
        '--entrypoint', '/bin/tar', $archiveImage, '-tzf', "/backup/$artifact"
    )
    Invoke-HaiArchiveContainer $listArgs 'Volume archive integrity check failed.'

    $labels = @('--label', "hai.cleanup.owner=$owner", '--label', 'hai.cleanup.kind=restore-drill')
    $createArgs = @('volume', 'create') + $labels + @($scratchVolume)
    $createOutput = @(& docker @createArgs 2>$null)
    $createExitCode = $LASTEXITCODE
    if ($createExitCode -eq 0) {
        $scratchCreated = $true
    } else {
        $existingAfterCreate = @(& docker volume ls --format '{{.Name}}' 2>$null)
        if ($LASTEXITCODE -eq 0 -and @($existingAfterCreate | Where-Object { [string]$_ -ceq $scratchVolume }).Count -eq 1) {
            try { Assert-HaiScratchVolumeOwnership $scratchVolume $owner; $scratchCreated = $true }
            catch { $scratchCreated = $false }
        }
    }
    if ($createExitCode -ne 0 -or $createOutput.Count -ne 1 -or [string]$createOutput[0] -cne $scratchVolume) {
        throw 'Could not create the uniquely owned isolated restore-drill volume.'
    }
    Assert-HaiScratchVolumeOwnership $scratchVolume $owner

    $restoreArgs = @(
        'run', '--rm', '--pull=never', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--user', '0:0',
        '--mount', "type=bind,source=$bundle,target=/backup,readonly",
        '--mount', "type=volume,source=$scratchVolume,target=/restore",
        '--entrypoint', '/bin/tar', $archiveImage, '-xzf', "/backup/$artifact", '-C', '/restore'
    )
    Invoke-HaiArchiveContainer $restoreArgs 'Restore drill extraction failed; the source volume was not modified.'

    $compareArgs = @(
        'run', '--rm', '--pull=never', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--user', '0:0',
        '--mount', "type=bind,source=$bundle,target=/backup,readonly",
        '--mount', "type=volume,source=$scratchVolume,target=/restore,readonly",
        '--entrypoint', '/bin/tar', $archiveImage, '-dzf', "/backup/$artifact", '-C', '/restore'
    )
    Invoke-HaiArchiveContainer $compareArgs 'Restored volume contents do not match the archive.'

    $sourceCompareArgs = @(
        'run', '--rm', '--pull=never', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--user', '0:0',
        '--mount', "type=bind,source=$bundle,target=/backup,readonly",
        '--mount', "type=volume,source=$VolumeName,target=/source,readonly",
        '--entrypoint', '/bin/tar', $archiveImage, '-dzf', "/backup/$artifact", '-C', '/source'
    )
    Invoke-HaiArchiveContainer $sourceCompareArgs 'Source volume changed during archive; cleanup eligibility was not established.'
    $restoreVerified = $true
} finally {
    if ($scratchCreated) {
        try {
            Assert-HaiScratchVolumeOwnership $scratchVolume $owner
            $null = & docker volume rm $scratchVolume 2>$null
            if ($LASTEXITCODE -ne 0) { throw 'Docker refused to remove the owned restore-drill volume.' }
            $remaining = @(& docker volume ls --format '{{.Name}}' 2>$null)
            if ($LASTEXITCODE -ne 0 -or @($remaining | Where-Object { [string]$_ -ceq $scratchVolume }).Count -ne 0) {
                throw 'Restore-drill volume removal could not be verified.'
            }
        } catch {
            $cleanupFailed = $true
            Write-Warning 'Owned restore-drill cleanup could not be verified; inspect the exact generated volume name before any cleanup.'
        }
    }
}

if ($cleanupFailed -or -not $restoreVerified) { throw 'Volume archive did not pass its restore drill and owned scratch cleanup.' }
Assert-HaiDetachedVolume $VolumeName $sourceCreatedAt $sourceDriver | Out-Null
$archiveItem = Get-Item -LiteralPath $archivePath -Force
if (($archiveItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Generated volume archive is a reparse point.' }
$manifest = [ordered]@{
    formatVersion = 1
    createdUtc = [DateTime]::UtcNow.ToString('o')
    owner = $owner
    sourceVolume = [ordered]@{ name = $VolumeName; driver = $sourceDriver; createdAt = $sourceCreatedAt }
    archiveImageId = $archiveImage
    artifact = [ordered]@{ name = $artifact; bytes = [long]$archiveItem.Length; sha256 = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant() }
    restoreDrill = 'passed'
    sourceUnchangedAfterArchive = $true
    scratchVolumeRemoved = $true
    sourceVolumeRemoved = $false
} | ConvertTo-Json -Depth 8
$manifestPath = Join-Path $bundle 'manifest.json'
if (Test-Path -LiteralPath $manifestPath) { throw 'An unexpected manifest already exists in the owned bundle.' }
$manifestTempPath = Join-Path $bundle 'manifest.tmp'
$manifestBytes = [Text.UTF8Encoding]::new($false).GetBytes($manifest)
$stream = [IO.File]::Open($manifestTempPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
try { $stream.Write($manifestBytes, 0, $manifestBytes.Length); $stream.Flush($true) } finally { $stream.Dispose() }
[Array]::Clear($manifestBytes, 0, $manifestBytes.Length)
[IO.File]::Move($manifestTempPath, $manifestPath)
Assert-HaiPrivateEnvironmentAcl $manifestPath
Assert-HaiPrivateEnvironmentAcl $archivePath
Write-Output ([pscustomobject][ordered]@{
    result = 'archive_and_restore_drill_passed'
    sourceVolume = $VolumeName
    bundle = $bundle
    bytes = [long]$archiveItem.Length
    sourceVolumeRemoved = $false
    cleanupAuthorized = $false
})
