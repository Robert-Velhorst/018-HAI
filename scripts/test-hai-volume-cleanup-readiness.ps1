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
    $disposition = if ($references.Count -gt 0) {
        'hold_attached_to_container'
    } elseif ($coverage -eq 'not_covered') {
        'hold_no_verified_archive_restore'
    } else {
        'hold_until_complete_bundle_and_restore_drill'
    }
    [pscustomobject][ordered]@{
        volume = $name
        recovery_method = $coverage
        container_reference_count = $references.Count
        container_references = $references
        disposition = $disposition
        safe_to_remove = $false
    }
}

$images = @(Invoke-DockerInventory @('image', 'ls', '--format', '{{.Repository}}:{{.Tag}}|{{.ID}}') | Where-Object { $_ -match '^018-hai-' })
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

$uncoveredPresent = @($inventory | Where-Object { $_.recovery_method -eq 'not_covered' }).Count
$result = [pscustomobject][ordered]@{
    docker_context = (& docker context show 2>$null | Out-String).Trim()
    inventory_complete = ($unknownNames.Count -eq 0)
    named_hai_volumes = $inventory.Count
    unknown_hai_volumes = $unknownNames
    uncovered_volume_count = $uncoveredPresent
    anonymous_hai_volume_mounts = $anonymousMounts
    hai_images = $images
    safe_to_remove_any = $false
    deletion_performed = $false
    volumes = @($inventory)
}
$result | ConvertTo-Json -Depth 8

if ($unknownNames.Count -gt 0) { exit 2 }
