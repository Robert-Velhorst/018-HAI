[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [Parameter(Mandatory)]
    [ValidateSet('018-hai-kafka-kraft-data', '018-hai-ollama-local-data', '018-hai-redis-data', '018-hai-redpanda-data', '018-hai-postgres-automation-data', '018-hai-postgres-idp-data', '018-hai-phase2-control-state')]
    [string[]]$VolumeNames,
    [string]$RecoveryArchiveRoot = (Join-Path $env:LOCALAPPDATA 'HAI\volume-recovery'),
    [switch]$Apply,
    [string]$ConfirmationPhrase = '',
    [switch]$AllowPersistentDataRemoval
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
$readinessScript = Join-Path $PSScriptRoot 'test-hai-volume-cleanup-readiness.ps1'
$verifierScript = Join-Path $PSScriptRoot 'verify-hai-detached-volume-archive.ps1'
$archiveRootFull = [IO.Path]::GetFullPath($RecoveryArchiveRoot).TrimEnd('\')
$requestedVolumes = @($VolumeNames | Sort-Object -Unique)
if ($requestedVolumes.Count -ne $VolumeNames.Count) { throw 'Duplicate volume names are not allowed.' }
$persistentVolumeNames = @('018-hai-postgres-automation-data', '018-hai-postgres-idp-data', '018-hai-phase2-control-state')
$requestedPersistentVolumes = @($requestedVolumes | Where-Object { $persistentVolumeNames -ccontains $_ })
if ($requestedPersistentVolumes.Count -gt 0 -and -not $AllowPersistentDataRemoval) {
    throw 'Persistent HAI data volumes require -AllowPersistentDataRemoval in addition to the exact persistent-volume confirmation phrase.'
}

function Invoke-HaiVolumeCleanupDocker([string[]]$Arguments) {
    $global:LASTEXITCODE = 0
    $output = @(& docker @Arguments 2>$null | ForEach-Object { [string]$_ })
    if ($LASTEXITCODE -ne 0) { throw "Docker inventory or removal failed: docker $($Arguments -join ' ')" }
    return $output
}

function Get-HaiVolumeCleanupReadiness {
    $output = @(& $readinessScript)
    if ($output.Count -eq 0) { throw 'Volume readiness returned no report.' }
    $report = ($output -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
    if ($report.inventory_complete -ne $true -or $report.deletion_performed -ne $false -or
        $report.safe_to_remove_any -ne $false -or $report.image_cleanup_authorized -ne $false) {
        throw 'Volume readiness was incomplete or performed an unexpected mutation.'
    }
    return $report
}

function Get-HaiVerifiedBundle([string]$VolumeName, $Readiness) {
    $verifiedRows = @($Readiness.verified_recovery_archives | Where-Object { [string]$_.volume -ceq $VolumeName })
    if ($verifiedRows.Count -ne 1 -or [string]$verifiedRows[0].source_matches -cne 'True') {
        throw "No unique current-source-verified recovery archive exists for '$VolumeName'."
    }
    $bundleName = [string]$verifiedRows[0].bundle_id
    if ($bundleName -cnotmatch '^hai-volume-recovery-[0-9a-f]{32}$') {
        throw 'Verified archive bundle identity is invalid.'
    }
    $bundlePath = Join-Path $archiveRootFull $bundleName
    $resolvedBundle = (Resolve-Path -LiteralPath $bundlePath -ErrorAction Stop).Path
    if ([IO.Path]::GetDirectoryName($resolvedBundle).TrimEnd('\') -cne $archiveRootFull -or
        [IO.Path]::GetFileName($resolvedBundle) -cne $bundleName) {
        throw 'Verified recovery bundle is not an exact child of the configured archive root.'
    }
    $null = Assert-HaiOwnedDirectory $resolvedBundle $archiveRootFull $bundleName
    $null = Assert-HaiPrivateEnvironmentAcl $resolvedBundle -Directory
    return [pscustomobject]@{ bundle = $resolvedBundle; metadata = $verifiedRows[0] }
}

function Assert-HaiVolumeRecovery([string]$VolumeName, $Readiness) {
    $bundle = Get-HaiVerifiedBundle $VolumeName $Readiness
    $verifyOutput = @(& $verifierScript -VolumeName $VolumeName -BundlePath $bundle.bundle -ArchiveRoot $archiveRootFull)
    if ($LASTEXITCODE -ne 0 -or $verifyOutput.Count -eq 0) { throw "Recovery verification failed for '$VolumeName'." }
    $verification = ($verifyOutput -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
    if ([string]$verification.result -cne 'current_source_matches_verified_recovery_archive' -or
        [string]$verification.sourceVolume -cne $VolumeName -or
        $verification.sourceDetached -ne $true -or
        $verification.sourceVolumeRemoved -ne $false -or
        $verification.safeToRemove -ne $false -or
        $verification.cleanupAuthorized -ne $false) {
        throw "Recovery verifier did not satisfy the detached-volume contract for '$VolumeName'."
    }
    $manifestPath = Join-Path $bundle.bundle 'manifest.json'
    $null = Assert-HaiPrivateEnvironmentAcl $manifestPath
    $manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
    if ([string]$manifest.sourceVolume.name -cne $VolumeName -or
        [string]$manifest.artifact.sha256 -cne [string]$verification.archiveSha256 -or
        [long]$manifest.artifact.bytes -le 0) {
        throw "Recovery manifest identity does not match the verifier for '$VolumeName'."
    }
    return [pscustomobject]@{
        volume = $VolumeName
        bundle_id = [IO.Path]::GetFileName($bundle.bundle)
        archive_bytes = [long]$manifest.artifact.bytes
        archive_sha256 = [string]$verification.archiveSha256
        source_detached = $true
        source_matches = $true
    }
}

function Get-HaiVerifiedSummary([string]$VolumeName, $Readiness) {
    $bundle = Get-HaiVerifiedBundle $VolumeName $Readiness
    $metadata = $bundle.metadata
    if ([string]$metadata.source_matches -cne 'True' -or $metadata.safe_to_remove -ne $false -or
        [long]$metadata.archive_bytes -le 0 -or
        [string]$metadata.archive_sha256 -cnotmatch '^[0-9a-f]{64}$') {
        throw "Current source/restore evidence is incomplete for '$VolumeName'."
    }
    return [pscustomobject]@{
        volume = $VolumeName
        bundle_id = [string]$metadata.bundle_id
        archive_bytes = [long]$metadata.archive_bytes
        archive_sha256 = [string]$metadata.archive_sha256
        source_detached = $true
        source_matches = $true
    }
}

function Assert-HaiVolumeContext([string]$ExpectedContext) {
    $null = Assert-HaiLocalDockerEngine
    $context = (Invoke-HaiVolumeCleanupDocker @('context', 'show') | Out-String).Trim()
    if ($context -cne $ExpectedContext) { throw 'Docker context changed during volume cleanup; refusing to mutate another engine.' }
}

Assert-HaiLocalDockerEngine
$contextName = (Invoke-HaiVolumeCleanupDocker @('context', 'show') | Out-String).Trim()
$requiredPhrase = if ($requestedPersistentVolumes.Count -gt 0) {
    "REMOVE HAI PERSISTENT VOLUMES $($requestedVolumes -join ',')"
} else {
    "REMOVE HAI DETACHED VOLUMES $($requestedVolumes.Count)"
}
$rootItem = Get-Item -LiteralPath $archiveRootFull -Force
if (-not $rootItem.PSIsContainer -or ($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
    throw 'Recovery archive root is not a regular directory.'
}

if (-not $Apply) {
    try {
        $readiness = Get-HaiVolumeCleanupReadiness
        $verified = foreach ($name in $requestedVolumes) { Get-HaiVerifiedSummary $name $readiness }
        [pscustomobject][ordered]@{
            mode = 'dry_run'
            docker_context = $contextName
            eligible_volumes = @($verified)
            eligible_count = @($verified).Count
            confirmation_phrase = $requiredPhrase
            cleanup_requires_apply = $true
            deletion_performed = $false
        } | ConvertTo-Json -Depth 5
    } catch {
        [pscustomobject][ordered]@{
            mode = 'blocked'
            requested_volumes = $requestedVolumes
            docker_context = $contextName
            blocker = [string]$_.Exception.Message
            deletion_performed = $false
        } | ConvertTo-Json -Depth 4
    }
    return
}

if ($ConfirmationPhrase -cne $requiredPhrase) { throw "Confirmation phrase mismatch. Required phrase: $requiredPhrase" }
$applyReadiness = Get-HaiVolumeCleanupReadiness
$verifiedBeforeRemoval = foreach ($name in $requestedVolumes) { Get-HaiVerifiedSummary $name $applyReadiness }
Assert-HaiVolumeContext $contextName
if (-not $PSCmdlet.ShouldProcess(($requestedVolumes -join ', '), 'Remove only detached HAI volumes with verified current-source restore archives')) {
    [pscustomobject][ordered]@{ mode = 'not_applied'; eligible_volumes = @($verifiedBeforeRemoval); deletion_performed = $false } | ConvertTo-Json -Depth 5
    return
}

$removed = [Collections.Generic.List[object]]::new()
$failure = $null
foreach ($name in $requestedVolumes) {
    try {
        Assert-HaiVolumeContext $contextName
        $verified = Assert-HaiVolumeRecovery $name $applyReadiness
        if ($verified.archive_sha256 -cne [string](@($verifiedBeforeRemoval | Where-Object volume -CEQ $name)[0].archive_sha256)) {
            throw 'Verified recovery archive identity changed during cleanup.'
        }
        $null = Invoke-HaiVolumeCleanupDocker @('volume', 'rm', $name)
        $remaining = @(Invoke-HaiVolumeCleanupDocker @('volume', 'ls', '--format', '{{.Name}}') | Where-Object { $_ -ceq $name })
        if ($remaining.Count -ne 0) { throw "Docker did not remove the exact selected volume '$name'." }
        $removed.Add([pscustomobject]@{ volume = $name; archive_bundle = $verified.bundle_id; archive_bytes = $verified.archive_bytes })
    } catch {
        $failure = 'A selected volume failed its final verify/remove/postflight step; inspect the exact local Docker state before retrying.'
        break
    }
}

$remainingSelected = @(Invoke-HaiVolumeCleanupDocker @('volume', 'ls', '--format', '{{.Name}}') | Where-Object { $requestedVolumes -ccontains $_ })
$result = [pscustomobject][ordered]@{
    mode = if ($failure) { 'partial_failure' } else { 'apply' }
    removed_volumes = $removed.Count
    removed = @($removed)
    remaining_selected_volumes = $remainingSelected
    deletion_performed = ($removed.Count -gt 0)
    postflight_verified = ($null -eq $failure -and $remainingSelected.Count -eq 0)
    failure = $failure
}
$result | ConvertTo-Json -Depth 5
if ($failure) { throw $failure }
