[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile((Join-Path $root 'installer/windows/Hai-InstallerSupport.ps1'), [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw 'Installer support did not parse.' }
$nodes = @($ast.FindAll({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq 'Stop-HaiComposeRuntimeForUninstall' }, $true))
if ($nodes.Count -ne 1) { throw 'Expected one uninstall stop function.' }
. ([scriptblock]::Create($nodes[0].Extent.Text))

function Test-Path { param([string]$LiteralPath, [string]$PathType) return $true }
function Get-Item { param([string]$LiteralPath, [switch]$Force, $ErrorAction) return [pscustomobject]@{ Attributes = [IO.FileAttributes]::Normal } }
function Assert-HaiDockerReady { }
function Get-HaiComposeArguments { return @('compose', '--project-name', 'synthetic-uninstall', '--env-file', 'synthetic.env', '-f', 'synthetic.yml') }
function Assert-HaiSingleInstallation { if ($script:scenario -ceq 'ownership-failure') { throw 'Synthetic ownership refusal.' } }
function docker {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$DockerArguments)
    $script:calls.Add(($DockerArguments -join ' ')) | Out-Null
    if ($DockerArguments.Count -lt 10 -or ($DockerArguments[0..8] -join ' ') -cne
        'compose --project-name synthetic-uninstall --env-file synthetic.env -f synthetic.yml --profile *') {
        throw 'Unexpected synthetic Compose scope.'
    }
    $global:LASTEXITCODE = 0
    $operation = $DockerArguments[9..($DockerArguments.Count - 1)] -join ' '
    if ($DockerArguments -contains 'config') {
        if ($operation -cne 'config --quiet') { throw 'Unexpected config arguments.' }
        if ($script:scenario -ceq 'config-failure') { $global:LASTEXITCODE = 1 }
    } elseif ($DockerArguments -contains 'stop') {
        if ($DockerArguments -contains '--timeout' -or $DockerArguments -contains '-t') { throw 'Uninstall overrides declared service stop grace.' }
        if ($operation -cne 'stop') { throw 'Unexpected stop arguments.' }
        if ($script:scenario -ceq 'stop-failure') { $global:LASTEXITCODE = 1 }
    } elseif ($DockerArguments -contains 'ps') {
        if ($operation -cne 'ps --status running --quiet') { throw 'Unexpected inspection arguments.' }
        if ($script:scenario -ceq 'query-failure') { $global:LASTEXITCODE = 1 }
        if ($script:scenario -ceq 'still-running') { Write-Output 'synthetic-container' }
    } else { throw 'Unexpected Docker operation in uninstall contract.' }
}

$originalProfile = $env:LOCALAPPDATA
$failures = New-Object 'System.Collections.Generic.List[string]'
try {
    $env:LOCALAPPDATA = [IO.Path]::GetTempPath()
    foreach ($script:scenario in @('success', 'ownership-failure', 'config-failure', 'stop-failure', 'query-failure', 'still-running')) {
        $script:calls = New-Object 'System.Collections.Generic.List[string]'
        $failed = $false
        try { Stop-HaiComposeRuntimeForUninstall } catch { $failed = $true }
        $expectedCalls = switch ($script:scenario) { 'ownership-failure' { 0 } 'config-failure' { 1 } 'stop-failure' { 2 } default { 3 } }
        if ($failed -ne ($script:scenario -cne 'success') -or $script:calls.Count -ne $expectedCalls) { $failures.Add($script:scenario) }
    }
} finally { $env:LOCALAPPDATA = $originalProfile }
if ($failures.Count -gt 0) { throw "Uninstall stop contract failed: $($failures -join ', ')" }
Write-Output 'Installer uninstall stop contract: 6 PASS; no real Docker, files or process changes.'
