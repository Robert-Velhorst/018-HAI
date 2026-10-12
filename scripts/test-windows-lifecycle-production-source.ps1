[CmdletBinding()]
param([switch]$MockedStop, [switch]$MockedOwnership)

$ErrorActionPreference = 'Stop'
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path

function Assert-HaiLifecycleSource([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

function Read-HaiLifecycleSource([string]$RelativePath) {
    $path = Join-Path $repositoryRoot $RelativePath
    $tokens = $null
    $errors = $null
    $ast = [Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors)
    Assert-HaiLifecycleSource ($errors.Count -eq 0) "PowerShell syntax errors in ${RelativePath}: $($errors.Message -join '; ')"
    return [pscustomobject]@{ Ast = $ast; Text = [IO.File]::ReadAllText($path) }
}

function Get-HaiLifecycleFunction($Source, [string]$Name) {
    $functions = @($Source.Ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $Name
    }, $true))
    Assert-HaiLifecycleSource ($functions.Count -eq 1) "Expected one $Name definition."
    return $functions[0].Extent.Text
}

$support = Read-HaiLifecycleSource 'installer\windows\Hai-InstallerSupport.ps1'
$start = Read-HaiLifecycleSource 'installer\windows\Start-HAI.ps1'
$stop = Read-HaiLifecycleSource 'installer\windows\Stop-HAI.ps1'
$open = Read-HaiLifecycleSource 'installer\windows\Open-HAI.ps1'
$null = Read-HaiLifecycleSource 'scripts\test-windows-runtime-lifecycle.ps1'
$null = Read-HaiLifecycleSource 'scripts\test-windows-installer-update-promotion.ps1'

$project = Get-HaiLifecycleFunction $support 'Get-HaiComposeProjectName'
Assert-HaiLifecycleSource ($project.Contains('(?<value>[^\r\n]*)') -and
    $project.IndexOf('$projectMatches.Count -gt 1', [StringComparison]::Ordinal) -lt
    $project.IndexOf("'\A[a-z0-9][a-z0-9_-]*\z'", [StringComparison]::Ordinal)) 'Project validation must count all assignments before validating any value.'
Assert-HaiLifecycleSource ($project -match '\$projectName\s+-cnotmatch') 'Project-name validation must reject uppercase values without silently selecting a different project.'

$ownership = Get-HaiLifecycleFunction $support 'Assert-HaiSingleInstallation'
Assert-HaiLifecycleSource ($ownership.Contains('label=com.docker.compose.project.config_files') -and
    $ownership.Contains('label=com.docker.compose.project=$projectName') -and
    $ownership.Contains('Select-Object -Unique')) 'Ownership enumeration must include and deduplicate both recognizable HAI stacks and configured-project collisions.'
Assert-HaiLifecycleSource ($ownership -match 'if \(\$usesConfiguredProject -or \$containerId -in \$projectContainerIds\)' -and
    $ownership -match 'if \(-not \$usesConfiguredProject -and \$containerId -notin \$projectContainerIds -and') 'Configured-project containers must not bypass ownership validation through missing labels or unknown service names.'
Assert-HaiLifecycleSource ($ownership -notmatch '& docker[^\r\n]*\b(up|stop|down|rm|kill)\b') 'Ownership inspection must not mutate Docker.'

$lastOwnershipCheck = $start.Text.LastIndexOf('Assert-HaiSingleInstallation', [StringComparison]::Ordinal)
$lastInitialization = $start.Text.LastIndexOf('Initialize-HaiLocalEnvironment', [StringComparison]::Ordinal)
$composeSelection = $start.Text.IndexOf('$composeArguments = Get-HaiComposeArguments', [StringComparison]::Ordinal)
Assert-HaiLifecycleSource ($lastInitialization -lt $lastOwnershipCheck -and $lastOwnershipCheck -lt $composeSelection) 'Start must verify ownership again after initialization and before choosing Compose arguments.'
Assert-HaiLifecycleSource ($stop.Text -match '(?s)Suspend-HaiComposeA2AOverrides\s+try\s*\{\s*& docker @composeArguments --profile local-a2a stop\s+\$composeStopExitCode = \$LASTEXITCODE\s*\}\s*finally\s*\{\s*Restore-HaiComposeA2AOverrides' -and
    $stop.Text -match 'if \(\$composeStopExitCode -ne 0\)') 'Stop must suppress ambient overrides, capture the Docker result, and restore caller state even on failure.'
Assert-HaiLifecycleSource ($open.Text -match 'Test-HaiEnvironmentFileAvailable' -and
    $open.Text -match 'Get-HaiStackReadinessResult' -and
    $open.Text -match 'Start-Process\s+-FilePath\s+\$dashboardUrl' -and
    $open.Text -notmatch 'Start-HaiComposeStack') 'Open must verify protected environment and runtime readiness before opening the dashboard, without starting containers.'

$promotion = Get-HaiLifecycleFunction $support 'Promote-HaiMaintenanceWorker'
$destinationGuard = $promotion.IndexOf('$workerItem.PSIsContainer', [StringComparison]::Ordinal)
$firstReplacement = $promotion.IndexOf('[IO.File]::Replace($pendingPath, $workerPath, $backupPath)', [StringComparison]::Ordinal)
Assert-HaiLifecycleSource ($destinationGuard -ge 0 -and $destinationGuard -lt $firstReplacement -and
    $promotion -match '\$workerItem.Attributes -band \[IO.FileAttributes\]::ReparsePoint') 'Promotion must reject directories and reparse-point destinations before replacement.'
Write-Output 'Windows lifecycle production source checks passed (syntax and source guards only; no runtime acceptance).'

if ($MockedOwnership) {
    $ownershipBody = [scriptblock]::Create($ownership)
    & {
        param($Body, $Root)
        . $Body
        $state = @{ Containers = @(); Calls = @(); EnumerationFailure = $false }
        $rootPath = [IO.Path]::GetFullPath($Root)
        $composePath = Join-Path $rootPath 'docker-compose.local.yml'
        function Get-HaiInstallRoot { return $rootPath }
        function Get-HaiComposeProjectName { return '018-hai' }
        function Get-HaiComposeFile { return $composePath }
        function docker {
            $state.Calls += ($args -join ' ')
            if ($args[0] -ceq 'ps') {
                $global:LASTEXITCODE = if ($state.EnumerationFailure) { 23 } else { 0 }
                if ($state.EnumerationFailure) { return }
                foreach ($container in $state.Containers) {
                    if (($args -contains 'label=com.docker.compose.project.config_files' -and -not [string]::IsNullOrWhiteSpace($container.Config)) -or
                        $args -contains "label=com.docker.compose.project=$($container.Project)") {
                        $container.Id
                    }
                }
                return
            }
            if ($args[0] -cne 'inspect') { throw 'The ownership check attempted an unexpected Docker operation.' }
            $containerId = $args[-1]
            $container = @($state.Containers | Where-Object { $_.Id -ceq $containerId })
            Assert-HaiLifecycleSource ($container.Count -eq 1) 'An ownership container was inspected without a unique fixture.'
            $labels = @{
                'com.docker.compose.project' = $container[0].Project
                'com.docker.compose.project.working_dir' = $container[0].Root
                'com.docker.compose.project.config_files' = $container[0].Config
                'com.docker.compose.service' = $container[0].Service
            }
            $global:LASTEXITCODE = 0
            return ('/' + $container[0].Id + '|' + ($labels | ConvertTo-Json -Compress))
        }
        $cases = @(
            @{ Id = 'owned'; Project = '018-hai'; Root = $rootPath; Config = $composePath; Service = 'custom-service'; Reject = $false },
            @{ Id = 'foreign-config'; Project = '018-hai'; Root = $rootPath; Config = (Join-Path $rootPath 'compose.yaml'); Service = 'custom-service'; Reject = $true },
            @{ Id = 'missing-config'; Project = '018-hai'; Root = $rootPath; Config = ''; Service = 'custom-service'; Reject = $true },
            @{ Id = 'missing-owner'; Project = '018-hai'; Root = ''; Config = $composePath; Service = 'custom-service'; Reject = $true },
            @{ Id = 'unrelated'; Project = 'other'; Root = $rootPath; Config = (Join-Path $rootPath 'compose.yaml'); Service = 'custom-service'; Reject = $false }
        )
        foreach ($case in $cases) {
            $state.Containers = @([pscustomobject]$case)
            $state.Calls = @()
            $failure = ''
            try { Assert-HaiSingleInstallation } catch { $failure = $_.Exception.Message }
            Assert-HaiLifecycleSource ((-not [string]::IsNullOrEmpty($failure)) -eq $case.Reject) "Unexpected ownership result for $($case.Id): $failure"
            Assert-HaiLifecycleSource (-not ($state.Calls -match '\b(up|stop|down|rm|kill)\b')) 'Ownership inspection mutated Docker.'
            $inspections = @($state.Calls | Where-Object { $_ -match '^inspect ' })
            Assert-HaiLifecycleSource ($inspections.Count -eq 1) 'Ownership enumeration did not deduplicate a container selected by both labels.'
        }
        $state.EnumerationFailure = $true
        $failure = ''
        try { Assert-HaiSingleInstallation } catch { $failure = $_.Exception.Message }
        Assert-HaiLifecycleSource ($failure -match 'Could not enumerate Docker Compose containers') 'Failed enumeration did not refuse ownership inspection.'
    } $ownershipBody $repositoryRoot
    Write-Output 'Mocked ownership selection, collisions, missing metadata, deduplication and enumeration-failure checks passed; no real Docker operation was invoked.'
}

if ($MockedStop) {
    # Load only the stop entrypoint body, after its support import, into an
    # isolated scope. Every operational helper is replaced by a local mock.
    $import = @($stop.Ast.EndBlock.Statements | Where-Object {
        $_.Extent.Text -match '^\. \(Join-Path \$PSScriptRoot "Hai-InstallerSupport\.ps1"\)'
    })
    Assert-HaiLifecycleSource ($import.Count -eq 1) 'The stop entrypoint support import could not be isolated safely.'
    $stopBody = [scriptblock]::Create($stop.Text.Substring($import[0].Extent.EndOffset))
    & {
        param($Body)
        $state = @{ Mode = 'success'; Suspended = $false; Restored = $false; Calls = 0; Restorations = 0 }
        function Stop-HaiHostRuntimeWorker { }
        function Assert-HaiDockerReady { }
        function Assert-HaiSingleInstallation { }
        function Get-HaiEnvironmentFile { return 'source-only-fixture.env' }
        function Test-Path { param([string]$LiteralPath, [string]$PathType) return $true }
        function Get-HaiComposeArguments { return @('compose', '--project-name', '018-hai') }
        function Suspend-HaiComposeA2AOverrides {
            $state.Suspended = $true
            return @{ COMPOSE_PROFILES = 'unrelated'; HAI_A2A_BRIDGE_URL = 'invalid' }
        }
        function Restore-HaiComposeA2AOverrides {
            param([hashtable]$Overrides)
            Assert-HaiLifecycleSource ($Overrides.COMPOSE_PROFILES -ceq 'unrelated' -and $Overrides.HAI_A2A_BRIDGE_URL -ceq 'invalid') 'Stop lost the caller overrides.'
            $state.Suspended = $false
            $state.Restored = $true
            $state.Restorations++
            # Deliberately disturb LASTEXITCODE to verify that stop uses its snapshot.
            $global:LASTEXITCODE = 0
        }
        function docker {
            Assert-HaiLifecycleSource ($state.Suspended -and ($args -join ' ') -ceq 'compose --project-name 018-hai --profile local-a2a stop') 'Stop did not use the pinned project and suspended overrides.'
            $state.Calls++
            if ($state.Mode -ceq 'throw') { throw 'mock Docker failure' }
            $global:LASTEXITCODE = if ($state.Mode -ceq 'exit') { 23 } else { 0 }
        }
        foreach ($mode in @('success', 'exit', 'throw')) {
            $state.Mode = $mode
            $state.Suspended = $false
            $state.Restored = $false
            $state.Calls = 0
            $state.Restorations = 0
            $failure = ''
            try { & $Body } catch { $failure = $_.Exception.Message }
            Assert-HaiLifecycleSource ($state.Restored -and -not $state.Suspended -and $state.Calls -eq 1 -and $state.Restorations -eq 1) "Stop did not restore overrides exactly once after $mode."
            if ($mode -ceq 'success') {
                Assert-HaiLifecycleSource ([string]::IsNullOrEmpty($failure)) 'A successful mocked stop failed.'
            } elseif ($mode -ceq 'exit') {
                Assert-HaiLifecycleSource ($failure -match 'could not be stopped cleanly') 'Restoring caller state hid the failed Compose exit code.'
            } else {
                Assert-HaiLifecycleSource ($failure -ceq 'mock Docker failure') 'Stop hid a terminating Docker error.'
            }
        }
    } $stopBody
    Write-Output 'Mocked Stop success, nonzero exit, and terminating-error restoration checks passed; no real Docker or process operation was invoked.'
}
