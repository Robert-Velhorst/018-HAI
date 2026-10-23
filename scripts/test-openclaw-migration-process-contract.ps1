[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$tokens = $null
$parseErrors = $null
$source = Join-Path $PSScriptRoot 'test-openclaw-maintenance-migration.ps1'
$ast = [Management.Automation.Language.Parser]::ParseFile($source, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw 'Migration fixture script syntax is invalid.' }
$helper = @($ast.FindAll({ param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and
    $node.Name -ceq 'Invoke-HaiTestPowerShell'
}, $true))
if ($helper.Count -ne 1) { throw 'Expected one migration child-process helper.' }
. ([scriptblock]::Create($helper[0].Extent.Text))
$command = Get-Command Invoke-HaiTestPowerShell
if (-not $command.Parameters.ContainsKey('TimeoutSeconds')) {
    throw 'Migration child processes must have an explicit bounded deadline.'
}

$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ('hai-migration-process-contract-' + [Guid]::NewGuid().ToString('N'))
$utf8 = New-Object Text.UTF8Encoding($false, $true)
New-Item -ItemType Directory -Path $temporaryRoot | Out-Null
try {
    $executable = (Get-Process -Id $PID).Path
    $success = Join-Path $temporaryRoot 'success.ps1'
    [IO.File]::WriteAllText($success, "Write-Output 'fixture-success'; exit 0", $utf8)
    $result = Invoke-HaiTestPowerShell -Executable $executable -ScriptPath $success -Arguments '' -TimeoutSeconds 15
    if ($result.ExitCode -ne 0 -or $result.Output -notmatch 'fixture-success') {
        throw 'Successful fixture output or exit code was lost.'
    }

    $failure = Join-Path $temporaryRoot 'failure.ps1'
    [IO.File]::WriteAllText($failure, "[Console]::Error.WriteLine('fixture-failure'); exit 7", $utf8)
    $result = Invoke-HaiTestPowerShell -Executable $executable -ScriptPath $failure -Arguments '' -TimeoutSeconds 15
    if ($result.ExitCode -ne 7 -or $result.Output -notmatch 'fixture-failure') {
        throw 'Failed fixture output or exit code was lost.'
    }

    $slow = Join-Path $temporaryRoot 'slow.ps1'
    [IO.File]::WriteAllText($slow, 'Start-Sleep -Seconds 30', $utf8)
    $watch = [Diagnostics.Stopwatch]::StartNew()
    $timeoutObserved = $false
    try {
        $null = Invoke-HaiTestPowerShell -Executable $executable -ScriptPath $slow -Arguments '' -TimeoutSeconds 1
    } catch {
        $timeoutObserved = $_.Exception.Message -ceq 'A migration test child process exceeded its 1-second deadline.'
    }
    $watch.Stop()
    if (-not $timeoutObserved -or $watch.Elapsed.TotalSeconds -gt 15) {
        throw 'A stalled fixture child was not stopped within the bounded deadline.'
    }
    Write-Output 'OpenClaw migration process contract passed: success output, failed exit/stderr, bounded timeout.'
} finally {
    $resolved = [IO.Path]::GetFullPath($temporaryRoot)
    $parent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if (-not $resolved.StartsWith($parent, [StringComparison]::OrdinalIgnoreCase) -or
        [IO.Path]::GetFileName($resolved) -notmatch '\Ahai-migration-process-contract-[a-f0-9]{32}\z') {
        throw 'Refusing cleanup outside the generated contract fixture directory.'
    }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
