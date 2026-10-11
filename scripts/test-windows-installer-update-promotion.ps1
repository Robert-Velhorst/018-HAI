[CmdletBinding()]
param([switch]$FocusedIntegrity)

$ErrorActionPreference = 'Stop'
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$supportSource = Join-Path $repositoryRoot 'installer\windows\Hai-InstallerSupport.ps1'
$payloadValidatorSource = Join-Path $repositoryRoot 'installer\windows\Hai-WindowsExecutable.ps1'
$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ('hai-worker-promotion-' + [Guid]::NewGuid().ToString('N'))
$installRoot = Join-Path $temporaryRoot 'install'
$windowsRoot = Join-Path $installRoot 'installer\windows'
$supportCopy = Join-Path $windowsRoot 'Hai-InstallerSupport.ps1'
$probePath = Join-Path $temporaryRoot 'promotion-budget-probe.ps1'
$readyPath = Join-Path $temporaryRoot 'promotion-budget-ready'
$workerPath = Join-Path $windowsRoot 'hai-openclaw-maintenance.exe'
$pendingPath = Join-Path $windowsRoot 'hai-openclaw-maintenance.pending.exe'
$mutexName = 'Local\HAI.PromotionBudget.Test.' + [Guid]::NewGuid().ToString('N')
$mutex = $null
$ownsMutex = $false
$child = $null

function Assert-HaiPromotionTest([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

New-Item -ItemType Directory -Path $windowsRoot -Force | Out-Null
Copy-Item -LiteralPath $supportSource -Destination $supportCopy
Copy-Item -LiteralPath $payloadValidatorSource -Destination (Join-Path $windowsRoot 'Hai-WindowsExecutable.ps1')
$oldWorkerSource = Join-Path $env:WINDIR 'System32\cmd.exe'
$newWorkerSource = (Get-Process -Id $PID).Path
Copy-Item -LiteralPath $oldWorkerSource -Destination $workerPath
Copy-Item -LiteralPath $newWorkerSource -Destination $pendingPath
$oldWorkerHash = (Get-FileHash -LiteralPath $workerPath -Algorithm SHA256).Hash
$newWorkerHash = (Get-FileHash -LiteralPath $pendingPath -Algorithm SHA256).Hash
Assert-HaiPromotionTest ($oldWorkerHash -cne $newWorkerHash) 'The promotion fixture requires distinct valid worker payloads.'

$probe = @'
param([string]$SupportPath, [string]$InstallRoot, [string]$MutexName, [string]$WorkerPath, [string]$ReadyPath)
$ErrorActionPreference = 'Stop'
. $SupportPath
$script:HaiOpenClawMaintenanceMutexName = $MutexName
$script:FixtureInstallRoot = $InstallRoot
function Get-HaiInstallRoot { return $script:FixtureInstallRoot }
function Get-CimInstance {
    [CmdletBinding()]
    param([string]$ClassName, [string]$Filter)
    if ($ClassName -ne 'Win32_Process') { throw "Unexpected process query: $ClassName" }
    return @([pscustomobject]@{ ExecutablePath = $WorkerPath })
}
[IO.File]::WriteAllText($ReadyPath, 'ready')
$timer = [Diagnostics.Stopwatch]::StartNew()
$failure = ''
try {
    [void](Promote-HaiMaintenanceWorker -WaitSeconds 4)
} catch {
    $failure = $_.Exception.Message
}
$timer.Stop()
if ($failure -notmatch 'could not be promoted within 4 seconds') { exit 21 }
if ($timer.Elapsed.TotalSeconds -gt 5.5) { exit 22 }
exit 0
'@
[IO.File]::WriteAllText($probePath, $probe)

try {
    if (-not $FocusedIntegrity) {
    $mutex = New-Object Threading.Mutex($false, $mutexName)
    $ownsMutex = $mutex.WaitOne(0)
    Assert-HaiPromotionTest $ownsMutex 'Could not acquire the isolated installer/worker mutex fixture.'

    $powerShellPath = (Get-Process -Id $PID).Path
    $arguments = '-NoLogo -NoProfile -File "{0}" "{1}" "{2}" "{3}" "{4}" "{5}"' -f `
        $probePath, $supportCopy, $installRoot, $mutexName, $workerPath, $readyPath
    $child = Start-Process -FilePath $powerShellPath -ArgumentList $arguments -WindowStyle Hidden -PassThru

    $readyDeadline = [DateTimeOffset]::UtcNow.AddSeconds(10)
    while (-not (Test-Path -LiteralPath $readyPath -PathType Leaf) -and -not $child.HasExited -and [DateTimeOffset]::UtcNow -lt $readyDeadline) {
        Start-Sleep -Milliseconds 50
    }
    Assert-HaiPromotionTest (Test-Path -LiteralPath $readyPath -PathType Leaf) 'The isolated promotion probe did not start.'
    Start-Sleep -Milliseconds 2200

    $mutex.ReleaseMutex()
    $ownsMutex = $false
    if (-not $child.WaitForExit(10000)) {
        $child.Kill()
        $child.WaitForExit(5000)
        throw 'The isolated promotion probe exceeded its bounded wait.'
    }
    Assert-HaiPromotionTest ($child.ExitCode -eq 0) "Promotion did not enforce one total timeout budget (probe exit $($child.ExitCode))."
    Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $workerPath -Algorithm SHA256).Hash -ceq $oldWorkerHash) 'A timed-out promotion modified the installed worker.'
    Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $pendingPath -Algorithm SHA256).Hash -ceq $newWorkerHash) 'A timed-out promotion consumed the staged worker.'
    }

    . $supportCopy
    $script:HaiOpenClawMaintenanceMutexName = $mutexName
    $script:FixtureInstallRoot = $installRoot
    function Get-HaiInstallRoot { return $script:FixtureInstallRoot }
    function Get-CimInstance {
        [CmdletBinding()]
        param([string]$ClassName, [string]$Filter)
        if ($ClassName -ne 'Win32_Process') { throw "Unexpected process query: $ClassName" }
        if ($script:PromotionReplacementSource) {
            Copy-Item -LiteralPath $script:PromotionReplacementSource -Destination $pendingPath -Force
            $script:PromotionReplacementSource = $null
        }
        return @()
    }
    $script:PromotionReplacementSource = $oldWorkerSource
    $changedPayloadRejected = $false
    try { [void](Promote-HaiMaintenanceWorker -WaitSeconds 4) } catch {
        $changedPayloadRejected = $_.Exception.Message -match 'staged.*changed'
    }
    Assert-HaiPromotionTest $changedPayloadRejected 'Promotion accepted different valid bytes substituted after initial validation.'
    Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $workerPath -Algorithm SHA256).Hash -ceq $oldWorkerHash) 'A changed staged payload modified the installed worker.'
    Assert-HaiPromotionTest (Test-Path -LiteralPath $pendingPath -PathType Leaf) 'A rejected changed payload was consumed.'
    Copy-Item -LiteralPath $newWorkerSource -Destination $pendingPath -Force

    function Get-Item {
        [CmdletBinding()]
        param([string]$LiteralPath, [switch]$Force)
        if ($LiteralPath -ceq $workerPath -and $null -ne $script:PromotionUnsafeDestination) {
            return $script:PromotionUnsafeDestination
        }
        return Microsoft.PowerShell.Management\Get-Item -LiteralPath $LiteralPath -Force:$Force
    }
    foreach ($destination in @(
        [pscustomobject]@{ PSIsContainer = $true; Attributes = [IO.FileAttributes]::Directory },
        [pscustomobject]@{ PSIsContainer = $false; Attributes = [IO.FileAttributes]::ReparsePoint }
    )) {
        $script:PromotionUnsafeDestination = $destination
        $destinationRejected = $false
        try { [void](Promote-HaiMaintenanceWorker -WaitSeconds 4) } catch {
            $destinationRejected = $_.Exception.Message -match 'destination is not a regular file'
        }
        Assert-HaiPromotionTest $destinationRejected 'Promotion did not reject a non-regular worker destination.'
        Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $workerPath -Algorithm SHA256).Hash -ceq $oldWorkerHash) 'A rejected destination changed the installed worker.'
        Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $pendingPath -Algorithm SHA256).Hash -ceq $newWorkerHash) 'A rejected destination consumed the staged payload.'
    }
    $script:PromotionUnsafeDestination = $null

    $userDataPath = Join-Path $temporaryRoot 'user-data.canary'
    [IO.File]::WriteAllText($userDataPath, 'preserve-user-data')
    $userDataHash = (Get-FileHash -LiteralPath $userDataPath -Algorithm SHA256).Hash
    $heldWorker = [IO.File]::Open($workerPath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        $lockedFailure = ''
        try { [void](Promote-HaiMaintenanceWorker -WaitSeconds 1) } catch { $lockedFailure = $_.Exception.Message }
        Assert-HaiPromotionTest ($lockedFailure -match 'could not be promoted within 1 seconds') 'A file-locked worker replacement did not stop within its timeout.'
        Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $workerPath -Algorithm SHA256).Hash -ceq $oldWorkerHash) 'Failed worker replacement changed the installed worker.'
        Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $pendingPath -Algorithm SHA256).Hash -ceq $newWorkerHash) 'Failed worker replacement consumed the staged payload.'
        Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $userDataPath -Algorithm SHA256).Hash -ceq $userDataHash) 'Failed promotion changed user data.'
    } finally { $heldWorker.Dispose() }
    Assert-HaiPromotionTest ([bool](Promote-HaiMaintenanceWorker -WaitSeconds 4)) 'Promotion did not succeed after fixture contention cleared.'
    Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $workerPath -Algorithm SHA256).Hash -ceq $newWorkerHash) 'Successful retry did not install the staged worker.'
    Assert-HaiPromotionTest (-not (Test-Path -LiteralPath $pendingPath)) 'Successful promotion left the pending worker behind.'

    $invalidPendingPath = Join-Path $windowsRoot 'hai-openclaw-maintenance.pending.exe'
    [IO.File]::WriteAllText($invalidPendingPath, 'not an executable')
    $installedHashBeforeInvalidPromotion = (Get-FileHash -LiteralPath $workerPath -Algorithm SHA256).Hash
    $invalidRejected = $false
    try { [void](Promote-HaiMaintenanceWorker -WaitSeconds 4) } catch { $invalidRejected = $_.Exception.Message -match 'could not be verified as a valid 64-bit Windows executable' }
    Assert-HaiPromotionTest $invalidRejected 'Promotion accepted a malformed staged worker payload.'
    Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $workerPath -Algorithm SHA256).Hash -ceq $installedHashBeforeInvalidPromotion) 'A malformed staged worker modified the installed worker.'
    Assert-HaiPromotionTest ((Get-Content -LiteralPath $invalidPendingPath -Raw) -ceq 'not an executable') 'A malformed staged worker was consumed.'
    Assert-HaiPromotionTest ((Get-FileHash -LiteralPath $userDataPath -Algorithm SHA256).Hash -ceq $userDataHash) 'Successful promotion changed user data.'
    Write-Output 'Windows installer worker-promotion integrity, file-lock timeout, recovery and user-data fixtures passed.'
} finally {
    if ($null -ne $child) { $child.Dispose() }
    if ($ownsMutex -and $null -ne $mutex) { $mutex.ReleaseMutex() }
    if ($null -ne $mutex) { $mutex.Dispose() }
    $resolvedTemporaryRoot = [IO.Path]::GetFullPath($temporaryRoot)
    $resolvedTempParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if ($resolvedTemporaryRoot.StartsWith($resolvedTempParent, [StringComparison]::OrdinalIgnoreCase) -and
        [IO.Path]::GetFileName($resolvedTemporaryRoot) -match '\Ahai-worker-promotion-[a-f0-9]{32}\z' -and
        (Test-Path -LiteralPath $resolvedTemporaryRoot -PathType Container)) {
        Remove-Item -LiteralPath $resolvedTemporaryRoot -Recurse -Force
    }
}
