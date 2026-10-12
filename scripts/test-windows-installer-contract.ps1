[CmdletBinding()]
param(
    [switch]$SkipPayload,
    [switch]$OpenClawMaintenanceOnly,
    [switch]$UninstallRuntimeOnly
)

$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$buildScript = Join-Path $PSScriptRoot "build-windows-installer.ps1"
$sourceSelectionScript = Join-Path $PSScriptRoot "windows-installer-payload-selection.ps1"
$sourceSelectionTest = Join-Path $PSScriptRoot "test-windows-installer-source-selection.ps1"
$maintenanceMigrationTest = Join-Path $PSScriptRoot "test-openclaw-maintenance-migration.ps1"
$maintenanceLauncherTest = Join-Path $PSScriptRoot "test-openclaw-maintenance-launcher.ps1"
$maintenancePromotionTest = Join-Path $PSScriptRoot "test-windows-installer-update-promotion.ps1"
$payloadValidationTest = Join-Path $PSScriptRoot "test-windows-installer-payload-validation.ps1"
$runtimeLifecycleTest = Join-Path $PSScriptRoot "test-windows-runtime-lifecycle.ps1"
$runtimeHealthTest = Join-Path $PSScriptRoot "test-windows-runtime-health.ps1"
$uninstallMaintenanceRecoveryTest = Join-Path $PSScriptRoot "test-windows-uninstall-maintenance-recovery.ps1"
$installerScript = Join-Path $repositoryRoot "installer\windows\HAI.iss"
$supportScript = Join-Path $repositoryRoot "installer\windows\Hai-InstallerSupport.ps1"
$maintenanceSupportScript = Join-Path $repositoryRoot "installer\windows\Hai-OpenClawMaintenance.ps1"
$maintenanceTaskManagerScript = Join-Path $repositoryRoot "installer\windows\Manage-HAI-OpenClawMaintenanceTask.ps1"
$maintenanceLauncherScript = Join-Path $repositoryRoot "installer\windows\Run-HAI-OpenClawMaintenance.ps1"
$initializerScript = Join-Path $PSScriptRoot "initialize-windows.ps1"
$runtimeDatabaseRoleScript = Join-Path $PSScriptRoot "provision-runtime-db-role.ps1"
$runtimeRoleProvisionerScript = Join-Path $repositoryRoot "services\postgres-runtime-role\provision-runtime-role.sh"
$documentation = Join-Path $repositoryRoot "docs\windows-installer.md"

foreach ($requiredFile in @($buildScript, $sourceSelectionScript, $sourceSelectionTest, $maintenanceMigrationTest, $maintenanceLauncherTest, $maintenancePromotionTest, $payloadValidationTest, $runtimeLifecycleTest, $runtimeHealthTest, $uninstallMaintenanceRecoveryTest, $installerScript, $supportScript, $initializerScript, $runtimeDatabaseRoleScript, $runtimeRoleProvisionerScript, $documentation, (Join-Path $repositoryRoot 'installer\windows\Hai-WindowsExecutable.ps1'), (Join-Path $repositoryRoot 'installer\windows\Manage-HAI-OpenClawMaintenanceTask.ps1'))) {
    if (-not (Test-Path -LiteralPath $requiredFile -PathType Leaf)) {
        throw "Windows installer contract is missing: $requiredFile"
    }
}

$build = [IO.File]::ReadAllText($buildScript)
$sourceSelection = [IO.File]::ReadAllText($sourceSelectionScript)
$installer = [IO.File]::ReadAllText($installerScript)
$support = [IO.File]::ReadAllText($supportScript)
$maintenanceSupport = [IO.File]::ReadAllText($maintenanceSupportScript)
$maintenanceTaskManager = [IO.File]::ReadAllText($maintenanceTaskManagerScript)
$maintenanceLauncher = [IO.File]::ReadAllText($maintenanceLauncherScript)
$initializer = [IO.File]::ReadAllText($initializerScript)
$runtimeDatabaseRole = [IO.File]::ReadAllText($runtimeDatabaseRoleScript)
$runtimeRoleProvisioner = [IO.File]::ReadAllText($runtimeRoleProvisionerScript)
$composeConfiguration = [IO.File]::ReadAllText((Join-Path $repositoryRoot "docker-compose.local.yml"))
$gatewayConfig = [IO.File]::ReadAllText((Join-Path $repositoryRoot "nginx-config\nginx.conf.template"))
$maintenanceDocumentation = [IO.File]::ReadAllText((Join-Path $repositoryRoot "docs\openclaw-maintenance.md"))
$startScript = [IO.File]::ReadAllText((Join-Path $repositoryRoot "installer\windows\Start-HAI.ps1"))
$stopScript = [IO.File]::ReadAllText((Join-Path $repositoryRoot "installer\windows\Stop-HAI.ps1"))
$openScript = [IO.File]::ReadAllText((Join-Path $repositoryRoot "installer\windows\Open-HAI.ps1"))
$connectorTest = [IO.File]::ReadAllText((Join-Path $repositoryRoot "installer\windows\Test-HAI-LocalConnector.ps1"))
$hostRuntimeWorker = [IO.File]::ReadAllText((Join-Path $repositoryRoot "installer\windows\Run-HAI-DeepSeekBridge.ps1"))
$ngrokStart = [IO.File]::ReadAllText((Join-Path $repositoryRoot "scripts\start-ngrok.ps1"))
$exampleEnvironment = [IO.File]::ReadAllText((Join-Path $repositoryRoot ".env.example"))
$secretGenerator = [IO.File]::ReadAllText((Join-Path $repositoryRoot "scripts\generate-secrets.sh"))
$docs = [IO.File]::ReadAllText($documentation)
$gitignore = [IO.File]::ReadAllText((Join-Path $repositoryRoot ".gitignore"))
$runtimeLifecycle = [IO.File]::ReadAllText($runtimeLifecycleTest)

foreach ($textName in @(
    'build', 'sourceSelection', 'installer', 'support', 'maintenanceSupport',
    'maintenanceTaskManager', 'maintenanceLauncher', 'initializer',
    'runtimeDatabaseRole', 'runtimeRoleProvisioner', 'composeConfiguration',
    'gatewayConfig', 'maintenanceDocumentation', 'startScript', 'stopScript',
    'openScript', 'connectorTest', 'hostRuntimeWorker', 'ngrokStart',
    'exampleEnvironment', 'secretGenerator', 'docs', 'gitignore', 'runtimeLifecycle'
)) {
    Set-Variable -Name $textName -Value ((Get-Variable -Name $textName -ValueOnly).Replace("`r`n", "`n"))
}

if ($gitignore -notmatch [Regex]::Escape("/installer/release/")) {
    throw "Generated installer release artifacts must be ignored by Git."
}

foreach ($script in @(
    $buildScript,
    $sourceSelectionScript,
    $sourceSelectionTest,
    $payloadValidationTest,
    $maintenanceLauncherTest,
    $runtimeLifecycleTest,
    $runtimeHealthTest,
    $supportScript,
    (Join-Path $repositoryRoot "installer\windows\Hai-WindowsExecutable.ps1"),
    $initializerScript,
    (Join-Path $repositoryRoot "installer\windows\Start-HAI.ps1"),
    (Join-Path $repositoryRoot "installer\windows\Stop-HAI.ps1"),
    (Join-Path $repositoryRoot "installer\windows\HAI-Status.ps1"),
    (Join-Path $repositoryRoot "installer\windows\Test-HAI-LocalConnector.ps1"),
    (Join-Path $repositoryRoot "installer\windows\Run-HAI-DeepSeekBridge.ps1"),
    (Join-Path $repositoryRoot "installer\windows\Run-HAI-OpenClawMaintenance.ps1"),
    (Join-Path $repositoryRoot "installer\windows\Hai-OpenClawMaintenance.ps1"),
    (Join-Path $repositoryRoot "installer\windows\Manage-HAI-OpenClawMaintenanceTask.ps1"),
    (Join-Path $repositoryRoot "installer\windows\Open-HAI.ps1"),
    $runtimeDatabaseRoleScript
)) {
    $tokens = $null
    $errors = $null
    [Management.Automation.Language.Parser]::ParseFile($script, [ref]$tokens, [ref]$errors) | Out-Null
    if ($errors.Count -gt 0) {
        throw "PowerShell syntax error in ${script}: $($errors[0].Message)"
    }
}

if (-not $OpenClawMaintenanceOnly -and -not $UninstallRuntimeOnly) {
    & (Join-Path $PSScriptRoot "test-dsh-version-validation.ps1")
}

if (-not $OpenClawMaintenanceOnly -and -not $UninstallRuntimeOnly) {
    & $runtimeHealthTest
}

& $uninstallMaintenanceRecoveryTest

if (-not $UninstallRuntimeOnly) {
    & $payloadValidationTest
}

foreach ($required in @(
    "-ExcludeRelativePath"
)) {
    if (($build + $sourceSelection) -notmatch [Regex]::Escape($required)) {
        throw "Installer build contract is missing '$required'."
    }
}

foreach ($required in @(
    "AllowDirtyWorktree",
    "status --porcelain=v1 --untracked-files=all",
    "Refusing to build a release installer from a dirty worktree"
)) {
    if ($build -notmatch [Regex]::Escape($required)) {
        throw "Installer release-integrity contract is missing '$required'."
    }
}

if ($build -match [Regex]::Escape('ls-files --cached --others --exclude-standard)')) {
    throw "Installer staging must not package arbitrary nonignored files from a developer checkout."
}
foreach ($required in @(
    "installer/windows",
    "scripts/build-windows-installer.ps1",
    "docs/windows-installer.md",
    "backend/cmd/hai-openclaw-maintenance",
    "backend/internal/openclawmaintenance"
)) {
    if (($build + $sourceSelection) -notmatch [Regex]::Escape($required)) {
        throw "Installer staging allowlist is missing '$required'."
    }
}

if ($sourceSelection -notmatch [Regex]::Escape("[IO.Path]::GetExtension(`$normalized) -ine '.go'")) {
    throw "Untracked OpenClaw worker payload sources must be limited to Go source files."
}
if ($sourceSelection -match "(?m)^\s*'installer/windows/'\s*,?\s*$" -or
    $sourceSelection -notmatch [Regex]::Escape('installer/windows/Manage-HAI-OpenClawMaintenanceTask.ps1')) {
    throw "Untracked installer sources must use explicit reviewed paths and include the uninstall task manager."
}
foreach ($required in @(
    'installer/windows/Hai-WindowsExecutable.ps1',
    'Test-HaiWindowsExecutablePayload -Path $pendingPath',
    'Test-HaiWindowsExecutablePayload -Path $binary',
    'Test-HaiWindowsExecutablePayload -Path (Join-Path $stagingRoot'
)) {
    if (($sourceSelection + $support + $maintenanceLauncher + $build) -notmatch [Regex]::Escape($required)) {
        throw "The Windows installer must select and validate worker payloads before promotion or execution: $required"
    }
}

foreach ($required in @(
    "initialize-windows.ps1",
    "docker compose",
    "Docker Desktop",
    'label=com.docker.compose.project.config_files',
    'Could not enumerate Docker Compose containers',
    "HAI environment initialization failed"
)) {
    if ($support -notmatch [Regex]::Escape($required)) {
        throw "Installer runtime contract is missing '$required'."
    }
}

foreach ($required in @(
    "hai-dsh-bridge.exe",
    "./cmd/hai-dsh-bridge",
    "hai-openclaw-maintenance.exe",
    "./cmd/hai-openclaw-maintenance",
    "GOOS=windows",
    'golang:1.27.2@sha256:5bc7f572bbaa98885a3a1fd9c0aa76b59e3e14e8628bfc316bbfd0c701e4818c',
    "--platform linux/amd64",
    'tool = $goBuilderImage',
    '$goBuilderImage `'
)) {
    if ($build -notmatch [Regex]::Escape($required)) {
        throw "Installer build contract is missing bundled host-runtime worker support: $required"
    }
}
if ($build -match '(?m)^\s*golang:1\.25\.13\s*`?$' -or
    $build -match 'tag reference; image digest is not pinned') {
    throw 'The installer Docker fallback must use its immutable, explicitly platformed builder image reference.'
}

foreach ($required in @(
    'Get-HaiHostRuntimeBridgeProcesses',
    'Get-HaiHostRuntimeLauncherProcesses',
    'Stop-HaiProcessAndWait',
    'OpenProcess(ProcessTerminate | ProcessQueryLimitedInformation | Synchronize',
    'QueryFullProcessImageName',
    'GetProcessTimes',
    'TerminateProcess(process',
    'WaitForSingleObject(process, timeoutMilliseconds)',
    'Older launchers did not always write a PID record',
    'exact native bridge before its PowerShell launcher'
)) {
    if (($support + $runtimeLifecycle) -notmatch [Regex]::Escape($required)) {
        throw "Windows runtime shutdown contract is missing '$required'."
    }
}
if ($stopScript.IndexOf('Stop-HaiHostRuntimeWorker', [StringComparison]::Ordinal) -gt
    $stopScript.IndexOf('& docker @composeArguments --profile local-a2a stop', [StringComparison]::Ordinal) -or
    $stopScript.IndexOf('Stop-HaiHostRuntimeWorker', [StringComparison]::Ordinal) -gt
    $stopScript.IndexOf('Assert-HaiDockerReady', [StringComparison]::Ordinal) -or
    $stopScript -notmatch '\$hostRuntimeStopFailure\s*=\s*\$_.Exception.Message' -or
    $stopScript -notmatch 'the optional host runtime could not be confirmed stopped') {
    throw 'Stop HAI must attempt the independent host runtime before Docker preflight, continue to stop Compose after runtime shutdown failure, then report residual process state.'
}
if ($support -notmatch 'Could not enumerate Docker Compose containers' -or
    $support -notmatch 'Could not verify the Compose project and owner path for HAI container') {
    throw 'The first-run Docker project ownership check must fail closed if Docker cannot prove container ownership.'
}
if ($initializer -notmatch 'Could not verify ownership of Docker project' -or
    $initializer -notmatch 'Could not verify the owner path for HAI container') {
    throw 'The first-run initializer must fail closed when Docker ownership checks are unavailable.'
}
$payloadStage = $build.IndexOf('.payload-$buildId.staging', [StringComparison]::Ordinal)
$payloadCleanup = $build.IndexOf('Remove-Item -LiteralPath $payloadRoot -Recurse -Force', [StringComparison]::Ordinal)
if ($build -notmatch 'requiredPayloadSources' -or
    $payloadStage -lt 0 -or
    $build -notmatch 'Move-Item -LiteralPath \$stagingRoot -Destination \$payloadRoot' -or
    ($payloadCleanup -ge 0 -and $payloadCleanup -lt $payloadStage)) {
    throw 'Installer payload generation must preflight required files and stage before replacing the last prepared payload.'
}
foreach ($required in @(
    '$payloadPromotionAttempted = $true',
    '$manifestPromotionAttempted = $true',
    'if ($payloadMovedToBackup) {',
    'if ($manifestMovedToBackup) {',
    'Rollback was incomplete'
)) {
    if ($build -notmatch [Regex]::Escape($required)) {
        throw "Installer payload rollback must preserve the prior payload when backup or promotion fails: '$required'."
    }
}
if ($support -match 'Initialize-HaiLocalEnvironment[\s\S]*?\$LASTEXITCODE -ne 0') {
    throw 'First-run PowerShell initialization must not use a stale native-command exit code as its success signal.'
}
if ($support -notmatch 'function Start-HaiComposeStack' -or
    $support -notmatch 'No containers were stopped automatically because concurrent Docker Compose activity cannot be distinguished safely' -or
    $support -notmatch 'ownership by this startup attempt could not be proven' -or
    $support -match 'docker stop @newContainerIds' -or
    $startScript -notmatch 'Start-HaiComposeStack -ComposeArguments \$composeArguments' -or
    $startScript -notmatch 'HAI is ready, but OpenClaw maintenance setup or its immediate start did not complete') {
    throw 'Startup must preserve ambiguous containers after Compose/readiness failure and report optional maintenance failure without misreporting the ready application.'
}

foreach ($required in @(
    "Start-HaiHostRuntimeWorker",
    "Stop-HaiHostRuntimeWorker",
    "Get-HaiHostRuntimeWorkerStatus",
    "Test-HaiHostRuntimeWorkerProcess",
    "Win32_Process",
    "pid record does not reference the HAI runtime worker",
    'bridge running without a verified launcher PID (PID record invalid)',
    'bridge running without a verified launcher PID (PID record points to another process)',
    'pid record path exists but is not a regular file',
    "Get-HaiHostRuntimeIsolationBlockReason",
    "no verified OS-enforced sandbox"
)) {
    if ($support -notmatch [Regex]::Escape($required)) {
        throw "Installer support contract is missing host-runtime lifecycle control: $required"
    }
}
foreach ($required in @(
    'runtimeLifecycleReusePidOnNextTermination',
    'bridge running without a verified launcher PID',
    'native process-handle regression test',
    'The native process handle did not refuse an image-path mismatch',
    'The native process handle did not refuse a creation-time mismatch',
    'A process with a reused PID was terminated.'
)) {
    if ($runtimeLifecycle -notmatch [Regex]::Escape($required)) {
        throw "Windows runtime regression coverage is missing '$required'."
    }
}

foreach ($required in @(
    "HAI_HOST_RUNTIME_BRIDGE_TOKEN",
    "HAI_HOST_RUNTIME_BRIDGE_URL",
    "DEEPSEEK_HARNESS_EXECUTABLE",
    "DEEPSEEK_HARNESS_VERSION",
    "DEEPSEEK_HARNESS_WORKSPACE",
    "DEEPSEEK_HARNESS_STATE_DIR",
    "DEEPSEEK_HARNESS_WORKSPACE_KEY",
    "DEEPSEEK_HARNESS_TIMEOUT_SECONDS"
)) {
    if ($hostRuntimeWorker -notmatch [Regex]::Escape($required)) {
        throw "Host-runtime worker contract is missing its declared configuration field: $required"
    }
}
if ($hostRuntimeWorker -match '(?i)--version|&\s*\$executable' -or
    $exampleEnvironment -notmatch '(?m)^DEEPSEEK_HARNESS_ENABLED=false\r?$' -or
    $exampleEnvironment -notmatch '(?m)^DEEPSEEK_HARNESS_EXECUTION_ENABLED=false\r?$') {
    throw 'The Windows installer must block DSH runtime execution and keep both execution flags disabled in a fresh installation.'
}

if ($initializer -notmatch [Regex]::Escape('[switch]$EnableA2ABridge') -or
    $initializer -notmatch 'if \(-not \(\$PSBoundParameters\.ContainsKey\(''EnableA2ABridge''\) -and -not \$EnableA2ABridge\)\)' -or
    $initializer -notmatch 'Get-HaiSavedA2AConfiguration\s+-Path\s+\$EnvFile' -or
    $initializer -notmatch '\$preserveSavedA2AConfiguration\s*=' -or
    $initializer -notmatch '\$EnableA2ABridge\s+-and\s+-not\s+\$preserveSavedA2AConfiguration' -or
    $initializer -notmatch '\$savedA2AConfiguration\.Token' -or
    $initializer -notmatch '\$a2aBridgeToken\s*=\s*\$savedA2AConfiguration\.Token' -or
    $initializer -notmatch [Regex]::Escape('Set-DotEnvValue $content "HAI_A2A_BRIDGE_TOKEN" $a2aBridgeToken') -or
    $initializer -notmatch [Regex]::Escape('-EnableA2ABridge; an existing install is controlled by HAI_A2A_BRIDGE_ENABLED') -or
    $startScript -notmatch 'if \(\$PSBoundParameters\.ContainsKey\(''EnableA2ABridge''\)\)\s*\{\s*Initialize-HaiLocalEnvironment -GatewayPort \$GatewayPort -EnableA2ABridge:\$EnableA2ABridge\s*\}\s*else\s*\{\s*Initialize-HaiLocalEnvironment -GatewayPort \$GatewayPort' -or
    $support -notmatch 'if \(\$PSBoundParameters\.ContainsKey\(''EnableA2ABridge''\)\)\s*\{\s*Set-HaiA2ABridgeConfiguration -Path \$environmentFile -Enabled \(\[bool\]\$EnableA2ABridge\)\s*\}' -or
    $support -notmatch 'if \(\$PSBoundParameters\.ContainsKey\(''EnableA2ABridge''\)\)\s*\{\s*& \$initializer -EnvFile \$environmentFile -GatewayPort \$GatewayPort -EnableA2ABridge:\$EnableA2ABridge\s*\}') {
    throw "A2A must default off on fresh installs, preserve valid existing settings when omitted, disable on an explicit false choice, and generate credentials only for explicit opt-in."
}
if ($support -notmatch 'function Test-HaiCredentialFreeA2ABridgeUrl' -or
    $support -notmatch '\.UserInfo' -or $support -notmatch '\.Query' -or $support -notmatch '\.Fragment' -or
    $support -notmatch '\[IO\.File\]::Replace\(\$temporaryPath, \$Path, \$backupPath\)' -or
    $support -notmatch 'Global\\HAI\.EnvironmentMigration\.' -or
    $initializer -notmatch 'function Test-HaiCredentialFreeA2ABridgeUrl' -or
    $initializer -notmatch 'endpoint and credentials withheld') {
    throw 'A2A URL credentials must be rejected and hidden, and explicit environment changes must be serialized and atomically replaced.'
}
if ($exampleEnvironment -notmatch '(?m)^HAI_A2A_BRIDGE_ENABLED=false$') {
    throw "New-install environment defaults must keep the optional A2A bridge disabled."
}
if ($runtimeLifecycle -notmatch 'foreach \(\$startAttempt in 1\.\.2\)' -or
    $runtimeLifecycle -notmatch 'Explicit enablement rotated a valid saved A2A token' -or
    $runtimeLifecycle -notmatch 'Repeated explicit enablement rotated the A2A token' -or
    $runtimeLifecycle -notmatch 'Repeated explicit first-run enablement did not preserve the generated A2A credentials' -or
    $runtimeLifecycle -notmatch '\. \$startScriptPath -NoBrowser -HealthTimeoutSeconds 30' -or
    $runtimeLifecycle -notmatch '\. \$startScriptPath -EnableA2ABridge -NoBrowser -HealthTimeoutSeconds 30' -or
    $runtimeLifecycle -notmatch '\. \$startScriptPath -EnableA2ABridge:\$false -NoBrowser -HealthTimeoutSeconds 30' -or
    $runtimeLifecycle -notmatch 'credential-bearing A2A URL' -or
    $runtimeLifecycle -notmatch 'Initializer output exposed saved A2A credentials or URL data') {
    throw 'Runtime lifecycle regressions must exercise the actual launcher omission/true/false paths and reject/redact credential-bearing A2A URLs.'
}

$composeLifecycle = [Regex]::Match($support, '(?ms)^function Start-HaiComposeStack\s*\{(?<body>.*?)(?=^function |\z)').Groups['body'].Value
if ($startScript -match '(?s)\$composeArguments\s*\+=\s*@\(\s*["'']--profile["'']\s*,\s*["'']local-a2a["'']\s*\)' -or
    $support -notmatch 'function Get-HaiA2AComposeSettings' -or
    $support -notmatch "'config', '--format', 'json'" -or
    $support -notmatch 'function Suspend-HaiComposeA2AOverrides' -or
    $support -match 'function Get-HaiComposeRunningContainerIds' -or
    $composeLifecycle -notmatch '\$a2aSettings\s*=\s*Get-HaiA2AComposeSettings\s+-ComposeArguments\s+\$ComposeArguments' -or
    $composeLifecycle -notmatch 'if \(-not \$a2aSettings\.Enabled\)\s*\{\s*& docker @ComposeArguments --profile local-a2a stop a2a-gateway' -or
    $composeLifecycle -notmatch 'if \(\$a2aSettings\.Enabled\)\s*\{\s*\$startupArguments\s*\+=\s*@\(' -or
    $composeLifecycle -match 'Get-HaiComposeRunningContainerIds|docker stop @newContainerIds' -or
    $composeLifecycle -notmatch 'No containers were stopped automatically because concurrent Docker Compose activity cannot be distinguished safely' -or
    $composeLifecycle -match '& docker @ComposeArguments --profile local-a2a stop\s*$' -or
    $composeLifecycle -notmatch 'Wait-HaiA2AReady\s+-TimeoutSeconds\s+\$HealthTimeoutSeconds\s+-BridgeUrl\s+\$a2aSettings\.BridgeUrl\s+-AgentCardUrl\s+\$a2aSettings\.AgentCardUrl' -or
    $support -notmatch 'ConvertFrom-Json -ErrorAction Stop' -or
    $support -notmatch "protocolBinding -eq 'JSONRPC'" -or
    $support -notmatch 'protocolVersion -eq ''1\.0''' -or
    $support -notmatch 'supportedInterfaces' -or
    $support -notmatch 'interface\.url -eq \$planningUrl') {
    throw "A2A startup must use resolved Compose settings, preserve opt-in behavior, validate the configured Agent Card URL, and preserve containers for explicit recovery after failed readiness."
}
if ($stopScript -notmatch '& docker @composeArguments --profile local-a2a stop') {
    throw "Stop HAI must always include the optional A2A profile so a previously enabled connector is stopped."
}

$a2aConfiguration = [Regex]::Match($support, '(?ms)^function Set-HaiA2ABridgeConfiguration\s*\{(?<body>.*?)(?=^function |\z)').Groups['body'].Value
$sensitiveCleanup = [Regex]::Match($support, '(?ms)^function Remove-HaiSensitiveTemporaryFile\s*\{(?<body>.*?)(?=^function |\z)').Groups['body'].Value
if ($sensitiveCleanup -notmatch 'throw\s+\[IO\.IOException\]::new' -or
    $sensitiveCleanup -match 'Write-Warning|return\s+\$false') {
    throw 'Sensitive A2A temporary-file cleanup failures must be terminating errors, not suppressible warnings or false return values.'
}
if ($a2aConfiguration -notmatch 'Remove-HaiSensitiveTemporaryFile' -or
    $a2aConfiguration -notmatch '\$cleanupFailures\s*=\s*New-Object' -or
    $a2aConfiguration -notmatch 'secure cleanup failed or could not be confirmed for:' -or
    $a2aConfiguration -match 'Remove-Item[^\r\n]*SilentlyContinue') {
    throw 'A2A environment updates must verify removal of credential-bearing temporary and backup files.'
}

foreach ($gateway in @(@{ Name = 'a2a-gateway'; Config = '/etc/nginx/a2a-local.conf.template' }, @{ Name = 'host-runtime-gateway'; Config = '/etc/nginx/host-runtime.conf.template' })) {
    $gatewaySection = [Regex]::Match($composeConfiguration, "(?ms)^  $($gateway.Name):(?<body>.*?)(?=^  [A-Za-z0-9_-]+:|\z)").Groups['body'].Value
    $expectedGatewayCommand = "umask 077; envsubst '`$`$BACKEND_API_SHARED_KEY' < $($gateway.Config) > /tmp/nginx.conf && exec nginx -c /tmp/nginx.conf"
    if ($gatewaySection -notmatch [Regex]::Escape($expectedGatewayCommand) -or
        $gatewaySection -notmatch '(?m)^\s+read_only: true\s*$' -or
        $gatewaySection -notmatch '(?m)^\s+user: "nginx"\s*$' -or
        $gatewaySection -notmatch [Regex]::Escape('/tmp:rw,noexec,nosuid,size=1m,uid=101,gid=101,mode=700') -or
        $gatewaySection -notmatch [Regex]::Escape('/var/cache/nginx:rw,noexec,nosuid,size=4m,uid=101,gid=101,mode=700') -or
        $gatewaySection -notmatch [Regex]::Escape(":8080") -or
        [IO.File]::ReadAllText((Join-Path $repositoryRoot ($gateway.Config -replace '^/etc/nginx/', 'nginx-config/'))) -notmatch '(?m)^\s*listen 8080;\s*$') {
        throw "$($gateway.Name) must render its secret-bearing configuration on memory-backed storage and run unprivileged with a read-only container filesystem."
    }
}

if ($stopScript -notmatch [Regex]::Escape('Assert-HaiSingleInstallation')) {
    throw "The installed stop command must refuse to manage another HAI installation."
}
if ($openScript -notmatch 'Get-HaiStackReadinessResult' -or
    $openScript -notmatch '\$dashboardUrl' -or
    $openScript -notmatch 'Start-Process\s+-FilePath\s+\$dashboardUrl' -or
    $openScript -notmatch '\[switch\]\$PauseOnError' -or
    $openScript -notmatch 'Open local dashboard failed:' -or
    $openScript -notmatch 'Press Enter to close this window' -or
    $installer -notmatch '(?m)^Name: "\{autoprograms\}\\HAI Local\\Open local dashboard";.*Open-HAI\.ps1"" -PauseOnError"; WorkingDir:' -or
    $openScript -match '(?i)&\s*docker\b|docker\s+compose|Start-HaiComposeStack|Start-HAI\.ps1') {
    throw 'Open HAI must verify readiness, keep shortcut failures visible with actionable output, and avoid creating or mutating a Compose stack.'
}
$stackReadiness = [Regex]::Match($support, '(?ms)^function Get-HaiStackReadinessResult\s*\{(?<body>.*?)(?=^function\s|\z)').Groups['body'].Value
$stackHealth = [Regex]::Match($support, '(?ms)^function Get-HaiStackHealthReport\s*\{(?<body>.*?)(?=^function\s|\z)').Groups['body'].Value
$httpProbe = [Regex]::Match($support, '(?ms)^function Get-HaiHttpProbeResult\s*\{(?<body>.*?)(?=^function\s|\z)').Groups['body'].Value
$protectedApiProbe = [Regex]::Match($support, '(?ms)^function Get-HaiProtectedApiProbeResult\s*\{(?<body>.*?)(?=^function\s|\z)').Groups['body'].Value
$stackReadinessWait = [Regex]::Match($support, '(?ms)^function Wait-HaiReady\s*\{(?<body>.*?)(?=^function\s|\z)').Groups['body'].Value
if ([string]::IsNullOrWhiteSpace($stackReadiness) -or
    $stackHealth -notmatch "-Path '/readyz'" -or
    $stackHealth -notmatch "-Path '/_hai/idp-readyz'" -or
    $stackHealth -notmatch "-Path '/login'" -or
    $stackHealth -notmatch "-Path '/control-center'" -or
    $stackHealth -notmatch 'Get-HaiProtectedApiProbeResult' -or
    $protectedApiProbe -notmatch "-Path '/api/v1/openclaw-maintenance'" -or
    $protectedApiProbe -match "-Path '/api/v1/user/'" -or
    $protectedApiProbe -notmatch 'cannot prove a signed-in user' -or
    $stackHealth -notmatch 'AuthenticatedApi = \[pscustomobject\]@\{ State = \$authenticatedState' -or
    $httpProbe -notmatch 'TimeoutSec\s+\$TimeoutSeconds' -or
    $httpProbe -notmatch 'MaximumRedirection\s+0' -or
    $stackReadinessWait -notmatch 'Get-HaiStackReadinessResult' -or
    $gatewayConfig -notmatch 'proxy_pass http://\$idp_upstream/readyz' -or
    $gatewayConfig -notmatch 'proxy_pass_request_headers off' -or
    $gatewayConfig -notmatch 'proxy_pass_request_body off' -or
    $gatewayConfig -notmatch '(?s)location = /_hai/idp-readyz\s*\{.*?proxy_connect_timeout 2s;.*?proxy_read_timeout 3s;') {
    throw 'HAI launch readiness must distinguish backend, IDP, frontend, and protected API checks, with a bounded sanitized IDP readiness alias.'
}
if ($stopScript -notmatch '& docker @composeArguments --profile local-a2a stop' -or
    $stopScript -match '(?i)\bdocker\b[^\r\n]*\b(down|rm|kill)\b|Remove-Volume|Remove-Item[^\r\n]*volume' -or
    $stopScript -notmatch 'Docker volumes and local settings were preserved') {
    throw 'Stop HAI must stop only the pinned stack and preserve its containers, Docker volumes, and local settings.'
}

$composeProjectNameHelper = [Regex]::Match($support, '(?ms)^function Get-HaiComposeProjectName\s*\{(?<body>.*?)(?=^function\s|\z)').Groups['body'].Value
$singleInstallationHelper = [Regex]::Match($support, '(?ms)^function Assert-HaiSingleInstallation\s*\{(?<body>.*?)(?=^function\s|\z)').Groups['body'].Value
$composeArgumentsHelper = [Regex]::Match($support, '(?ms)^function Get-HaiComposeArguments\s*\{(?<body>.*?)(?=^function\s|\z)').Groups['body'].Value
if ([string]::IsNullOrWhiteSpace($composeProjectNameHelper) -or
    $composeProjectNameHelper -notmatch 'Get-HaiEnvironmentFile' -or
    $composeProjectNameHelper -notmatch 'duplicate COMPOSE_PROJECT_NAME settings' -or
    $composeProjectNameHelper -notmatch 'invalid COMPOSE_PROJECT_NAME setting' -or
    $singleInstallationHelper -notmatch '\$projectName\s*=\s*Get-HaiComposeProjectName' -or
    $composeArgumentsHelper -notmatch '"--project-name"\s*,\s*\(Get-HaiComposeProjectName\)' -or
    $startScript -notmatch '(?s)\$composeArguments\s*=\s*Get-HaiComposeArguments.*?Start-HaiComposeStack\s+-ComposeArguments\s+\$composeArguments' -or
    $stopScript -notmatch '(?s)\$composeArguments\s*=\s*Get-HaiComposeArguments.*?&\s+docker\s+@composeArguments') {
    throw 'HAI start, stop, and single-installation ownership checks must share the validated Compose project name from the local environment file.'
}

$environmentRecoveryGuard = [Regex]::Match(
    $support,
    '(?ms)^function Assert-HaiExistingDataRequiresEnvironment\s*\{(?<body>.*?)(?=^function |\z)'
).Groups['body'].Value
$startupEnvironmentGuard = $startScript.IndexOf('Assert-HaiExistingDataRequiresEnvironment', [StringComparison]::Ordinal)
$startupEnvironmentInitialization = $startScript.IndexOf('Initialize-HaiLocalEnvironment', [StringComparison]::Ordinal)
$startupHostWorkerMutation = $startScript.IndexOf('Stop-HaiHostRuntimeWorkerIfPresent', [StringComparison]::Ordinal)
if ([string]::IsNullOrWhiteSpace($environmentRecoveryGuard) -or
    $environmentRecoveryGuard -notmatch 'Test-HaiEnvironmentFileAvailable' -or
    $environmentRecoveryGuard -notmatch 'docker volume ls --quiet --filter' -or
    $environmentRecoveryGuard -notmatch 'refusing to initialize replacement credentials' -or
    $environmentRecoveryGuard -notmatch 'Restore the original hai\.env' -or
    $environmentRecoveryGuard -match '(?i)docker\s+volume\s+rm' -or
    $startupEnvironmentGuard -lt 0 -or
    $startupEnvironmentInitialization -lt 0 -or
    $startupEnvironmentGuard -gt $startupEnvironmentInitialization -or
    $startupHostWorkerMutation -lt 0 -or
    $startupEnvironmentGuard -gt $startupHostWorkerMutation) {
    throw 'HAI startup must fail closed when existing Compose data volumes are present but the original protected environment file is missing, before creating replacement credentials.'
}

$initializerScript = Get-Content -LiteralPath (Join-Path $repositoryRoot 'scripts/initialize-windows.ps1') -Raw
$initializerOwnershipGuard = [Regex]::Match(
    $initializerScript,
    '(?ms)^function Assert-HaiComposeProjectAvailable\s*\{(?<body>.*?)(?=^function\s|^\$content\s*=\s*\[IO\.File\]::ReadAllText\(\$examplePath\))'
).Groups['body'].Value
$initializerCredentialCreation = $initializerScript.IndexOf('$content = Set-DotEnvValue $content "BACKEND_API_SHARED_KEY"', [StringComparison]::Ordinal)
if ([string]::IsNullOrWhiteSpace($initializerOwnershipGuard) -or
    $initializerOwnershipGuard -notmatch 'Docker is unavailable' -or
    $initializerOwnershipGuard -notmatch 'docker volume ls --quiet' -or
    $initializerOwnershipGuard -notmatch '018-hai-' -or
    $initializerOwnershipGuard -notmatch 'Existing HAI data volumes were found' -or
    $initializerOwnershipGuard -match '(?i)docker\s+volume\s+rm' -or
    $initializerCredentialCreation -lt 0 -or
    $initializerScript.IndexOf('Assert-HaiComposeProjectAvailable -ProjectName', [StringComparison]::Ordinal) -gt $initializerCredentialCreation) {
    throw 'The standalone Windows initializer must fail closed on existing HAI data or unverifiable Docker state before generating new credentials.'
}

foreach ($required in @(
    "HAI_A2A_BRIDGE_TOKEN",
    "A2A-Version",
    "supportedInterfaces",
    'Planning endpoint: $($planningInterface.url)',
    "SendMessage",
    "TASK_STATE_COMPLETED",
    "hai-controlled-planning-proposal"
)) {
    if ($connectorTest -notmatch [Regex]::Escape($required)) {
        throw "The local connector diagnostic does not verify '$required'."
    }
}

if ($connectorTest -match [Regex]::Escape('Planning endpoint: $($agentCard.url)')) {
    throw "The local connector diagnostic reports the removed Agent Card URL field instead of its advertised planning interface."
}

if ($initializer -notmatch [Regex]::Escape('GATEWAY_HOST_BIND') -or
    $initializer -notmatch [Regex]::Escape('"127.0.0.1"')) {
    throw "The first-run initializer does not enforce a loopback gateway."
}

$initializerEnvironmentKeys = [Regex]::Matches($initializer, 'Set-DotEnvValue\s+\$content\s+"([A-Z0-9_]+)"') |
    ForEach-Object { $_.Groups[1].Value } |
    Select-Object -Unique
foreach ($key in $initializerEnvironmentKeys) {
    if ($exampleEnvironment -notmatch "(?m)^$([Regex]::Escape($key))=") {
        throw "The first-run initializer writes '$key', but .env.example does not define it."
    }
}

if ($exampleEnvironment -notmatch '(?m)^HAI_OPENCLAW_MAINTENANCE_ENABLED=true$') {
    throw "The clean-install environment template must enable verified OpenClaw maintenance by default."
}
if ($composeConfiguration -notmatch '(?m)^\s*HAI_OPENCLAW_MAINTENANCE_ENABLED:\s*\$\{HAI_OPENCLAW_MAINTENANCE_ENABLED:-true\}$') {
    throw "Compose must default OpenClaw maintenance on when no explicit setting is supplied."
}
if ($initializer -notmatch 'Set-DotEnvValue\s+\$content\s+"HAI_OPENCLAW_MAINTENANCE_TOKEN"\s+\(New-HaiSecret\)') {
    throw "The Windows first-run initializer must generate a unique maintenance worker token."
}
if ($maintenanceDocumentation -notmatch '(?m)^HAI_OPENCLAW_MAINTENANCE_ENABLED=false$') {
    throw "OpenClaw maintenance documentation must describe the explicit opt-out."
}
$existingEnvironmentMigration = 'if \(Test-Path -LiteralPath \$environmentFile -PathType Leaf\)\s*\{[\s\S]*?& \$initializer -EnvFile \$environmentFile -GatewayPort \$GatewayPort -MigrateExisting'
if ($support -notmatch $existingEnvironmentMigration) {
    throw "An existing Windows install must run the additive environment migration before startup continues."
}
if ($installer -notmatch '(?m)^Filename: .*Start-HAI\.ps1.*Flags: postinstall nowait skipifsilent$') {
    throw "Silent installs must continue to skip the full Start HAI application command."
}
$installerRunSection = [Regex]::Match($installer, '(?ms)^\[Run\]\s*(?<body>.*?)(?=^\[|\z)').Groups['body'].Value
if ($installerRunSection -match 'ConfigureSilentUpgrade') {
    throw "Silent upgrade work must use captured Inno [Code] process results, not a [Run] entry that can hide failures."
}
$maintenanceWorkerSource = '..\release\payload\installer\windows\hai-openclaw-maintenance.exe'
$pendingWorkerDestination = 'hai-openclaw-maintenance.pending.exe'
if ($installer.IndexOf($maintenanceWorkerSource, [StringComparison]::OrdinalIgnoreCase) -lt 0 -or
    $installer.IndexOf($pendingWorkerDestination, [StringComparison]::OrdinalIgnoreCase) -lt 0 -or
    $installer -notmatch '(?im)^Source: "\.\.\\release\\payload\\\*"; Excludes: "\\installer\\windows\\hai-openclaw-maintenance\.exe"') {
    throw "The maintenance worker must be excluded from in-place replacement and staged under a pending name."
}
if ($installer.IndexOf('Run-HAI-OpenClawMaintenance.ps1"; DestDir:', [StringComparison]::OrdinalIgnoreCase) -gt
    $installer.IndexOf('Source: "..\release\payload\*"', [StringComparison]::OrdinalIgnoreCase)) {
    throw "The updated maintenance launcher must be installed before the remaining payload so future scheduled starts honor the mutex."
}
$prepareToInstall = $installer.IndexOf('function PrepareToInstall', [StringComparison]::Ordinal)
$installMutex = $installer.IndexOf('Global\HAI.OpenClawMaintenance', [StringComparison]::Ordinal)
$acquireMutex = $installer.IndexOf('function AcquireHaiMaintenanceMutex', [StringComparison]::Ordinal)
$workerPreflight = $installer.IndexOf('function HasLegacyMaintenanceWorker', [StringComparison]::Ordinal)
$prepareBodyEnd = $installer.IndexOf('function InitializeUninstall', $prepareToInstall, [StringComparison]::Ordinal)
$prepareBody = if ($prepareToInstall -ge 0 -and $prepareBodyEnd -gt $prepareToInstall) { $installer.Substring($prepareToInstall, $prepareBodyEnd - $prepareToInstall) } else { '' }
$postInstallEvent = $installer.IndexOf('procedure CurStepChanged', [StringComparison]::Ordinal)
$workerPromotion = $installer.IndexOf("RunHaiInstallerSupport('-PromoteMaintenanceWorker'", [StringComparison]::Ordinal)
$lockRelease = $installer.IndexOf('if not ReleaseHaiMaintenanceMutex then', $postInstallEvent, [StringComparison]::Ordinal)
$silentConfigurationCall = $installer.IndexOf('-ConfigureSilentUpgrade', [StringComparison]::Ordinal)
$postUnlockTaskStart = $installer.IndexOf('-StartMaintenanceTask', $postInstallEvent, [StringComparison]::Ordinal)
$setupCleanup = $installer.IndexOf('procedure DeinitializeSetup', [StringComparison]::Ordinal)
if ($prepareToInstall -lt 0 -or $installMutex -lt 0 -or $acquireMutex -lt 0 -or $workerPreflight -lt 0 -or
    $prepareBody -notmatch 'AcquireHaiMaintenanceMutex' -or $prepareBody -notmatch 'HasLegacyMaintenanceWorker' -or
    $postInstallEvent -lt 0 -or $lockRelease -lt $postInstallEvent -or $workerPromotion -lt $lockRelease -or
    $silentConfigurationCall -lt $workerPromotion -or
    $postUnlockTaskStart -lt $silentConfigurationCall -or $setupCleanup -lt 0 -or
    $installer -notmatch 'WaitForSingleObject\(HaiMaintenanceMutexHandle, 0\)' -or
    $installer -notmatch 'procedure DeinitializeSetup;\s*begin\s*ReleaseHaiMaintenanceMutex;') {
    throw "The installer must protect file replacement, release its lock before helper processes acquire it, and clean up on every exit."
}
if ($installer -notmatch 'if ExitCode <> 0 then[\s\S]*?RaiseException\(FailureMessage\)' -or
    $installer -notmatch 'ewWaitUntilTerminated, ExitCode' -or
    $installer -notmatch 'PowerShell exit code ' -or
    $installer -notmatch 'if not Exec\([\s\S]*?RaiseException\(FailureMessage\)') {
    throw "Silent upgrade and worker promotion failures must be captured, logged without secret output, and fail setup."
}
if ($installer -notmatch 'function InitializeUninstall\(\): Boolean;[\s\S]*?RunHaiMaintenanceTaskManager\(''Unregister''[\s\S]*?AcquireHaiMaintenanceMutex[\s\S]*?RunHaiMaintenanceTaskManager\(''AssertAbsent''[\s\S]*?ReleaseHaiMaintenanceMutex[\s\S]*?Exit;[\s\S]*?Result := True;' -or
    $installer -notmatch 'procedure DeinitializeUninstall\(\);[\s\S]*?ReleaseHaiMaintenanceMutex' -or
    $installer -notmatch 'WaitForSingleObject\(HaiMaintenanceMutexHandle, 0\)' -or
    $installer -match '(?im)^\s*\[UninstallRun\]') {
    throw 'Uninstall must unregister through the normally locked task manager, reacquire the installer lock, verify no task reappeared, cancel cleanly on failure, and preserve user data.'
}
$uninstallBlock = [Regex]::Match($installer, '(?ms)^function InitializeUninstall\(\): Boolean;(?<body>.*?)(?=^procedure DeinitializeUninstall)').Groups['body'].Value
$uninstallRuntimeStop = $uninstallBlock.IndexOf('StopHaiRuntimeForUninstall(ExitCode)', [StringComparison]::Ordinal)
$uninstallRuntimeFailure = $uninstallBlock.IndexOf('uninstall was cancelled', $uninstallRuntimeStop, [StringComparison]::OrdinalIgnoreCase)
$uninstallReleaseAfterStopFailure = $uninstallBlock.IndexOf('ReleaseHaiMaintenanceMutex', $uninstallRuntimeFailure, [StringComparison]::Ordinal)
$uninstallSuccess = $uninstallBlock.IndexOf('Result := True;', [StringComparison]::Ordinal)
$uninstallStopHelper = [Regex]::Match($installer, '(?ms)^function StopHaiRuntimeForUninstall\s*\(var ExitCode: Integer\): Boolean;(?<body>.*?)(?=^function InitializeUninstall)').Groups['body'].Value
$uninstallStopHelperScript = [Regex]::Match($support, '(?ms)^function Stop-HaiComposeRuntimeForUninstall\s*\{(?<body>.*?)(?=^function\s|^if \()').Groups['body'].Value
$uninstallEnvironmentCheck = $uninstallStopHelperScript.IndexOf('if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf))', [StringComparison]::Ordinal)
$uninstallDockerPreflight = $uninstallStopHelperScript.IndexOf('Assert-HaiDockerReady', [StringComparison]::Ordinal)
$uninstallComposeArgs = $uninstallStopHelperScript.IndexOf('Get-HaiComposeArguments', [StringComparison]::Ordinal)
$uninstallOwnershipCheck = $uninstallStopHelperScript.IndexOf('Assert-HaiSingleInstallation', [StringComparison]::Ordinal)
$uninstallConfigCheck = $uninstallStopHelperScript.IndexOf('docker @composeArguments config --quiet', [StringComparison]::Ordinal)
$uninstallComposeStop = $uninstallStopHelperScript.IndexOf('docker @composeArguments stop --timeout 30', [StringComparison]::Ordinal)
$uninstallRuntimeVerification = $uninstallStopHelperScript.IndexOf('docker @composeArguments ps --status running --quiet', [StringComparison]::Ordinal)
if ($uninstallRuntimeStop -lt 0 -or $uninstallRuntimeFailure -lt $uninstallRuntimeStop -or
    $uninstallReleaseAfterStopFailure -lt $uninstallRuntimeFailure -or $uninstallSuccess -lt $uninstallReleaseAfterStopFailure -or
    $uninstallStopHelper -notmatch '-StopRuntimeForUninstall' -or
    $uninstallStopHelperScript -notmatch 'LOCALAPPDATA' -or
    $uninstallStopHelperScript -notmatch 'HAI\\hai\.env' -or
    $uninstallEnvironmentCheck -lt 0 -or $uninstallEnvironmentCheck -gt $uninstallDockerPreflight -or
    $uninstallDockerPreflight -lt 0 -or $uninstallComposeArgs -lt $uninstallDockerPreflight -or
    $uninstallOwnershipCheck -lt $uninstallComposeArgs -or $uninstallConfigCheck -lt $uninstallOwnershipCheck -or
    $uninstallComposeStop -lt $uninstallConfigCheck -or $uninstallRuntimeVerification -lt $uninstallComposeStop -or
    $uninstallStopHelperScript -notmatch "(?s)@\(Get-HaiComposeArguments\)\s*\+\s*@\('--profile', '\*'\)" -or
    $uninstallStopHelperScript -notmatch 'if \(\$verifyExitCode -ne 0\)' -or
    $uninstallStopHelperScript -notmatch 'if \(\$runningContainerIds\.Count -gt 0\)' -or
    $uninstallStopHelperScript -match '(?i)\bdown\b|\bvolume\s+(?:rm|prune)\b|-v(?:\s|$)|\bdocker\s+(?:stop|kill)\b') {
    throw 'Uninstall must stop only the verified installed HAI Compose project after environment, Docker, and ownership checks; any failed check/stop must cancel uninstall and preserve files and volumes.'
}
if ($installer -match '(?i)restartreplace|Stop-Process|Stop-ScheduledTask') {
    throw "Installer synchronization must not depend on restartreplace or terminate/stop a maintenance worker."
}
if ($UninstallRuntimeOnly) {
    Write-Host 'Windows uninstall runtime stop, ownership, failure-cancellation, and data-preservation contracts passed.'
    exit 0
}
$maintenanceMutexName = 'Global\HAI.OpenClawMaintenance'
$taskRegistration = [Regex]::Match($maintenanceSupport, '(?ms)^function Register-HaiOpenClawMaintenanceTask\s*\{(?<body>.*?)(?=^function |\z)').Groups['body'].Value
$taskUnregistration = [Regex]::Match($maintenanceSupport, '(?ms)^function Unregister-HaiOpenClawMaintenanceTask\s*\{(?<body>.*?)(?=^function |\z)').Groups['body'].Value
$maintenanceLock = [Regex]::Match($maintenanceSupport, '(?ms)^function Enter-HaiOpenClawMaintenanceLock\s*\{(?<body>.*?)(?=^function |\z)').Groups['body'].Value
if ($support -notmatch [Regex]::Escape($maintenanceMutexName) -or
    $maintenanceSupport -notmatch [Regex]::Escape($maintenanceMutexName) -or
    $maintenanceLauncher -notmatch 'Hai-OpenClawMaintenance\.ps1' -or
    $maintenanceLauncher -notmatch 'Enter-HaiOpenClawMaintenanceLock\s+-WaitSeconds\s+\$LockWaitSeconds' -or
    $maintenanceLock -notmatch '\$script:HaiOpenClawMaintenanceMutexName' -or
    $maintenanceLock -notmatch 'WaitOne\(\[TimeSpan\]::FromSeconds\(\$WaitSeconds\)\)' -or
    $taskRegistration -notmatch 'Enter-HaiOpenClawMaintenanceLock' -or
    $taskRegistration -notmatch 'Exit-HaiOpenClawMaintenanceLock' -or
    $taskUnregistration -notmatch 'Enter-HaiOpenClawMaintenanceLock' -or
    $taskUnregistration -notmatch 'Exit-HaiOpenClawMaintenanceLock' -or
    $maintenanceTaskManager -match 'InstallerOwnsMaintenanceLock' -or
    $maintenanceTaskManager -notmatch 'AssertAbsent' -or
    $maintenanceTaskManager -notmatch 'SkipImmediateRun' -or
    $installer -match 'InstallerOwnsMaintenanceLock' -or
    $maintenanceLauncher -match '(?i)Stop-Process|Stop-ScheduledTask|taskkill') {
    throw "Installer, task registration, and the worker must share the mutex; task mutation must fail closed and never stop a running worker."
}
$dshDocumentation = [Regex]::Match([IO.File]::ReadAllText($documentation), '(?ms)^## DeepSeek Harness execution status\s*(?<body>.*?)(?=^## |\z)').Groups['body'].Value
if ($dshDocumentation -notmatch '\*\*hard-disabled\*\*' -or
    $dshDocumentation -notmatch 'does not provide an authenticated worker heartbeat' -or
    $dshDocumentation -match 'The worker runs only already-approved tasks|obtains a final server confirmation immediately before starting') {
    throw 'The installer guide must describe DSH as hard-disabled and must not promise live worker or start/cancellation behavior.'
}
$dshBlockReason = [Regex]::Match($support, '(?ms)^function Get-HaiHostRuntimeIsolationBlockReason\s*\{(?<body>.*?)(?=^function |\z)').Groups['body'].Value
if ($dshBlockReason -notmatch 'no verified OS-enforced sandbox' -or
    $dshBlockReason -notmatch 'authenticated peer-verified bridge channel' -or
    $dshBlockReason -notmatch 'acknowledged start/cancellation protocol' -or
    $dshBlockReason -notmatch 'no environment-variable override') {
    throw 'The Windows DSH hard-disable must explain every outstanding security gate and reject environment overrides.'
}
$silentConfigurationMarker = $support.IndexOf('if ($ConfigureSilentUpgrade)', [StringComparison]::Ordinal)
if ($silentConfigurationMarker -lt 0) {
    throw "Installer support is missing the dedicated silent-upgrade configuration path."
}
$silentConfiguration = $support.Substring($silentConfigurationMarker)
foreach ($required in @('Initialize-HaiLocalEnvironment', 'Register-HaiOpenClawMaintenanceTask', '-SkipImmediateRun', 'No existing HAI environment')) {
    if ($silentConfiguration -notmatch [Regex]::Escape($required)) {
        throw "Silent-upgrade configuration is missing '$required'."
    }
}
if ($silentConfiguration -match '(?im)^\s*(?:&\s*docker|Start-HAI|Start-Process)\b') {
    throw "Silent-upgrade configuration must not launch HAI, Docker, or an interactive process."
}
if ($silentConfiguration -match 'Promote-HaiMaintenanceWorker') {
    throw "Silent-upgrade configuration must not reacquire the installer-held worker lock or promote the executable a second time."
}
$promotionStart = $support.IndexOf('function Promote-HaiMaintenanceWorker', [StringComparison]::Ordinal)
$promotionEnd = $support.IndexOf('function Get-HaiDataRoot', $promotionStart, [StringComparison]::Ordinal)
$promotionBody = if ($promotionStart -ge 0 -and $promotionEnd -gt $promotionStart) { $support.Substring($promotionStart, $promotionEnd - $promotionStart) } else { '' }
if ($support -match 'InstallerOwnsMaintenanceLock|InstallerOwnsLock' -or
    $promotionBody -notmatch '\$mutex\.WaitOne\(' -or
    $promotionBody -notmatch 'File\]::Replace\(\$pendingPath, \$workerPath, \$backupPath\)' -or
    $promotionBody -notmatch 'Test-HaiMaintenanceWorkerRunning' -or
    $promotionBody -notmatch 'WaitSeconds' -or
    $promotionBody -match '(?i)Stop-Process|Stop-ScheduledTask|taskkill') {
    throw "Worker promotion must acquire the shared lock, wait/retry safely against legacy processes and atomic file locks, and never terminate a worker."
}
if ($initializer -notmatch 'Write-HaiAclProtectedFile -Path \$temporaryPath -Bytes \$updatedBytes -FileSecurity \$environmentAcl' -or
    $initializer -notmatch 'Write-HaiAclProtectedFile -Path \$temporaryPath -Bytes \$environmentBytes -FileSecurity \$environmentAcl' -or
    $initializer -notmatch 'if \(\$targetExists -and \$MigrateExisting\)\s*\{\s*\$environmentAcl = Get-Acl -LiteralPath \$EnvFile\s*\}\s*else\s*\{\s*\$environmentAcl = New-HaiRestrictedEnvironmentFileSecurity' -or
    $initializer -notmatch 'Set-HaiExistingFileAccessRules -Path \$EnvFile -DesiredSecurity \$environmentAcl' -or
    $initializer -match '\[IO\.File\]::WriteAll(?:Bytes|Text)\(\$temporaryPath') {
    throw "Existing-install migration must preserve its ACL, while first-run or forced initialization must restrict and verify the target and temporary-file ACLs before replacement and secret writes."
}
& $maintenanceMigrationTest
& $maintenanceLauncherTest
& $maintenancePromotionTest
if (-not $OpenClawMaintenanceOnly) {
    & $runtimeLifecycleTest
} else {
    foreach ($required in @('Uninstall waits for the same maintenance lock', 'uninstall is cancelled')) {
        if ($docs -notmatch [Regex]::Escape($required)) {
            throw "Windows installer documentation is missing '$required'."
        }
    }
    Write-Host 'OpenClaw Windows installer static, scheduling, locking, upgrade, and uninstall contracts passed with offline maintenance fixtures only.'
    exit 0
}

foreach ($required in @(
    "BACKEND_DB_USER",
    "BACKEND_DB_PASSWORD",
    "DB_MIGRATIONS_ENABLED"
)) {
    if ($exampleEnvironment -notmatch "(?m)^$([Regex]::Escape($required))=") {
        throw "The least-privilege database contract is missing '$required' from .env.example."
    }
    if ($initializer -notmatch [Regex]::Escape($required)) {
        throw "The Windows initializer does not configure '$required'."
    }
}

foreach ($required in @(
    "BACKEND_DB_USER",
    "BACKEND_DB_PASSWORD",
    "backend-runtime-role",
    "--no-deps",
    "DB_MIGRATIONS_ENABLED=false"
)) {
    if ($runtimeDatabaseRole -notmatch [Regex]::Escape($required)) {
        throw "The Windows runtime-role repair path is missing '$required'."
    }
}

foreach ($required in @(
    "ALTER DEFAULT PRIVILEGES",
    "NOSUPERUSER",
    "NOCREATEDB",
    "NOCREATEROLE",
    "NOBYPASSRLS",
    "NOREPLICATION"
)) {
    if ($runtimeRoleProvisioner -notmatch [Regex]::Escape($required)) {
        throw "The shared runtime database role provisioner is missing '$required'."
    }
}

foreach ($required in @(
    "ComposeProjectName",
    "Assert-HaiComposeProjectAvailable",
    "com.docker.compose.project.working_dir",
    "Another HAI installation already owns Docker project"
)) {
    if ($initializer -notmatch [Regex]::Escape($required)) {
        throw "The first-run initializer does not protect Docker project ownership: '$required'."
    }
}

foreach ($forbidden in @(
    "Copy-Item -Path (Join-Path $repositoryRoot '.env.local')",
    "GATEWAY_HOST_BIND=0.0.0.0",
    "LOCAL_LOGIN_BYPASS_ENABLED=true",
    "-match '(^|/)\\.env($|\\.)'"
)) {
    if ($build -match [Regex]::Escape($forbidden)) {
        throw "Installer build script contains unsafe value '$forbidden'."
    }
}

foreach ($required in @(
    "[Setup]",
    "[Files]",
    "[Icons]",
    "[Run]",
    "HAI Local",
    "Start-HAI.ps1",
    "Stop-HAI.ps1",
    "Open local dashboard"
)) {
    if ($installer -notmatch [Regex]::Escape($required)) {
        throw "Inno Setup contract is missing '$required'."
    }
}

if ($build -notmatch [Regex]::Escape('HAI_INSTALLER_OUTPUT_DIR') -or
    $installer -notmatch [Regex]::Escape('HAI_INSTALLER_OUTPUT_DIR')) {
    throw "Installer output-directory configuration is not wired through the build and Inno Setup scripts."
}

foreach ($forbidden in @(
    ".env.local",
    "db_data_automation",
    "db_data_idp",
    "connected-sources\\*"
)) {
    if ($installer -match [Regex]::Escape($forbidden)) {
        throw "Inno Setup script must not directly package local data: '$forbidden'."
    }
}

foreach ($required in @(
    "Docker Desktop",
    "127.0.0.1",
    "%LOCALAPPDATA%\HAI",
    "Uninstall",
    "does not delete"
)) {
    if ($docs -notmatch [Regex]::Escape($required)) {
        throw "Installer documentation is missing '$required'."
    }
}
foreach ($required in @(
    'Uninstall waits for the same maintenance lock',
    'uninstall is cancelled'
)) {
    if ($docs -notmatch [Regex]::Escape($required)) {
        throw "Windows installer documentation is missing '$required'."
    }
}

foreach ($required in @(
    '{{json .Config.Labels}}',
    'ConvertFrom-Json',
    'COMPOSE_PROJECT_NAME',
    'Assert-HaiComposeOwnership -ProjectName $composeProjectName',
    'Refusing to manage cloud access until ownership can be verified'
)) {
    if ($ngrokStart -notmatch [Regex]::Escape($required)) {
        throw "The cloud-tunnel ownership gate is missing '$required'."
    }
}

foreach ($required in @(
    'db_password="$(secret)"',
    'first_run_admin_password="$(secret)"',
    'DB_PASSWORD=$db_password',
    'FIRST_RUN_ADMIN_PASSWORD=$first_run_admin_password'
)) {
    if ($secretGenerator -notmatch [Regex]::Escape($required)) {
        throw "The cross-platform secret generator is missing '$required'."
    }
}
if ($initializer -notmatch [Regex]::Escape('FIRST_RUN_ADMIN_PASSWORD')) {
    throw "The Windows initializer must configure the first-run owner password."
}

if ($exampleEnvironment -match [Regex]::Escape('DB_PASSWORD=postgres') -or
    $exampleEnvironment -match [Regex]::Escape('FIRST_RUN_ADMIN_PASSWORD=ChangeMe123!')) {
    throw "The safe environment template must not ship a known database or owner password."
}

foreach ($required in @(
    "'DB_PASSWORD'",
    "'FIRST_RUN_ADMIN_PASSWORD'"
)) {
    if ($ngrokStart -notmatch [Regex]::Escape($required)) {
        throw "The cloud-tunnel secret gate is missing '$required'."
    }
}

if ($SkipPayload) {
    & $sourceSelectionTest
    Write-Host "Windows installer source contracts passed without regenerating the release payload."
    exit 0
}

& $buildScript -SkipCompile -AllowDirtyWorktree
if ($LASTEXITCODE -ne 0) {
    throw "Installer payload preparation failed."
}
$payloadRoot = Join-Path $repositoryRoot "installer\release\payload"
foreach ($forbiddenPayloadPath in @(
    ".env.local",
    ".env-backend",
    ".env-gateway",
    ".env-idp",
    "connected-sources\private.txt"
)) {
    if (Test-Path -LiteralPath (Join-Path $payloadRoot $forbiddenPayloadPath)) {
        throw "Installer payload contains excluded local data: $forbiddenPayloadPath"
    }
}
if (-not (Test-Path -LiteralPath (Join-Path $payloadRoot ".env.example") -PathType Leaf)) {
    throw "Installer payload is missing the safe environment template."
}
foreach ($requiredPayloadPath in @(
    "docker-compose.local.yml",
    "nginx-config\a2a-local.conf.template",
    "installer\windows\Start-HAI.ps1",
    "installer\windows\Stop-HAI.ps1",
    "installer\windows\HAI-Status.ps1",
    "installer\windows\Test-HAI-LocalConnector.ps1",
    "installer\windows\Manage-HAI-OpenClawMaintenanceTask.ps1"
)) {
    if (-not (Test-Path -LiteralPath (Join-Path $payloadRoot $requiredPayloadPath) -PathType Leaf)) {
        throw "Installer payload is missing required product file: $requiredPayloadPath"
    }
}

Write-Host "Windows installer behavioral contracts passed."
