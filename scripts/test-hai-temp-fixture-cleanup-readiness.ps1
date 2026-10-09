param(
    [string]$TempRoot = [IO.Path]::GetTempPath()
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath $TempRoot).Path
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
    $global:LASTEXITCODE = 0
    $output = @(& docker @Arguments 2>$null)
    if ($LASTEXITCODE -ne 0) { return [pscustomobject]@{ Success = $false; Items = @() } }
    return [pscustomobject]@{ Success = $true; Items = @($output | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) }) }
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
        source_hashes = @()
        docker_resources_checked = $dockerAvailable
        related_containers = $null
        related_networks = $null
        related_volumes = $null
        disposition = 'retain_unverified'
        cleanup_authorized = $false
    }

    try {
        $allEntries = @(Get-ChildItem -LiteralPath $directory.FullName -Force -Recurse -ErrorAction Stop)
        if (@($allEntries | Where-Object { ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 }).Count -gt 0) {
            throw 'reparse point present'
        }
        $expectedFiles = @('compose.json', 'init.sql', 'manifest.json', 'nginx.conf.template', 'provision-runtime-role.sh', 'sources/acceptance.txt', 'synthetic.env')
        $expectedDirectories = @('sites-enabled', 'sources')
        $actualFiles = @($allEntries | Where-Object { -not $_.PSIsContainer } | ForEach-Object { $_.FullName.Substring($directory.FullName.Length + 1).Replace('\', '/') } | Sort-Object)
        $actualDirectories = @($allEntries | Where-Object PSIsContainer | ForEach-Object { $_.FullName.Substring($directory.FullName.Length + 1).Replace('\', '/') } | Sort-Object)
        if (@(Compare-Object $expectedFiles $actualFiles).Count -gt 0 -or
            @(Compare-Object $expectedDirectories $actualDirectories).Count -gt 0) {
            throw 'fixture file/directory inventory differs from the generated acceptance layout'
        }
        $files = @($allEntries | Where-Object { -not $_.PSIsContainer })
        $record.file_count = $files.Count
        $record.bytes = [long](($files | Measure-Object -Property Length -Sum).Sum)

        $manifestPath = Join-Path $directory.FullName 'manifest.json'
        $composePath = Join-Path $directory.FullName 'compose.json'
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
    inspected_directories = @($results).Count
    candidate_directories = @($results | Where-Object disposition -CEQ 'candidate_manual_cleanup').Count
    retained_or_unverified_directories = @($results | Where-Object disposition -CEQ 'retain_unverified').Count
    cleanup_authorized = $false
    deletion_performed = $false
    directories = @($results)
} | ConvertTo-Json -Depth 8
