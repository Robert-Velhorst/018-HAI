[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
. (Join-Path $repositoryRoot 'installer\windows\Hai-InstallerSupport.ps1')
$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ('hai-start-env-validation-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temporaryRoot -Force | Out-Null
$script:fixtureEnvironmentFile = Join-Path $temporaryRoot 'hai.env'

function Get-HaiEnvironmentFile { return $script:fixtureEnvironmentFile }
function Assert-HaiEnvironmentTest {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

try {
    $startSource = [IO.File]::ReadAllText((Join-Path $repositoryRoot 'installer\windows\Start-HAI.ps1'))
    $initializeIndex = $startSource.IndexOf('Initialize-HaiLocalEnvironment', [StringComparison]::Ordinal)
    $validateIndex = $startSource.IndexOf('Assert-HaiRequiredEnvironment', [StringComparison]::Ordinal)
    $startWorkerIndex = $startSource.IndexOf('Start-HaiHostRuntimeWorker', [StringComparison]::Ordinal)
    $stopWorkerIndex = $startSource.IndexOf('Stop-HaiHostRuntimeWorkerIfPresent', [StringComparison]::Ordinal)
    $composeIndex = $startSource.IndexOf('Start-HaiComposeStack', [StringComparison]::Ordinal)
    if ($initializeIndex -lt 0 -or $validateIndex -le $initializeIndex -or
        $startWorkerIndex -le $validateIndex -or $stopWorkerIndex -le $validateIndex -or
        $composeIndex -le $validateIndex) {
        throw 'Required environment validation must follow initialization and precede worker or Compose changes.'
    }

    $settings = @(
        'BACKEND_API_SHARED_KEY',
        'JWT_SECRET',
        'HAI_MEMORY_ENCRYPTION_KEY',
        'HAI_APPROVAL_PROOF_SIGNING_KEY',
        'DB_PASSWORD',
        'BACKEND_DB_PASSWORD',
        'FIRST_RUN_ADMIN_EMAIL',
        'FIRST_RUN_ADMIN_PASSWORD'
    )
    $validContent = ($settings | ForEach-Object {
        switch ($_) {
            'FIRST_RUN_ADMIN_EMAIL' { "$_=owner@example.test"; break }
            'FIRST_RUN_ADMIN_PASSWORD' { "$_=LongFixturePassword123!"; break }
            default { "$_=fixture-secret-not-for-output" }
        }
    }) -join "`n"
    [IO.File]::WriteAllText($script:fixtureEnvironmentFile, $validContent)
    Assert-HaiRequiredEnvironment

    $missingContent = ($settings | Where-Object { $_ -ne 'JWT_SECRET' } | ForEach-Object { "$_=fixture-secret-not-for-output" }) -join "`n"
    [IO.File]::WriteAllText($script:fixtureEnvironmentFile, $missingContent)
    $failure = ''
    try { Assert-HaiRequiredEnvironment } catch { $failure = $_.Exception.Message }
    Assert-HaiEnvironmentTest ($failure -match "required setting 'JWT_SECRET' is missing") 'A missing required setting must be identified before startup.'
    Assert-HaiEnvironmentTest ($failure -notmatch 'fixture-secret-not-for-output') 'Environment validation must never disclose a secret value.'

    $emptyContent = $validContent -replace '(?m)^JWT_SECRET=.*$', 'JWT_SECRET='
    [IO.File]::WriteAllText($script:fixtureEnvironmentFile, $emptyContent)
    $failure = ''
    try { Assert-HaiRequiredEnvironment } catch { $failure = $_.Exception.Message }
    Assert-HaiEnvironmentTest ($failure -match "required setting 'JWT_SECRET' is missing or empty") 'An empty required setting must be identified before startup.'
    Assert-HaiEnvironmentTest ($failure -notmatch 'fixture-secret-not-for-output') 'Empty-setting validation must never disclose another secret value.'

    $duplicateContent = $validContent + "`nJWT_SECRET=another-fixture-secret"
    [IO.File]::WriteAllText($script:fixtureEnvironmentFile, $duplicateContent)
    $failure = ''
    try { Assert-HaiRequiredEnvironment } catch { $failure = $_.Exception.Message }
    Assert-HaiEnvironmentTest ($failure -match "setting 'JWT_SECRET' appears more than once") 'Duplicate required settings must fail before startup.'
    Assert-HaiEnvironmentTest ($failure -notmatch 'fixture-secret-not-for-output|another-fixture-secret') 'Duplicate-setting validation must never disclose secret values.'

    $malformedContent = $validContent -replace '(?m)^JWT_SECRET=.*$', 'JWT_SECRET="unterminated'
    [IO.File]::WriteAllText($script:fixtureEnvironmentFile, $malformedContent)
    $failure = ''
    try { Assert-HaiRequiredEnvironment } catch { $failure = $_.Exception.Message }
    Assert-HaiEnvironmentTest ($failure -match "setting 'JWT_SECRET' has malformed quoting") 'Malformed required settings must fail before startup.'
    Assert-HaiEnvironmentTest ($failure -notmatch 'unterminated|fixture-secret-not-for-output') 'Malformed-setting validation must never disclose values.'

    $placeholderContent = $validContent -replace '(?m)^JWT_SECRET=.*$', 'JWT_SECRET=replace-me'
    [IO.File]::WriteAllText($script:fixtureEnvironmentFile, $placeholderContent)
    $failure = ''
    try { Assert-HaiRequiredEnvironment } catch { $failure = $_.Exception.Message }
    Assert-HaiEnvironmentTest ($failure -match "setting 'JWT_SECRET' still contains a template placeholder") 'Secret placeholders must fail before startup.'

    $invalidEmailContent = $validContent -replace '(?m)^FIRST_RUN_ADMIN_EMAIL=.*$', 'FIRST_RUN_ADMIN_EMAIL=not-an-email'
    [IO.File]::WriteAllText($script:fixtureEnvironmentFile, $invalidEmailContent)
    $failure = ''
    try { Assert-HaiRequiredEnvironment } catch { $failure = $_.Exception.Message }
    Assert-HaiEnvironmentTest ($failure -match "setting 'FIRST_RUN_ADMIN_EMAIL' is not a valid email address") 'An invalid owner email must fail before startup.'

    $shortPasswordContent = $validContent -replace '(?m)^FIRST_RUN_ADMIN_PASSWORD=.*$', 'FIRST_RUN_ADMIN_PASSWORD=short'
    [IO.File]::WriteAllText($script:fixtureEnvironmentFile, $shortPasswordContent)
    $failure = ''
    try { Assert-HaiRequiredEnvironment } catch { $failure = $_.Exception.Message }
    Assert-HaiEnvironmentTest ($failure -match "setting 'FIRST_RUN_ADMIN_PASSWORD' is shorter than 12 characters") 'An invalid first-run password must fail before startup.'

    Write-Output 'Windows required-environment startup validation passed (required fields, duplicates, syntax, placeholders, identity, and secret redaction).'
} finally {
    $resolvedTempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    $resolvedFixtureRoot = [IO.Path]::GetFullPath($temporaryRoot)
    if ($resolvedFixtureRoot.StartsWith($resolvedTempRoot, [StringComparison]::OrdinalIgnoreCase) -and
        (Split-Path -Leaf $resolvedFixtureRoot).StartsWith('hai-start-env-validation-', [StringComparison]::Ordinal) -and
        (Test-Path -LiteralPath $resolvedFixtureRoot -PathType Container)) {
        Remove-Item -LiteralPath $resolvedFixtureRoot -Recurse -Force
    }
}
