[CmdletBinding()]
param(
    [string]$SourceEnvFile = ".env.local",
    [string]$OutputEnvFile = ".env.ngrok.local",
    [Parameter(Mandatory = $true)][string]$PublicUrl,
    [ValidateRange(1, 10000)][int]$RateLimitPerMinute = 60,
    [switch]$PublishA2A,
    [switch]$Force
)

$ErrorActionPreference = "Stop"
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path

function Resolve-RepoPath([string]$Path) {
    if ([IO.Path]::IsPathRooted($Path)) { return [IO.Path]::GetFullPath($Path) }
    return [IO.Path]::GetFullPath((Join-Path $root $Path))
}

function Set-DotEnvValue([string]$Content, [string]$Name, [string]$Value) {
    $pattern = "(?m)^" + [Regex]::Escape($Name) + "=.*$"
    if (-not [Regex]::IsMatch($Content, $pattern)) { throw "The source environment does not define $Name." }
    return [Regex]::Replace($Content, $pattern, "$Name=$Value", 1)
}

function Get-DotEnvValue([string]$Content, [string]$Name) {
    $match = [Regex]::Match($Content, "(?m)^" + [Regex]::Escape($Name) + "=(.*)$")
    if (-not $match.Success) { return "" }
    return $match.Groups[1].Value.Trim().Trim("'", '"')
}

function Convert-SecureValue([Security.SecureString]$Value) {
    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($Value)
    try { return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer) }
}

$sourcePath = Resolve-RepoPath $SourceEnvFile
$outputPath = Resolve-RepoPath $OutputEnvFile
if (-not (Test-Path -LiteralPath $sourcePath -PathType Leaf)) { throw "Source environment not found: $sourcePath" }
if ((Test-Path -LiteralPath $outputPath) -and -not $Force) { throw "$outputPath already exists. Use -Force only for an intentional replacement." }

$uri = $null
if (-not [Uri]::TryCreate($PublicUrl.TrimEnd('/'), [UriKind]::Absolute, [ref]$uri) -or $uri.Scheme -ne 'https') {
    throw "PublicUrl must be an absolute HTTPS ngrok origin."
}
$origin = $uri.GetLeftPart([UriPartial]::Authority)
if ($origin -ne $PublicUrl.TrimEnd('/')) { throw "PublicUrl must not contain credentials, a path, query, or fragment." }

$token = [Environment]::GetEnvironmentVariable("NGROK_AUTHTOKEN")
if ([string]::IsNullOrWhiteSpace($token)) {
    $token = Convert-SecureValue (Read-Host "Dedicated ngrok authtoken" -AsSecureString)
}
if ($token.Length -lt 20) { throw "A dedicated ngrok authtoken of at least 20 characters is required." }

$content = [IO.File]::ReadAllText($sourcePath)
foreach ($setting in ([ordered]@{
    RUN_MODE = "production"
    LOCAL_LOGIN_BYPASS_ENABLED = "false"
    IDP_COOKIE_SECURE = "true"
    GATEWAY_HOST_BIND = "127.0.0.1"
    RATE_LIMIT_PER_MINUTE = $RateLimitPerMinute.ToString()
    NGROK_AUTHTOKEN = $token
    HAI_NGROK_URL = $origin
    GOOGLE_LOGIN_REDIRECT_URL = "$origin/api/v1/auth/google/callback"
    GOOGLE_OAUTH_REDIRECT_URL = "$origin/api/v1/sources/oauth/google/callback"
    HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED = $(if ($PublishA2A) { "true" } else { "false" })
}).GetEnumerator()) {
    $content = Set-DotEnvValue $content $setting.Key ([string]$setting.Value)
}

if ($PublishA2A) {
    if ((Get-DotEnvValue $content "HAI_A2A_BRIDGE_ENABLED").ToLowerInvariant() -ne "true") {
        throw "The source profile must already enable the bounded A2A bridge before it can be published."
    }
    if ([string]::IsNullOrWhiteSpace((Get-DotEnvValue $content "HAI_A2A_BRIDGE_OWNER_ID"))) {
        throw "The source profile must bind the A2A bridge to one owner."
    }
    if ((Get-DotEnvValue $content "HAI_A2A_BRIDGE_TOKEN").Length -lt 32) {
        throw "The source profile must contain a dedicated A2A token of at least 32 characters."
    }
    $content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_URL" "$origin/api/v1/a2a"
}

$parent = Split-Path -Parent $outputPath
if (-not (Test-Path -LiteralPath $parent -PathType Container)) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }
[IO.File]::WriteAllText($outputPath, $content, [Text.UTF8Encoding]::new($false))
try {
    & (Join-Path $PSScriptRoot "start-ngrok.ps1") -ValidateOnly -EnvFile $outputPath
    if ($LASTEXITCODE -ne 0) { throw "Generated cloud profile failed ngrok preflight." }
} catch {
    Remove-Item -LiteralPath $outputPath -Force -ErrorAction SilentlyContinue
    throw
}

Write-Host "Created validated cloud profile: $outputPath"
Write-Host "The ngrok token was written to the ignored profile and was not printed."
