Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$path = Join-Path $PSScriptRoot 'test-windows-native-runtime.ps1'
$ast = [Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw 'Native runner parse failed.' }
$helper = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Invoke-BoundedNativeCompiler' }, $true)
if (-not $helper) { throw 'Bounded compiler helper missing.' }
. ([scriptblock]::Create($helper.Extent.Text))
$node = Get-Command node.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($node) { $nodePath = $node.Source } else {
    $nodePath = Join-Path $env:USERPROFILE '.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe'
}
if (-not (Test-Path -LiteralPath $nodePath -PathType Leaf)) { throw 'Existing Node runtime required; no install attempted.' }
$scratch = Join-Path ([IO.Path]::GetTempPath()) ('hai-compile-deadline-test-' + [guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($scratch) | Out-Null
function Invoke-Fixture([string]$Name, [string]$Code, [int]$Deadline) {
    $logs = Join-Path $scratch $Name
    [IO.Directory]::CreateDirectory($logs) | Out-Null
    Invoke-BoundedNativeCompiler -Executable $nodePath -Arguments @('-e', $Code) -WorkingDirectory $scratch -LogDirectory $logs -TimeoutMilliseconds $Deadline
}
try {
    Invoke-Fixture 'success' 'console.log("compile-fixture-ok");console.error("diagnostic-fixture");' 15000
    if (-not ([IO.File]::ReadAllText((Join-Path $scratch 'success/native-compile.stdout.log'))).Contains('compile-fixture-ok')) { throw 'Standard output not retained.' }
    if (-not ([IO.File]::ReadAllText((Join-Path $scratch 'success/native-compile.stderr.log'))).Contains('diagnostic-fixture')) { throw 'Standard error not retained.' }
    $failure = $false
    try { Invoke-Fixture 'failure' 'process.exit(7)' 15000 } catch {
        if ($_.Exception.Message -notmatch 'exit 7') { throw }
        $failure = $true
    }
    if (-not $failure) { throw 'Nonzero compiler exit was accepted.' }
    $deadline = $false
    try { Invoke-Fixture 'timeout' 'console.log(process.pid);setInterval(()=>{},1000);' 1000 } catch {
        if ($_.Exception.Message -notmatch 'exceeded its bounded deadline') { throw }
        $deadline = $true
    }
    if (-not $deadline) { throw 'Compiler timeout was accepted.' }
    $fixturePid = [int]([IO.File]::ReadAllText((Join-Path $scratch 'timeout/native-compile.stdout.log')).Trim())
    if (Get-Process -Id $fixturePid -ErrorAction SilentlyContinue) { throw 'Own timeout fixture still running.' }
    $startup = $false
    try {
        Invoke-BoundedNativeCompiler -Executable (Join-Path $scratch 'missing.exe') -Arguments @() -WorkingDirectory $scratch -LogDirectory $scratch -TimeoutMilliseconds 1000
    } catch {
        if ($_.Exception.Message -match 'No process is associated') { throw 'Finally masked the compiler startup failure.' }
        $startup = $true
    }
    if (-not $startup) { throw 'Missing compiler executable was accepted.' }
    Invoke-Fixture 'after-failure' 'process.exit(0)' 15000
    $ready = [Threading.ManualResetEventSlim]::new($false)
    $release = [Threading.ManualResetEventSlim]::new($false)
    $holder = [powershell]::Create()
    $handle = $null
    try {
        $null = $holder.AddScript({
            param($Ready, $Release)
            $mutex = [Threading.Mutex]::new($false, 'HAI_NATIVE_COMPILER_V1')
            $owned = $false
            try {
                try { $owned = $mutex.WaitOne(1000) }
                catch [Threading.AbandonedMutexException] { $owned = $true }
                if (-not $owned) { throw 'Compiler contention fixture could not acquire the lock.' }
                $Ready.Set()
                if (-not $Release.Wait(10000)) { throw 'Compiler contention fixture release timed out.' }
            } finally {
                if ($owned) { $mutex.ReleaseMutex() }
                $mutex.Dispose()
            }
        }).AddArgument($ready).AddArgument($release)
        $handle = $holder.BeginInvoke()
        if (-not $ready.Wait(3000)) { throw 'Compiler contention fixture not ready.' }
        $refused = $false
        try { Invoke-Fixture 'contending' 'console.log("must-not-run")' 15000 } catch {
            if ($_.Exception.Message -notmatch 'Another native acceptance compilation is active') { throw }
            $refused = $true
        }
        if (-not $refused -or (Test-Path -LiteralPath (Join-Path $scratch 'contending/native-compile.stdout.log'))) {
            throw 'Contending compilation reached execution or log creation.'
        }
    } finally {
        $release.Set()
        try {
            if ($null -ne $handle) {
                $null = $holder.EndInvoke($handle)
                if ($holder.HadErrors) { throw 'Compiler contention fixture failed.' }
            }
        } finally {
            $holder.Dispose()
            $ready.Dispose()
            $release.Dispose()
        }
    }
    Invoke-Fixture 'after-contention' 'process.exit(0)' 15000
    Write-Host 'Compiler deadline: output capture, exit failure, actual timeout, startup failure, lock release and cross-thread contention checks passed. No Go compiler, server, provider or container executed.'
} finally {
    Write-Host "Synthetic logs retained: $scratch"
}
