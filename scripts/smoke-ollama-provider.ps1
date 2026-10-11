[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://127.0.0.1',
    [string]$EnvFile = (Join-Path $PSScriptRoot '..\.env.local'),
    [string]$ExpectedModel = ''
)

$ErrorActionPreference = 'Stop'

function Read-DotEnv {
    param([Parameter(Mandatory = $true)][string]$Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Environment file not found: $Path"
    }
    $values = @{}
    foreach ($rawLine in Get-Content -LiteralPath $Path) {
        $line = $rawLine.Trim()
        if (-not $line -or $line.StartsWith('#') -or -not $line.Contains('=')) {
            continue
        }
        $parts = $line.Split('=', 2)
        $value = $parts[1].Trim()
        if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$parts[0].Trim()] = $value
    }
    return $values
}

function Invoke-HaiApi {
    param(
        [Parameter(Mandatory = $true)][ValidateSet('GET', 'POST')][string]$Method,
        [Parameter(Mandatory = $true)][string]$Path,
        [object]$Body
    )

    $request = @{
        Uri = "$root$Path"
        Method = $Method
        WebSession = $haiSession
        UseBasicParsing = $true
    }
    if ($null -ne $Body) {
        $request.ContentType = 'application/json'
        $request.Body = $Body | ConvertTo-Json -Depth 6 -Compress
    }
    $response = Invoke-WebRequest @request
    if ([string]::IsNullOrWhiteSpace($response.Content)) {
        return $null
    }
    return $response.Content | ConvertFrom-Json
}

function ConvertTo-HaiArray {
    param([object]$Value)

    if ($null -eq $Value) {
        return @()
    }
    $wrappedValue = $Value.PSObject.Properties['value']
    if ($null -ne $wrappedValue) {
        return @($wrappedValue.Value)
    }
    return @($Value)
}

$baseUri = $null
if (-not [Uri]::TryCreate($BaseUrl, [UriKind]::Absolute, [ref]$baseUri) -or
    $baseUri.Scheme -notin @('http', 'https') -or
    -not $baseUri.IsLoopback -or
    $baseUri.UserInfo -or
    $baseUri.AbsolutePath -ne '/' -or
    $baseUri.Query -or
    $baseUri.Fragment) {
    throw 'BaseUrl must be a loopback HTTP(S) origin without credentials, path, query, or fragment.'
}

$configuration = Read-DotEnv -Path $EnvFile
$email = $configuration['FIRST_RUN_ADMIN_EMAIL']
$password = $configuration['FIRST_RUN_ADMIN_PASSWORD']
if (-not $email -or -not $password) {
    throw 'FIRST_RUN_ADMIN_EMAIL and FIRST_RUN_ADMIN_PASSWORD are required in the environment file.'
}
if ([string]::IsNullOrWhiteSpace($ExpectedModel)) {
    $configuredModels = [string]$configuration['OLLAMA_MODEL_IDS']
    $ExpectedModel = @($configuredModels.Split(',') | ForEach-Object { $_.Trim() } | Where-Object { $_ })[0]
}
if ([string]::IsNullOrWhiteSpace($ExpectedModel)) {
    throw 'ExpectedModel or OLLAMA_MODEL_IDS is required.'
}

$root = $baseUri.AbsoluteUri.TrimEnd('/')
$loginBody = @{ email = $email; password = $password } | ConvertTo-Json -Compress
Invoke-WebRequest -UseBasicParsing -Uri "$root/api/v1/auth/login" -Method Post -ContentType 'application/json' -Body $loginBody -SessionVariable haiSession | Out-Null

$probes = ConvertTo-HaiArray (Invoke-HaiApi -Method GET -Path '/api/v1/llm/probes')
$ollama = $probes | Where-Object { $_.providerId -eq 'ollama' } | Select-Object -First 1
if (-not $ollama -or -not $ollama.live -or $ollama.status -ne 'live' -or $ollama.modelsSeen -lt 1) {
    throw 'Ollama did not pass the persisted live-provider probe.'
}

$task = 'Return exactly HAI_LOCAL_OK and nothing else.'
$routeRequest = @{
    task = $task
    taskType = 'classification'
    difficulty = 1
    requiredReasoning = 'low'
}
$route = Invoke-HaiApi -Method POST -Path '/api/v1/llm/route' -Body $routeRequest
if ($route.selectedProviderId -ne 'ollama' -or $route.selectedModelId -ne $ExpectedModel -or $route.estimatedCostEur -ne 0 -or $route.requiresApproval) {
    throw 'The routing policy did not select the expected zero-cost local Ollama model.'
}

$generation = Invoke-HaiApi -Method POST -Path '/api/v1/llm/generate' -Body @{
    task = $task
    systemPrompt = 'Follow the requested output format exactly. Do not add explanation.'
    routeRequest = $routeRequest
    temperature = 0
    maxTokens = 16
}
if ($generation.status -ne 'completed' -or $generation.providerId -ne 'ollama' -or $generation.modelId -ne $ExpectedModel) {
    throw 'The bounded local generation did not complete on the expected Ollama model.'
}
if ($generation.output.Trim() -ne 'HAI_LOCAL_OK') {
    throw 'The local model did not satisfy the bounded fixed-output acceptance request.'
}
if ($generation.estimatedCostEur -ne 0 -or $generation.inputTokens -lt 1 -or $generation.outputTokens -lt 1 -or $generation.usageSource -ne 'provider_reported' -or $generation.auditStatus -ne 'recorded') {
    throw 'The local generation did not record exact zero-cost provider usage and audit evidence.'
}

$history = ConvertTo-HaiArray (Invoke-HaiApi -Method GET -Path '/api/v1/llm/generations?limit=10')
$record = $history | Where-Object { $_.generationId -eq $generation.generationId } | Select-Object -First 1
if (-not $record -or $record.telemetryId -ne $generation.telemetryId -or $record.status -ne 'completed' -or $record.auditStatus -ne 'recorded') {
    throw 'The redacted generation history does not contain the matching audit and telemetry record.'
}
if ($record.PSObject.Properties.Name -contains 'output') {
    throw 'Redacted generation history unexpectedly contains model output.'
}

$readiness = Invoke-HaiApi -Method GET -Path '/readyz'
if ($readiness.status -ne 'ready' -or $readiness.summary.fail -ne 0 -or $readiness.summary.warn -ne 0) {
    throw 'HAI readiness is not fully healthy after local model generation.'
}

[pscustomobject]@{
    Provider = $generation.providerId
    Model = $generation.modelId
    Status = $generation.status
    CostEUR = $generation.estimatedCostEur
    InputTokens = $generation.inputTokens
    OutputTokens = $generation.outputTokens
    UsageSource = $generation.usageSource
    AuditStatus = $generation.auditStatus
    TelemetryID = $generation.telemetryId
    Readiness = $readiness.status
}
