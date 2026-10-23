[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$launcherPath = Join-Path $PSScriptRoot "test-live-trello.ps1"
$liveTestPath = Join-Path $repositoryRoot "backend\internal\source\trello_live_test.go"

foreach ($path in @($launcherPath, $liveTestPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Live Trello acceptance source is missing: $path"
    }
}

$tokens = $null
$errors = $null
[Management.Automation.Language.Parser]::ParseFile($launcherPath, [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count -gt 0) {
    throw "PowerShell syntax error in ${launcherPath}: $($errors[0].Message)"
}

$launcher = [IO.File]::ReadAllText($launcherPath)
foreach ($required in @(
    'TRELLO_API_KEY',
    'TRELLO_READ_TOKEN',
    'TRELLO_ACCOUNT_OWNER_IDENTITY',
    'TRELLO_ACCOUNT_MEMBER_ID',
    'TRELLO_LIVE_BOARD',
    ':/workspace:ro',
    'finally',
    'TestLiveTrelloSyncAgainstRealBoard',
    'TestLiveTrelloIncrementalSyncSkipsUnchanged',
    'TestLiveTrelloTokenIsReadOnly',
    'Raw provider/test output was suppressed'
)) {
    if (-not $launcher.Contains($required)) {
        throw "Live Trello launcher is missing required safety/acceptance contract: $required"
    }
}
if ($launcher -match 'Write-Host\s+\$_|--privileged|--network\s+host') {
    throw "Live Trello launcher must not forward raw test output or use privileged/host-network Docker mode."
}

$liveTest = [IO.File]::ReadAllText($liveTestPath)
foreach ($required in @(
    '//go:build live',
    'func TestLiveTrelloSyncAgainstRealBoard',
    'func TestLiveTrelloIncrementalSyncSkipsUnchanged',
    'func TestLiveTrelloTokenIsReadOnly',
    'if p.Write',
    'trelloGetJSON'
)) {
    if (-not $liveTest.Contains($required)) {
        throw "Live Trello Go test is missing required acceptance contract: $required"
    }
}

$missingEnv = Join-Path $env:TEMP "hai-live-trello-contract-missing-env"
try {
    & $launcherPath -EnvFile $missingEnv
    throw "Live Trello launcher unexpectedly continued without its environment file."
} catch {
    if ($_.Exception.Message -notmatch "Environment file not found") {
        throw
    }
}

Write-Host "Live Trello launcher contract passed without reading credentials or contacting Docker/Trello."
