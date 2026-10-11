[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

function Get-HaiInstallRoot {
    return (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..\..")).Path
}

function Get-HaiDataRoot {
    $dataRoot = Join-Path $env:LOCALAPPDATA "HAI"
    if (-not (Test-Path -LiteralPath $dataRoot -PathType Container)) {
        New-Item -ItemType Directory -Path $dataRoot -Force | Out-Null
    }
    return $dataRoot
}

function Get-HaiEnvironmentFile {
    return (Join-Path (Get-HaiDataRoot) "hai.env")
}

function Get-HaiComposeFile {
    $composeFile = Join-Path (Get-HaiInstallRoot) "docker-compose.local.yml"
    if (-not (Test-Path -LiteralPath $composeFile -PathType Leaf)) {
        throw "HAI installation is incomplete: missing $composeFile"
    }
    return $composeFile
}

function Get-HaiGatewayPort {
    $environmentFile = Get-HaiEnvironmentFile
    if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
        return 8088
    }

    $match = [Regex]::Match(
        [IO.File]::ReadAllText($environmentFile),
        "(?m)^GATEWAY_HOST_PORT=(?<port>[0-9]{1,5})$"
    )
    if (-not $match.Success) {
        return 8088
    }

    $port = [int]$match.Groups["port"].Value
    if ($port -lt 1 -or $port -gt 65535) {
        throw "HAI environment file contains an invalid gateway port."
    }
    return $port
}

function Get-HaiUrl {
    return "http://127.0.0.1:$(Get-HaiGatewayPort)"
}

function Assert-HaiDockerReady {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        throw "Docker Desktop is required. Install and start Docker Desktop, then run Start HAI again."
    }

    $serverVersion = & docker version --format '{{.Server.Version}}' 2>$null
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($serverVersion)) {
        throw "Docker Desktop is installed but its Linux engine is not ready. Start Docker Desktop and wait for it to finish starting."
    }

    & docker compose version *> $null
    if ($LASTEXITCODE -ne 0) {
        throw "Docker Compose v2 is required. Update Docker Desktop, then run Start HAI again."
    }
}

function Assert-HaiSingleInstallation {
    $installRoot = Get-HaiInstallRoot
    $containerIds = @(& docker ps -aq --filter "label=com.docker.compose.project=018-hai")
    if ($LASTEXITCODE -ne 0 -or $containerIds.Count -eq 0) {
        return
    }

    $workingDirectories = @(
        $containerIds |
            ForEach-Object {
                $labelLines = @(& docker inspect --format '{{json .Config.Labels}}' $_ 2>$null)
                $inspectExitCode = $LASTEXITCODE
                $labelsJSON = $labelLines | Select-Object -First 1
                if ($inspectExitCode -ne 0 -or [string]::IsNullOrWhiteSpace($labelsJSON)) {
                    throw "Could not inspect the existing HAI container $_ before checking its installation directory."
                }
                try {
                    $labels = $labelsJSON | ConvertFrom-Json -ErrorAction Stop
                } catch {
                    throw "Existing HAI container $_ returned invalid Compose labels. Stop it manually before starting this installation."
                }
                [string]$labels.'com.docker.compose.project.working_dir'
            } |
            Where-Object { -not [string]::IsNullOrWhiteSpace($_) } |
            Select-Object -Unique
    )
    foreach ($workingDirectory in $workingDirectories) {
        if (-not [string]::Equals(
            [IO.Path]::GetFullPath($workingDirectory),
            [IO.Path]::GetFullPath($installRoot),
            [StringComparison]::OrdinalIgnoreCase
        )) {
            throw "HAI is already running from '$workingDirectory'. Stop that installation before starting this one so HAI keeps one canonical local stack and one set of Docker volumes."
        }
    }
}

function Get-HaiComposeArguments {
    return @("compose", "--env-file", (Get-HaiEnvironmentFile), "-f", (Get-HaiComposeFile))
}

function Initialize-HaiLocalEnvironment {
    param([ValidateRange(1, 65535)][int]$GatewayPort = 8088)

    $environmentFile = Get-HaiEnvironmentFile
    if (Test-Path -LiteralPath $environmentFile -PathType Leaf) {
        return
    }

    $initializer = Join-Path (Get-HaiInstallRoot) "scripts\initialize-windows.ps1"
    & $initializer -EnvFile $environmentFile -GatewayPort $GatewayPort
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
        throw "HAI environment initialization failed. Check the setup prompts and try Start HAI again."
    }
}

function Wait-HaiReady {
    param([ValidateRange(30, 900)][int]$TimeoutSeconds = 600)

    $uri = "$(Get-HaiUrl)/readyz"
    $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $uri -TimeoutSec 5
            if ($response.StatusCode -eq 200) {
                return
            }
        } catch {
            # The stack is still building or waiting for dependencies.
        }
        Start-Sleep -Seconds 3
    } while ([DateTimeOffset]::UtcNow -lt $deadline)

    throw "HAI did not become ready within $TimeoutSeconds seconds. Open Docker Desktop and inspect the 018-hai containers."
}
