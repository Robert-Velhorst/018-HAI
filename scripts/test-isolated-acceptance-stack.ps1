[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$launcher = Join-Path $PSScriptRoot 'isolated-acceptance-stack.ps1'
$savedServer = [Environment]::GetEnvironmentVariable('SERVER_PORT', 'Process')
$savedGoogle = [Environment]::GetEnvironmentVariable('GOOGLE_OAUTH_CLIENT_ID', 'Process')
$savedProfiles = [Environment]::GetEnvironmentVariable('COMPOSE_PROFILES', 'Process')
try {
    $env:SERVER_PORT = '29999'
    $env:GOOGLE_OAUTH_CLIENT_ID = 'synthetic-inherited-poison'
    $env:COMPOSE_PROFILES = 'cloud-tunnel,local-host-runtime'
    $prepared = & $launcher -Action Prepare -Port 58144
    $prepared | Write-Output
    $line = @($prepared | Where-Object { $_ -like 'Prepared: *' })
    if ($line.Count -ne 1) { throw 'Prepare did not return its unique evidence directory.' }
    $preparedDirectory = $line[0].Substring('Prepared: '.Length)
    if ($env:SERVER_PORT -ne '29999' -or $env:GOOGLE_OAUTH_CLIENT_ID -ne 'synthetic-inherited-poison' -or
        $env:COMPOSE_PROFILES -ne 'cloud-tunnel,local-host-runtime') { throw 'Prepare changed the caller environment.' }
} finally {
    foreach ($entry in @(@('SERVER_PORT', $savedServer), @('GOOGLE_OAUTH_CLIENT_ID', $savedGoogle), @('COMPOSE_PROFILES', $savedProfiles))) {
        if ($null -eq $entry[1]) { Remove-Item -LiteralPath "Env:$($entry[0])" -ErrorAction SilentlyContinue }
        else { Set-Item -LiteralPath "Env:$($entry[0])" -Value $entry[1] }
    }
}

# Load the real guard without creating or deleting any runtime resources.
. $launcher -Action Validate -EvidenceDirectory $preparedDirectory
if ($config.services.backend.environment.SERVER_PORT -ne '80' -or
    $config.services.idp.environment.GOOGLE_OAUTH_CLIENT_ID -ne '' -or
    $config.services.idp.environment.FIRST_RUN_ADMIN_EMAIL -ne 'e2e-owner@example.test') {
    throw 'Inherited configuration leaked into the synthetic stack.'
}
$baseline = $config | ConvertTo-Json -Depth 100
$externalConfigurationCases = @(
    @{ name = 'service-env-file'; edit = { param($c) $c.services.backend | Add-Member NoteProperty env_file @('/external/private.env') }; error = 'External configuration' },
    @{ name = 'service-secret'; edit = { param($c) $c.services.idp | Add-Member NoteProperty secrets @('foreign') }; error = 'External configuration' },
    @{ name = 'service-config'; edit = { param($c) $c.services.nginx | Add-Member NoteProperty configs @('foreign') }; error = 'External configuration' },
    @{ name = 'service-extends'; edit = { param($c) $c.services.backend | Add-Member NoteProperty extends @{ file = '/external/compose.yml'; service = 'backend' } }; error = 'External configuration' },
    @{ name = 'top-level-secret'; edit = { param($c) $c | Add-Member NoteProperty secrets @{ foreign = @{ file = '/external/private.key' } } }; error = 'External configuration' },
    @{ name = 'top-level-config'; edit = { param($c) $c | Add-Member NoteProperty configs @{ foreign = @{ file = '/external/private.json' } } }; error = 'External configuration' },
    @{ name = 'top-level-include'; edit = { param($c) $c | Add-Member NoteProperty include @('/external/compose.yml') }; error = 'External configuration' },
    @{ name = 'empty-env-file'; edit = { param($c) $c.services.backend | Add-Member NoteProperty env_file @() }; error = 'External configuration' },
    @{ name = 'empty-secrets'; edit = { param($c) $c | Add-Member NoteProperty secrets @{} }; error = 'External configuration' }
)
function Assert-ExternalConfigurationStartRefused($Candidate, $Case, [string]$Directory) {
    $path = Join-Path $Directory 'compose.json'
    $original = [IO.File]::ReadAllText($path)
    $script:externalStartDockerCalls = 0
    function docker { $script:externalStartDockerCalls++; throw 'Docker must not run for external configuration.' }
    try {
        Write-JsonFile $Candidate $path
        $rejected = $false
        try { & $launcher -Action Start -EvidenceDirectory $Directory | Out-Null }
        catch { if ($_.Exception.Message -notmatch $Case.error) { throw }; $rejected = $true }
        if (-not $rejected -or $script:externalStartDockerCalls -ne 0) {
            throw "External configuration reached Docker startup: $($Case.name)"
        }
    } finally {
        [IO.File]::WriteAllText($path, $original, [Text.UTF8Encoding]::new($false))
        Remove-Item -LiteralPath Function:docker
    }
}
$cases = @(
    @{ name = 'unbounded-memory'; edit = { param($c) $c.services.backend.PSObject.Properties.Remove('mem_limit') }; error = 'resource limits changed or missing' },
    @{ name = 'unbounded-cpu'; edit = { param($c) $c.services.backend.cpus = 0 }; error = 'resource limits changed or missing' },
    @{ name = 'additional-swap'; edit = { param($c) $c.services.backend.memswap_limit = 805306368 }; error = 'resource limits changed or missing' },
    @{ name = 'conflicting-deployment-limits'; edit = { param($c) $c.services.backend | Add-Member NoteProperty deploy @{} }; error = 'Conflicting acceptance resource setting' },
    @{ name = 'persistent-volume'; edit = { param($c) $c | Add-Member NoteProperty volumes @{ bad = @{} } }; error = 'Persistent volumes' },
    @{ name = 'foreign-container'; edit = { param($c) $c.services.backend.container_name = '018-hai-backend' }; error = 'ownership contract' },
    @{ name = 'host-port'; edit = { param($c) $c.services.nginx.ports[0].host_ip = '0.0.0.0' }; error = 'loopback gateway' },
    @{ name = 'writable-host-mount'; edit = { param($c) $c.services.backend.volumes[0].read_only = $false }; error = 'read-only files' },
    @{ name = 'foreign-host-mount'; edit = { param($c) $c.services.backend.volumes[0].source = [IO.Path]::GetTempPath() }; error = 'read-only files' },
    @{ name = 'privileged'; edit = { param($c) $c.services.backend | Add-Member NoteProperty privileged $true }; error = 'Host access' },
    @{ name = 'host-process-namespace'; edit = { param($c) $c.services.backend | Add-Member NoteProperty pid 'host' }; error = 'Host access' },
    @{ name = 'enabled-provider'; edit = { param($c) $c.services.backend.environment.HAI_RAGFLOW_ENABLED = 'true' }; error = 'must be disabled' },
    @{ name = 'implicit-manual-worker'; edit = { param($c) $c.services.backend.environment.SOURCE_MANUAL_WORKER_ENABLED = 'true' }; error = 'must be disabled' },
    @{ name = 'external-network'; edit = { param($c) $c.networks.isolated.internal = $false }; error = 'external network' },
    @{ name = 'external-network-reference'; edit = { param($c) $c.networks.isolated | Add-Member NoteProperty external $true }; error = 'external network' },
    @{ name = 'missing-networks'; edit = { param($c) $c.PSObject.Properties.Remove('networks') }; error = 'Exactly the isolated' },
    @{ name = 'missing-network-attachment'; edit = { param($c) $c.services.backend.PSObject.Properties.Remove('networks') }; error = 'isolated attachment' },
    @{ name = 'foreign-network-attachment'; edit = { param($c) $c.services.backend.networks | Add-Member NoteProperty foreign @{} }; error = 'isolated attachment' },
    @{ name = 'backend-ingress-attachment'; edit = { param($c) $c.services.backend.networks | Add-Member NoteProperty ingress @{} }; error = 'Only the gateway' },
    @{ name = 'gateway-missing-isolation'; edit = { param($c) $c.services.nginx.networks.PSObject.Properties.Remove('isolated') }; error = 'isolated attachment' },
    @{ name = 'anonymous-role-volume'; edit = { param($c) $c.services.'backend-runtime-role'.tmpfs = @('/tmp:rw,size=64m') }; error = 'storage must be covered' },
    @{ name = 'missing-scheduler-disable'; edit = { param($c) $c.services.backend.environment.PSObject.Properties.Remove('AMBIENT_SCHEDULER_ENABLED') }; error = 'Required synthetic setting' },
    @{ name = 'missing-manual-worker-setting'; edit = { param($c) $c.services.backend.environment.PSObject.Properties.Remove('SOURCE_MANUAL_WORKER_ENABLED') }; error = 'Required synthetic setting' },
    @{ name = 'populated-providers'; edit = { param($c) $c.services.backend.environment.LLM_PROVIDERS_JSON = '[{"provider":"unexpected"}]' }; error = 'Required synthetic setting' },
    @{ name = 'foreign-bootstrap-email'; edit = { param($c) $c.services.idp.environment.FIRST_RUN_ADMIN_EMAIL = 'other@example.test' }; error = 'Required synthetic setting' },
    @{ name = 'foreign-database-host'; edit = { param($c) $c.services.backend.environment.DB_HOST = 'personal-database' }; error = 'Required synthetic setting' },
    @{ name = 'foreign-idp-database-host'; edit = { param($c) $c.services.idp.environment.DB_HOST = 'personal-database' }; error = 'Required synthetic setting' },
    @{ name = 'foreign-migration-database-host'; edit = { param($c) $c.services.'backend-migrate'.environment.DB_HOST = 'personal-database' }; error = 'Required synthetic setting' },
    @{ name = 'missing-local-database-marker'; edit = { param($c) $c.services.backend.environment.HAI_LOCAL_COMPOSE_DATABASE = 'false' }; error = 'Required synthetic setting' },
    @{ name = 'tls-mode-does-not-match-local-compose'; edit = { param($c) $c.services.idp.environment.DB_SSLMODE = 'verify-full' }; error = 'Required synthetic setting' },
    @{ name = 'mismatched-database-credentials'; edit = { param($c) $c.services.'postgres-idp'.environment.POSTGRES_PASSWORD = 'synthetic-but-wrong' }; error = 'credentials do not match' },
    @{ name = 'mismatched-role-credentials'; edit = { param($c) $c.services.'backend-runtime-role'.environment.HAI_RUNTIME_DB_PASSWORD = 'synthetic-but-wrong' }; error = 'role credentials do not match' },
    @{ name = 'extra-service'; edit = { param($c) $c.services | Add-Member NoteProperty unwanted @{} }; error = 'service set' }
) + $externalConfigurationCases
foreach ($case in $cases) {
    $candidate = $baseline | ConvertFrom-Json
    & $case.edit $candidate
    $rejected = $false
    try { Assert-IsolatedConfiguration $candidate $manifest }
    catch {
        if ($_.Exception.Message -notmatch $case.error) { throw }
        $rejected = $true
    }
    if (-not $rejected) { throw "Unsafe configuration accepted: $($case.name)" }
    if ($case.error -eq 'External configuration') { Assert-ExternalConfigurationStartRefused $candidate $case $preparedDirectory }
    Write-Output "PASS: $($case.name)"
}
$pausedManifest = $manifest
$pausedConfig = $config
$pausedEvidenceDirectory = $EvidenceDirectory
try {
    $preparedManual = & $launcher -Action Prepare -Port 58145 -ExecutionMode manual-local
    $manualLine = @($preparedManual | Where-Object { $_ -like 'Prepared: *' })
    if ($manualLine.Count -ne 1) { throw 'Manual mode preparation did not return a unique evidence directory.' }
    . $launcher -Action Validate -EvidenceDirectory $manualLine[0].Substring('Prepared: '.Length)
    if ($manifest.executionMode -ne 'manual-local' -or
        $config.services.backend.environment.SOURCE_MANUAL_WORKER_ENABLED -ne 'true' -or
        $config.services.backend.environment.HAI_PHASE2_MODE -ne 'autonomous_safe' -or
        $config.services.backend.environment.SOURCE_SCHEDULER_ENABLED -ne 'false') { throw 'Manual-only execution configuration is incomplete.' }
    $manualBaseline = $config | ConvertTo-Json -Depth 100
    foreach ($case in $externalConfigurationCases) {
        $candidate = $manualBaseline | ConvertFrom-Json
        & $case.edit $candidate
        $rejected = $false
        try { Assert-IsolatedConfiguration $candidate $manifest }
        catch { if ($_.Exception.Message -notmatch $case.error) { throw }; $rejected = $true }
        if (-not $rejected) { throw "Unsafe manual configuration accepted: $($case.name)" }
        Assert-ExternalConfigurationStartRefused $candidate $case $EvidenceDirectory
        Write-Output "PASS: manual-local/$($case.name)"
    }
    $config.services.backend.environment.SOURCE_SCHEDULER_ENABLED = 'true'
    $refused = $false
    try { Assert-IsolatedConfiguration $config $manifest }
    catch { if ($_.Exception.Message -notmatch 'must be disabled') { throw }; $refused = $true }
    if (-not $refused) { throw 'Manual mode enabled an automatic scheduler.' }
    Write-Output 'PASS: explicit manual mode permits only the manual worker, never automatic scheduling'
} finally {
    $manifest = $pausedManifest
    $config = $pausedConfig
    $EvidenceDirectory = $pausedEvidenceDirectory
}
function docker {
    $script:dockerCalls += ,@($args)
    $global:LASTEXITCODE = 0
    if ($args[0] -eq 'ps') { return ('a' * 64) }
    if ($args[0] -eq 'inspect') {
        return (@(@{ Config = @{ Labels = @{ 'com.docker.compose.project' = 'foreign-project'; 'hai.acceptance.owner' = $manifest.owner } }; Name = "/$($manifest.project)-backend"; Mounts = @() }) | ConvertTo-Json -Depth 8 -Compress)
    }
    throw 'Unexpected Docker call in ownership negative control.'
}
$script:dockerCalls = @()
try {
    $refused = $false
    try { & $launcher -Action Stop -EvidenceDirectory $preparedDirectory }
    catch { if ($_.Exception.Message -notmatch 'cleanup ownership mismatch') { throw }; $refused = $true }
    if (-not $refused -or @($script:dockerCalls | Where-Object { $_[0] -in @('stop', 'rm', 'kill') -or ($_[0] -eq 'network' -and $_[1] -eq 'rm') }).Count) {
        throw 'Wrong-owner cleanup did not fail before mutation.'
    }
    Write-Output 'PASS: wrong-owner cleanup refuses all destructive calls'
} finally { Remove-Item -LiteralPath Function:docker }

$savedServer = [Environment]::GetEnvironmentVariable('SERVER_PORT', 'Process')
function docker { throw 'Synthetic Docker configuration failure.' }
try {
    $env:SERVER_PORT = 'failure-restoration-sentinel'
    $failed = $false
    try { & $launcher -Action Prepare -Port 58144 | Out-Null }
    catch { if ($_.Exception.Message -notmatch 'Synthetic Docker configuration failure') { throw }; $failed = $true }
    if (-not $failed -or $env:SERVER_PORT -ne 'failure-restoration-sentinel') { throw 'Prepare failure did not restore the caller environment.' }
    Write-Output 'PASS: configuration failure restores caller environment'
} finally {
    Remove-Item -LiteralPath Function:docker
    if ($null -eq $savedServer) { Remove-Item -LiteralPath Env:SERVER_PORT -ErrorAction SilentlyContinue }
    else { Set-Item -LiteralPath Env:SERVER_PORT -Value $savedServer }
}
Write-Output 'PASS: synthetic environment isolation, caller restoration, and baseline validation'
Write-Output "Evidence retained: $preparedDirectory"
