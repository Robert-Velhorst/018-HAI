[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$library = Join-Path $PSScriptRoot "windows-recovery-contract.ps1"
if (-not (Test-Path -LiteralPath $library -PathType Leaf)) {
    throw "Windows recovery contract library is missing."
}
. $library
$backupScript = Join-Path $PSScriptRoot "backup-windows.ps1"
. $backupScript -LibraryOnly

function Assert-Throws([string]$Name, [scriptblock]$Action, [string]$Pattern) {
    try {
        & $Action
        throw "$Name unexpectedly passed."
    } catch {
        if ($_.Exception.Message -eq "$Name unexpectedly passed." -or
            $_.Exception.Message -notmatch $Pattern) {
            throw
        }
    }
}

function New-ManifestFile([string]$Path) {
    $item = Get-Item -LiteralPath $Path
    return [pscustomobject]@{
        name = $item.Name
        bytes = $item.Length
        sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
    }
}

function Set-HaiRecoveryContractDockerHost([string]$DockerHost) {
    $global:HaiRecoveryContractMock.DockerHost = $DockerHost
}

$testRoot = Join-Path ([IO.Path]::GetTempPath()) ("hai-recovery-contract-" + [Guid]::NewGuid().ToString("N"))
$sourceFixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ("hai-env-source-fixture-" + [Guid]::NewGuid().ToString("N"))
$environmentFixture = Join-Path $sourceFixtureRoot 'source.env'
$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$imagesFixture = Join-Path $repoRoot 'images'
$createdImagesFixture = $false
$priorMock = Get-Variable -Name HaiRecoveryContractMock -Scope Global -ErrorAction SilentlyContinue
[IO.Directory]::CreateDirectory($testRoot) | Out-Null
[IO.Directory]::CreateDirectory($sourceFixtureRoot) | Out-Null
try {
    foreach ($name in @("automation.dump", "identity.dump", "media.zip", "phase2-control-state.tar.gz")) {
        [IO.File]::WriteAllText((Join-Path $testRoot $name), "fixture-$name", [Text.UTF8Encoding]::new($false))
    }
    $optionalStore = Join-Path $testRoot 'agent-workspaces\.hai-openclaw-ecosystem'
    $supportedHaiVolumes = @(
        '018-hai-postgres-automation-data',
        '018-hai-postgres-idp-data',
        '018-hai-phase2-control-state'
    )
    $coverage = Assert-HaiOptionalRecoveryAssetsAbsent $supportedHaiVolumes $optionalStore
    Assert-HaiExtendedRecoveryCoverage $coverage
    if ($coverage.temporal.state -cne 'absent' -or $coverage.openClawManagedArchives.state -cne 'absent' -or
        @($coverage.haiVolumes | Where-Object state -ne 'present').Count -ne 0) {
        throw 'Empty optional recovery fixtures were not recorded as absent.'
    }
    Assert-Throws 'existing Temporal persistence' {
        Assert-HaiOptionalRecoveryAssetsAbsent ($supportedHaiVolumes + '018-hai-temporal-postgres-data') $optionalStore
    } 'Temporal persistence volume exists.*no complete backup'
    Assert-Throws 'uncovered HAI persistent volume' {
        Assert-HaiOptionalRecoveryAssetsAbsent ($supportedHaiVolumes + '018-hai-ollama-local-data') $optionalStore
    } 'HAI persistent volume.*018-hai-ollama-local-data.*No complete backup'
    Assert-Throws 'anonymous HAI volume mount' {
        Assert-HaiOptionalRecoveryAssetsAbsent $supportedHaiVolumes $optionalStore @([pscustomobject]@{ volume = 'synthetic-anonymous-volume'; container = 'fixture-hai-container'; destination = '/data' })
    } 'Docker volume.*synthetic-anonymous-volume.*No complete backup'
    Assert-Throws 'unlisted external HAI volume mount' {
        Assert-HaiOptionalRecoveryAssetsAbsent $supportedHaiVolumes $optionalStore @([pscustomobject]@{ volume = 'shared-state-volume'; container = 'fixture-hai-container'; destination = '/state'; anonymous = $false })
    } 'Docker volume.*shared-state-volume.*No complete backup'
    [IO.Directory]::CreateDirectory($optionalStore) | Out-Null
    $openClawArchiveFixture = Join-Path $optionalStore 'archive-fixture.zip'
    [IO.File]::WriteAllText($openClawArchiveFixture, 'synthetic archive payload', [Text.UTF8Encoding]::new($false))
    Assert-Throws 'persisted OpenClaw archive state' {
        Assert-HaiOptionalRecoveryAssetsAbsent $supportedHaiVolumes $optionalStore
    } 'OpenClaw managed archive selection/rollback data exists.*no complete backup'
    Remove-Item -LiteralPath $openClawArchiveFixture -Force
    $coverage = Assert-HaiOptionalRecoveryAssetsAbsent $supportedHaiVolumes $optionalStore
    Assert-HaiExtendedRecoveryCoverage $coverage
    if ($coverage.openClawManagedArchives.state -cne 'empty') { throw 'Empty OpenClaw store was not recorded as empty.' }
    $ambiguousTemporalCoverage = $coverage | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $ambiguousTemporalCoverage.temporal.state = 'present'
    Assert-Throws 'ambiguous Temporal coverage' { Assert-HaiExtendedRecoveryCoverage $ambiguousTemporalCoverage } 'unsupported or ambiguous Temporal'
    $ambiguousOpenClawCoverage = $coverage | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $ambiguousOpenClawCoverage.openClawManagedArchives.state = 'present'
    Assert-Throws 'ambiguous OpenClaw coverage' { Assert-HaiExtendedRecoveryCoverage $ambiguousOpenClawCoverage } 'unsupported or ambiguous OpenClaw'
    $ambiguousHaiVolumeCoverage = $coverage | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $ambiguousHaiVolumeCoverage.haiVolumes[0].artifact = 'identity.dump'
    Assert-Throws 'ambiguous HAI volume coverage' { Assert-HaiExtendedRecoveryCoverage $ambiguousHaiVolumeCoverage } 'unsupported or ambiguous HAI persistent-volume'
    $missingSafetyVolumeCoverage = $coverage | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $missingSafetyVolumeCoverage.haiVolumes = @($missingSafetyVolumeCoverage.haiVolumes | Where-Object volume -ne '018-hai-phase2-control-state')
    Assert-Throws 'missing safety volume coverage' { Assert-HaiExtendedRecoveryCoverage $missingSafetyVolumeCoverage } 'incomplete or ambiguous HAI persistent-volume coverage'
    $duplicateHaiVolumeCoverage = $coverage | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $duplicateHaiVolumeCoverage.haiVolumes += $duplicateHaiVolumeCoverage.haiVolumes[0]
    Assert-Throws 'duplicate HAI volume coverage' { Assert-HaiExtendedRecoveryCoverage $duplicateHaiVolumeCoverage } 'incomplete or ambiguous HAI persistent-volume coverage'
    $legacyCoverage = [pscustomobject]@{
        contract = 'hai-extended-recovery.v1'
        temporal = [pscustomobject]@{ state = 'absent'; volume = '018-hai-temporal-postgres-data' }
        openClawManagedArchives = [pscustomobject]@{ state = 'absent'; path = 'agent-workspaces/.hai-openclaw-ecosystem' }
    }
    Assert-HaiExtendedRecoveryCoverage $legacyCoverage
    Assert-Throws 'missing extended coverage' { Assert-HaiExtendedRecoveryCoverage $null } 'lacks explicit optional recovery coverage'
    $oldManifestWithoutCoverage = [pscustomobject]@{ formatVersion = 3 }
    Assert-Throws 'old manifest without optional coverage' {
        Assert-HaiExtendedRecoveryCoverage $oldManifestWithoutCoverage.extendedRecoveryCoverage
    } 'lacks explicit optional recovery coverage'

    $validManifest = [pscustomobject]@{
        formatVersion = 2
        controlStateSource = "018-hai-phase2-control-state"
        databases = @('fixture_automation', 'fixture_identity')
        files = @(
            New-ManifestFile (Join-Path $testRoot "automation.dump")
            New-ManifestFile (Join-Path $testRoot "identity.dump")
            New-ManifestFile (Join-Path $testRoot "media.zip")
            New-ManifestFile (Join-Path $testRoot "phase2-control-state.tar.gz")
        )
    }
    Assert-HaiRecoveryManifest $validManifest $testRoot

    $versionOne = $validManifest | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    $versionOne.formatVersion = 1
    Assert-Throws "version-one manifest" { Assert-HaiRecoveryManifest $versionOne $testRoot } "Versions 2 and 3 are supported"

    $missingState = $validManifest | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    $missingState.files = @($missingState.files | Where-Object { $_.name -ne "phase2-control-state.tar.gz" })
    Assert-Throws "missing safety state" { Assert-HaiRecoveryManifest $missingState $testRoot } "exactly 4"

    $wrongSource = $validManifest | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    $wrongSource.controlStateSource = "other-volume"
    Assert-Throws "wrong safety source" { Assert-HaiRecoveryManifest $wrongSource $testRoot } "expected safety control-state volume"

    $badChecksum = $validManifest | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    $badChecksum.files[3].sha256 = "0" * 64
    Assert-Throws "bad safety checksum" { Assert-HaiRecoveryManifest $badChecksum $testRoot } "Checksum mismatch"

    [IO.File]::WriteAllText($environmentFixture, "DB_USER=fixture_user`nAUTOMATION_DB_NAME=fixture_automation`nIDP_DB_NAME=fixture_identity`nIMAGE_SAVE_DIR=/root/images`nFIXTURE_ONLY_SECRET=not-a-real-secret`n", [Text.UTF8Encoding]::new($false))
    $fixtureBackupId = [Guid]::NewGuid().ToString('N')
    $protectedMetadata = Protect-HaiEnvironmentFile $environmentFixture (Join-Path $testRoot 'environment.dpapi') $fixtureBackupId
    $v3Manifest = [pscustomobject]@{
        formatVersion = 3
        backupId = $fixtureBackupId
        controlStateSource = '018-hai-phase2-control-state'
        databases = @('fixture_automation', 'fixture_identity')
        protectedEnvironment = $protectedMetadata
        files = @('automation.dump', 'identity.dump', 'media.zip', 'phase2-control-state.tar.gz', 'environment.dpapi') | ForEach-Object { New-ManifestFile (Join-Path $testRoot $_) }
    }
    Assert-HaiRecoveryManifest $v3Manifest $testRoot
    $missingProtectedArtifact = $v3Manifest | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $missingProtectedArtifact.files = @($missingProtectedArtifact.files | Where-Object name -cne 'environment.dpapi')
    Assert-Throws 'missing protected environment artifact' { Assert-HaiRecoveryManifest $missingProtectedArtifact $testRoot } 'exactly 5'
    $unsupportedManifestProtection = $v3Manifest | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $unsupportedManifestProtection.protectedEnvironment.version = 7
    Assert-Throws 'unsupported manifest environment protection version' { Assert-HaiRecoveryManifest $unsupportedManifestProtection $testRoot } 'invalid Windows-user-bound'
    $wrongManifestBundle = $v3Manifest | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $wrongManifestBundle.protectedEnvironment.bundleId = 'other-bundle'
    Assert-Throws 'wrong manifest environment bundle binding' { Assert-HaiRecoveryManifest $wrongManifestBundle $testRoot } 'invalid Windows-user-bound'
    $restoreParent = Join-Path $testRoot 'restored-config'
    [IO.Directory]::CreateDirectory($restoreParent) | Out-Null
    $restoreTarget = Join-Path $restoreParent 'hai.env'
    Restore-HaiProtectedEnvironmentFile $testRoot $v3Manifest $restoreTarget
    if (-not (Test-Path -LiteralPath $restoreTarget -PathType Leaf) -or
        (Get-FileHash -LiteralPath $restoreTarget -Algorithm SHA256).Hash -cne (Get-FileHash -LiteralPath $environmentFixture -Algorithm SHA256).Hash) {
        throw 'Fixture protected environment did not round-trip byte-for-byte.'
    }
    $restoredAcl = Get-Acl -LiteralPath $restoreTarget
    $allowedSids = @((Get-HaiWindowsUserSid), 'S-1-5-18')
    $actualSids = @($restoredAcl.Access | ForEach-Object { $_.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value } | Sort-Object -Unique)
    if (-not $restoredAcl.AreAccessRulesProtected -or ($actualSids | Where-Object { $_ -notin $allowedSids }).Count -gt 0 -or
        ($allowedSids | Where-Object { $_ -notin $actualSids }).Count -gt 0) { throw 'Recovered environment ACL is not restricted to the current user and SYSTEM.' }
    Assert-Throws 'existing environment overwrite' { Restore-HaiProtectedEnvironmentFile $testRoot $v3Manifest $restoreTarget } 'already exists; refusing to overwrite'
    if ((Get-FileHash -LiteralPath $restoreTarget -Algorithm SHA256).Hash -cne (Get-FileHash -LiteralPath $environmentFixture -Algorithm SHA256).Hash) {
        throw 'Refused overwrite changed the pre-existing environment file.'
    }
    if (@(Get-ChildItem -LiteralPath $restoreParent -Force -Directory | Where-Object Name -like '.hai-env-recovery-*').Count -ne 0) {
        throw 'Protected environment staging directory was not cleaned after restore.'
    }

    $wrongIdentity = $v3Manifest | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $wrongIdentity.protectedEnvironment.windowsSid = 'S-1-5-21-101-202-303-1001'
    Assert-Throws 'protected environment identity mismatch' { Restore-HaiProtectedEnvironmentFile $testRoot $wrongIdentity (Join-Path $restoreParent 'identity-failure.env') } 'different Windows user'
    $unsupportedProtection = $v3Manifest | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $unsupportedProtection.protectedEnvironment.version = 9
    Assert-Throws 'protected environment version mismatch' { Restore-HaiProtectedEnvironmentFile $testRoot $unsupportedProtection (Join-Path $restoreParent 'version-failure.env') } 'invalid or unsupported'
    $wrongBundle = $v3Manifest | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $wrongBundle.protectedEnvironment.bundleId = 'different-bundle'
    Assert-Throws 'protected environment bundle identity mismatch' { Restore-HaiProtectedEnvironmentFile $testRoot $wrongBundle (Join-Path $restoreParent 'bundle-failure.env') } 'invalid or unsupported'
    $wrongDatabaseIdentity = $v3Manifest | ConvertTo-Json -Depth 10 | ConvertFrom-Json
    $wrongDatabaseIdentity.databases[0] = 'other_fixture_database'
    Assert-Throws 'protected environment database mismatch' { Restore-HaiProtectedEnvironmentFile $testRoot $wrongDatabaseIdentity (Join-Path $restoreParent 'database-failure.env') } 'does not match the backup manifest'
    Assert-Throws 'legacy bundle cannot recover missing environment' { Restore-HaiProtectedEnvironmentFile $testRoot $validManifest (Join-Path $restoreParent 'legacy-failure.env') } 'no recoverable protected environment'
    if (@(Get-ChildItem -LiteralPath $restoreParent -Force -File | Where-Object Name -in @('identity-failure.env', 'version-failure.env', 'bundle-failure.env', 'database-failure.env', 'legacy-failure.env')).Count -ne 0) {
        throw 'A rejected protected environment created a target file.'
    }

    $cleanupStage = Join-Path $restoreParent ('.hai-env-recovery-' + [Guid]::NewGuid().ToString('N'))
    $currentSid = [Security.Principal.SecurityIdentifier]::new((Get-HaiWindowsUserSid))
    New-HaiPrivateEnvironmentDirectory $cleanupStage (New-HaiPrivateDirectorySecurity $currentSid)
    $backupDirectoryFixture = Join-Path $restoreParent ('hai-backup-' + [Guid]::NewGuid().ToString('N'))
    New-HaiPrivateEnvironmentDirectory $backupDirectoryFixture (New-HaiPrivateDirectorySecurity $currentSid)
    Assert-HaiPrivateEnvironmentAcl $backupDirectoryFixture -Directory
    $backupDirectoryAcl = Get-Acl -LiteralPath $backupDirectoryFixture
    $backupDirectorySids = @($backupDirectoryAcl.Access | ForEach-Object { $_.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value } | Sort-Object -Unique)
    if (-not $backupDirectoryAcl.AreAccessRulesProtected -or ($backupDirectorySids | Where-Object { $_ -notin @((Get-HaiWindowsUserSid), 'S-1-5-18') }).Count -gt 0) {
        throw 'A private backup bundle directory is not restricted to the current user and SYSTEM.'
    }
    Remove-Item -LiteralPath $backupDirectoryFixture -Force
    $cleanupStageFile = Join-Path $cleanupStage 'environment.tmp'
    $cleanupStream = New-HaiPrivateEnvironmentFile $cleanupStageFile (New-HaiPrivateFileSecurity $currentSid)
    try { $cleanupBytes = [Text.Encoding]::UTF8.GetBytes('fixture-only'); $cleanupStream.Write($cleanupBytes, 0, $cleanupBytes.Length); [Array]::Clear($cleanupBytes, 0, $cleanupBytes.Length) }
    finally { $cleanupStream.Dispose() }
    Remove-HaiProtectedEnvironmentStage $cleanupStage
    if (Test-Path -LiteralPath $cleanupStage) { throw 'Protected environment cleanup helper left plaintext staging behind.' }

    $cipherPath = Join-Path $testRoot 'environment.dpapi'
    $cipherOriginal = [IO.File]::ReadAllBytes($cipherPath)
    try {
        $cipherTampered = [byte[]]$cipherOriginal.Clone()
        $cipherTampered[0] = $cipherTampered[0] -bxor 1
        [IO.File]::WriteAllBytes($cipherPath, $cipherTampered)
        $tamperedManifest = $v3Manifest | ConvertTo-Json -Depth 10 | ConvertFrom-Json
        $tamperedManifest.files | Where-Object name -ceq 'environment.dpapi' | ForEach-Object { $_.sha256 = '0' * 64 }
        Assert-Throws 'protected environment checksum mismatch' { Assert-HaiRecoveryManifest $tamperedManifest $testRoot } 'Checksum mismatch'
        $tamperedManifest.files | Where-Object name -ceq 'environment.dpapi' | ForEach-Object {
            $_.bytes = (Get-Item -LiteralPath $cipherPath).Length
            $_.sha256 = (Get-FileHash -LiteralPath $cipherPath -Algorithm SHA256).Hash.ToLowerInvariant()
        }
        Assert-HaiRecoveryManifest $tamperedManifest $testRoot
        Assert-Throws 'tampered protected environment' { Restore-HaiProtectedEnvironmentFile $testRoot $tamperedManifest (Join-Path $restoreParent 'tampered.env') } 'could not be decrypted'
    } finally { [IO.File]::WriteAllBytes($cipherPath, $cipherOriginal); [Array]::Clear($cipherOriginal, 0, $cipherOriginal.Length) }

    Assert-HaiArchiveEntries @("./", "./background_mode.json", "./emergency_stop.json")
    Assert-Throws "unsafe archive" { Assert-HaiArchiveEntries @("./", "../escape") } "unsafe path"

    $mode = '{"mode":"read_only"}'
    $stop = '{"engaged":false,"updatedAt":"2026-08-15T00:00:00Z","revision":1}'
    Assert-HaiControlStateDocuments $mode $stop
    Assert-Throws "missing mode" { Assert-HaiControlStateDocuments '{}' $stop } "background mode"
    Assert-Throws "invalid mode" { Assert-HaiControlStateDocuments '{"mode":"unrestricted"}' $stop } "background mode"
    Assert-Throws "malformed stop" { Assert-HaiControlStateDocuments $mode '{not-json' } "emergency-stop JSON"
    Assert-Throws "missing stop revision" { Assert-HaiControlStateDocuments $mode '{"engaged":false,"updatedAt":"2026-08-15T00:00:00Z"}' } "revision"
    Assert-Throws "engaged stop without evidence" { Assert-HaiControlStateDocuments $mode '{"engaged":true,"updatedAt":"2026-08-15T00:00:00Z","revision":1}' } "engaged state"

    $suffix = 'a' * 32
    $automation = "hai_restore_automation_$suffix"
    $identity = "hai_restore_identity_$suffix"
    $volume = "018-hai-phase2-restore-drill-$suffix"
    Assert-HaiScratchTargets $suffix $automation $identity $volume @('live_automation', 'live_identity')
    Assert-Throws "cross-database collision" { Assert-HaiScratchTargets $suffix $automation $identity $volume @('live_automation', $automation) } "unowned or live"
    Assert-Throws "unowned volume" { Assert-HaiScratchTargets $suffix $automation $identity '018-hai-phase2-control-state' @() } "unowned or live"
    Assert-Throws "noncanonical suffix" { Assert-HaiScratchTargets '123' $automation $identity $volume @() } "unowned or live"
    Assert-HaiOwnedDirectory $testRoot ([IO.Path]::GetTempPath()) ([IO.Path]::GetFileName($testRoot))
    Assert-Throws "sibling directory" { Assert-HaiOwnedDirectory ($testRoot + '-other') ([IO.Path]::GetTempPath()) ([IO.Path]::GetFileName($testRoot)) } "unowned recovery directory"

    $entries = @('./', './background_mode.json', './emergency_stop.json')
    $details = @('drwx------ fixture ./', '-rw------- fixture ./background_mode.json', '-rw------- fixture ./emergency_stop.json')
    Assert-HaiStrictControlArchive $entries $details
    Assert-Throws "safety symlink" { Assert-HaiStrictControlArchive $entries @($details[0], 'lrwx------ fixture ./background_mode.json -> elsewhere', $details[2]) } "non-regular"
    Assert-Throws "duplicate safety member" { Assert-HaiStrictControlArchive @('./', './background_mode.json', './background_mode.json') $details } "missing required|duplicate"
    Assert-Throws "extra safety member" { Assert-HaiStrictControlArchive ($entries + './extra') ($details + '-rw------- fixture ./extra') } "only its directory"

    foreach ($name in @('../escape', '/absolute', 'C:/escape', 'nested\escape', 'CON.txt', 'folder/trailing.', 'folder//file')) {
        $badEntry = [pscustomobject]@{ FullName = $name; ExternalAttributes = 0 }
        Assert-Throws "unsafe media $name" { Assert-HaiMediaEntries ([pscustomobject]@{ Entries = @($badEntry) }) } "Unsafe"
    }
    Assert-Throws 'media file before child path collision' {
        Assert-HaiMediaEntries ([pscustomobject]@{ Entries = @(
            [pscustomobject]@{ FullName = 'folder'; ExternalAttributes = 0 },
            [pscustomobject]@{ FullName = 'folder/child.txt'; ExternalAttributes = 0 }
        ) })
    } 'paths collide'
    Assert-Throws 'media child path before file collision' {
        Assert-HaiMediaEntries ([pscustomobject]@{ Entries = @(
            [pscustomobject]@{ FullName = 'folder/child.txt'; ExternalAttributes = 0 },
            [pscustomobject]@{ FullName = 'folder'; ExternalAttributes = 0 }
        ) })
    } 'paths collide'
    Assert-Throws 'media explicit directory and file collision' {
        Assert-HaiMediaEntries ([pscustomobject]@{ Entries = @(
            [pscustomobject]@{ FullName = 'folder/'; ExternalAttributes = 0 },
            [pscustomobject]@{ FullName = 'folder'; ExternalAttributes = 0 }
        ) })
    } 'Duplicate'
    Assert-Throws "media symlink" {
        Assert-HaiMediaEntries ([pscustomobject]@{ Entries = @([pscustomobject]@{ FullName = 'link'; ExternalAttributes = 0xA0000000L }) })
    } "Unsafe"
    Assert-Throws "case-colliding media" {
        Assert-HaiMediaEntries ([pscustomobject]@{ Entries = @([pscustomobject]@{ FullName = 'A.txt'; ExternalAttributes = 0 }, [pscustomobject]@{ FullName = 'a.txt'; ExternalAttributes = 0 }) })
    } "Duplicate"

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $mediaSource = Join-Path $testRoot 'media-source'
    [IO.Directory]::CreateDirectory($mediaSource) | Out-Null
    [IO.File]::WriteAllText((Join-Path $mediaSource 'receipt.txt'), 'original receipt')
    $zipPath = Join-Path $testRoot 'real-media.zip'
    [IO.Compression.ZipFile]::CreateFromDirectory($mediaSource, $zipPath)
    $archive = [IO.Compression.ZipFile]::OpenRead($zipPath)
    try {
        Assert-HaiMediaContents $archive $mediaSource
        [IO.File]::WriteAllText((Join-Path $mediaSource 'receipt.txt'), 'modified receipt')
        Assert-Throws "changed media bytes" { Assert-HaiMediaContents $archive $mediaSource } "does not match"
    } finally { $archive.Dispose() }

    # Child-script calls resolve this function, never the installed Docker executable.
    $global:HaiRecoveryContractMock = @{ Calls = [Collections.Generic.List[object]]::new(); VolumeExists = $false; VolumeToken = ''; FailCreate = ''; FailQuery = $false; FailCleanup = $false; AnonymousMounts = $false; DockerHost = 'npipe:////./pipe/docker_engine' }
    function docker {
        $commandArgs = @($args | ForEach-Object { [string]$_ })
        $global:HaiRecoveryContractMock.Calls.Add($commandArgs)
        $global:LASTEXITCODE = 0
        if ($commandArgs[0] -eq 'context' -and $commandArgs[1] -eq 'inspect') {
            return (@(@{ Endpoints = @{ docker = @{ Host = $global:HaiRecoveryContractMock.DockerHost } } }) | ConvertTo-Json -Depth 5)
        }
        if ($commandArgs[0] -eq 'compose') { return }
        if ($commandArgs[0] -eq 'volume' -and $commandArgs[1] -eq 'ls') { return @('018-hai-phase2-control-state') }
        if ($commandArgs[0] -eq 'ps' -and $commandArgs -contains '--format') { return @('fixture-hai-container') }
        if ($commandArgs[0] -eq 'inspect' -and $commandArgs -contains '{{json .Mounts}}') {
            if ($global:HaiRecoveryContractMock.AnonymousMounts) {
                return '[{"Type":"volume","Name":"synthetic-anonymous-volume","Destination":"/data"}]'
            }
            return '[]'
        }
        if ($commandArgs[0] -eq 'volume' -and $commandArgs[1] -eq 'inspect' -and $commandArgs -contains '{{json .Labels}}') {
            if ($global:HaiRecoveryContractMock.AnonymousMounts) { return '{"com.docker.volume.anonymous":""}' }
            return '{}'
        }
        if ($commandArgs[0] -eq 'image' -and $commandArgs[1] -eq 'inspect') { return 'fixture image exists' }
        if ($commandArgs[0] -eq 'volume' -and $commandArgs[1] -eq 'inspect' -and $commandArgs -contains '018-hai-phase2-control-state') { return 'fixture control volume exists' }
        if ($commandArgs[0] -eq 'exec' -and $commandArgs[2] -eq 'psql') {
            if ($global:HaiRecoveryContractMock.FailQuery) { $global:LASTEXITCODE = 1; return 'private fixture row must not be exposed' }
            $sql = $commandArgs[-1]
            if ($sql -match 'pg_catalog.pg_tables') {
                $names = if ($commandArgs[1] -eq '018-hai-postgres-automation') {
                    @('context_memories', 'workflow_decisions', 'workflow_events', 'workflow_items', 'life_ledger_commitment_revisions', 'life_ledger_cost_entries') | Sort-Object
                } else { @('users') }
                foreach ($name in $names) { [pscustomobject]@{ name = $name; columns = @([pscustomobject]@{ name = 'owner_identity'; type = 'varchar'; nullable = 'NO'; default = $null }) } | ConvertTo-Json -Depth 5 -Compress }
                return
            }
            if ($sql -match 'COPY \(') { return @('726f7731', '726f7732') }
            if ($sql -match 'pg_catalog.pg_sequences') { return 'fixture_sequence' }
            if ($sql -match 'last_value') { return '{"lastValue":"7","isCalled":true}' }
            if ($sql -match 'relowner') { return '0' }
            throw 'Unexpected mock evidence query.'
        }
        if ($commandArgs[0] -eq 'exec' -and $commandArgs[2] -eq 'createdb' -and $global:HaiRecoveryContractMock.FailCreate -eq $commandArgs[1]) { $global:LASTEXITCODE = 1; return }
        if ($commandArgs[0] -eq 'exec' -and $commandArgs[2] -eq 'dropdb' -and $global:HaiRecoveryContractMock.FailCleanup) { $global:LASTEXITCODE = 1; return }
        if ($commandArgs[0] -eq 'volume') {
            if ($commandArgs[1] -eq 'inspect') {
                if (-not $global:HaiRecoveryContractMock.VolumeExists) { $global:LASTEXITCODE = 1; return }
                if ($commandArgs -contains '--format') { return $global:HaiRecoveryContractMock.VolumeToken }
                return 'fixture volume exists'
            }
            if ($commandArgs[1] -eq 'create') { $global:HaiRecoveryContractMock.VolumeExists = $true; $global:HaiRecoveryContractMock.VolumeToken = $commandArgs[3].Substring('hai.recovery.run='.Length); return }
            if ($commandArgs[1] -eq 'rm') { $global:HaiRecoveryContractMock.VolumeExists = $false; return }
        }
        if ($commandArgs[0] -eq 'run') {
            if ($commandArgs -contains '-tzf') { return @('./', './background_mode.json', './emergency_stop.json') }
            if ($commandArgs -contains '-tvzf') { return @('drwx------ fixture ./', '-rw------- fixture ./background_mode.json', '-rw------- fixture ./emergency_stop.json') }
            if ($commandArgs[-1] -match 'sha256sum') { return @((('a' * 64) + '  /state/background_mode.json'), (('b' * 64) + '  /state/emergency_stop.json')) }
            if ($commandArgs[-1] -match 'cat /state/background_mode.json') { return '{"mode":"read_only"}' }
            if ($commandArgs[-1] -match 'cat /state/emergency_stop.json') { return '{"engaged":false,"updatedAt":"2026-08-15T00:00:00Z","revision":1}' }
        }
        if ($commandArgs[0] -notin @('exec', 'cp', 'compose', 'image', 'run')) { throw 'Unexpected Docker mock command.' }
    }

    function Invoke-HaiBoundedDockerCommand([string[]]$Arguments, [ValidateRange(1, 120)][int]$TimeoutSeconds = 15) {
        $commandArgs = @($Arguments | ForEach-Object { [string]$_ })
        $global:HaiRecoveryContractMock.Calls.Add($commandArgs)
        $global:LASTEXITCODE = 0
        if ($commandArgs.Count -eq 2 -and $commandArgs[0] -eq 'context' -and $commandArgs[1] -eq 'inspect') {
            return [pscustomobject]@{
                succeeded = $true
                timed_out = $false
                exit_code = 0
                output = (@(@{ Endpoints = @{ docker = @{ Host = $global:HaiRecoveryContractMock.DockerHost } } }) | ConvertTo-Json -Depth 5)
            }
        }
        throw 'Unexpected bounded Docker contract command.'
    }

    $savedDockerHost = $env:DOCKER_HOST
    $global:HaiRecoveryContractMock.AnonymousMounts = $true
    $uncoveredMounts = @(Get-HaiUncoveredVolumeMounts)
    if ($uncoveredMounts.Count -ne 1 -or $uncoveredMounts[0].container -cne 'fixture-hai-container' -or
        $uncoveredMounts[0].volume -cne 'synthetic-anonymous-volume' -or
        $uncoveredMounts[0].destination -cne '/data' -or -not $uncoveredMounts[0].anonymous) {
        throw 'Uncovered HAI Docker volume mounts were not identified from their Docker ownership label.'
    }
    $global:HaiRecoveryContractMock.AnonymousMounts = $false
    if (@(Get-HaiUncoveredVolumeMounts).Count -ne 0) { throw 'The HAI volume inventory reported an uncovered mount when none existed.' }
    $backupSource = [IO.File]::ReadAllText($backupScript)
    if (-not $backupSource.Contains('$uncoveredVolumeMounts = @(Get-HaiUncoveredVolumeMounts)') -or
        -not $backupSource.Contains('$latestUncoveredVolumeMounts = @(Get-HaiUncoveredVolumeMounts)')) {
        throw 'Backup does not inventory uncovered HAI volumes before and after producing a bundle.'
    }
    try {
        Remove-Item Env:DOCKER_HOST -ErrorAction SilentlyContinue
        Assert-HaiLocalDockerEngine
        Set-HaiRecoveryContractDockerHost 'tcp://remote.example:2376'
        Assert-Throws 'remote Docker context' { Assert-HaiLocalDockerEngine } 'require one verified local Docker engine'
        Set-HaiRecoveryContractDockerHost 'npipe:////./pipe/docker_engine'
        $env:DOCKER_HOST = 'tcp://remote.example:2376'
        Assert-Throws 'Docker host override' { Assert-HaiLocalDockerEngine } 'DOCKER_HOST override'
    } finally {
        if ($null -eq $savedDockerHost) { Remove-Item Env:DOCKER_HOST -ErrorAction SilentlyContinue }
        else { $env:DOCKER_HOST = $savedDockerHost }
    }

    $evidence = Get-HaiDatabaseIntegrity '018-hai-postgres-automation' 'fixture_user' 'fixture_db' 'automation'
    if ($evidence.tables.Count -ne 6 -or $evidence.tables[0].rows -ne 2 -or $evidence.tables[0].sha256 -cne (Get-HaiTextDigest "726f7731`n726f7732`n")) { throw 'Canonical evidence does not stream/count the actual query rows.' }
    foreach ($change in @('rows', 'sha256', 'columns', 'sequence')) {
        $mutated = $evidence | ConvertTo-Json -Depth 30 | ConvertFrom-Json
        switch ($change) {
            'rows' { $mutated.tables[0].rows++ }
            'sha256' { $mutated.tables[0].sha256 = '0' * 64 }
            'columns' { $mutated.tables[0].columns[0].name = 'changed_owner_identity' }
            'sequence' { $mutated.sequences[0].state.lastValue = '8' }
        }
        Assert-Throws "changed $change evidence" { Assert-HaiRecoveryEvidence $evidence $mutated 'Fixture' } "does not match"
    }
    $global:HaiRecoveryContractMock.FailQuery = $true
    Assert-Throws "failed private query" { Get-HaiDatabaseIntegrity '018-hai-postgres-automation' 'fixture_user' 'fixture_db' 'automation' } "query failed; database output suppressed"
    $global:HaiRecoveryContractMock.FailQuery = $false

    # Exercise the real drill's finally block using an entirely owned, tiny bundle.
    [IO.File]::Copy($zipPath, (Join-Path $testRoot 'media.zip'), $true)
    $validManifest | Add-Member -NotePropertyName integrity -NotePropertyValue ([pscustomobject]@{
        contract = 'hai-recovery-integrity.v1'
        automation = $evidence
        identity = (Get-HaiDatabaseIntegrity '018-hai-postgres-idp' 'fixture_user' 'fixture_identity' 'identity')
        controls = (Get-HaiControlDigests 'fixture-volume' 'fixture-image')
    })
    $validManifest | Add-Member -NotePropertyName extendedRecoveryCoverage -NotePropertyValue $coverage -Force
    $validManifest.files = @('automation.dump', 'identity.dump', 'media.zip', 'phase2-control-state.tar.gz') | ForEach-Object { New-ManifestFile (Join-Path $testRoot $_) }
    $validManifest | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath (Join-Path $testRoot 'manifest.json') -Encoding utf8
    $envFixture = Join-Path $testRoot 'fixture.env'
    [IO.File]::WriteAllText($envFixture, "DB_USER=fixture_user`nAUTOMATION_DB_NAME=fixture_automation`nIDP_DB_NAME=fixture_identity`nIMAGE_SAVE_DIR=/root/images`n")
    if (-not (Test-Path -LiteralPath $imagesFixture)) {
        [IO.Directory]::CreateDirectory($imagesFixture) | Out-Null
        $createdImagesFixture = $true
    }
    $drill = Join-Path $PSScriptRoot 'test-restore-windows.ps1'
    $global:HaiRecoveryContractMock.Calls.Clear()
    Set-HaiRecoveryContractDockerHost 'tcp://remote.example:2376'
    Assert-Throws 'restore against remote Docker context' { & $drill -BackupDirectory $testRoot -EnvFile $envFixture -ContractTest } 'require one verified local Docker engine'
    $remoteMutations = @($global:HaiRecoveryContractMock.Calls | Where-Object {
        $_[0] -in @('exec', 'cp', 'run') -or ($_[0] -eq 'volume' -and $_[1] -in @('create', 'rm'))
    })
    if ($remoteMutations.Count -ne 0) { throw 'Restore attempted Docker mutations before refusing the remote context.' }
    Set-HaiRecoveryContractDockerHost 'npipe:////./pipe/docker_engine'
    foreach ($collision in @('018-hai-postgres-automation', '018-hai-postgres-idp')) {
        $global:HaiRecoveryContractMock.Calls.Clear()
        $global:HaiRecoveryContractMock.FailCreate = $collision
        Assert-Throws "refused scratch creation $collision" { & $drill -BackupDirectory $testRoot -EnvFile $envFixture -ContractTest } "Could not create the .* scratch database"
        $drops = @($global:HaiRecoveryContractMock.Calls | Where-Object { $_[0] -eq 'exec' -and $_[2] -eq 'dropdb' })
        $expectedDrops = if ($collision -eq '018-hai-postgres-automation') { 0 } else { 1 }
        if ($drops.Count -ne $expectedDrops -or @($drops | Where-Object { $_[1] -eq $collision }).Count -ne 0) { throw 'Drill attempted to drop a database it failed to create.' }
    }
    $global:HaiRecoveryContractMock.FailCreate = ''
    $global:HaiRecoveryContractMock.Calls.Clear()
    $manifestPath = Join-Path $testRoot 'manifest.json'
    $savedManifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding utf8 | ConvertFrom-Json
    $savedManifest.PSObject.Properties.Remove('extendedRecoveryCoverage')
    $savedManifest | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath $manifestPath -Encoding utf8
    Assert-Throws 'restore old incomplete bundle' { & $drill -BackupDirectory $testRoot -EnvFile $envFixture -ContractTest } 'lacks explicit optional recovery coverage'
    $earlyRestoreMutations = @($global:HaiRecoveryContractMock.Calls | Where-Object {
        $_[0] -in @('exec', 'cp', 'run') -or ($_[0] -eq 'volume' -and $_[1] -in @('create', 'rm'))
    })
    if ($earlyRestoreMutations.Count -ne 0) { throw 'Restore attempted a mutation before rejecting missing extended coverage.' }
    $savedManifest | Add-Member -NotePropertyName extendedRecoveryCoverage -NotePropertyValue $coverage -Force
    $savedManifest | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath $manifestPath -Encoding utf8

    $global:HaiRecoveryContractMock.Calls.Clear()
    $global:HaiRecoveryContractMock.VolumeExists = $true
    $global:HaiRecoveryContractMock.VolumeToken = 'another-owner'
    Assert-Throws 'colliding volume' { & $drill -BackupDirectory $testRoot -EnvFile $envFixture -ContractTest } 'already exists'
    if (@($global:HaiRecoveryContractMock.Calls | Where-Object { $_[0] -eq 'volume' -and $_[1] -eq 'rm' }).Count -ne 0) { throw 'Drill removed a preexisting volume.' }
    $global:HaiRecoveryContractMock.VolumeExists = $false
    $global:HaiRecoveryContractMock.FailCleanup = $true
    Assert-Throws 'failed fixture cleanup' { & $drill -BackupDirectory $testRoot -EnvFile $envFixture -ContractTest } 'clean owned-fixture cleanup'
    $global:HaiRecoveryContractMock.FailCleanup = $false
    & $drill -BackupDirectory $testRoot -EnvFile $envFixture -ContractTest

    $v3Manifest | Add-Member -NotePropertyName integrity -NotePropertyValue ([pscustomobject]@{
        contract = 'hai-recovery-integrity.v1'
        automation = $evidence
        identity = (Get-HaiDatabaseIntegrity '018-hai-postgres-idp' 'fixture_user' 'fixture_identity' 'identity')
        controls = (Get-HaiControlDigests 'fixture-volume' 'fixture-image')
    }) -Force
    $v3Manifest | Add-Member -NotePropertyName extendedRecoveryCoverage -NotePropertyValue $coverage -Force
    $v3Manifest.files = @('automation.dump', 'identity.dump', 'media.zip', 'phase2-control-state.tar.gz', 'environment.dpapi') | ForEach-Object { New-ManifestFile (Join-Path $testRoot $_) }
    $v3Manifest | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath (Join-Path $testRoot 'manifest.json') -Encoding utf8
    $validateOnlyTarget = Join-Path $restoreParent 'validate-only.env'
    & $drill -BackupDirectory $testRoot -EnvFile $validateOnlyTarget -ValidateOnly -ContractTest
    if ((Test-Path -LiteralPath $validateOnlyTarget -PathType Leaf) -or
        @(Get-ChildItem -LiteralPath $restoreParent -Force -Directory | Where-Object Name -like '.hai-env-recovery-*').Count -ne 0) {
        throw 'ValidateOnly wrote a recovered environment or left protected plaintext staging behind.'
    }
    $integratedTarget = Join-Path $restoreParent 'recovered-by-restore.env'
    & $drill -BackupDirectory $testRoot -EnvFile $integratedTarget -ContractTest
    if ((Get-FileHash -LiteralPath $integratedTarget -Algorithm SHA256).Hash -cne (Get-FileHash -LiteralPath $environmentFixture -Algorithm SHA256).Hash -or
        @(Get-ChildItem -LiteralPath $restoreParent -Force -Directory | Where-Object Name -like '.hai-env-recovery-*').Count -ne 0) {
        throw 'Integrated missing-environment restore did not preserve bytes and clean its protected staging directory.'
    }

    $restore = [IO.File]::ReadAllText((Join-Path $PSScriptRoot "test-restore-windows.ps1"))
    if ($restore -match '018-hai-phase2-control-state:/restore') {
        throw "Restore drill must never mount the live safety volume as its restore target."
    }
    if ($restore -notmatch '\$scratchControlVolume' -or $restore -notmatch 'docker volume rm') {
        throw "Restore drill does not own and remove a disposable safety volume."
    }
    if ($restore -notmatch '--cap-add CHOWN' -or
        $restore -notmatch 'tar -oxzf' -or
        $restore -notmatch 'chown -R 10001:10001 /restore') {
        throw "Restore drill does not normalize safety-state ownership for the HAI service account."
    }
    $backup = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'backup-windows.ps1'))
    if ($backup -match 'compose .*up -d' -or $backup -notmatch 'compose .*start \$service' -or
        $backup -match 'Remove-Item -LiteralPath \$bundle' -or $backup -match 'Directory -Path \$bundle -Force') {
        throw 'Backup must preserve failed/colliding bundles and recover only the original stopped services.'
    }
    if ($backup -notmatch '(?s)New-HaiPrivateDirectorySecurity\s+\$backupSid.*?New-HaiPrivateEnvironmentDirectory\s+\$bundle.*?Assert-HaiPrivateEnvironmentAcl\s+\$bundle\s+-Directory') {
        throw 'Windows backups must create and verify a current-user/SYSTEM-only ACL before writing sensitive bundle files.'
    }
} finally {
    if ($null -eq $priorMock) { Remove-Variable -Name HaiRecoveryContractMock -Scope Global -ErrorAction SilentlyContinue }
    else { Set-Variable -Name HaiRecoveryContractMock -Scope Global -Value $priorMock.Value }
    $resolved = [IO.Path]::GetFullPath($testRoot)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    Assert-HaiOwnedDirectory $resolved $tempRoot ([IO.Path]::GetFileName($testRoot))
    if ([IO.Directory]::Exists($resolved)) { [IO.Directory]::Delete($resolved, $true) }
    $sourceResolved = [IO.Path]::GetFullPath($sourceFixtureRoot)
    Assert-HaiOwnedDirectory $sourceResolved $tempRoot ([IO.Path]::GetFileName($sourceFixtureRoot))
    if ([IO.Directory]::Exists($sourceResolved)) { [IO.Directory]::Delete($sourceResolved, $true) }
    if ($createdImagesFixture -and [IO.Directory]::Exists($imagesFixture)) {
        $imageItem = Get-Item -LiteralPath $imagesFixture -Force
        if (($imageItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0 -and
            @(Get-ChildItem -LiteralPath $imagesFixture -Force).Count -eq 0) {
            [IO.Directory]::Delete($imagesFixture, $false)
        } else {
            Write-Warning 'Owned images test fixture changed during the test and was preserved for inspection.'
        }
    }
}

Write-Host "Windows recovery behavioral contracts passed."
