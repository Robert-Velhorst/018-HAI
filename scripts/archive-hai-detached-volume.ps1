[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet('018-hai-kafka-kraft-data', '018-hai-ollama-local-data', '018-hai-redis-data', '018-hai-redpanda-data', '018-hai-postgres-automation-data', '018-hai-postgres-idp-data', '018-hai-phase2-control-state')]
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
    $output = @(& docker @Arguments 2>&1 | ForEach-Object { [string]$_ })
    $exitCode = $LASTEXITCODE
    if ($exitCode -ne 0) {
        $diagnostics = @($output | ForEach-Object {
            $line = [string]$_
            if ($line -match '(?i)permission denied') { 'permission denied' }
            elseif ($line -match '(?i)cannot change ownership|cannot set ownership|chown failed') { 'ownership metadata restore denied' }
            elseif ($line -match '(?i)cannot change mode|cannot set permissions|chmod failed') { 'permission metadata restore denied' }
            elseif ($line -match '(?i)cannot set times|cannot change times|utime failed') { 'timestamp metadata restore denied' }
            elseif ($line -match '(?i)cannot create|cannot make directory|cannot link|cannot mknod') { 'filesystem object restore denied' }
            elseif ($line -match '(?i)operation not permitted') { 'operation not permitted' }
            elseif ($line -match '(?i)read-only file system') { 'read-only filesystem' }
            elseif ($line -match '(?i)no space left|disk quota exceeded') { 'insufficient storage' }
            elseif ($line -match '(?i)no such file or directory') { 'missing file or path' }
            elseif ($line -match '(?i)failed to (create|start) task|mounts denied|invalid mount config') { 'container mount/runtime failure' }
        } | Where-Object { $_ } | Sort-Object -Unique)
        $detail = if ($diagnostics.Count -gt 0) { $diagnostics -join ', ' } else { 'container command failed' }
        throw "$FailureMessage (exit code $exitCode; $detail)."
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
$restoreVerified = $false
$fileSecurity = New-HaiPrivateFileSecurity $sid
$archiveStream = New-HaiPrivateEnvironmentFile $archivePath $fileSecurity
$archiveStream.Dispose()
Assert-HaiPrivateEnvironmentAcl $archivePath
$archiveArgs = @(
        'run', '--rm', '--pull=never', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--cap-add', 'DAC_READ_SEARCH', '--user', '0:0',
        '--mount', "type=volume,source=$VolumeName,target=/source,readonly",
        '--mount', "type=bind,source=$bundle,target=/backup",
        '--entrypoint', '/bin/tar', $archiveImage,
        '-czf', "/backup/$artifact", '-C', '/source', '.'
    )
    Invoke-HaiArchiveContainer $archiveArgs 'Detached volume archive failed; the source volume was not modified.'
    if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) { throw 'Docker reported success without creating the expected archive.' }

    $listArgs = @(
        'run', '--rm', '--pull=never', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--cap-add', 'DAC_READ_SEARCH', '--user', '0:0',
        '--mount', "type=bind,source=$bundle,target=/backup,readonly",
        '--entrypoint', '/bin/tar', $archiveImage, '-tzf', "/backup/$artifact"
    )
    Invoke-HaiArchiveContainer $listArgs 'Volume archive integrity check failed.'

    $restoreScript = "tar -xzf '/backup/$artifact' -C /restore && tar -dzf '/backup/$artifact' -C /restore && tar -dzf '/backup/$artifact' -C /source"
    $restoreArgs = @(
        'run', '--rm', '--pull=never', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--cap-add', 'DAC_READ_SEARCH', '--cap-add', 'CHOWN', '--cap-add', 'FOWNER', '--user', '0:0',
        '--tmpfs', '/restore:rw,size=1g',
        '--mount', "type=volume,source=$VolumeName,target=/source,readonly",
        '--mount', "type=bind,source=$bundle,target=/backup,readonly",
        '--entrypoint', '/bin/sh', $archiveImage, '-ec', $restoreScript
    )
Invoke-HaiArchiveContainer $restoreArgs 'Restore drill extraction, restored-data comparison, or source consistency check failed; the source volume was not modified.'
$restoreVerified = $true

if (-not $restoreVerified) { throw 'Volume archive did not pass its bounded tmpfs restore drill.' }
Assert-HaiDetachedVolume $VolumeName $sourceCreatedAt $sourceDriver | Out-Null
$archiveItem = Get-Item -LiteralPath $archivePath -Force
if (($archiveItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Generated volume archive is a reparse point.' }
$manifest = [ordered]@{
    formatVersion = 2
    createdUtc = [DateTime]::UtcNow.ToString('o')
    owner = $owner
    sourceVolume = [ordered]@{ name = $VolumeName; driver = $sourceDriver; createdAt = $sourceCreatedAt }
    archiveImageId = $archiveImage
    artifact = [ordered]@{ name = $artifact; bytes = [long]$archiveItem.Length; sha256 = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant() }
    restoreDrill = 'passed'
    restoreTarget = 'tmpfs-1g'
    restoreTargetDisposed = $true
    sourceUnchangedAfterArchive = $true
    sourceVolumeRemoved = $false
} | ConvertTo-Json -Depth 8
$manifestPath = Join-Path $bundle 'manifest.json'
if (Test-Path -LiteralPath $manifestPath) { throw 'An unexpected manifest already exists in the owned bundle.' }
$manifestTempPath = Join-Path $bundle 'manifest.tmp'
$manifestBytes = [Text.UTF8Encoding]::new($false).GetBytes($manifest)
$stream = New-HaiPrivateEnvironmentFile $manifestTempPath $fileSecurity
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
    restoreTarget = 'tmpfs-1g'
    sourceVolumeRemoved = $false
    cleanupAuthorized = $false
})
