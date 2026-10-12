[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$auditScript = Join-Path $PSScriptRoot "no-fake-claims-audit.ps1"

if (-not (Test-Path -LiteralPath $auditScript -PathType Leaf)) {
    throw "Windows no-fake-claims audit is missing: $auditScript"
}

. $auditScript -Library

$resolvedBase = Resolve-Phase2AuditMergeBase -BaseRef "origin/main"
if ($resolvedBase -notmatch "^[0-9a-fA-F]{40}$") {
    throw "Audit base resolver did not return a full merge-base commit SHA."
}

$failedClosed = $false
try {
    Resolve-Phase2AuditMergeBase -BaseRef "refs/heads/this-ref-must-not-exist"
} catch {
    $failedClosed = $_.Exception.Message -match "Unable to resolve audit base ref"
}
if (-not $failedClosed) {
    throw "Audit base resolver must fail closed for a missing comparison ref."
}

foreach ($path in @(
    "idp/internal/app/repositories/reset_token_security_test.go",
    "backend/internal/example/tokenizer.go",
    "docs/credential-model.md"
)) {
    if (Test-Phase2SensitiveAddedFile $path) {
        throw "Audit must not classify ordinary source or documentation as a sensitive artifact: $path"
    }
}

foreach ($path in @(
    ".env.local",
    "runtime/credentials.json",
    "secrets/provider.yaml",
    "state/access-token.json",
    "models/local.gguf"
)) {
    if (-not (Test-Phase2SensitiveAddedFile $path)) {
        throw "Audit must classify sensitive artifact paths as blocked: $path"
    }
}

$tokens = $null
$errors = $null
[Management.Automation.Language.Parser]::ParseFile($auditScript, [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count -gt 0) {
    throw "PowerShell syntax error in ${auditScript}: $($errors[0].Message)"
}

Push-Location $repositoryRoot
try {
    & $auditScript
    if ($LASTEXITCODE -ne 0) {
        throw "Windows no-fake-claims audit failed with exit code $LASTEXITCODE."
    }
} finally {
    Pop-Location
}

Write-Host "Windows no-fake-claims audit contract passed."
