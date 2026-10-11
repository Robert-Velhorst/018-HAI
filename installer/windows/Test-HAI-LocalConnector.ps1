[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "Hai-InstallerSupport.ps1")

Assert-HaiDockerReady
$environmentFile = Get-HaiEnvironmentFile
if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
    throw "HAI is not initialized. Use Start HAI before testing the local connector."
}

$agentCardUrl = "$(Get-HaiA2AUrl)/.well-known/agent-card.json"
try {
    $response = Invoke-WebRequest -UseBasicParsing -Uri $agentCardUrl -TimeoutSec 10
} catch {
    throw "The local A2A connector is unavailable at $agentCardUrl. Start HAI, then try again."
}

if ($response.StatusCode -ne 200) {
    throw "The local A2A connector returned HTTP $($response.StatusCode) at $agentCardUrl."
}

try {
    $agentCard = $response.Content | ConvertFrom-Json -ErrorAction Stop
} catch {
    throw "The local A2A connector returned an invalid Agent Card."
}

if ([string]::IsNullOrWhiteSpace($agentCard.name)) {
    throw "The local A2A connector returned an incomplete Agent Card."
}

$planningUrl = "$(Get-HaiA2AUrl)/api/v1/a2a"
$planningInterface = @($agentCard.supportedInterfaces | Where-Object {
    [string]$_.protocolBinding -eq "JSONRPC" -and
    [string]$_.protocolVersion -eq "1.0" -and
    [string]$_.url -eq $planningUrl
}) | Select-Object -First 1
if ($null -eq $planningInterface) {
    throw "The local A2A Agent Card does not advertise the expected JSON-RPC planning endpoint: $planningUrl."
}

$bridgeToken = Get-HaiEnvironmentValue -Name "HAI_A2A_BRIDGE_TOKEN"
if ($bridgeToken.Length -lt 32) {
    throw "The local A2A connector token is missing or invalid. Start HAI again to recreate the local configuration."
}

$planningRequest = @{
    jsonrpc = "2.0"
    id = [guid]::NewGuid().ToString()
    method = "SendMessage"
    params = @{
        message = @{
            messageId = [guid]::NewGuid().ToString()
            role = "ROLE_USER"
            parts = @(@{ text = "Create a source-backed plan without taking action."; mediaType = "text/plain" })
        }
    }
} | ConvertTo-Json -Depth 8 -Compress

try {
    $planningResponse = Invoke-WebRequest -UseBasicParsing -Method Post -Uri $planningUrl -TimeoutSec 15 `
        -ContentType "application/json" `
        -Headers @{ Authorization = "Bearer $bridgeToken"; "A2A-Version" = "1.0" } `
        -Body $planningRequest
} catch {
    throw "The local A2A planning endpoint is unavailable at $planningUrl. Start HAI, then try again."
}

if ($planningResponse.StatusCode -ne 200) {
    throw "The local A2A planning endpoint returned HTTP $($planningResponse.StatusCode)."
}

try {
    $planningResult = $planningResponse.Content | ConvertFrom-Json -ErrorAction Stop
} catch {
    throw "The local A2A planning endpoint returned invalid JSON."
}

$proposal = $planningResult.result.task.artifacts | Where-Object { $_.name -eq "hai-controlled-planning-proposal" } | Select-Object -First 1
if ($planningResult.result.task.status.state -ne "TASK_STATE_COMPLETED" -or $null -eq $proposal -or [string]$proposal.parts[0].data.scope -notmatch "Planning draft only") {
    throw "The local A2A planning endpoint did not return the expected non-executable planning result."
}

Write-Host "Local A2A connector is reachable at $agentCardUrl" -ForegroundColor Green
Write-Host "Agent: $($agentCard.name)"
Write-Host "Planning endpoint: $($planningInterface.url)"
Write-Host "Authenticated planning probe completed without creating or executing work." -ForegroundColor Green
