[CmdletBinding()]
param(
    [ValidatePattern('^[a-zA-Z0-9][a-zA-Z0-9_-]*$')]
    [string]$ProjectName = "018-hai",
    [switch]$AsJson
)

$ErrorActionPreference = "Stop"

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "Docker CLI is not available on PATH."
}

function ConvertTo-Mebibytes {
    param([Parameter(Mandatory = $true)][string]$Value)

    if ($Value -notmatch '^\s*([0-9]+(?:\.[0-9]+)?)\s*(B|KiB|MiB|GiB|TiB)\s*$') {
        throw "Unsupported Docker memory value: '$Value'"
    }

    $number = [double]$Matches[1]
    switch ($Matches[2]) {
        "B"   { return $number / 1MB }
        "KiB" { return $number / 1024 }
        "MiB" { return $number }
        "GiB" { return $number * 1024 }
        "TiB" { return $number * 1024 * 1024 }
    }
}

$containerIds = @(& docker ps --filter "label=com.docker.compose.project=$ProjectName" --format '{{.ID}}')
if ($LASTEXITCODE -ne 0) {
    throw "Unable to list Docker containers for Compose project '$ProjectName'."
}
$containerIds = @($containerIds | ForEach-Object { ([string]$_).Trim() } | Where-Object { $_ })

if ($containerIds.Count -eq 0) {
    throw "No running containers found for Compose project '$ProjectName'."
}

$rows = @()
$statsLines = @(& docker stats --no-stream --format '{{json .}}' @containerIds)
if ($LASTEXITCODE -ne 0) {
    throw "Unable to read Docker statistics for Compose project '$ProjectName'."
}

foreach ($line in $statsLines) {
    if ([string]::IsNullOrWhiteSpace($line)) {
        continue
    }

    $stat = $line | ConvertFrom-Json
    $usage = ([string]$stat.MemUsage -split '/', 2)[0].Trim()
    $cpuPercent = [double](([string]$stat.CPUPerc).Trim().TrimEnd('%'))
    $rows += [pscustomobject]@{
        Name      = [string]$stat.Name
        CpuPct    = [math]::Round($cpuPercent, 2)
        MemoryMiB = [math]::Round((ConvertTo-Mebibytes -Value $usage), 2)
        MemoryPct = [string]$stat.MemPerc
        Pids      = [int]$stat.PIDs
    }
}

$rows = @($rows | Sort-Object Name)
$summary = [pscustomobject]@{
    Project         = $ProjectName
    RunningServices = $rows.Count
    TotalCpuPct     = [math]::Round(($rows | Measure-Object CpuPct -Sum).Sum, 2)
    TotalMemoryMiB  = [math]::Round(($rows | Measure-Object MemoryMiB -Sum).Sum, 2)
    Services        = $rows
    MeasuredAtUtc   = [DateTimeOffset]::UtcNow.ToString("o")
}

if ($AsJson) {
    $summary | ConvertTo-Json -Depth 4
    return
}

$rows | Format-Table Name, CpuPct, MemoryMiB, MemoryPct, Pids -AutoSize
Write-Host ("HAI total: {0:N2} MiB memory, {1:N2}% aggregate CPU across {2} running services." -f $summary.TotalMemoryMiB, $summary.TotalCpuPct, $summary.RunningServices)
Write-Host "Only containers labelled com.docker.compose.project=$ProjectName are included."
