$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly

$shell = Join-Path $PSHOME 'pwsh.exe'
if (-not (Test-Path -LiteralPath $shell -PathType Leaf)) {
    throw 'The bounded-process behavior test requires the current PowerShell executable.'
}

$success = Invoke-HaiBoundedProcess $shell @('-NoProfile', '-NonInteractive', '-Command', 'Write-Output bounded-process-ok') -TimeoutSeconds 5
if (-not $success.succeeded -or $success.timed_out -or $success.output.Trim() -cne 'bounded-process-ok') {
    throw 'Bounded process runner did not capture a successful child process.'
}

$timer = [Diagnostics.Stopwatch]::StartNew()
$timeout = Invoke-HaiBoundedProcess $shell @('-NoProfile', '-NonInteractive', '-Command', 'Start-Sleep -Seconds 10') -TimeoutSeconds 1
$timer.Stop()
if ($timeout.succeeded -or -not $timeout.timed_out -or $timer.Elapsed.TotalSeconds -gt 8) {
    throw 'Bounded process runner did not terminate and report a timed-out child process.'
}

Write-Output 'HAI bounded process timeout behavior: PASS'
