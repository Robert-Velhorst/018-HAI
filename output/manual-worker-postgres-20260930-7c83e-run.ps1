$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$owner = 'manual-worker-pg-20260930-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$pgName = 'hai-' + $owner + '-pg'
$goName = 'hai-' + $owner + '-go'
$labelKey = 'com.hai.manual-worker-postgres.owner'
$logDir = Join-Path $PSScriptRoot $owner
New-Item -ItemType Directory -Path $logDir | Out-Null
$created = @()
$utf8 = New-Object System.Text.UTF8Encoding($false)
$pgImage = 'sha256:d74eeac9a635390a49bc21bd49fccd973de707e2a53a76ac49b552b8712ec46f'
$goImage = 'sha256:14e75143c833c7398ea3a5e4c673aeaae35f40e781e6b060e2f97b72c475c975'
$dsn = 'host=127.0.0.1 user=postgres dbname=hai_source_manual_worker_test port=5432 sslmode=disable TimeZone=UTC'

function Invoke-DockerEvidence([string] $file, [string[]] $arguments, [switch] $AllowFailure) {
    $lines = @(& docker @arguments 2>&1)
    $code = $LASTEXITCODE
    [IO.File]::WriteAllLines((Join-Path $logDir $file), [string[]]$lines, $utf8)
    [IO.File]::WriteAllText((Join-Path $logDir "$file.exit"), "$code", $utf8)
    if ($code -ne 0 -and -not $AllowFailure) { throw "Docker failed ($code): $file" }
    return [pscustomobject]@{ Lines = $lines; ExitCode = $code }
}

function Inspect-Owned([string] $name, [string] $suffix) {
    $raw = Invoke-DockerEvidence "$suffix-inspect.json" @('inspect', $name)
    $items = @(($raw.Lines -join "`n") | ConvertFrom-Json)
    $item = $items[0]
    if ($items.Count -ne 1 -or $item.Name -ne "/$name" -or $item.Config.Labels.$labelKey -ne $owner) {
        throw "Ownership mismatch: $name"
    }
    if (@($item.HostConfig.PortBindings.PSObject.Properties).Count -ne 0) { throw 'Unexpected published ports' }
    if (@($item.Mounts | Where-Object { $_.Type -eq 'volume' }).Count -ne 0) { throw 'No volume mounts permitted' }
    if ($name -eq $pgName) {
        if ($item.HostConfig.NetworkMode -ne 'none' -or $item.Config.Image -ne $pgImage -or
            $item.HostConfig.Tmpfs.'/var/lib/postgresql/data' -ne 'rw,size=2g') { throw 'Postgres isolation mismatch' }
        if (@($item.Mounts | Where-Object { $_.Type -eq 'bind' }).Count -ne 0) { throw 'Postgres bind mount forbidden' }
    } else {
        $pg = ((Invoke-DockerEvidence "$suffix-pg-identity.json" @('inspect', $pgName)).Lines -join "`n" | ConvertFrom-Json)[0]
        if ($item.HostConfig.NetworkMode -notin @("container:$pgName", "container:$($pg.Id)") -or
            $item.Config.Image -ne $goImage -or -not $item.HostConfig.ReadonlyRootfs) { throw 'Go isolation mismatch' }
        $binds = @($item.Mounts | Where-Object { $_.Type -eq 'bind' })
        if ($binds.Count -ne 3) { throw 'Go must mount only backend source and two read-only cache directories' }
        foreach ($mount in $binds) {
            if ($mount.RW) { throw 'Writable host/cache mount forbidden' }
            switch ($mount.Destination) {
                '/src' { if ($mount.Source -notmatch 'github-plugin-github-openai-curated-noodzakelijk[/\\]backend$') { throw 'Wrong source mount' } }
                '/cache/mod' { if ($mount.Source -ne $modCache) { throw 'Wrong module cache' } }
                '/cache/build' { if ($mount.Source -ne $buildCache) { throw 'Wrong build cache' } }
                default { throw 'Unexpected bind destination' }
            }
        }
    }
    return $item
}

function Save-SourceHashes([string] $file) {
    $files = @(Get-ChildItem -LiteralPath (Join-Path $root 'backend/internal/source') -File -Filter '*.go') +
        @(Get-ChildItem -LiteralPath (Join-Path $root 'backend/internal/durablejob') -File -Filter '*.go') +
        @(Get-ChildItem -LiteralPath (Join-Path $root 'backend/internal/pgtestguard') -File -Filter '*.go') +
        @(Get-Item -LiteralPath (Join-Path $root 'backend/go.mod'), (Join-Path $root 'backend/go.sum'))
    $hashes = @($files | Sort-Object FullName | ForEach-Object {
        @{ path = $_.FullName.Substring($root.Length + 1); sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLower() }
    })
    [IO.File]::WriteAllText((Join-Path $logDir $file), ($hashes | ConvertTo-Json -Depth 4), $utf8)
}

try {
    $caches = ((Invoke-DockerEvidence 'cache-directory-identities.json' @('volume', 'inspect', 'hai-go-modcache', 'hai-go-buildcache')).Lines -join "`n" | ConvertFrom-Json)
    $modCache = ($caches | Where-Object Name -eq 'hai-go-modcache').Mountpoint
    $buildCache = ($caches | Where-Object Name -eq 'hai-go-buildcache').Mountpoint
    if (-not $modCache -or -not $buildCache) { throw 'Existing cache directories unavailable; do not create volumes' }
    Invoke-DockerEvidence 'images.json' @('image', 'inspect', $pgImage, $goImage) | Out-Null
    Invoke-DockerEvidence 'postgres-create.log' @('create', '--pull', 'never', '--name', $pgName,
        '--label', "$labelKey=$owner", '--network', 'none', '--memory', '3g', '--cpus', '2', '--shm-size', '128m',
        '--tmpfs', '/var/lib/postgresql/data:rw,size=2g', '--tmpfs', '/var/run/postgresql:rw,size=16m',
        '-e', 'POSTGRES_DB=hai_source_manual_worker_test', '-e', 'POSTGRES_HOST_AUTH_METHOD=trust',
        $pgImage, '-c', 'listen_addresses=127.0.0.1', '-c', 'max_connections=40') | Out-Null
    $created += $pgName
    Inspect-Owned $pgName 'postgres-created' | Out-Null
    Invoke-DockerEvidence 'postgres-start.log' @('start', $pgName) | Out-Null
    $ready = $false
    for ($i = 0; $i -lt 60; $i++) {
        $probe = Invoke-DockerEvidence "postgres-ready-$i.log" @('exec', $pgName, 'pg_isready', '-h', '127.0.0.1', '-U', 'postgres', '-d', 'hai_source_manual_worker_test') -AllowFailure
        if ($probe.ExitCode -eq 0) { $ready = $true; break }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) { throw 'Owned Postgres did not become ready' }
    Invoke-DockerEvidence 'postgres-version.log' @('exec', $pgName, 'psql', '-U', 'postgres', '-d', 'hai_source_manual_worker_test', '-c', 'SELECT version(), current_database();') | Out-Null
    Invoke-DockerEvidence 'go-create.log' @('create', '--pull', 'never', '--name', $goName, '--label', "$labelKey=$owner",
        '--network', "container:$pgName", '--read-only', '--memory', '5g', '--cpus', '2', '--cap-drop', 'ALL',
        '--security-opt', 'no-new-privileges', '--tmpfs', '/tmp:rw,exec,size=3g',
        '--mount', "type=bind,source=$(Join-Path $root 'backend'),target=/src,readonly",
        '--mount', "type=bind,source=$modCache,target=/cache/mod,readonly",
        '--mount', "type=bind,source=$buildCache,target=/cache/build,readonly",
        '--workdir', '/src', '-e', 'GOTOOLCHAIN=local', '-e', 'GOPROXY=off', '-e', 'GOSUMDB=off', '-e', 'GOMAXPROCS=2',
        '-e', 'GOMODCACHE=/cache/mod', '-e', 'GOCACHE=/tmp/go-build', '-e', 'HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true',
        '-e', "HAI_SOURCE_MANUAL_WORKER_POSTGRES_TEST_DSN=$dsn", $goImage, 'sh', '-c', 'sleep 7200') | Out-Null
    $created += $goName
    Inspect-Owned $goName 'go-created' | Out-Null
    Invoke-DockerEvidence 'go-start.log' @('start', $goName) | Out-Null
    $prepare = @'
set -eu
go version
test "$(go env GOVERSION)" = go1.25.13
go env GOMODCACHE GOCACHE GOPROXY GOTOOLCHAIN
mkdir -p /tmp/go-build
size=$(du -sb /cache/build | cut -f1)
echo "read-only existing build cache bytes=$size"
if [ "$size" -lt 1073741824 ]; then cp -a /cache/build/. /tmp/go-build/; else echo 'Build cache too large; using fresh tmpfs cache'; fi
'@
    Invoke-DockerEvidence 'go-prepare.log' @('exec', $goName, 'sh', '-c', $prepare) | Out-Null
    # Formatting is restricted to the newly owned test file; source mount stays read-only.
    $formatted = Invoke-DockerEvidence 'gofmt-output.log' @('exec', $goName, 'gofmt', '/src/internal/source/manual_worker_postgres_test.go')
    [IO.File]::WriteAllText((Join-Path $root 'backend/internal/source/manual_worker_postgres_test.go'), ($formatted.Lines -join "`n") + "`n", $utf8)
    Save-SourceHashes 'source-hashes-before.json'
    foreach ($negative in @(
        @{ Name = 'wrong-database'; DSN = $dsn.Replace('hai_source_manual_worker_test', 'hai_source_manual_worker_test_shadow'); OptIn = 'true' },
        @{ Name = 'hostname'; DSN = $dsn.Replace('127.0.0.1', 'localhost'); OptIn = 'true' },
        @{ Name = 'nonliteral-opt-in'; DSN = $dsn; OptIn = ' TRUE ' }
    )) {
        $red = Invoke-DockerEvidence "guard-red-$($negative.Name).log" @('exec', '-e', "HAI_SOURCE_MANUAL_WORKER_POSTGRES_TEST_DSN=$($negative.DSN)",
            '-e', "HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=$($negative.OptIn)", $goName,
            'go', 'test', '-json', '-race', '-count=1', '-p=2', '-timeout=12m', '-run', '^TestManualWorkerPostgresClaimIsolation$', './internal/source') -AllowFailure
        if ($red.ExitCode -eq 0 -or ($red.Lines -join "`n") -notmatch 'refusing.*PostgreSQL') { throw 'Expected guard rejection missing' }
    }
    Invoke-DockerEvidence 'guard-schemas-after-red.log' @('exec', $pgName, 'psql', '-U', 'postgres', '-d', 'hai_source_manual_worker_test', '-c', "SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'manual_worker_%';") | Out-Null
    $positive = Invoke-DockerEvidence 'go-tests-positive.log' @('exec', $goName, 'go', 'test', '-json', '-race', '-count=3', '-p=2', '-timeout=12m',
        '-run', '^(TestManualWorker|TestValidateDedicatedPostgresTestDSN|TestDestructiveTestOptIn)', './internal/source', './internal/pgtestguard') -AllowFailure
    if ($positive.ExitCode -ne 0) { throw 'Positive tests failed; evidence retained' }
    $events = @($positive.Lines | Where-Object { $_ -match '^\{' } | ForEach-Object { $_ | ConvertFrom-Json })
    $skips = @($events | Where-Object Action -eq 'skip')
    $fails = @($events | Where-Object Action -eq 'fail')
    if ($skips.Count -ne 0 -or $fails.Count -ne 0) { throw 'Positive proof must have zero skips/failures' }
    $required = @('ClaimIsolation', 'ConcurrentSingleOwner', 'SkipLocked', 'ReapIsolationAndAttemptLimit', 'DurableRetryLimit', 'ExecutionReload')
    $summary = @($required | ForEach-Object {
        $testName = "TestManualWorkerPostgres$_"
        $passes = @($events | Where-Object { $_.Test -eq $testName -and $_.Action -eq 'pass' }).Count
        if ($passes -ne 3) { throw "Missing three passes: $testName ($passes)" }
        @{ test = $testName; passes = $passes; skips = 0; failures = 0 }
    })
    [IO.File]::WriteAllText((Join-Path $logDir 'named-postgres-proof.json'), ($summary | ConvertTo-Json -Depth 4), $utf8)
    Save-SourceHashes 'source-hashes-after.json'
    Invoke-DockerEvidence 'schemas-after-positive.log' @('exec', $pgName, 'psql', '-U', 'postgres', '-d', 'hai_source_manual_worker_test', '-c', "SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'manual_worker_%';") | Out-Null
    Write-Output "Positive real PostgreSQL proof: six named tests x three passes; zero skips. Evidence: $logDir"
} finally {
    foreach ($name in @($goName, $pgName)) {
        if ($name -notin $created) { continue }
        Inspect-Owned $name "$name-precleanup" | Out-Null
        Invoke-DockerEvidence "$name-final.log" @('logs', $name) | Out-Null
        Invoke-DockerEvidence "$name-stop.log" @('stop', '-t', '10', $name) | Out-Null
        Inspect-Owned $name "$name-stopped" | Out-Null
        Invoke-DockerEvidence "$name-remove.log" @('rm', $name) | Out-Null
    }
    Invoke-DockerEvidence 'owned-containers-after.log' @('ps', '-a', '--filter', "label=$labelKey=$owner", '--format', '{{.ID}} {{.Names}}') | Out-Null
    Write-Output "Retained evidence: $logDir"
}
