[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$contractPath = Join-Path $PSScriptRoot 'test-windows-installer-contract.ps1'
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($contractPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw 'Installer contract did not parse.' }

$staticExit = @($ast.EndBlock.Statements | Where-Object {
    $_ -is [Management.Automation.Language.IfStatementAst] -and
    $_.Clauses[0].Item1.Extent.Text -ceq '$StaticOnly'
})
if ($staticExit.Count -ne 1 -or $staticExit[0].ElseClause -or
    $staticExit[0].Clauses.Count -ne 1 -or
    $staticExit[0].Clauses[0].Item2.Statements[-1].Extent.Text -cne 'exit 0') {
    throw 'Static mode must exit successfully before payload preparation.'
}

# New native, script, or mutation commands must not silently become static-safe.
$readOnlyCommands = @(
    'Join-Path', 'Resolve-Path', 'Test-Path', 'Get-Content', 'Get-Variable',
    'Set-Variable', 'ForEach-Object', 'Select-Object', 'Out-Null', 'Write-Host'
)
$calls = @($ast.FindAll({ param($node)
    $node -is [Management.Automation.Language.CommandAst]
}, $true) | Where-Object {
    $_.InvocationOperator -ne [Management.Automation.Language.TokenKind]::Unknown -or
    $readOnlyCommands -notcontains $_.GetCommandName()
})
if ($calls.Count -eq 0) { throw 'Expected fixture and payload invocation boundaries.' }
foreach ($call in $calls) {
    if ($call.Extent.StartOffset -gt $staticExit[0].Extent.EndOffset) { continue }
    $guarded = $false
    for ($parent = $call.Parent; $null -ne $parent; $parent = $parent.Parent) {
        if ($parent -isnot [Management.Automation.Language.IfStatementAst]) { continue }
        foreach ($clause in $parent.Clauses) {
            if ($clause.Item1.Extent.Text -cmatch '^-not \$StaticOnly(?: -and -not \$[A-Za-z]+)*$' -and
                $call.Extent.StartOffset -ge $clause.Item2.Extent.StartOffset -and
                $call.Extent.EndOffset -le $clause.Item2.Extent.EndOffset) {
                $guarded = $true
            }
        }
    }
    if (-not $guarded) {
        throw "Fixture invocation at line $($call.Extent.StartLineNumber) is not excluded from static mode."
    }
}

foreach ($mode in @('SkipPayload', 'OpenClawMaintenanceOnly', 'UninstallRuntimeOnly')) {
    $arguments = @{ StaticOnly = $true }
    $arguments[$mode] = $true
    $rejected = $false
    try {
        & $contractPath @arguments
    } catch {
        if ($_.Exception.Message -cne 'StaticOnly cannot be combined with payload or behavioral fixture modes.') { throw }
        $rejected = $true
    }
    if (-not $rejected) { throw "Static mode accepted conflicting switch $mode." }
}

& $contractPath -StaticOnly
if ($LASTEXITCODE -ne 0) { throw 'Static installer contract failed.' }
Write-Host "Installer static-mode invocation boundaries passed: $($calls.Count) guarded calls and 3 conflicting-mode rejections."
