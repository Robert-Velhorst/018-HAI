$ErrorActionPreference = 'Stop'
$source = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-transcript-cleanup-readiness.ps1'))
$required = @(
    'RequireCommittedLedger',
    'RequireMergedPullRequest',
    "branch -cne 'main'",
    'remote get-url origin',
    'diff --quiet HEAD --',
    'diff --cached --quiet HEAD --',
    'gh.Source pr view',
    'gh.Source pr checks',
    '--json name,state',
    "state -cne 'SUCCESS'",
    'merge-base --is-ancestor',
    '$cleanupGateReady = $sourceVerified -and $committedLedgerVerified -and $mergedPullRequestVerified',
    'cleanup_authorized = $false'
)
foreach ($token in $required) {
    if (-not $source.Contains($token)) { throw "Transcript cleanup readiness is missing required gate: $token" }
}
if ($source -match 'Remove-Item|Move-Item|git\s+clean|docker\s+volume\s+rm') {
    throw 'Transcript cleanup readiness must remain read-only.'
}
Write-Output 'HAI transcript cleanup gate contract: PASS'
