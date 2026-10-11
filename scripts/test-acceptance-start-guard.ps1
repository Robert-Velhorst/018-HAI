[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'isolated-acceptance-stack.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw 'Acceptance launcher has syntax errors.' }
$definitions = @($ast.FindAll({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Invoke-ExclusiveAcceptanceStart' }, $true))
if ($definitions.Count -ne 1) { throw 'Expected one exclusive start guard.' }
. ([scriptblock]::Create($definitions[0].Extent.Text))
$script:queries = 0
$script:starts = 0
$script:existing = @()
$script:queryFailure = $false
function Invoke-Docker([string[]]$Arguments) {
    $script:queries++
    if (($Arguments -join '|') -ne 'ps|-aq|--filter|label=hai.acceptance.owner') { throw 'Guard did not inspect all acceptance owners.' }
    if ($script:queryFailure) { throw 'Synthetic inventory failure.' }
    return $script:existing
}
foreach ($ids in @(@('same-run'), @('other-run'), @('same-run', 'other-run'))) {
    $script:existing = $ids
    $refused = $false
    try { Invoke-ExclusiveAcceptanceStart { $script:starts++ } }
    catch { if ($_.Exception.Message -notmatch 'Acceptance containers already exist') { throw }; $refused = $true }
    if (-not $refused -or $script:starts) { throw 'Existing acceptance run did not prevent startup.' }
}
$script:existing = @()
$script:queryFailure = $true
$refused = $false
try { Invoke-ExclusiveAcceptanceStart { $script:starts++ } }
catch { if ($_.Exception.Message -notmatch 'Synthetic inventory failure') { throw }; $refused = $true }
if (-not $refused -or $script:starts) { throw 'Inventory failure permitted startup.' }
$script:queryFailure = $false
try { Invoke-ExclusiveAcceptanceStart { throw 'Synthetic startup failure.' } }
catch { if ($_.Exception.Message -notmatch 'Synthetic startup failure') { throw } }
Invoke-ExclusiveAcceptanceStart { $script:starts++ }
Invoke-ExclusiveAcceptanceStart { $script:starts++ }
if ($script:starts -ne 2 -or $script:queries -ne 7) { throw 'Guard did not release after failed/successful startup.' }

# A second runspace holds the real OS mutex on another thread, in this process.
$ready = [Threading.ManualResetEventSlim]::new($false)
$release = [Threading.ManualResetEventSlim]::new($false)
$holder = [powershell]::Create()
$handle = $null
try {
    $null = $holder.AddScript({
        param($Ready, $Release)
        $mutex = [Threading.Mutex]::new($false, 'HAI_ACCEPTANCE_START_V1')
        $owned = $false
        try {
            try { $owned = $mutex.WaitOne(1000) }
            catch [Threading.AbandonedMutexException] { $owned = $true }
            if (-not $owned) { throw 'Test holder could not acquire the mutex.' }
            $Ready.Set()
            if (-not $Release.Wait(10000)) { throw 'Test holder release timed out.' }
        } finally {
            if ($owned) { $mutex.ReleaseMutex() }
            $mutex.Dispose()
        }
    }).AddArgument($ready).AddArgument($release)
    $handle = $holder.BeginInvoke()
    if (-not $ready.Wait(3000)) { throw 'Mutex contention fixture did not become ready.' }
    $refused = $false
    try { Invoke-ExclusiveAcceptanceStart { $script:starts++ } }
    catch { if ($_.Exception.Message -notmatch 'Another acceptance startup is in progress') { throw }; $refused = $true }
    if (-not $refused -or $script:starts -ne 2 -or $script:queries -ne 7) {
        throw 'Contending startup reached inventory or execution.'
    }
} finally {
    $release.Set()
    try {
        if ($null -ne $handle) {
            $null = $holder.EndInvoke($handle)
            if ($holder.HadErrors) { throw "Mutex holder failed: $($holder.Streams.Error)" }
        }
    } finally {
        $holder.Dispose()
        $ready.Dispose()
        $release.Dispose()
    }
}
Invoke-ExclusiveAcceptanceStart { $script:starts++ }
if ($script:starts -ne 3 -or $script:queries -ne 8) { throw 'Mutex remained locked after contention.' }
Write-Output 'PASS: existing-run, inventory failure, lock release, and real cross-thread contention with mocked Docker. No containers, listeners, builds, or downloads. Cross-process contention and live startup remain unverified.'
