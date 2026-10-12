[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
. (Join-Path $repositoryRoot 'installer\windows\Hai-InstallerSupport.ps1')

$script:probeResponses = @{}
$script:fixtureGateway = 'http://127.0.0.1:18088'

function Assert-HaiHealthTest {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Get-HaiUrl { return $script:fixtureGateway }

function Invoke-WebRequest {
    param(
        [string]$Uri,
        [int]$TimeoutSec,
        [int]$MaximumRedirection,
        [switch]$UseBasicParsing,
        [string]$Method,
        [hashtable]$Headers,
        [string]$ErrorAction
    )

    $path = ([Uri]$Uri).AbsolutePath
    if (-not $script:probeResponses.ContainsKey($path)) { throw "Unexpected probe path: $path" }
    $statusCode = [int]$script:probeResponses[$path]
    $contentType = 'text/html'
    if ($path -eq '/api/v1/openclaw-maintenance') { $contentType = 'application/json' }
    $responseHeaders = @{ 'Content-Type' = $contentType }
    if ($statusCode -ge 300 -and $statusCode -lt 400) { $responseHeaders.Location = '/login' }
    if ($statusCode -ge 400) {
        $response = [pscustomobject]@{ StatusCode = $statusCode; Headers = $responseHeaders }
        $exception = [InvalidOperationException]::new("Fixture HTTP $statusCode")
        $exception | Add-Member -MemberType NoteProperty -Name Response -Value $response
        throw $exception
    }
    return [pscustomobject]@{ StatusCode = $statusCode; Headers = $responseHeaders; Content = '' }
}

function Set-HaiHealthyFixture {
    $script:probeResponses = @{
        '/readyz' = 200
        '/_hai/idp-readyz' = 200
        '/login' = 200
        '/control-center' = 200
        '/api/v1/openclaw-maintenance' = 401
        '/api/v1/user/' = 404
    }
}

Set-HaiHealthyFixture
$report = Get-HaiStackHealthReport
Assert-HaiHealthTest $report.Ready 'Healthy backend, identity provider, and frontend probes must produce a ready core runtime.'
Assert-HaiHealthTest ($report.Backend.State -eq 'healthy') 'Backend readiness HTTP 200 must be healthy.'
Assert-HaiHealthTest ($report.FrontendLogin.State -eq 'healthy' -and $report.FrontendDashboard.State -eq 'healthy') 'Frontend shell health must be reported independently from API health.'
Assert-HaiHealthTest ($report.ProtectedApiRoute.State -eq 'sign_in_required') 'An unauthenticated protected API response must show that the auth gate is reachable and sign-in is required.'
Assert-HaiHealthTest ($report.ProtectedApiRoute.Uri -eq "$script:fixtureGateway/api/v1/openclaw-maintenance") 'The protected API probe must target a backend-owned authenticated route, not the IDP-owned /api/v1/user/ route.'
Assert-HaiHealthTest ($report.AuthenticatedApi.State -eq 'not_verified') 'A no-session status check must never claim authenticated API readiness.'

$script:probeResponses['/readyz'] = 503
$report = Get-HaiStackHealthReport
Assert-HaiHealthTest (-not $report.Ready -and $report.Component -eq 'backend') 'Backend HTTP 503 must make runtime readiness unhealthy even when frontend shells return 200.'
Assert-HaiHealthTest ($report.FrontendDashboard.State -eq 'healthy') 'Frontend health must remain visible when the API backend is unhealthy.'

Set-HaiHealthyFixture
$script:probeResponses['/api/v1/openclaw-maintenance'] = 503
$report = Get-HaiStackHealthReport
Assert-HaiHealthTest (-not $report.Ready -and $report.Component -eq 'Protected API route') 'Protected API HTTP 503 must not be reported as a healthy runtime.'
Assert-HaiHealthTest ($report.AuthenticatedApi.State -eq 'unhealthy') 'Protected API failure must be reflected in authenticated API readiness.'

function Get-HaiEnvironmentFile { return (Join-Path $env:TEMP 'HAI-health-test-missing\hai.env') }
$guidance = Get-HaiMissingEnvironmentGuidance
Assert-HaiHealthTest ($guidance.Summary -match 'protected local environment file is missing') 'Missing environment state must be named clearly.'
Assert-HaiHealthTest ($guidance.Command -match '-RestoreEnvironmentOnly' -and $guidance.Command -match 'version-3-backup-folder') 'Recovery must direct users to version-3 environment-only restore.'
Assert-HaiHealthTest ($guidance.Recovery -match 'same Windows user/profile' -and $guidance.Recovery -match 'replacement credentials' -and $guidance.Recovery -match 'remove volumes') 'Missing-environment guidance must preserve DPAPI scope and the existing-volume credential guard.'
Assert-HaiHealthTest ($guidance.Command -notmatch 'docker|compose|volume|remove|delete') 'Environment-only recovery guidance must not start Docker or remove data.'

$statusScript = Join-Path $repositoryRoot 'installer\windows\HAI-Status.ps1'
$savedLocalAppData = $env:LOCALAPPDATA
$missingProfile = Join-Path ([IO.Path]::GetTempPath()) ('hai-status-missing-env-' + [Guid]::NewGuid().ToString('N'))
$global:haiStatusDockerFixture = [pscustomobject]@{ Mode = 'empty'; Calls = 0 }
function docker {
    $global:haiStatusDockerFixture.Calls++
    $commandLine = $args -join ' '
    if ($global:haiStatusDockerFixture.Mode -eq 'unavailable') {
        $global:LASTEXITCODE = 23
        return
    }
    $global:LASTEXITCODE = 0
    if ($commandLine -match '^volume ls --quiet --filter' -and $global:haiStatusDockerFixture.Mode -eq 'existing-data') {
        Write-Output '018-hai-postgres-data'
    }
}
try {
    $env:LOCALAPPDATA = $missingProfile
    $statusOutput = @(& $statusScript 6>&1 2>&1 | ForEach-Object { $_.ToString() }) -join "`n"
    Assert-HaiHealthTest ($global:haiStatusDockerFixture.Calls -eq 2) 'Status must use only the two read-only volume listings for a missing environment.'
    Assert-HaiHealthTest ($statusOutput.Contains('first-run candidate') -and $statusOutput.Contains('read-only inventory found no HAI data volumes')) 'An empty known-volume inventory should identify first-run as a candidate, not a certainty.'
    Assert-HaiHealthTest ($statusOutput.Contains('Only read-only Docker volume inventory was attempted') -and $statusOutput.Contains('no Docker services or containers were started')) 'Status must state that diagnosis is read-only and does not start Docker.'

    $global:haiStatusDockerFixture.Mode = 'existing-data'
    $global:haiStatusDockerFixture.Calls = 0
    $recoveryOutput = @(& $statusScript 6>&1 2>&1 | ForEach-Object { $_.ToString() }) -join "`n"
    Assert-HaiHealthTest ($global:haiStatusDockerFixture.Calls -eq 2) 'Recovery diagnosis must remain limited to read-only volume listings.'
    Assert-HaiHealthTest ($recoveryOutput.Contains('recovery required') -and $recoveryOutput.Contains('Existing HAI data volumes were detected')) 'Detected HAI volumes must direct the user to recovery, not first-run initialization.'
    Assert-HaiHealthTest ($recoveryOutput.Contains('-RestoreEnvironmentOnly') -and $recoveryOutput.Contains('replacement credentials')) 'Recovery diagnosis must retain the protected environment-only restore instructions.'

    $global:haiStatusDockerFixture.Mode = 'unavailable'
    $global:haiStatusDockerFixture.Calls = 0
    $unknownOutput = @(& $statusScript 6>&1 2>&1 | ForEach-Object { $_.ToString() }) -join "`n"
    Assert-HaiHealthTest ($global:haiStatusDockerFixture.Calls -eq 1) 'A failed first inventory query must stop further Docker inspection.'
    Assert-HaiHealthTest ($unknownOutput.Contains('diagnosis: unknown') -and $unknownOutput.Contains('Treat this as recovery, not first run')) 'An incomplete inventory must fail closed instead of recommending initialization.'
} finally {
    if ($null -eq $savedLocalAppData) { Remove-Item Env:LOCALAPPDATA -ErrorAction SilentlyContinue }
    else { $env:LOCALAPPDATA = $savedLocalAppData }
    Remove-Variable -Name haiStatusDockerFixture -Scope Global -ErrorAction SilentlyContinue
}
Assert-HaiHealthTest (-not (Test-Path -LiteralPath $missingProfile)) 'HAI status must not create profile data when hai.env is missing.'
Assert-HaiHealthTest ($statusOutput.Contains('protected local environment file is missing') -and $statusOutput.Contains('-RestoreEnvironmentOnly')) "The actual status command must present safe missing-environment recovery guidance. Output='$statusOutput'"

$statusSource = [IO.File]::ReadAllText((Join-Path $repositoryRoot 'installer\windows\HAI-Status.ps1'))
$missingCheckIndex = $statusSource.IndexOf('Test-HaiEnvironmentFileAvailable', [StringComparison]::Ordinal)
$dockerCheckIndex = $statusSource.IndexOf('Assert-HaiDockerReady', [StringComparison]::Ordinal)
Assert-HaiHealthTest ($missingCheckIndex -ge 0 -and $dockerCheckIndex -gt $missingCheckIndex) 'HAI status must handle a missing environment before invoking Docker.'
Assert-HaiHealthTest ($statusSource -match 'Get-HaiMissingEnvironmentGuidance' -and $statusSource -match 'Assert-HaiExistingDataRequiresEnvironment' -and $statusSource -match 'read-only Docker volume inventory') 'HAI status must classify missing-environment state using only the existing read-only volume guard.'

Write-Output 'Windows runtime health checks passed (frontend/API states and read-only first-run, recovery-required, and unknown environment diagnoses).'
