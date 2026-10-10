[CmdletBinding()]
param(
    [ValidateSet('Prepare', 'Validate', 'Start', 'Inspect', 'Stop')][string]$Action = 'Prepare',
    [string]$EvidenceDirectory,
    [ValidateRange(1025, 65535)][int]$Port = 18080,
    [ValidateSet('paused', 'manual-local')][string]$ExecutionMode = 'paused',
    [ValidatePattern('^10\.254\.(?:[1-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-4])\.0/24$')][string]$Subnet,
    [ValidatePattern('^10\.254\.(?:[1-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-4])\.0/24$')][string]$IngressSubnet
)

$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$core = @('idp', 'backend', 'backend-migrate', 'backend-runtime-role', 'frontend', 'nginx', 'postgres-idp', 'postgres-automation', 'redis')

function Invoke-Docker([string[]]$Arguments, [string]$LogPath) {
    if ($LogPath) {
        & docker @Arguments 2>&1 | Tee-Object -FilePath $LogPath | ForEach-Object { Write-Output $_ }
        if ($LASTEXITCODE -ne 0) { throw "Docker operation failed with exit code $LASTEXITCODE. See $LogPath." }
        return
    }
    $lines = & docker @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker operation failed with exit code $LASTEXITCODE. Evidence is retained." }
    return $lines
}

function Write-JsonFile($Value, [string]$Path) {
    [IO.File]::WriteAllText($Path, ($Value | ConvertTo-Json -Depth 100), [Text.UTF8Encoding]::new($false))
}

function Invoke-ExclusiveAcceptanceStart([scriptblock]$Start) {
    # Hold the host-wide start guard through readiness, including failed starts.
    $mutex = [Threading.Mutex]::new($false, 'HAI_ACCEPTANCE_START_V1')
    $acquired = $false
    try {
        try { $acquired = $mutex.WaitOne(0) }
        catch [Threading.AbandonedMutexException] { $acquired = $true }
        if (-not $acquired) { throw 'Another acceptance startup is in progress. Inspect it instead of starting another stack.' }
        $existing = @(Invoke-Docker -Arguments @('ps', '-aq', '--filter', 'label=hai.acceptance.owner'))
        if (@($existing | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) }).Count) {
            throw 'Acceptance containers already exist. Inspect and explicitly clean up that run before starting another stack.'
        }
        & $Start
    } finally {
        if ($acquired) { $mutex.ReleaseMutex() }
        $mutex.Dispose()
    }
}

function Get-PropertyNames($Value) {
    if ($Value -is [Collections.IDictionary]) { return @($Value.Keys) }
    return @($Value.PSObject.Properties.Name)
}

function Get-AcceptanceResourceLimits([string]$Service) {
    $limits = @{
        idp = @(256, 0.5); backend = @(384, 0.75); 'backend-migrate' = @(256, 0.5)
        'backend-runtime-role' = @(128, 0.25); frontend = @(128, 0.25); nginx = @(64, 0.25)
        'postgres-idp' = @(256, 0.5); 'postgres-automation' = @(256, 0.5); redis = @(64, 0.25)
    }
    if (-not $limits.ContainsKey($Service)) { throw 'Unknown acceptance service resource budget.' }
    return @{ memory = [long]$limits[$Service][0] * 1MB; cpus = $limits[$Service][1]; pids = 128 }
}

function Assert-AcceptanceResourceLimits($Service, [string]$Name) {
    $limit = Get-AcceptanceResourceLimits $Name
    foreach ($property in @('deploy', 'cpu_quota', 'cpu_period', 'cpu_count', 'cpu_percent', 'mem_reservation')) {
        if ($Service.PSObject.Properties[$property]) { throw "Conflicting acceptance resource setting: $Name/$property" }
    }
    if ($null -eq $Service.mem_limit -or $null -eq $Service.memswap_limit -or
        $null -eq $Service.cpus -or $null -eq $Service.pids_limit -or
        $Service.mem_limit -ne $limit.memory -or $Service.memswap_limit -ne $limit.memory -or
        $Service.cpus -ne $limit.cpus -or $Service.pids_limit -ne $limit.pids) {
        throw "Acceptance resource limits changed or missing: $Name"
    }
}

function Assert-AcceptanceManifest($Manifest) {
    if ($Manifest.version -ne 1 -or $Manifest.project -notmatch '^hai-acceptance-[0-9a-f]{12}$' -or
        $Manifest.owner -notmatch '^[0-9a-f]{32}$' -or $Manifest.email -ne 'e2e-owner@example.test' -or
        $Manifest.port -lt 1025 -or $Manifest.port -gt 65535) { throw 'Invalid acceptance manifest.' }
    if ($Manifest.project -cne ('hai-acceptance-' + $Manifest.owner.Substring(0, 12))) { throw 'Acceptance project and owner differ.' }
    if ($Manifest.executionMode -and $Manifest.executionMode -notin @('paused', 'manual-local')) { throw 'Invalid acceptance execution mode.' }
}

function Assert-IsolatedConfiguration($Config, $Manifest) {
    Assert-AcceptanceManifest $Manifest
    $manualLocal = $Manifest.executionMode -eq 'manual-local'
    $names = @($Config.services.PSObject.Properties.Name)
    if ((($names | Sort-Object) -join ',') -ne (($core | Sort-Object) -join ',')) { throw 'Unexpected acceptance service set.' }
    if ($Config.PSObject.Properties['volumes']) { throw 'Persistent volumes are forbidden in acceptance.' }
    foreach ($property in @('secrets', 'configs', 'include', 'extends')) {
        if ($Config.PSObject.Properties[$property]) { throw "External configuration is forbidden in acceptance: $property" }
    }
    if (((@(Get-PropertyNames $Config.networks) | Sort-Object) -join ',') -ne 'ingress,isolated') { throw 'Exactly the isolated and gateway ingress networks are required.' }
    foreach ($entry in $Config.services.PSObject.Properties) {
        $s = $entry.Value
        Assert-AcceptanceResourceLimits $s $entry.Name
        if ($null -ne $s.depends_on) {
            foreach ($dependency in @(Get-PropertyNames $s.depends_on)) {
                if (-not [string]::IsNullOrWhiteSpace([string]$dependency) -and $dependency -notin $names) {
                    throw "Acceptance service dependency is missing: $($entry.Name)/$dependency"
                }
            }
        }
        foreach ($property in @('env_file', 'secrets', 'configs', 'extends')) {
            if ($s.PSObject.Properties[$property]) { throw "External configuration is forbidden in acceptance: $($entry.Name)/$property" }
        }
        if ($s.container_name -ne "$($Manifest.project)-$($entry.Name)" -or $s.restart -ne 'no' -or
            $s.labels.'hai.acceptance.owner' -ne $Manifest.owner) { throw 'Container ownership contract changed.' }
        if ($s.privileged -or $s.network_mode -or $s.pid -eq 'host' -or $s.ipc -eq 'host' -or
            $s.devices -or $s.cap_add -or $s.extra_hosts -or $s.volumes_from) { throw 'Host access is forbidden in acceptance.' }
        $expectedNetworks = if ($entry.Name -eq 'nginx') { 'ingress,isolated' } else { 'isolated' }
        if (((@(Get-PropertyNames $s.networks) | Sort-Object) -join ',') -ne $expectedNetworks) { throw 'Only the gateway may attach to ingress; every service requires isolated attachment.' }
        if ($entry.Name -match '^postgres-' -or $entry.Name -eq 'backend-runtime-role') {
            if (-not @($s.tmpfs | Where-Object { $_ -like '/var/lib/postgresql/data:rw,*' }).Count) {
                throw 'Image-declared PostgreSQL storage must be covered by tmpfs.'
            }
        }
        foreach ($mount in @($s.volumes)) {
            if ($null -eq $mount) { continue }
            $prefix = [IO.Path]::GetFullPath($EvidenceDirectory).TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
            if ($mount.type -ne 'bind' -or -not $mount.read_only -or
                -not [IO.Path]::GetFullPath($mount.source).StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) {
                throw 'Acceptance mounts must be read-only files inside its evidence directory.'
            }
        }
        foreach ($published in @($s.ports)) {
            if ($null -eq $published) { continue }
            if ($entry.Name -ne 'nginx' -or $published.host_ip -ne '127.0.0.1' -or
                [int]$published.published -ne $Manifest.port -or [int]$published.target -ne 80) {
                throw 'Only the declared loopback gateway port may be published.'
            }
        }
        foreach ($setting in $s.environment.PSObject.Properties) {
            $expected = if ($manualLocal -and $entry.Name -eq 'backend' -and $setting.Name -eq 'SOURCE_MANUAL_WORKER_ENABLED') { 'true' } else { 'false' }
            if ($setting.Name -match '_ENABLED$' -and [string]$setting.Value -ne $expected) {
                throw "Acceptance feature must be disabled: $($setting.Name)"
            }
        }
    }
    foreach ($network in $Config.networks.PSObject.Properties) {
        $mustBeInternal = $network.Name -eq 'isolated'
        if ([bool]$network.Value.internal -ne $mustBeInternal -or $network.Value.external -or $network.Value.name -ne "$($Manifest.project)-$($network.Name)" -or
            $network.Value.labels.'hai.acceptance.owner' -ne $Manifest.owner) { throw 'Unowned or external network.' }
        $expectedSubnet = if ($mustBeInternal) { $Manifest.subnet } else { $Manifest.ingressSubnet }
        if ($expectedSubnet) {
            if ($expectedSubnet -notmatch '^10\.254\.(?:[1-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-4])\.0/24$' -or
                @($network.Value.ipam.config).Count -ne 1 -or $network.Value.ipam.config[0].subnet -ne $expectedSubnet) {
                throw 'Acceptance subnet does not match the explicit manifest.'
            }
        } elseif ($network.Value.ipam) { throw 'Unexpected acceptance subnet configuration.' }
    }
    $required = @{
        idp = @{ RUN_MODE = 'production'; DB_SSLMODE = 'disable'; HAI_LOCAL_COMPOSE_DATABASE = 'true'; FIRST_RUN_ADMIN_EMAIL = $Manifest.email; FIRST_RUN_ADMIN_PASSWORD = $Manifest.password; DB_HOST = 'postgres-idp'; GOOGLE_OAUTH_CLIENT_ID = ''; GOOGLE_OAUTH_CLIENT_SECRET = ''; SMTP_HOST = ''; SMTP_USERNAME = ''; SMTP_PASSWORD = ''; LOCAL_LOGIN_BYPASS_ENABLED = 'false' }
        backend = @{ RUN_MODE = 'production'; DB_SSLMODE = 'disable'; HAI_LOCAL_COMPOSE_DATABASE = 'true'; DB_HOST = 'postgres-automation'; LLM_PROVIDERS_JSON = '[]'; HAI_PHASE2_MODE = 'paused'; HAI_PHASE2_FEED_FILES = ''; SOURCE_SCHEDULER_ENABLED = 'false'; WORKFLOW_SCHEDULER_ENABLED = 'false'; WORKFLOW_OPEN_LOOP_SCHEDULER_ENABLED = 'false'; AMBIENT_SCHEDULER_ENABLED = 'false'; OUTCOME_MONITOR_SCHEDULER_ENABLED = 'false'; LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED = 'false'; HAI_CATALOG_REVALIDATION_SCHEDULER_ENABLED = 'false' }
        'backend-migrate' = @{ RUN_MODE = 'production'; DB_SSLMODE = 'disable'; HAI_LOCAL_COMPOSE_DATABASE = 'true'; DB_HOST = 'postgres-automation' }
        'backend-runtime-role' = @{ PGHOST = 'postgres-automation' }
    }
    $required.backend.SOURCE_MANUAL_WORKER_ENABLED = if ($manualLocal) { 'true' } else { 'false' }
    $required.backend.SOURCE_WORKER_POLL_SECONDS = '15'
    if ($manualLocal) { $required.backend.HAI_PHASE2_MODE = 'autonomous_safe' }
    foreach ($name in $required.Keys) {
        foreach ($key in $required[$name].Keys) {
            $setting = $Config.services.$name.environment.PSObject.Properties[$key]
            if ($null -eq $setting -or [string]$setting.Value -cne [string]$required[$name][$key]) {
                throw "Required synthetic setting changed: $name/$key"
            }
        }
    }
    foreach ($pair in @(@('postgres-idp', 'idp'), @('postgres-automation', 'backend-migrate'))) {
        if ($Config.services.($pair[0]).environment.POSTGRES_PASSWORD -cne $Config.services.($pair[1]).environment.DB_PASSWORD) {
            throw 'Synthetic database credentials do not match.'
        }
    }
    if ($Config.services.'backend-runtime-role'.environment.HAI_RUNTIME_DB_PASSWORD -cne $Config.services.backend.environment.DB_PASSWORD -or
        $Config.services.'backend-runtime-role'.environment.PGPASSWORD -cne $Config.services.'postgres-automation'.environment.POSTGRES_PASSWORD) {
        throw 'Synthetic runtime role credentials do not match.'
    }
}

if ($Action -eq 'Prepare') {
    if ([bool]$Subnet -ne [bool]$IngressSubnet -or ($Subnet -and $Subnet -eq $IngressSubnet)) { throw 'Explicit internal and ingress subnets must both be supplied and differ.' }
    if ($EvidenceDirectory) { throw 'Prepare always creates a fresh, uniquely named evidence directory.' }
    $owner = [Guid]::NewGuid().ToString('N')
    $EvidenceDirectory = Join-Path ([IO.Path]::GetTempPath()) "hai-acceptance-$owner"
    New-Item -ItemType Directory -Path $EvidenceDirectory | Out-Null
    Write-Output "Preparing evidence: $EvidenceDirectory"
    $project = 'hai-acceptance-' + $owner.Substring(0, 12)
    $cleanupManifestPath = Join-Path $EvidenceDirectory 'cleanup-manifest.json'
    $cleanupManifest = [pscustomobject]@{
        version = 1
        kind = 'hai-acceptance-synthetic-fixture'
        state = 'preparing'
        owner = $owner
        project = $project
        createdUtc = [DateTime]::UtcNow.ToString('o')
        syntheticEnvBytes = 0
        syntheticEnvSha256 = ''
    }
    Write-JsonFile $cleanupManifest $cleanupManifestPath
    $values = [ordered]@{}
    foreach ($line in Get-Content -LiteralPath (Join-Path $repo '.env.example')) {
        if ($line -match '^([A-Z][A-Z0-9_]*)=(.*)$') {
            if ($values.Contains($Matches[1])) { throw "Duplicate example key: $($Matches[1])" }
            $values[$Matches[1]] = $Matches[2]
        }
    }
    foreach ($key in @($values.Keys)) {
        if ($key -match '_ENABLED$') { $values[$key] = 'false' }
    }
    foreach ($key in @($values.Keys)) {
        if ($key -match '(?:^|_)(?:PASSWORD|PASS|TOKEN|SECRET|API_KEY|CLIENT_ID|PRIVATE_KEY|SIGNING_KEY|WORKSPACE_KEY|ENCRYPTION_KEY|ACCESS_KEY|SHARED_KEY|CREDENTIALS?)$') {
            $values[$key] = ''
        }
    }
    foreach ($key in @('DB_PASSWORD', 'BACKEND_DB_PASSWORD', 'BACKEND_API_SHARED_KEY', 'HAI_MEMORY_ENCRYPTION_KEY', 'JWT_SECRET', 'HAI_APPROVAL_PROOF_SIGNING_KEY')) {
        $values[$key] = [Guid]::NewGuid().ToString('N') + [Guid]::NewGuid().ToString('N')
    }
    $password = 'E2eOnly-' + [Guid]::NewGuid().ToString('N')
    $overrides = @{
        COMPOSE_PROJECT_NAME = $project; GATEWAY_HOST_BIND = '127.0.0.1'; GATEWAY_HOST_PORT = [string]$Port
        RUN_MODE = 'production'; FIRST_RUN_ADMIN_EMAIL = 'e2e-owner@example.test'; FIRST_RUN_ADMIN_PASSWORD = $password
        BACKEND_DB_USER = 'hai_runtime'; DB_MIGRATIONS_ENABLED = 'false'; IDP_COOKIE_SECURE = 'false'
        LOCAL_LOGIN_BYPASS_ENABLED = 'false'; HAI_PHASE2_MODE = 'paused'; HAI_PHASE2_FEED_FILES = ''
        LLM_PROVIDERS_JSON = '[]'; GOOGLE_OAUTH_CLIENT_ID = ''; GOOGLE_OAUTH_CLIENT_SECRET = ''
        SMTP_HOST = ''; SMTP_USERNAME = ''; SMTP_PASSWORD = ''; GITHUB_SOURCE_TOKEN = ''
        TRELLO_API_KEY = ''; TRELLO_API_SECRET = ''; TRELLO_READ_TOKEN = ''
        AUTOMATION_API_ALLOWED_HOSTS = 'backend'; AUTOMATION_HEALTH_ALLOWED_HOSTS = 'backend'
    }
    foreach ($key in $overrides.Keys) { $values[$key] = $overrides[$key] }
    $values['SOURCE_MANUAL_WORKER_ENABLED'] = if ($ExecutionMode -eq 'manual-local') { 'true' } else { 'false' }
    $values['SOURCE_WORKER_POLL_SECONDS'] = '15'
    if ($ExecutionMode -eq 'manual-local') { $values['HAI_PHASE2_MODE'] = 'autonomous_safe' }
    $envFile = Join-Path $EvidenceDirectory 'synthetic.env'
    [IO.File]::WriteAllLines($envFile, @($values.Keys | ForEach-Object { "$_=$($values[$_])" }), [Text.UTF8Encoding]::new($false))
    $cleanupManifest.syntheticEnvBytes = (Get-Item -LiteralPath $envFile).Length
    $cleanupManifest.syntheticEnvSha256 = (Get-FileHash -LiteralPath $envFile -Algorithm SHA256).Hash.ToLowerInvariant()
    Write-JsonFile $cleanupManifest $cleanupManifestPath
    # Parent process variables must not override the deliberately synthetic file.
    $saved = @{}
    $references = [regex]::Matches([IO.File]::ReadAllText((Join-Path $repo 'docker-compose.local.yml')), '\$\{([A-Z][A-Z0-9_]*)')
    foreach ($name in @($references | ForEach-Object { $_.Groups[1].Value } | Select-Object -Unique) + @('COMPOSE_PROFILES', 'COMPOSE_FILE', 'COMPOSE_PROJECT_NAME')) {
        $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
        Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue
    }
    try {
        $config = ((Invoke-Docker -Arguments @('compose', '--env-file', $envFile, '-p', $project, '-f', (Join-Path $repo 'docker-compose.local.yml'), 'config', '--format', 'json')) -join "`n") | ConvertFrom-Json
    } finally {
        foreach ($name in $saved.Keys) {
            if ($null -eq $saved[$name]) { Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue }
            else { Set-Item -LiteralPath "Env:$name" -Value $saved[$name] }
        }
    }
    Copy-Item -LiteralPath (Join-Path $repo 'nginx-config/nginx.conf.template') -Destination (Join-Path $EvidenceDirectory 'nginx.conf.template')
    Copy-Item -LiteralPath (Join-Path $repo 'services/postgres-runtime-role/provision-runtime-role.sh') -Destination (Join-Path $EvidenceDirectory 'provision-runtime-role.sh')
    Copy-Item -LiteralPath (Join-Path $repo 'init.sql') -Destination (Join-Path $EvidenceDirectory 'init.sql')
    New-Item -ItemType Directory -Path (Join-Path $EvidenceDirectory 'sources'), (Join-Path $EvidenceDirectory 'sites-enabled') | Out-Null
    [IO.File]::WriteAllText((Join-Path $EvidenceDirectory 'sources/acceptance.txt'), 'Synthetic HAI acceptance source. No personal records.', [Text.UTF8Encoding]::new($false))
    $services = [ordered]@{}
    foreach ($name in $core) {
        $s = $config.services.$name
        $s | Add-Member -Force NoteProperty container_name "$project-$name"
        $s | Add-Member -Force NoteProperty restart 'no'
        $s | Add-Member -Force NoteProperty labels @{ 'hai.acceptance.owner' = $owner }
        $s | Add-Member -Force NoteProperty networks @{ 'isolated' = @{ aliases = @($name) } }
        foreach ($property in @('ports', 'volumes', 'profiles', 'extra_hosts', 'deploy', 'cpu_quota', 'cpu_period', 'cpu_count', 'cpu_percent', 'mem_reservation')) { $s.PSObject.Properties.Remove($property) }
        $resourceLimit = Get-AcceptanceResourceLimits $name
        $s | Add-Member -Force NoteProperty mem_limit $resourceLimit.memory
        $s | Add-Member -Force NoteProperty memswap_limit $resourceLimit.memory
        $s | Add-Member -Force NoteProperty cpus $resourceLimit.cpus
        $s | Add-Member -Force NoteProperty pids_limit $resourceLimit.pids
        if ($s.PSObject.Properties['build']) {
            $suffix = if ($name -eq 'backend-migrate') { 'backend' } else { $name }
            $s | Add-Member -Force NoteProperty image "$project-$suffix"
        }
        $mounts = @()
        if ($name -eq 'backend') {
            $mounts = @(@{ type = 'bind'; source = (Join-Path $EvidenceDirectory 'sources'); target = '/root/connected-sources'; read_only = $true })
            $s.tmpfs = @('/tmp:rw,noexec,nosuid,size=128m', '/root/images:rw,noexec,nosuid,size=16m', '/root/agent-workspaces:rw,noexec,nosuid,size=32m', '/root/phase2-control-state:rw,noexec,nosuid,size=8m,uid=10001,gid=10001', '/root/phase2-feeds:rw,noexec,nosuid,size=8m')
            # Production repairs ownership of its persistent state volume with
            # a privileged one-shot service. Acceptance uses private tmpfs,
            # so assign the runtime UID directly and omit that volume helper.
            $s.depends_on.PSObject.Properties.Remove('backend-state-permissions')
        }
        if ($name -match '^postgres-') {
            $s.image = 'postgres:17-alpine'
            $s | Add-Member -Force NoteProperty tmpfs @('/var/lib/postgresql/data:rw,nosuid,size=512m')
            $mounts = @(@{ type = 'bind'; source = (Join-Path $EvidenceDirectory 'init.sql'); target = '/docker-entrypoint-initdb.d/init.sql'; read_only = $true })
        }
        if ($name -eq 'redis') { $s | Add-Member -Force NoteProperty tmpfs @('/data:rw,nosuid,size=32m,mode=1777') }
        if ($name -eq 'backend-runtime-role') {
            $s.tmpfs += '/var/lib/postgresql/data:rw,nosuid,size=8m'
            $mounts = @(@{ type = 'bind'; source = (Join-Path $EvidenceDirectory 'provision-runtime-role.sh'); target = '/scripts/provision-runtime-role.sh'; read_only = $true })
        }
        if ($name -eq 'nginx') {
            $s.networks.ingress = @{}
            $s | Add-Member -Force NoteProperty ports @(@{ target = 80; published = [string]$Port; host_ip = '127.0.0.1'; protocol = 'tcp' })
            $mounts = @(@{ type = 'bind'; source = (Join-Path $EvidenceDirectory 'nginx.conf.template'); target = '/etc/nginx/nginx.conf.template'; read_only = $true }, @{ type = 'bind'; source = (Join-Path $EvidenceDirectory 'sites-enabled'); target = '/etc/nginx/sites-enabled'; read_only = $true })
        }
        if ($mounts.Count) { $s | Add-Member -Force NoteProperty volumes $mounts }
        $services[$name] = $s
    }
    $config = [pscustomobject]@{
        name = $project; services = [pscustomobject]$services
        networks = [pscustomobject]@{
            isolated = @{ name = "$project-isolated"; internal = $true; labels = @{ 'hai.acceptance.owner' = $owner } }
            ingress = @{ name = "$project-ingress"; internal = $false; labels = @{ 'hai.acceptance.owner' = $owner } }
        }
    }
    if ($Subnet) {
        $config.networks.isolated.ipam = @{ config = @(@{ subnet = $Subnet }) }
        $config.networks.ingress.ipam = @{ config = @(@{ subnet = $IngressSubnet }) }
    }
    $manifest = [pscustomobject]@{ version = 1; project = $project; owner = $owner; port = $Port; subnet = $Subnet; ingressSubnet = $IngressSubnet; executionMode = $ExecutionMode; email = 'e2e-owner@example.test'; password = $password; createdUtc = [DateTime]::UtcNow.ToString('o') }
    Assert-IsolatedConfiguration $config $manifest
    Write-JsonFile $config (Join-Path $EvidenceDirectory 'compose.json')
    Write-JsonFile $manifest (Join-Path $EvidenceDirectory 'manifest.json')
    Remove-Item -LiteralPath $cleanupManifestPath -Force -ErrorAction Stop
    Write-Output "Prepared: $EvidenceDirectory"
    Write-Output 'Only synthetic credentials are retained in manifest.json; do not publish that file.'
    return
}

if (-not $EvidenceDirectory) { throw 'An explicitly prepared evidence directory is required.' }
$EvidenceDirectory = [IO.Path]::GetFullPath($EvidenceDirectory)
$manifest = Get-Content -LiteralPath (Join-Path $EvidenceDirectory 'manifest.json') -Raw | ConvertFrom-Json
$composeFile = Join-Path $EvidenceDirectory 'compose.json'
Assert-AcceptanceManifest $manifest
# Cleanup validates actual resource ownership and mounts, not obsolete startup
# configuration. This also permits cleanup after a failed configuration repair.
if ($Action -ne 'Stop') {
    $config = Get-Content -LiteralPath $composeFile -Raw | ConvertFrom-Json
    Assert-IsolatedConfiguration $config $manifest
}
if ($Action -eq 'Validate') { Write-Output 'Acceptance configuration validated; no containers started.'; return }
$composeArgs = @('compose', '-p', $manifest.project, '-f', $composeFile)
if ($Action -eq 'Start') {
    Invoke-ExclusiveAcceptanceStart {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $manifest.port)
        try { $listener.Start() } finally { $listener.Stop() }
        Invoke-Docker -Arguments ($composeArgs + @('up', '-d', '--build', '--wait', '--wait-timeout', '300')) -LogPath (Join-Path $EvidenceDirectory 'start.log')
        $page = Invoke-WebRequest -Uri "http://127.0.0.1:$($manifest.port)/login" -TimeoutSec 10
        if ($page.StatusCode -ne 200 -or $page.Content -notmatch '<app-root') { throw 'The declared loopback gateway did not serve the HAI application.' }
        Write-Output "Acceptance URL: http://127.0.0.1:$($manifest.port)"
    }
    return
}
if ($Action -eq 'Inspect') {
    Invoke-Docker -Arguments ($composeArgs + @('ps', '-a'))
    return
}

$ids = @(Invoke-Docker -Arguments @('ps', '-aq', '--filter', "label=hai.acceptance.owner=$($manifest.owner)"))
foreach ($id in $ids) {
    $container = ((Invoke-Docker -Arguments @('inspect', $id)) -join "`n") | ConvertFrom-Json
    if ($container.Config.Labels.'com.docker.compose.project' -ne $manifest.project -or
        $container.Config.Labels.'hai.acceptance.owner' -ne $manifest.owner -or
        $container.Name -notmatch ('^/' + [regex]::Escape($manifest.project) + '-')) { throw 'Container cleanup ownership mismatch.' }
    foreach ($mount in @($container.Mounts)) {
        if ($mount.Type -eq 'volume') { throw 'Unexpected persistent volume; refusing cleanup.' }
    }
}
foreach ($id in $ids) {
    Invoke-Docker -Arguments @('stop', $id)
    Invoke-Docker -Arguments @('rm', $id)
}
$networkIds = @(Invoke-Docker -Arguments @('network', 'ls', '-q', '--filter', "label=hai.acceptance.owner=$($manifest.owner)"))
foreach ($id in $networkIds) {
    $network = ((Invoke-Docker -Arguments @('network', 'inspect', $id)) -join "`n") | ConvertFrom-Json
    if ($network.Name -notin @("$($manifest.project)-isolated", "$($manifest.project)-ingress") -or $network.Labels.'hai.acceptance.owner' -ne $manifest.owner -or
        @($network.Containers.PSObject.Properties).Count -ne 0) { throw 'Network cleanup ownership mismatch or attached containers.' }
    Invoke-Docker -Arguments @('network', 'rm', $id)
}
Write-Output 'Owned acceptance containers and network removed. No files, evidence, images, caches, or volumes deleted.'
