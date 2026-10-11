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
$repoRoot = Split-Path -Parent $PSScriptRoot

function Resolve-RepoFile([string]$Path) {
    if ([System.IO.Path]::IsPathRooted($Path)) { return $Path }
    return Join-Path $repoRoot $Path
}

function Read-DotEnv([string]$Path) {
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $Path) {
        $trimmed = $line.Trim()
        if (!$trimmed -or $trimmed.StartsWith('#') -or !$trimmed.Contains('=')) { continue }
        $key, $value = $trimmed.Split('=', 2)
        $values[$key.Trim()] = $value.Trim().Trim('"').Trim("'")
    }
    return $values
}

function Require-Setting($Values, [string]$Name, [int]$MinimumLength = 1) {
    $value = [string]$Values[$Name]
    if ($value.Length -lt $MinimumLength -or $value -match '(?i)change-this|changeme|example|placeholder') {
        throw "$Name must be configured with a non-placeholder value."
    }
    return $value
}

function Require-Value($Values, [string]$Name, [string]$Expected) {
    if ([string]$Values[$Name] -ne $Expected) { throw "$Name must be '$Expected' before public access can start." }
}

function Require-PositiveInteger($Values, [string]$Name) {
    $raw = ([string]$Values[$Name]).Trim()
    $parsed = 0
    if (![int]::TryParse($raw, [ref]$parsed) -or $parsed -lt 1) {
        throw "$Name must be a positive integer before public access can start."
    }
    return $parsed
}

function Assert-HaiComposeOwnership([string]$ProjectName) {
    if ($ProjectName -notmatch '^[a-z0-9][a-z0-9_-]*$') {
        throw 'COMPOSE_PROJECT_NAME must contain only lowercase letters, numbers, hyphens, or underscores before public access can start.'
    }

    $ids = @(docker ps -aq --filter "label=com.docker.compose.project=$ProjectName")
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to inspect existing Docker Compose containers for '$ProjectName'. Refusing to manage cloud access until ownership can be verified."
    }
    if (!$ids.Count) { return }
    # Query labels only. Passing a template with nested quoted keys breaks in
    # Windows PowerShell's legacy native-command argument mode, and full
    # container inspection could expose environment values unnecessarily.
    $inspection = @(docker inspect @ids --format '{{json .Config.Labels}}' 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to inspect existing Docker Compose container ownership for '$ProjectName'. Refusing to manage cloud access until ownership can be verified."
    }
    $workingDirs = @()
    foreach ($labelJson in $inspection) {
        try {
            $labels = $labelJson | ConvertFrom-Json -ErrorAction Stop
        } catch {
            throw "Unable to parse existing Docker Compose ownership labels for '$ProjectName'. Refusing to manage cloud access until ownership can be verified."
        }
        $workingDirectory = [string]$labels.'com.docker.compose.project.working_dir'
        if (![string]::IsNullOrWhiteSpace($workingDirectory)) {
            $workingDirs += $workingDirectory
        }
    }
    $workingDirs = @($workingDirs | Select-Object -Unique)
    foreach ($workingDir in $workingDirs) {
        if (([System.IO.Path]::GetFullPath($workingDir)).TrimEnd('\\') -ne $repoRoot.TrimEnd('\\')) {
            throw "Refusing to manage Docker project '$ProjectName' owned by $workingDir. Use that checkout instead."
        }
    }
}

$envPath = Resolve-RepoFile $EnvFile
$composePath = Resolve-RepoFile $ComposeFile
if (!(Test-Path -LiteralPath $envPath)) { throw "Environment file not found: $envPath" }
if (!(Test-Path -LiteralPath $composePath)) { throw "Compose file not found: $composePath" }

$settings = Read-DotEnv $envPath
$composeProjectName = ([string]$settings['COMPOSE_PROJECT_NAME']).Trim()
Assert-HaiComposeOwnership -ProjectName $composeProjectName
if ($Stop) {
    docker compose --env-file $envPath --profile cloud-tunnel -f $composePath stop ngrok
    exit $LASTEXITCODE
}

Require-Value $settings 'RUN_MODE' 'production'
Require-Value $settings 'LOCAL_LOGIN_BYPASS_ENABLED' 'false'
Require-Value $settings 'IDP_COOKIE_SECURE' 'true'
Require-Value $settings 'GATEWAY_HOST_BIND' '127.0.0.1'
Require-Value $settings 'HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED' 'false'
Require-PositiveInteger $settings 'RATE_LIMIT_PER_MINUTE' | Out-Null
$ngrokUrl = Require-Setting $settings 'HAI_NGROK_URL'
Require-Setting $settings 'NGROK_AUTHTOKEN' 20 | Out-Null
foreach ($secret in 'JWT_SECRET', 'BACKEND_API_SHARED_KEY', 'HAI_MEMORY_ENCRYPTION_KEY', 'HAI_APPROVAL_PROOF_SIGNING_KEY', 'DB_PASSWORD') {
    Require-Setting $settings $secret 32 | Out-Null
}
Require-Setting $settings 'FIRST_RUN_ADMIN_PASSWORD' 12 | Out-Null
if ($ngrokUrl -notmatch '^https://[^/@?#]+\.(ngrok\.app|ngrok\.dev|ngrok-free\.app|ngrok-free\.dev)$') {
    throw 'HAI_NGROK_URL must be a reserved HTTPS ngrok origin without path, query, fragment, or credentials.'
}

# Google sends the browser to its registered callback after consent. A localhost
# callback would strand a remote user behind this public tunnel, so reject a
# stale local callback instead of starting a partly functional cloud session.
$expectedGoogleLoginRedirect = "$ngrokUrl/api/v1/auth/google/callback"
$expectedGoogleSourceRedirect = "$ngrokUrl/api/v1/sources/oauth/google/callback"
foreach ($redirect in @(
    @{ Name = 'GOOGLE_LOGIN_REDIRECT_URL'; Expected = $expectedGoogleLoginRedirect },
    @{ Name = 'GOOGLE_OAUTH_REDIRECT_URL'; Expected = $expectedGoogleSourceRedirect }
)) {
    $configured = ([string]$settings[$redirect.Name]).Trim()
    if ($configured -and $configured -ne $redirect.Expected) {
        throw "$($redirect.Name) must be '$($redirect.Expected)' while public ngrok access is enabled, or be empty to disable that optional Google flow."
    }
}

docker compose --env-file $envPath --profile cloud-tunnel -f $composePath config --quiet
if ($LASTEXITCODE -ne 0 -or $ValidateOnly) { exit $LASTEXITCODE }
docker compose --env-file $envPath --profile cloud-tunnel -f $composePath up -d ngrok
exit $LASTEXITCODE
