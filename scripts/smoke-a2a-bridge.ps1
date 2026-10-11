[CmdletBinding()]
param(
    [string]$EnvFile = '.env.local',
    [switch]$Public
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Net.Http
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$envPath = if ([IO.Path]::IsPathRooted($EnvFile)) {
    [IO.Path]::GetFullPath($EnvFile)
} else {
    [IO.Path]::GetFullPath((Join-Path $repoRoot $EnvFile))
}
if (-not (Test-Path -LiteralPath $envPath -PathType Leaf)) {
    throw "Environment file not found: $envPath"
}

function Read-DotEnv([string]$PathValue) {
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $PathValue) {
        if ($line -notmatch '^\s*([A-Za-z_][A-Za-z0-9_]*)=(.*)$') {
            continue
        }
        $value = $Matches[2].Trim()
        if (($value.StartsWith('"') -and $value.EndsWith('"')) -or
            ($value.StartsWith("'") -and $value.EndsWith("'"))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$Matches[1]] = $value
    }
    return $values
}

function Require-Value([hashtable]$Values, [string]$Name) {
    $value = [Environment]::GetEnvironmentVariable($Name)
    if ([string]::IsNullOrWhiteSpace($value) -and $Values.ContainsKey($Name)) {
        $value = [string]$Values[$Name]
    }
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "$Name is required for the A2A smoke test."
    }
    return $value
}

function Get-Value([hashtable]$Values, [string]$Name, [string]$Default = '') {
    $value = [Environment]::GetEnvironmentVariable($Name)
    if (-not [string]::IsNullOrWhiteSpace($value)) {
        return $value
    }
    if ($Values.ContainsKey($Name)) {
        return [string]$Values[$Name]
    }
    return $Default
}

function Send-Request(
    [System.Net.Http.HttpClient]$Client,
    [System.Net.Http.HttpMethod]$Method,
    [string]$Url,
    [string]$Body = '',
    [string]$Token = ''
) {
    $request = [System.Net.Http.HttpRequestMessage]::new($Method, $Url)
    try {
        if ($Token) {
            $request.Headers.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $Token)
        }
        if ($Body) {
            $request.Headers.Add('A2A-Version', '1.0')
            $request.Content = [System.Net.Http.StringContent]::new($Body, [Text.Encoding]::UTF8, 'application/json')
        }
        $response = $Client.SendAsync($request).GetAwaiter().GetResult()
        $content = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
        return [pscustomobject]@{ Status = [int]$response.StatusCode; Body = $content }
    } finally {
        $request.Dispose()
    }
}

$settings = Read-DotEnv $envPath
if ((Get-Value $settings 'HAI_A2A_BRIDGE_ENABLED' 'false').ToLowerInvariant() -ne 'true') {
    throw 'HAI_A2A_BRIDGE_ENABLED must be true before running this smoke test.'
}
$token = Require-Value $settings 'HAI_A2A_BRIDGE_TOKEN'
$advertisedEndpoint = Require-Value $settings 'HAI_A2A_BRIDGE_URL'
$baseUrl = if ($Public) {
    (Require-Value $settings 'HAI_NGROK_URL').TrimEnd('/')
} else {
    $port = Get-Value $settings 'GATEWAY_HOST_PORT' '8088'
    "http://127.0.0.1:$port"
}

$client = [System.Net.Http.HttpClient]::new()
$client.Timeout = [TimeSpan]::FromSeconds(15)
if ($Public) {
    # Free ngrok endpoints can otherwise return the browser interstitial to the
    # Agent Card GET. This header is not identity and does not replace the
    # dedicated Bearer token required by SendMessage.
    $client.DefaultRequestHeaders.TryAddWithoutValidation(
        'ngrok-skip-browser-warning',
        'hai-a2a-readiness'
    ) | Out-Null
}
try {
    $card = Send-Request $client ([System.Net.Http.HttpMethod]::Get) "$baseUrl/.well-known/agent-card.json"
    if ($card.Status -ne 200) {
        throw "Agent Card returned HTTP $($card.Status)."
    }
    $cardPayload = $card.Body | ConvertFrom-Json
    if ($cardPayload.supportedInterfaces[0].url -ne $advertisedEndpoint -or $cardPayload.capabilities.streaming) {
        throw 'Agent Card does not match the configured bounded connector.'
    }

    $rpcBody = @{
        jsonrpc = '2.0'
        id = 'hai-smoke-1'
        method = 'SendMessage'
        params = @{
            message = @{
                messageId = 'hai-smoke-message-1'
                role = 'ROLE_USER'
                parts = @(@{ text = 'Create a bounded readiness plan without taking action.'; mediaType = 'text/plain' })
            }
        }
    } | ConvertTo-Json -Depth 8 -Compress

    $denied = Send-Request $client ([System.Net.Http.HttpMethod]::Post) "$baseUrl/api/v1/a2a" $rpcBody
    if ($denied.Status -ne 404) {
        throw "Unauthenticated SendMessage returned HTTP $($denied.Status), expected 404."
    }

    $accepted = Send-Request $client ([System.Net.Http.HttpMethod]::Post) "$baseUrl/api/v1/a2a" $rpcBody $token
    if ($accepted.Status -ne 200) {
        throw "Authenticated SendMessage returned HTTP $($accepted.Status): $($accepted.Body)"
    }
    $result = $accepted.Body | ConvertFrom-Json
    $proposal = $result.result.task.artifacts[0].parts[0].data
    if ($result.result.task.status.state -ne 'TASK_STATE_COMPLETED' -or
        $proposal.scope -notmatch 'Planning draft only' -or
        $proposal.scope -notmatch 'did not create a task') {
        throw 'A2A response did not prove a completed, non-executable planning draft.'
    }

    Write-Host "A2A connector smoke passed through $baseUrl (token not displayed)."
} finally {
    $client.Dispose()
}
