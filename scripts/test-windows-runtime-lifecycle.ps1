[CmdletBinding()]
param(
    [switch]$FocusedOrphanShutdown,
    [switch]$FocusedInstallerSettings
)

$ErrorActionPreference = 'Stop'
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$supportSource = Join-Path $repositoryRoot 'installer\windows\Hai-InstallerSupport.ps1'
$startSource = Join-Path $repositoryRoot 'installer\windows\Start-HAI.ps1'
$openSource = Join-Path $repositoryRoot 'installer\windows\Open-HAI.ps1'
$initializerSource = Join-Path $repositoryRoot 'scripts\initialize-windows.ps1'
$exampleEnvironmentSource = Join-Path $repositoryRoot '.env.example'
$composeSource = Join-Path $repositoryRoot 'docker-compose.local.yml'
$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ('hai-runtime-lifecycle-' + [Guid]::NewGuid().ToString('N'))
$installRoot = Join-Path $temporaryRoot 'install'
$windowsDirectory = Join-Path $installRoot 'installer\windows'
$scriptsDirectory = Join-Path $installRoot 'scripts'
$profileRoot = Join-Path $temporaryRoot 'profile'
$workerScript = Join-Path $windowsDirectory 'Run-HAI-DeepSeekBridge.ps1'
$bridgePath = Join-Path $windowsDirectory 'hai-dsh-bridge.exe'
$startScriptPath = Join-Path $windowsDirectory 'Start-HAI.ps1'
$openScriptPath = Join-Path $windowsDirectory 'Open-HAI.ps1'
$maintenanceScriptPath = Join-Path $windowsDirectory 'Hai-OpenClawMaintenance.ps1'
$composePath = Join-Path $installRoot 'docker-compose.local.yml'
$pidFile = Join-Path $profileRoot 'HAI\hai-dsh-bridge.pid'
$environmentFile = Join-Path $profileRoot 'HAI\hai.env'
$originalLocalAppData = $env:LOCALAPPDATA
$originalSandboxOverride = $env:HAI_HOST_RUNTIME_SANDBOX_VERIFIED
$originalComposeA2AEnvironment = @{}
foreach ($entry in Get-ChildItem Env:) {
    if ($entry.Name -match '^(?i:HAI_A2A_)' -or $entry.Name -ieq 'COMPOSE_PROFILES') {
        $originalComposeA2AEnvironment[$entry.Name] = $entry.Value
    }
}

function Assert-HaiRuntimeTest([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

New-Item -ItemType Directory -Path $windowsDirectory, $scriptsDirectory, (Split-Path -Parent $pidFile) -Force | Out-Null
Copy-Item -LiteralPath $supportSource -Destination (Join-Path $windowsDirectory 'Hai-InstallerSupport.ps1')
Copy-Item -LiteralPath $startSource -Destination $startScriptPath
Copy-Item -LiteralPath $openSource -Destination $openScriptPath
Copy-Item -LiteralPath $initializerSource -Destination (Join-Path $scriptsDirectory 'initialize-windows.ps1')
Copy-Item -LiteralPath $exampleEnvironmentSource -Destination (Join-Path $installRoot '.env.example')
Copy-Item -LiteralPath $composeSource -Destination $composePath
[IO.File]::WriteAllText($maintenanceScriptPath, 'function Register-HaiOpenClawMaintenanceTask { param([string]$EnvFile) return $false }')
[IO.File]::WriteAllText($workerScript, '# test fixture')
[IO.File]::WriteAllText($bridgePath, 'test fixture')
    [IO.File]::WriteAllText($environmentFile, 'fixture=true')
$env:LOCALAPPDATA = $profileRoot
$global:runtimeStartupDockerCalls = New-Object 'System.Collections.Generic.List[string]'
$global:runtimeRejectDockerCalls = $false
$global:runtimeDockerVolumes = @()
$global:runtimeStartProcessCalls = 0
$global:runtimeAllowStartProcess = $false
$global:runtimeOpenedUrls = New-Object 'System.Collections.Generic.List[string]'

function docker {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$DockerArguments)
    $global:runtimeStartupDockerCalls.Add(($DockerArguments -join ' ')) | Out-Null
    if ($global:runtimeRejectDockerCalls) {
        throw 'The isolated runtime-gate test must not invoke Docker.'
    }
    $isEngineVersionQuery = $DockerArguments.Count -ge 1 -and $DockerArguments[0] -eq 'version'
    $isComposeVersionQuery = $DockerArguments.Count -ge 2 -and
        $DockerArguments[0] -eq 'compose' -and $DockerArguments[1] -eq 'version'
    $isReadOnlyContainerQuery = $DockerArguments -contains 'ps'
    $isReadOnlyVolumeQuery = $DockerArguments.Count -ge 2 -and
        $DockerArguments[0] -eq 'volume' -and $DockerArguments[1] -eq 'ls'
    if (-not $isEngineVersionQuery -and -not $isComposeVersionQuery -and
        -not $isReadOnlyContainerQuery -and -not $isReadOnlyVolumeQuery) {
        throw "Unexpected Docker command in isolated initializer test: $($DockerArguments -join ' ')"
    }
    if ($isEngineVersionQuery) { Write-Output '27.0.0' }
    if ($isComposeVersionQuery) { Write-Output 'Docker Compose version v2.30.0' }
    if ($isReadOnlyVolumeQuery) { $global:runtimeDockerVolumes }
    $global:LASTEXITCODE = 0
}
function Start-Process {
    param([Parameter(Position = 0)][string]$FilePath)
    if ($global:runtimeAllowStartProcess) {
        $global:runtimeStartProcessCalls++
        $global:runtimeOpenedUrls.Add($FilePath) | Out-Null
        return
    }
    $global:runtimeStartProcessCalls++
    throw 'The isolated runtime-gate test must not start a process.'
}

$script:runtimeLifecycleProcesses = @()
$script:runtimeLifecycleStops = New-Object 'System.Collections.Generic.List[int]'
$script:runtimeLifecycleTerminationAttempts = New-Object 'System.Collections.Generic.List[int]'
$script:runtimeLifecycleKeepAliveIds = @()
$script:runtimeLifecycleReusePidOnNextTermination = 0
function Get-CimInstance {
    [CmdletBinding()]
    param([string]$ClassName, [string]$Filter)
    if ($script:runtimeLifecycleCimFailure) { throw 'fixture CIM failure' }
    if ($ClassName -ne 'Win32_Process') { throw "Unexpected CIM class: $ClassName" }
    if ($Filter -match 'ProcessId\s*=\s*(?<id>[0-9]+)') {
        $id = [int]$Matches.id
        $matches = @($script:runtimeLifecycleProcesses | Where-Object { $_.Active -and $_.ProcessId -eq $id })
    } elseif ($Filter -match "Name\s*=\s*'(?<name>[^']+)'" ) {
        $name = $Matches.name
        $matches = @($script:runtimeLifecycleProcesses | Where-Object { $_.Active -and $_.Name -ieq $name })
    } else {
        throw "Unexpected CIM filter: $Filter"
    }
    foreach ($process in $matches) {
        if (-not $process.PSObject.Properties['CreationDate']) {
            $creation = [DateTime]::FromFileTimeUtc(132537600000000000 + ([int64]$process.ProcessId * [TimeSpan]::TicksPerSecond))
            $process | Add-Member -MemberType NoteProperty -Name CreationDate -Value $creation
        }
    }
    return $matches
}
function Get-Process {
    [CmdletBinding()]
    param([int]$Id)
    if ($script:runtimeLifecycleProcesses | Where-Object { $_.Active -and $_.ProcessId -eq $Id } | Select-Object -First 1) {
        return [pscustomobject]@{ Id = $Id }
    }
    return $null
}
function Stop-Process {
    [CmdletBinding()]
    param([int]$Id)
    $process = $script:runtimeLifecycleProcesses | Where-Object { $_.Active -and $_.ProcessId -eq $Id } | Select-Object -First 1
    if ($null -eq $process) { throw "fixture process $Id is not running" }
    if ($Id -notin $script:runtimeLifecycleKeepAliveIds) { $process.Active = $false }
    $script:runtimeLifecycleStops.Add($Id)
}
function Wait-Process {
    [CmdletBinding()]
    param([int]$Id, [int]$Timeout)
    if ($script:runtimeLifecycleProcesses | Where-Object { $_.Active -and $_.ProcessId -eq $Id } | Select-Object -First 1) {
        throw "fixture process $Id did not exit"
    }
}

try {
    . (Join-Path $windowsDirectory 'Hai-InstallerSupport.ps1')

    if (-not $FocusedOrphanShutdown) {
        $originalFixtureBytes = [IO.File]::ReadAllBytes($environmentFile)
        foreach ($newline in @("`r`n", "`n")) {
            [IO.File]::WriteAllText($environmentFile, "GATEWAY_HOST_PORT=18088${newline}HAI_A2A_LOCAL_PORT=18091${newline}KEEP_UNRELATED_SETTING=preserve-me${newline}")
            $before = [Convert]::ToBase64String([IO.File]::ReadAllBytes($environmentFile))
            Assert-HaiRuntimeTest ((Get-HaiGatewayPort) -eq 18088) 'Dashboard lookup ignored the configured custom port.'
            Assert-HaiRuntimeTest ((Get-HaiA2ALocalPort) -eq 18091) 'A2A lookup ignored the configured custom port.'
            Assert-HaiRuntimeTest ((Get-HaiUrl) -ceq 'http://127.0.0.1:18088' -and (Get-HaiA2AUrl) -ceq 'http://127.0.0.1:18091') 'Local URLs did not retain custom ports.'
            Assert-HaiRuntimeTest ([Convert]::ToBase64String([IO.File]::ReadAllBytes($environmentFile)) -ceq $before) 'Reading installed ports rewrote user settings.'
        }
        foreach ($setting in @('GATEWAY_HOST_PORT', 'HAI_A2A_LOCAL_PORT')) {
            foreach ($value in @('0', '65536', '-1', 'abc', '', '18088-extra', '"18088')) {
                [IO.File]::WriteAllText($environmentFile, "$setting=$value`r`n")
                $rejected = $false
                try {
                    if ($setting -eq 'GATEWAY_HOST_PORT') { Get-HaiGatewayPort | Out-Null }
                    else { Get-HaiA2ALocalPort | Out-Null }
                } catch { $rejected = $true }
                Assert-HaiRuntimeTest $rejected "An invalid $setting value silently selected a default port."
            }
            [IO.File]::WriteAllText($environmentFile, "$setting=18088`r`n$setting=18091`r`n")
            $rejected = $false
            try {
                if ($setting -eq 'GATEWAY_HOST_PORT') { Get-HaiGatewayPort | Out-Null }
                else { Get-HaiA2ALocalPort | Out-Null }
            } catch { $rejected = $true }
            Assert-HaiRuntimeTest $rejected "Duplicate $setting values were accepted."
        }
        [IO.File]::WriteAllText($environmentFile, 'KEEP_UNRELATED_SETTING=preserve-me')
        Assert-HaiRuntimeTest ((Get-HaiGatewayPort) -eq 8088 -and (Get-HaiA2ALocalPort) -eq 8091) 'Missing port settings did not use backwards-compatible defaults.'
        foreach ($newline in @("`r`n", "`n")) {
            foreach ($duplicate in @('COMPOSE_PROJECT_NAME=INVALID', ' COMPOSE_PROJECT_NAME =other-project', 'COMPOSE_PROJECT_NAME=', 'COMPOSE_PROJECT_NAME="other-project"')) {
                [IO.File]::WriteAllText($environmentFile, "COMPOSE_PROJECT_NAME=018-hai${newline}$duplicate${newline}")
                $before = [Convert]::ToBase64String([IO.File]::ReadAllBytes($environmentFile))
                $failure = ''
                try { Get-HaiComposeProjectName | Out-Null } catch { $failure = $_.Exception.Message }
                Assert-HaiRuntimeTest ($failure -match 'duplicate COMPOSE_PROJECT_NAME settings') 'A malformed or whitespace-prefixed duplicate Compose project assignment was accepted.'
                Assert-HaiRuntimeTest ([Convert]::ToBase64String([IO.File]::ReadAllBytes($environmentFile)) -ceq $before) 'Project-name validation changed local settings.'
            }
        }
        foreach ($projectValue in @('INVALID', '', '"018-hai"', '018-hai # comment')) {
            [IO.File]::WriteAllText($environmentFile, "COMPOSE_PROJECT_NAME=$projectValue`r`n")
            $failure = ''
            try { Get-HaiComposeProjectName | Out-Null } catch { $failure = $_.Exception.Message }
            Assert-HaiRuntimeTest ($failure -match 'invalid COMPOSE_PROJECT_NAME setting') 'An invalid Compose project assignment selected a default or a different project.'
        }
        [IO.File]::WriteAllText($environmentFile, "COMPOSE_PROJECT_NAME=hai-local_2`r`n")
        Assert-HaiRuntimeTest ((Get-HaiComposeProjectName) -ceq 'hai-local_2') 'A valid custom Compose project was rejected.'
        [IO.File]::WriteAllText($environmentFile, 'KEEP_UNRELATED_SETTING=preserve-me')
        Assert-HaiRuntimeTest ((Get-HaiComposeProjectName) -ceq '018-hai') 'A missing Compose project assignment lost its backwards-compatible default.'
        Assert-HaiRuntimeTest ($global:runtimeStartupDockerCalls.Count -eq 0 -and $global:runtimeStartProcessCalls -eq 0) 'Settings lookup attempted to mutate Docker or launch a process.'
        [IO.File]::WriteAllBytes($environmentFile, $originalFixtureBytes)
        Write-Output 'Focused installed-settings regression tests passed (CRLF/LF custom ports, invalid/duplicate rejection, defaults and unchanged user settings).'
        if ($FocusedInstallerSettings) { return }
    }

    function Invoke-HaiVerifiedProcessTermination {
        param(
            [Parameter(Mandatory = $true)][uint32]$ProcessId,
            [Parameter(Mandatory = $true)][string]$ExpectedExecutablePath,
            [Parameter(Mandatory = $true)][long]$ExpectedCreationFileTimeUtc,
            [ValidateRange(1, 60000)][uint32]$TimeoutMilliseconds
        )
        $process = @($script:runtimeLifecycleProcesses | Where-Object { $_.Active -and $_.ProcessId -eq [int]$ProcessId }) | Select-Object -First 1
        if (-not $process) { return $false }
        $script:runtimeLifecycleTerminationAttempts.Add([int]$ProcessId)
        if ($script:runtimeLifecycleReusePidOnNextTermination -eq [int]$ProcessId) {
            $script:runtimeLifecycleReusePidOnNextTermination = 0
            $process.CreationDate = $process.CreationDate.AddSeconds(10)
        }
        if ([string]$process.ExecutablePath -ine $ExpectedExecutablePath -or
            $process.CreationDate.ToUniversalTime().ToFileTimeUtc() -ne $ExpectedCreationFileTimeUtc) {
            throw 'The process ID was reused before shutdown; refusing to terminate the new process.'
        }
        if ([int]$ProcessId -in $script:runtimeLifecycleKeepAliveIds) {
            throw [TimeoutException]::new('The verified process fixture did not exit.')
        }
        $process.Active = $false
        $script:runtimeLifecycleStops.Add([int]$ProcessId)
        return $true
    }
    $script:runtimeLifecycleTerminatorMock = (Get-Command Invoke-HaiVerifiedProcessTermination -CommandType Function).ScriptBlock

    if ($FocusedOrphanShutdown) {
        $script:runtimeLifecycleCimFailure = $false
        $script:runtimeLifecycleProcesses = @(
            [pscustomobject]@{ ProcessId = 820; ParentProcessId = 1; Name = 'hai-dsh-bridge.exe'; ExecutablePath = $bridgePath; CommandLine = $bridgePath; Active = $true },
            [pscustomobject]@{ ProcessId = 821; ParentProcessId = 1; Name = 'hai-dsh-bridge.exe'; ExecutablePath = (Join-Path $temporaryRoot 'other-install\hai-dsh-bridge.exe'); CommandLine = 'unrelated'; Active = $true }
        )
        $script:runtimeLifecycleStops.Clear()
        $script:runtimeLifecycleTerminationAttempts.Clear()

        Assert-HaiRuntimeTest (-not (Test-Path -LiteralPath $pidFile)) 'The focused orphan-shutdown fixture unexpectedly has a PID marker.'
        Stop-HaiHostRuntimeWorker

        Assert-HaiRuntimeTest (($script:runtimeLifecycleStops -join ',') -ceq '820') 'Shutdown did not terminate only the exact-install-path orphan when the PID marker was absent.'
        Assert-HaiRuntimeTest (($script:runtimeLifecycleTerminationAttempts -join ',') -ceq '820') 'Shutdown did not pass the exact-install-path orphan through the mocked verified terminator.'
        Assert-HaiRuntimeTest (-not ($script:runtimeLifecycleProcesses | Where-Object { $_.ProcessId -eq 820 -and $_.Active })) 'The exact-install-path orphan remained active after mocked shutdown.'
        Assert-HaiRuntimeTest (($script:runtimeLifecycleProcesses | Where-Object { $_.ProcessId -eq 821 -and $_.Active }).Count -eq 1) 'Shutdown affected a same-name bridge outside this installation.'
        Assert-HaiRuntimeTest (-not (Test-Path -LiteralPath $pidFile)) 'Shutdown created a PID marker while recovering an orphan.'
        Write-Output 'Focused orphan shutdown regression test passed (mocked process enumeration and termination only).'
        return
    }

    $sensitiveTemporaryPath = Join-Path $temporaryRoot ('.hai-cleanup-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    [IO.File]::WriteAllText($sensitiveTemporaryPath, 'cleanup-canary-secret')
    $heldSensitiveFile = [IO.File]::Open($sensitiveTemporaryPath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        $cleanupFailure = ''
        try {
            Remove-HaiSensitiveTemporaryFile -Path $sensitiveTemporaryPath -WarningAction SilentlyContinue
        } catch {
            $cleanupFailure = $_.Exception.Message
        }
        Assert-HaiRuntimeTest ($cleanupFailure -match [Regex]::Escape([IO.Path]::GetFileName($sensitiveTemporaryPath)) -and $cleanupFailure -notmatch 'cleanup-canary-secret') 'A caller could silence the cleanup warning and leave a credential-bearing file without a terminating error naming the file.'
        Assert-HaiRuntimeTest (Test-Path -LiteralPath $sensitiveTemporaryPath) 'The locked sensitive temporary fixture unexpectedly disappeared.'
    } finally {
        $heldSensitiveFile.Dispose()
    }
    [void](Remove-HaiSensitiveTemporaryFile -Path $sensitiveTemporaryPath)
    Assert-HaiRuntimeTest (-not (Test-Path -LiteralPath $sensitiveTemporaryPath)) 'Sensitive temporary cleanup did not remove the file after its lock was released.'

    $a2aCleanupEnvironment = Join-Path $temporaryRoot 'a2a-cleanup.env'
    [IO.File]::WriteAllText($a2aCleanupEnvironment, "FIRST_RUN_ADMIN_EMAIL=operator@example.test`r`nHAI_A2A_BRIDGE_ENABLED=true`r`nHAI_A2A_BRIDGE_OWNER_ID=operator@example.test`r`nHAI_A2A_BRIDGE_TOKEN=$('c' * 64)`r`nHAI_A2A_LOCAL_PORT=18091`r`nHAI_A2A_BRIDGE_URL=https://a2a.example.test/saved/rpc`r`nHAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED=false`r`n")
    $script:a2aCleanupRemovalCalls = New-Object 'System.Collections.Generic.List[string]'
    $script:a2aCleanupHeldFile = $null
    $script:a2aCleanupOriginalRemove = (Get-Item Function:\Remove-HaiSensitiveTemporaryFile).ScriptBlock
    function Remove-HaiSensitiveTemporaryFile {
        param([Parameter(Mandatory = $true)][string]$Path)
        $script:a2aCleanupRemovalCalls.Add($Path) | Out-Null
        if ($null -eq $script:a2aCleanupHeldFile -and [IO.Path]::GetFileName($Path) -like '.hai-a2a-backup-*.tmp') {
            $script:a2aCleanupHeldFile = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
        }
        & $script:a2aCleanupOriginalRemove -Path $Path -WarningAction SilentlyContinue
    }
    try {
        $configurationFailure = ''
        try {
            Set-HaiA2ABridgeConfiguration -Path $a2aCleanupEnvironment -Enabled $false
        } catch {
            $configurationFailure = $_.Exception.Message
        }
        $backupAttempts = @($script:a2aCleanupRemovalCalls | Where-Object { [IO.Path]::GetFileName($_) -match '^\.hai-a2a-backup-[0-9a-f]{32}\.tmp$' })
        $temporaryAttempts = @($script:a2aCleanupRemovalCalls | Where-Object { [IO.Path]::GetFileName($_) -match '^\.hai-a2a-[0-9a-f]{32}\.tmp$' })
        Assert-HaiRuntimeTest ($backupAttempts.Count -ge 1 -and $temporaryAttempts.Count -ge 1) 'A2A cleanup did not attempt both the backup and temporary environment files after one cleanup failure.'
        Assert-HaiRuntimeTest ($configurationFailure -match 'secure cleanup' -and $configurationFailure -match [Regex]::Escape([IO.Path]::GetFileName($backupAttempts[0])) -and $configurationFailure -notmatch ('c' * 64)) 'An A2A backup cleanup failure did not fail the configuration update with the exact remaining filename, or exposed its credential contents.'
        Assert-HaiRuntimeTest ($null -ne $script:a2aCleanupHeldFile -and (Test-Path -LiteralPath $backupAttempts[0])) 'The A2A cleanup failure fixture did not retain the locked backup for explicit recovery.'
    } finally {
        if ($null -ne $script:a2aCleanupHeldFile) { $script:a2aCleanupHeldFile.Dispose() }
        Set-Item -Path Function:\Remove-HaiSensitiveTemporaryFile -Value $script:a2aCleanupOriginalRemove
        foreach ($candidate in @(Get-ChildItem -LiteralPath $temporaryRoot -Filter '.hai-a2a-*.tmp' -File -ErrorAction SilentlyContinue)) {
            [IO.File]::Delete($candidate.FullName)
        }
    }
    Assert-HaiRuntimeTest (-not (Get-ChildItem -LiteralPath $temporaryRoot -Filter '.hai-a2a-*.tmp' -File -ErrorAction SilentlyContinue)) 'An isolated A2A cleanup fixture remained after its lock was released and cleanup was retried.'

    $script:a2aWriteOriginal = (Get-Item Function:\Write-HaiAclProtectedFile).ScriptBlock
    $script:a2aTempCleanupOriginalRemove = (Get-Item Function:\Remove-HaiSensitiveTemporaryFile).ScriptBlock
    $script:a2aTempCleanupHeldFile = $null
    $script:a2aTempCleanupPath = ''
    function Write-HaiAclProtectedFile {
        param(
            [Parameter(Mandatory = $true)][string]$Path,
            [Parameter(Mandatory = $true)][byte[]]$Bytes,
            [Parameter(Mandatory = $true)][Security.AccessControl.FileSecurity]$FileSecurity,
            [scriptblock]$BeforeWrite
        )
        $writeArguments = @{ Path = $Path; Bytes = $Bytes; FileSecurity = $FileSecurity }
        if ($null -ne $BeforeWrite) { $writeArguments.BeforeWrite = $BeforeWrite }
        & $script:a2aWriteOriginal @writeArguments
        $script:a2aTempCleanupPath = $Path
        $script:a2aTempCleanupHeldFile = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
        throw 'fixture replacement failure'
    }
    try {
        $operationAndCleanupFailure = ''
        try {
            Set-HaiA2ABridgeConfiguration -Path $a2aCleanupEnvironment -Enabled $false
        } catch {
            $operationAndCleanupFailure = $_.Exception.Message
        }
        Assert-HaiRuntimeTest ($operationAndCleanupFailure -match 'did not complete' -and
            $operationAndCleanupFailure -match 'secure cleanup failed' -and
            $operationAndCleanupFailure -match [Regex]::Escape([IO.Path]::GetFileName($script:a2aTempCleanupPath)) -and
            $operationAndCleanupFailure -notmatch 'fixture replacement failure') 'A replacement failure masked the remaining sensitive temporary file or exposed an unsafe raw operation error.'
        Assert-HaiRuntimeTest ($null -ne $script:a2aTempCleanupHeldFile -and (Test-Path -LiteralPath $script:a2aTempCleanupPath)) 'The combined replacement/cleanup failure fixture did not preserve the locked temporary file for recovery.'
    } finally {
        if ($null -ne $script:a2aTempCleanupHeldFile) { $script:a2aTempCleanupHeldFile.Dispose() }
        Set-Item -Path Function:\Write-HaiAclProtectedFile -Value $script:a2aWriteOriginal
        Set-Item -Path Function:\Remove-HaiSensitiveTemporaryFile -Value $script:a2aTempCleanupOriginalRemove
        if (Test-Path -LiteralPath $script:a2aTempCleanupPath -PathType Leaf) {
            [IO.File]::Delete($script:a2aTempCleanupPath)
        }
    }
    Assert-HaiRuntimeTest (-not (Test-Path -LiteralPath $script:a2aTempCleanupPath)) 'The isolated A2A temporary-file failure fixture remained after its lock was released and cleanup was retried.'

    $savedToken = 'a' * 64
    $savedA2ALines = @(
        'HAI_A2A_BRIDGE_ENABLED=true',
        'HAI_A2A_BRIDGE_OWNER_ID=operator@example.test',
        "HAI_A2A_BRIDGE_TOKEN=$savedToken",
        'HAI_A2A_LOCAL_PORT=18091',
        'HAI_A2A_BRIDGE_URL=https://a2a.example.test/saved/rpc',
        'HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED=false'
    )
    $savedNetworkLines = @('HAI_A2A_LOCAL_SUBNET=10.254.0.0/24', 'HAI_HOST_RUNTIME_INTERNAL_SUBNET=10.254.1.0/24')
    $savedEnvironment = $savedA2ALines + $savedNetworkLines + 'HAI_OPENCLAW_MAINTENANCE_ENABLED=false'
    [IO.File]::WriteAllLines($environmentFile, [string[]]$savedEnvironment)
    foreach ($startAttempt in 1..2) {
        Initialize-HaiLocalEnvironment -GatewayPort 8088
        $currentA2ALines = @(Get-Content -LiteralPath $environmentFile | Where-Object { $_ -match '^HAI_A2A_(?:BRIDGE_|LOCAL_PORT=)' })
        Assert-HaiRuntimeTest (($currentA2ALines -join "`n") -ceq ($savedA2ALines -join "`n")) "No-switch startup attempt $startAttempt changed the saved enabled A2A settings or token."
        Assert-HaiRuntimeTest (Test-HaiA2ABridgeEnabled) "No-switch startup attempt $startAttempt disabled the saved A2A bridge."
    }

    $initializer = Join-Path $scriptsDirectory 'initialize-windows.ps1'
    & $initializer -EnvFile $environmentFile -AdminEmail 'operator@example.test' -AdminPasswordPlainText 'fixture-password-123' -Force
    $expectedEnvironmentAcl = Get-HaiComparableFileAccessDescriptor -FileSecurity (New-HaiRestrictedEnvironmentFileSecurity)
    $actualEnvironmentAcl = Get-HaiComparableFileAccessDescriptor -FileSecurity (Get-Acl -LiteralPath $environmentFile)
    Assert-HaiRuntimeTest ([string]::Equals($expectedEnvironmentAcl, $actualEnvironmentAcl, [StringComparison]::Ordinal)) 'Forced initialization retained the existing environment-file ACL instead of restricting access to the current user, SYSTEM, and administrators.'
    $preservedA2ALines = @(Get-Content -LiteralPath $environmentFile | Where-Object { $_ -match '^HAI_A2A_(?:BRIDGE_|LOCAL_PORT=)' })
    Assert-HaiRuntimeTest (($preservedA2ALines -join "`n") -ceq ($savedA2ALines -join "`n")) 'Replacing an existing environment without an A2A switch did not preserve the saved enabled bridge credentials and settings.'
    $preservedNetworkLines = @(Get-Content -LiteralPath $environmentFile | Where-Object { $_ -match '^(?:HAI_A2A_LOCAL_SUBNET|HAI_HOST_RUNTIME_INTERNAL_SUBNET)=' })
    Assert-HaiRuntimeTest ((@($preservedNetworkLines | Sort-Object) -join "`n") -ceq (@($savedNetworkLines | Sort-Object) -join "`n")) 'Replacing an existing environment reset an operator-selected Docker network range.'

    & $initializer -EnvFile $environmentFile -AdminEmail 'operator@example.test' -AdminPasswordPlainText 'fixture-password-123' -Force -EnableA2ABridge
    $explicitlyPreservedA2ALines = @(Get-Content -LiteralPath $environmentFile | Where-Object { $_ -match '^HAI_A2A_(?:BRIDGE_|LOCAL_PORT=)' })
    Assert-HaiRuntimeTest (($explicitlyPreservedA2ALines -join "`n") -ceq ($savedA2ALines -join "`n")) 'Explicit enablement rotated a valid saved A2A token or changed its settings.'

    & $initializer -EnvFile $environmentFile -AdminEmail 'operator@example.test' -AdminPasswordPlainText 'fixture-password-123' -Force -EnableA2ABridge
    $repeatedExplicitA2ALines = @(Get-Content -LiteralPath $environmentFile | Where-Object { $_ -match '^HAI_A2A_(?:BRIDGE_|LOCAL_PORT=)' })
    Assert-HaiRuntimeTest (($repeatedExplicitA2ALines -join "`n") -ceq ($explicitlyPreservedA2ALines -join "`n")) 'Repeated explicit enablement rotated the A2A token or changed its settings.'

    & $initializer -EnvFile $environmentFile -AdminEmail 'operator@example.test' -AdminPasswordPlainText 'fixture-password-123' -Force -EnableA2ABridge:$false
    $disabledA2ALines = @(Get-Content -LiteralPath $environmentFile | Where-Object { $_ -match '^HAI_A2A_' })
    Assert-HaiRuntimeTest ($disabledA2ALines -contains 'HAI_A2A_BRIDGE_ENABLED=false' -and $disabledA2ALines -contains 'HAI_A2A_BRIDGE_TOKEN=' -and $disabledA2ALines -contains 'HAI_A2A_BRIDGE_OWNER_ID=') 'An explicit false initializer choice did not disable A2A and clear its credentials.'

    $freshDefaultEnvironment = Join-Path (Split-Path -Parent $environmentFile) 'fresh-default.env'
    & $initializer -EnvFile $freshDefaultEnvironment -AdminEmail 'operator@example.test' -AdminPasswordPlainText 'fixture-password-123'
    $freshDefaultA2ALines = @(Get-Content -LiteralPath $freshDefaultEnvironment | Where-Object { $_ -match '^HAI_A2A_' })
    Assert-HaiRuntimeTest ($freshDefaultA2ALines -contains 'HAI_A2A_BRIDGE_ENABLED=false' -and $freshDefaultA2ALines -contains 'HAI_A2A_BRIDGE_TOKEN=') 'A fresh install without an A2A choice did not remain disabled without credentials.'

    $freshEnabledEnvironment = Join-Path (Split-Path -Parent $environmentFile) 'fresh-enabled.env'
    & $initializer -EnvFile $freshEnabledEnvironment -AdminEmail 'operator@example.test' -AdminPasswordPlainText 'fixture-password-123' -EnableA2ABridge
    $freshEnabledA2ALines = @(Get-Content -LiteralPath $freshEnabledEnvironment | Where-Object { $_ -match '^HAI_A2A_' })
    $freshTokenMatch = [Regex]::Match(($freshEnabledA2ALines | Where-Object { $_ -match '^HAI_A2A_BRIDGE_TOKEN=' }), '^HAI_A2A_BRIDGE_TOKEN=(?<token>.+)$')
    Assert-HaiRuntimeTest ($freshEnabledA2ALines -contains 'HAI_A2A_BRIDGE_ENABLED=true' -and $freshEnabledA2ALines -contains 'HAI_A2A_BRIDGE_OWNER_ID=operator@example.test' -and $freshTokenMatch.Success -and $freshTokenMatch.Groups['token'].Value.Length -ge 32) 'Explicit first-run opt-in did not generate A2A owner credentials.'
    & $initializer -EnvFile $freshEnabledEnvironment -AdminEmail 'operator@example.test' -AdminPasswordPlainText 'fixture-password-123' -Force -EnableA2ABridge
    $repeatedFreshEnabledA2ALines = @(Get-Content -LiteralPath $freshEnabledEnvironment | Where-Object { $_ -match '^HAI_A2A_' })
    Assert-HaiRuntimeTest (($repeatedFreshEnabledA2ALines -join "`n") -ceq ($freshEnabledA2ALines -join "`n")) 'Repeated explicit first-run enablement did not preserve the generated A2A credentials.'

    $secretMarker = 'credential-marker-3f8b7d2a'
    $invalidBridgeUrls = @(
        "https://user:$secretMarker@a2a.example.test/saved/rpc",
        "https://a2a.example.test/saved/rpc?access_token=$secretMarker",
        "https://a2a.example.test/saved/rpc#$secretMarker",
        "https://a2a.example.test/api/v1/a2a/token/$secretMarker",
        'https://a2a.example.test/api/v1/a2a/eyJhbGciOiJIUzI1NiJ9eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwMTIzNDU2Nzg5MCJ9.abcdefghijklmnopqrstuvwxyzABCDEFG1234567890'
    )
    foreach ($invalidBridgeUrl in $invalidBridgeUrls) {
        $invalidSavedEnvironment = Join-Path (Split-Path -Parent $environmentFile) ('invalid-a2a-' + [Guid]::NewGuid().ToString('N') + '.env')
        $invalidSavedLines = @(
            'HAI_A2A_BRIDGE_ENABLED=true',
            'HAI_A2A_BRIDGE_OWNER_ID=operator@example.test',
            "HAI_A2A_BRIDGE_TOKEN=$savedToken",
            'HAI_A2A_LOCAL_PORT=18091',
            "HAI_A2A_BRIDGE_URL=$invalidBridgeUrl",
            'HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED=false'
        )
        [IO.File]::WriteAllLines($invalidSavedEnvironment, [string[]]$invalidSavedLines)
        $beforeInvalidUrl = [Convert]::ToBase64String([IO.File]::ReadAllBytes($invalidSavedEnvironment))
        $initializerOutput = ''
        $initializerRejectedUrl = $false
        try {
            $initializerOutput = (& $initializer -EnvFile $invalidSavedEnvironment -AdminEmail 'operator@example.test' -AdminPasswordPlainText 'fixture-password-123' -Force *>&1 | Out-String)
        } catch {
            $initializerRejectedUrl = $true
            $initializerOutput += $_.Exception.Message
        }
        Assert-HaiRuntimeTest $initializerRejectedUrl 'The initializer accepted a saved credential-bearing A2A URL.'
        Assert-HaiRuntimeTest ([Convert]::ToBase64String([IO.File]::ReadAllBytes($invalidSavedEnvironment)) -ceq $beforeInvalidUrl) 'Rejecting a credential-bearing URL modified the saved environment file.'
        Assert-HaiRuntimeTest ($initializerOutput -notmatch [Regex]::Escape($secretMarker) -and $initializerOutput -notmatch [Regex]::Escape($savedToken)) 'Initializer output exposed saved A2A credentials or URL data.'
        Assert-HaiRuntimeTest (-not (Test-HaiCredentialFreeA2ABridgeUrl -Url $invalidBridgeUrl)) 'Installer support accepted a credential-bearing A2A URL.'
        Remove-Item -LiteralPath $invalidSavedEnvironment -Force
    }
    Assert-HaiRuntimeTest (Test-HaiCredentialFreeA2ABridgeUrl -Url 'https://a2a.example.test/saved/rpc') 'Installer support rejected a safe HTTPS A2A endpoint.'

    $safeSecretUrl = "https://a2a.example.test/saved/$secretMarker"
    $safeOutputLines = @($savedEnvironment | Where-Object { $_ -notmatch '^HAI_A2A_BRIDGE_URL=' }) + "HAI_A2A_BRIDGE_URL=$safeSecretUrl"
    $safeOutputEnvironment = Join-Path (Split-Path -Parent $environmentFile) 'safe-output.env'
    [IO.File]::WriteAllLines($safeOutputEnvironment, [string[]]$safeOutputLines)
    $successfulInitializerOutput = (& $initializer -EnvFile $safeOutputEnvironment -AdminEmail 'operator@example.test' -AdminPasswordPlainText 'fixture-password-123' -Force *>&1 | Out-String)
    Assert-HaiRuntimeTest ($successfulInitializerOutput -notmatch [Regex]::Escape($savedToken) -and $successfulInitializerOutput -notmatch [Regex]::Escape($secretMarker)) 'Successful initializer output exposed the saved A2A token or endpoint.'

    Assert-HaiRuntimeTest (-not (Test-HaiA2ABridgeEnabled)) 'A missing A2A opt-in was treated as enabled.'
    [IO.File]::WriteAllText($environmentFile, "HAI_A2A_BRIDGE_ENABLED=false`r`n")
    Assert-HaiRuntimeTest (-not (Test-HaiA2ABridgeEnabled)) 'An explicit false A2A setting was treated as enabled.'
    [IO.File]::WriteAllText($environmentFile, " HAI_A2A_BRIDGE_ENABLED = 'TrUe' `r`n")
    Assert-HaiRuntimeTest (Test-HaiA2ABridgeEnabled) 'A single explicit true A2A setting was not recognized.'
    [IO.File]::WriteAllText($environmentFile, "HAI_A2A_BRIDGE_ENABLED=true`r`nHAI_A2A_BRIDGE_ENABLED=false`r`n")
    Assert-HaiRuntimeTest (-not (Test-HaiA2ABridgeEnabled)) 'Duplicate A2A settings were not rejected as ambiguous.'
    [IO.File]::WriteAllText($environmentFile, "HAI_A2A_BRIDGE_ENABLED=`"true'`r`n")
    Assert-HaiRuntimeTest (-not (Test-HaiA2ABridgeEnabled)) 'A malformed quoted A2A setting was treated as enabled.'

    $expectedPlanningUrl = 'https://a2a.example.test/custom/rpc'
    $agentCardProbeUrl = 'http://127.0.0.1:18091/.well-known/agent-card.json'
    $validAgentCard = [pscustomobject]@{
        name = 'HAI controlled planning bridge'
        supportedInterfaces = @([pscustomobject]@{
            protocolBinding = 'JSONRPC'
            protocolVersion = '1.0'
            url = $expectedPlanningUrl
        })
    }
    Assert-HaiRuntimeTest (Test-HaiA2AAgentCard -AgentCard $validAgentCard -BridgeUrl $expectedPlanningUrl) 'The valid configured JSON-RPC Agent Card was rejected.'
    Assert-HaiRuntimeTest (-not (Test-HaiA2AAgentCard -AgentCard $validAgentCard -BridgeUrl 'http://127.0.0.1:8091/api/v1/a2a')) 'An Agent Card URL different from the configured bridge URL was accepted.'
    Assert-HaiRuntimeTest (-not (Test-HaiA2AAgentCard -AgentCard ([pscustomobject]@{ name = ''; supportedInterfaces = @() }) -BridgeUrl $expectedPlanningUrl)) 'An incomplete Agent Card was accepted.'
    Assert-HaiRuntimeTest (-not (Test-HaiA2AAgentCard -AgentCard ([pscustomobject]@{
        name = 'HAI controlled planning bridge'
        supportedInterfaces = @([pscustomobject]@{ protocolBinding = 'JSONRPC'; protocolVersion = '1.0'; url = 'http://127.0.0.1:8091/wrong' })
    }) -BridgeUrl $expectedPlanningUrl)) 'An Agent Card advertising the wrong planning endpoint was accepted.'

    $script:runtimeAgentCardResponse = [pscustomobject]@{ StatusCode = 200; Content = $validAgentCard | ConvertTo-Json -Depth 5 -Compress }
    $global:runtimeAgentCardResponse = $script:runtimeAgentCardResponse
    $script:runtimeAgentCardRequests = New-Object 'System.Collections.Generic.List[string]'
    $global:runtimeAgentCardRequests = $script:runtimeAgentCardRequests
    $global:runtimeReadinessFailure = $false
    $global:runtimeFailedReadinessPath = ''
    function global:Invoke-WebRequest {
        [CmdletBinding()]
        param([switch]$UseBasicParsing, [string]$Uri, [int]$TimeoutSec, [int]$MaximumRedirection)
        $global:runtimeAgentCardRequests.Add($Uri) | Out-Null
        $path = ([uri]$Uri).AbsolutePath
        if ($global:runtimeFailedReadinessPath -and $path -ceq $global:runtimeFailedReadinessPath) {
            throw 'fixture component unavailable'
        }
        if ($path -ceq '/readyz' -and $global:runtimeReadinessFailure) {
            throw 'fixture gateway unavailable'
        }
        if ($path -in @('/readyz', '/_hai/idp-readyz', '/login', '/control-center')) { return [pscustomobject]@{ StatusCode = 200; Headers = @{ 'Content-Type' = 'text/html' }; Content = 'ready' } }
        if ($path -ceq '/api/v1/user/') { return [pscustomobject]@{ StatusCode = 200; Headers = @{ 'Content-Type' = 'text/html' }; Content = '<html>login</html>' } }
        return $global:runtimeAgentCardResponse
    }
    Wait-HaiA2AReady -TimeoutSeconds 30 -BridgeUrl $expectedPlanningUrl -AgentCardUrl $agentCardProbeUrl
    Assert-HaiRuntimeTest ($script:runtimeAgentCardRequests.Count -eq 1 -and $script:runtimeAgentCardRequests[0] -ceq $agentCardProbeUrl) 'A2A readiness did not fetch the Compose-published Agent Card endpoint.'

    $dockerCallsBeforeOpen = $global:runtimeStartupDockerCalls.Count
    $global:runtimeAllowStartProcess = $true
    try {
        & $openScriptPath
        & $openScriptPath
    } finally {
        $global:runtimeAllowStartProcess = $false
    }
    $dashboardUrl = "$(Get-HaiUrl)/control-center"
    Assert-HaiRuntimeTest ($global:runtimeOpenedUrls.Count -eq 2 -and
        @($global:runtimeOpenedUrls | Where-Object { $_ -ceq $dashboardUrl }).Count -eq 2 -and
        @($script:runtimeAgentCardRequests | Where-Object { $_ -ceq "$(Get-HaiUrl)/readyz" }).Count -eq 2 -and
        @($script:runtimeAgentCardRequests | Where-Object { $_ -ceq "$(Get-HaiUrl)/_hai/idp-readyz" }).Count -eq 2 -and
        @($script:runtimeAgentCardRequests | Where-Object { $_ -ceq "$(Get-HaiUrl)/login" }).Count -eq 2 -and
        @($script:runtimeAgentCardRequests | Where-Object { $_ -ceq $dashboardUrl }).Count -eq 2 -and
        @($script:runtimeAgentCardRequests | Where-Object { $_ -ceq "$(Get-HaiUrl)/api/v1/openclaw-maintenance" }).Count -eq 2 -and
        $global:runtimeStartupDockerCalls.Count -eq $dockerCallsBeforeOpen) "Repeated Open did not verify readiness and open the exact dashboard route without touching the Compose stack. Opened='$($global:runtimeOpenedUrls -join ',')'; probes='$($script:runtimeAgentCardRequests -join ',')'"

    $openedBeforeUnavailableProbe = $global:runtimeOpenedUrls.Count
    $dockerCallsBeforeUnavailableProbe = $global:runtimeStartupDockerCalls.Count
    $global:runtimeReadinessFailure = $true
    $openFailure = ''
    try { & $openScriptPath } catch { $openFailure = $_.Exception.Message }
    $global:runtimeReadinessFailure = $false
    Assert-HaiRuntimeTest ($openFailure -match 'backend readiness failed' -and
        $openFailure -match 'Use Start HAI' -and $openFailure -match 'No containers or data were changed' -and
        $global:runtimeOpenedUrls.Count -eq $openedBeforeUnavailableProbe -and
        $global:runtimeStartupDockerCalls.Count -eq $dockerCallsBeforeUnavailableProbe) 'Open did not fail with an actionable, non-mutating message when the local gateway was unavailable.'

    foreach ($componentFailure in @(
        [pscustomobject]@{ Path = '/_hai/idp-readyz'; Component = 'IDP' },
        [pscustomobject]@{ Path = '/login'; Component = 'login frontend' },
        [pscustomobject]@{ Path = '/control-center'; Component = 'dashboard frontend' }
    )) {
        $openedBeforeFailure = $global:runtimeOpenedUrls.Count
        $dockerBeforeFailure = $global:runtimeStartupDockerCalls.Count
        $global:runtimeFailedReadinessPath = $componentFailure.Path
        $componentError = ''
        try { & $openScriptPath } catch { $componentError = $_.Exception.Message }
        $global:runtimeFailedReadinessPath = ''
        Assert-HaiRuntimeTest ($componentError -match [Regex]::Escape($componentFailure.Component) -and
            $componentError -match 'readiness failed' -and
            $global:runtimeOpenedUrls.Count -eq $openedBeforeFailure -and
            $global:runtimeStartupDockerCalls.Count -eq $dockerBeforeFailure) "Open claimed success or mutated Docker while $($componentFailure.Component) was unavailable. Error='$componentError'"
    }

    $savedEnvironmentBytes = [IO.File]::ReadAllBytes($environmentFile)
    $openedBeforeMissingEnvironment = $global:runtimeOpenedUrls.Count
    $requestsBeforeMissingEnvironment = $script:runtimeAgentCardRequests.Count
    $dockerCallsBeforeMissingEnvironment = $global:runtimeStartupDockerCalls.Count
    Remove-Item -LiteralPath $environmentFile -Force
    try {
        $missingEnvironmentFailure = ''
        try { & $openScriptPath } catch { $missingEnvironmentFailure = $_.Exception.Message }
    Assert-HaiRuntimeTest ($missingEnvironmentFailure -match 'protected local environment file is missing' -and
            $missingEnvironmentFailure -match 'Restore the original protected environment' -and
            $missingEnvironmentFailure -match 'No containers or data were changed' -and
            $global:runtimeOpenedUrls.Count -eq $openedBeforeMissingEnvironment -and
            $script:runtimeAgentCardRequests.Count -eq $requestsBeforeMissingEnvironment -and
            $global:runtimeStartupDockerCalls.Count -eq $dockerCallsBeforeMissingEnvironment) 'Open did not fail with a precise, non-mutating recovery message when the saved HAI environment was missing.'
    } finally {
        [IO.File]::WriteAllBytes($environmentFile, $savedEnvironmentBytes)
    }

    $missingProfileRoot = Join-Path $temporaryRoot 'missing-protected-profile'
    $env:LOCALAPPDATA = $missingProfileRoot
    $global:runtimeStartupDockerCalls.Clear()
    $global:runtimeDockerVolumes = @('018-hai-postgres-idp')
    $openedBeforeUninitializedOpen = $global:runtimeOpenedUrls.Count
    $requestsBeforeUninitializedOpen = $script:runtimeAgentCardRequests.Count
    $uninitializedOpenFailure = ''
    try { & $openScriptPath } catch { $uninitializedOpenFailure = $_.Exception.Message }
    Assert-HaiRuntimeTest ($uninitializedOpenFailure -match 'protected local environment file is missing' -and
        -not (Test-Path -LiteralPath $missingProfileRoot) -and
        $global:runtimeOpenedUrls.Count -eq $openedBeforeUninitializedOpen -and
        $script:runtimeAgentCardRequests.Count -eq $requestsBeforeUninitializedOpen) 'Open created a profile directory or attempted readiness/browser access while reporting an uninitialized profile.'

    $invalidEnvironmentRoot = Join-Path $temporaryRoot 'invalid-environment-profile'
    $invalidEnvironmentPath = Join-Path $invalidEnvironmentRoot 'HAI\hai.env'
    New-Item -ItemType Directory -Path $invalidEnvironmentPath -Force | Out-Null
    $env:LOCALAPPDATA = $invalidEnvironmentRoot
    $requestsBeforeInvalidEnvironment = $script:runtimeAgentCardRequests.Count
    $invalidEnvironmentOpenFailure = ''
    try { & $openScriptPath } catch { $invalidEnvironmentOpenFailure = $_.Exception.Message }
    Assert-HaiRuntimeTest ($invalidEnvironmentOpenFailure -match 'exists but is not a regular file' -and
        $script:runtimeAgentCardRequests.Count -eq $requestsBeforeInvalidEnvironment) 'Open misreported a non-file environment path or attempted readiness against it.'
    $global:runtimeStartupDockerCalls.Clear()
    $invalidEnvironmentStartFailure = ''
    try { . $startScriptPath -NoBrowser } catch { $invalidEnvironmentStartFailure = $_.Exception.Message }
    Assert-HaiRuntimeTest ($invalidEnvironmentStartFailure -match 'exists but is not a regular file' -and
        $global:runtimeStartupDockerCalls.Count -eq 0) 'Startup did not reject an invalid environment path before Docker access.'

    $global:runtimeStartupDockerCalls.Clear()
    $env:LOCALAPPDATA = $missingProfileRoot
    $global:runtimeRejectDockerCalls = $true
    $unverifiableMissingEnvironmentFailure = ''
    try { . $startScriptPath -NoBrowser } catch { $unverifiableMissingEnvironmentFailure = $_.Exception.Message }
    finally { $global:runtimeRejectDockerCalls = $false }
    Assert-HaiRuntimeTest ($unverifiableMissingEnvironmentFailure -match 'protected hai.env is missing' -and
        $unverifiableMissingEnvironmentFailure -match 'Docker could not perform the read-only check for existing data volumes' -and
        $unverifiableMissingEnvironmentFailure -match 'No credentials, containers, or volumes were changed' -and
        -not (Test-Path -LiteralPath $missingProfileRoot)) 'Startup hid missing-environment recovery when Docker was unavailable or created a profile directory while failing closed.'

    $global:runtimeStartupDockerCalls.Clear()
    $startCallsBeforeMissingProtectedEnvironment = $global:runtimeStartProcessCalls
    $missingProtectedEnvironmentFailure = ''
    try { . $startScriptPath -NoBrowser } catch { $missingProtectedEnvironmentFailure = $_.Exception.Message }
    Assert-HaiRuntimeTest ($missingProtectedEnvironmentFailure -match 'protected environment file is missing' -and
        $missingProtectedEnvironmentFailure -match 'Restore the original hai.env from a secure backup' -and
        $missingProtectedEnvironmentFailure -match 'No volumes were changed' -and
        -not (Test-Path -LiteralPath $missingProfileRoot) -and
        $global:runtimeStartProcessCalls -eq $startCallsBeforeMissingProtectedEnvironment) 'Startup did not stop with a specific missing-protected-environment recovery message before initialization or process launch.'
    Assert-HaiRuntimeTest (($global:runtimeStartupDockerCalls | Where-Object { $_ -match '\b(up|down|stop|start|rm|prune)\b' }).Count -eq 0) 'Missing-environment startup issued a Docker container or volume mutation.'
    Assert-HaiRuntimeTest (($global:runtimeStartupDockerCalls | Where-Object { $_ -match '^volume ls' }).Count -eq 2) 'Missing-environment startup did not restrict Docker access to read-only HAI-volume verification.'
    Set-Item -LiteralPath Function:\Invoke-HaiVerifiedProcessTermination -Value $script:runtimeLifecycleTerminatorMock
    $global:runtimeDockerVolumes = @()
    $env:LOCALAPPDATA = $profileRoot

    $powershellPath = Join-Path $env:WINDIR 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $launcherCommandLine = '"{0}" -NoProfile -ExecutionPolicy Bypass -File "{1}" -EnvFile "{2}"' -f $powershellPath, $workerScript, (Join-Path $profileRoot 'HAI\hai.env')
    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 100; ParentProcessId = 1; Name = 'powershell.exe'; ExecutablePath = $powershellPath; CommandLine = $launcherCommandLine; Active = $true },
        [pscustomobject]@{ ProcessId = 200; ParentProcessId = 100; Name = 'hai-dsh-bridge.exe'; ExecutablePath = $bridgePath; CommandLine = $bridgePath; Active = $true },
        [pscustomobject]@{ ProcessId = 300; ParentProcessId = 100; Name = 'hai-dsh-bridge.exe'; ExecutablePath = (Join-Path $temporaryRoot 'other-install\hai-dsh-bridge.exe'); CommandLine = 'unrelated'; Active = $true }
    )
    $script:runtimeLifecycleCimFailure = $false
    [IO.File]::WriteAllText($pidFile, '100')
    Stop-HaiHostRuntimeWorker
    Assert-HaiRuntimeTest (($script:runtimeLifecycleStops -join ',') -ceq '200,100') 'Stop did not terminate the exact native bridge before its PowerShell launcher.'
    Assert-HaiRuntimeTest (($script:runtimeLifecycleTerminationAttempts -join ',') -ceq '200,100') 'Stop did not attempt the exact native bridge before its PowerShell launcher.'
    Assert-HaiRuntimeTest ((Get-Process -Id 300 -ErrorAction SilentlyContinue) -ne $null) 'Stop terminated a bridge outside this installation path.'
    Assert-HaiRuntimeTest (-not (Test-Path -LiteralPath $pidFile)) 'Stop retained a PID record after verified process shutdown.'

    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 100; ParentProcessId = 1; Name = 'powershell.exe'; ExecutablePath = $powershellPath; CommandLine = $launcherCommandLine; Active = $true },
        [pscustomobject]@{ ProcessId = 200; ParentProcessId = 100; Name = 'hai-dsh-bridge.exe'; ExecutablePath = $bridgePath; CommandLine = $bridgePath; Active = $true }
    )
    $script:runtimeLifecycleStops.Clear()
    $script:runtimeLifecycleTerminationAttempts.Clear()
    $script:runtimeLifecycleKeepAliveIds = @(200)
    [IO.File]::WriteAllText($pidFile, '100')
    try {
        Stop-HaiHostRuntimeWorker
        throw 'Stop accepted a legacy bridge process that remained alive.'
    } catch {
        if ($_.Exception.Message -eq 'Stop accepted a legacy bridge process that remained alive.') { throw }
        if ($_.Exception.Message -notmatch 'bridge did not exit') { throw }
    }
    Assert-HaiRuntimeTest (($script:runtimeLifecycleTerminationAttempts -join ',') -ceq '200') 'Stop attempted to stop the launcher after the owned bridge failed to exit.'
    Assert-HaiRuntimeTest ($script:runtimeLifecycleStops.Count -eq 0) 'A bridge that failed termination was reported as stopped.'
    Assert-HaiRuntimeTest ((Get-Process -Id 200 -ErrorAction SilentlyContinue) -ne $null) 'The stuck bridge fixture unexpectedly exited.'
    Assert-HaiRuntimeTest ([IO.File]::ReadAllText($pidFile) -ceq '100') 'Stop deleted the legacy PID record when the bridge had not exited.'
    $script:runtimeLifecycleKeepAliveIds = @()

    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 250; ParentProcessId = 1; Name = 'hai-dsh-bridge.exe'; ExecutablePath = $bridgePath; CommandLine = $bridgePath; Active = $true }
    )
    $script:runtimeLifecycleStops.Clear()
    $script:runtimeLifecycleTerminationAttempts.Clear()
    $script:runtimeLifecycleReusePidOnNextTermination = 250
    [IO.File]::WriteAllText($pidFile, '999')
    try {
        Stop-HaiHostRuntimeWorker
        throw 'Stop terminated a process after its PID was reused.'
    } catch {
        if ($_.Exception.Message -eq 'Stop terminated a process after its PID was reused.') { throw }
        if ($_.Exception.Message -notmatch 'bridge did not exit') { throw }
    }
    Assert-HaiRuntimeTest (($script:runtimeLifecycleTerminationAttempts -join ',') -ceq '250') 'PID reuse was not checked at the final termination boundary.'
    Assert-HaiRuntimeTest ($script:runtimeLifecycleStops.Count -eq 0 -and (Get-Process -Id 250 -ErrorAction SilentlyContinue)) 'A process with a reused PID was terminated.'
    Assert-HaiRuntimeTest ([IO.File]::ReadAllText($pidFile) -ceq '999') 'The PID record was removed after process identity changed.'

    Remove-Item -LiteralPath $pidFile -Force
    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 260; ParentProcessId = 1; Name = 'hai-dsh-bridge.exe'; ExecutablePath = $bridgePath; CommandLine = $bridgePath; Active = $true }
    )
    Assert-HaiRuntimeTest ((Get-HaiHostRuntimeWorkerStatus) -ceq 'bridge running without a verified launcher PID') 'Runtime status hid an orphaned bridge when its launcher PID file was absent.'
    [IO.File]::WriteAllText($pidFile, '261')
    Assert-HaiRuntimeTest ((Get-HaiHostRuntimeWorkerStatus) -ceq 'bridge running without a verified launcher PID') 'Runtime status reported stopped while an orphaned bridge remained active.'
    [IO.File]::WriteAllText($pidFile, 'invalid-pid')
    Assert-HaiRuntimeTest ((Get-HaiHostRuntimeWorkerStatus) -ceq 'bridge running without a verified launcher PID (PID record invalid)') 'Runtime status hid an orphaned bridge behind an invalid PID record.'
    $script:runtimeLifecycleProcesses += [pscustomobject]@{ ProcessId = 262; ParentProcessId = 1; Name = 'notepad.exe'; ExecutablePath = 'C:\Windows\System32\notepad.exe'; CommandLine = 'notepad'; Active = $true }
    [IO.File]::WriteAllText($pidFile, '262')
    Assert-HaiRuntimeTest ((Get-HaiHostRuntimeWorkerStatus) -ceq 'bridge running without a verified launcher PID (PID record points to another process)') 'Runtime status hid an orphaned bridge behind a mismatched PID record.'
    $script:runtimeLifecycleProcesses = @()
    Remove-Item -LiteralPath $pidFile -Force
    New-Item -ItemType Directory -Path $pidFile | Out-Null
    Assert-HaiRuntimeTest ((Get-HaiHostRuntimeWorkerStatus) -ceq 'pid record path exists but is not a regular file') 'Runtime status reported not started while the PID path was occupied by a non-file object.'
    Remove-Item -LiteralPath $pidFile -Force

    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 500; ParentProcessId = 100; Name = 'hai-dsh-bridge.exe'; ExecutablePath = $bridgePath; CommandLine = $bridgePath; Active = $true }
    )
    $script:runtimeLifecycleStops.Clear()
    [IO.File]::WriteAllText($pidFile, '100')
    Stop-HaiHostRuntimeWorker
    Assert-HaiRuntimeTest (($script:runtimeLifecycleStops -join ',') -ceq '500') 'Stop did not recover a bridge whose launcher had already exited.'
    Assert-HaiRuntimeTest (-not (Test-Path -LiteralPath $pidFile)) 'Stop retained a stale launcher PID after its orphaned bridge exited.'

    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 400; ParentProcessId = 1; Name = 'notepad.exe'; ExecutablePath = 'C:\Windows\System32\notepad.exe'; CommandLine = 'notepad'; Active = $true }
    )
    [IO.File]::WriteAllText($pidFile, '400')
    try {
        Stop-HaiHostRuntimeWorker
        throw 'Stop accepted a PID record that points to an unrelated process.'
    } catch {
        if ($_.Exception.Message -eq 'Stop accepted a PID record that points to an unrelated process.') { throw }
        if ($_.Exception.Message -notmatch 'different process') { throw }
    }
    Assert-HaiRuntimeTest ([IO.File]::ReadAllText($pidFile) -ceq '400') 'Stop removed an unverified PID record.'
    Assert-HaiRuntimeTest ($script:runtimeLifecycleStops.Count -eq 1) 'Stop attempted to terminate an unrelated process.'

    $script:runtimeLifecycleCimFailure = $true
    [IO.File]::WriteAllText($pidFile, '100')
    try {
        Stop-HaiHostRuntimeWorker
        throw 'Stop accepted a process query failure.'
    } catch {
        if ($_.Exception.Message -eq 'Stop accepted a process query failure.') { throw }
        if ($_.Exception.Message -notmatch 'fixture CIM failure') { throw }
    }
    Assert-HaiRuntimeTest ([IO.File]::ReadAllText($pidFile) -ceq '100') 'Stop removed the PID record after process verification failed.'

    $script:runtimeLifecycleCimFailure = $false
    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 600; ParentProcessId = 0; Name = 'hai-dsh-bridge.exe'; ExecutablePath = $bridgePath; CommandLine = $bridgePath; Active = $true }
    )
    $script:runtimeLifecycleStops.Clear()
    [IO.File]::WriteAllText($pidFile, '0')
    try {
        Stop-HaiHostRuntimeWorker
        throw 'Stop accepted PID zero as a launcher identity.'
    } catch {
        if ($_.Exception.Message -eq 'Stop accepted PID zero as a launcher identity.') { throw }
        if ($_.Exception.Message -notmatch 'PID record is invalid') { throw }
    }
    Assert-HaiRuntimeTest ([IO.File]::ReadAllText($pidFile) -ceq '0') 'Stop removed a numerically invalid PID record.'
    Assert-HaiRuntimeTest ($script:runtimeLifecycleStops.Count -eq 0 -and (Get-Process -Id 600 -ErrorAction SilentlyContinue)) 'Stop used PID zero to terminate a bridge process.'

    Remove-Item -LiteralPath $pidFile -Force
    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 700; ParentProcessId = 0; Name = 'hai-dsh-bridge.exe'; ExecutablePath = $bridgePath; CommandLine = $bridgePath; Active = $true },
        [pscustomobject]@{ ProcessId = 701; ParentProcessId = 0; Name = 'hai-dsh-bridge.exe'; ExecutablePath = (Join-Path $temporaryRoot 'other-install\hai-dsh-bridge.exe'); CommandLine = 'unrelated'; Active = $true }
    )
    $script:runtimeLifecycleStops.Clear()
    Stop-HaiHostRuntimeWorkerIfPresent
    Assert-HaiRuntimeTest (($script:runtimeLifecycleStops -join ',') -ceq '700') 'Startup did not stop an orphaned bridge at this install path when its PID file was absent.'
    Assert-HaiRuntimeTest ((Get-Process -Id 701 -ErrorAction SilentlyContinue) -ne $null) 'Startup stopped a bridge belonging to another installation when the PID file was absent.'
    Assert-HaiRuntimeTest (-not (Test-Path -LiteralPath $pidFile)) 'Orphan recovery created a PID file that did not previously exist.'

    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 710; ParentProcessId = 1; Name = 'powershell.exe'; ExecutablePath = $powershellPath; CommandLine = $launcherCommandLine; Active = $true },
        [pscustomobject]@{ ProcessId = 720; ParentProcessId = 710; Name = 'hai-dsh-bridge.exe'; ExecutablePath = $bridgePath; CommandLine = $bridgePath; Active = $true }
    )
    $script:runtimeLifecycleStops.Clear()
    Stop-HaiHostRuntimeWorkerIfPresent
    Assert-HaiRuntimeTest (($script:runtimeLifecycleStops -join ',') -ceq '720,710') 'Startup did not stop an unrecorded launcher after its bridge when no PID file existed.'

    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 730; ParentProcessId = 0; Name = 'hai-dsh-bridge.exe'; ExecutablePath = ''; CommandLine = 'unknown'; Active = $true }
    )
    $script:runtimeLifecycleStops.Clear()
    try {
        Stop-HaiHostRuntimeWorkerIfPresent
        throw 'Startup accepted a same-name bridge whose executable path could not be verified.'
    } catch {
        if ($_.Exception.Message -eq 'Startup accepted a same-name bridge whose executable path could not be verified.') { throw }
        if ($_.Exception.Message -notmatch 'ownership is unknown') { throw }
    }
    Assert-HaiRuntimeTest ($script:runtimeLifecycleStops.Count -eq 0 -and (Get-Process -Id 730 -ErrorAction SilentlyContinue)) 'Startup stopped an unverified same-name process.'

    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 400; ParentProcessId = 1; Name = 'notepad.exe'; ExecutablePath = 'C:\Windows\System32\notepad.exe'; CommandLine = 'notepad'; Active = $true }
    )
    [IO.File]::WriteAllText($pidFile, '400')
    try {
        Start-HaiHostRuntimeWorker
        throw 'Start allowed execution when a PID record referred to another live process.'
    } catch {
        if ($_.Exception.Message -eq 'Start allowed execution when a PID record referred to another live process.') { throw }
        if ($_.Exception.Message -notmatch 'no verified OS-enforced sandbox' -or $_.Exception.Message -notmatch 'different process') { throw }
    }
    Assert-HaiRuntimeTest ([IO.File]::ReadAllText($pidFile) -ceq '400') 'Start removed a PID record that referred to another live process.'
    Assert-HaiRuntimeTest ($script:runtimeLifecycleStops.Count -eq 0) 'Start attempted to stop a process from an unverified PID record.'

    [IO.File]::WriteAllText($pidFile, 'invalid-pid')
    try {
        Start-HaiHostRuntimeWorker
        throw 'Start allowed execution with an invalid PID record.'
    } catch {
        if ($_.Exception.Message -eq 'Start allowed execution with an invalid PID record.') { throw }
        if ($_.Exception.Message -notmatch 'no verified OS-enforced sandbox' -or $_.Exception.Message -notmatch 'PID record is invalid') { throw }
    }
    Assert-HaiRuntimeTest ([IO.File]::ReadAllText($pidFile) -ceq 'invalid-pid') 'Start removed an invalid PID record.'

    Remove-Item -LiteralPath $pidFile -Force
    $script:runtimeLifecycleProcesses = @()
    $script:runtimeLifecycleStops.Clear()
    $global:runtimeStartupDockerCalls.Clear()
    $global:runtimeStartProcessCalls = 0
    $environmentBeforeBlockedStart = [IO.File]::ReadAllBytes($environmentFile)
    $env:HAI_HOST_RUNTIME_SANDBOX_VERIFIED = 'true'
    try {
        Start-HaiHostRuntimeWorker
        throw 'Direct worker start accepted an environment-variable sandbox override.'
    } catch {
        if ($_.Exception.Message -eq 'Direct worker start accepted an environment-variable sandbox override.') { throw }
        Assert-HaiRuntimeTest ($_.Exception.Message -match 'no verified OS-enforced sandbox' -and $_.Exception.Message -match 'no environment-variable override') 'Direct worker start did not explain its unconditional sandbox gate.'
    } finally {
        if ($null -eq $originalSandboxOverride) {
            Remove-Item Env:HAI_HOST_RUNTIME_SANDBOX_VERIFIED -ErrorAction SilentlyContinue
        } else {
            $env:HAI_HOST_RUNTIME_SANDBOX_VERIFIED = $originalSandboxOverride
        }
    }
    Assert-HaiRuntimeTest ($global:runtimeStartProcessCalls -eq 0) 'Direct worker start launched a process despite the sandbox gate.'
    Assert-HaiRuntimeTest ($global:runtimeStartupDockerCalls.Count -eq 0) 'Direct worker start invoked Docker despite the sandbox gate.'
    Assert-HaiRuntimeTest ([Convert]::ToBase64String([IO.File]::ReadAllBytes($environmentFile)) -ceq [Convert]::ToBase64String($environmentBeforeBlockedStart)) 'Direct worker start changed the existing environment file.'

    $versionProbeWorkspace = Join-Path $temporaryRoot 'version-probe-workspace'
    New-Item -ItemType Directory -Path (Join-Path $versionProbeWorkspace 'state') -Force | Out-Null
    $versionProbeExecutable = Join-Path $temporaryRoot 'version-probe.cmd'
    $versionProbeMarker = Join-Path $temporaryRoot 'version-probe-started.txt'
    [IO.File]::WriteAllText($versionProbeExecutable, "@echo off`r`n> `"$versionProbeMarker`" echo launched`r`necho 1.2.3`r`nexit /b 0`r`n", [Text.Encoding]::ASCII)
    $runtimeConfiguration = @(
        'HAI_HOST_RUNTIME_BRIDGE_ENABLED=true',
        'HAI_HOST_RUNTIME_BRIDGE_TOKEN=fixture-token-with-more-than-32-characters',
        'HAI_HOST_RUNTIME_BRIDGE_URL=http://127.0.0.1:18092',
        'DEEPSEEK_HARNESS_ENABLED=true',
        'DEEPSEEK_HARNESS_EXECUTION_ENABLED=true',
        "DEEPSEEK_HARNESS_EXECUTABLE=$versionProbeExecutable",
        'DEEPSEEK_HARNESS_VERSION=1.2.3',
        "DEEPSEEK_HARNESS_WORKSPACE=$versionProbeWorkspace",
        "DEEPSEEK_HARNESS_STATE_DIR=$(Join-Path $versionProbeWorkspace 'state')",
        'DEEPSEEK_HARNESS_WORKSPACE_KEY=fixture-workspace-key'
    )
    [IO.File]::WriteAllText($environmentFile, ($runtimeConfiguration -join "`r`n") + "`r`n", [Text.Encoding]::ASCII)
    try {
        Assert-HaiHostRuntimeConfigured
        throw 'The legacy runtime configuration helper did not block before probing its executable.'
    } catch {
        if ($_.Exception.Message -eq 'The legacy runtime configuration helper did not block before probing its executable.') { throw }
        if ($_.Exception.Message -notmatch 'no verified OS-enforced sandbox') { throw }
    }
    Assert-HaiRuntimeTest (-not (Test-Path -LiteralPath $versionProbeMarker)) 'The legacy runtime configuration helper launched the configured executable while blocked.'

    $freshProfileRoot = Join-Path $temporaryRoot 'fresh-profile'
    $env:LOCALAPPDATA = $freshProfileRoot
    $global:runtimeStartupDockerCalls.Clear()
    $global:runtimeRejectDockerCalls = $true
    $freshInstallerGateFailure = ''
    try {
        . $startScriptPath -EnableHostRuntime -NoBrowser
    } catch {
        $freshInstallerGateFailure = $_.Exception.Message
    } finally {
        $global:runtimeRejectDockerCalls = $false
        $env:LOCALAPPDATA = $profileRoot
    }
    Assert-HaiRuntimeTest ($freshInstallerGateFailure -match 'no verified OS-enforced sandbox') 'A first-run -EnableHostRuntime request did not fail with the sandbox reason.'
    Assert-HaiRuntimeTest (-not (Test-Path -LiteralPath $freshProfileRoot)) 'A blocked first-run request created the HAI profile directory.'
    Assert-HaiRuntimeTest ($global:runtimeStartupDockerCalls.Count -eq 0) 'A blocked first-run request reached Docker or Compose.'

    $script:runtimeLifecycleProcesses = @()
    $script:runtimeLifecycleStops.Clear()
    $script:runtimeLifecycleTerminationAttempts.Clear()
    [IO.File]::WriteAllText($pidFile, '999999')
    $environmentBeforeInstallerGate = [IO.File]::ReadAllBytes($environmentFile)
    $script:runtimeLifecycleStops.Clear()
    $script:runtimeLifecycleTerminationAttempts.Clear()
    $global:runtimeStartupDockerCalls.Clear()
    $global:runtimeRejectDockerCalls = $true
    $installerGateFailure = ''
    try {
        . $startScriptPath -EnableHostRuntime -NoBrowser
    } catch {
        $installerGateFailure = $_.Exception.Message
    } finally {
        $global:runtimeRejectDockerCalls = $false
    }
    Assert-HaiRuntimeTest ($installerGateFailure -match 'no verified OS-enforced sandbox') 'The production installer did not report why -EnableHostRuntime is blocked.'
    Assert-HaiRuntimeTest ($global:runtimeStartupDockerCalls.Count -eq 0) 'The -EnableHostRuntime refusal reached Docker or Compose before failing.'
    Assert-HaiRuntimeTest ($global:runtimeStartProcessCalls -eq 0) 'The -EnableHostRuntime refusal launched an unrelated process.'
    Assert-HaiRuntimeTest ([Convert]::ToBase64String([IO.File]::ReadAllBytes($environmentFile)) -ceq [Convert]::ToBase64String($environmentBeforeInstallerGate)) 'The -EnableHostRuntime refusal changed the local environment file.'
    Assert-HaiRuntimeTest ($script:runtimeLifecycleStops.Count -eq 0 -and $script:runtimeLifecycleTerminationAttempts.Count -eq 0) 'The unsupported runtime request attempted to stop a process before refusing startup.'
    Assert-HaiRuntimeTest ((Get-Content -LiteralPath $pidFile -Raw) -ceq '999999') 'The unsupported runtime request changed the existing PID record.'

    $script:runtimeLifecycleProcesses = @(
        [pscustomobject]@{ ProcessId = 400; ParentProcessId = 1; Name = 'notepad.exe'; ExecutablePath = 'C:\Windows\System32\notepad.exe'; CommandLine = 'notepad'; Active = $true }
    )
    [IO.File]::WriteAllText($pidFile, '400')
    $installerGateFailure = ''
    try {
        . $startScriptPath -EnableHostRuntime -NoBrowser
    } catch {
        $installerGateFailure = $_.Exception.Message
    }
    Assert-HaiRuntimeTest ($installerGateFailure -match 'no verified OS-enforced sandbox') 'The installer did not fail closed with the sandbox reason for the unsupported runtime option.'
    Assert-HaiRuntimeTest ([IO.File]::ReadAllText($pidFile) -ceq '400') 'The installer removed an unverified legacy PID record.'
    Assert-HaiRuntimeTest ((Get-Process -Id 400 -ErrorAction SilentlyContinue) -ne $null) 'The installer stopped a process not positively identified as HAI-owned.'
    Assert-HaiRuntimeTest ($script:runtimeLifecycleStops.Count -eq 0 -and $script:runtimeLifecycleTerminationAttempts.Count -eq 0) 'The installer attempted to stop an unrelated process after the legacy PID failed verification.'
    Assert-HaiRuntimeTest ($global:runtimeStartupDockerCalls.Count -eq 0) 'The installer performed Docker or Compose work despite an unverified legacy PID.'
    Assert-HaiRuntimeTest ([Convert]::ToBase64String([IO.File]::ReadAllBytes($environmentFile)) -ceq [Convert]::ToBase64String($environmentBeforeInstallerGate)) 'A failed legacy-process verification changed the local environment file.'
    Remove-Item -LiteralPath $pidFile -Force
    $script:runtimeLifecycleProcesses = @()

    # Mock the Docker command and readiness probes; these cases never contact Docker or start containers.
    $script:runtimeComposeCalls = New-Object 'System.Collections.Generic.List[string]'
    $script:runtimeComposeUpExitCode = 0
    $script:runtimeComposeCleanupExitCode = 0
    $script:runtimeComposeStaleA2AStopExitCode = 0
    $script:runtimeComposeBaselineIds = @('aaaaaaaaaaaa')
    $script:runtimeComposeCurrentIds = @('aaaaaaaaaaaa', 'bbbbbbbbbbbb')
    $script:runtimeComposeHasAttemptedUp = $false
    $script:runtimeComposeBaselinePsExitCode = 0
    $script:runtimeComposeCleanupPsExitCode = 0
    $script:runtimeComposeWorkingDirectory = Get-HaiInstallRoot
    $script:runtimeComposeStoppedIds = New-Object 'System.Collections.Generic.List[string]'
    $script:runtimeComposeFailedStartupCleanupCalls = New-Object 'System.Collections.Generic.List[string]'
    $script:runtimeComposeSimulateConcurrentHealthyContainer = $false
    $script:runtimeComposeConcurrentHealthyContainerId = 'healthy-unrelated-hai-container'
    $script:runtimeComposeConcurrentHealthyContainer = $null
    $script:runtimeComposeIdentityFixtureEnabled = $false
    $script:runtimeComposeIdentityContainers = @()
    $script:runtimeReadinessFailure = $null
    $script:runtimeA2AReadinessFailure = $null
    $script:runtimeA2AReadinessUrls = New-Object 'System.Collections.Generic.List[string]'
    $script:runtimeA2AReadinessBridgeUrls = New-Object 'System.Collections.Generic.List[string]'
    function docker {
        $DockerArguments = @($args | ForEach-Object { [string]$_ })
        $script:runtimeComposeCalls.Add(($DockerArguments -join ' ')) | Out-Null
        $ambientOverrides = @(Get-ChildItem Env: | Where-Object { $_.Name -match '^(?i:HAI_A2A_)' -or $_.Name -ieq 'COMPOSE_PROFILES' })
        if ($ambientOverrides.Count -gt 0) {
            throw "Ambient Compose overrides were not suspended: $($ambientOverrides.Name -join ', ')"
        }
        if ($DockerArguments.Count -gt 0 -and $DockerArguments[0] -eq 'version') {
            $global:LASTEXITCODE = 0
            Write-Output '27.0.0'
        } elseif ($DockerArguments.Count -gt 1 -and $DockerArguments[0] -eq 'compose' -and $DockerArguments[1] -eq 'version') {
            $global:LASTEXITCODE = 0
            Write-Output 'Docker Compose version v2.test'
        } elseif ($script:runtimeComposeIdentityFixtureEnabled -and $DockerArguments.Count -gt 0 -and $DockerArguments[0] -eq 'ps') {
            $global:LASTEXITCODE = 0
            foreach ($container in $script:runtimeComposeIdentityContainers) {
                if ($DockerArguments -contains 'label=com.docker.compose.project.config_files') {
                    if (-not [string]::IsNullOrWhiteSpace($container.ConfigFiles)) { Write-Output $container.Id }
                } elseif ($DockerArguments -contains "label=com.docker.compose.project=$($container.Project)") {
                    Write-Output $container.Id
                }
            }
        } elseif ($script:runtimeComposeIdentityFixtureEnabled -and $DockerArguments.Count -gt 0 -and $DockerArguments[0] -eq 'inspect') {
            $containerId = [string]$DockerArguments[-1]
            $container = @($script:runtimeComposeIdentityContainers | Where-Object { $_.Id -ceq $containerId } | Select-Object -First 1)
            if ($container.Count -ne 1) { throw "Unknown identity fixture container: $containerId" }
            $labels = [ordered]@{
                'com.docker.compose.project' = $container[0].Project
                'com.docker.compose.project.working_dir' = $container[0].WorkingDirectory
                'com.docker.compose.project.config_files' = $container[0].ConfigFiles
                'com.docker.compose.service' = $container[0].Service
            }
            $global:LASTEXITCODE = 0
            Write-Output ("{0}|{1}" -f $container[0].Name, ($labels | ConvertTo-Json -Depth 4 -Compress))
        } elseif ($DockerArguments -contains 'ps' -and
            @($DockerArguments | Where-Object { $_ -match '^label=com\.docker\.compose\.project(?:\.config_files|=)' }).Count -gt 0) {
            $global:LASTEXITCODE = 0
        } elseif ($DockerArguments -contains 'ps') {
            $ids = if ($script:runtimeComposeHasAttemptedUp) { $script:runtimeComposeCurrentIds } else { $script:runtimeComposeBaselineIds }
            $global:LASTEXITCODE = if ($script:runtimeComposeHasAttemptedUp) { $script:runtimeComposeCleanupPsExitCode } else { $script:runtimeComposeBaselinePsExitCode }
            if ($global:LASTEXITCODE -eq 0) { foreach ($id in $ids) { Write-Output $id } }
        } elseif ($DockerArguments.Count -gt 0 -and $DockerArguments[0] -eq 'inspect') {
            $global:LASTEXITCODE = 0
            Write-Output $script:runtimeComposeWorkingDirectory
        } elseif ($DockerArguments -contains 'config') {
            $script:runtimeComposeHasAttemptedUp = $false
            $envFileArgumentIndex = [Array]::IndexOf($DockerArguments, '--env-file')
            if ($envFileArgumentIndex -lt 0 -or $envFileArgumentIndex + 1 -ge $DockerArguments.Count) {
                throw 'Compose config did not receive the selected environment file.'
            }
            $fixtureEnvironment = [IO.File]::ReadAllText($DockerArguments[$envFileArgumentIndex + 1])
            function Get-HaiRuntimeFixtureValue([string]$Name, [string]$DefaultValue) {
                $match = [Regex]::Match($fixtureEnvironment, '(?m)^' + [Regex]::Escape($Name) + '=(?<value>[^\r\n]*)')
                if (-not $match.Success) { return $DefaultValue }
                return $match.Groups['value'].Value.Trim().Trim('"').Trim("'")
            }
            $resolvedEnabled = Get-HaiRuntimeFixtureValue 'HAI_A2A_BRIDGE_ENABLED' 'false'
            $resolvedUrl = Get-HaiRuntimeFixtureValue 'HAI_A2A_BRIDGE_URL' 'http://127.0.0.1:8091/api/v1/a2a'
            $resolvedPort = Get-HaiRuntimeFixtureValue 'HAI_A2A_LOCAL_PORT' '8091'
            $resolvedConfiguration = @{
                services = @{
                    backend = @{ environment = @{ HAI_A2A_BRIDGE_ENABLED = $resolvedEnabled; HAI_A2A_BRIDGE_URL = $resolvedUrl } }
                    'a2a-gateway' = @{ ports = @(@{ target = 80; published = $resolvedPort; host_ip = '127.0.0.1'; protocol = 'tcp' }) }
                }
            }
            $global:LASTEXITCODE = 0
            Write-Output ($resolvedConfiguration | ConvertTo-Json -Depth 10 -Compress)
        } elseif ($DockerArguments -contains 'up') {
            $script:runtimeComposeHasAttemptedUp = $true
            if ($script:runtimeComposeSimulateConcurrentHealthyContainer) {
                $script:runtimeComposeConcurrentHealthyContainer = [pscustomobject]@{
                    Id = $script:runtimeComposeConcurrentHealthyContainerId
                    Health = 'healthy'
                    Owner = 'independent concurrent Compose invocation'
                }
                $script:runtimeComposeCurrentIds += $script:runtimeComposeConcurrentHealthyContainerId
            }
            $global:LASTEXITCODE = $script:runtimeComposeUpExitCode
        } elseif ($DockerArguments.Count -gt 0 -and $DockerArguments[0] -eq 'stop') {
            foreach ($id in @($DockerArguments | Select-Object -Skip 1)) { $script:runtimeComposeStoppedIds.Add($id) | Out-Null }
            if ($script:runtimeComposeHasAttemptedUp) { $script:runtimeComposeFailedStartupCleanupCalls.Add(($DockerArguments -join ' ')) | Out-Null }
            $global:LASTEXITCODE = $script:runtimeComposeCleanupExitCode
        } elseif ($DockerArguments -contains 'down' -or $DockerArguments -contains 'rm' -or $DockerArguments -contains 'kill') {
            if ($script:runtimeComposeHasAttemptedUp) { $script:runtimeComposeFailedStartupCleanupCalls.Add(($DockerArguments -join ' ')) | Out-Null }
            $global:LASTEXITCODE = $script:runtimeComposeCleanupExitCode
        } elseif ($DockerArguments -contains 'stop') {
            if ($script:runtimeComposeHasAttemptedUp) { $script:runtimeComposeFailedStartupCleanupCalls.Add(($DockerArguments -join ' ')) | Out-Null }
            if ($DockerArguments[-1] -eq 'a2a-gateway') {
                $global:LASTEXITCODE = $script:runtimeComposeStaleA2AStopExitCode
            } else {
                $global:LASTEXITCODE = $script:runtimeComposeCleanupExitCode
            }
        } else {
            throw "Unexpected mocked Docker operation: $($DockerArguments -join ' ')"
        }
    }
    function Wait-HaiReady {
        param([int]$TimeoutSeconds)
        if ($null -ne $script:runtimeReadinessFailure) { throw $script:runtimeReadinessFailure }
    }
    function Start-Sleep {
        param([int]$Seconds)
    }
    function Wait-HaiA2AReady {
        param([int]$TimeoutSeconds, [string]$BridgeUrl, [string]$AgentCardUrl)
        if ($null -ne $script:runtimeA2AReadinessFailure) { throw $script:runtimeA2AReadinessFailure }
        $script:runtimeA2AReadinessBridgeUrls.Add($BridgeUrl) | Out-Null
        $script:runtimeA2AReadinessUrls.Add($AgentCardUrl) | Out-Null
    }
    function Get-HaiComposeStartupFailure {
        param([int]$ExpectedUpExitCode, [string]$ReadinessFailure)
        $script:runtimeComposeCalls.Clear()
        $script:runtimeComposeUpExitCode = $ExpectedUpExitCode
        $script:runtimeComposeStaleA2AStopExitCode = 0
        $script:runtimeReadinessFailure = $ReadinessFailure
        $script:runtimeA2AReadinessFailure = $null
        [IO.File]::WriteAllText($environmentFile, "HAI_A2A_BRIDGE_ENABLED=false`r`n")
        return Invoke-HaiRuntimeStartAndCaptureFailure
    }
    function Invoke-HaiRuntimeStartAndCaptureFailure {
        try {
            Start-HaiComposeStack -ComposeArguments @('compose', '--env-file', $environmentFile) -HealthTimeoutSeconds 30
            return ''
        } catch {
            return $_.Exception.Message
        }
    }

    [IO.File]::WriteAllText($environmentFile, "HAI_A2A_BRIDGE_ENABLED=false`r`n")
    $failure = Get-HaiComposeStartupFailure -ExpectedUpExitCode 17 -ReadinessFailure $null
    Assert-HaiRuntimeTest ($failure -match 'exited with code 17' -and $failure -match 'No containers were stopped automatically') "Compose up failure did not preserve containers for explicit recovery. Failure='$failure'; calls='$($script:runtimeComposeCalls -join ' | ')'"
    Assert-HaiRuntimeTest ($script:runtimeComposeCalls.Count -eq 3 -and $script:runtimeComposeCalls[0] -match 'config --format json' -and $script:runtimeComposeCalls[1] -match '--profile local-a2a stop a2a-gateway' -and $script:runtimeComposeCalls[2] -match 'up -d --build' -and $script:runtimeComposeStoppedIds.Count -eq 0) 'Failed startup attempted automatic container cleanup that could race a concurrent Compose operation.'
    Assert-HaiRuntimeTest (@($script:runtimeAgentCardRequests | Where-Object { $_ -match '/\.well-known/agent-card\.json$' }).Count -eq 1) 'A disabled A2A bridge triggered Agent Card readiness validation.'

    $script:runtimeComposeCalls.Clear()
    $script:runtimeComposeUpExitCode = 29
    $script:runtimeComposeSimulateConcurrentHealthyContainer = $true
    $script:runtimeComposeCurrentIds = @('aaaaaaaaaaaa')
    [IO.File]::WriteAllText($environmentFile, "HAI_A2A_BRIDGE_ENABLED=true`r`nHAI_A2A_BRIDGE_OWNER_ID=operator@example.test`r`nHAI_A2A_BRIDGE_TOKEN=$('d' * 64)`r`nHAI_A2A_LOCAL_PORT=18091`r`nHAI_A2A_BRIDGE_URL=https://a2a.example.test/saved/rpc`r`n")
    $failure = Invoke-HaiRuntimeStartAndCaptureFailure
    Assert-HaiRuntimeTest ($script:runtimeComposeCurrentIds -contains $script:runtimeComposeConcurrentHealthyContainerId -and
        $script:runtimeComposeConcurrentHealthyContainer.Health -eq 'healthy' -and
        $script:runtimeComposeConcurrentHealthyContainer.Owner -eq 'independent concurrent Compose invocation') 'The concurrent healthy-container failure fixture did not create its unrelated container.'
    Assert-HaiRuntimeTest ($failure -match 'exited with code 29' -and $failure -match 'No containers were stopped automatically') "A failed startup with a concurrently healthy container did not report non-destructive recovery. Failure='$failure'"
    Assert-HaiRuntimeTest ($script:runtimeComposeCalls.Count -eq 2 -and $script:runtimeComposeCalls[1] -match '--profile local-a2a up' -and $script:runtimeComposeStoppedIds.Count -eq 0 -and $script:runtimeComposeFailedStartupCleanupCalls.Count -eq 0) 'Failed startup stopped or removed a concurrent healthy HAI container whose ownership by this startup attempt could not be proven.'
    $script:runtimeComposeSimulateConcurrentHealthyContainer = $false
    [IO.File]::WriteAllText($environmentFile, "HAI_A2A_BRIDGE_ENABLED=false`r`n")

    $script:runtimeComposeCalls.Clear()
    $script:runtimeComposeUpExitCode = 0
    $script:runtimeComposeCurrentIds = @('aaaaaaaaaaaa', 'bbbbbbbbbbbb')
    $script:runtimeComposeUpExitCode = 17
    $failure = Invoke-HaiRuntimeStartAndCaptureFailure
    Assert-HaiRuntimeTest ($failure -match 'No containers were stopped automatically' -and $script:runtimeComposeCalls.Count -eq 3 -and $script:runtimeComposeStoppedIds.Count -eq 0) 'A container that appeared during another Compose operation was stopped during failed startup recovery.'

    $failure = Get-HaiComposeStartupFailure -ExpectedUpExitCode 0 -ReadinessFailure 'fixture readiness timeout'
    Assert-HaiRuntimeTest ($failure -match 'fixture readiness timeout' -and $failure -match 'No containers were stopped automatically') 'Readiness failure did not preserve containers for safe, explicit recovery.'
    Assert-HaiRuntimeTest ($script:runtimeComposeCalls.Count -eq 3 -and $script:runtimeComposeStoppedIds.Count -eq 0) 'Readiness failure attempted automatic cleanup while another Compose operation could own a container.'

    $script:runtimeComposeCalls.Clear()
    $script:runtimeComposeUpExitCode = 0
    $script:runtimeComposeCleanupExitCode = 0
    $script:runtimeComposeStaleA2AStopExitCode = 0
    $script:runtimeReadinessFailure = $null
    $script:runtimeA2AReadinessFailure = $null
    [IO.File]::WriteAllText($environmentFile, "HAI_A2A_BRIDGE_ENABLED=false`r`nHAI_A2A_LOCAL_PORT=18191`r`nHAI_A2A_BRIDGE_URL=https://a2a.example.test/configured/rpc`r`n")
    $env:HAI_A2A_BRIDGE_ENABLED = 'true'
    $env:HAI_A2A_BRIDGE_URL = 'https://ambient.example.test/override'
    $env:HAI_A2A_LOCAL_PORT = '28765'
    $env:COMPOSE_PROFILES = 'local-a2a'
    Start-HaiComposeStack -ComposeArguments @('compose', '--env-file', $environmentFile) -HealthTimeoutSeconds 30
    Assert-HaiRuntimeTest ($script:runtimeComposeCalls.Count -eq 3) 'Successful disabled startup issued unexpected Compose operations.'
    Assert-HaiRuntimeTest ($script:runtimeComposeCalls[0] -match 'config --format json' -and $script:runtimeComposeCalls[1] -match '--profile local-a2a stop a2a-gateway' -and $script:runtimeComposeCalls[2] -notmatch '--profile local-a2a' -and $script:runtimeComposeCalls[2] -match 'up -d --build') 'The disabled transition stopped core services or started the optional A2A profile.'
    Assert-HaiRuntimeTest ($env:HAI_A2A_BRIDGE_ENABLED -ceq 'true' -and $env:HAI_A2A_BRIDGE_URL -ceq 'https://ambient.example.test/override' -and $env:HAI_A2A_LOCAL_PORT -ceq '28765' -and $env:COMPOSE_PROFILES -ceq 'local-a2a') 'Compose environment overrides were not restored after startup.'
    Assert-HaiRuntimeTest (@($script:runtimeAgentCardRequests | Where-Object { $_ -match '/\.well-known/agent-card\.json$' }).Count -eq 1 -and $script:runtimeA2AReadinessUrls.Count -eq 0) 'A disabled A2A bridge triggered Agent Card readiness validation.'

    $script:runtimeComposeCalls.Clear()
    [IO.File]::WriteAllText($environmentFile, "HAI_A2A_BRIDGE_ENABLED=true`r`nHAI_A2A_LOCAL_PORT=18091`r`nHAI_A2A_BRIDGE_URL=https://a2a.example.test/configured/rpc`r`n")
    Start-HaiComposeStack -ComposeArguments @('compose', '--env-file', $environmentFile) -HealthTimeoutSeconds 30
    Assert-HaiRuntimeTest ($script:runtimeComposeCalls.Count -eq 2 -and $script:runtimeComposeCalls[0] -match 'config --format json' -and $script:runtimeComposeCalls[1] -match '--profile local-a2a up') 'An explicitly enabled A2A bridge profile was not included in Compose startup.'
    Assert-HaiRuntimeTest ($script:runtimeA2AReadinessUrls.Count -eq 1) 'An explicitly enabled A2A bridge did not receive Agent Card readiness validation after dashboard readiness.'
    Assert-HaiRuntimeTest ($script:runtimeA2AReadinessUrls[-1] -ceq 'http://127.0.0.1:18091/.well-known/agent-card.json' -and $script:runtimeA2AReadinessBridgeUrls[-1] -ceq 'https://a2a.example.test/configured/rpc') 'Startup readiness did not use the Compose-published probe port and configured advertised bridge URL.'

    $script:runtimeComposeCalls.Clear()
    $script:runtimeA2AReadinessFailure = 'The local A2A Agent Card was unavailable or invalid.'
    $failure = Invoke-HaiRuntimeStartAndCaptureFailure
    Assert-HaiRuntimeTest ($failure -match 'Agent Card was unavailable or invalid' -and $failure -match 'No containers were stopped automatically') "Failed A2A readiness did not report safe, explicit recovery. Failure='$failure'"
    Assert-HaiRuntimeTest ($script:runtimeComposeCalls.Count -eq 2 -and $script:runtimeComposeCalls[0] -match 'config --format json' -and $script:runtimeComposeCalls[1] -match '--profile local-a2a up' -and $script:runtimeComposeStoppedIds.Count -eq 0) 'Invalid Agent Card readiness attempted automatic container cleanup.'

    $script:runtimeComposeCalls.Clear()
    $script:runtimeA2AReadinessFailure = $null
    $script:runtimeComposeStaleA2AStopExitCode = 23
    [IO.File]::WriteAllText($environmentFile, "HAI_A2A_BRIDGE_ENABLED=false`r`n")
    $failure = Invoke-HaiRuntimeStartAndCaptureFailure
    Assert-HaiRuntimeTest ($failure -match 'stale service could not be stopped' -and $failure -match 'core services were not stopped') 'A failed stale-bridge stop did not fail closed without stopping core HAI.'
    Assert-HaiRuntimeTest ($script:runtimeComposeCalls.Count -eq 2 -and $script:runtimeComposeCalls[1] -match 'stop a2a-gateway') 'HAI startup continued or issued a broad stop when stale optional-bridge cleanup failed.'

    foreach ($entry in @(Get-ChildItem Env: | Where-Object { $_.Name -match '^(?i:HAI_A2A_)' -or $_.Name -ieq 'COMPOSE_PROFILES' })) {
        Remove-Item -LiteralPath ("Env:" + $entry.Name) -Force
    }
    $startLifecycleLines = @(
        'COMPOSE_PROJECT_NAME=018-hai',
        'BACKEND_API_SHARED_KEY=fixture-backend-key-not-a-real-secret',
        'JWT_SECRET=fixture-jwt-secret-not-a-real-secret',
        'HAI_MEMORY_ENCRYPTION_KEY=fixture-memory-key-not-a-real-secret',
        'HAI_APPROVAL_PROOF_SIGNING_KEY=fixture-approval-key-not-a-real-secret',
        'DB_PASSWORD=fixture-db-password-not-a-real-secret',
        'BACKEND_DB_PASSWORD=fixture-backend-db-password-not-a-real-secret',
        'FIRST_RUN_ADMIN_EMAIL=operator@example.test',
        'FIRST_RUN_ADMIN_PASSWORD=FixturePasswordForLifecycle123!',
        'HAI_OPENCLAW_MAINTENANCE_ENABLED=false',
        'KEEP_UNRELATED_SETTING=preserve-me'
    ) + $savedA2ALines
    [IO.File]::WriteAllLines($environmentFile, [string[]]$startLifecycleLines)

    $script:runtimeComposeIdentityFixtureEnabled = $true
    $currentComposePath = [IO.Path]::GetFullPath((Get-HaiComposeFile))
    $currentInstallPath = [IO.Path]::GetFullPath((Get-HaiInstallRoot))
    $otherInstallPath = [IO.Path]::GetFullPath((Join-Path $temporaryRoot 'other-install'))
    $script:runtimeComposeIdentityContainers = @(
        [pscustomobject]@{ Id = 'hai-idp'; Name = '/018-hai-idp'; Project = '018-hai'; WorkingDirectory = $currentInstallPath; ConfigFiles = $currentComposePath; Service = 'idp' },
        [pscustomobject]@{ Id = 'hai-backend'; Name = '/018-hai-backend'; Project = '018-hai'; WorkingDirectory = $currentInstallPath; ConfigFiles = $currentComposePath; Service = 'backend' },
        [pscustomobject]@{ Id = 'hai-frontend'; Name = '/018-hai-frontend'; Project = '018-hai'; WorkingDirectory = $currentInstallPath; ConfigFiles = $currentComposePath; Service = 'frontend' }
    )
    $script:runtimeComposeCalls.Clear()
    Assert-HaiSingleInstallation
    Assert-HaiRuntimeTest (-not ($script:runtimeComposeCalls -match '\b(up|stop|down|rm|kill)\b')) 'The single-stack ownership check mutated Docker while inspecting a valid multi-service HAI stack.'

    $script:runtimeComposeCalls.Clear()
    for ($startAttempt = 1; $startAttempt -le 2; $startAttempt++) {
        Assert-HaiSingleInstallation
        Start-HaiComposeStack -ComposeArguments (Get-HaiComposeArguments) -HealthTimeoutSeconds 30
    }
    $repeatedStartUpCalls = @($script:runtimeComposeCalls | Where-Object { $_ -match '\bup -d --build$' })
    Assert-HaiRuntimeTest ($repeatedStartUpCalls.Count -eq 2 -and
        @($repeatedStartUpCalls | Where-Object { $_ -notmatch '--project-name 018-hai' }).Count -eq 0 -and
        -not ($script:runtimeComposeCalls -match '\b(down|rm|kill)\b')) 'Repeated Start did not reuse the validated 018-hai Compose project or attempted destructive cleanup.'
    $script:runtimeComposeCalls.Clear()

    $script:runtimeComposeIdentityContainers = @(
        [pscustomobject]@{ Id = 'hai-shadow'; Name = '/018-hai-idp'; Project = 'hai-shadow'; WorkingDirectory = $otherInstallPath; ConfigFiles = (Join-Path $otherInstallPath 'docker-compose.local.yml'); Service = 'idp' }
    )
    $identityFailure = ''
    try { Assert-HaiSingleInstallation } catch { $identityFailure = $_.Exception.Message }
    Assert-HaiRuntimeTest ($identityFailure -match 'already managed from' -and $identityFailure -match 'No containers or data were changed') 'A HAI stack under a different Compose project name escaped the single-installation check.'

    $script:runtimeComposeIdentityContainers = @(
        [pscustomobject]@{ Id = 'hai-second-project'; Name = '/018-hai-backend'; Project = 'hai-second-project'; WorkingDirectory = $currentInstallPath; ConfigFiles = $currentComposePath; Service = 'backend' }
    )
    $identityFailure = ''
    try { Assert-HaiSingleInstallation } catch { $identityFailure = $_.Exception.Message }
    Assert-HaiRuntimeTest ($identityFailure -match "already has a Compose stack named 'hai-second-project'" -and $identityFailure -match 'no containers or data were changed') 'A second Compose project using the same HAI installation path was not rejected.'

    $script:runtimeComposeIdentityContainers = @(
        [pscustomobject]@{ Id = 'hai-partial-without-owner'; Name = '/018-hai-backend'; Project = ''; WorkingDirectory = ''; ConfigFiles = $currentComposePath; Service = 'backend' }
    )
    $script:runtimeComposeCalls.Clear()
    $identityFailure = ''
    try { Assert-HaiSingleInstallation } catch { $identityFailure = $_.Exception.Message }
    Assert-HaiRuntimeTest ($identityFailure -match 'Could not verify the Compose project and owner path' -and
        -not ($script:runtimeComposeCalls -match '\b(up|stop|down|rm|kill)\b')) 'A partial HAI container with missing ownership labels was not rejected safely without a Docker mutation.'

    $script:runtimeComposeIdentityContainers = @(
        [pscustomobject]@{ Id = 'unrelated-compose'; Name = '/unrelated-backend'; Project = 'unrelated'; WorkingDirectory = $otherInstallPath; ConfigFiles = (Join-Path $otherInstallPath 'compose.yaml'); Service = 'backend' }
    )
    Assert-HaiSingleInstallation
    foreach ($collision in @(
        [pscustomobject]@{ Id = 'same-project-foreign-config'; Name = '/018-hai-custom'; Project = '018-hai'; WorkingDirectory = $currentInstallPath; ConfigFiles = (Join-Path $currentInstallPath 'compose.yaml'); Service = 'custom-service' },
        [pscustomobject]@{ Id = 'same-project-no-config'; Name = '/018-hai-custom'; Project = '018-hai'; WorkingDirectory = $currentInstallPath; ConfigFiles = ''; Service = 'custom-service' },
        [pscustomobject]@{ Id = 'same-project-foreign-root'; Name = '/018-hai-custom'; Project = '018-hai'; WorkingDirectory = $otherInstallPath; ConfigFiles = (Join-Path $otherInstallPath 'compose.yaml'); Service = 'custom-service' }
    )) {
        $script:runtimeComposeIdentityContainers = @($collision)
        $script:runtimeComposeCalls.Clear()
        $identityFailure = ''
        try { Assert-HaiSingleInstallation } catch { $identityFailure = $_.Exception.Message }
        Assert-HaiRuntimeTest (-not [string]::IsNullOrWhiteSpace($identityFailure) -and
            -not ($script:runtimeComposeCalls -match '\b(up|stop|down|rm|kill)\b')) "A foreign container selected by the configured Compose project escaped ownership validation: $($collision.Id)"
    }
    $script:runtimeComposeIdentityFixtureEnabled = $false
    $launcherAgentCard = [pscustomobject]@{
        name = 'HAI controlled planning bridge'
        supportedInterfaces = @([pscustomobject]@{
            protocolBinding = 'JSONRPC'
            protocolVersion = '1.0'
            url = 'https://a2a.example.test/saved/rpc'
        })
    }
    $script:runtimeAgentCardResponse = [pscustomobject]@{ StatusCode = 200; Content = $launcherAgentCard | ConvertTo-Json -Depth 5 -Compress }
    $global:runtimeAgentCardResponse = $script:runtimeAgentCardResponse
    $script:runtimeAgentCardRequests.Clear()
    $script:runtimeComposeCalls.Clear()
    Assert-HaiRuntimeTest (@($script:runtimeLifecycleProcesses | Where-Object { $_.Active }).Count -eq 0) 'The mocked native-process fixture leaked into the launcher lifecycle checks.'
    $originalComposeProjectName = $env:COMPOSE_PROJECT_NAME
    $env:COMPOSE_PROJECT_NAME = 'unrelated-project'
    try {
        . $startScriptPath -NoBrowser -HealthTimeoutSeconds 30
        $composeStartCalls = @($script:runtimeComposeCalls | Where-Object { $_ -match '(^|\s)compose(\s|$)' -and $_ -notmatch '(^|\s)compose version(\s|$)' })
        Assert-HaiRuntimeTest ($composeStartCalls.Count -gt 0) 'An ordinary launcher start did not invoke Docker Compose.'
        foreach ($composeStartCall in $composeStartCalls) {
            Assert-HaiRuntimeTest ($composeStartCall -match '(^|\s)--project-name 018-hai(\s|$)') "An ordinary launcher start did not pin the project name from hai.env: $composeStartCall"
        }
        Assert-HaiRuntimeTest ($env:COMPOSE_PROJECT_NAME -ceq 'unrelated-project') 'An ordinary launcher start changed the caller process project-name setting.'
    } finally {
        if ($null -eq $originalComposeProjectName) {
            Remove-Item Env:COMPOSE_PROJECT_NAME -ErrorAction SilentlyContinue
        } else {
            $env:COMPOSE_PROJECT_NAME = $originalComposeProjectName
        }
    }

    $script:runtimeComposeIdentityFixtureEnabled = $true
    $script:runtimeComposeIdentityContainers = @(
        [pscustomobject]@{ Id = 'foreign-hai-stack'; Name = '/018-hai-backend'; Project = '018-hai'; WorkingDirectory = $otherInstallPath; ConfigFiles = (Join-Path $otherInstallPath 'docker-compose.local.yml'); Service = 'backend' }
    )
    $script:runtimeComposeCalls.Clear()
    $openedBeforeOwnershipRejection = $global:runtimeOpenedUrls.Count
    $ownershipFailure = ''
    try {
        $global:runtimeAllowStartProcess = $true
        . $startScriptPath -HealthTimeoutSeconds 30
    } catch {
        $ownershipFailure = $_.Exception.Message
    } finally {
        $global:runtimeAllowStartProcess = $false
    }
    Assert-HaiRuntimeTest ($ownershipFailure -match 'already managed from' -and
        $ownershipFailure -match 'No containers or data were changed' -and
        -not ($script:runtimeComposeCalls -match '\b(up|stop|down|rm|kill)\b') -and
        $global:runtimeOpenedUrls.Count -eq $openedBeforeOwnershipRejection) 'Start did not reject a foreign-owned stack before Compose mutation or browser launch.'
    $script:runtimeComposeIdentityContainers = @(
        [pscustomobject]@{ Id = 'hai-idp'; Name = '/018-hai-idp'; Project = '018-hai'; WorkingDirectory = $currentInstallPath; ConfigFiles = $currentComposePath; Service = 'idp' },
        [pscustomobject]@{ Id = 'hai-backend'; Name = '/018-hai-backend'; Project = '018-hai'; WorkingDirectory = $currentInstallPath; ConfigFiles = $currentComposePath; Service = 'backend' },
        [pscustomobject]@{ Id = 'hai-frontend'; Name = '/018-hai-frontend'; Project = '018-hai'; WorkingDirectory = $currentInstallPath; ConfigFiles = $currentComposePath; Service = 'frontend' }
    )
    $script:runtimeComposeCalls.Clear()
    $global:runtimeOpenedUrls.Clear()
    $global:runtimeAllowStartProcess = $true
    try {
        . $startScriptPath -HealthTimeoutSeconds 30
    } finally {
        $global:runtimeAllowStartProcess = $false
    }
    Assert-HaiRuntimeTest (@($global:runtimeOpenedUrls | Where-Object { $_ -ceq $dashboardUrl }).Count -eq 1) 'Successful Start did not open the exact /control-center route after readiness succeeded.'
    $global:runtimeOpenedUrls.Clear()
    $script:runtimeComposeIdentityFixtureEnabled = $false
    $afterOmittedStart = [IO.File]::ReadAllText($environmentFile)
    foreach ($savedA2ALine in $savedA2ALines) {
        Assert-HaiRuntimeTest ($afterOmittedStart.Contains($savedA2ALine)) 'An actual launcher start with the A2A switch omitted changed a saved A2A setting or token.'
    }
    Assert-HaiRuntimeTest ($afterOmittedStart.Contains('KEEP_UNRELATED_SETTING=preserve-me')) 'An actual ordinary launcher start changed an unrelated environment setting.'
    Assert-HaiRuntimeTest (@($script:runtimeAgentCardRequests | Where-Object { $_ -match '/\.well-known/agent-card\.json$' }).Count -eq 2) 'Successful launcher starts did not exercise the enabled Agent Card readiness path, or the rejected owner-mismatch start reached readiness.'

    $script:runtimeComposeCalls.Clear()
    . $startScriptPath -EnableA2ABridge -NoBrowser -HealthTimeoutSeconds 30
    $afterExplicitTrueStart = [IO.File]::ReadAllText($environmentFile)
    foreach ($savedA2ALine in $savedA2ALines) {
        Assert-HaiRuntimeTest ($afterExplicitTrueStart.Contains($savedA2ALine)) 'An actual explicit-true launcher start changed valid saved A2A configuration or rotated its token.'
    }

    $script:runtimeComposeCalls.Clear()
    $script:runtimeComposeStaleA2AStopExitCode = 0
    . $startScriptPath -EnableA2ABridge:$false -NoBrowser -HealthTimeoutSeconds 30
    $afterExplicitFalseStart = [IO.File]::ReadAllText($environmentFile)
    $afterExplicitFalseLines = [IO.File]::ReadAllLines($environmentFile)
    $launcherComposeActions = @($script:runtimeComposeCalls | Where-Object { $_ -match 'config --format json|up -d --build|stop' })
    Assert-HaiRuntimeTest ($afterExplicitFalseLines -contains 'HAI_A2A_BRIDGE_ENABLED=false' -and
        $afterExplicitFalseLines -contains 'HAI_A2A_BRIDGE_OWNER_ID=' -and
        $afterExplicitFalseLines -contains 'HAI_A2A_BRIDGE_TOKEN=' -and
        $afterExplicitFalseLines -contains 'HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED=false' -and
        $afterExplicitFalseLines -contains 'KEEP_UNRELATED_SETTING=preserve-me') 'An actual explicit-false launcher start did not clear A2A credentials while preserving unrelated settings.'
    Assert-HaiRuntimeTest ($launcherComposeActions.Count -eq 3 -and
        $launcherComposeActions[0] -match 'config --format json' -and
        $launcherComposeActions[1] -match '--profile local-a2a stop a2a-gateway' -and
        $launcherComposeActions[2] -notmatch '--profile local-a2a' -and
        $launcherComposeActions[2] -match 'up -d --build') 'An actual explicit-false launcher start did not stop only the stale optional bridge before starting core HAI.'

    Remove-Item -LiteralPath Function:\Get-CimInstance -Force
    Remove-Item -LiteralPath Function:\Invoke-HaiVerifiedProcessTermination -Force
    Remove-Item -LiteralPath Function:\Get-Process -Force
    Remove-Item -LiteralPath Function:\Stop-Process -Force
    Remove-Item -LiteralPath Function:\Wait-Process -Force
    . (Join-Path $windowsDirectory 'Hai-InstallerSupport.ps1')
    $nativePowerShell = Join-Path $PSHOME 'pwsh.exe'
    if (-not (Test-Path -LiteralPath $nativePowerShell -PathType Leaf)) {
        $nativePowerShell = Join-Path $PSHOME 'powershell.exe'
    }
    if (-not (Test-Path -LiteralPath $nativePowerShell -PathType Leaf)) {
        throw 'Could not locate the current PowerShell executable for the native process-handle regression test.'
    }
    $nativeChildStart = New-Object System.Diagnostics.ProcessStartInfo
    $nativeChildStart.FileName = $nativePowerShell
    $nativeChildStart.Arguments = '-NoProfile -NonInteractive -WindowStyle Hidden -Command "Start-Sleep -Seconds 60"'
    $nativeChildStart.UseShellExecute = $false
    $nativeChildStart.CreateNoWindow = $true
    $nativeChildStart.WindowStyle = [Diagnostics.ProcessWindowStyle]::Hidden
    $nativeChild = [Diagnostics.Process]::Start($nativeChildStart)
    try {
        $nativeChildSnapshot = $null
        $deadline = [DateTime]::UtcNow.AddSeconds(10)
        do {
            $nativeChildSnapshot = Get-CimInstance -ClassName Win32_Process -Filter "ProcessId = $($nativeChild.Id)" -ErrorAction Stop
            if ($null -eq $nativeChildSnapshot) { Start-Sleep -Milliseconds 100 }
        } while ($null -eq $nativeChildSnapshot -and [DateTime]::UtcNow -lt $deadline)
        Assert-HaiRuntimeTest ($null -ne $nativeChildSnapshot -and $nativeChildSnapshot.ProcessId -eq $nativeChild.Id) 'Could not inspect the controlled native child process.'
        $wrongImageRejected = $false
        try {
            Invoke-HaiVerifiedProcessTermination -ProcessId ([uint32]$nativeChild.Id) `
                -ExpectedExecutablePath (Join-Path $temporaryRoot 'not-the-child.exe') `
                -ExpectedCreationFileTimeUtc (ConvertTo-HaiProcessCreationFileTime -CreationDate $nativeChildSnapshot.CreationDate) `
                -TimeoutMilliseconds 10000 | Out-Null
        } catch {
            $wrongImageRejected = $_.Exception.Message -match 'image changed before shutdown'
        }
        Assert-HaiRuntimeTest ($wrongImageRejected -and -not $nativeChild.HasExited) 'The native process handle did not refuse an image-path mismatch without stopping the child.'
        $wrongCreationRejected = $false
        try {
            Invoke-HaiVerifiedProcessTermination -ProcessId ([uint32]$nativeChild.Id) `
                -ExpectedExecutablePath ([string]$nativeChildSnapshot.ExecutablePath) `
                -ExpectedCreationFileTimeUtc ((ConvertTo-HaiProcessCreationFileTime -CreationDate $nativeChildSnapshot.CreationDate) + 10000000) `
                -TimeoutMilliseconds 10000 | Out-Null
        } catch {
            $wrongCreationRejected = $_.Exception.Message -match 'process ID was reused'
        }
        Assert-HaiRuntimeTest ($wrongCreationRejected -and -not $nativeChild.HasExited) 'The native process handle did not refuse a creation-time mismatch without stopping the child.'
        Stop-HaiProcessAndWait -ProcessSnapshot $nativeChildSnapshot -TimeoutSeconds 10
        Assert-HaiRuntimeTest ($nativeChild.WaitForExit(1000)) 'The verified process-handle shutdown returned before the controlled child exited.'
    } finally {
        if (-not $nativeChild.HasExited) {
            $nativeChild.Kill()
            $nativeChild.WaitForExit(5000)
        }
        $nativeChild.Dispose()
    }

    [IO.File]::WriteAllText($workerScript, 'Start-Sleep -Seconds 60')
    [IO.File]::WriteAllText($environmentFile, ($runtimeConfiguration -join "`r`n") + "`r`n", [Text.Encoding]::ASCII)
    $runtimeEnvironmentBeforeNativeGate = [IO.File]::ReadAllBytes($environmentFile)
    $nativeLauncher = $null
    try {
        $nativeLauncherStart = New-Object System.Diagnostics.ProcessStartInfo
        $nativeLauncherStart.FileName = Join-Path $env:WINDIR 'System32\WindowsPowerShell\v1.0\powershell.exe'
        $nativeLauncherStart.Arguments = '-NoProfile -ExecutionPolicy Bypass -File "{0}" -EnvFile "{1}"' -f $workerScript, $environmentFile
        $nativeLauncherStart.UseShellExecute = $false
        $nativeLauncherStart.CreateNoWindow = $true
        $nativeLauncherStart.WindowStyle = [Diagnostics.ProcessWindowStyle]::Hidden
        $nativeLauncher = [Diagnostics.Process]::Start($nativeLauncherStart)
        [IO.File]::WriteAllText($pidFile, [string]$nativeLauncher.Id)

        $nativeLauncherSnapshot = $null
        $deadline = [DateTime]::UtcNow.AddSeconds(10)
        do {
            $nativeLauncherSnapshot = Get-CimInstance -ClassName Win32_Process -Filter "ProcessId = $($nativeLauncher.Id)" -ErrorAction Stop
            if ($null -eq $nativeLauncherSnapshot) { Start-Sleep -Milliseconds 100 }
        } while ($null -eq $nativeLauncherSnapshot -and [DateTime]::UtcNow -lt $deadline)
        Assert-HaiRuntimeTest ($nativeLauncherSnapshot.Name -ieq 'powershell.exe' -and $nativeLauncherSnapshot.CommandLine.Contains($workerScript)) 'The controlled launcher fixture did not match the installed command line.'

        $global:runtimeStartupDockerCalls.Clear()
        $runtimeGateFailure = ''
        try {
            . $startScriptPath -EnableHostRuntime -NoBrowser
        } catch {
            $runtimeGateFailure = $_.Exception.Message
        }
        Assert-HaiRuntimeTest ($runtimeGateFailure -match 'no verified OS-enforced sandbox') 'The live installer path did not report the host-runtime safety gate.'
        Assert-HaiRuntimeTest (-not $nativeLauncher.WaitForExit(1000)) 'A host-runtime request stopped an existing process before refusing the unsupported feature.'
        Assert-HaiRuntimeTest (([IO.File]::ReadAllText($pidFile)) -ceq [string]$nativeLauncher.Id) 'A refused host-runtime request changed the existing PID record.'
        Assert-HaiRuntimeTest ([Convert]::ToBase64String([IO.File]::ReadAllBytes($environmentFile)) -ceq [Convert]::ToBase64String($runtimeEnvironmentBeforeNativeGate)) 'The live installer path changed the environment while refusing runtime enablement.'
        Assert-HaiRuntimeTest ($global:runtimeStartupDockerCalls.Count -eq 0) 'The live installer path reached Docker before refusing runtime enablement.'
    } finally {
        foreach ($child in @($nativeLauncher)) {
            if ($null -ne $child) {
                try {
                    if (-not $child.HasExited) { $child.Kill(); $child.WaitForExit(5000) }
                } catch { }
                $child.Dispose()
            }
        }
    }

    Write-Output 'Windows runtime lifecycle contract tests passed, including exact dashboard routing, Open readiness, startup ownership gates, PID-reuse, orphan status, and fail-closed host-runtime refusal.'
} finally {
    $env:LOCALAPPDATA = $originalLocalAppData
    if ($null -eq $originalSandboxOverride) {
        Remove-Item Env:HAI_HOST_RUNTIME_SANDBOX_VERIFIED -ErrorAction SilentlyContinue
    } else {
        $env:HAI_HOST_RUNTIME_SANDBOX_VERIFIED = $originalSandboxOverride
    }
    foreach ($entry in @(Get-ChildItem Env: | Where-Object { $_.Name -match '^(?i:HAI_A2A_)' -or $_.Name -ieq 'COMPOSE_PROFILES' })) {
        [Environment]::SetEnvironmentVariable($entry.Name, $null, 'Process')
    }
    foreach ($name in $originalComposeA2AEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable([string]$name, [string]$originalComposeA2AEnvironment[$name], 'Process')
    }
    Remove-Variable -Name runtimeStartupDockerCalls,runtimeRejectDockerCalls,runtimeStartProcessCalls,runtimeAllowStartProcess,runtimeOpenedUrls -Scope Global -ErrorAction SilentlyContinue
    $resolvedTemporaryRoot = [IO.Path]::GetFullPath($temporaryRoot)
    $resolvedTempParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if ($resolvedTemporaryRoot.StartsWith($resolvedTempParent, [StringComparison]::OrdinalIgnoreCase) -and
        [IO.Path]::GetFileName($resolvedTemporaryRoot) -match '\Ahai-runtime-lifecycle-[a-f0-9]{32}\z' -and
        (Test-Path -LiteralPath $resolvedTemporaryRoot -PathType Container)) {
        Remove-Item -LiteralPath $resolvedTemporaryRoot -Recurse -Force
    }
}
