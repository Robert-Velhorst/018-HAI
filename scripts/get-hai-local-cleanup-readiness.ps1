[CmdletBinding()]
param(
    [string]$TranscriptRoot = 'D:\codex-temp\hai-completed-agent-sessions',
    [string]$RecoveryArchiveRoot = (Join-Path $env:LOCALAPPDATA 'HAI\volume-recovery'),
    [ValidateRange(1, 8760)]
    [int]$MinimumFixtureAgeHours = 24
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$expectedRoot = 'D:\codex-temp\hai-pr-update-018-20261009'
if ([IO.Path]::GetFullPath($repoRoot).TrimEnd('\') -cne $expectedRoot) {
    throw 'Unified HAI cleanup inventory is restricted to the inventoried PR worktree.'
}

function Get-ReadOnlyReport([string]$Name, [string]$ScriptName, [hashtable]$Arguments = @{}) {
    $path = Join-Path $PSScriptRoot $ScriptName
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        return [pscustomobject]@{ name = $Name; status = 'blocked'; blocker = 'readiness script is missing'; report = $null }
    }
    try {
        $global:LASTEXITCODE = 0
        $lines = @(& $path @Arguments)
        if ($lines.Count -eq 0) {
            return [pscustomobject]@{ name = $Name; status = 'blocked'; blocker = 'readiness command returned no successful report'; report = $null }
        }
        $report = ($lines -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
        if ($report.PSObject.Properties.Name -contains 'deletion_performed' -and $report.deletion_performed -ne $false) {
            return [pscustomobject]@{ name = $Name; status = 'blocked'; blocker = 'readiness command violated the no-deletion contract'; report = $null }
        }
        return [pscustomobject]@{ name = $Name; status = 'reported'; blocker = $null; report = $report }
    } catch {
        return [pscustomobject]@{ name = $Name; status = 'blocked'; blocker = [string]$_.Exception.Message; report = $null }
    }
}

$temp = Get-ReadOnlyReport 'synthetic_temp_fixtures' 'test-hai-temp-fixture-cleanup-readiness.ps1' @{ MinimumAgeHours = $MinimumFixtureAgeHours }
$transcriptIntegrity = Get-ReadOnlyReport 'transcript_source_integrity' 'test-hai-transcript-cleanup-readiness.ps1' @{
    TranscriptRoot = $TranscriptRoot
    RequireSourceArchive = $true
}
$transcripts = Get-ReadOnlyReport 'completed_session_transcripts' 'remove-hai-completed-session-transcripts.ps1' @{ TranscriptRoot = $TranscriptRoot }
$volumes = Get-ReadOnlyReport 'docker_volumes_and_recovery_archives' 'test-hai-volume-cleanup-readiness.ps1' @{ RecoveryArchiveRoot = $RecoveryArchiveRoot }
$diagnostics = Get-ReadOnlyReport 'pr_diagnostics_and_tool_downloads' 'remove-hai-pr-diagnostic-artifacts.ps1'
$images = Get-ReadOnlyReport 'unreferenced_local_images' 'remove-hai-unreferenced-image.ps1' @{
    ImageReferences = @(
        '018-hai-backend:latest',
        '018-hai-backend-migrate:latest',
        '018-hai-idp:latest',
        '018-hai-frontend:latest',
        '018-hai-nginxconfigmanager:latest'
    )
}

$targets = [Collections.Generic.List[object]]::new()
if ($temp.status -eq 'reported') {
    $candidateDirectories = @($temp.report.directories | Where-Object disposition -CEQ 'candidate_manual_cleanup')
    $targets.Add([pscustomobject][ordered]@{
        id = 'synthetic_temp_fixtures'
        source_path = [string]$temp.report.temp_root
        status = if ($candidateDirectories.Count -gt 0) { 'candidate_requires_explicit_confirmation' } else { 'retain_or_wait' }
        candidate_count = $candidateDirectories.Count
        candidate_bytes = [long](($candidateDirectories | Measure-Object -Property bytes -Sum).Sum)
        retained = @($temp.report.directories | Where-Object disposition -CNE 'candidate_manual_cleanup' | ForEach-Object {
            [pscustomobject]@{
                owner = [string]$_.owner
                disposition = [string]$_.disposition
                age_hours = $_.age_hours
                bytes = [long]$_.bytes
                reason = [string]$_.reason
            }
        })
    })
} else {
    $targets.Add([pscustomobject]@{ id = 'synthetic_temp_fixtures'; status = 'blocked'; blocker = $temp.blocker })
}

if ($transcripts.status -eq 'reported') {
    $integrity = if ($transcriptIntegrity.status -eq 'reported') { $transcriptIntegrity.report } else { $null }
    $targets.Add([pscustomobject][ordered]@{
        id = 'completed_session_transcripts'
        source_path = $TranscriptRoot
        status = [string]$transcripts.report.mode
        candidate_count = [int]$transcripts.report.candidate_files
        candidate_bytes = [long]$transcripts.report.candidate_bytes
        blocker = [string]$transcripts.report.blocker
        source_archive_integrity = [pscustomobject][ordered]@{
            status = if ($null -ne $integrity -and $integrity.source_archive_verified -eq $true) { 'verified' } else { 'not_verified' }
            result = if ($null -ne $integrity) { [string]$integrity.result } else { 'blocked' }
            source_archive_verified = ($null -ne $integrity -and $integrity.source_archive_verified -eq $true)
            source_archive_files = if ($null -ne $integrity) { $integrity.source_archive_files } else { $null }
            source_archive_logical_bytes = if ($null -ne $integrity) { $integrity.source_archive_logical_bytes } else { $null }
            blocker = if ($null -ne $integrity) { $null } else { [string]$transcriptIntegrity.blocker }
        }
        source_hash_audit_started = ($null -ne $integrity)
    })
} else {
    $integrity = if ($transcriptIntegrity.status -eq 'reported') { $transcriptIntegrity.report } else { $null }
    $targets.Add([pscustomobject][ordered]@{
        id = 'completed_session_transcripts'
        status = 'blocked'
        blocker = $transcripts.blocker
        source_archive_integrity = [pscustomobject][ordered]@{
            status = if ($null -ne $integrity -and $integrity.source_archive_verified -eq $true) { 'verified' } else { 'not_verified' }
            result = if ($null -ne $integrity) { [string]$integrity.result } else { 'blocked' }
            source_archive_verified = ($null -ne $integrity -and $integrity.source_archive_verified -eq $true)
            source_archive_files = if ($null -ne $integrity) { $integrity.source_archive_files } else { $null }
            source_archive_logical_bytes = if ($null -ne $integrity) { $integrity.source_archive_logical_bytes } else { $null }
            blocker = if ($null -ne $integrity) { $null } else { [string]$transcriptIntegrity.blocker }
        }
        source_hash_audit_started = ($null -ne $integrity)
    })
}

if ($volumes.status -eq 'reported') {
    $targets.Add([pscustomobject][ordered]@{
        id = 'docker_volumes_and_recovery_archives'
        status = if ($volumes.report.safe_to_remove_any -eq $true) { 'candidate_requires_explicit_confirmation' } else { 'retain' }
        named_volumes = @($volumes.report.volumes | ForEach-Object {
            [pscustomobject]@{
                name = [string]$_.volume
                recovery_method = [string]$_.recovery_method
                recovery_status = [string]$_.recovery_status
                container_reference_count = [int]$_.container_reference_count
                container_references = @($_.container_references)
                disposition = [string]$_.disposition
                safe_to_remove = [bool]$_.safe_to_remove
            }
        })
        unverified_recovery_bundles = @($volumes.report.unverified_recovery_bundles | ForEach-Object {
            [pscustomobject]@{
                bundle_id = [string]$_.bundle_id
                failure_code = [string]$_.failure_code
                bundle_bytes = [long]$_.bundle_bytes
                disposition = [string]$_.disposition
            }
        })
        unverified_recovery_bundle_bytes = [long]$volumes.report.unverified_recovery_bundle_bytes
        potential_unreferenced_images = @($volumes.report.hai_images | Where-Object container_reference_count -eq 0 | ForEach-Object {
            [pscustomobject]@{
                reference = [string]$_.reference
                image_id = [string]$_.image_id
                size = [string]$_.size
                disposition = [string]$_.disposition
                safe_to_remove = [bool]$_.safe_to_remove
            }
        })
        image_cleanup_authorized = [bool]$volumes.report.image_cleanup_authorized
        blocker = 'Attached or unverified state remains protected; persistent-volume removal requires an independently verified archive and restore drill.'
    })
} else {
    $targets.Add([pscustomobject]@{ id = 'docker_volumes_and_recovery_archives'; status = 'blocked'; blocker = $volumes.blocker })
}

foreach ($check in @($diagnostics, $images)) {
    if ($check.status -eq 'reported') {
        $report = $check.report
        $targets.Add([pscustomobject][ordered]@{
            id = $check.name
            status = [string]$report.mode
            candidate_count = if ($report.PSObject.Properties.Name -contains 'candidate_files') { [int]$report.candidate_files } elseif ($report.PSObject.Properties.Name -contains 'eligible_count') { [int]$report.eligible_count } else { 0 }
            candidate_bytes = if ($report.PSObject.Properties.Name -contains 'candidate_bytes') { [long]$report.candidate_bytes } elseif ($report.PSObject.Properties.Name -contains 'image_size_bytes_estimate_not_reclaimable') { [long]$report.image_size_bytes_estimate_not_reclaimable } else { 0L }
            blocker = [string]$report.blocker
        })
    } else {
        $targets.Add([pscustomobject]@{ id = $check.name; status = 'blocked'; blocker = $check.blocker })
    }
}

$report = [pscustomobject][ordered]@{
    schema_version = 1
    repository = 'Robert-Velhorst/018-HAI'
    pull_request = 36
    worktree = $repoRoot
    generated_utc = [DateTimeOffset]::UtcNow.ToString('o')
    mode = 'read_only_inventory'
    cleanup_authorized = $false
    deletion_performed = $false
    preserve_active_pr_worktree = $true
    preserve_source_reports_ledgers_and_recovery_archives = $true
    preservation_requirements = @(
        'Do not stop or remove Joyce Work Schedule, ShareT, or LARO containers.',
        'Do not remove attached HAI database/control-state volumes or containers.',
        'Do not remove the active PR worktree or toolchain before PR #36 is merged and its checks pass.',
        'Do not remove retained, aborted, duplicate-ID, or nonterminal transcripts.'
    )
    cleanup_targets = @($targets)
}
$report | ConvertTo-Json -Depth 10
