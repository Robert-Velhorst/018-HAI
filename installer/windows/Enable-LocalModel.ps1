[CmdletBinding()]
param(
    [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$')]
    [string]$ModelID = "qwen2.5:0.5b",

    [ValidateRange(30, 900)]
    [int]$HealthTimeoutSeconds = 600,

    [switch]$NoBrowser
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "Hai-InstallerSupport.ps1")

function Set-HaiEnvironmentValue {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Value
    )

    $environmentFile = Get-HaiEnvironmentFile
    $content = [IO.File]::ReadAllText($environmentFile)
    $pattern = "(?m)^" + [Regex]::Escape($Name) + "=.*$"
    if (-not [Regex]::IsMatch($content, $pattern)) {
        throw "HAI environment file is missing $Name. Reinstall HAI or restore a valid configuration."
    }
    $replacement = [Text.RegularExpressions.MatchEvaluator]{
        param($match)
        return "$Name=$Value"
    }
    $content = [Regex]::Replace($content, $pattern, $replacement)
    [IO.File]::WriteAllText($environmentFile, $content, [Text.UTF8Encoding]::new($false))
}

function Wait-HaiComposeServiceHealthy {
    param(
        [Parameter(Mandatory = $true)][string]$Service,
        [ValidateRange(30, 900)][int]$TimeoutSeconds = 600
    )

    $composeArguments = Get-HaiComposeArguments
    $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $containerIDLines = @(& docker @composeArguments --profile local-model ps -q $Service 2>$null)
        $serviceExitCode = $LASTEXITCODE
        $containerIDLine = $containerIDLines | Select-Object -First 1
        if ($serviceExitCode -eq 0 -and -not [string]::IsNullOrWhiteSpace($containerIDLine)) {
            $containerID = $containerIDLine.Trim()
            $health = & docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' $containerID 2>$null
            if ($LASTEXITCODE -eq 0 -and $health.Trim() -eq "healthy") {
                return
            }
            if ($health.Trim() -eq "unhealthy" -or $health.Trim() -eq "exited") {
                throw "$Service stopped before becoming healthy. Open Docker Desktop and inspect its logs."
            }
        }
        Start-Sleep -Seconds 3
    } while ([DateTimeOffset]::UtcNow -lt $deadline)

    throw "$Service did not become healthy within $TimeoutSeconds seconds."
}

Assert-HaiDockerReady
Assert-HaiSingleInstallation
Initialize-HaiLocalEnvironment

$composeArguments = Get-HaiComposeArguments
Write-Host "Starting the private local-model service. Model downloads can take several minutes." -ForegroundColor Cyan
& docker @composeArguments --profile local-model up -d --build ollama-local
if ($LASTEXITCODE -ne 0) {
    throw "The local-model service could not start. Open Docker Desktop and inspect the ollama-local logs."
}

Wait-HaiComposeServiceHealthy -Service "ollama-local" -TimeoutSeconds $HealthTimeoutSeconds
Write-Host "Downloading the reviewed local model $ModelID. No HAI source data is sent to this service." -ForegroundColor Cyan
& docker @composeArguments --profile local-model exec -T ollama-local ollama pull $ModelID
if ($LASTEXITCODE -ne 0) {
    throw "The local model download failed. HAI was not reconfigured to route requests to it."
}

Set-HaiEnvironmentValue -Name "OLLAMA_BASE_URL" -Value "http://ollama-local:11434"
Set-HaiEnvironmentValue -Name "OLLAMA_MODEL_IDS" -Value $ModelID

& docker @composeArguments --profile local-model up -d --build backend
if ($LASTEXITCODE -ne 0) {
    throw "HAI could not restart with the local model configuration. Inspect the backend logs before retrying."
}

Wait-HaiReady -TimeoutSeconds $HealthTimeoutSeconds
$url = Get-HaiUrl
Write-Host "HAI is ready with local model $ModelID at $url" -ForegroundColor Green
if (-not $NoBrowser) {
    Start-Process $url
}
