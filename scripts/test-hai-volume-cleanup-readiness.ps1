param(
    [string]$RecoveryArchiveRoot = (Join-Path $env:LOCALAPPDATA 'HAI\volume-recovery')
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
Assert-HaiLocalDockerEngine

$knownVolumes = [ordered]@{
    '018-hai-postgres-automation-data' = 'offline_archive_and_restore_drill'
    '018-hai-postgres-idp-data' = 'offline_archive_and_restore_drill'
    '018-hai-phase2-control-state' = 'offline_archive_and_restore_drill'
    '018-hai-redpanda-data' = 'not_covered'
    '018-hai-kafka-kraft-data' = 'not_covered'
    '018-hai-ollama-local-data' = 'not_covered'
    '018-hai-redis-data' = 'not_covered'
}

$archiveSources = @(
    '018-hai-postgres-automation-data',
    '018-hai-postgres-idp-data',
    '018-hai-phase2-control-state',
    '018-hai-redpanda-data',
    '018-hai-kafka-kraft-data',
    '018-hai-ollama-local-data',
    '018-hai-redis-data'
)
$verifiedArchiveVolumes = @{}
$verifiedRecoveryArchives = [Collections.Generic.List[object]]::new()
$sourceRemovedRecoveryArchives = [Collections.Generic.List[object]]::new()
$unverifiedRecoveryBundles = [Collections.Generic.List[object]]::new()
$archiveVerifier = Join-Path $PSScriptRoot 'verify-hai-detached-volume-archive.ps1'
$archiveRootFull = [IO.Path]::GetFullPath($RecoveryArchiveRoot)

function Invoke-DockerInventory([string[]]$Arguments) {
    $global:LASTEXITCODE = 0
    $lines = @(& docker @Arguments 2>$null)
    if ($LASTEXITCODE -ne 0) { throw "Docker inventory query failed: docker $($Arguments -join ' ')" }
    return @($lines | ForEach-Object { ([string]$_).Trim() } | Where-Object { $_ })
}

$names = @(Invoke-DockerInventory @('volume', 'ls', '--format', '{{.Name}}') | Where-Object { $_.StartsWith('018-hai-', [StringComparison]::Ordinal) } | Sort-Object)

if (Test-Path -LiteralPath $archiveRootFull -PathType Container) {
    $archiveRootItem = Get-Item -LiteralPath $archiveRootFull -Force
    if (($archiveRootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'Recovery archive root is a reparse point; volume cleanup readiness is blocked.'
    }

    foreach ($bundleDirectory in @(Get-ChildItem -LiteralPath $archiveRootFull -Directory -Force | Where-Object Name -Match '^hai-volume-recovery-[0-9a-f]{32}$')) {
        $bundleVolume = $null
        $artifactSourceHint = $null
        $manifestPresent = $false
        $failureCode = 'bundle_validation_failed'
        $bundleBytes = 0L
        $artifactSha256 = $null
        try {
            if (($bundleDirectory.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                $failureCode = 'unsafe_bundle_path'
                throw 'reparse point'
            }
            Assert-HaiOwnedDirectory $bundleDirectory.FullName $archiveRootFull $bundleDirectory.Name
            try { Assert-HaiPrivateEnvironmentAcl $bundleDirectory.FullName -Directory }
            catch {
                $failureCode = 'bundle_directory_acl_unverified'
                throw 'bundle directory ACL is not private'
            }

            $manifestPath = Join-Path $bundleDirectory.FullName 'manifest.json'
            if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
                $failureCode = 'manifest_missing'
                throw 'manifest missing'
            }
            $manifestPresent = $true
            $manifestItem = Get-Item -LiteralPath $manifestPath -Force
            if (($manifestItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $manifestItem.Length -gt 65536) {
                $failureCode = 'invalid_manifest_file'
                throw 'manifest is not a bounded regular file'
            }
            try { Assert-HaiPrivateEnvironmentAcl $manifestPath }
            catch {
                $failureCode = 'manifest_acl_unverified'
                throw 'manifest ACL is not private'
            }
            try { $manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop }
            catch {
                $failureCode = 'invalid_manifest_json'
                throw 'manifest JSON is invalid'
            }
            $bundleVolume = [string]$manifest.sourceVolume.name
            if ($bundleVolume -cnotin $archiveSources) {
                $failureCode = 'unsupported_source_volume'
                throw 'unsupported source volume'
            }
            $artifactSourceHint = $bundleVolume
            $bundleBytes = [long]$manifest.artifact.bytes
            $artifactSha256 = [string]$manifest.artifact.sha256

            $archiveOnly = -not ($names -ccontains $bundleVolume)
            $verifierArguments = @{
                VolumeName = $bundleVolume
                BundlePath = $bundleDirectory.FullName
                ArchiveRoot = $archiveRootFull
            }
            if ($archiveOnly) { $verifierArguments.ArchiveOnly = $true }
            try { $verificationOutput = @(& $archiveVerifier @verifierArguments) }
            catch {
                $failureCode = 'archive_verification_failed'
                throw 'archive verification failed'
            }
            $verification = [string]::Join([Environment]::NewLine, [string[]]$verificationOutput) | ConvertFrom-Json -ErrorAction Stop
            $expectedVerificationResult = if ($archiveOnly) {
                'verified_recovery_archive_source_removed'
            } else {
                'current_source_matches_verified_recovery_archive'
            }
            if ([string]$verification.result -cne $expectedVerificationResult -or
                $verification.safeToRemove -ne $false -or $verification.cleanupAuthorized -ne $false -or
                ($archiveOnly -and ($verification.archiveIntegrityVerified -ne $true -or $verification.sourceVolumeRemoved -ne $true)) -or
                (-not $archiveOnly -and $verification.sourceVolumeRemoved -ne $false)) {
                $failureCode = 'verification_contract_failed'
                throw 'archive verifier did not return the fail-closed success contract'
            }

            $archiveRecord = [pscustomobject][ordered]@{
                volume = $bundleVolume
                bundle_id = $bundleDirectory.Name
                archive_bytes = [long]$manifest.artifact.bytes
                archive_sha256 = [string]$verification.archiveSha256
                source_matches = (-not $archiveOnly)
                source_removed = $archiveOnly
                archive_integrity_verified = $true
                restore_drill_recorded = 'passed'
                safe_to_remove = $false
            }
            $verifiedRecoveryArchives.Add($archiveRecord)
            if ($archiveOnly) {
                $sourceRemovedRecoveryArchives.Add($archiveRecord)
            } else {
                $verifiedArchiveVolumes[$bundleVolume] = $true
            }
        } catch {
            if (($bundleBytes -le 0 -or -not $artifactSha256) -and
                ($bundleDirectory.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0) {
                try {
                    $candidateFiles = @(Get-ChildItem -LiteralPath $bundleDirectory.FullName -File -Force -ErrorAction Stop)
                    if ($candidateFiles.Count -eq 1 -and
                        ($candidateFiles[0].Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0 -and
                        $candidateFiles[0].Name -cmatch '^(018-hai-(kafka-kraft|ollama-local|redis|redpanda|postgres-automation|postgres-idp)-data|018-hai-phase2-control-state)\.tar\.gz$' -and
                        $candidateFiles[0].Length -gt 0) {
                        $artifactSourceHint = [string]$Matches[1]
                        $bundleBytes = [long]$candidateFiles[0].Length
                        try { Assert-HaiPrivateEnvironmentAcl $candidateFiles[0].FullName }
                        catch {
                            $failureCode = 'artifact_acl_unverified'
                            throw 'archive artifact ACL is not private'
                        }
                        $artifactSha256 = (Get-FileHash -LiteralPath $candidateFiles[0].FullName -Algorithm SHA256).Hash.ToLowerInvariant()
                    } else {
                        $bundleBytes = [long](($candidateFiles | Measure-Object -Property Length -Sum).Sum)
                    }
                } catch {
                    if ($bundleBytes -le 0) { $bundleBytes = 0L }
                }
            }
            $unverifiedRecoveryBundles.Add([pscustomobject][ordered]@{
                bundle_id = $bundleDirectory.Name
                source_volume = $bundleVolume
                artifact_source_hint = $artifactSourceHint
                manifest_present = $manifestPresent
                failure_code = $failureCode
                bundle_modified_utc = $bundleDirectory.LastWriteTimeUtc.ToString('o')
                bundle_bytes = $bundleBytes
                artifact_sha256 = $artifactSha256
                exact_duplicate_of_verified_archive = $false
                disposition = 'retain_unverified'
            })
        }
    }

    foreach ($bundle in $unverifiedRecoveryBundles) {
        if ($bundle.manifest_present -or -not $bundle.artifact_source_hint -or -not $bundle.artifact_sha256) { continue }
        $matchingVerifiedArchive = @($verifiedRecoveryArchives | Where-Object {
            $_.volume -ceq $bundle.artifact_source_hint -and $_.archive_sha256 -ceq $bundle.artifact_sha256
        }) | Select-Object -First 1
        if ($null -ne $matchingVerifiedArchive) {
            $bundle.exact_duplicate_of_verified_archive = $true
            $bundle.disposition = 'review_exact_duplicate_of_verified_archive'
        }
    }
}

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
    } elseif ($coverage -ceq 'offline_archive_and_restore_drill') {
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
$unverifiedRecoveryBytes = [long](($unverifiedRecoveryBundles | Measure-Object -Property bundle_bytes -Sum).Sum)
$result = [pscustomobject][ordered]@{
    docker_context = (& docker context show 2>$null | Out-String).Trim()
    inventory_complete = ($unknownNames.Count -eq 0)
    named_hai_volumes = $inventory.Count
    unknown_hai_volumes = $unknownNames
    volumes_without_supported_recovery_method = $unsupportedRecoveryMethods
    volumes_without_current_verified_recovery_evidence = $unverifiedRecoveryCount
    verified_recovery_archives = @($verifiedRecoveryArchives)
    source_removed_recovery_archives = @($sourceRemovedRecoveryArchives)
    unverified_recovery_bundles = @($unverifiedRecoveryBundles)
    unverified_recovery_bundle_bytes = $unverifiedRecoveryBytes
    anonymous_hai_volume_mounts = $anonymousMounts
    hai_images = $images
    image_cleanup_authorized = $false
    safe_to_remove_any = $false
    deletion_performed = $false
    volumes = @($inventory)
}
$result | ConvertTo-Json -Depth 8

if ($unknownNames.Count -gt 0) { exit 2 }
