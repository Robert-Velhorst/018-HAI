$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
. (Join-Path $PSScriptRoot 'windows-installer-payload-selection.ps1')

function Assert-HaiInstallerSelectionContains {
    param([string[]]$Actual, [string[]]$Expected, [string]$Context)

    foreach ($path in $Expected) {
        if ($path -notin $Actual) { throw "$Context is missing expected payload source: $path" }
    }
}

function Assert-HaiInstallerSelectionOmits {
    param([string[]]$Actual, [string[]]$Forbidden, [string]$Context)

    foreach ($path in $Forbidden) {
        if ($path -in $Actual) { throw "$Context unexpectedly selected excluded content: $path" }
    }
}

$fixtureSelection = @(Resolve-HaiInstallerSourceSelection `
    -TrackedFiles @(
        'README.md',
        '.env.example',
        '.env.local',
        'db_data_automation/ha.db',
        'docs/private.pdf',
        'installer/release/old.exe',
        'backend/cmd/hai-openclaw-maintenance/tracked.go'
    ) `
    -InstallerUntrackedFiles @(
        'installer/windows/Run-HAI-OpenClawMaintenance.ps1',
        'installer/windows/Hai-OpenClawMaintenance.ps1',
        'installer/windows/Manage-HAI-OpenClawMaintenanceTask.ps1',
        'installer/windows/local-notes.txt',
        'installer/windows/local-settings.json',
        'installer/windows/Extra-Unreviewed.ps1',
        'scripts/build-windows-installer.ps1',
        'docs/windows-installer.md',
        'unrelated/private-notes.txt',
        'installer/windows/.env.local',
        'installer/windows/runtime.sqlite',
        'installer/windows/certificate.pem',
        'installer/windows/local-state.db'
    ) `
    -WorkerUntrackedFiles @(
        'backend/cmd/hai-openclaw-maintenance/main.go',
        'backend/internal/openclawmaintenance/service.go',
        'backend/internal/openclawmaintenance/.env',
        'backend/internal/openclawmaintenance/state.db',
        'backend/internal/openclawmaintenance/certificate.pem',
        'backend/internal/openclawmaintenance/source.zip',
        'backend/internal/openclawmaintenance/trace.txt',
        'backend/internal/unrelated/unrelated.go'
    ))

Assert-HaiInstallerSelectionContains $fixtureSelection @(
    'README.md',
    '.env.example',
    'backend/cmd/hai-openclaw-maintenance/tracked.go',
    'installer/windows/Run-HAI-OpenClawMaintenance.ps1',
    'installer/windows/Hai-OpenClawMaintenance.ps1',
    'installer/windows/Manage-HAI-OpenClawMaintenanceTask.ps1',
    'scripts/build-windows-installer.ps1',
    'docs/windows-installer.md',
    'backend/cmd/hai-openclaw-maintenance/main.go',
    'backend/internal/openclawmaintenance/service.go'
) 'Fixture selection'
Assert-HaiInstallerSelectionOmits $fixtureSelection @(
    '.env.local',
    'db_data_automation/ha.db',
    'docs/private.pdf',
    'installer/release/old.exe',
    'unrelated/private-notes.txt',
    'installer/windows/.env.local',
    'installer/windows/runtime.sqlite',
    'installer/windows/certificate.pem',
    'installer/windows/local-state.db',
    'installer/windows/local-notes.txt',
    'installer/windows/local-settings.json',
    'installer/windows/Extra-Unreviewed.ps1',
    'backend/internal/openclawmaintenance/.env',
    'backend/internal/openclawmaintenance/state.db',
    'backend/internal/openclawmaintenance/certificate.pem',
    'backend/internal/openclawmaintenance/source.zip',
    'backend/internal/openclawmaintenance/trace.txt',
    'backend/internal/unrelated/unrelated.go'
) 'Fixture selection'

$sensitiveArtifacts = @(
    '.env', '.env.production', '.envrc', 'installer/windows/.env.local',
    'config/secrets.yml', 'config/credentials.json', 'config/service-account.json', 'keys/id_rsa', 'keys/id_ed25519',
    '.npmrc', '.pypirc', '.netrc', '.pgpass',
    'db_data_postgres/base/16384', 'database_data/postgres', 'backups/release.sql',
    'exports/customer.sql', 'exports/snapshot.dump', 'state/runtime.sqlite-wal',
    'state/runtime.sqlite-shm', 'state/runtime.db-wal', 'state/runtime.db-shm',
    'certificates/server.pem', 'certificates/server.key', 'certificates/server.pfx',
    'certificates/server.crt', 'certificates/server.cer', 'certificates/chain.der', 'infra/terraform.tfstate',
    '../outside.txt', 'nested/../../outside.txt'
)
$artifactSelection = @(Resolve-HaiInstallerSourceSelection -TrackedFiles $sensitiveArtifacts)
Assert-HaiInstallerSelectionOmits $artifactSelection $sensitiveArtifacts 'Sensitive artifact exclusion'
if (@(Resolve-HaiInstallerSourceSelection -TrackedFiles @('.env.example')).Count -ne 1) {
    throw 'The documented non-secret .env.example template must remain eligible for the installer payload.'
}

$fixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ('hai-source-provenance-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixtureRoot | Out-Null
try {
    $fixtureFile = Join-Path $fixtureRoot 'config\app.txt'
    New-Item -ItemType Directory -Path (Split-Path -Parent $fixtureFile) | Out-Null
    [IO.File]::WriteAllText($fixtureFile, 'installer provenance fixture')
    $safePath = Assert-HaiInstallerSourceFileSafe -RepositoryRoot $fixtureRoot -RelativePath 'config/app.txt'
    if ($safePath -ne [IO.Path]::GetFullPath($fixtureFile)) { throw 'Safe source resolution changed the selected file.' }

    $integrity = @(Get-HaiInstallerFileProvenance -PayloadRoot $fixtureRoot -RelativePaths @('config/app.txt'))
    $expectedHash = (Get-FileHash -LiteralPath $fixtureFile -Algorithm SHA256).Hash
    if ($integrity.Count -ne 1 -or $integrity[0].path -ne 'config/app.txt' -or
        $integrity[0].sha256 -ne $expectedHash -or $integrity[0].sizeBytes -ne ([IO.FileInfo]$fixtureFile).Length) {
        throw 'Payload provenance does not describe the exact source bytes and size.'
    }

    foreach ($unsafePath in @('../outside.txt', 'config/../../outside.txt', [IO.Path]::GetFullPath($fixtureFile))) {
        $rejected = $false
        try { Assert-HaiInstallerSourceFileSafe -RepositoryRoot $fixtureRoot -RelativePath $unsafePath | Out-Null } catch { $rejected = $true }
        if (-not $rejected) { throw "Unsafe source path was accepted: $unsafePath" }
    }

    $failure = Format-HaiInstallerCompilerFailure -ExitCode 7 -MaximumLines 2 -Output @('old diagnostic', 'error: missing file', 'compile stopped')
    if ($failure -notmatch 'exit code 7' -or $failure -match 'old diagnostic' -or
        $failure -notmatch 'error: missing file' -or $failure -notmatch 'compile stopped') {
        throw 'Compiler failure reporting did not retain only the bounded, useful diagnostic tail.'
    }
} finally {
    $resolvedFixtureRoot = [IO.Path]::GetFullPath($fixtureRoot)
    $resolvedTempParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if ($resolvedFixtureRoot.StartsWith($resolvedTempParent, [StringComparison]::OrdinalIgnoreCase) -and
        [IO.Path]::GetFileName($resolvedFixtureRoot) -match '\Ahai-source-provenance-[a-f0-9]{32}\z' -and
        (Test-Path -LiteralPath $resolvedFixtureRoot -PathType Container)) {
        Remove-Item -LiteralPath $resolvedFixtureRoot -Recurse -Force
    }
}

$repositorySelection = @(Get-HaiInstallerSourceFiles -RepositoryRoot $repositoryRoot)
$cachedWorkerGoFiles = @(& git -C $repositoryRoot ls-files --cached -- `
    backend/cmd/hai-openclaw-maintenance `
    backend/internal/openclawmaintenance)
if ($LASTEXITCODE -ne 0) { throw 'Could not inspect tracked OpenClaw maintenance source files.' }
$untrackedWorkerGoFiles = @(& git -C $repositoryRoot ls-files --others --exclude-standard -- `
    backend/cmd/hai-openclaw-maintenance `
    backend/internal/openclawmaintenance)
if ($LASTEXITCODE -ne 0) { throw 'Could not inspect untracked OpenClaw maintenance source files.' }
$workerGoFiles = @($cachedWorkerGoFiles + $untrackedWorkerGoFiles | Where-Object { [IO.Path]::GetExtension($_) -ieq '.go' })
Assert-HaiInstallerSelectionContains $repositorySelection $workerGoFiles 'Current Git selection'

foreach ($required in @(
    'backend/cmd/hai-openclaw-maintenance/main.go',
    'backend/internal/openclawmaintenance/service.go'
)) {
    if (-not (Test-Path -LiteralPath (Join-Path $repositoryRoot $required) -PathType Leaf)) {
        throw "The current checkout is missing required OpenClaw maintenance source: $required"
    }
    Assert-HaiInstallerSelectionContains $repositorySelection @($required) 'Current Git selection'
}

foreach ($required in @(
    '.env.example',
    'docker-compose.local.yml',
    'installer/windows/HAI.iss',
    'installer/windows/Hai-WindowsExecutable.ps1',
    'installer/windows/Hai-InstallerSupport.ps1',
    'installer/windows/Manage-HAI-OpenClawMaintenanceTask.ps1',
    'installer/windows/Start-HAI.ps1',
    'installer/windows/Stop-HAI.ps1',
    'scripts/initialize-windows.ps1',
    'backend/cmd/hai-dsh-bridge/main.go'
)) {
    Assert-HaiInstallerSelectionContains $repositorySelection @($required) 'Required first-run installer payload'
}

Write-Host 'Windows installer source-selection tests passed without modifying the payload.'
