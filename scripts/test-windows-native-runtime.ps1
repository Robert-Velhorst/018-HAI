[CmdletBinding()]
param(
    [switch]$CrossCompileDocker,
    [switch]$CompileOnly,
    [string]$GoExecutable,
    [ValidateRange(30, 900)][int]$NativeBuildTimeoutSeconds = 600,
    [ValidatePattern('^hai-[a-z0-9-]+$')]
    [string]$ReadOnlyModuleCacheVolume
)

function Resolve-NativeAcceptanceGo([string]$ExplicitPath) {
    if (-not [string]::IsNullOrWhiteSpace($ExplicitPath)) {
        if (-not [IO.Path]::IsPathRooted($ExplicitPath) -or -not (Test-Path -LiteralPath $ExplicitPath -PathType Leaf)) {
            throw 'The selected native Go executable must be an existing absolute file path.'
        }
        return (Resolve-Path -LiteralPath $ExplicitPath -ErrorAction Stop).Path
    }
    $command = Get-Command go.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($command) { return $command.Source }
    if ($env:USERPROFILE) {
        $cached = Join-Path $env:USERPROFILE 'go\pkg\mod\golang.org\toolchain@v0.0.1-go1.25.13.windows-amd64\bin\go.exe'
        if (Test-Path -LiteralPath $cached -PathType Leaf) { return $cached }
    }
    throw 'Existing Go 1.25.13 windows/amd64 is required; this runner does not download or install it.'
}

function Invoke-BoundedNativeCompiler([string]$Executable, [string[]]$Arguments, [string]$WorkingDirectory, [string]$LogDirectory, [int]$TimeoutMilliseconds) {
    if ($PSVersionTable.PSVersion.Major -lt 7) {
        throw 'Bounded native compilation requires PowerShell 7 for owned process-tree termination.'
    }
    if ($TimeoutMilliseconds -le 0) { throw 'A positive native compilation deadline is required.' }
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = $Executable
    $info.WorkingDirectory = $WorkingDirectory
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    foreach ($argument in $Arguments) { $info.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $info
    $stdout = $null
    $stderr = $null
    $started = $false
    $mutex = [Threading.Mutex]::new($false, 'HAI_NATIVE_COMPILER_V1')
    $owned = $false
    try {
        try { $owned = $mutex.WaitOne(0) }
        catch [Threading.AbandonedMutexException] { $owned = $true }
        if (-not $owned) { throw 'Another native acceptance compilation is active; no compiler was started.' }
        $stdout = [IO.File]::Create((Join-Path $LogDirectory 'native-compile.stdout.log'))
        $stderr = [IO.File]::Create((Join-Path $LogDirectory 'native-compile.stderr.log'))
        $started = $process.Start()
        if (-not $started) { throw 'The own native compiler could not start.' }
        $outCopy = $process.StandardOutput.BaseStream.CopyToAsync($stdout)
        $errCopy = $process.StandardError.BaseStream.CopyToAsync($stderr)
        if (-not $process.WaitForExit($TimeoutMilliseconds)) {
            $process.Kill($true)
            if (-not $process.WaitForExit(10000)) { throw 'Own native compiler termination was not confirmed; inspect retained logs.' }
            if (-not [Threading.Tasks.Task]::WaitAll([Threading.Tasks.Task[]]@($outCopy, $errCopy), 10000)) {
                throw 'Native compiler log capture did not finish; retained logs may be incomplete.'
            }
            throw 'Native compilation exceeded its bounded deadline; no acceptance pass is claimed.'
        }
        if (-not [Threading.Tasks.Task]::WaitAll([Threading.Tasks.Task[]]@($outCopy, $errCopy), 10000)) {
            throw 'Native compiler log capture did not finish; retained logs may be incomplete.'
        }
        if ($process.ExitCode -ne 0) { throw "Native compilation failed with exit $($process.ExitCode); inspect retained compiler logs." }
    } finally {
        try {
            if ($started -and -not $process.HasExited) {
                $process.Kill($true)
                if (-not $process.WaitForExit(10000)) { throw 'Own native compiler cleanup was not confirmed.' }
            }
        } finally {
            try {
                if ($stdout) { $stdout.Dispose() }
                if ($stderr) { $stderr.Dispose() }
                $process.Dispose()
            } finally {
                if ($owned) { $mutex.ReleaseMutex() }
                $mutex.Dispose()
            }
        }
    }
}

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
    throw 'This runner requires native Windows; Linux compilation is not native proof.'
}
if (-not [Environment]::Is64BitOperatingSystem) {
    throw 'The guarded acceptance target is Windows amd64.'
}
if ($ReadOnlyModuleCacheVolume -and -not $CrossCompileDocker) {
    throw 'A read-only module cache volume applies only to explicit Docker cross-compilation.'
}
if ($GoExecutable -and $CrossCompileDocker) {
    throw 'An explicit native Go executable cannot be combined with Docker cross-compilation.'
}
if (-not $CrossCompileDocker -and $PSVersionTable.PSVersion.Major -lt 7) {
    throw 'Native acceptance compilation requires PowerShell 7; no build was started.'
}

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$backendRoot = Join-Path $repositoryRoot 'backend'
$testSource = Join-Path $backendRoot 'internal\agentruntime\native_windows_acceptance_test.go'
$scratch = Join-Path ([IO.Path]::GetTempPath()) ('hai-windows-native-acceptance-' + [Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($scratch) | Out-Null
$binary = Join-Path $scratch 'agentruntime-native.test.exe'
$marker = Join-Path $scratch 'synthetic-only.marker'
[IO.File]::WriteAllText($marker, 'synthetic-only-v1')
$sourceHash = (Get-FileHash -LiteralPath $testSource -Algorithm SHA256).Hash
$runnerHash = (Get-FileHash -LiteralPath $PSCommandPath -Algorithm SHA256).Hash
$contractSources = [ordered]@{}
foreach ($directory in @('agentruntime', 'pathsafety', 'safety')) {
    foreach ($source in Get-ChildItem -LiteralPath (Join-Path $backendRoot "internal\$directory") -Filter '*.go' -File | Sort-Object Name) {
        $contractSources["internal/$directory/$($source.Name)"] = (Get-FileHash -LiteralPath $source.FullName -Algorithm SHA256).Hash
    }
}
$evidencePath = Join-Path $scratch 'acceptance-evidence.json'
$evidence = [ordered]@{
    startedUtc = [DateTime]::UtcNow.ToString('o')
    buildMode = 'not-built'
    nativeBuildTimeoutSeconds = $NativeBuildTimeoutSeconds
    nativeExecuted = $false
    nativePassed = $false
    testSourceSha256 = $sourceHash
    runnerSha256 = $runnerHash
    binarySha256 = $null
    contractSourceSha256 = $contractSources
    osVersion = [Environment]::OSVersion.Version.ToString()
    scratch = $scratch
    fixturePolicy = 'synthetic-only; no credentials/providers/shell evaluation/descendants; retain all scratch'
    limitations = @('Not an OS-enforced sandbox proof', 'Not descendant-tree containment proof', 'Not installed-product or live-provider acceptance', 'Current agentruntime adapter and pathsafety contracts only; not a controlledruntime Windows supervisor proof')
}

function Save-AcceptanceEvidence {
    [IO.File]::WriteAllText($evidencePath, ($evidence | ConvertTo-Json -Depth 5))
}

function Invoke-IsolatedAcceptanceBinary([string]$Arguments, [string]$LogName) {
    $info = New-Object Diagnostics.ProcessStartInfo
    $info.FileName = $binary
    $info.Arguments = $Arguments
    $info.WorkingDirectory = $scratch
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $info.EnvironmentVariables.Clear()
    # Retain only Windows loader requirements. Never inherit host credentials,
    # runtime configuration, proxy settings, PATH, or real user-profile paths.
    foreach ($name in @('SystemRoot', 'WINDIR')) {
        $value = [Environment]::GetEnvironmentVariable($name)
        if ($value) { $info.EnvironmentVariables[$name] = $value }
    }
    foreach ($name in @('HOME', 'USERPROFILE', 'TEMP', 'TMP', 'PATH')) {
        $info.EnvironmentVariables[$name] = $scratch
    }
    $info.EnvironmentVariables['HAI_WINDOWS_NATIVE_ACCEPTANCE'] = 'synthetic-only-v1'
    $info.EnvironmentVariables['HAI_WINDOWS_NATIVE_ACCEPTANCE_ROOT'] = $scratch
    $process = New-Object Diagnostics.Process
    $process.StartInfo = $info
    try {
        if (-not $process.Start()) { throw 'The own acceptance binary did not start.' }
        if ($LogName -eq 'native-tests.log') {
            $evidence['nativeExecuted'] = $true
            Save-AcceptanceEvidence
        }
        $stdoutTask = $process.StandardOutput.ReadToEndAsync()
        $stderrTask = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit(90000)) {
            $process.Kill() # Only this runner-owned process; no process-name matching.
            $process.WaitForExit()
            throw 'Own acceptance binary exceeded the bounded runner deadline.'
        }
        $stdout = $stdoutTask.GetAwaiter().GetResult()
        $stderr = $stderrTask.GetAwaiter().GetResult()
        [IO.File]::WriteAllText((Join-Path $scratch $LogName), $stdout + $stderr)
        if ($LogName -eq 'native-tests.log') {
            $evidence['passedChecks'] = @([regex]::Matches($stdout, '(?m)^\s*--- PASS: (TestWindowsNativeAcceptance/\S+) \(') | ForEach-Object { $_.Groups[1].Value })
            $evidence['failedChecks'] = @([regex]::Matches($stdout, '(?m)^\s*--- FAIL: (TestWindowsNativeAcceptance/\S+) \(') | ForEach-Object { $_.Groups[1].Value })
            Save-AcceptanceEvidence
        }
        Write-Host $stdout
        if ($stderr) { Write-Host $stderr }
        if ($process.ExitCode -ne 0) { throw "Acceptance binary exited $($process.ExitCode); see $LogName." }
        return $stdout
    } finally {
        $process.Dispose()
    }
}

try {
    if ($CrossCompileDocker) {
        $docker = Get-Command docker.exe -CommandType Application -ErrorAction Stop
        # Use only an already-present pinned toolchain. No pull/install, Docker
        # socket mount, credentials, shared writable cache, or service mutation.
        $image = & $docker.Source image inspect golang:1.25.13 --format '{{.Id}}'
        if ($LASTEXITCODE -ne 0) { throw 'golang:1.25.13 must already exist locally; this runner does not pull it.' }
        $evidence['buildImage'] = "$image".Trim()
        $containerName = 'hai-native-acceptance-build-' + [Guid]::NewGuid().ToString('N')
        $evidence['buildContainer'] = $containerName
        $dockerArgs = @('run', '--name', $containerName, '--network', 'none', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
            '--cpus', '1', '--memory', '512m', '--memory-swap', '512m', '--pids-limit', '128',
            '--mount', "type=bind,source=$backendRoot,target=/src,readonly",
            '--mount', "type=bind,source=$scratch,target=/evidence",
            '--workdir', '/src', '--env', 'GOOS=windows', '--env', 'GOARCH=amd64', '--env', 'CGO_ENABLED=0',
            '--env', 'GOTOOLCHAIN=local', '--env', 'GOPROXY=off', '--env', 'GOSUMDB=off', '--env', 'GOCACHE=/evidence/go-build',
            '--env', 'GOMAXPROCS=1', '--env', 'GOMEMLIMIT=384MiB')
        if ($ReadOnlyModuleCacheVolume) {
            & $docker.Source volume inspect $ReadOnlyModuleCacheVolume --format '{{.Name}}' | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'The explicitly selected read-only module cache does not exist.' }
            $dockerArgs += @('--mount', "type=volume,source=$ReadOnlyModuleCacheVolume,target=/go/pkg/mod,readonly")
        }
        $dockerArgs += @('--entrypoint', 'go', "$image".Trim(), 'test', '-p', '1', '-mod=readonly', '-c', '-o', '/evidence/agentruntime-native.test.exe', './internal/agentruntime')
        Write-Host 'Cross-compiling in a socket-free, network-disabled build container. This is not native execution.'
        & $docker.Source @dockerArgs
        if ($LASTEXITCODE -ne 0) { throw 'Windows test cross-compilation failed; native execution was not attempted.' }
        $evidence['buildMode'] = 'linux-cross-compile-go1.25.13-windows-amd64'
        # Preserve the stopped one-shot container and all scratch; no cleanup.
    } else {
        $go = Resolve-NativeAcceptanceGo $GoExecutable
        $savedEnvironment = @{}
        foreach ($name in @('GOTOOLCHAIN', 'GOPROXY', 'GOSUMDB', 'GOCACHE', 'GOFLAGS', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'GOMAXPROCS', 'GOMEMLIMIT')) {
            $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
        }
        Push-Location -LiteralPath $backendRoot
        try {
            $env:GOTOOLCHAIN = 'local'
            $env:GOPROXY = 'off'
            $env:GOSUMDB = 'off'
            $env:GOCACHE = Join-Path $scratch 'go-build'
            $env:GOFLAGS = ''
            $env:GOOS = 'windows'
            $env:GOARCH = 'amd64'
            $env:CGO_ENABLED = '0'
            $env:GOMAXPROCS = '1'
            $env:GOMEMLIMIT = '384MiB'
            $version = & $go version
            if ($LASTEXITCODE -ne 0 -or $version -notmatch 'go1\.25\.13 windows/amd64') {
                throw 'Native build requires existing Go 1.25.13 windows/amd64; use -CrossCompileDocker when absent.'
            }
            Invoke-BoundedNativeCompiler -Executable $go -Arguments @('test', '-p', '1', '-mod=readonly', '-c', '-o', $binary, './internal/agentruntime') -WorkingDirectory $backendRoot -LogDirectory $scratch -TimeoutMilliseconds ($NativeBuildTimeoutSeconds * 1000)
        } finally {
            Pop-Location
            foreach ($name in $savedEnvironment.Keys) {
                [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process')
            }
        }
        $evidence['buildMode'] = 'native-build-go1.25.13-windows-amd64'
    }
    if ((Get-FileHash -LiteralPath $testSource -Algorithm SHA256).Hash -ne $sourceHash) {
        throw 'Acceptance source changed during compilation; refuse potentially stale native proof.'
    }
    foreach ($relative in $contractSources.Keys) {
        if ((Get-FileHash -LiteralPath (Join-Path $backendRoot $relative) -Algorithm SHA256).Hash -ne $contractSources[$relative]) {
            throw "Runtime contract source changed during compilation: $relative; refuse stale native proof."
        }
    }
    $evidence['binarySha256'] = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash
    Save-AcceptanceEvidence
    if ($CompileOnly) {
        Write-Host "Compilation only; nativeExecuted=false. Evidence: $evidencePath"
        return
    }
    $listing = Invoke-IsolatedAcceptanceBinary '-test.list=^TestWindowsNativeAcceptance$' 'test-list.log'
    if ($listing -notmatch '(?m)^TestWindowsNativeAcceptance\r?$') {
        throw 'Own guarded acceptance suite is missing from the binary; do not report native proof.'
    }
    $output = Invoke-IsolatedAcceptanceBinary '-test.run=^TestWindowsNativeAcceptance$ -test.v -test.count=1 -test.parallel=1 -test.timeout=60s' 'native-tests.log'
    if ($output -match '--- SKIP:' -or $output -notmatch '(?m)^--- PASS: TestWindowsNativeAcceptance \(') {
        throw 'Native suite did not complete without skips; no acceptance pass claimed.'
    }
    $evidence['nativePassed'] = $true
    $evidence['completedUtc'] = [DateTime]::UtcNow.ToString('o')
    Save-AcceptanceEvidence
    Write-Host "NATIVE WINDOWS SYNTHETIC ACCEPTANCE PASSED. Evidence and all fixtures retained: $evidencePath"
} catch {
    $evidence['failure'] = $_.Exception.Message
    Save-AcceptanceEvidence
    throw
} finally {
    Write-Host "Retained scratch: $scratch"
}
