[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [Parameter(Mandatory)]
    [ValidateSet(
        '018-hai-backend:latest',
        '018-hai-backend-migrate:latest',
        '018-hai-idp:latest',
        '018-hai-frontend:latest',
        '018-hai-nginxconfigmanager:latest'
    )]
    [string[]]$ImageReferences,
    [switch]$Apply,
    [string]$ConfirmationPhrase = ''
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
$repo = 'Robert-Velhorst/018-HAI'
$prNumber = 36
$expectedRoot = 'D:\codex-temp\hai-pr-update-018-20261009'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..')).TrimEnd('\')
$composeFile = Join-Path $repoRoot 'docker-compose.local.yml'
$environmentFile = Join-Path $repoRoot '.env.example'
$serviceByReference = @{
    '018-hai-backend:latest' = 'backend'
    '018-hai-backend-migrate:latest' = 'backend-migrate'
    '018-hai-idp:latest' = 'idp'
    '018-hai-frontend:latest' = 'frontend'
    '018-hai-nginxconfigmanager:latest' = 'nginxconfigmanager'
}

function Invoke-HaiImageDocker([string[]]$Arguments) {
    $result = Invoke-HaiBoundedDockerCommand $Arguments -TimeoutSeconds 30
    if ($result.timed_out) { throw "Docker command timed out; the command outcome may be unknown. Run fresh read-only inventory before retrying: docker $($Arguments -join ' ')" }
    if (-not $result.succeeded) { throw "HAI image inventory failed: docker $($Arguments -join ' ')" }
    return @(([string]$result.output -split "`r?`n") | ForEach-Object { $_.Trim() } | Where-Object { $_ })
}

function Test-HaiBuildSource([string]$Service, $Compose) {
    $config = $Compose.services.$Service
    if ($null -eq $config -or $null -eq $config.build) { throw "Compose service '$Service' has no rebuildable source definition." }
    $build = $config.build
    $contextValue = if ($build -is [string]) { $build } else { [string]$build.context }
    $dockerfileValue = if ($build -is [string] -or [string]::IsNullOrWhiteSpace([string]$build.dockerfile)) {
        'Dockerfile'
    } else { [string]$build.dockerfile }
    $context = if ([IO.Path]::IsPathRooted($contextValue)) { [IO.Path]::GetFullPath($contextValue) }
        else { [IO.Path]::GetFullPath((Join-Path $repoRoot $contextValue)) }
    $dockerfile = if ([IO.Path]::IsPathRooted($dockerfileValue)) { [IO.Path]::GetFullPath($dockerfileValue) }
        else { [IO.Path]::GetFullPath((Join-Path $context $dockerfileValue)) }
    if (-not $context.StartsWith($repoRoot + '\', [StringComparison]::OrdinalIgnoreCase) -and $context -cne $repoRoot) {
        throw "Compose build context for '$Service' is outside the repository."
    }
    if (-not $dockerfile.StartsWith($repoRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
        -not $dockerfile.StartsWith($context.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase) -or
        -not (Test-Path -LiteralPath $context -PathType Container) -or -not (Test-Path -LiteralPath $dockerfile -PathType Leaf)) {
        throw "Compose build source for '$Service' is missing or outside the repository."
    }
    foreach ($path in @($context, $dockerfile)) {
        $item = Get-Item -LiteralPath $path -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "Compose build source for '$Service' traverses a reparse point." }
    }
}

function Get-HaiImageCandidates([string[]]$References, [string]$ExpectedContext) {
    if ($repoRoot -cne $expectedRoot) { throw 'Image cleanup is restricted to the exact inventoried HAI PR worktree.' }
    $git = Get-Command git -ErrorAction SilentlyContinue
    $gh = Get-Command gh -ErrorAction SilentlyContinue
    if ($null -eq $git -or $null -eq $gh) { throw 'git and GitHub CLI are required for merged-PR verification.' }
    $gitArgs = @('-c', "safe.directory=$repoRoot", '-C', $repoRoot)
    $origin = (& $git.Source @gitArgs remote get-url origin 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $origin -notmatch '(?i)(github\.com[:/]Robert-Velhorst/018-HAI(?:\.git)?$)') {
        throw 'origin does not identify the inventoried HAI repository.'
    }
    $branch = (& $git.Source @gitArgs branch --show-current 2>$null | Out-String).Trim()
    $head = (& $git.Source @gitArgs rev-parse HEAD 2>$null | Out-String).Trim().ToLowerInvariant()
    if ($LASTEXITCODE -ne 0 -or $branch -cne 'codex/hai-runtime-release' -or $head -notmatch '^[0-9a-f]{40}$') {
        throw 'the expected HAI PR worktree branch and valid HEAD are required.'
    }
    $prLines = @(& $gh.Source pr view $prNumber --repo $repo --json state,mergedAt,baseRefName,headRefOid,mergeCommit 2>$null)
    if ($LASTEXITCODE -ne 0 -or $prLines.Count -eq 0) { throw 'GitHub could not verify PR #36.' }
    $pr = ($prLines -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
    if ([string]$pr.state -cne 'CLOSED' -or [string]::IsNullOrWhiteSpace([string]$pr.mergedAt) -or
        [string]$pr.baseRefName -cne 'main' -or [string]$pr.headRefOid -cne $head -or
        [string]$pr.mergeCommit.oid -notmatch '^[0-9a-f]{40}$') {
        throw 'PR #36 must be merged into main at this exact local HEAD.'
    }
    $checkLines = @(& $gh.Source pr checks $prNumber --repo $repo --json name,state 2>$null)
    if ($LASTEXITCODE -ne 0 -or $checkLines.Count -eq 0) { throw 'PR #36 checks are unavailable or empty.' }
    $checks = @(($checkLines -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop)
    if ($checks.Count -eq 0 -or @($checks | Where-Object { [string]$_.state -cne 'SUCCESS' }).Count -gt 0) {
        throw 'Every PR #36 check must succeed before local images are removed.'
    }
    $null = Assert-HaiLocalDockerEngine
    $context = (Invoke-HaiImageDocker @('context', 'show') | Out-String).Trim()
    if ($context -cne $ExpectedContext) { throw 'Docker context changed during image cleanup.' }
    if (-not (Test-Path -LiteralPath $composeFile -PathType Leaf) -or -not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
        throw 'The local Compose file or environment configuration is missing.'
    }
    $composeLines = @(& docker compose --profile event-bus --env-file $environmentFile -f $composeFile config --format json 2>$null)
    if ($LASTEXITCODE -ne 0 -or $composeLines.Count -eq 0) { throw 'Current local Compose build configuration could not be validated.' }
    $compose = ($composeLines -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop

    $candidates = foreach ($reference in $References) {
        $service = $serviceByReference[$reference]
        Test-HaiBuildSource $service $compose
        $id = (Invoke-HaiImageDocker @('image', 'inspect', '--format', '{{.Id}}', $reference) | Out-String).Trim()
        if ($id -notmatch '^sha256:[0-9a-f]{64}$') { throw "Image identity is invalid for '$reference'." }
        $tagLines = @(Invoke-HaiImageDocker @('image', 'inspect', '--format', '{{json .RepoTags}}', $reference))
        if ($tagLines.Count -ne 1) { throw "Image tags could not be verified for '$reference'." }
        $tags = @($tagLines[0] | ConvertFrom-Json -ErrorAction Stop)
        if ($tags.Count -ne 1 -or [string]$tags[0] -cne $reference) { throw "Image '$reference' has extra or unexpected tags." }
        $labelLines = @(Invoke-HaiImageDocker @('image', 'inspect', '--format', '{{json .Config.Labels}}', $reference))
        if ($labelLines.Count -ne 1) { throw "Image labels could not be verified for '$reference'." }
        $labels = $labelLines[0] | ConvertFrom-Json -ErrorAction Stop
        if ([string]$labels.'com.docker.compose.project' -cne '018-hai' -or
            [string]$labels.'com.docker.compose.service' -cne $service) { throw "Image ownership labels do not match '$reference'." }
        $containers = @(Invoke-HaiImageDocker @('ps', '-a', '--filter', "ancestor=$id", '--format', '{{.Names}}|{{.Status}}'))
        if ($containers.Count -gt 0) { throw "Image '$reference' is still referenced by a container." }
        $sizeText = (Invoke-HaiImageDocker @('image', 'inspect', '--format', '{{.Size}}', $reference) | Out-String).Trim()
        $size = 0L
        if (-not [long]::TryParse($sizeText, [ref]$size) -or $size -le 0) { throw "Image size could not be verified for '$reference'." }
        [pscustomobject]@{ reference = $reference; image_id = $id; service = $service; bytes = $size; build_source_verified = $true; container_references = @() }
    }
    return [pscustomobject]@{ docker_context = $context; head = $head; candidates = @($candidates) }
}

$requested = @($ImageReferences | Sort-Object -Unique)
if ($requested.Count -ne $ImageReferences.Count) { throw 'Duplicate image references are not allowed.' }
$rootItem = Get-Item -LiteralPath $repoRoot -Force
if (($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'HAI repository root must not be a reparse point.' }
$contextName = (Invoke-HaiImageDocker @('context', 'show') | Out-String).Trim()
try {
    $inspection = Get-HaiImageCandidates $requested $contextName
} catch {
    [pscustomobject][ordered]@{
        mode = 'blocked'
        requested_images = $requested
        docker_context = $contextName
        blocker = [string]$_.Exception.Message
        cleanup_requires_apply = $true
        deletion_performed = $false
    } | ConvertTo-Json -Depth 5
    return
}

if (-not $Apply) {
    [pscustomobject][ordered]@{
        mode = 'dry_run'
        docker_context = $inspection.docker_context
        eligible_images = @($inspection.candidates)
        eligible_count = @($inspection.candidates).Count
        image_size_bytes_estimate_not_reclaimable = [long](($inspection.candidates | Measure-Object -Property bytes -Sum).Sum)
        confirmation_phrase = "REMOVE HAI UNREFERENCED IMAGES $(@($inspection.candidates).Count)"
        cleanup_requires_apply = $true
        deletion_performed = $false
    } | ConvertTo-Json -Depth 5
    return
}

$requiredPhrase = "REMOVE HAI UNREFERENCED IMAGES $($requested.Count)"
if ($ConfirmationPhrase -cne $requiredPhrase) { throw "Confirmation phrase mismatch. Required phrase: $requiredPhrase" }
if (-not $PSCmdlet.ShouldProcess(($requested -join ', '), 'Remove only unreferenced HAI image tags with a committed Compose build source')) {
    [pscustomobject]@{ mode = 'not_applied'; eligible_images = @($inspection.candidates); deletion_performed = $false } | ConvertTo-Json -Depth 5
    return
}

$finalInspection = Get-HaiImageCandidates $requested $contextName
foreach ($candidate in $inspection.candidates) {
    $current = @($finalInspection.candidates | Where-Object reference -CEQ $candidate.reference)
    if ($current.Count -ne 1 -or [string]$current[0].image_id -cne [string]$candidate.image_id) {
        throw "Image identity changed after confirmation for '$($candidate.reference)'; no further image was removed."
    }
}
$removed = [Collections.Generic.List[object]]::new()
$failure = $null
$outcomeUnknown = $false
foreach ($candidate in $finalInspection.candidates) {
    $removeAttempted = $false
    try {
        $currentContext = (Invoke-HaiImageDocker @('context', 'show') | Out-String).Trim()
        if ($currentContext -cne $contextName) { throw 'Docker context changed during image removal.' }
        $current = Get-HaiImageCandidates @([string]$candidate.reference) $contextName
        if ($current.candidates.Count -ne 1 -or [string]$current.candidates[0].image_id -cne [string]$candidate.image_id) {
            throw "Image identity or container references changed for '$($candidate.reference)'."
        }
        $removeAttempted = $true
        $null = Invoke-HaiImageDocker @('image', 'rm', [string]$candidate.reference)
        $remaining = @(Invoke-HaiImageDocker @('image', 'ls', '--all', '--no-trunc', '--format', '{{.Repository}}:{{.Tag}}|{{.ID}}') | Where-Object { $_ -match ('^' + [regex]::Escape([string]$candidate.reference) + '\|') })
        if ($remaining.Count -gt 0) { throw "Docker did not remove the exact image tag '$($candidate.reference)'." }
        $removed.Add([pscustomobject]@{ reference = [string]$candidate.reference; image_id = [string]$candidate.image_id; bytes = [long]$candidate.bytes; postflight_verified = $true; remove_response_confirmed = $true })
    } catch {
        $failure = 'An image failed its final context, reference, remove, or postflight check; inspect exact Docker state before retrying.'
        if ($removeAttempted) {
            try {
                $postflight = @(Invoke-HaiImageDocker @('image', 'ls', '--all', '--no-trunc', '--format', '{{.Repository}}:{{.Tag}}|{{.ID}}') | Where-Object { $_ -match ('^' + [regex]::Escape([string]$candidate.reference) + '\|') })
                if ($postflight.Count -eq 0) {
                    $removed.Add([pscustomobject]@{ reference = [string]$candidate.reference; image_id = [string]$candidate.image_id; bytes = [long]$candidate.bytes; postflight_verified = $true; remove_response_confirmed = $false })
                    $failure = 'Docker returned an error after image removal was requested, but fresh postflight confirms the tag is absent; verify state before further cleanup.'
                }
            } catch {
                $outcomeUnknown = $true
                $failure = 'Docker image removal was requested but postflight could not verify the tag state; outcome is unknown. Do not retry until fresh inventory succeeds.'
            }
        }
        break
    }
}
$postflightVerified = $true
try {
    $remainingTags = @(Invoke-HaiImageDocker @('image', 'ls', '--all', '--no-trunc', '--format', '{{.Repository}}:{{.Tag}}|{{.ID}}') | Where-Object {
        $line = [string]$_
        @($finalInspection.candidates | Where-Object { $line -match ('^' + [regex]::Escape([string]$_.reference) + '\|') }).Count -gt 0
    })
    if ($remainingTags.Count -gt 0) { $postflightVerified = $false }
} catch {
    $outcomeUnknown = $true
    $postflightVerified = $false
    $failure = 'Docker postflight inventory failed; image removal outcome is unknown. Do not retry until fresh inventory succeeds.'
}
[pscustomobject][ordered]@{
    mode = if ($failure) { 'partial_failure' } else { 'completed' }
    removed = @($removed)
    image_size_bytes_estimate_not_reclaimable = [long](($removed | Measure-Object -Property bytes -Sum).Sum)
    failure = $failure
    docker_volumes_and_containers_touched = $false
    deletion_performed = if ($outcomeUnknown) { $null } else { ($removed.Count -gt 0) }
    deletion_outcome_unknown = $outcomeUnknown
    postflight_verified = $postflightVerified
} | ConvertTo-Json -Depth 5
if ($failure) { throw $failure }
