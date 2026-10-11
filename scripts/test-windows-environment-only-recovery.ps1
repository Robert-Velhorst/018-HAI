[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$restorePath = Join-Path $PSScriptRoot 'test-restore-windows.ps1'
$startPath = Join-Path $repositoryRoot 'installer\windows\Start-HAI.ps1'
$installerDocsPath = Join-Path $repositoryRoot 'docs\windows-installer.md'
$documentationPath = Join-Path $repositoryRoot 'docs\backup-restore.md'
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($restorePath, [ref]$tokens, [ref]$parseErrors)

if ($parseErrors.Count -gt 0) { throw "Restore script has PowerShell parse errors: $($parseErrors[0].Message)" }
$source = [IO.File]::ReadAllText($restorePath)
$startSource = [IO.File]::ReadAllText($startPath)
$installerDocumentation = [IO.File]::ReadAllText($installerDocsPath)
$documentation = [IO.File]::ReadAllText($documentationPath)
$switch = $ast.ParamBlock.Parameters | Where-Object { $_.Name.VariablePath.UserPath -ceq 'RestoreEnvironmentOnly' }
if ($null -eq $switch -or $switch.StaticType -ne [switch]) { throw 'Restore script must expose RestoreEnvironmentOnly as a switch.' }

$branch = @($ast.FindAll({
    param($node)
    $node -is [Management.Automation.Language.IfStatementAst] -and
        $node.Clauses.Count -gt 0 -and $node.Clauses[0].Item1.Extent.Text.Trim() -ceq '$RestoreEnvironmentOnly'
}, $true) | Select-Object -Last 1)
if ($branch.Count -ne 1) { throw 'Environment-only recovery branch is missing or ambiguous.' }
$branchText = $branch[0].Clauses[0].Item2.Extent.Text
foreach ($required in @(
    'Restore-HaiProtectedEnvironmentFile',
    '%LOCALAPPDATA%\HAI\hai.env',
    'refusing to overwrite',
    'No Docker commands ran'
)) {
    if ($branchText -notmatch [Regex]::Escape($required)) { throw "Environment-only recovery is missing required guard: $required" }
}
$dockerCommands = @($branch[0].Clauses[0].Item2.FindAll({
    param($node)
    $node -is [Management.Automation.Language.CommandAst] -and $node.GetCommandName() -ieq 'docker'
}, $true))
if ($dockerCommands.Count -gt 0) { throw 'Environment-only recovery must not call Docker.' }

$firstDockerUse = $source.IndexOf("if (-not (Get-Command docker", [StringComparison]::Ordinal)
if ($firstDockerUse -lt 0 -or $branch[0].Extent.StartOffset -ge $firstDockerUse) {
    throw 'Environment-only recovery must return before the full Docker restore path.'
}
if ($branchText -notmatch '\breturn\b') { throw 'Environment-only recovery must exit before entering the full restore drill.' }
$manifestValidation = $source.IndexOf('Assert-HaiRecoveryManifest $manifest $bundle', [StringComparison]::Ordinal)
if ($manifestValidation -lt 0 -or $manifestValidation -ge $branch[0].Extent.StartOffset) {
    throw 'The selected backup must be fully validated before environment-only recovery.'
}
if ($documentation -notmatch '(?s)test-restore-windows\.ps1.*?-RestoreEnvironmentOnly') {
    throw 'Backup/restore documentation must include the environment-only recovery command.'
}
if ($startSource -notmatch 'read-only check for existing data volumes' -or
    $startSource -match 'retry so HAI can verify existing data before any initialization') {
    throw 'Missing-environment startup guidance must state that Docker retry only permits an inventory check.'
}
if ($installerDocumentation -notmatch '(?s)hai\.env.*?read-only inventory.*?environment-only recovery procedure') {
    throw 'Windows installer documentation must link missing-environment recovery to the protected backup procedure.'
}

Write-Output 'Windows environment-only recovery contract passed (PowerShell syntax, validated backup, protected target, no overwrite, and pre-Docker exit).'
