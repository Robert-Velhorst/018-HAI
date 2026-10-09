[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$initializerPath = Join-Path $PSScriptRoot 'initialize-windows.ps1'
$supportPath = Join-Path $repositoryRoot 'installer\windows\Hai-InstallerSupport.ps1'
$startPath = Join-Path $repositoryRoot 'installer\windows\Start-HAI.ps1'
$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ('hai-openclaw-migration-test-' + [Guid]::NewGuid().ToString('N'))
$utf8 = New-Object Text.UTF8Encoding($false, $true)
$script:fixtureEnvironmentFile = ''
$script:fixtureInstallRoot = $repositoryRoot

foreach ($path in @($initializerPath, $supportPath, $startPath)) {
    $tokens = $null
    $errors = $null
    [Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors) | Out-Null
    if ($errors.Count -gt 0) {
        throw "PowerShell syntax validation failed for a scoped HAI migration file."
    }
}

function Assert-HaiTest {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) {
        throw $Message
    }
}

function Invoke-HaiTestPowerShell {
    param(
        [Parameter(Mandatory = $true)][string]$Executable,
        [Parameter(Mandatory = $true)][string]$ScriptPath,
        [Parameter(Mandatory = $true)][string]$Arguments
    )

    $runId = [Guid]::NewGuid().ToString('N')
    $stdoutPath = Join-Path $temporaryRoot ("child-$runId.stdout")
    $stderrPath = Join-Path $temporaryRoot ("child-$runId.stderr")
    $argumentLine = '-NoProfile -ExecutionPolicy Bypass -File "{0}" {1}' -f $ScriptPath.Replace('"', '""'), $Arguments
    $process = Start-Process -FilePath $Executable -ArgumentList $argumentLine -WindowStyle Hidden -Wait -PassThru `
        -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
    $output = @()
    if (Test-Path -LiteralPath $stdoutPath -PathType Leaf) { $output += [IO.File]::ReadAllText($stdoutPath) }
    if (Test-Path -LiteralPath $stderrPath -PathType Leaf) { $output += [IO.File]::ReadAllText($stderrPath) }
    return [pscustomobject]@{ ExitCode = $process.ExitCode; Output = $output -join "`n" }
}

function New-HaiFixture {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Content,
        [switch]$Utf8Bom
    )

    $path = Join-Path $temporaryRoot ($Name + '.env')
    $bytes = $utf8.GetBytes($Content)
    if ($Utf8Bom) {
        $withBom = New-Object byte[] ($bytes.Length + 3)
        $withBom[0] = 0xEF
        $withBom[1] = 0xBB
        $withBom[2] = 0xBF
        [Array]::Copy($bytes, 0, $withBom, 3, $bytes.Length)
        $bytes = $withBom
    }
    [IO.File]::WriteAllBytes($path, $bytes)
    return $path
}

function Invoke-HaiUpgradeFixture {
    param([Parameter(Mandatory = $true)][string]$Path)

    $script:fixtureEnvironmentFile = $Path
    try {
        $messages = @(& { Initialize-HaiLocalEnvironment -GatewayPort 8088 } 6>&1)
        $output = ($messages | ForEach-Object {
            if ($_ -is [Management.Automation.InformationRecord]) { [string]$_.MessageData }
            else { [string]$_ }
        }) -join "`n"
        return [pscustomobject]@{ Succeeded = $true; Output = $output }
    } catch {
        return [pscustomobject]@{ Succeeded = $false; Output = $_.Exception.Message }
    }
}

function Get-HaiFixtureValue {
    param([string]$Content, [string]$Name)
    $pattern = '(?m)^' + [Regex]::Escape($Name) + '=(?<value>[^\r\n]*)'
    $matches = [Regex]::Matches($Content, $pattern)
    if ($matches.Count -ne 1) { return $null }
    return $matches[0].Groups['value'].Value
}

function Get-HaiTestMigrationMutexName {
    param([Parameter(Mandatory = $true)][string]$Path)

    $canonicalPath = [IO.Path]::GetFullPath($Path).TrimEnd([IO.Path]::DirectorySeparatorChar).ToUpperInvariant()
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        $digest = $sha256.ComputeHash([Text.Encoding]::UTF8.GetBytes($canonicalPath))
    } finally {
        $sha256.Dispose()
    }
    return 'Global\HAI.EnvironmentMigration.' + [BitConverter]::ToString($digest).Replace('-', '')
}

. $supportPath
function Get-HaiEnvironmentFile { return $script:fixtureEnvironmentFile }
function Get-HaiInstallRoot { return $script:fixtureInstallRoot }

New-Item -ItemType Directory -Path $temporaryRoot | Out-Null
try {
    $protectedFile = Join-Path $temporaryRoot 'prewrite-protected.env'
    $restrictedFileSecurity = New-HaiRestrictedEnvironmentFileSecurity
    $secretBytes = $utf8.GetBytes('TEST_SECRET_VALUE=must-not-exist-before-acl-check')
    $script:prewriteAclObserved = $false
    $beforeSecretWrite = {
        param($path, $stream, $actualSecurity)
        Assert-HaiTest ($stream.Length -eq 0) 'Secret bytes existed before the temporary file ACL was verified.'
        $expectedSddl = Get-HaiComparableFileAccessDescriptor -FileSecurity $restrictedFileSecurity
        $observedSddl = Get-HaiComparableFileAccessDescriptor -FileSecurity $actualSecurity
        Assert-HaiTest ([string]::Equals($expectedSddl, $observedSddl, [StringComparison]::Ordinal)) 'The restrictive ACL was not active before secret bytes were written.'
        $script:prewriteAclObserved = $true
    }
    Write-HaiAclProtectedFile -Path $protectedFile -Bytes $secretBytes -FileSecurity $restrictedFileSecurity -BeforeWrite $beforeSecretWrite
    Assert-HaiTest $script:prewriteAclObserved 'The protected-file writer did not expose its pre-write ACL checkpoint.'
    Assert-HaiTest ([string]::Equals([Convert]::ToBase64String($secretBytes), [Convert]::ToBase64String([IO.File]::ReadAllBytes($protectedFile)), [StringComparison]::Ordinal)) 'The ACL-protected writer did not persist the complete file bytes.'

    $b = 'b' * 64
    $accent = [char]0xE9
    $unconfiguredContent = "# Preserve this comment: cafe$accent`r`nUNRELATED_SETTING=keep exact spacing  `r`nBACKEND_API_SHARED_KEY=$b`r`n"
    $missingPath = New-HaiFixture -Name 'missing-settings' -Content $unconfiguredContent -Utf8Bom
    $originalAcl = Get-HaiComparableFileAccessDescriptor -FileSecurity (Get-Acl -LiteralPath $missingPath)
    $firstRun = Invoke-HaiUpgradeFixture -Path $missingPath
    Assert-HaiTest $firstRun.Succeeded ("The existing-install migration did not complete for a valid fixture: {0}" -f $firstRun.Output)
    Assert-HaiTest (-not [string]::IsNullOrEmpty($firstRun.Output)) 'The migration did not report its non-secret status.'
    $migratedBytes = [IO.File]::ReadAllBytes($missingPath)
    Assert-HaiTest ($migratedBytes.Length -ge 3 -and $migratedBytes[0] -eq 0xEF -and $migratedBytes[1] -eq 0xBB -and $migratedBytes[2] -eq 0xBF) 'The UTF-8 BOM was not preserved.'
    $migratedContent = $utf8.GetString($migratedBytes, 3, $migratedBytes.Length - 3)
    $generatedToken = Get-HaiFixtureValue -Content $migratedContent -Name 'HAI_OPENCLAW_MAINTENANCE_TOKEN'
    $migratedEnabled = Get-HaiFixtureValue -Content $migratedContent -Name 'HAI_OPENCLAW_MAINTENANCE_ENABLED'
    Assert-HaiTest ($migratedEnabled -ceq 'true') ("A missing maintenance setting did not default to enabled (observed: '$migratedEnabled').")
    Assert-HaiTest ($generatedToken -match '\A[a-f0-9]{64}\z') 'The migration did not create a cryptographically random-format token.'
    Assert-HaiTest (-not $firstRun.Output.Contains($generatedToken)) 'The migration printed its generated token.'
    $migratedAcl = Get-HaiComparableFileAccessDescriptor -FileSecurity (Get-Acl -LiteralPath $missingPath)
    Assert-HaiTest ([string]::Equals($originalAcl, $migratedAcl, [StringComparison]::Ordinal)) 'The migration changed the existing environment file DACL.'
    Assert-HaiTest ($migratedContent.Contains("# Preserve this comment: cafe$accent`r`nUNRELATED_SETTING=keep exact spacing  `r`nBACKEND_API_SHARED_KEY=$b`r`n")) 'Unrelated environment text or CRLF line endings changed.'

    $stableBytes = [Convert]::ToBase64String($migratedBytes)
    $repeatRun = Invoke-HaiUpgradeFixture -Path $missingPath
    Assert-HaiTest $repeatRun.Succeeded 'An idempotent second migration failed.'
    Assert-HaiTest ([string]::Equals($stableBytes, [Convert]::ToBase64String([IO.File]::ReadAllBytes($missingPath)), [StringComparison]::Ordinal)) 'A second migration rewrote an already-current environment.'
    Assert-HaiTest (-not $repeatRun.Output.Contains($generatedToken)) 'The second migration printed the existing token.'

    $existingToken = 't' * 48
    $validContent = "HAI_OPENCLAW_MAINTENANCE_ENABLED=`"true`"`nHAI_OPENCLAW_MAINTENANCE_TOKEN=$existingToken`nBACKEND_API_SHARED_KEY=$b`nCUSTOM_VALUE=unchanged`n"
    $validPath = New-HaiFixture -Name 'preserve-valid-token' -Content $validContent
    $validRun = Invoke-HaiUpgradeFixture -Path $validPath
    Assert-HaiTest $validRun.Succeeded 'Migration failed with an already-valid token.'
    Assert-HaiTest ([string]::Equals($validContent, [IO.File]::ReadAllText($validPath), [StringComparison]::Ordinal)) 'A valid token or its surrounding configuration was rewritten.'
    Assert-HaiTest (-not $validRun.Output.Contains($existingToken)) 'The migration printed an existing token.'

    $falseContent = "# Owner opted out`r`nHAI_OPENCLAW_MAINTENANCE_ENABLED=`"false`"`r`nHAI_OPENCLAW_MAINTENANCE_TOKEN=short`r`nCUSTOM_VALUE=unchanged`r`n"
    $falsePath = New-HaiFixture -Name 'explicit-opt-out' -Content $falseContent
    $falseRun = Invoke-HaiUpgradeFixture -Path $falsePath
    Assert-HaiTest $falseRun.Succeeded 'An explicit false opt-out should not block the upgrade.'
    Assert-HaiTest ([string]::Equals($falseContent, [IO.File]::ReadAllText($falsePath), [StringComparison]::Ordinal)) 'The explicit false setting or its token was modified.'

    $invalidContent = "HAI_OPENCLAW_MAINTENANCE_ENABLED=true`nHAI_OPENCLAW_MAINTENANCE_TOKEN=short`nBACKEND_API_SHARED_KEY=$b`nKEEP=this line`n"
    $invalidPath = New-HaiFixture -Name 'replace-invalid-token' -Content $invalidContent
    $invalidRun = Invoke-HaiUpgradeFixture -Path $invalidPath
    Assert-HaiTest $invalidRun.Succeeded 'An invalid token should be repaired when maintenance is enabled.'
    $repairedContent = [IO.File]::ReadAllText($invalidPath)
    $repairedToken = Get-HaiFixtureValue -Content $repairedContent -Name 'HAI_OPENCLAW_MAINTENANCE_TOKEN'
    Assert-HaiTest ($repairedToken -match '\A[a-f0-9]{64}\z') 'An invalid maintenance token was not replaced.'
    Assert-HaiTest ($repairedToken -cne $generatedToken) 'Separate migrations reused the same generated maintenance token.'
    Assert-HaiTest (-not $invalidRun.Output.Contains($repairedToken)) 'The migration printed a replacement token.'
    Assert-HaiTest ($repairedContent.Contains("BACKEND_API_SHARED_KEY=$b`nKEEP=this line`n")) 'An unrelated value changed while replacing an invalid token.'

    $reuseContent = "HAI_OPENCLAW_MAINTENANCE_ENABLED=true`nHAI_OPENCLAW_MAINTENANCE_TOKEN=$b`nBACKEND_API_SHARED_KEY=$b`n"
    $reusePath = New-HaiFixture -Name 'replace-reused-token' -Content $reuseContent
    $reuseRun = Invoke-HaiUpgradeFixture -Path $reusePath
    Assert-HaiTest $reuseRun.Succeeded 'A reused credential should be replaced before worker activation.'
    $reuseRepaired = [IO.File]::ReadAllText($reusePath)
    $reuseToken = Get-HaiFixtureValue -Content $reuseRepaired -Name 'HAI_OPENCLAW_MAINTENANCE_TOKEN'
    Assert-HaiTest ($reuseToken -match '\A[a-f0-9]{64}\z' -and $reuseToken -cne $b) 'A maintenance token reused from the backend credential was retained.'

    foreach ($case in @(
        [pscustomobject]@{ Name = 'invalid-flag'; Content = "HAI_OPENCLAW_MAINTENANCE_ENABLED=maybe`nHAI_OPENCLAW_MAINTENANCE_TOKEN=short`n" },
        [pscustomobject]@{ Name = 'duplicate-flag'; Content = "HAI_OPENCLAW_MAINTENANCE_ENABLED=true`nHAI_OPENCLAW_MAINTENANCE_ENABLED=false`n" },
        [pscustomobject]@{ Name = 'duplicate-token'; Content = "HAI_OPENCLAW_MAINTENANCE_ENABLED=true`nHAI_OPENCLAW_MAINTENANCE_TOKEN=one`nHAI_OPENCLAW_MAINTENANCE_TOKEN=two`n" }
    )) {
        $path = New-HaiFixture -Name $case.Name -Content $case.Content
        $before = [Convert]::ToBase64String([IO.File]::ReadAllBytes($path))
        $failedRun = Invoke-HaiUpgradeFixture -Path $path
        Assert-HaiTest (-not $failedRun.Succeeded) "The $($case.Name) fixture should fail closed."
        Assert-HaiTest ([string]::Equals($before, [Convert]::ToBase64String([IO.File]::ReadAllBytes($path)), [StringComparison]::Ordinal)) "The $($case.Name) fixture was modified despite ambiguity."
    }

    $concurrentPath = New-HaiFixture -Name 'concurrent-migration' -Content "HAI_OPENCLAW_MAINTENANCE_ENABLED=true`nHAI_OPENCLAW_MAINTENANCE_TOKEN=short`nBACKEND_API_SHARED_KEY=$b`n"
    $mutexName = Get-HaiTestMigrationMutexName -Path $concurrentPath
    $testMutex = New-Object Threading.Mutex($false, $mutexName)
    $ownsTestMutex = $false
    $jobs = @()
    $readyDirectory = $temporaryRoot
    try {
        $ownsTestMutex = $testMutex.WaitOne(0)
        Assert-HaiTest $ownsTestMutex 'The concurrency fixture could not acquire the migration lock.'
        $worker = {
            param($Initializer, $EnvironmentFile, $ReadyDirectory)
            # Separate marker files avoid concurrent AppendAllText sharing races.
            [IO.File]::WriteAllText((Join-Path $ReadyDirectory ("concurrent-worker-$PID.ready")), [string]$PID)
            & $Initializer -EnvFile $EnvironmentFile -MigrateExisting
        }
        $jobs = @(
            Start-Job -ScriptBlock $worker -ArgumentList $initializerPath, $concurrentPath, $readyDirectory
            Start-Job -ScriptBlock $worker -ArgumentList $initializerPath, $concurrentPath, $readyDirectory
        )

        $readyDeadline = [DateTimeOffset]::UtcNow.AddSeconds(20)
        do {
            $readyCount = @(Get-ChildItem -LiteralPath $readyDirectory -Filter 'concurrent-worker-*.ready' -File -ErrorAction SilentlyContinue).Count
            if ($readyCount -ge 2) { break }
            $finishedJobs = @($jobs | Where-Object { $_.State -in @('Failed', 'Stopped', 'Completed') })
            if ($finishedJobs.Count -gt 0) {
                $workerStates = @($finishedJobs | ForEach-Object {
                    $reasonType = if ($null -ne $_.JobStateInfo.Reason) { $_.JobStateInfo.Reason.GetType().Name } else { 'none' }
                    "job $($_.Id): $($_.State), reason type $reasonType"
                }) -join '; '
                throw "A concurrent migration worker exited before both workers reached the lock test ($workerStates)."
            }
            Start-Sleep -Milliseconds 100
        } while ([DateTimeOffset]::UtcNow -lt $readyDeadline)
        Assert-HaiTest ($readyCount -ge 2) 'Concurrent migration workers did not reach the lock test.'
        Start-Sleep -Milliseconds 300
        Assert-HaiTest (@($jobs | Where-Object { $_.State -eq 'Completed' }).Count -eq 0) 'A startup migration completed while another process held the path lock.'

        $testMutex.ReleaseMutex()
        $ownsTestMutex = $false
        $null = Wait-Job -Job $jobs -Timeout 45
        Assert-HaiTest (@($jobs | Where-Object { $_.State -ne 'Completed' }).Count -eq 0) 'Concurrent startup migrations did not both finish within the bounded test window.'
        $workerOutput = @(Receive-Job -Job $jobs 2>&1 | ForEach-Object { [string]$_ }) -join "`n"
        Assert-HaiTest (-not $workerOutput.Contains($generatedToken)) 'Concurrent migration output exposed a maintenance token.'
        $concurrentContent = [IO.File]::ReadAllText($concurrentPath)
        $concurrentToken = Get-HaiFixtureValue -Content $concurrentContent -Name 'HAI_OPENCLAW_MAINTENANCE_TOKEN'
        Assert-HaiTest ($concurrentToken -match '\A[a-f0-9]{64}\z') 'Concurrent migrations did not leave one complete valid token.'
        Assert-HaiTest (@(Get-ChildItem -LiteralPath $temporaryRoot -Filter '.hai-maintenance-*' -File).Count -eq 0) 'Concurrent migrations left a temporary config or backup file behind.'
    } finally {
        if ($ownsTestMutex) { $testMutex.ReleaseMutex() }
        if ($jobs.Count -gt 0) {
            $jobs | Where-Object { $_.State -in @('Running', 'NotStarted') } | Stop-Job -ErrorAction SilentlyContinue
            $jobs | Remove-Job -Force -ErrorAction SilentlyContinue
        }
        $testMutex.Dispose()
    }

    $silentInstallRoot = Join-Path $temporaryRoot 'silent-install'
    $silentWindows = Join-Path $silentInstallRoot 'installer\windows'
    $silentScripts = Join-Path $silentInstallRoot 'scripts'
    $silentProfile = Join-Path $temporaryRoot 'silent-profile'
    $silentEvents = Join-Path $temporaryRoot 'silent-upgrade-events.txt'
    New-Item -ItemType Directory -Path $silentWindows, $silentScripts, (Join-Path $silentProfile 'HAI') -Force | Out-Null
    $silentSupport = Join-Path $silentWindows 'Hai-InstallerSupport.ps1'
    $offlineSilentMutexName = 'Local\HAI.OpenClawMaintenance.SilentTest.' + [Guid]::NewGuid().ToString('N')
    $silentSupportText = [IO.File]::ReadAllText($supportPath).Replace(
        "`$script:HaiOpenClawMaintenanceMutexName = 'Global\HAI.OpenClawMaintenance'",
        "`$script:HaiOpenClawMaintenanceMutexName = '$offlineSilentMutexName'")
    [IO.File]::WriteAllText($silentSupport, $silentSupportText, $utf8)
    $silentInitializer = Join-Path $silentScripts 'initialize-windows.ps1'
    [IO.File]::WriteAllText($silentInitializer, @'
param([string]$EnvFile, [switch]$MigrateExisting, [int]$GatewayPort)
if (-not $MigrateExisting -or -not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) { throw 'Expected additive migration of existing settings.' }
[IO.File]::AppendAllText($env:HAI_SILENT_UPGRADE_EVENTS, "migration`n")
'@, $utf8)
    $silentMaintenance = Join-Path $silentWindows 'Hai-OpenClawMaintenance.ps1'
    [IO.File]::WriteAllText($silentMaintenance, @'
function Register-HaiOpenClawMaintenanceTask {
    param([string]$EnvFile, [switch]$SkipImmediateRun)
    if (-not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) { throw 'Expected migrated environment.' }
    if (-not $SkipImmediateRun) { throw 'Silent installer task registration must defer its immediate run.' }
    $mutex = New-Object Threading.Mutex($false, $script:HaiOpenClawMaintenanceMutexName)
    $locked = $false
    try {
        $locked = $mutex.WaitOne(0)
        if (-not $locked) { throw 'Task registration could not acquire the isolated maintenance lock.' }
        [IO.File]::AppendAllText($env:HAI_SILENT_UPGRADE_EVENTS, "task-registration`n")
    } finally {
        if ($locked) { $mutex.ReleaseMutex() }
        $mutex.Dispose()
    }
    return $true
}
'@, $utf8)
    [IO.File]::WriteAllText((Join-Path $silentProfile 'HAI\hai.env'), "HAI_OPENCLAW_MAINTENANCE_ENABLED=true`n", $utf8)
    $powershellPath = (Get-Process -Id $PID).Path
    $priorLocalAppData = $env:LOCALAPPDATA
    $priorSilentEvents = $env:HAI_SILENT_UPGRADE_EVENTS
    try {
        $env:LOCALAPPDATA = $silentProfile
        $env:HAI_SILENT_UPGRADE_EVENTS = $silentEvents
        $bypassResult = Invoke-HaiTestPowerShell -Executable $powershellPath -ScriptPath $silentSupport -Arguments '-ConfigureSilentUpgrade -InstallerOwnsMaintenanceLock'
        Assert-HaiTest ($bypassResult.ExitCode -ne 0) 'The removed installer-lock bypass switch remained accepted.'
        Assert-HaiTest (-not (Test-Path -LiteralPath $silentEvents)) 'An unsupported lock-bypass option changed fixture state.'
        $silentResult = Invoke-HaiTestPowerShell -Executable $powershellPath -ScriptPath $silentSupport -Arguments '-ConfigureSilentUpgrade'
        Assert-HaiTest ($silentResult.ExitCode -eq 0) ("The silent-upgrade configuration path failed: {0}" -f $silentResult.Output)
        $silentEventLines = [IO.File]::ReadAllLines($silentEvents)
        Assert-HaiTest ($silentEventLines.Count -eq 2 -and $silentEventLines[0] -ceq 'migration' -and $silentEventLines[1] -ceq 'task-registration') 'Silent upgrade must complete additive environment migration before task registration.'
        $silentSource = [IO.File]::ReadAllText($silentSupport)
        $silentMarker = $silentSource.IndexOf('if ($ConfigureSilentUpgrade)', [StringComparison]::Ordinal)
        $silentBody = $silentSource.Substring($silentMarker)
        Assert-HaiTest ($silentBody -notmatch '(?im)^\s*(?:&\s*docker|Start-HAI|Start-Process)\b') 'Silent-upgrade configuration must not launch HAI, Docker, or an interactive process.'

        $freshProfile = Join-Path $temporaryRoot 'fresh-silent-profile'
        $env:LOCALAPPDATA = $freshProfile
        Remove-Item -LiteralPath $silentEvents -Force
        $freshResult = Invoke-HaiTestPowerShell -Executable $powershellPath -ScriptPath $silentSupport -Arguments '-ConfigureSilentUpgrade'
        Assert-HaiTest ($freshResult.ExitCode -eq 0) ("A fresh silent install should defer first-run setup: {0}" -f $freshResult.Output)
        Assert-HaiTest (-not (Test-Path -LiteralPath $silentEvents)) 'A fresh silent install must not migrate or register maintenance before interactive first-run setup.'
    } finally {
        $env:LOCALAPPDATA = $priorLocalAppData
        if ($null -eq $priorSilentEvents) { Remove-Item Env:HAI_SILENT_UPGRADE_EVENTS -ErrorAction SilentlyContinue }
        else { $env:HAI_SILENT_UPGRADE_EVENTS = $priorSilentEvents }
    }

    $promotionRoot = Join-Path $temporaryRoot 'promotion-install'
    $promotionWindows = Join-Path $promotionRoot 'installer\windows'
    New-Item -ItemType Directory -Path $promotionWindows -Force | Out-Null
    $promotionSupport = Join-Path $promotionWindows 'Hai-InstallerSupport.ps1'
    $promotionValidator = Join-Path $promotionWindows 'Hai-WindowsExecutable.ps1'
    $offlinePromotionMutexName = 'Local\HAI.OpenClawMaintenance.MigrationTest.' + [Guid]::NewGuid().ToString('N')
    $promotionSupportText = [IO.File]::ReadAllText($supportPath).Replace(
        "`$script:HaiOpenClawMaintenanceMutexName = 'Global\HAI.OpenClawMaintenance'",
        "`$script:HaiOpenClawMaintenanceMutexName = '$offlinePromotionMutexName'")
    [IO.File]::WriteAllText($promotionSupport, $promotionSupportText, $utf8)
    Copy-Item -LiteralPath (Join-Path $repositoryRoot 'installer\windows\Hai-WindowsExecutable.ps1') -Destination $promotionValidator
    $promotionWorker = Join-Path $promotionWindows 'hai-openclaw-maintenance.exe'
    $pendingWorker = Join-Path $promotionWindows 'hai-openclaw-maintenance.pending.exe'
    $oldPromotionSource = Join-Path $env:WINDIR 'System32\cmd.exe'
    $newPromotionSource = (Get-Process -Id $PID).Path
    Copy-Item -LiteralPath $oldPromotionSource -Destination $promotionWorker
    Copy-Item -LiteralPath $newPromotionSource -Destination $pendingWorker
    $oldPromotionHash = (Get-FileHash -LiteralPath $promotionWorker -Algorithm SHA256).Hash
    $newPromotionHash = (Get-FileHash -LiteralPath $pendingWorker -Algorithm SHA256).Hash
    Assert-HaiTest ($oldPromotionHash -cne $newPromotionHash) 'The promotion fixture requires distinct valid worker payloads.'
    $promotionMutex = New-Object Threading.Mutex($false, $offlinePromotionMutexName)
    $ownsPromotionMutex = $false
    try {
        $ownsPromotionMutex = $promotionMutex.WaitOne(0)
        Assert-HaiTest $ownsPromotionMutex 'The installer/worker mutex fixture could not acquire the shared lock.'
        $powershellPath = (Get-Process -Id $PID).Path

        $blockedResult = Invoke-HaiTestPowerShell -Executable $powershellPath -ScriptPath $promotionSupport -Arguments '-PromoteMaintenanceWorker -MaintenanceWorkerWaitSeconds 1'
        Assert-HaiTest ($blockedResult.ExitCode -ne 0) 'Worker promotion proceeded while another process owned the shared installer/worker mutex.'
        Assert-HaiTest ((Get-FileHash -LiteralPath $promotionWorker -Algorithm SHA256).Hash -ceq $oldPromotionHash) 'A mutex-blocked promotion modified the installed worker.'
        Assert-HaiTest ((Get-FileHash -LiteralPath $pendingWorker -Algorithm SHA256).Hash -ceq $newPromotionHash) 'A mutex-blocked promotion consumed the staged worker.'

        $script:fixtureInstallRoot = $promotionRoot
        $script:fixtureMaintenanceProcesses = @()
        function Get-CimInstance {
            param([string]$ClassName, [string]$Filter, [string]$ErrorAction)
            if ($ClassName -ne 'Win32_Process') { throw 'Unexpected system query in the offline process fixture.' }
            return @($script:fixtureMaintenanceProcesses)
        }

        $promotionMutex.ReleaseMutex()
        $ownsPromotionMutex = $false
        $ownedLockResult = $true
        try { [void](Promote-HaiMaintenanceWorker -WaitSeconds 1) } catch { $ownedLockResult = $_.Exception.Message }
        Assert-HaiTest ($ownedLockResult -is [bool] -and $ownedLockResult) ("Worker promotion could not acquire its own isolated lock: {0}" -f $ownedLockResult)
        Assert-HaiTest ((Get-FileHash -LiteralPath $promotionWorker -Algorithm SHA256).Hash -ceq $newPromotionHash) 'The installer-owned promotion did not atomically replace the old worker.'
        Assert-HaiTest (-not (Test-Path -LiteralPath $pendingWorker)) 'Successful worker promotion left the pending executable behind.'

        Copy-Item -LiteralPath $oldPromotionSource -Destination $promotionWorker -Force
        Copy-Item -LiteralPath $newPromotionSource -Destination $pendingWorker
        $legacyPromotionHash = (Get-FileHash -LiteralPath $promotionWorker -Algorithm SHA256).Hash
        $replacementPromotionHash = (Get-FileHash -LiteralPath $pendingWorker -Algorithm SHA256).Hash
        $script:fixtureMaintenanceProcesses = @([pscustomobject]@{ ExecutablePath = $promotionWorker })
        $legacyResult = $null
        try { [void](Promote-HaiMaintenanceWorker -WaitSeconds 1) } catch { $legacyResult = $_.Exception.Message }
        Assert-HaiTest (-not [string]::IsNullOrWhiteSpace($legacyResult)) 'Promotion proceeded while an offline fixture reported a running legacy worker.'
        Assert-HaiTest ((Get-FileHash -LiteralPath $promotionWorker -Algorithm SHA256).Hash -ceq $legacyPromotionHash) 'The active legacy worker fixture was changed before it exited.'
        Assert-HaiTest ((Get-FileHash -LiteralPath $pendingWorker -Algorithm SHA256).Hash -ceq $replacementPromotionHash) 'The active legacy worker consumed the staged replacement.'
        $script:fixtureMaintenanceProcesses = @()

        $retryResult = $true
        try { [void](Promote-HaiMaintenanceWorker -WaitSeconds 1) } catch { $retryResult = $_.Exception.Message }
        Assert-HaiTest ($retryResult -is [bool] -and $retryResult) ("Worker promotion did not succeed after the offline legacy-process fixture cleared: {0}" -f $retryResult)
        Assert-HaiTest ((Get-FileHash -LiteralPath $promotionWorker -Algorithm SHA256).Hash -ceq $replacementPromotionHash) 'The retry did not install the staged worker after the legacy process exited.'
        Assert-HaiTest (-not (Test-Path -LiteralPath $pendingWorker)) 'A successful retry left its pending executable behind.'
    } finally {
        if ($ownsPromotionMutex) { $promotionMutex.ReleaseMutex() }
        $promotionMutex.Dispose()
    }

    Assert-HaiTest (@(Get-ChildItem -LiteralPath $temporaryRoot -Filter '.hai-maintenance-*' -File).Count -eq 0) 'The migration left a temporary config or backup file behind.'

    $supportText = [IO.File]::ReadAllText($supportPath)
    $startText = [IO.File]::ReadAllText($startPath)
    $migrationCall = $supportText.IndexOf('-MigrateExisting', [StringComparison]::Ordinal)
    $startupInitialize = $startText.IndexOf('Initialize-HaiLocalEnvironment', [StringComparison]::Ordinal)
    $startupTaskRegistration = $startText.IndexOf('Register-HaiOpenClawMaintenanceTask', [StringComparison]::Ordinal)
    Assert-HaiTest ($migrationCall -ge 0 -and $startupInitialize -ge 0 -and $startupTaskRegistration -gt $startupInitialize) 'The Windows upgrade path no longer reaches maintenance task registration after environment migration.'

    Write-Output 'OpenClaw migration/startup fixtures passed: opt-out, token, encoding, ACL-before-write, fail-closed, concurrent migration locking, isolated installer mutex exclusion, mocked legacy-worker replacement guard/retry, self-locking silent-upgrade ordering, fresh-install deferral, and no application launch.'
} finally {
    $resolvedTemporaryRoot = [IO.Path]::GetFullPath($temporaryRoot)
    $resolvedTempParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if ($resolvedTemporaryRoot.StartsWith($resolvedTempParent, [StringComparison]::OrdinalIgnoreCase) -and
        [IO.Path]::GetFileName($resolvedTemporaryRoot) -match '\Ahai-openclaw-migration-test-[a-f0-9]{32}\z' -and
        (Test-Path -LiteralPath $resolvedTemporaryRoot -PathType Container)) {
        Remove-Item -LiteralPath $resolvedTemporaryRoot -Recurse -Force
    }
}
