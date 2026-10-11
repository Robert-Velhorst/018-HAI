[CmdletBinding()]
param(
    [string]$EnvFile = "",
    [ValidateRange(10, 600)]
    [int]$HealthTimeoutSeconds = 90
)

$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$composeFile = Join-Path $repositoryRoot "docker-compose.local.yml"

if ([string]::IsNullOrWhiteSpace($EnvFile)) {
    $EnvFile = Join-Path $repositoryRoot ".env.local"
} elseif (-not [System.IO.Path]::IsPathRooted($EnvFile)) {
    $EnvFile = Join-Path $repositoryRoot $EnvFile
}
$EnvFile = [System.IO.Path]::GetFullPath($EnvFile)

if (-not (Test-Path -LiteralPath $composeFile -PathType Leaf)) {
    throw "Missing Compose file: $composeFile"
}
if (-not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) {
    throw "Missing environment file: $EnvFile"
}
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "Docker CLI is not available on PATH."
}

function Get-ContainerId {
    param([Parameter(Mandatory = $true)][string]$Name)

    $containerId = & docker inspect --format '{{.Id}}' $Name 2>$null
    if ($LASTEXITCODE -ne 0) {
        return ""
    }
    return ([string]$containerId).Trim()
}

function Get-ContainerHealth {
    param([Parameter(Mandatory = $true)][string]$Name)

    $health = & docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' $Name 2>$null
    if ($LASTEXITCODE -ne 0) {
        return "missing"
    }
    return ([string]$health).Trim()
}

$backendBefore = Get-ContainerId -Name "018-hai-backend"
$composeArguments = @("compose", "--env-file", $EnvFile, "-f", $composeFile)

Write-Host "Building the HAI frontend image..."
& docker @composeArguments build frontend
if ($LASTEXITCODE -ne 0) {
    throw "Frontend image build failed with exit code $LASTEXITCODE."
}

Write-Host "Recreating only the frontend container..."
& docker @composeArguments up -d --no-deps frontend
if ($LASTEXITCODE -ne 0) {
    throw "Frontend deployment failed with exit code $LASTEXITCODE."
}

$deadline = [DateTimeOffset]::UtcNow.AddSeconds($HealthTimeoutSeconds)
do {
    $frontendHealth = Get-ContainerHealth -Name "018-hai-frontend"
    if ($frontendHealth -eq "healthy") {
        break
    }
    if ($frontendHealth -in @("unhealthy", "exited", "dead", "missing")) {
        throw "Frontend entered the '$frontendHealth' state. Inspect it with 'docker logs 018-hai-frontend'."
    }
    Start-Sleep -Seconds 2
} while ([DateTimeOffset]::UtcNow -lt $deadline)

if ($frontendHealth -ne "healthy") {
    throw "Frontend did not become healthy within $HealthTimeoutSeconds seconds (last state: $frontendHealth)."
}

if (-not [string]::IsNullOrWhiteSpace($backendBefore)) {
    $backendAfter = Get-ContainerId -Name "018-hai-backend"
    if ($backendAfter -ne $backendBefore) {
        throw "Safety check failed: the backend container changed during a frontend-only deployment."
    }
    Write-Host "Frontend is healthy. Backend container identity was preserved." -ForegroundColor Green
} else {
    Write-Host "Frontend is healthy. No running backend was present to compare." -ForegroundColor Yellow
}
