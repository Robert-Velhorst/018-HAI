[CmdletBinding()]
param(
    [ValidateRange(1, 60)]
    [int]$TimeoutSeconds = 10,
    [ValidateLength(1, 1000)]
    [string]$Request = "Prepare a source-backed review checklist without sending or changing anything."
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "Hai-InstallerSupport.ps1")

Assert-HaiDockerReady
Assert-HaiSingleInstallation

$environmentFile = Get-HaiEnvironmentFile
if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
    throw "HAI is not initialized. Use Start HAI before testing the local connector."
}

$environmentContent = [IO.File]::ReadAllText($environmentFile)
function Get-LocalEnvironmentValue {
    param([Parameter(Mandatory = $true)][string]$Name)
    $escapedName = [Regex]::Escape($Name)
    $match = [Regex]::Match($environmentContent, "(?m)^$escapedName=(?<value>.*)$")
    if (-not $match.Success) {
        return ""
    }
    return $match.Groups["value"].Value.Trim().Trim('"').Trim("'")
}

if ((Get-LocalEnvironmentValue -Name "GATEWAY_HOST_BIND") -ne "127.0.0.1") {
    throw "The HAI gateway is not configured for loopback-only access. Do not test the local connector until the installation is repaired."
}
if ((Get-LocalEnvironmentValue -Name "HAI_A2A_BRIDGE_ENABLED").ToLowerInvariant() -ne "true") {
    throw "The local HAI connector is disabled. Start HAI again to restore the installed local planning connector."
}
$token = Get-LocalEnvironmentValue -Name "HAI_A2A_BRIDGE_TOKEN"
if ($token.Length -lt 32) {
    throw "The local HAI connector token is missing or invalid. Start HAI again to regenerate the installed local environment."
}

$baseUrl = (Get-HaiUrl).TrimEnd("/")
$agentCardUrl = "$baseUrl/.well-known/agent-card.json"
$planningUrl = "$baseUrl/api/v1/a2a"
try {
    $agentCard = Invoke-RestMethod -UseBasicParsing -Uri $agentCardUrl -TimeoutSec $TimeoutSeconds
} catch {
    throw "The local HAI connector Agent Card is unavailable at $agentCardUrl. Start HAI and wait for readiness before retrying."
}

$planningSkill = @($agentCard.skills | Where-Object { $_.id -eq "hai_controlled_planning" }) | Select-Object -First 1
$planningInterface = @($agentCard.supportedInterfaces | Where-Object {
    [string]$_.protocolBinding -eq "JSONRPC" -and
    [string]$_.protocolVersion -eq "1.0" -and
    [string]$_.url -eq $planningUrl
}) | Select-Object -First 1
if ($agentCard.name -ne "HAI controlled planning" -or $null -eq $planningSkill -or $null -eq $planningInterface) {
    throw "The local HAI connector returned an unexpected Agent Card. Do not connect another tool until the HAI installation has been repaired."
}

$requestId = [guid]::NewGuid().ToString()
$planningRequest = @{
    jsonrpc = "2.0"
    id = $requestId
    method = "SendMessage"
    params = @{
        message = @{
            messageId = [guid]::NewGuid().ToString()
            role = "ROLE_USER"
            parts = @(@{ text = $Request; mediaType = "text/plain" })
        }
    }
} | ConvertTo-Json -Depth 8 -Compress

try {
    $result = Invoke-RestMethod -UseBasicParsing -Method Post -Uri $planningUrl `
        -Headers @{ Authorization = "Bearer $token"; "A2A-Version" = "1.0" } `
        -ContentType "application/json" -Body $planningRequest -TimeoutSec 15
} catch {
    throw "The local HAI connector rejected its controlled planning check. Verify that HAI is ready and that no other local gateway is using this port."
}

$task = $result.result.task
$artifact = @($task.artifacts | Where-Object { $_.name -eq "hai-controlled-planning-proposal" }) | Select-Object -First 1
$scope = [string]$artifact.parts[0].data.scope
if ($task.status.state -ne "TASK_STATE_COMPLETED" -or
    $artifact.description -notmatch "Non-executable planning" -or
    $scope -notmatch "Planning draft only") {
    throw "The local HAI connector returned an unexpected result. No external work was performed, but the connector should be repaired before use."
}

Write-Host "Local HAI connector passed: Agent Card and authenticated planning probe are available at $baseUrl." -ForegroundColor Green
Write-Host "The probe returned a non-executable planning draft; it did not create or execute work."
