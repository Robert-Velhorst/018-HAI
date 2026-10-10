param(
    [string]$TempRoot = [IO.Path]::GetTempPath(),
    [ValidateRange(1, 8760)]
    [int]$MinimumAgeHours = 24
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath $TempRoot).Path
$rootItem = Get-Item -LiteralPath $root -Force
if (($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
    throw 'Temp scan root is a reparse point; fixture cleanup readiness is blocked.'
}
$pattern = '^hai-acceptance-([0-9a-f]{32})$'
$dockerAvailable = $false
$dockerFailure = 'local Docker engine was not verified'

try {
    . (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
    Assert-HaiLocalDockerEngine
    $dockerAvailable = $true
    $dockerFailure = ''
} catch {
    $dockerFailure = 'local Docker engine unavailable or not verifiably local'
}

function Test-DockerQuery([string[]]$Arguments) {
    $result = Invoke-HaiBoundedDockerCommand $Arguments -TimeoutSeconds 10
    if (-not $result.succeeded -or $result.timed_out) { return [pscustomobject]@{ Success = $false; Items = @() } }
    return [pscustomobject]@{ Success = $true; Items = @(([string]$result.output -split "`r?`n") | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) }) }
}

function Get-HaiExampleEnvironmentValues {
    $repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
    $examplePath = Join-Path $repoRoot '.env.example'
    $exampleItem = Get-Item -LiteralPath $examplePath -Force -ErrorAction Stop
    if ($exampleItem.PSIsContainer -or $exampleItem.Length -gt 1048576 -or
        ($exampleItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'tracked environment example is not a bounded regular file'
    }

    $git = Get-Command git -ErrorAction Stop
    $gitPrefix = @('-c', "safe.directory=$repoRoot", '-C', $repoRoot)
    $tracked = @(& $git.Source @gitPrefix ls-files --error-unmatch -- .env.example 2>$null)
    if ($LASTEXITCODE -ne 0 -or $tracked.Count -ne 1 -or [string]$tracked[0] -cne '.env.example') {
        throw 'environment example is not tracked by the current repository'
    }
    & $git.Source @gitPrefix diff --quiet HEAD -- .env.example 2>$null
    if ($LASTEXITCODE -ne 0) { throw 'environment example has uncommitted changes' }
    & $git.Source @gitPrefix diff --cached --quiet HEAD -- .env.example 2>$null
    if ($LASTEXITCODE -ne 0) { throw 'environment example has staged changes' }

    $values = @{}
    foreach ($line in Get-Content -LiteralPath $examplePath) {
        if ([string]::IsNullOrWhiteSpace($line) -or $line.TrimStart().StartsWith('#', [StringComparison]::Ordinal)) { continue }
        if ($line -notmatch '^([A-Z][A-Z0-9_]*)=(.*)$') { throw 'environment example contains an invalid line' }
        $key = $Matches[1]
        if ($values.ContainsKey($key)) { throw 'environment example contains duplicate keys' }
        $values[$key] = $Matches[2]
    }
    return $values
}

function Assert-HaiSyntheticEnvironment([string]$Path, [string]$Project) {
    $item = Get-Item -LiteralPath $Path -Force
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.Length -gt 1048576) {
        throw 'synthetic environment is not a bounded regular file'
    }
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $Path) {
        if ($line -notmatch '^([A-Z][A-Z0-9_]*)=(.*)$') { throw 'synthetic environment contains an invalid line' }
        $key = $Matches[1]
        if ($values.ContainsKey($key)) { throw 'synthetic environment contains duplicate keys' }
        $values[$key] = $Matches[2]
    }
    foreach ($expected in @{
        COMPOSE_PROJECT_NAME = $Project
        RUN_MODE = 'production'
        FIRST_RUN_ADMIN_EMAIL = 'e2e-owner@example.test'
        LOCAL_LOGIN_BYPASS_ENABLED = 'false'
        LLM_PROVIDERS_JSON = '[]'
        GOOGLE_OAUTH_CLIENT_ID = ''
        GOOGLE_OAUTH_CLIENT_SECRET = ''
        SMTP_HOST = ''
        SMTP_USERNAME = ''
        SMTP_PASSWORD = ''
        GITHUB_SOURCE_TOKEN = ''
        TRELLO_API_KEY = ''
        TRELLO_API_SECRET = ''
        TRELLO_READ_TOKEN = ''
    }.GetEnumerator()) {
        if (-not $values.ContainsKey($expected.Key) -or [string]$values[$expected.Key] -cne [string]$expected.Value) {
            throw 'synthetic environment contains a missing or non-synthetic required setting'
        }
    }
    if ([string]$values.FIRST_RUN_ADMIN_PASSWORD -notmatch '^E2eOnly-[0-9a-f]{32}$') {
        throw 'synthetic bootstrap password marker is invalid'
    }
    $exampleValues = Get-HaiExampleEnvironmentValues
    $credentialPattern = '(?:^|_)(?:PASSWORD|PASS|TOKEN|SECRET|API_KEY|CLIENT_ID|PRIVATE_KEY|SIGNING_KEY|WORKSPACE_KEY|ENCRYPTION_KEY|ACCESS_KEY|SHARED_KEY|CREDENTIALS?)$'
    foreach ($entry in $values.GetEnumerator()) {
        if ($entry.Key -match $credentialPattern -and -not [string]::IsNullOrEmpty([string]$entry.Value) -and
            [string]$entry.Value -notmatch '^(?:[0-9a-f]{64}|E2eOnly-[0-9a-f]{32})$' -and
            (-not $exampleValues.ContainsKey([string]$entry.Key) -or
                [string]$entry.Value -cne [string]$exampleValues[[string]$entry.Key])) {
            throw 'synthetic environment contains a credential-like value outside generated markers and the unchanged tracked example'
        }
        if ($entry.Key -match '_ENABLED$' -and $entry.Key -cne 'SOURCE_MANUAL_WORKER_ENABLED' -and [string]$entry.Value -cne 'false') {
            throw 'synthetic environment enables an external or autonomous capability'
        }
    }
    if ([string]$values.SOURCE_MANUAL_WORKER_ENABLED -notin @('true', 'false') -or
        [string]$values.HAI_PHASE2_MODE -notin @('paused', 'autonomous_safe')) {
        throw 'synthetic environment execution mode is invalid'
    }
}

function Assert-HaiCleanupManifest([string]$Path, [string]$Owner, [string]$Project, [string]$EnvironmentPath, [switch]$AllowEnvironmentMissing) {
    $item = Get-Item -LiteralPath $Path -Force
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.Length -gt 16384) {
        throw 'cleanup manifest is not a bounded regular file'
    }
    $marker = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    foreach ($property in @('version', 'kind', 'state', 'owner', 'project', 'createdUtc', 'syntheticEnvBytes', 'syntheticEnvSha256')) {
        if ($null -eq $marker.PSObject.Properties[$property]) { throw 'cleanup manifest is missing a required provenance field' }
    }
    $hasEnvironment = Test-Path -LiteralPath $EnvironmentPath -PathType Leaf
    $environment = if ($hasEnvironment) { Get-Item -LiteralPath $EnvironmentPath -Force } else { $null }
    $environmentHash = $null
    if ($hasEnvironment) {
        Assert-HaiSyntheticEnvironment $EnvironmentPath $Project
        $environmentHash = (Get-FileHash -LiteralPath $EnvironmentPath -Algorithm SHA256).Hash.ToLowerInvariant()
    } elseif (-not $AllowEnvironmentMissing -or [long]$marker.syntheticEnvBytes -ne 0 -or
        -not [string]::IsNullOrEmpty([string]$marker.syntheticEnvSha256)) {
        throw 'synthetic environment is missing without an empty interrupted-preparation marker'
    }
    if ([int]$marker.version -ne 1 -or
        [string]$marker.kind -cne 'hai-acceptance-synthetic-fixture' -or
        [string]$marker.state -cne 'preparing' -or
        [string]$marker.owner -cne $Owner -or
        [string]$marker.project -cne $Project -or
        ($hasEnvironment -and [long]$marker.syntheticEnvBytes -ne [long]$environment.Length) -or
        ($hasEnvironment -and [string]$marker.syntheticEnvSha256 -cne $environmentHash) -or
        ($hasEnvironment -and [string]$marker.syntheticEnvSha256 -notmatch '^[0-9a-f]{64}$')) {
        throw 'cleanup manifest identity or synthetic environment hash does not match'
    }
    return [DateTimeOffset]::Parse([string]$marker.createdUtc).ToUniversalTime()
}

$results = foreach ($directory in @(Get-ChildItem -LiteralPath $root -Directory -Force | Where-Object Name -Match $pattern)) {
    $match = [regex]::Match($directory.Name, $pattern)
    $owner = $match.Groups[1].Value
    $expectedProject = 'hai-acceptance-' + $owner.Substring(0, 12)
    $record = [ordered]@{
        directory = $directory.FullName
        owner = $owner
        project = $null
        file_count = 0
        bytes = 0L
        age_hours = $null
        source_hashes = @()
        docker_resources_checked = $dockerAvailable
        related_containers = $null
        related_networks = $null
        related_volumes = $null
        disposition = 'retain_unverified'
        cleanup_authorized = $false
    }

    try {
        if (($directory.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw 'fixture root is a reparse point'
        }
        $allEntries = @(Get-ChildItem -LiteralPath $directory.FullName -Force -Recurse -ErrorAction Stop)
        if (@($allEntries | Where-Object { ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 }).Count -gt 0) {
            throw 'reparse point present'
        }
        $expectedFiles = @('compose.json', 'init.sql', 'manifest.json', 'nginx.conf.template', 'provision-runtime-role.sh', 'sources/acceptance.txt', 'synthetic.env')
        $expectedDirectories = @('sites-enabled', 'sources')
        $actualFiles = @($allEntries | Where-Object { -not $_.PSIsContainer } | ForEach-Object { $_.FullName.Substring($directory.FullName.Length + 1).Replace('\', '/') } | Sort-Object)
        $actualDirectories = @($allEntries | Where-Object PSIsContainer | ForEach-Object { $_.FullName.Substring($directory.FullName.Length + 1).Replace('\', '/') } | Sort-Object)
        $cleanupManifestPath = Join-Path $directory.FullName 'cleanup-manifest.json'
        $environmentPath = Join-Path $directory.FullName 'synthetic.env'
        $hasCleanupManifest = $actualFiles -ccontains 'cleanup-manifest.json'
        $isPreparingFixture = $actualFiles.Count -in @(1, 2) -and
            @($actualFiles | Where-Object { $_ -cnotin @('cleanup-manifest.json', 'synthetic.env') }).Count -eq 0 -and
            $actualDirectories.Count -eq 0
        $isCompleteFixture = @($actualFiles | Where-Object { $_ -cne 'cleanup-manifest.json' }).Count -eq $expectedFiles.Count -and
            @(Compare-Object ($expectedFiles | Sort-Object) (@($actualFiles | Where-Object { $_ -cne 'cleanup-manifest.json' } | Sort-Object))).Count -eq 0 -and
            @(Compare-Object $expectedDirectories $actualDirectories).Count -eq 0
        $files = @($allEntries | Where-Object { -not $_.PSIsContainer })
        $record.file_count = $files.Count
        $record.bytes = [long](($files | Measure-Object -Property Length -Sum).Sum)
        if (-not $isPreparingFixture -and -not $isCompleteFixture) {
            throw 'fixture file/directory inventory differs from the generated acceptance layout'
        }

        $manifestPath = Join-Path $directory.FullName 'manifest.json'
        $composePath = Join-Path $directory.FullName 'compose.json'
        if ($isPreparingFixture) {
            if (-not $hasCleanupManifest) {
                throw 'interrupted fixture is missing its cleanup manifest'
            }
            $createdUtc = Assert-HaiCleanupManifest $cleanupManifestPath $owner $expectedProject $environmentPath -AllowEnvironmentMissing:(-not (Test-Path -LiteralPath $environmentPath -PathType Leaf))
            $record.project = $expectedProject
        } else {
            if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf) -or
                -not (Test-Path -LiteralPath $composePath -PathType Leaf)) {
                throw 'generated manifest or compose definition is missing'
            }
            $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
            $compose = Get-Content -LiteralPath $composePath -Raw | ConvertFrom-Json
            if ([int]$manifest.version -ne 1 -or
                [string]$manifest.owner -cne $owner -or
                [string]$manifest.project -cne $expectedProject -or
                [string]$manifest.email -cne 'e2e-owner@example.test' -or
                [string]$manifest.password -notmatch '^E2eOnly-[0-9a-f]{32}$' -or
                [string]$compose.name -cne $expectedProject -or
                $null -eq $compose.services -or $compose.services.PSObject.Properties.Count -eq 0 -or
                $null -eq $compose.networks -or $compose.networks.PSObject.Properties.Count -eq 0 -or
                (Get-Content -LiteralPath (Join-Path $directory.FullName 'sources/acceptance.txt') -Raw).Trim() -cne 'Synthetic HAI acceptance source. No personal records.') {
                throw 'generated fixture identity markers do not match the directory'
            }
            $record.project = $expectedProject
            $createdUtc = [DateTimeOffset]::Parse([string]$manifest.createdUtc).ToUniversalTime()
            if ($hasCleanupManifest) {
                $markerCreatedUtc = Assert-HaiCleanupManifest $cleanupManifestPath $owner $expectedProject $environmentPath
                if ($markerCreatedUtc -gt $createdUtc) { throw 'cleanup manifest timestamp is inconsistent with the completed fixture' }
                if ($markerCreatedUtc -lt $createdUtc) { $createdUtc = $markerCreatedUtc }
            } else {
                Assert-HaiSyntheticEnvironment $environmentPath $expectedProject
            }
        }
        if ($directory.CreationTimeUtc -gt $createdUtc.UtcDateTime) {
            $createdUtc = [DateTimeOffset]$directory.CreationTimeUtc
        }
        $latestWriteUtc = (@($allEntries | ForEach-Object LastWriteTimeUtc) + @($directory.LastWriteTimeUtc) | Measure-Object -Maximum).Maximum
        if ($null -eq $latestWriteUtc) { throw 'fixture modification time is unavailable' }
        $record.age_hours = [math]::Round([math]::Min(
            ([DateTimeOffset]::UtcNow - $createdUtc).TotalHours,
            ([DateTimeOffset]::UtcNow - [DateTimeOffset]$latestWriteUtc).TotalHours
        ), 2)
        if ($record.age_hours -lt $MinimumAgeHours) {
            $record.disposition = 'retain_recent'
            $record.reason = 'generated fixture has not reached the minimum retention age'
            [pscustomobject]$record
            continue
        }

        if (-not $isPreparingFixture) {
            foreach ($service in $compose.services.PSObject.Properties) {
                $config = $service.Value
                if ($null -eq $config.labels -or [string]$config.labels.'hai.acceptance.owner' -cne $owner) {
                    throw 'Compose resource owner marker mismatch'
                }
                foreach ($mount in @($config.volumes)) {
                    if ([string]$mount.type -ceq 'bind') {
                        $source = [IO.Path]::GetFullPath([string]$mount.source)
                        $fixturePrefix = [IO.Path]::GetFullPath($directory.FullName).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
                        if (-not $source.StartsWith($fixturePrefix, [StringComparison]::OrdinalIgnoreCase)) {
                            throw 'Compose bind source escapes the fixture directory'
                        }
                    }
                }
            }
            foreach ($network in $compose.networks.PSObject.Properties) {
                if ([string]$network.Value.labels.'hai.acceptance.owner' -cne $owner) {
                    throw 'Compose network owner marker mismatch'
                }
            }
        }

        $record.source_hashes = @($files | Sort-Object FullName | ForEach-Object {
            [pscustomobject]@{
                relative_path = $_.FullName.Substring($directory.FullName.Length + 1).Replace('\', '/')
                bytes = [long]$_.Length
                sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
            }
        })

        if (-not $dockerAvailable) {
            throw 'Docker inventory unavailable; resource absence cannot be verified'
        }
        $containerIds = Test-DockerQuery @('ps', '-aq', '--filter', "label=com.docker.compose.project=$expectedProject")
        $ownerContainerIds = Test-DockerQuery @('ps', '-aq', '--filter', "label=hai.acceptance.owner=$owner")
        $networkIds = Test-DockerQuery @('network', 'ls', '-q', '--filter', "label=com.docker.compose.project=$expectedProject")
        $ownerNetworkIds = Test-DockerQuery @('network', 'ls', '-q', '--filter', "label=hai.acceptance.owner=$owner")
        $volumeIds = Test-DockerQuery @('volume', 'ls', '-q', '--filter', "label=com.docker.compose.project=$expectedProject")
        $ownerVolumeIds = Test-DockerQuery @('volume', 'ls', '-q', '--filter', "label=hai.acceptance.owner=$owner")
        $containerNames = Test-DockerQuery @('ps', '-a', '--format', '{{.Names}}')
        $networkNames = Test-DockerQuery @('network', 'ls', '--format', '{{.Name}}')
        $volumeNames = Test-DockerQuery @('volume', 'ls', '--format', '{{.Name}}')
        if (-not $containerIds.Success -or -not $ownerContainerIds.Success -or
            -not $networkIds.Success -or -not $ownerNetworkIds.Success -or
            -not $volumeIds.Success -or -not $ownerVolumeIds.Success -or
            -not $containerNames.Success -or -not $networkNames.Success -or -not $volumeNames.Success) {
            throw 'Docker resource inventory query failed'
        }
        $record.related_containers = @(@($containerIds.Items) + @($ownerContainerIds.Items) + @($containerNames.Items | Where-Object { ([string]$_).StartsWith($expectedProject + '-', [StringComparison]::OrdinalIgnoreCase) }) | Sort-Object -Unique).Count
        $record.related_networks = @(@($networkIds.Items) + @($ownerNetworkIds.Items) + @($networkNames.Items | Where-Object { ([string]$_).StartsWith($expectedProject + '-', [StringComparison]::OrdinalIgnoreCase) }) | Sort-Object -Unique).Count
        $record.related_volumes = @(@($volumeIds.Items) + @($ownerVolumeIds.Items) + @($volumeNames.Items | Where-Object { ([string]$_).StartsWith($expectedProject + '-', [StringComparison]::OrdinalIgnoreCase) }) | Sort-Object -Unique).Count
        if ($record.related_containers -eq 0 -and $record.related_networks -eq 0 -and $record.related_volumes -eq 0) {
            $record.disposition = 'candidate_manual_cleanup'
        }
    }
    catch {
        $record.disposition = 'retain_unverified'
        $record.reason = if ($dockerFailure) { $dockerFailure } else { $_.Exception.Message }
    }
    [pscustomobject]$record
}

[pscustomobject][ordered]@{
    temp_root = $root
    minimum_age_hours = $MinimumAgeHours
    inspected_directories = @($results).Count
    candidate_directories = @($results | Where-Object disposition -CEQ 'candidate_manual_cleanup').Count
    retained_or_unverified_directories = @($results | Where-Object disposition -CEQ 'retain_unverified').Count
    cleanup_authorized = $false
    deletion_performed = $false
    directories = @($results)
} | ConvertTo-Json -Depth 8
