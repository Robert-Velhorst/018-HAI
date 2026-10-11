[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Net.Http
$launcherPath = Join-Path $PSScriptRoot 'start-ngrok.ps1'
$discoveryPath = Join-Path $PSScriptRoot 'discover-ngrok-windows.ps1'

$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile(
    $launcherPath,
    [ref]$tokens,
    [ref]$errors
)
if ($errors.Count -gt 0) {
    throw 'start-ngrok.ps1 contains parse errors.'
}
foreach ($name in @(
    'Read-BoundedResponseBody',
    'Invoke-PublicOwnershipProbe',
    'Test-PublicEndpointOwnership'
)) {
    $function = $ast.Find({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and
            $node.Name -eq $name
    }, $true)
    if ($null -eq $function) {
        throw "Launcher function not found: $name"
    }
    Invoke-Expression $function.Extent.Text
}

$discoveryTokens = $null
$discoveryErrors = $null
$discoveryAst = [Management.Automation.Language.Parser]::ParseFile(
    $discoveryPath,
    [ref]$discoveryTokens,
    [ref]$discoveryErrors
)
if ($discoveryErrors.Count -gt 0) {
    throw 'discover-ngrok-windows.ps1 contains parse errors.'
}
$safeFailureFunction = $discoveryAst.Find({
    param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'Get-SafeNgrokFailure'
}, $true)
if ($null -eq $safeFailureFunction) {
    throw 'Discovery diagnostic function not found: Get-SafeNgrokFailure'
}
Invoke-Expression $safeFailureFunction.Extent.Text

$diagnosticPath = Join-Path ([IO.Path]::GetTempPath()) ("hai-ngrok-diagnostic-" + [Guid]::NewGuid().ToString('N') + '.log')
try {
    $fixture = '{"err":"endpoint already online: ERR_NGROK_334","api_token":"this-value-must-never-appear-in-diagnostics"}'
    [IO.File]::WriteAllText($diagnosticPath, $fixture, [Text.UTF8Encoding]::new($false))
    $diagnostic = Get-SafeNgrokFailure @($diagnosticPath)
    if ($diagnostic -notmatch 'ERR_NGROK_334' -or
        $diagnostic -notmatch 'did not stop or take over' -or
        $diagnostic -match 'this-value-must-never-appear') {
        throw "Unsafe or unactionable discovery diagnostic: $diagnostic"
    }
} finally {
    Remove-Item -LiteralPath $diagnosticPath -Force -ErrorAction SilentlyContinue
}

function Get-FreeLoopbackPort {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try {
        return ([Net.IPEndPoint]$listener.LocalEndpoint).Port
    } finally {
        $listener.Stop()
    }
}

function Start-ProbeServer([int]$Port, [string]$Body) {
    return Start-Job -ArgumentList $Port, $Body -ScriptBlock {
        param($serverPort, $responseBody)
        $listener = [Net.HttpListener]::new()
        $listener.Prefixes.Add("http://127.0.0.1:$serverPort/")
        $listener.Start()
        try {
            $context = $listener.GetContext()
            $bytes = [Text.Encoding]::UTF8.GetBytes($responseBody)
            $context.Response.StatusCode = 200
            $context.Response.ContentType = 'application/json'
            $context.Response.ContentLength64 = $bytes.Length
            $context.Response.OutputStream.Write($bytes, 0, $bytes.Length)
            $context.Response.Close()
        } finally {
            $listener.Stop()
            $listener.Close()
        }
    }
}

function Invoke-ProbeScenario(
    [string]$Body,
    [bool]$CurrentProjectTunnelRunning,
    [string]$ExpectedError = ''
) {
    $port = Get-FreeLoopbackPort
    $job = Start-ProbeServer $port $Body
    try {
        Start-Sleep -Milliseconds 300
        if ($ExpectedError) {
            try {
                Test-PublicEndpointOwnership `
                    "http://127.0.0.1:$port" `
                    $CurrentProjectTunnelRunning
                throw "Ownership probe unexpectedly passed; expected $ExpectedError"
            } catch {
                if ($_.Exception.Message -notmatch [Regex]::Escape($ExpectedError)) {
                    throw
                }
            }
        } else {
            Test-PublicEndpointOwnership `
                "http://127.0.0.1:$port" `
                $CurrentProjectTunnelRunning
        }
        Wait-Job $job -Timeout 10 | Out-Null
        if ($job.State -ne 'Completed') {
            throw "Ownership fixture did not complete: $($job.State)"
        }
    } finally {
        Stop-Job $job -ErrorAction SilentlyContinue
        Remove-Job $job -Force -ErrorAction SilentlyContinue
    }
}

Invoke-ProbeScenario `
    '{"service":"other","status":"ok"}' `
    $false `
    'non-HAI application'
Invoke-ProbeScenario `
    '{"service":"backend","status":"ok"}' `
    $false `
    'different HAI tunnel'
Invoke-ProbeScenario `
    '{"service":"backend","status":"ok"}' `
    $true

Write-Host 'Ngrok endpoint ownership tests passed.'
