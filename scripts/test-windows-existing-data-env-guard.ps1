[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
. (Join-Path $repositoryRoot 'installer\windows\Hai-InstallerSupport.ps1')
$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ('hai-existing-data-env-guard-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temporaryRoot -Force | Out-Null
$script:fixtureEnvironmentFile = Join-Path $temporaryRoot 'hai.env'
$global:haiGuardDockerFixture = [pscustomobject]@{ Mode = 'empty'; Calls = 0 }

function Assert-HaiGuardTest {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Get-HaiEnvironmentFile { return $script:fixtureEnvironmentFile }
function Get-HaiComposeProjectName { return 'hai-guard-test' }
function docker {
    $global:haiGuardDockerFixture.Calls++
    $commandLine = $args -join ' '
    if ($global:haiGuardDockerFixture.Mode -eq 'inspection-fails') {
        $global:LASTEXITCODE = 23
        return
    }
    $global:LASTEXITCODE = 0
    if ($commandLine -match '^volume ls --quiet --filter') {
        if ($global:haiGuardDockerFixture.Mode -eq 'has-volumes') { Write-Output 'hai-guard-test-postgres-data' }
        return
    }
    if ($commandLine -eq 'volume ls --quiet') {
        if ($global:haiGuardDockerFixture.Mode -eq 'has-unlabeled-volumes') { Write-Output '018-hai-postgres-idp-data' }
        return
    }
    if ($commandLine -match '^ps -aq') { return }
}

try {
    [IO.File]::WriteAllText($script:fixtureEnvironmentFile, 'TEST_ONLY=true')
    Assert-HaiExistingDataRequiresEnvironment
    Assert-HaiGuardTest ($global:haiGuardDockerFixture.Calls -eq 0) 'An existing environment file should not trigger Docker volume inspection.'

    [IO.File]::Delete($script:fixtureEnvironmentFile)
    $global:haiGuardDockerFixture.Mode = 'empty'
    Assert-HaiExistingDataRequiresEnvironment
    Assert-HaiGuardTest ($global:haiGuardDockerFixture.Calls -eq 2) 'A clean first run should inspect project labels and fixed HAI volume names.'

    $global:haiGuardDockerFixture.Mode = 'has-volumes'
    $failure = ''
    try { Assert-HaiExistingDataRequiresEnvironment } catch { $failure = $_.Exception.Message }
    Assert-HaiGuardTest ($failure -match 'existing data volumes.*protected environment file is missing') 'Existing data without its environment file must refuse credential regeneration.'
    Assert-HaiGuardTest ($global:haiGuardDockerFixture.Calls -eq 4) 'Labeled existing data should be rejected after its read-only volume inventories.'

    $global:haiGuardDockerFixture.Mode = 'has-unlabeled-volumes'
    $failure = ''
    try { Assert-HaiExistingDataRequiresEnvironment } catch { $failure = $_.Exception.Message }
    Assert-HaiGuardTest ($failure -match 'existing data volumes.*protected environment file is missing') 'Fixed-name HAI volumes must be detected even when their old Compose label differs.'
    Assert-HaiGuardTest ($global:haiGuardDockerFixture.Calls -eq 6) 'Unlabeled-data detection did not inspect both volume inventories.'

    $global:haiGuardDockerFixture.Mode = 'inspection-fails'
    $failure = ''
    try { Assert-HaiExistingDataRequiresEnvironment } catch { $failure = $_.Exception.Message }
    Assert-HaiGuardTest ($failure -match 'Could not verify existing HAI data volumes') 'Docker inspection failure must fail closed.'
    Assert-HaiGuardTest ($global:haiGuardDockerFixture.Calls -eq 7) 'Docker inspection failure did not stop after the first read-only query.'

    $global:haiGuardDockerFixture.Mode = 'has-volumes'
    $failure = ''
    try {
        & (Join-Path $repositoryRoot 'scripts\initialize-windows.ps1') `
            -EnvFile $script:fixtureEnvironmentFile `
            -AdminEmail 'owner@example.test' `
            -AdminPasswordPlainText 'test-password-1234' `
            -ComposeProjectName 'hai-guard-test'
    } catch { $failure = $_.Exception.Message }
    Assert-HaiGuardTest ($failure -match 'Existing HAI data volumes were found') "The standalone initializer must refuse to replace credentials while HAI volumes exist. Actual error: $failure"
    Assert-HaiGuardTest (-not (Test-Path -LiteralPath $script:fixtureEnvironmentFile)) 'The standalone initializer wrote credentials before refusing existing data.'
    Assert-HaiGuardTest ($global:haiGuardDockerFixture.Calls -eq 9) 'The standalone initializer did not stop after detecting labeled HAI volumes.'

    Write-Output 'Existing HAI data/environment guards passed (first-run, labeled/unlabeled data, initializer bypass, and inspection failure).'
} finally {
    $resolvedTempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    $resolvedFixtureRoot = [IO.Path]::GetFullPath($temporaryRoot)
    if ($resolvedFixtureRoot.StartsWith($resolvedTempRoot, [StringComparison]::OrdinalIgnoreCase) -and
        (Split-Path -Leaf $resolvedFixtureRoot).StartsWith('hai-existing-data-env-guard-', [StringComparison]::Ordinal) -and
        (Test-Path -LiteralPath $resolvedFixtureRoot -PathType Container)) {
        Remove-Item -LiteralPath $resolvedFixtureRoot -Recurse -Force
    }
    Remove-Variable -Name haiGuardDockerFixture -Scope Global -ErrorAction SilentlyContinue
}
