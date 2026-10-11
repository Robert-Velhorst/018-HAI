[CmdletBinding()]
param(
    [string]$ConfigFile = (Join-Path $env:LOCALAPPDATA 'ngrok\ngrok.yml'),
    [ValidateRange(1024, 65535)][int]$InspectionPort = 4041,
    [ValidateRange(5, 120)][int]$TimeoutSeconds = 20
)

$ErrorActionPreference = 'Stop'

function Test-NgrokHost([string]$HostName) {
    $normalized = $HostName.Trim().ToLowerInvariant()
    foreach ($suffix in @('.ngrok.app', '.ngrok.dev', '.ngrok-free.app', '.ngrok-free.dev')) {
        if ($normalized.Length -gt $suffix.Length -and $normalized.EndsWith($suffix)) {
            return $true
        }
    }
    return $false
}

function Get-SafeNgrokFailure([string[]]$LogPaths) {
    $messages = [Collections.Generic.List[string]]::new()
    foreach ($logPath in $LogPaths) {
        if (-not (Test-Path -LiteralPath $logPath -PathType Leaf)) {
            continue
        }
        foreach ($line in Get-Content -LiteralPath $logPath -ErrorAction SilentlyContinue) {
            try {
                $record = $line | ConvertFrom-Json -ErrorAction Stop
                foreach ($property in @('err', 'error', 'msg')) {
                    $value = [string]$record.$property
                    if ($value -and $value -notin @(
                        'starting web service',
                        'client session established',
                        'received stop request',
                        'terminating with error',
                        'command failed'
                    )) {
                        $messages.Add($value)
                    }
                }
            } catch {
                # Ignore non-JSON agent output instead of returning unreviewed text.
            }
        }
    }
    if ($messages.Count -eq 0) {
        return 'No safe diagnostic was emitted.'
    }
    $summary = ($messages | Select-Object -Unique | Select-Object -First 6) -join ' | '
    $summary = $summary -replace '(?i)((?:auth|api)[_-]?token|api[_-]?key)([\s"''=:]+)[^\s,"'']+', '$1$2[redacted]'
    $summary = $summary -replace '[A-Za-z0-9_-]{32,}', '[redacted]'
    if ($summary -match '(?i)ERR_NGROK_334|already online') {
        return 'The configured ngrok endpoint is already online under another agent (ERR_NGROK_334). HAI did not stop or take over that agent. Provision a dedicated fixed HAI endpoint, or intentionally stop the known owner before retrying.'
    }
    if ($summary.Length -gt 600) {
        $summary = $summary.Substring(0, 600)
    }
    return $summary
}

$configPath = [IO.Path]::GetFullPath($ConfigFile)
if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
    throw "ngrok configuration not found: $configPath"
}

$ngrok = Get-Command ngrok -ErrorAction Stop
& $ngrok.Source config check --config $configPath | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw 'ngrok rejected the configured profile.'
}

$existingListener = Get-NetTCPConnection -State Listen -LocalPort $InspectionPort -ErrorAction SilentlyContinue
if ($existingListener) {
    throw "Inspection port $InspectionPort is already in use; choose another port so existing ngrok agents remain untouched."
}

$runID = [Guid]::NewGuid().ToString('N')
$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$overlayPath = Join-Path $tempRoot "hai-ngrok-discovery-$runID.yml"
$stdoutPath = Join-Path $tempRoot "hai-ngrok-discovery-$runID.out.log"
$stderrPath = Join-Path $tempRoot "hai-ngrok-discovery-$runID.err.log"
$process = $null

try {
    $overlay = @"
version: 3
agent:
  web_addr: 127.0.0.1:$InspectionPort
  update_check: false
"@
    [IO.File]::WriteAllText($overlayPath, $overlay, [Text.UTF8Encoding]::new($false))

    $arguments = @(
        'http',
        '127.0.0.1:1',
        '--config', $configPath,
        '--config', $overlayPath,
        '--log', 'stdout',
        '--log-format', 'json',
        '--log-level', 'info'
    )
    $process = Start-Process -FilePath $ngrok.Source `
        -ArgumentList $arguments `
        -RedirectStandardOutput $stdoutPath `
        -RedirectStandardError $stderrPath `
        -PassThru `
        -WindowStyle Hidden

    $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
    $endpoint = $null
    while ([DateTimeOffset]::UtcNow -lt $deadline) {
        $process.Refresh()
        if ($process.HasExited) {
            break
        }
        try {
            $response = Invoke-RestMethod `
                -Uri "http://127.0.0.1:$InspectionPort/api/tunnels" `
                -TimeoutSec 2
            $endpoint = @($response.tunnels | Where-Object { $_.proto -eq 'https' } | Select-Object -First 1).public_url
            if ($endpoint) {
                break
            }
        } catch {
            # The isolated agent API is expected to be unavailable during startup.
        }
        Start-Sleep -Milliseconds 500
    }

    $publicUri = $null
    if (-not $endpoint -or
        -not [Uri]::TryCreate($endpoint, [UriKind]::Absolute, [ref]$publicUri) -or
        $publicUri.Scheme -ne 'https' -or
        -not $publicUri.IsDefaultPort -or
        $publicUri.AbsolutePath -ne '/' -or
        $publicUri.UserInfo -or
        $publicUri.Query -or
        $publicUri.Fragment -or
        -not (Test-NgrokHost $publicUri.DnsSafeHost)) {
        $diagnostic = Get-SafeNgrokFailure @($stdoutPath, $stderrPath)
        throw "The isolated ngrok agent did not return a valid HTTPS ngrok endpoint. $diagnostic"
    }

    Write-Output $publicUri.AbsoluteUri.TrimEnd('/')
} finally {
    if ($process) {
        $process.Refresh()
        if (-not $process.HasExited) {
            Stop-Process -Id $process.Id -Force
            $process.WaitForExit()
        }
    }
    foreach ($path in @($overlayPath, $stdoutPath, $stderrPath)) {
        $resolved = [IO.Path]::GetFullPath($path)
        if ($resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -and
            (Test-Path -LiteralPath $resolved -PathType Leaf)) {
            Remove-Item -LiteralPath $resolved -Force
        }
    }
}
