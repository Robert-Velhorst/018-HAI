$ErrorActionPreference = 'Stop'
$sourcePath = Join-Path $PSScriptRoot 'test-hai-supplemental-transcript-cleanup-readiness.ps1'
$source = [IO.File]::ReadAllText($sourcePath)
$required = @(
    'Exactly the two inventoried supplemental transcript roots are required.',
    'A supplemental transcript root or ancestor is a reparse point.',
    'supplemental-child-transcript-manifest.csv',
    'supplemental-child-final-reports.md',
    'Get-ChildItem -LiteralPath $rootPath -Force -Recurse',
    'Get-FileHash -LiteralPath $sourcePath -Algorithm SHA256',
    'Supplemental transcript size or exact archive inventory differs from the manifest.',
    'Supplemental completed reports contain no repository source links to verify.',
    'Git could not enumerate tracked source files for report verification.',
    'Supplemental transcript evidence has uncommitted worktree changes.',
    "result = 'supplemental_source_archive_verified'",
    'deletion_performed = $false',
    'cleanup_authorized = $false'
)
foreach ($token in $required) {
    if (-not $source.Contains($token)) { throw "Supplemental transcript verification is missing contract token: $token" }
}
if ($source -match '(?im)^\s*(?:Remove-Item|Move-Item|docker\s+(?:image|volume|container)\s+rm|git\s+clean)') {
    throw 'Supplemental transcript readiness must remain read-only.'
}
$tokens = $null
$errors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile($sourcePath, [ref]$tokens, [ref]$errors)
if ($errors.Count -gt 0) { throw "Supplemental transcript verifier has PowerShell syntax errors: $($errors[0].Message)" }
Write-Output 'HAI supplemental transcript cleanup contract: PASS'
