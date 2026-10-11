[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [string[]]$OwnerIds = @(),
    [ValidateRange(1, 8760)]
    [int]$MinimumAgeHours = 24,
    [switch]$Apply,
    [string]$ConfirmationPhrase = ''
)

$ErrorActionPreference = 'Stop'
$readinessScript = Join-Path $PSScriptRoot 'test-hai-temp-fixture-cleanup-readiness.ps1'
$root = (Resolve-Path -LiteralPath ([IO.Path]::GetTempPath())).Path.TrimEnd([IO.Path]::DirectorySeparatorChar)
$rootItem = Get-Item -LiteralPath $root -Force
if (($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
    throw 'Temp root is a reparse point; cleanup is blocked.'
}

function Get-HaiFixtureReadiness {
    $json = @(& $readinessScript -TempRoot $root -MinimumAgeHours $MinimumAgeHours)
    if ($LASTEXITCODE -ne 0) { throw 'Fixture readiness check failed; no directories were removed.' }
    return (($json -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop)
}

$initial = Get-HaiFixtureReadiness
$requestedOwners = @($OwnerIds | ForEach-Object { ([string]$_).ToLowerInvariant() } | Sort-Object -Unique)
$candidates = @($initial.directories | Where-Object { $_.disposition -ceq 'candidate_manual_cleanup' })
if ($requestedOwners.Count -eq 0) {
    [pscustomobject][ordered]@{
        mode = 'dry_run'
        candidate_directories = $candidates.Count
        candidate_bytes = [long](($candidates | Measure-Object -Property bytes -Sum).Sum)
        owner_ids = @($candidates | ForEach-Object owner)
        cleanup_requires_apply = $true
        deletion_performed = $false
    } | ConvertTo-Json -Depth 4
    return
}

$selected = @($candidates | Where-Object { $requestedOwners -ccontains ([string]$_.owner).ToLowerInvariant() })
if ($selected.Count -ne $requestedOwners.Count) {
    throw 'Every requested owner ID must match a verified, sufficiently old, unreferenced fixture candidate.'
}
if (-not $Apply) {
    throw 'Owner IDs are a preview only. Pass -Apply plus the exact confirmation phrase to remove verified fixtures.'
}
$requiredPhrase = "REMOVE HAI TEMP FIXTURES $($selected.Count)"
if ($ConfirmationPhrase -cne $requiredPhrase) {
    throw "Confirmation phrase mismatch. Required phrase: $requiredPhrase"
}

# Re-run the full file-hash and Docker-resource audit immediately before removal.
$fresh = Get-HaiFixtureReadiness
$freshByOwner = @{}
foreach ($entry in $fresh.directories) { $freshByOwner[[string]$entry.owner] = $entry }
$pathsToRemove = [Collections.Generic.List[string]]::new()
foreach ($candidate in $selected) {
    $owner = [string]$candidate.owner
    if (-not $freshByOwner.ContainsKey($owner)) { throw 'A selected fixture disappeared during cleanup validation.' }
    $current = $freshByOwner[$owner]
    if ($current.disposition -cne 'candidate_manual_cleanup' -or
        [long]$current.bytes -ne [long]$candidate.bytes -or
        $current.related_containers -ne 0 -or $current.related_networks -ne 0 -or $current.related_volumes -ne 0 -or
        (ConvertTo-Json -InputObject @($current.source_hashes) -Depth 5 -Compress) -cne
            (ConvertTo-Json -InputObject @($candidate.source_hashes) -Depth 5 -Compress)) {
        throw 'Fixture identity, content, age, or Docker references changed; no selected directory was removed.'
    }

    $expectedName = "hai-acceptance-$owner"
    $path = (Resolve-Path -LiteralPath ([string]$current.directory)).Path
    $expectedPath = Join-Path $root $expectedName
    if ($path -cne $expectedPath -or
        -not $path.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Resolved fixture path is outside the exact Temp-owned fixture directory.'
    }
    $item = Get-Item -LiteralPath $path -Force
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'Fixture path became a reparse point; cleanup is blocked.'
    }

    $processes = Get-CimInstance Win32_Process -ErrorAction Stop
    $processReference = @($processes | Where-Object {
        -not [string]::IsNullOrWhiteSpace([string]$_.CommandLine) -and
        ([string]$_.CommandLine).IndexOf($path, [StringComparison]::OrdinalIgnoreCase) -ge 0
    })
    if ($processReference.Count -gt 0) { throw 'A running process command line references a selected fixture.' }
    $pathsToRemove.Add($path)
}

$removed = [Collections.Generic.List[object]]::new()
foreach ($path in $pathsToRemove) {
    if ($PSCmdlet.ShouldProcess($path, 'Remove verified synthetic HAI acceptance fixture')) {
        $selectedEntry = @($selected | Where-Object { $_.directory -ceq $path })
        if ($selectedEntry.Count -ne 1) { throw 'Selected fixture identity is ambiguous; no further directory was removed.' }
        $owner = [string]$selectedEntry[0].owner
        $finalReadiness = Get-HaiFixtureReadiness
        if ($finalReadiness.deletion_performed -ne $false -or $finalReadiness.cleanup_authorized -ne $false) {
            throw 'Final fixture audit violated its read-only contract; no further directory was removed.'
        }
        $finalEntry = @($finalReadiness.directories | Where-Object { [string]$_.owner -ceq $owner })
        if ($finalEntry.Count -ne 1 -or
            [string]$finalEntry[0].disposition -cne 'candidate_manual_cleanup' -or
            [string]$finalEntry[0].directory -cne $path -or
            [long]$finalEntry[0].bytes -ne [long]$selectedEntry[0].bytes -or
            $null -eq $finalEntry[0].age_hours -or [double]$finalEntry[0].age_hours -lt $MinimumAgeHours -or
            $finalEntry[0].related_containers -ne 0 -or $finalEntry[0].related_networks -ne 0 -or
            $finalEntry[0].related_volumes -ne 0 -or
            (ConvertTo-Json -InputObject @($finalEntry[0].source_hashes) -Depth 5 -Compress) -cne
                (ConvertTo-Json -InputObject @($selectedEntry[0].source_hashes) -Depth 5 -Compress)) {
            throw 'Fixture identity, hashes, age, or Docker references changed after confirmation; no further directory was removed.'
        }

        $verifiedPath = (Resolve-Path -LiteralPath $path -ErrorAction Stop).Path
        $expectedPath = Join-Path $root "hai-acceptance-$owner"
        if ($verifiedPath -cne $expectedPath -or
            -not $verifiedPath.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Fixture path changed after confirmation; no further directory was removed.'
        }
        $verifiedItem = Get-Item -LiteralPath $verifiedPath -Force
        $verifiedEntries = @(Get-ChildItem -LiteralPath $verifiedPath -Force -Recurse -ErrorAction Stop)
        if (($verifiedItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
            @($verifiedEntries | Where-Object { ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 }).Count -gt 0) {
            throw 'Fixture acquired a reparse point after confirmation; no further directory was removed.'
        }
        $finalProcesses = Get-CimInstance Win32_Process -ErrorAction Stop
        $finalProcessReference = @($finalProcesses | Where-Object {
            -not [string]::IsNullOrWhiteSpace([string]$_.CommandLine) -and
            ([string]$_.CommandLine).IndexOf($verifiedPath, [StringComparison]::OrdinalIgnoreCase) -ge 0
        })
        if ($finalProcessReference.Count -gt 0) { throw 'A running process now references the selected fixture; no further directory was removed.' }

        Remove-Item -LiteralPath $verifiedPath -Recurse -Force -ErrorAction Stop
        if (Test-Path -LiteralPath $verifiedPath) { throw 'Fixture removal could not be verified.' }
        $removed.Add([pscustomobject]@{
            path = $verifiedPath
            owner = $owner
            bytes = [long]$selectedEntry[0].bytes
            provenance = [string]$finalEntry[0].provenance
            source_hashes = @($finalEntry[0].source_hashes)
        })
    }
}

[pscustomobject][ordered]@{
    mode = 'apply'
    removed_directories = $removed.Count
    removed_bytes = [long](($removed | Measure-Object -Property bytes -Sum).Sum)
    removed = @($removed)
    deletion_performed = ($removed.Count -gt 0)
} | ConvertTo-Json -Depth 4
