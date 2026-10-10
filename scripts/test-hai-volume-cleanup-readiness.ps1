param(
    [string]$RecoveryArchiveRoot = (Join-Path $env:LOCALAPPDATA 'HAI\volume-recovery')
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
Assert-HaiLocalDockerEngine

$knownVolumes = [ordered]@{
    '018-hai-postgres-automation-data' = 'postgres_logical_backup'
    '018-hai-postgres-idp-data' = 'postgres_logical_backup'
    '018-hai-phase2-control-state' = 'safety_control_archive'
    '018-hai-redpanda-data' = 'not_covered'
    '018-hai-kafka-kraft-data' = 'not_covered'
    '018-hai-ollama-local-data' = 'not_covered'
    '018-hai-redis-data' = 'not_covered'
}

$archiveSources = @(
    '018-hai-redpanda-data',
    '018-hai-kafka-kraft-data',
    '018-hai-ollama-local-data',
    '018-hai-redis-data'
)
$verifiedArchiveVolumes = @{}
$verifiedRecoveryArchives = [Collections.Generic.List[object]]::new()
$unverifiedRecoveryBundles = [Collections.Generic.List[object]]::new()
$archiveVerifier = Join-Path $PSScriptRoot 'verify-hai-detached-volume-archive.ps1'
$archiveRootFull = [IO.Path]::GetFullPath($RecoveryArchiveRoot)

if (Test-Path -LiteralPath $archiveRootFull -PathType Container) {
    $archiveRootItem = Get-Item -LiteralPath $archiveRootFull -Force
    if (($archiveRootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'Recovery archive root is a reparse point; volume cleanup readiness is blocked.'
    }

    foreach ($bundleDirectory in @(Get-ChildItem -LiteralPath $archiveRootFull -Directory -Force | Where-Object Name -Match '^hai-volume-recovery-[0-9a-f]{32}$')) {
        $bundleVolume = $null
        try {
            if (($bundleDirectory.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw 'reparse point'
            }
            Assert-HaiOwnedDirectory $bundleDirectory.FullName $archiveRootFull $bundleDirectory.Name
            Assert-HaiPrivateEnvironmentAcl $bundleDirectory.FullName -Directory

            $manifestPath = Join-Path $bundleDirectory.FullName 'manifest.json'
            if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'manifest missing' }
            $manifestItem = Get-Item -LiteralPath $manifestPath -Force
            if (($manifestItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $manifestItem.Length -gt 65536) {
                throw 'manifest is not a bounded regular file'
            }
            Assert-HaiPrivateEnvironmentAcl $manifestPath
            $manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
            $bundleVolume = [string]$manifest.sourceVolume.name
            if ($bundleVolume -cnotin $archiveSources) { throw 'unsupported source volume' }

            $verificationOutput = @(& $archiveVerifier -VolumeName $bundleVolume -BundlePath $bundleDirectory.FullName -ArchiveRoot $archiveRootFull)
            $verification = [string]::Join([Environment]::NewLine, [string[]]$verificationOutput) | ConvertFrom-Json -ErrorAction Stop
            if ([string]$verification.result -cne 'current_source_matches_verified_recovery_archive' -or
                $verification.safeToRemove -ne $false -or $verification.cleanupAuthorized -ne $false) {
                throw 'archive verifier did not return the fail-closed success contract'
            }

            $verifiedArchiveVolumes[$bundleVolume] = $true
            $verifiedRecoveryArchives.Add([pscustomobject][ordered]@{
                volume = $bundleVolume
                bundle_id = $bundleDirectory.Name
                archive_bytes = [long]$manifest.artifact.bytes
                archive_sha256 = [string]$verification.archiveSha256
                source_matches = $true
                safe_to_remove = $false
            })
        } catch {
            $unverifiedRecoveryBundles.Add([pscustomobject][ordered]@{
                bundle_id = $bundleDirectory.Name
                source_volume = $bundleVolume
                disposition = 'retain_unverified'
            })
        }
    }
}

function Invoke-DockerInventory([string[]]$Arguments) {
    $global:LASTEXITCODE = 0
    $lines = @(& docker @Arguments 2>$null)
    if ($LASTEXITCODE -ne 0) { throw "Docker inventory query failed: docker $($Arguments -join ' ')" }
    return @($lines | ForEach-Object { ([string]$_).Trim() } | Where-Object { $_ })
}

$names = @(Invoke-DockerInventory @('volume', 'ls', '--format', '{{.Name}}') | Where-Object { $_.StartsWith('018-hai-', [StringComparison]::Ordinal) } | Sort-Object)
$unknownNames = @($names | Where-Object { -not $knownVolumes.Contains([string]$_) })
$inventory = foreach ($name in $names) {
    $references = @(Invoke-DockerInventory @('ps', '-a', '--filter', "volume=$name", '--format', '{{.Names}}|{{.Status}}'))
    $coverage = [string]$knownVolumes[$name]
    $archiveVerified = $verifiedArchiveVolumes.ContainsKey([string]$name)
    if ($coverage -ceq 'not_covered' -and $archiveVerified) {
        $coverage = 'verified_detached_archive'
    }
    $recoveryStatus = if ($archiveVerified) {
        'verified_current_source_archive'
    } elseif ($coverage -in @('postgres_logical_backup', 'safety_control_archive')) {
        'supported_workflow_not_run_in_this_check'
    } else {
        'no_verified_recovery_archive'
    }
    $disposition = if ($references.Count -gt 0) {
        'hold_attached_to_container'
    } elseif ($coverage -ceq 'verified_detached_archive') {
        'hold_retention_decision'
    } elseif ($coverage -eq 'not_covered') {
        if (@($unverifiedRecoveryBundles | Where-Object source_volume -CEQ $name).Count -gt 0) {
            'hold_unverified_archive_bundle'
        } else {
            'hold_no_verified_archive_restore'
        }
    } else {
        'hold_until_complete_bundle_and_restore_drill'
    }
    [pscustomobject][ordered]@{
        volume = $name
        recovery_method = $coverage
        recovery_status = $recoveryStatus
        recovery_verified_this_run = $archiveVerified
        container_reference_count = $references.Count
        container_references = $references
        disposition = $disposition
        safe_to_remove = $false
    }
}

$imageRows = @(Invoke-DockerInventory @('image', 'ls', '--all', '--no-trunc', '--format', '{{.Repository}}:{{.Tag}}|{{.ID}}|{{.Size}}') | Where-Object { $_ -match '^018-hai-' })
$images = foreach ($row in $imageRows) {
    $parts = $row -split '\|', 3
    if ($parts.Count -ne 3 -or $parts[1] -notmatch '^sha256:[0-9a-f]{64}$') {
        throw 'HAI image inventory returned an invalid image identity; cleanup remains blocked.'
    }
    $references = @(Invoke-DockerInventory @('ps', '-a', '--filter', "ancestor=$($parts[1])", '--format', '{{.Names}}|{{.Status}}'))
    [pscustomobject][ordered]@{
        reference = [string]$parts[0]
        image_id = [string]$parts[1]
        size = [string]$parts[2]
        container_reference_count = $references.Count
        container_references = $references
        disposition = if ($references.Count -gt 0) { 'hold_container_reference' } else { 'hold_retention_review' }
        safe_to_remove = $false
    }
}
$anonymousMounts = @()
$haiContainerNames = @(Invoke-DockerInventory @('ps', '-a', '--filter', 'name=018-hai-', '--format', '{{.Names}}') | Sort-Object -Unique)
foreach ($container in $haiContainerNames) {
    $inspectJson = @(Invoke-DockerInventory @('inspect', '--format', '{{json .Mounts}}', $container))
    if ($inspectJson.Count -ne 1) { throw "Could not inspect mount inventory for HAI container '$container'." }
    $mounts = @($inspectJson[0] | ConvertFrom-Json)
    foreach ($mount in $mounts) {
        if ([string]$mount.Type -ceq 'volume' -and [string]$mount.Name -and
            -not ([string]$mount.Name).StartsWith('018-hai-', [StringComparison]::Ordinal)) {
            $anonymousMounts += [pscustomobject]@{ container = $container; volume = [string]$mount.Name }
        }
    }
}

$unsupportedRecoveryMethods = @($inventory | Where-Object { $_.recovery_method -eq 'not_covered' }).Count
$unverifiedRecoveryCount = @($inventory | Where-Object { -not $_.recovery_verified_this_run }).Count
$result = [pscustomobject][ordered]@{
    docker_context = (& docker context show 2>$null | Out-String).Trim()
    inventory_complete = ($unknownNames.Count -eq 0)
    named_hai_volumes = $inventory.Count
    unknown_hai_volumes = $unknownNames
    volumes_without_supported_recovery_method = $unsupportedRecoveryMethods
    volumes_without_current_verified_recovery_evidence = $unverifiedRecoveryCount
    verified_recovery_archives = @($verifiedRecoveryArchives)
    unverified_recovery_bundles = @($unverifiedRecoveryBundles)
    anonymous_hai_volume_mounts = $anonymousMounts
    hai_images = $images
    image_cleanup_authorized = $false
    safe_to_remove_any = $false
    deletion_performed = $false
    volumes = @($inventory)
}
$result | ConvertTo-Json -Depth 8

if ($unknownNames.Count -gt 0) { exit 2 }
