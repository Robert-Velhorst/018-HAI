[CmdletBinding()]
param(
    [string]$EnvFile = "",
    [string]$AdminEmail = "",
    [string]$AdminPasswordPlainText = "",
    [ValidateRange(1, 65535)]
    [int]$GatewayPort = 80,
    [switch]$Force
)

$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$examplePath = Join-Path $repositoryRoot ".env.example"
if (-not (Test-Path -LiteralPath $examplePath -PathType Leaf)) {
    throw "Missing environment template: $examplePath"
}

if ([string]::IsNullOrWhiteSpace($EnvFile)) {
    $EnvFile = Join-Path $repositoryRoot ".env.local"
} elseif (-not [System.IO.Path]::IsPathRooted($EnvFile)) {
    $EnvFile = Join-Path $repositoryRoot $EnvFile
}
$EnvFile = [System.IO.Path]::GetFullPath($EnvFile)

if ((Test-Path -LiteralPath $EnvFile) -and -not $Force) {
    throw "$EnvFile already exists. Re-run with -Force only when replacing it is intentional."
}

if ([string]::IsNullOrWhiteSpace($AdminEmail)) {
    $AdminEmail = Read-Host "First-run owner email"
}
$AdminEmail = $AdminEmail.Trim()
if ($AdminEmail -notmatch '^[^\s@]+@[^\s@]+\.[^\s@]+$') {
    throw "AdminEmail must be a valid email address."
}

if ([string]::IsNullOrEmpty($AdminPasswordPlainText)) {
    $securePassword = Read-Host "First-run owner password (at least 12 characters)" -AsSecureString
    $passwordPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($securePassword)
    try {
        $AdminPasswordPlainText = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($passwordPointer)
    } finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($passwordPointer)
    }
}
if ($AdminPasswordPlainText.Length -lt 12) {
    throw "The first-run owner password must contain at least 12 characters."
}
if ($AdminPasswordPlainText -match "[\r\n']") {
    throw "The first-run owner password cannot contain a line break or single quote."
}

function New-HaiSecret {
    $bytes = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    return ([BitConverter]::ToString($bytes)).Replace("-", "").ToLowerInvariant()
}

function Set-DotEnvValue {
    param(
        [Parameter(Mandatory = $true)][string]$Content,
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Value
    )
    $pattern = "(?m)^" + [Regex]::Escape($Name) + "=.*$"
    if (-not [Regex]::IsMatch($Content, $pattern)) {
        throw "The environment template does not define $Name."
    }
    $replacement = [Text.RegularExpressions.MatchEvaluator]{
        param($match)
        return "$Name=$Value"
    }
    return [Regex]::Replace($Content, $pattern, $replacement)
}

$content = [IO.File]::ReadAllText($examplePath)
$content = Set-DotEnvValue $content "BACKEND_API_SHARED_KEY" (New-HaiSecret)
$content = Set-DotEnvValue $content "HAI_MEMORY_ENCRYPTION_KEY" (New-HaiSecret)
$content = Set-DotEnvValue $content "JWT_SECRET" (New-HaiSecret)
$content = Set-DotEnvValue $content "HAI_APPROVAL_PROOF_SIGNING_KEY" (New-HaiSecret)
$content = Set-DotEnvValue $content "DB_PASSWORD" (New-HaiSecret)
$content = Set-DotEnvValue $content "DB_RUNTIME_PASSWORD" (New-HaiSecret)
$content = Set-DotEnvValue $content "FIRST_RUN_ADMIN_EMAIL" $AdminEmail
$content = Set-DotEnvValue $content "FIRST_RUN_ADMIN_PASSWORD" ("'" + $AdminPasswordPlainText + "'")
$a2aBridgeUrl = if ($GatewayPort -eq 80) {
    "http://127.0.0.1/api/v1/a2a"
} else {
    "http://127.0.0.1:$GatewayPort/api/v1/a2a"
}
$content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_ENABLED" "true"
$content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_OWNER_ID" $AdminEmail
$content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_TOKEN" (New-HaiSecret)
$content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_URL" $a2aBridgeUrl
$content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED" "false"
$content = Set-DotEnvValue $content "GATEWAY_HOST_PORT" $GatewayPort.ToString()
$content = Set-DotEnvValue $content "GATEWAY_HOST_BIND" "127.0.0.1"
$content = Set-DotEnvValue $content "LOCAL_LOGIN_BYPASS_ENABLED" "false"
$content = Set-DotEnvValue $content "RUN_MODE" "production"

$parent = Split-Path -Parent $EnvFile
if (-not (Test-Path -LiteralPath $parent -PathType Container)) {
    New-Item -ItemType Directory -Path $parent -Force | Out-Null
}
$utf8WithoutBom = New-Object Text.UTF8Encoding($false)
[IO.File]::WriteAllText($EnvFile, $content, $utf8WithoutBom)

Write-Host "Created $EnvFile"
Write-Host "Owner: $AdminEmail"
Write-Host "Gateway: http://127.0.0.1:$GatewayPort"
Write-Host "Local planning connector: $a2aBridgeUrl"
Write-Host "Secrets and the owner password were written to the ignored environment file and were not printed."
