function Test-HaiInstallerExcludedPath {
    param([Parameter(Mandatory = $true)][string]$ExcludeRelativePath)

    $normalized = $ExcludeRelativePath -replace '\\', '/'
    if ([IO.Path]::IsPathRooted($normalized) -or $normalized -match '(^|/)\.\.(/|$)' -or $normalized -match '(^|/)\.(?:/|$)') { return $true }
    if ($normalized -ne '.env.example' -and ($normalized -match '(^|/)\.env(?:$|[._-])' -or $normalized -match '(^|/)\.envrc$')) { return $true }
    if ($normalized -match '(^|/)(db_data[^/]*|database_data|backups?|connected-sources|agent-workspaces|mini-swe-workspaces|images)(/|$)') { return $true }
    if ($normalized -match '(^|/)(node_modules|dist|\.npm-cache|\.playwright-cli|\.playwright-mcp|\.verify|\.pytest_cache|\.git|installer/release)(/|$)') { return $true }
    $fileName = [IO.Path]::GetFileName($normalized)
    if ($fileName -match '(?i)(^|[._-])(?:secrets?|credentials?|service-account)(?:[._-]|$)' -or
        $fileName -match '^(?i)(?:id_rsa|id_ed25519|id_ecdsa|id_dsa)(?:\..*)?$' -or
        $fileName -match '^(?i)(?:\.npmrc|\.pypirc|\.netrc|\.pgpass|\.my\.cnf)$') { return $true }
    if ($normalized -match '(?i)\.(?:zip|tar|gz|7z|rar|pdf|bak|backup|old|db|sqlite|sqlite3|sqlite-wal|sqlite-shm|db-wal|db-shm|wal|sql|dump|bson|mdb|accdb|ibd|frm|myd|myi|pem|key|keyx|p12|pfx|p7b|p7c|p7s|p8|jks|keystore|crt|cer|der|asc|gpg|snk|mobileprovision|tfstate|tfstate\.backup)$') { return $true }
    return $false
}

function Assert-HaiInstallerSourceFileSafe {
    param(
        [Parameter(Mandatory = $true)][string]$RepositoryRoot,
        [Parameter(Mandatory = $true)][string]$RelativePath
    )

    $root = [IO.Path]::GetFullPath($RepositoryRoot).TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar)
    $normalized = $RelativePath -replace '\\', '/'
    if ([string]::IsNullOrWhiteSpace($normalized) -or [IO.Path]::IsPathRooted($normalized) -or
        $normalized -match '(^|/)\.\.?(/|$)' -or $normalized.Contains(':')) {
        throw 'Installer source path is not a normalized repository-relative path.'
    }
    $candidate = [IO.Path]::GetFullPath((Join-Path $root ($normalized -replace '/', [IO.Path]::DirectorySeparatorChar)))
    $rootPrefix = $root + [IO.Path]::DirectorySeparatorChar
    if (-not $candidate.StartsWith($rootPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Installer source path resolves outside the repository checkout.'
    }

    $current = $root
    foreach ($segment in $normalized.Split('/')) {
        $current = Join-Path $current $segment
        if (-not (Test-Path -LiteralPath $current)) { throw "Selected installer source does not exist: $normalized" }
        $item = Get-Item -LiteralPath $current -Force -ErrorAction Stop
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Installer source paths may not contain symbolic links or reparse points: $normalized"
        }
    }
    if (-not (Test-Path -LiteralPath $candidate -PathType Leaf)) {
        throw "Selected installer source is not a regular file: $normalized"
    }
    return $candidate
}

function Get-HaiInstallerFileProvenance {
    param(
        [Parameter(Mandatory = $true)][string]$PayloadRoot,
        [Parameter(Mandatory = $true)][AllowEmptyCollection()][string[]]$RelativePaths
    )

    $records = New-Object 'System.Collections.Generic.List[object]'
    foreach ($relativePath in $RelativePaths) {
        $path = Assert-HaiInstallerSourceFileSafe -RepositoryRoot $PayloadRoot -RelativePath $relativePath
        $item = Get-Item -LiteralPath $path -Force -ErrorAction Stop
        $records.Add([ordered]@{
            path = ($relativePath -replace '\\', '/')
            sizeBytes = [long]$item.Length
            sha256 = (Get-FileHash -LiteralPath $path -Algorithm SHA256 -ErrorAction Stop).Hash
        })
    }
    return @($records.ToArray())
}

function Format-HaiInstallerCompilerFailure {
    param(
        [Parameter(Mandatory = $true)][int]$ExitCode,
        [AllowEmptyCollection()][string[]]$Output = @(),
        [int]$MaximumLines = 20
    )

    $maximum = [Math]::Max(1, [Math]::Min(100, $MaximumLines))
    $lines = @($Output | ForEach-Object { ([string]$_).TrimEnd() } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if ($lines.Count -gt $maximum) { $lines = @($lines | Select-Object -Last $maximum) }
    $summary = "Inno Setup compilation failed with exit code $ExitCode."
    if ($lines.Count -gt 0) { $summary += "`nCompiler output (last $($lines.Count) lines):`n" + ($lines -join "`n") }
    return $summary
}

function Resolve-HaiInstallerSourceSelection {
    param(
        [AllowEmptyCollection()][string[]]$TrackedFiles = @(),
        [AllowEmptyCollection()][string[]]$InstallerUntrackedFiles = @(),
        [AllowEmptyCollection()][string[]]$WorkerUntrackedFiles = @()
    )

    $included = New-Object 'System.Collections.Generic.List[string]'
    $seen = New-Object 'System.Collections.Generic.HashSet[string]' ([StringComparer]::OrdinalIgnoreCase)
    $installerAllowlist = @(
        'installer/windows/Hai-OpenClawMaintenance.ps1',
        'installer/windows/Hai-WindowsExecutable.ps1',
        'installer/windows/Manage-HAI-OpenClawMaintenanceTask.ps1',
        'installer/windows/Run-HAI-OpenClawMaintenance.ps1',
        'scripts/build-windows-installer.ps1',
        'docs/windows-installer.md'
    )
    $workerSourceRoots = @(
        'backend/cmd/hai-openclaw-maintenance/',
        'backend/internal/openclawmaintenance/'
    )

    $candidates = New-Object 'System.Collections.Generic.List[string]'
    foreach ($path in $TrackedFiles) {
        if (-not [string]::IsNullOrWhiteSpace($path)) { $candidates.Add($path) }
    }
    foreach ($path in $InstallerUntrackedFiles) {
        if ([string]::IsNullOrWhiteSpace($path)) { continue }
        $normalized = $path -replace '\\', '/'
        $allowed = $false
        foreach ($allowedPath in $installerAllowlist) {
            if ($normalized.Equals($allowedPath, [StringComparison]::OrdinalIgnoreCase)) {
                $allowed = $true
                break
            }
        }
        if ($allowed) { $candidates.Add($normalized) }
    }
    foreach ($path in $WorkerUntrackedFiles) {
        if ([string]::IsNullOrWhiteSpace($path)) { continue }
        $normalized = $path -replace '\\', '/'
        if ([IO.Path]::GetExtension($normalized) -ine '.go') { continue }
        $underWorkerSourceRoot = $false
        foreach ($prefix in $workerSourceRoots) {
            if ($normalized.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) { $underWorkerSourceRoot = $true; break }
        }
        if ($underWorkerSourceRoot) { $candidates.Add($normalized) }
    }

    foreach ($path in $candidates) {
        $normalized = $path -replace '\\', '/'
        if (Test-HaiInstallerExcludedPath -ExcludeRelativePath $normalized) { continue }
        if ($seen.Add($normalized)) { $included.Add($normalized) }
    }

    return @($included.ToArray())
}

function Get-HaiInstallerSourceFiles {
    param([Parameter(Mandatory = $true)][string]$RepositoryRoot)

    $trackedFiles = @(& git -C $RepositoryRoot ls-files --cached)
    if ($LASTEXITCODE -ne 0) { throw 'Could not enumerate tracked Git files for the installer payload.' }

    $installerUntrackedFiles = @(& git -C $RepositoryRoot ls-files --others --exclude-standard -- `
        installer/windows `
        scripts/build-windows-installer.ps1 `
        docs/windows-installer.md)
    if ($LASTEXITCODE -ne 0) { throw 'Could not enumerate allowlisted installer source files.' }

    $workerUntrackedFiles = @(& git -C $RepositoryRoot ls-files --others --exclude-standard -- `
        backend/cmd/hai-openclaw-maintenance `
        backend/internal/openclawmaintenance)
    if ($LASTEXITCODE -ne 0) { throw 'Could not enumerate OpenClaw maintenance worker source files.' }

    return Resolve-HaiInstallerSourceSelection `
        -TrackedFiles $trackedFiles `
        -InstallerUntrackedFiles $installerUntrackedFiles `
        -WorkerUntrackedFiles $workerUntrackedFiles
}
