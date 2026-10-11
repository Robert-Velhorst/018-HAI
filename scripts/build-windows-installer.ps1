[CmdletBinding()]
param(
    [string]$Version = "",
    [string]$OutputDirectory = "",
    [string]$ISCCPath = "",
    [switch]$SkipCompile
)

$ErrorActionPreference = "Stop"
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$installerRoot = Join-Path $repositoryRoot "installer"
$releaseRoot = Join-Path $installerRoot "release"
$payloadRoot = Join-Path $releaseRoot "payload"
$installerScript = Join-Path $installerRoot "windows\HAI.iss"

if (-not (Test-Path -LiteralPath (Join-Path $repositoryRoot ".git") -PathType Any)) {
    throw "The Windows installer must be built from a Git checkout so it cannot accidentally package local data."
}
if (-not (Test-Path -LiteralPath $installerScript -PathType Leaf)) {
    throw "Missing Inno Setup script: $installerScript"
}
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = (& git -C $repositoryRoot rev-parse --short HEAD).Trim()
}
if ($Version -notmatch '^[0-9A-Za-z][0-9A-Za-z._-]{0,63}$') {
    throw "Version must contain only letters, numbers, dots, underscores, or hyphens."
}
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = $releaseRoot
}
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

function Test-HaiInstallerExcludedPath {
    param([Parameter(Mandatory = $true)][string]$ExcludeRelativePath)

    if ($ExcludeRelativePath -ne ".env.example" -and $ExcludeRelativePath -match '(^|/)\.env') { return $true }
    if ($ExcludeRelativePath -match '(^|/)(db_data_automation|db_data_idp|images|backups|connected-sources|agent-workspaces|mini-swe-workspaces)(/|$)') { return $true }
    if ($ExcludeRelativePath -match '(^|/)(node_modules|dist|\.npm-cache|\.playwright-cli|\.playwright-mcp|\.verify|\.pytest_cache)(/|$)') { return $true }
    if ($ExcludeRelativePath -match '(^|/)(installer/release|\.git)(/|$)') { return $true }
    if ($ExcludeRelativePath -match '\.(zip|tar\.gz|pdf|bak)$') { return $true }
    return $false
}

if (Test-Path -LiteralPath $payloadRoot) {
    Remove-Item -LiteralPath $payloadRoot -Recurse -Force
}
New-Item -ItemType Directory -Path $payloadRoot -Force | Out-Null

$trackedFiles = @(& git -C $repositoryRoot ls-files --cached)
$installerSourceFiles = @(& git -C $repositoryRoot ls-files --others --exclude-standard -- `
    installer/windows `
    scripts/build-windows-installer.ps1 `
    docs/windows-installer.md)
$sourceFiles = @($trackedFiles + $installerSourceFiles | Select-Object -Unique)
if ($LASTEXITCODE -ne 0 -or $sourceFiles.Count -eq 0) {
    throw "Could not enumerate product files from the Git checkout."
}

$included = New-Object System.Collections.Generic.List[string]
foreach ($relativePath in $sourceFiles) {
    $normalizedPath = $relativePath.Replace("\\", "/")
    if (Test-HaiInstallerExcludedPath -ExcludeRelativePath $normalizedPath) {
        continue
    }
    $source = Join-Path $repositoryRoot $relativePath
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
        throw "Tracked installer file is missing from the checkout: $relativePath"
    }
    $destination = Join-Path $payloadRoot $relativePath
    $destinationDirectory = Split-Path -Parent $destination
    New-Item -ItemType Directory -Path $destinationDirectory -Force | Out-Null
    Copy-Item -LiteralPath $source -Destination $destination -Force
    $included.Add($normalizedPath)
}

$manifest = [ordered]@{
    formatVersion = 1
    version = $Version
    commit = (& git -C $repositoryRoot rev-parse HEAD).Trim()
    generatedAtUtc = [DateTimeOffset]::UtcNow.ToString("o")
    fileCount = $included.Count
    files = $included
}
$manifest | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $releaseRoot "payload-manifest.json") -Encoding utf8

if ($SkipCompile) {
    Write-Host "Prepared installer payload with $($included.Count) source files at $payloadRoot"
    return
}

if ([string]::IsNullOrWhiteSpace($ISCCPath)) {
    $candidates = @(
        "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
        "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
        "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe"
    ) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    $ISCCPath = $candidates | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
    if ([string]::IsNullOrWhiteSpace($ISCCPath)) {
        $command = Get-Command ISCC.exe -ErrorAction SilentlyContinue
        if ($command) { $ISCCPath = $command.Source }
    }
}
if ([string]::IsNullOrWhiteSpace($ISCCPath) -or -not (Test-Path -LiteralPath $ISCCPath -PathType Leaf)) {
    throw "Inno Setup 6 is required to create Setup.exe. Install it with: winget install --id JRSoftware.InnoSetup -e"
}

$env:HAI_INSTALLER_VERSION = $Version
$env:HAI_INSTALLER_OUTPUT_DIR = $OutputDirectory
& $ISCCPath $installerScript
if ($LASTEXITCODE -ne 0) {
    throw "Inno Setup compilation failed with exit code $LASTEXITCODE."
}

$installerPath = Join-Path $OutputDirectory "HAI-Setup-$Version.exe"
if (-not (Test-Path -LiteralPath $installerPath -PathType Leaf)) {
    throw "Inno Setup did not produce the expected installer: $installerPath"
}
Write-Host "Created Windows installer: $installerPath" -ForegroundColor Green
