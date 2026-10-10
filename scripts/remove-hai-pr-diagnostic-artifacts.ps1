[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [switch]$Apply,
    [string]$ConfirmationPhrase = ''
)

$ErrorActionPreference = 'Stop'
$repo = 'Robert-Velhorst/018-HAI'
$prNumber = 36
$expectedRoot = 'D:\codex-temp\hai-pr-update-018-20261009'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..')).TrimEnd('\')
$manifestPath = Join-Path $PSScriptRoot 'local-hai-pr-artifact-cleanup-manifest.json'

function Get-HaiArtifactPreflight {
    if ($repoRoot -cne $expectedRoot) { return [pscustomobject]@{ ready = $false; reason = 'cleanup is restricted to the inventoried HAI PR worktree' } }
    $git = Get-Command git -ErrorAction SilentlyContinue
    $gh = Get-Command gh -ErrorAction SilentlyContinue
    if ($null -eq $git -or $null -eq $gh) { return [pscustomobject]@{ ready = $false; reason = 'git and GitHub CLI are required for remote merge verification' } }
    $gitArgs = @('-c', "safe.directory=$repoRoot", '-C', $repoRoot)
    $origin = (& $git.Source @gitArgs remote get-url origin 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $origin -notmatch '(?i)(github\.com[:/]Robert-Velhorst/018-HAI(?:\.git)?$)') {
        return [pscustomobject]@{ ready = $false; reason = 'origin does not identify the inventoried HAI repository' }
    }
    $branch = (& $git.Source @gitArgs branch --show-current 2>$null | Out-String).Trim()
    $head = (& $git.Source @gitArgs rev-parse HEAD 2>$null | Out-String).Trim().ToLowerInvariant()
    if ($LASTEXITCODE -ne 0 -or $branch -cne 'codex/hai-runtime-release' -or $head -notmatch '^[0-9a-f]{40}$') {
        return [pscustomobject]@{ ready = $false; reason = 'the expected PR worktree branch and valid HEAD are required' }
    }

    $prOutput = @(& $gh.Source pr view $prNumber --repo $repo --json state,mergedAt,baseRefName,headRefOid,mergeCommit 2>$null)
    if ($LASTEXITCODE -ne 0 -or $prOutput.Count -eq 0) { return [pscustomobject]@{ ready = $false; reason = 'GitHub could not verify PR #36' } }
    try { $pr = ($prOutput -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop }
    catch { return [pscustomobject]@{ ready = $false; reason = 'GitHub returned invalid PR metadata' } }
    if ([string]$pr.state -cne 'CLOSED' -or [string]::IsNullOrWhiteSpace([string]$pr.mergedAt) -or
        [string]$pr.baseRefName -cne 'main' -or [string]$pr.headRefOid -cne $head -or
        [string]$pr.mergeCommit.oid -notmatch '^[0-9a-f]{40}$') {
        return [pscustomobject]@{ ready = $false; reason = 'PR #36 must be merged into main at this exact local HEAD' }
    }
    $checksOutput = @(& $gh.Source pr checks $prNumber --repo $repo --json name,state 2>$null)
    if ($LASTEXITCODE -ne 0 -or $checksOutput.Count -eq 0) { return [pscustomobject]@{ ready = $false; reason = 'PR #36 checks are unavailable or empty' } }
    try { $checks = @(($checksOutput -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop) }
    catch { return [pscustomobject]@{ ready = $false; reason = 'GitHub returned invalid PR check metadata' } }
    if ($checks.Count -eq 0 -or @($checks | Where-Object { [string]$_.state -cne 'SUCCESS' }).Count -gt 0) {
        return [pscustomobject]@{ ready = $false; reason = 'every PR #36 check must succeed before local diagnostics are discarded' }
    }
    return [pscustomobject]@{ ready = $true; reason = $null; branch = $branch; head = $head }
}

if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'Committed local artifact manifest is missing.' }
$manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json -ErrorAction Stop
if ([int]$manifest.version -ne 1 -or [string]$manifest.repository -cne $repo -or
    [int]$manifest.pullRequest -ne $prNumber -or [string]$manifest.worktreeRoot -cne $expectedRoot -or
    @($manifest.artifacts).Count -ne 15) {
    throw 'Local artifact manifest identity or exact candidate count is invalid.'
}

$preflight = Get-HaiArtifactPreflight
$artifactPaths = [Collections.Generic.List[string]]::new()
$bytes = 0L
foreach ($entry in @($manifest.artifacts)) {
    $relative = [string]$entry.path
    if ([IO.Path]::IsPathRooted($relative) -or @($relative -split '[/\\]' | Where-Object { $_ -in @('', '.', '..') }).Count -gt 0 -or
        $relative -notmatch '^(ci-113991681(379|677|724|733|798|876|884|896)\.log|ci-113991682(018|031|127|189)\.log|ci-frontend-job\.zip|gitleaks\.exe|gitleaks_8\.30\.1_windows_x64\.zip)$') {
        throw 'Manifest contains a path outside the exact local diagnostic allowlist.'
    }
    $path = [IO.Path]::GetFullPath((Join-Path $repoRoot $relative))
    if (-not $path.StartsWith($repoRoot + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Artifact escaped the inventoried worktree.' }
    $item = Get-Item -LiteralPath $path -Force -ErrorAction Stop
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        [long]$item.Length -ne [long]$entry.bytes -or [string]$entry.sha256 -notmatch '^[0-9a-f]{64}$' -or
        (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$entry.sha256) {
        throw "Local artifact does not match the committed size/hash manifest: $relative"
    }
    $tracked = @(& git -c "safe.directory=$repoRoot" -C $repoRoot ls-files --error-unmatch -- $relative 2>$null)
    if ($LASTEXITCODE -eq 0 -or $tracked.Count -gt 0) { throw "Refusing to remove a tracked artifact: $relative" }
    $artifactPaths.Add($path)
    $bytes += [long]$entry.bytes
}

if (-not $Apply) {
    [pscustomobject][ordered]@{
        mode = if ($preflight.ready) { 'dry_run' } else { 'blocked' }
        candidate_files = $artifactPaths.Count
        candidate_bytes = $bytes
        readiness_gate_ready = $preflight.ready
        blocker = $preflight.reason
        requires_apply_and_confirmation = $true
        deletion_performed = $false
    } | ConvertTo-Json -Depth 4
    return
}
if (-not $preflight.ready) { throw "Cleanup gate failed: $($preflight.reason); no local artifact was removed." }
$requiredPhrase = "REMOVE HAI LOCAL DIAGNOSTICS $($artifactPaths.Count)"
if ($ConfirmationPhrase -cne $requiredPhrase) { throw "Confirmation phrase mismatch. Required phrase: $requiredPhrase" }

$processes = Get-CimInstance Win32_Process -ErrorAction Stop
$toolPath = Join-Path $repoRoot 'gitleaks.exe'
$busy = @($processes | Where-Object {
    -not [string]::IsNullOrWhiteSpace([string]$_.CommandLine) -and
    ([string]$_.CommandLine).IndexOf($toolPath, [StringComparison]::OrdinalIgnoreCase) -ge 0
})
if ($busy.Count -gt 0) { throw 'A running process references the local Gitleaks executable; cleanup is blocked.' }

if (-not $PSCmdlet.ShouldProcess("$($artifactPaths.Count) exact manifest-listed files ($bytes bytes) in $repoRoot", 'Remove HAI PR local diagnostics')) {
    [pscustomobject]@{ mode = 'not_applied'; candidate_files = $artifactPaths.Count; candidate_bytes = $bytes; deletion_performed = $false } | ConvertTo-Json
    return
}

$removed = [Collections.Generic.List[object]]::new()
foreach ($entry in @($manifest.artifacts)) {
    $path = Join-Path $repoRoot ([string]$entry.path)
    $item = Get-Item -LiteralPath $path -Force -ErrorAction Stop
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        [long]$item.Length -ne [long]$entry.bytes -or
        (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$entry.sha256) {
        throw 'Artifact changed during cleanup; stopping before removing this file.'
    }
    Remove-Item -LiteralPath $path -Force -ErrorAction Stop
    if (Test-Path -LiteralPath $path) { throw "Artifact removal could not be verified: $($entry.path)" }
    $removed.Add([pscustomobject]@{ path = [string]$entry.path; bytes = [long]$entry.bytes })
}
[pscustomobject][ordered]@{
    mode = 'completed'
    removed_files = @($removed)
    removed_bytes = $bytes
    preserved_paths = @('local-working-tree.patch', 'docs/isolated-acceptance.md', 'backend/internal/outcomeevaluation/test-evidence/', 'docs/child-agent-archive-2026-07-30/', 'all Docker containers, images, volumes, and recovery bundles')
    deletion_performed = $true
} | ConvertTo-Json -Depth 5
