[CmdletBinding(DefaultParameterSetName = 'Start')]
param(
    [Parameter(ParameterSetName = 'Start')]
    [switch]$ValidateOnly,

    [Parameter(ParameterSetName = 'Stop', Mandatory = $true)]
    [switch]$Stop,

    [string]$EnvFile = '.env.local',
    [string]$ComposeFile = 'docker-compose.local.yml'
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Net.Http
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))

function Resolve-RepoFile([string]$PathValue) {
    $candidate = if ([IO.Path]::IsPathRooted($PathValue)) {
        $PathValue
    } else {
        Join-Path $repoRoot $PathValue
    }
    $resolved = [IO.Path]::GetFullPath($candidate)
    if (-not (Test-Path -LiteralPath $resolved -PathType Leaf)) {
        throw "Required file not found: $resolved"
    }
    return $resolved
}

function Read-DotEnv([string]$PathValue) {
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $PathValue) {
        if ($line -notmatch '^\s*([A-Za-z_][A-Za-z0-9_]*)=(.*)$') {
            continue
        }
        $name = $Matches[1]
        $value = $Matches[2].Trim()
        if (($value.StartsWith('"') -and $value.EndsWith('"')) -or
            ($value.StartsWith("'") -and $value.EndsWith("'"))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$name] = $value
    }
    return $values
}

function Get-Setting([hashtable]$Values, [string]$Name, [string]$Default = '') {
    if ($Values.ContainsKey($Name)) {
        return [string]$Values[$Name]
    }
    return $Default
}

function Require-Secret([hashtable]$Values, [string]$Name, [int]$MinimumLength) {
    $value = Get-Setting $Values $Name
    if ($value.Length -lt $MinimumLength -or $value -match '(?i)change[-_ ]?this|changeme|example|placeholder') {
        throw "$Name must be a non-placeholder secret of at least $MinimumLength characters before public access."
    }
}

function Test-NgrokHostname([string]$HostName) {
    $normalized = $HostName.Trim().ToLowerInvariant()
    foreach ($suffix in @('.ngrok.app', '.ngrok.dev', '.ngrok-free.app', '.ngrok-free.dev')) {
        if ($normalized.Length -gt $suffix.Length -and $normalized.EndsWith($suffix)) {
            return $true
        }
    }
    return $false
}

$envPath = Resolve-RepoFile $EnvFile
$composePath = Resolve-RepoFile $ComposeFile

if ($Stop) {
    & docker compose --env-file $envPath --profile cloud-tunnel -f $composePath stop ngrok
    if ($LASTEXITCODE -ne 0) {
        throw 'Failed to stop the ngrok service.'
    }
    return
}

$settings = Read-DotEnv $envPath
if ((Get-Setting $settings 'RUN_MODE' 'production').ToLowerInvariant() -ne 'production') {
    throw 'RUN_MODE must be production before public access is enabled.'
}
if ((Get-Setting $settings 'LOCAL_LOGIN_BYPASS_ENABLED' 'false').ToLowerInvariant() -ne 'false') {
    throw 'LOCAL_LOGIN_BYPASS_ENABLED must be false before public access is enabled.'
}
if ((Get-Setting $settings 'IDP_COOKIE_SECURE' 'false').ToLowerInvariant() -ne 'true') {
    throw 'IDP_COOKIE_SECURE must be true before public access is enabled.'
}
if ((Get-Setting $settings 'GATEWAY_HOST_BIND' '127.0.0.1') -ne '127.0.0.1') {
    throw 'GATEWAY_HOST_BIND must remain 127.0.0.1; ngrok reaches nginx on the private Docker network.'
}
$rateLimit = 0
if (-not [int]::TryParse((Get-Setting $settings 'RATE_LIMIT_PER_MINUTE' '0'), [ref]$rateLimit) -or $rateLimit -le 0) {
    throw 'RATE_LIMIT_PER_MINUTE must be a positive integer before public access is enabled.'
}

Require-Secret $settings 'NGROK_AUTHTOKEN' 20
Require-Secret $settings 'JWT_SECRET' 32
Require-Secret $settings 'BACKEND_API_SHARED_KEY' 32
Require-Secret $settings 'HAI_MEMORY_ENCRYPTION_KEY' 32
Require-Secret $settings 'HAI_APPROVAL_PROOF_SIGNING_KEY' 32
Require-Secret $settings 'DB_PASSWORD' 32
Require-Secret $settings 'DB_RUNTIME_PASSWORD' 32

$publicUrlText = (Get-Setting $settings 'HAI_NGROK_URL').TrimEnd('/')
$publicUri = $null
if (-not [Uri]::TryCreate($publicUrlText, [UriKind]::Absolute, [ref]$publicUri) -or
    $publicUri.Scheme -ne 'https' -or
    -not [string]::IsNullOrEmpty($publicUri.UserInfo) -or
    $publicUri.AbsolutePath -ne '/' -or
    -not [string]::IsNullOrEmpty($publicUri.Query) -or
    -not [string]::IsNullOrEmpty($publicUri.Fragment) -or
    -not $publicUri.IsDefaultPort -or
    -not (Test-NgrokHostname $publicUri.DnsSafeHost) -or
    $publicUri.Host -eq 'your-reserved-domain.ngrok.app') {
    throw 'HAI_NGROK_URL must be a fixed ngrok HTTPS origin without credentials, port, path, query, or fragment.'
}

$publicA2A = (Get-Setting $settings 'HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED' 'false').ToLowerInvariant() -eq 'true'
if ($publicA2A) {
    if ((Get-Setting $settings 'HAI_A2A_BRIDGE_ENABLED' 'false').ToLowerInvariant() -ne 'true') {
        throw 'HAI_A2A_BRIDGE_ENABLED must be true when the public ngrok bridge is enabled.'
    }
    Require-Secret $settings 'HAI_A2A_BRIDGE_TOKEN' 32
    if ([string]::IsNullOrWhiteSpace((Get-Setting $settings 'HAI_A2A_BRIDGE_OWNER_ID'))) {
        throw 'HAI_A2A_BRIDGE_OWNER_ID is required when the public ngrok bridge is enabled.'
    }
    if ((Get-Setting $settings 'HAI_A2A_BRIDGE_URL').TrimEnd('/') -ne ($publicUrlText + '/api/v1/a2a')) {
        throw "HAI_A2A_BRIDGE_URL must equal $publicUrlText/api/v1/a2a when the public ngrok bridge is enabled."
    }
}

$callbackPaths = @{
    GOOGLE_LOGIN_REDIRECT_URL = '/api/v1/auth/google/callback'
    GOOGLE_OAUTH_REDIRECT_URL = '/api/v1/sources/oauth/google/callback'
}
foreach ($entry in $callbackPaths.GetEnumerator()) {
    $configured = Get-Setting $settings $entry.Key
    if ($configured -and $configured -ne ($publicUrlText + $entry.Value)) {
        throw "$($entry.Key) must equal $publicUrlText$($entry.Value) when configured."
    }
}

& docker compose --env-file $envPath --profile cloud-tunnel -f $composePath config --quiet
if ($LASTEXITCODE -ne 0) {
    throw 'Docker Compose validation failed.'
}

Write-Host "Cloud-tunnel preflight passed for $publicUrlText"
if ($ValidateOnly) {
    return
}

function Wait-ForHealthyContainer([string]$ContainerName, [int]$Attempts = 45) {
    for ($attempt = 0; $attempt -lt $Attempts; $attempt++) {
        $state = & docker inspect --format '{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{end}}' $ContainerName 2>$null
        if ($LASTEXITCODE -eq 0) {
            $parts = $state -split '\|', 2
            if ($parts[0] -eq 'running' -and $parts[1] -eq 'healthy') {
                return
            }
            if ($parts[0] -eq 'exited' -or $parts[1] -eq 'unhealthy') {
                & docker logs --tail 40 $ContainerName
                throw "$ContainerName entered state $state."
            }
        }
        Start-Sleep -Seconds 2
    }
    & docker logs --tail 40 $ContainerName
    throw "$ContainerName did not become healthy within $($Attempts * 2) seconds."
}

function Invoke-PublicRequest(
    [System.Net.Http.HttpClient]$Client,
    [System.Net.Http.HttpMethod]$Method,
    [string]$Url,
    [int]$ExpectedStatus,
    [int]$Attempts = 1
) {
    $lastStatus = 0
    $lastError = ''
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        $request = [System.Net.Http.HttpRequestMessage]::new($Method, $Url)
        try {
            # The header suppresses the free-tier HTML interstitial for this
            # machine probe. It is not an authentication or authorization
            # credential and is never forwarded as HAI identity.
            $request.Headers.TryAddWithoutValidation('ngrok-skip-browser-warning', 'hai-cloud-readiness') | Out-Null
            $response = $Client.SendAsync($request).GetAwaiter().GetResult()
            try {
                $lastStatus = [int]$response.StatusCode
                $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                if ($lastStatus -eq $ExpectedStatus) {
                    $selectedHeaders = @{}
                    foreach ($headerName in @(
                        'Strict-Transport-Security',
                        'Cache-Control',
                        'Content-Security-Policy',
                        'X-Content-Type-Options',
                        'X-Frame-Options'
                    )) {
                        $selectedHeaders[$headerName] = ''
                        if ($response.Headers.Contains($headerName)) {
                            $selectedHeaders[$headerName] = $response.Headers.GetValues($headerName) -join ','
                        } elseif ($response.Content.Headers.Contains($headerName)) {
                            $selectedHeaders[$headerName] = $response.Content.Headers.GetValues($headerName) -join ','
                        }
                    }
                    return [pscustomobject]@{
                        Status = $lastStatus
                        Body = $body
                        Headers = $selectedHeaders
                    }
                }
                $lastError = "HTTP $lastStatus"
            } finally {
                $response.Dispose()
            }
        } catch {
            $lastError = $_.Exception.Message
        } finally {
            $request.Dispose()
        }
        if ($attempt -lt $Attempts) {
            Start-Sleep -Seconds 2
        }
    }
    throw "$Url did not return HTTP $ExpectedStatus after $Attempts attempt(s); last result: $lastError."
}

function Read-BoundedResponseBody(
    [System.Net.Http.HttpResponseMessage]$Response,
    [int]$MaximumBytes = 8192
) {
    $stream = $Response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
    try {
        $buffer = [byte[]]::new($MaximumBytes)
        $total = 0
        while ($total -lt $MaximumBytes) {
            $read = $stream.Read($buffer, $total, $MaximumBytes - $total)
            if ($read -le 0) { break }
            $total += $read
        }
        return [Text.Encoding]::UTF8.GetString($buffer, 0, $total)
    } finally {
        $stream.Dispose()
    }
}

function Invoke-PublicOwnershipProbe(
    [System.Net.Http.HttpClient]$Client,
    [string]$Url
) {
    $request = [System.Net.Http.HttpRequestMessage]::new(
        [System.Net.Http.HttpMethod]::Get,
        $Url
    )
    try {
        $request.Headers.TryAddWithoutValidation(
            'ngrok-skip-browser-warning',
            'hai-endpoint-ownership'
        ) | Out-Null
        $response = $Client.SendAsync(
            $request,
            [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead
        ).GetAwaiter().GetResult()
        try {
            return [pscustomobject]@{
                Status = [int]$response.StatusCode
                Body = Read-BoundedResponseBody $response
            }
        } finally {
            $response.Dispose()
        }
    } finally {
        $request.Dispose()
    }
}

function Test-PublicEndpointOwnership(
    [string]$BaseUrl,
    [bool]$CurrentProjectTunnelRunning
) {
    $handler = [System.Net.Http.HttpClientHandler]::new()
    $handler.AllowAutoRedirect = $false
    $client = [System.Net.Http.HttpClient]::new($handler)
    $client.Timeout = [TimeSpan]::FromSeconds(5)
    try {
        try {
            $health = Invoke-PublicOwnershipProbe $client "$BaseUrl/healthz"
        } catch {
            Write-Host 'The fixed endpoint is not currently reachable; startup acceptance will verify ownership.'
            return
        }

        if ($health.Status -eq 200) {
            $payload = $null
            try { $payload = $health.Body | ConvertFrom-Json } catch { }
            $isHAI = $null -ne $payload -and
                $payload.service -eq 'backend' -and
                $payload.status -eq 'ok'
            if ($isHAI -and $CurrentProjectTunnelRunning) {
                Write-Host 'The fixed endpoint already serves this running HAI tunnel; startup will reconcile and re-verify it.'
                return
            }
            if ($isHAI) {
                throw 'The fixed endpoint already serves a different HAI tunnel. Stop its owning agent during an approved maintenance window or reserve a separate HAI domain.'
            }
            throw 'The fixed endpoint is already serving a non-HAI application. HAI will not attempt to take over another application endpoint.'
        }

        if ($health.Body -match 'ERR_NGROK_[0-9]+') {
            return
        }

        try {
            $root = Invoke-PublicOwnershipProbe $client "$BaseUrl/"
        } catch {
            return
        }
        if ($root.Body -match 'ERR_NGROK_[0-9]+') {
            return
        }
        if ($root.Status -ge 200 -and $root.Status -lt 500) {
            throw "The fixed endpoint already responds with HTTP $($root.Status) but does not identify HAI. HAI will not attempt to take over another application endpoint."
        }
    } finally {
        $client.Dispose()
        $handler.Dispose()
    }
}

function Test-PublicOrigin([string]$BaseUrl, [bool]$PublicA2A) {
    $handler = [System.Net.Http.HttpClientHandler]::new()
    $handler.AllowAutoRedirect = $false
    $client = [System.Net.Http.HttpClient]::new($handler)
    $client.Timeout = [TimeSpan]::FromSeconds(10)
    try {
        $health = Invoke-PublicRequest $client ([System.Net.Http.HttpMethod]::Get) "$BaseUrl/healthz" 200 30
        $healthPayload = $health.Body | ConvertFrom-Json
        if ($healthPayload.status -ne 'ok' -or $healthPayload.service -ne 'backend') {
            throw 'The public health response did not identify a healthy HAI backend.'
        }
        if ($health.Headers['Strict-Transport-Security'] -notmatch 'max-age=31536000') {
            throw 'The public origin did not return the required HSTS policy.'
        }

        $readiness = Invoke-PublicRequest $client ([System.Net.Http.HttpMethod]::Get) "$BaseUrl/readyz" 200
        $readinessPayload = $readiness.Body | ConvertFrom-Json
        if ($readinessPayload.service -ne 'backend' -or
            $readinessPayload.status -notin @('ready', 'degraded')) {
            throw 'The public readiness response did not identify a serving HAI backend.'
        }
        if ($null -ne $readinessPayload.PSObject.Properties['checks']) {
            throw 'The public readiness response exposed internal dependency checks.'
        }
        if ($readiness.Headers['Cache-Control'] -notmatch 'no-store') {
            throw 'The public readiness response was not marked no-store.'
        }

        $session = Invoke-PublicRequest $client ([System.Net.Http.HttpMethod]::Get) "$BaseUrl/api/v1/auth/session" 200
        $sessionPayload = $session.Body | ConvertFrom-Json
        if ($sessionPayload.authenticated -ne $false -or $sessionPayload.permissions.canRead -ne $false) {
            throw 'The unauthenticated public session check returned an unsafe identity state.'
        }
        if ($session.Headers['Cache-Control'] -notmatch 'no-store') {
            throw 'The anonymous session response was not marked no-store.'
        }

        $page = Invoke-PublicRequest $client ([System.Net.Http.HttpMethod]::Get) "$BaseUrl/" 200
        if ($page.Body -notmatch '<app-root') {
            throw 'The public origin did not return the HAI frontend shell.'
        }
        if ($page.Headers['X-Content-Type-Options'] -notmatch 'nosniff' -or
            $page.Headers['X-Frame-Options'] -notmatch 'DENY' -or
            $page.Headers['Content-Security-Policy'] -notmatch "frame-ancestors 'none'") {
            throw 'The public frontend shell did not return the required browser security headers.'
        }

        if (-not $PublicA2A) {
            Invoke-PublicRequest $client ([System.Net.Http.HttpMethod]::Get) "$BaseUrl/.well-known/agent-card.json" 404 | Out-Null
            Invoke-PublicRequest $client ([System.Net.Http.HttpMethod]::Post) "$BaseUrl/api/v1/a2a" 404 | Out-Null
        }
    } finally {
        $client.Dispose()
        $handler.Dispose()
    }
}

$ngrokContainerState = & docker inspect --format '{{.State.Status}}' '018-hai-ngrok' 2>$null
$currentProjectTunnelRunning = $LASTEXITCODE -eq 0 -and $ngrokContainerState -eq 'running'
Test-PublicEndpointOwnership $publicUrlText $currentProjectTunnelRunning

# Reconcile the security-sensitive base services before creating any public
# endpoint. This applies secure-cookie and OAuth callback changes to the actual
# running IDP rather than trusting only the env file.
& docker compose --env-file $envPath --profile cloud-tunnel -f $composePath up -d --no-build idp backend frontend nginx
if ($LASTEXITCODE -ne 0) {
    throw 'Failed to reconcile the secured HAI gateway services. Build the base stack first.'
}
foreach ($container in @('018-hai-idp', '018-hai-backend', '018-hai-frontend', 'gateway')) {
    Wait-ForHealthyContainer $container
}

$gatewayPort = Get-Setting $settings 'GATEWAY_HOST_PORT' '8088'
try {
    $ready = Invoke-WebRequest -Uri "http://127.0.0.1:$gatewayPort/readyz" -UseBasicParsing -TimeoutSec 5
    if ($ready.StatusCode -ne 200) {
        throw "unexpected HTTP $($ready.StatusCode)"
    }
    $readyPayload = $ready.Content | ConvertFrom-Json
    if ($readyPayload.service -ne 'backend' -or
        $readyPayload.status -notin @('ready', 'degraded')) {
        throw 'the local readiness response did not identify a serving HAI backend'
    }
    if ($null -ne $readyPayload.PSObject.Properties['checks']) {
        throw 'the local readiness response exposed internal dependency checks'
    }
    if ($ready.Headers['Cache-Control'] -notmatch 'no-store') {
        throw 'the local readiness response was not marked no-store'
    }
} catch {
    throw "The reconciled local HAI gateway is not ready on port $gatewayPort. $($_.Exception.Message)"
}

& docker compose --env-file $envPath --profile cloud-tunnel -f $composePath up -d --no-build ngrok
if ($LASTEXITCODE -ne 0) {
    throw 'Failed to start the ngrok service.'
}
Wait-ForHealthyContainer '018-hai-ngrok' 30
try {
    Test-PublicOrigin $publicUrlText $publicA2A
    if ($publicA2A) {
        & (Join-Path $repoRoot 'scripts/smoke-a2a-bridge.ps1') -EnvFile $envPath -Public
    }
} catch {
    & docker compose --env-file $envPath --profile cloud-tunnel -f $composePath stop ngrok | Out-Null
    throw "The public HAI acceptance probe failed and the tunnel was stopped. $($_.Exception.Message)"
}
Write-Host "HAI is available through $publicUrlText"
