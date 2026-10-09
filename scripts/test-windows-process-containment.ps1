[CmdletBinding()]
param(
    [ValidateRange(1, 10)][int]$Repetitions = 3,
    [ValidateRange(60, 900)][int]$BuildTimeoutSeconds = 600
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT -or
    -not [Environment]::Is64BitProcess) {
    throw 'Native Windows amd64 PowerShell is required; compilation alone is not containment proof.'
}
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$backend = Join-Path $root 'backend'
$runId = [Guid]::NewGuid().ToString('N')
$scratch = Join-Path ([IO.Path]::GetTempPath()) "hai-windows-process-containment-$runId"
[IO.Directory]::CreateDirectory($scratch) | Out-Null
$evidencePath = Join-Path $scratch 'containment-evidence.json'
$suites = @(
    @{ Package = './cmd/hai-dsh-bridge'; Binary = 'bridge.test.exe'; MinimumProcesses = 4; Names = @(
        'TestWindowsProcessTreeFixture', 'TestWindowsExitCode259Fixture',
        'TestWindowsTerminateRejectsMissingJobHandleAsUnverified',
        'TestWindowsCancellationTerminatesDescendantHoldingOutputHandles',
        'TestWindowsPreservesApplicationExitCode259') },
    @{ Package = './internal/openclawmaintenance'; Binary = 'maintenance.test.exe'; MinimumProcesses = 4; Names = @(
        'TestContainedProcessHelper', 'TestWindowsJobContainsDescendantsAndWaitsForEmpty',
        'TestWindowsJobProcessCreationAndCleanExitAreContained',
        'TestUnverifiedWindowsTerminationIsReportedUnknown') }
)
$evidence = [ordered]@{
    startedUtc = [DateTime]::UtcNow.ToString('o'); runId = $runId; scratch = $scratch
    osVersion = [Environment]::OSVersion.Version.ToString(); nativeExecuted = $false; nativePassed = $false
    powershellVersion = $PSVersionTable.PSVersion.ToString(); architecture = 'windows/amd64'
    repetitions = $Repetitions; commands = @(); runs = @(); binarySha256 = [ordered]@{}
    capabilities = 'Local PowerShell, Win32 Job Objects, cached Docker Go; no eligible parallel agent runner'
    fixturePolicy = 'Explicit named synthetic tests only; cleared child environment; own kill-on-close watchdog job; retained evidence and stopped build container'
    limitations = @('Process-tree supervision is not an OS security sandbox',
        'No installed-product, live-provider, database, WSL, or bridge-enablement acceptance',
        'Three CI names are helper entry points; their standalone PASS is not behavioural coverage',
        'No native race detector: binaries cross-compiled with CGO_ENABLED=0',
        'Parallel agents unavailable without a prohibited external paid provider')
}
function Save-Evidence {
    [IO.File]::WriteAllText($evidencePath, ($evidence | ConvertTo-Json -Depth 12))
}
function Get-SourceHashes {
    $hashes = [ordered]@{}
    if ($evidence.Contains('sourceDirectories')) {
        $files = @(foreach ($directory in $evidence.sourceDirectories) {
            Get-ChildItem -LiteralPath (Join-Path $root $directory) -Filter '*.go' -File
        })
        $files += @(foreach ($relative in $evidence.sourceEmbeddedPaths) {
            Get-Item -LiteralPath (Join-Path $root $relative)
        })
    } else {
        $files = @(Get-ChildItem -LiteralPath $backend -Recurse -Filter '*.go' -File)
        $files += @(Get-ChildItem -LiteralPath $backend -Recurse -Filter '*.sql' -File)
    }
    $files += @(Get-Item -LiteralPath (Join-Path $backend 'go.mod'), (Join-Path $backend 'go.sum'),
        (Join-Path $root '.github\workflows\ci.yml'), $PSCommandPath)
    foreach ($file in $files | Sort-Object FullName) {
        $relative = $file.FullName.Substring($root.Length + 1).Replace('\', '/')
        $hashes[$relative] = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash
    }
    return $hashes
}
function Assert-SourceHashes {
    $current = Get-SourceHashes
    if ($current.Count -ne $evidence.sourceSha256.Count) { throw 'Source file set changed; refuse stale proof.' }
    foreach ($key in $evidence.sourceSha256.Keys) {
        if (-not $current.Contains($key) -or $current[$key] -ne $evidence.sourceSha256[$key]) {
            throw "Source changed during verification: $key; refuse stale proof."
        }
    }
    $evidence['sourceHashesVerifiedUtc'] = [DateTime]::UtcNow.ToString('o')
    Save-Evidence
}

# This independent outer job provides bounded cleanup without PID/name matching.
# Survivors cause failure BEFORE the watchdog terminates them, so it cannot mask
# a production supervisor that falsely reports successful tree termination.
$watchdogSource = @'
using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
public static class HaiProcessContainmentWatchdog {
    [StructLayout(LayoutKind.Sequential)] struct Security {
        public int Length; public IntPtr Descriptor; public int Inherit;
    }
    [StructLayout(LayoutKind.Sequential)] struct Startup {
        public int Size; public IntPtr Reserved, Desktop, Title;
        public uint X, Y, XSize, YSize, XChars, YChars, Fill, Flags;
        public short Show, ReservedSize; public IntPtr ReservedBytes, Input, Output, Error;
    }
    [StructLayout(LayoutKind.Sequential)] struct ProcessInfo {
        public IntPtr Process, Thread; public uint ProcessId, ThreadId;
    }
    [StructLayout(LayoutKind.Sequential)] struct BasicLimit {
        public long User, JobUser; public uint Flags; public UIntPtr Min, Max;
        public uint ActiveLimit; public UIntPtr Affinity; public uint Priority, Scheduling;
    }
    [StructLayout(LayoutKind.Sequential)] struct ExtendedLimit {
        public BasicLimit Basic; public ulong ReadOps, WriteOps, OtherOps, ReadBytes, WriteBytes, OtherBytes;
        public UIntPtr ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory;
    }
    [StructLayout(LayoutKind.Sequential)] struct Accounting {
        public long User, Kernel, PeriodUser, PeriodKernel;
        public uint Faults, Total, Active, Terminated;
    }
    [DllImport("kernel32.dll", SetLastError=true, CharSet=CharSet.Unicode)] static extern IntPtr CreateJobObject(IntPtr a, string n);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool SetInformationJobObject(IntPtr j, int c, ref ExtendedLimit i, uint s);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool QueryInformationJobObject(IntPtr j, int c, out Accounting i, uint s, IntPtr r);
    [DllImport("kernel32.dll", EntryPoint="QueryInformationJobObject", SetLastError=true)] static extern bool QueryIds(IntPtr j, int c, IntPtr i, uint s, IntPtr r);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool AssignProcessToJobObject(IntPtr j, IntPtr p);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool TerminateJobObject(IntPtr j, uint c);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool TerminateProcess(IntPtr p, uint c);
    [DllImport("kernel32.dll", SetLastError=true)] static extern uint ResumeThread(IntPtr t);
    [DllImport("kernel32.dll", SetLastError=true)] static extern uint WaitForSingleObject(IntPtr p, uint m);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool GetExitCodeProcess(IntPtr p, out uint c);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool CloseHandle(IntPtr h);
    [DllImport("kernel32.dll", SetLastError=true, CharSet=CharSet.Unicode)] static extern IntPtr CreateFile(string p, uint a, uint s, ref Security x, uint c, uint f, IntPtr t);
    [DllImport("kernel32.dll", SetLastError=true, CharSet=CharSet.Unicode)] static extern bool CreateProcess(string a, StringBuilder c, IntPtr p, IntPtr t, bool inherit, uint flags, IntPtr env, string dir, ref Startup s, out ProcessInfo i);
    public class Result {
        public uint RootPid, ExitCode, TotalProcesses, ActiveProcesses, TerminatedProcesses;
        public uint ActiveAtRootExit; public long PostExitDrainMilliseconds;
        public Dictionary<string, string> ObservedProcessNames;
        public uint[] ObservedPids; public bool WatchdogApplied, TimedOut; public string Error;
    }
    static void Check(bool ok, string action) {
        if (!ok) throw new Win32Exception(Marshal.GetLastWin32Error(), action);
    }
    static Accounting Count(IntPtr job, HashSet<uint> ids, Dictionary<string, string> names) {
        Accounting a;
        Check(QueryInformationJobObject(job, 1, out a, (uint)Marshal.SizeOf(typeof(Accounting)), IntPtr.Zero), "Query own job accounting");
        int size = 8 + 8 * 256;
        IntPtr buffer = Marshal.AllocHGlobal(size);
        try {
            Check(QueryIds(job, 3, buffer, (uint)size, IntPtr.Zero), "Query own job process IDs");
            int count = Marshal.ReadInt32(buffer, 4);
            if (count < 0 || count > 256) throw new InvalidOperationException("Own fixture process list exceeded bound");
            for (int i = 0; i < count; i++) {
                uint pid = (uint)Marshal.ReadInt64(buffer, 8 + i * 8); ids.Add(pid);
                string key = pid.ToString(System.Globalization.CultureInfo.InvariantCulture);
                if (!names.ContainsKey(key)) {
                    try { using (Process p = Process.GetProcessById((int)pid)) { names[key] = p.ProcessName; } }
                    catch (ArgumentException) { }
                    catch (InvalidOperationException) { }
                    catch (Win32Exception) { }
                }
            }
        } finally { Marshal.FreeHGlobal(buffer); }
        return a;
    }
    public static Result Run(string binary, string args, string dir, string log, string env, int deadlineMs) {
        Result result = new Result(); HashSet<uint> ids = new HashSet<uint>();
        Dictionary<string, string> names = new Dictionary<string, string>();
        IntPtr job = IntPtr.Zero, output = IntPtr.Zero, input = IntPtr.Zero, environment = IntPtr.Zero;
        ProcessInfo pi = new ProcessInfo(); bool assigned = false;
        try {
            job = CreateJobObject(IntPtr.Zero, null); Check(job != IntPtr.Zero, "Create own watchdog job");
            ExtendedLimit limit = new ExtendedLimit(); limit.Basic.Flags = 0x2000;
            Check(SetInformationJobObject(job, 9, ref limit, (uint)Marshal.SizeOf(typeof(ExtendedLimit))), "Set own kill-on-close job");
            Security security = new Security(); security.Length = Marshal.SizeOf(typeof(Security)); security.Inherit = 1;
            output = CreateFile(log, 0x40000000, 3, ref security, 1, 0x80, IntPtr.Zero);
            Check(output != new IntPtr(-1), "Create exclusive fixture log");
            input = CreateFile("NUL", 0x80000000, 3, ref security, 3, 0x80, IntPtr.Zero);
            Check(input != new IntPtr(-1), "Create fixture stdin");
            Startup startup = new Startup(); startup.Size = Marshal.SizeOf(typeof(Startup));
            startup.Flags = 0x100; startup.Input = input; startup.Output = output; startup.Error = output;
            environment = Marshal.StringToHGlobalUni(env);
            Check(CreateProcess(binary, new StringBuilder("\"" + binary + "\" " + args), IntPtr.Zero, IntPtr.Zero,
                true, 0x08000404, environment, dir, ref startup, out pi), "Create own suspended fixture runner");
            result.RootPid = pi.ProcessId; ids.Add(pi.ProcessId);
            Check(AssignProcessToJobObject(job, pi.Process), "Assign suspended fixture runner to own job"); assigned = true;
            if (ResumeThread(pi.Thread) != 1) throw new Win32Exception(Marshal.GetLastWin32Error(), "Resume own fixture runner");
            Stopwatch clock = Stopwatch.StartNew(); Accounting accounting;
            while (true) {
                accounting = Count(job, ids, names);
                uint state = WaitForSingleObject(pi.Process, 0);
                if (state == 0) break;
                Check(state == 258, "Wait for own fixture runner");
                if (clock.ElapsedMilliseconds >= deadlineMs) { result.TimedOut = true; break; }
                Thread.Sleep(10);
            }
            accounting = Count(job, ids, names);
            result.ActiveAtRootExit = accounting.Active;
            // A signalled console process can precede conhost/job-accounting
            // teardown. Require the whole outer job to empty naturally within
            // a bounded drain, never confuse root exit with tree completion.
            if (!result.TimedOut) {
                Stopwatch drain = Stopwatch.StartNew();
                while (accounting.Active != 0 && drain.ElapsedMilliseconds < 2000) {
                    Thread.Sleep(10); accounting = Count(job, ids, names);
                }
                result.PostExitDrainMilliseconds = drain.ElapsedMilliseconds;
            }
            if (result.TimedOut || accounting.Active != 0) {
                result.WatchdogApplied = true;
                if (!result.TimedOut) result.Error = "Fixture descendants survived test runner exit";
                Check(TerminateJobObject(job, 124), "Terminate own watchdog tree");
                Stopwatch stop = Stopwatch.StartNew();
                do {
                    accounting = Count(job, ids, names);
                    if (accounting.Active == 0) break;
                    Thread.Sleep(10);
                } while (stop.ElapsedMilliseconds < 10000);
                if (accounting.Active != 0) throw new InvalidOperationException("Own watchdog job did not empty within deadline");
            }
            Check(WaitForSingleObject(pi.Process, 10000) == 0, "Confirm own root exit");
            Check(GetExitCodeProcess(pi.Process, out result.ExitCode), "Read own fixture exit code");
            result.TotalProcesses = accounting.Total; result.ActiveProcesses = accounting.Active;
            result.TerminatedProcesses = accounting.Terminated;
        } catch (Exception ex) {
            result.Error = ex.ToString();
            if (pi.Process != IntPtr.Zero) {
                result.WatchdogApplied = true;
                bool stopped = assigned ? TerminateJobObject(job, 124) : TerminateProcess(pi.Process, 124);
                if (!stopped || WaitForSingleObject(pi.Process, 10000) != 0) result.Error += "; own cleanup unverified";
                if (assigned) {
                    try {
                        Stopwatch stop = Stopwatch.StartNew(); Accounting a;
                        do {
                            a = Count(job, ids, names);
                            if (a.Active == 0) break;
                            Thread.Sleep(10);
                        } while (stop.ElapsedMilliseconds < 10000);
                        result.ActiveProcesses = a.Active; result.TotalProcesses = a.Total;
                        if (a.Active != 0) result.Error += "; own descendant cleanup unverified";
                    } catch (Exception cleanup) { result.Error += "; own cleanup: " + cleanup.Message; }
                }
            }
        } finally {
            result.ObservedPids = new uint[ids.Count]; ids.CopyTo(result.ObservedPids);
            result.ObservedProcessNames = names;
            if (job != IntPtr.Zero) CloseHandle(job);
            if (pi.Thread != IntPtr.Zero) CloseHandle(pi.Thread);
            if (pi.Process != IntPtr.Zero) CloseHandle(pi.Process);
            if (output != IntPtr.Zero && output != new IntPtr(-1)) CloseHandle(output);
            if (input != IntPtr.Zero && input != new IntPtr(-1)) CloseHandle(input);
            if (environment != IntPtr.Zero) Marshal.FreeHGlobal(environment);
        }
        return result;
    }
}
'@

function Invoke-Fixture([string]$Binary, [string]$Arguments, [string]$LogName,
    [int]$DeadlineMs = 90000, [hashtable]$Overrides = @{}) {
    $environment = @{}
    foreach ($name in @('SystemRoot', 'WINDIR')) {
        $value = [Environment]::GetEnvironmentVariable($name)
        if ($value) { $environment[$name] = $value }
    }
    foreach ($name in @('HOME', 'USERPROFILE', 'TEMP', 'TMP', 'PATH')) { $environment[$name] = $scratch }
    foreach ($name in $Overrides.Keys) { $environment[$name] = $Overrides[$name] }
    $block = (($environment.Keys | Sort-Object | ForEach-Object { "$_=$($environment[$_])" }) -join "`0") + "`0`0"
    $evidence.commands += @{ executable = $Binary; arguments = $Arguments; deadlineMs = $DeadlineMs; log = $LogName }
    Save-Evidence
    $result = [HaiProcessContainmentWatchdog]::Run($Binary, $Arguments, $scratch, (Join-Path $scratch $LogName), $block, $DeadlineMs)
    $record = [ordered]@{ log = $LogName; arguments = $Arguments; supervision = $result }
    $evidence.runs += $record
    Save-Evidence
    if ($result.Error) { throw "Fixture supervision failed: $($result.Error)" }
    return $record
}

try {
    $evidence['sourceSha256'] = Get-SourceHashes
    $workflow = Get-Content -LiteralPath (Join-Path $root '.github\workflows\ci.yml') -Raw
    $ciStep = [regex]::Match($workflow, '(?s)- name: Windows process containment tests.*?(?=\r?\n      - name:)').Value
    if (-not $ciStep) { throw 'Existing Windows installer containment CI step is missing.' }
    foreach ($suite in $suites) {
        foreach ($name in $suite.Names) {
            if (-not $ciStep.Contains('"' + $name + '"')) { throw "Required CI test missing: $name" }
        }
    }
    $evidence['ciJob'] = 'windows-installer / Windows installer build / Windows process containment tests'
    $evidence['requiredSuites'] = $suites
    [IO.File]::WriteAllText((Join-Path $scratch 'git-status-before.txt'), ((& git -C $root status --short --untracked-files=all) -join "`n"))
    Save-Evidence
    $docker = (Get-Command docker.exe -CommandType Application -ErrorAction Stop).Source
    $image = & $docker image inspect golang:1.25.13 --format '{{.Id}}'
    if ($LASTEXITCODE -ne 0) { throw 'Cached golang:1.25.13 is unavailable; no pull permitted.' }
    $image = "$image".Trim()
    foreach ($volume in @('hai-go-modcache', 'hai-go-buildcache')) {
        & $docker volume inspect $volume --format '{{.Name}}' | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "Existing build cache missing: $volume" }
    }
    $name = "hai-process-containment-build-$runId"
    $manifestTemplate = '{{range .GoFiles}}{{if eq (slice . 0 1) "/"}}{{.}}{{else}}{{$.Dir}}/{{.}}{{end}}{{"\n"}}{{end}}{{range .EmbedFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}'
    $build = "go list -mod=readonly -deps -test -f '$manifestTemplate' ./cmd/hai-dsh-bridge ./internal/openclawmaintenance > /evidence/build-sources.txt && " +
        'go test -mod=readonly -c -o /evidence/bridge.test.exe ./cmd/hai-dsh-bridge && go test -mod=readonly -c -o /evidence/maintenance.test.exe ./internal/openclawmaintenance'
    $arguments = @('create', '--name', $name, '--label', "com.hai.process-containment.owner=$runId",
        '--pull', 'never', '--read-only', '--network', 'none', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
        '--tmpfs', '/tmp:rw,exec,size=2g', '--mount', "type=bind,source=$backend,target=/src,readonly",
        '--mount', "type=bind,source=$scratch,target=/evidence",
        '--mount', 'type=volume,source=hai-go-modcache,target=/go/pkg/mod,readonly',
        '--mount', 'type=volume,source=hai-go-buildcache,target=/go-build',
        '--workdir', '/src', '--env', 'GOOS=windows', '--env', 'GOARCH=amd64', '--env', 'CGO_ENABLED=0',
        '--env', 'GOTOOLCHAIN=local', '--env', 'GOPROXY=off', '--env', 'GOSUMDB=off', '--env', 'GOCACHE=/go-build',
        '--env', 'GOFLAGS=', '--entrypoint', '/bin/sh', $image, '-c', $build)
    $evidence.commands += @{ executable = $docker; arguments = $arguments }
    $containerId = "$(& $docker @arguments)".Trim()
    if ($LASTEXITCODE -ne 0 -or $containerId -notmatch '^[a-f0-9]{64}$') { throw 'Owned build container creation failed.' }
    $evidence['buildContainerId'] = $containerId
    $evidence['buildImageId'] = $image
    $evidence['buildContainerName'] = $name
    Save-Evidence
    function Get-OwnedBuild {
        $json = & $docker inspect $containerId
        if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect own build container.' }
        $state = @($json | ConvertFrom-Json)[0]
        if ($state.Id -ne $containerId -or $state.Config.Labels.'com.hai.process-containment.owner' -ne $runId -or $state.Image -ne $image) {
            throw 'Build container ownership mismatch; refuse start/stop.'
        }
        return $state
    }
    $null = Get-OwnedBuild
    & $docker start $containerId | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Own build container did not start.' }
    $timer = [Diagnostics.Stopwatch]::StartNew()
    try {
        do {
            $state = Get-OwnedBuild
            if (-not $state.State.Running) { break }
            if ($timer.Elapsed.TotalSeconds -ge $BuildTimeoutSeconds) { throw 'Cross-compilation exceeded bounded deadline.' }
            Start-Sleep -Milliseconds 500
        } while ($true)
    } finally {
        $state = Get-OwnedBuild
        if ($state.State.Running) {
            & $docker kill $containerId | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'Own build-container stop failed.' }
        }
        $logs = & $docker logs $containerId 2>&1
        [IO.File]::WriteAllText((Join-Path $scratch 'cross-compile.log'), ($logs -join "`n"))
        $evidence['buildContainerState'] = (Get-OwnedBuild).State
        Save-Evidence
    }
    if ($state.State.ExitCode -ne 0) { throw 'Windows cross-compilation failed; see cross-compile.log.' }
    $compiledInputs = @(Get-Content -LiteralPath (Join-Path $scratch 'build-sources.txt') |
        Where-Object { $_.StartsWith('/src/') } | Sort-Object -Unique)
    if ($compiledInputs.Count -eq 0) { throw 'Actual compiled-source manifest is empty.' }
    $relativeInputs = @(foreach ($inputPath in $compiledInputs) {
        if ($inputPath -notmatch '^/src/[a-zA-Z0-9_./-]+$' -or $inputPath.Split('/') -contains '..') {
            throw "Invalid compiled-source path: $inputPath"
        }
        'backend/' + $inputPath.Substring(5)
    })
    $evidence['sourceDirectories'] = @($relativeInputs | Where-Object { $_.EndsWith('.go') } |
        ForEach-Object { [IO.Path]::GetDirectoryName($_).Replace('\', '/') } | Sort-Object -Unique)
    $evidence['sourceEmbeddedPaths'] = @($relativeInputs | Where-Object { -not $_.EndsWith('.go') })
    # Include every Go file in actual dependency directories, not only files
    # named in the manifest, so added/deleted files invalidate source identity.
    $scoped = Get-SourceHashes
    $baseline = [ordered]@{}
    foreach ($key in $scoped.Keys) {
        if (-not $evidence.sourceSha256.Contains($key)) { throw "Build input was not present in initial snapshot: $key" }
        $baseline[$key] = $evidence.sourceSha256[$key]
    }
    $evidence['excludedUnrelatedSourceCount'] = $evidence.sourceSha256.Count - $baseline.Count
    $evidence.sourceSha256 = $baseline
    $evidence['buildSourceManifestSha256'] = (Get-FileHash -LiteralPath (Join-Path $scratch 'build-sources.txt') -Algorithm SHA256).Hash
    Assert-SourceHashes
    Add-Type -TypeDefinition $watchdogSource
    foreach ($suite in $suites) {
        $binary = Join-Path $scratch $suite.Binary
        $evidence.binarySha256[$suite.Binary] = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash
        $record = Invoke-Fixture $binary '-test.list=^Test' "$($suite.Binary)-list.log"
        if ($record.supervision.ExitCode -ne 0 -or $record.supervision.WatchdogApplied) { throw 'Test listing failed.' }
        $listing = [IO.File]::ReadAllText((Join-Path $scratch $record.log))
        foreach ($testName in $suite.Names) {
            if ($listing -notmatch "(?m)^$([regex]::Escape($testName))\r?$") { throw "Named test absent: $testName" }
        }
    }
    # Negative control: the helper deliberately leaves a native root and child
    # running. The independent watchdog must kill and verify exactly that tree.
    $record = Invoke-Fixture (Join-Path $scratch 'bridge.test.exe') '-test.run=^TestWindowsProcessTreeFixture$ -test.timeout=30s' 'watchdog-negative-control.log' 1500 @{
        HAI_DSH_TREE_FIXTURE_MODE = 'root'; HAI_DSH_TREE_FIXTURE_MARKER = (Join-Path $scratch 'watchdog-fixture-pids.txt')
    }
    if (-not $record.supervision.TimedOut -or -not $record.supervision.WatchdogApplied -or
        $record.supervision.ActiveProcesses -ne 0 -or $record.supervision.TotalProcesses -lt 2) {
        throw 'Watchdog negative control did not verify root and descendant cleanup.'
    }
    $fixturePids = @(Get-Content -LiteralPath (Join-Path $scratch 'watchdog-fixture-pids.txt') | ForEach-Object { [uint32]::Parse($_) })
    if ($fixturePids.Count -ne 2) { throw 'Negative control did not create its root and child fixture.' }
    foreach ($fixturePid in $fixturePids) {
        if ($record.supervision.ObservedPids -notcontains $fixturePid) { throw 'Negative-control fixture PID was not observed in own job.' }
    }
    for ($iteration = 1; $iteration -le $Repetitions; $iteration++) {
        foreach ($suite in $suites) {
            Assert-SourceHashes
            $binary = Join-Path $scratch $suite.Binary
            if ((Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash -ne $evidence.binarySha256[$suite.Binary]) { throw 'Own test binary changed.' }
            $pattern = '^(' + ($suite.Names -join '|') + ')$'
            $arguments = "-test.run=$pattern -test.v -test.count=1 -test.parallel=1 -test.timeout=60s"
            $evidence.nativeExecuted = $true
            $record = Invoke-Fixture $binary $arguments "$($suite.Binary)-run-$iteration.log"
            $output = [IO.File]::ReadAllText((Join-Path $scratch $record.log))
            $record['passedChecks'] = @([regex]::Matches($output, '(?m)^--- PASS: (\S+) \(') | ForEach-Object { $_.Groups[1].Value })
            $record['skippedChecks'] = @([regex]::Matches($output, '(?m)^\s*--- SKIP: (\S+)') | ForEach-Object { $_.Groups[1].Value })
            Save-Evidence
            Write-Host $output
            if ($record.supervision.ExitCode -ne 0 -or $record.supervision.WatchdogApplied -or
                $record.supervision.ActiveProcesses -ne 0 -or $record.supervision.TotalProcesses -lt $suite.MinimumProcesses -or
                $record.skippedChecks.Count -ne 0 -or $output -match '--- FAIL:') {
                throw "Native suite failed, skipped, lacked actual child creation, or needed watchdog cleanup: $($suite.Package)"
            }
            foreach ($testName in $suite.Names) {
                if (@($record.passedChecks | Where-Object { $_ -ceq $testName }).Count -ne 1) { throw "Expected one named PASS: $testName" }
            }
            if ($record.passedChecks.Count -ne $suite.Names.Count) { throw 'Unexpected test selection; refuse acceptance.' }
        }
    }
    Assert-SourceHashes
    $evidence.nativePassed = $true
    $evidence['completedUtc'] = [DateTime]::UtcNow.ToString('o')
    Save-Evidence
    Write-Host "NATIVE WINDOWS PROCESS CONTAINMENT PASSED: nine CI checks x $Repetitions, zero skips. Evidence: $evidencePath"
} catch {
    $evidence['failure'] = $_.Exception.Message
    Save-Evidence
    throw
} finally {
    Write-Host "All own evidence retained: $scratch"
}
