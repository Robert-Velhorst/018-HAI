[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [string]$RecoveryArchiveRoot = (Join-Path $env:LOCALAPPDATA 'HAI\volume-recovery'),
    [switch]$Apply,
    [string]$ConfirmationPhrase = ''
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
$readinessScript = Join-Path $PSScriptRoot 'test-hai-volume-cleanup-readiness.ps1'
$archiveRoot = [IO.Path]::GetFullPath($RecoveryArchiveRoot).TrimEnd('\')
$expectedRoot = [IO.Path]::GetFullPath((Join-Path $env:LOCALAPPDATA 'HAI\volume-recovery')).TrimEnd('\')
if ($archiveRoot -cne $expectedRoot) { throw 'Duplicate cleanup is restricted to the exact local HAI recovery archive root.' }

function Get-HaiRecoveryReadiness {
    $output = @(& $readinessScript)
    if ($LASTEXITCODE -ne 0 -or $output.Count -eq 0) { throw 'HAI recovery readiness is incomplete.' }
    $report = ($output -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
    if ($report.inventory_complete -ne $true -or $report.deletion_performed -ne $false) {
        throw 'HAI recovery readiness did not return a complete, read-only inventory.'
    }
    return $report
}

function Assert-HaiEffectivePrivateFileAcl([string]$Path) {
    $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'Duplicate bundle entries must be regular files.'
    }
    $acl = Get-Acl -LiteralPath $Path -ErrorAction Stop
    $expectedOwner = Get-HaiWindowsUserSid
    $ownerSid = try { ([Security.Principal.NTAccount]::new([string]$acl.Owner)).Translate([Security.Principal.SecurityIdentifier]).Value }
        catch { [string]$acl.Owner }
    if ($ownerSid -cne $expectedOwner) { throw 'Duplicate bundle file is not owned by the current user.' }
    $rules = @($acl.Access)
    $expectedSids = @($expectedOwner, 'S-1-5-18')
    if ($rules.Count -ne 2) { throw 'Duplicate bundle file has unexpected access rules.' }
    foreach ($rule in $rules) {
        $ruleSid = $rule.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value
        if ($ruleSid -notin $expectedSids -or
            $rule.AccessControlType -ne [Security.AccessControl.AccessControlType]::Allow -or
            ($rule.FileSystemRights -band [Security.AccessControl.FileSystemRights]::FullControl) -ne [Security.AccessControl.FileSystemRights]::FullControl) {
            throw 'Duplicate bundle file is not effectively private to the current user and SYSTEM.'
        }
    }
}

function Get-HaiExactDuplicateBundles($Readiness) {
    $canonical = @($Readiness.source_removed_recovery_archives | Where-Object {
        $_.source_removed -eq $true -and $_.archive_integrity_verified -eq $true -and
        $_.restore_drill_recorded -ceq 'passed' -and $_.safe_to_remove -eq $false
    })
    $canonicalByVolume = @{}
    foreach ($archive in $canonical) {
        if ($canonicalByVolume.ContainsKey([string]$archive.volume)) { throw 'Multiple canonical recovery archives exist for one removed source volume.' }
        $canonicalByVolume[[string]$archive.volume] = $archive
    }

    $duplicates = [Collections.Generic.List[object]]::new()
    foreach ($bundle in @(Get-ChildItem -LiteralPath $archiveRoot -Directory -Force | Where-Object Name -Match '^hai-volume-recovery-[0-9a-f]{32}$')) {
        if (($bundle.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'A recovery bundle path is a reparse point.' }
        Assert-HaiOwnedDirectory $bundle.FullName $archiveRoot $bundle.Name
        Assert-HaiPrivateEnvironmentAcl $bundle.FullName -Directory

        $entries = @(Get-ChildItem -LiteralPath $bundle.FullName -Force -ErrorAction Stop)
        if (@($entries | Where-Object PSIsContainer).Count -gt 0) { continue }
        $artifactFiles = @($entries | Where-Object { $_.Name -cmatch '^(018-hai-(kafka-kraft|ollama-local|redis|redpanda)-data)\.tar\.gz$' })
        if ($artifactFiles.Count -ne 1 -or $entries.Count -notin @(1, 2)) { continue }
        $artifact = $artifactFiles[0]
        Assert-HaiEffectivePrivateFileAcl $artifact.FullName
        $sourceVolume = [regex]::Match($artifact.Name, '^(018-hai-(?:kafka-kraft|ollama-local|redis|redpanda)-data)\.tar\.gz$').Groups[1].Value
        if (-not $canonicalByVolume.ContainsKey($sourceVolume)) { continue }
        $archive = $canonicalByVolume[$sourceVolume]
        if ([long]$artifact.Length -ne [long]$archive.archive_bytes -or
            (Get-FileHash -LiteralPath $artifact.FullName -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$archive.archive_sha256) {
            continue
        }

        $manifestFiles = @($entries | Where-Object Name -CEQ 'manifest.json')
        if ($manifestFiles.Count -gt 1 -or ($entries.Count -eq 2 -and $manifestFiles.Count -ne 1)) { continue }
        if ($manifestFiles.Count -eq 1) {
            $manifestFile = $manifestFiles[0]
            Assert-HaiEffectivePrivateFileAcl $manifestFile.FullName
            if ($manifestFile.Length -gt 65536) { continue }
            $manifest = Get-Content -LiteralPath $manifestFile.FullName -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
            $owner = [regex]::Match($bundle.Name, '^hai-volume-recovery-([0-9a-f]{32})$').Groups[1].Value
            if ([string]$manifest.owner -cne $owner -or
                [string]$manifest.sourceVolume.name -cne $sourceVolume -or
                [string]$manifest.artifact.name -cne $artifact.Name -or
                [long]$manifest.artifact.bytes -ne [long]$archive.archive_bytes -or
                [string]$manifest.artifact.sha256 -cne [string]$archive.archive_sha256 -or
                [string]$manifest.restoreDrill -cne 'passed' -or $manifest.sourceUnchangedAfterArchive -ne $true) {
                continue
            }
        }

        if ($bundle.Name -ceq [string]$archive.bundle_id) { continue }
        $duplicates.Add([pscustomobject][ordered]@{
            bundle_id = $bundle.Name
            volume = $sourceVolume
            canonical_bundle_id = [string]$archive.bundle_id
            archive_bytes = [long]$artifact.Length
            archive_sha256 = (Get-FileHash -LiteralPath $artifact.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
            manifest_present = ($manifestFiles.Count -eq 1)
            bundle_bytes = [long](($entries | Measure-Object -Property Length -Sum).Sum)
        })
    }
    return @($duplicates)
}

function Assert-HaiDuplicatePath($Duplicate) {
    $path = [IO.Path]::GetFullPath((Join-Path $archiveRoot ([string]$Duplicate.bundle_id)))
    if ([IO.Path]::GetDirectoryName($path).TrimEnd('\') -cne $archiveRoot -or
        [IO.Path]::GetFileName($path) -cne [string]$Duplicate.bundle_id) {
        throw 'Duplicate bundle resolved outside the exact recovery archive root.'
    }
    $current = @(Get-HaiExactDuplicateBundles (Get-HaiRecoveryReadiness) | Where-Object bundle_id -CEQ ([string]$Duplicate.bundle_id))
    if ($current.Count -ne 1 -or $current[0].archive_sha256 -cne [string]$Duplicate.archive_sha256 -or
        $current[0].canonical_bundle_id -cne [string]$Duplicate.canonical_bundle_id) {
        throw 'Duplicate bundle or canonical archive changed during cleanup.'
    }
    return $path
}

$initialReadiness = Get-HaiRecoveryReadiness
$candidates = @(Get-HaiExactDuplicateBundles $initialReadiness)
if (-not $Apply) {
    [pscustomobject][ordered]@{
        mode = 'dry_run'
        candidate_bundles = $candidates
        candidate_count = $candidates.Count
        candidate_bytes = [long](($candidates | Measure-Object -Property bundle_bytes -Sum).Sum)
        confirmation_phrase = "REMOVE HAI EXACT DUPLICATE RECOVERY BUNDLES $($candidates.Count)"
        cleanup_requires_apply = $true
        deletion_performed = $false
    } | ConvertTo-Json -Depth 5
    return
}

if ($candidates.Count -eq 0) { throw 'No exact duplicate recovery bundles are eligible.' }
$requiredPhrase = "REMOVE HAI EXACT DUPLICATE RECOVERY BUNDLES $($candidates.Count)"
if ($ConfirmationPhrase -cne $requiredPhrase) { throw "Confirmation phrase mismatch. Required phrase: $requiredPhrase" }
$processes = Get-CimInstance Win32_Process -ErrorAction Stop
if (@($processes | Where-Object { [string]$_.CommandLine -and $_.CommandLine.IndexOf($archiveRoot, [StringComparison]::OrdinalIgnoreCase) -ge 0 }).Count -gt 0) {
    throw 'A running process references the HAI recovery archive; duplicate cleanup is blocked.'
}

$removed = [Collections.Generic.List[object]]::new()
foreach ($candidate in $candidates) {
    $directory = Assert-HaiDuplicatePath $candidate
    if (-not $PSCmdlet.ShouldProcess($directory, 'Remove exact duplicate HAI recovery bundle; retain canonical archive')) { continue }
    $entries = @(Get-ChildItem -LiteralPath $directory -Force -ErrorAction Stop)
    foreach ($entry in $entries) {
        if ($entry.PSIsContainer -or ($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw 'Recovery bundle changed after verification; refusing file removal.'
        }
        Remove-Item -LiteralPath $entry.FullName -Force -ErrorAction Stop
    }
    if (@(Get-ChildItem -LiteralPath $directory -Force -ErrorAction Stop).Count -ne 0) {
        throw 'Unexpected entries remain; the recovery bundle directory was retained.'
    }
    Remove-Item -LiteralPath $directory -Force -ErrorAction Stop
    if (Test-Path -LiteralPath $directory) { throw 'Exact duplicate recovery bundle removal could not be verified.' }
    $removed.Add($candidate)
}

$finalReadiness = Get-HaiRecoveryReadiness
$remainingIds = @($finalReadiness.unverified_recovery_bundles | ForEach-Object bundle_id)
$unexpectedRemaining = @($removed | Where-Object { $remainingIds -ccontains $_.bundle_id })
if ($unexpectedRemaining.Count -gt 0) { throw 'Postflight still reports a removed duplicate bundle.' }
[pscustomobject][ordered]@{
    mode = 'apply'
    removed_bundles = @($removed)
    removed_count = $removed.Count
    removed_bytes = [long](($removed | Measure-Object -Property bundle_bytes -Sum).Sum)
    deletion_performed = ($removed.Count -gt 0)
    canonical_archives_retained = @($finalReadiness.source_removed_recovery_archives | Select-Object volume, bundle_id, archive_sha256)
    postflight_verified = ($unexpectedRemaining.Count -eq 0)
} | ConvertTo-Json -Depth 6
