[CmdletBinding()]
param(
    [string]$Version = "",
    [string]$OutputDirectory = "",
    [string]$ISCCPath = "",
    [switch]$SkipCompile,
    # Deliberately opt-in for developer-only payload experiments. A release
    # build must otherwise be reproducible from one committed Git revision.
    [switch]$AllowDirtyWorktree,
    # Without this switch the output is a developer preview, not a signed release.
    [switch]$Production,
    [string]$SignToolPath = "",
    [string]$SigningCertificateThumbprint = "",
    [string]$TimestampServer = "",
    # Internal Inno callback. Signing configuration is inherited, not logged in
    # the compiler command or written into the source/payload.
    [string]$InnoSigningTarget = ""
)

$ErrorActionPreference = "Stop"

# Pin the Docker fallback to the official multi-platform index digest. Keep the
# tag for human-readable provenance, and select the builder platform explicitly.
$goBuilderImage = 'golang:1.25.13@sha256:cbff9d1a9041b316010f2da6b701b6c0d597718cb90928c85eb597334a0d23d4'

function Resolve-HaiInstallerSigningPolicy {
    param(
        [switch]$Production,
        [switch]$AllowDirtyWorktree,
        [switch]$SkipCompile,
        [string]$SignToolPath,
        [string]$SigningCertificateThumbprint,
        [string]$TimestampServer
    )

    if (-not $Production) {
        if (-not [string]::IsNullOrWhiteSpace($SignToolPath) -or
            -not [string]::IsNullOrWhiteSpace($SigningCertificateThumbprint) -or
            -not [string]::IsNullOrWhiteSpace($TimestampServer)) {
            throw 'Signing options require -Production; refusing to silently create an unsigned preview.'
        }
        return $null
    }
    if ($AllowDirtyWorktree -or $SkipCompile) {
        throw 'Production signing forbids -AllowDirtyWorktree and -SkipCompile.'
    }
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT -or
        -not (Get-Command Get-AuthenticodeSignature -ErrorAction SilentlyContinue)) {
        throw 'Production signing requires Windows Authenticode verification.'
    }
    $thumbprint = $SigningCertificateThumbprint.Trim().ToUpperInvariant()
    if ($thumbprint -notmatch '^[0-9A-F]{40}$') {
        throw 'Production signing requires an explicit 40-hex signing certificate thumbprint.'
    }
    if ([string]::IsNullOrWhiteSpace($SignToolPath) -or
        -not [IO.Path]::IsPathRooted($SignToolPath) -or
        $SignToolPath -notmatch '^(?:[A-Za-z]:[\\/]|[\\/]{2}[^\\/]+[\\/][^\\/]+[\\/])' -or
        [IO.Path]::GetExtension($SignToolPath) -ine '.exe' -or
        -not (Test-Path -LiteralPath $SignToolPath -PathType Leaf)) {
        throw 'Production signing requires an explicit absolute path to an existing SignTool executable.'
    }
    $timestampUri = $null
    if (-not [Uri]::TryCreate($TimestampServer, [UriKind]::Absolute, [ref]$timestampUri) -or
        $timestampUri.Scheme -ne 'https' -or [string]::IsNullOrWhiteSpace($timestampUri.Host) -or
        $timestampUri.UserInfo -ne '' -or $timestampUri.Fragment -ne '') {
        throw 'Production signing requires an operator-approved HTTPS RFC 3161 timestamp URL without credentials or a fragment.'
    }

    # Use only an explicitly provisioned CurrentUser certificate. Never select a
    # publisher by display name, import a key, or alter certificate trust here.
    $certificate = Get-Item -LiteralPath "Cert:\CurrentUser\My\$thumbprint" -ErrorAction Stop
    $now = [DateTime]::Now
    $codeSigningUsage = @($certificate.EnhancedKeyUsageList | Where-Object { $_.ObjectId -eq '1.3.6.1.5.5.7.3.3' })
    if ($certificate.Thumbprint -ine $thumbprint -or -not $certificate.HasPrivateKey -or
        $certificate.NotBefore -gt $now -or $certificate.NotAfter -le $now -or $codeSigningUsage.Count -eq 0) {
        throw 'The pinned signing certificate must be current, have a private key, and explicitly permit code signing.'
    }
    return [pscustomobject]@{
        SignToolPath = [IO.Path]::GetFullPath($SignToolPath)
        Thumbprint = $thumbprint
        TimestampServer = $timestampUri.AbsoluteUri
    }
}

function Invoke-HaiInstallerSignTool {
    param([string]$SignToolPath, [string[]]$Arguments)

    try {
        & $SignToolPath @Arguments 2>&1 | Out-Null
    } catch {
        throw 'SignTool could not execute; production signing output is not accepted.'
    }
    if ($LASTEXITCODE -ne 0) {
        throw "SignTool $($Arguments[0]) failed or returned warnings (exit code $LASTEXITCODE)."
    }
}

function Assert-HaiInstallerAuthenticode {
    param([string]$Path, [Parameter(Mandatory = $true)]$Policy)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Missing production signing output: $Path"
    }
    $signature = Get-AuthenticodeSignature -LiteralPath $Path -ErrorAction Stop
    if ($signature.Status -ne 'Valid' -or $signature.SignatureType -ne 'Authenticode' -or
        $null -eq $signature.SignerCertificate -or
        $signature.SignerCertificate.Thumbprint -ine $Policy.Thumbprint -or
        $null -eq $signature.TimeStamperCertificate) {
        throw "Production Authenticode verification failed (status $($signature.Status)); an embedded, valid, timestamped signature from the pinned certificate is required: $Path"
    }
    # SignTool verifies embedded signatures with Authenticode policy rather than
    # accepting a catalog signature. Treat warnings as failures as well.
    Invoke-HaiInstallerSignTool -SignToolPath $Policy.SignToolPath -Arguments @('verify', '/pa', '/all', '/tw', $Path)
    return [pscustomobject]@{
        sha256 = (Get-FileHash -LiteralPath $Path -Algorithm SHA256 -ErrorAction Stop).Hash
        signerThumbprint = $signature.SignerCertificate.Thumbprint
        timestampSignerThumbprint = $signature.TimeStamperCertificate.Thumbprint
    }
}

function Protect-HaiInstallerProductionFile {
    param([string]$Path, [Parameter(Mandatory = $true)]$Policy)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Missing production signing input: $Path"
    }
    Invoke-HaiInstallerSignTool -SignToolPath $Policy.SignToolPath -Arguments @(
        'sign', '/s', 'My', '/sha1', $Policy.Thumbprint,
        '/fd', 'SHA256', '/tr', $Policy.TimestampServer, '/td', 'SHA256', $Path
    )
    return Assert-HaiInstallerAuthenticode -Path $Path -Policy $Policy
}

function ConvertTo-HaiInnoQuotedArgument {
    param([Parameter(Mandatory = $true)][string]$Value)

    if ($Value.IndexOfAny([char[]]@([char]0, [char]10, [char]13, [char]34)) -ge 0 -or
        $Value.EndsWith('\')) {
        throw 'Unsupported character in Inno signing callback argument.'
    }
    # Inno expands $q/$f/$$ itself; $f already includes filename quotes.
    return '$q' + $Value.Replace('$', '$$') + '$q'
}

function Get-HaiInnoSigningCommand {
    param([string]$PowerShellPath, [string]$BuildScriptPath)

    return '/Shai=' + (ConvertTo-HaiInnoQuotedArgument $PowerShellPath) +
        ' -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ' +
        (ConvertTo-HaiInnoQuotedArgument $BuildScriptPath) + ' -InnoSigningTarget $f'
}

function Copy-HaiInstallerEvidenceBytes {
    param([string]$Source, [string]$Destination)
    [IO.File]::Copy($Source, $Destination, $false)
}

function Invoke-HaiInnoSigningCallback {
    param([string]$Path)

    if ($env:HAI_INSTALLER_PRODUCTION_SIGNING -ne '1' -or
        [string]::IsNullOrWhiteSpace($env:HAI_INSTALLER_SIGNED_UNINSTALLER_DIR) -or
        [string]::IsNullOrWhiteSpace($env:HAI_INSTALLER_OUTPUT_DIR)) {
        throw 'Inno signing callback requires an active production compilation.'
    }
    $target = [IO.Path]::GetFullPath($Path)
    $parent = [IO.Path]::GetDirectoryName($target)
    $uninstallerDirectory = [IO.Path]::GetFullPath($env:HAI_INSTALLER_SIGNED_UNINSTALLER_DIR)
    $setupDirectory = [IO.Path]::GetFullPath($env:HAI_INSTALLER_OUTPUT_DIR)
    if ($Path -notmatch '^(?:[A-Za-z]:[\\/]|[\\/]{2}[^\\/]+[\\/][^\\/]+[\\/])' -or
        ($parent -ine $uninstallerDirectory -and $parent -ine $setupDirectory)) {
        throw 'Inno signing callback target is outside this production compilation.'
    }
    $policy = Resolve-HaiInstallerSigningPolicy -Production `
        -SignToolPath $env:HAI_INSTALLER_SIGNTOOL_PATH `
        -SigningCertificateThumbprint $env:HAI_INSTALLER_SIGNING_THUMBPRINT `
        -TimestampServer $env:HAI_INSTALLER_TIMESTAMP_SERVER
    $signature = Protect-HaiInstallerProductionFile -Path $target -Policy $policy
    if ($parent -ieq $uninstallerDirectory) {
        # Inno signs uninst.e32.tmp, embeds its bytes, then removes the temporary
        # file. Retain the verified bytes inside this fresh build before return.
        $retainedPath = Join-Path $uninstallerDirectory 'HAI-signed-uninstaller.exe'
        Copy-HaiInstallerEvidenceBytes -Source $target -Destination $retainedPath
        $retainedSignature = Assert-HaiInstallerAuthenticode -Path $retainedPath -Policy $policy
        if ($retainedSignature.sha256 -ne $signature.sha256) {
            throw 'Generated uninstaller evidence differs from the compiler signing target.'
        }
    }
}

function Assert-HaiInnoSignedUninstaller {
    param([string]$Directory, [Parameter(Mandatory = $true)]$Policy)

    if (-not (Test-Path -LiteralPath $Directory -PathType Container)) {
        throw 'Missing production signed-uninstaller directory.'
    }
    # A fresh, exclusive directory cannot reuse Inno's signed-uninstaller cache.
    # Check the exact sole compiler output, not a guessed installed unins000.exe.
    $outputs = @(Get-ChildItem -LiteralPath $Directory -Force -ErrorAction Stop)
    if ($outputs.Count -ne 1 -or $outputs[0].PSIsContainer -or
        ($outputs[0].Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        [IO.Path]::GetDirectoryName($outputs[0].FullName) -ine [IO.Path]::GetFullPath($Directory)) {
        throw 'Expected exactly one regular generated production uninstaller; output is not accepted.'
    }
    $signature = Assert-HaiInstallerAuthenticode -Path $outputs[0].FullName -Policy $Policy
    return [pscustomobject]@{ Path = $outputs[0].FullName; Signature = $signature }
}

if ($PSBoundParameters.ContainsKey('InnoSigningTarget')) {
    try {
        Invoke-HaiInnoSigningCallback -Path $InnoSigningTarget
        exit 0
    } catch {
        # Inno logs callback output. Never print provider/certificate paths or
        # exception details from a failed signing process.
        Write-Error 'Production Inno signing callback failed; compiler output is not accepted.' -ErrorAction Continue
        exit 1
    }
}

$signingPolicy = Resolve-HaiInstallerSigningPolicy -Production:$Production -AllowDirtyWorktree:$AllowDirtyWorktree `
    -SkipCompile:$SkipCompile -SignToolPath $SignToolPath -SigningCertificateThumbprint $SigningCertificateThumbprint `
    -TimestampServer $TimestampServer
if (-not $Production) {
    Write-Warning 'Developer preview only: production Authenticode signing is not enforced. Do not publish this output as a production release.'
}

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$installerRoot = Join-Path $repositoryRoot "installer"
$releaseRoot = Join-Path $installerRoot "release"
$payloadRoot = Join-Path $releaseRoot "payload"
$installerScript = Join-Path $installerRoot "windows\HAI.iss"
. (Join-Path $PSScriptRoot "windows-installer-payload-selection.ps1")
. (Join-Path $repositoryRoot "installer\windows\Hai-WindowsExecutable.ps1")

if (-not (Test-Path -LiteralPath (Join-Path $repositoryRoot ".git") -PathType Any)) {
    throw "The Windows installer must be built from a Git checkout so it cannot accidentally package local data."
}
$sourceCommit = @(& git -C $repositoryRoot rev-parse --verify 'HEAD^{commit}')
if ($LASTEXITCODE -ne 0 -or $sourceCommit.Count -ne 1 -or $sourceCommit[0] -notmatch '^[0-9a-fA-F]{40,64}$') {
    throw 'Could not resolve a full Git commit for installer build provenance.'
}
$sourceCommit = $sourceCommit[0].Trim().ToLowerInvariant()
$dirtyPaths = @(& git -C $repositoryRoot status --porcelain=v1 --untracked-files=all)
if ($LASTEXITCODE -ne 0) {
    throw 'Could not determine Git worktree state for installer build provenance.'
}
if (-not $AllowDirtyWorktree -and $dirtyPaths.Count -gt 0) {
    throw 'Refusing to build a release installer from a dirty worktree. Commit or stash the changes first, or use -AllowDirtyWorktree only for a non-release developer payload.'
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
$installerPath = Join-Path $OutputDirectory "HAI-Setup-$Version.exe"
if ($Production -and (Test-Path -LiteralPath $installerPath)) {
    throw 'Production output already exists; choose a new version or output directory. Existing installers will not be overwritten.'
}
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

# Resolve the compiler before replacing the generated payload. This preserves a
# previously prepared release when a machine is missing the build prerequisite.
if (-not $SkipCompile -and [string]::IsNullOrWhiteSpace($ISCCPath)) {
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
if (-not $SkipCompile -and ([string]::IsNullOrWhiteSpace($ISCCPath) -or -not (Test-Path -LiteralPath $ISCCPath -PathType Leaf))) {
    throw "Inno Setup 6 is required to create Setup.exe. Install it with: winget install --id JRSoftware.InnoSetup -e"
}

$sourceFiles = @(Get-HaiInstallerSourceFiles -RepositoryRoot $repositoryRoot)
if ($LASTEXITCODE -ne 0 -or $sourceFiles.Count -eq 0) {
    throw "Could not enumerate product files from the Git checkout."
}
$requiredPayloadSources = @(
    ".env.example",
    "docker-compose.local.yml",
    "installer/windows/HAI.iss",
    "installer/windows/Hai-WindowsExecutable.ps1",
    "installer/windows/Hai-InstallerSupport.ps1",
    "installer/windows/Start-HAI.ps1",
    "installer/windows/Stop-HAI.ps1",
    "scripts/initialize-windows.ps1",
    "backend/cmd/hai-dsh-bridge/main.go",
    "backend/cmd/hai-openclaw-maintenance/main.go",
    "backend/internal/openclawmaintenance/service.go",
    "installer/windows/Manage-HAI-OpenClawMaintenanceTask.ps1"
)
foreach ($requiredPath in $requiredPayloadSources) {
    if ($requiredPath -notin $sourceFiles) {
        throw "The installer source selection is incomplete; required payload source is missing: $requiredPath"
    }
}
foreach ($relativePath in $sourceFiles) {
    $null = Assert-HaiInstallerSourceFileSafe -RepositoryRoot $repositoryRoot -RelativePath $relativePath
}

New-Item -ItemType Directory -Path $releaseRoot -Force | Out-Null
$buildId = [Guid]::NewGuid().ToString("N")
$stagingRoot = Join-Path $releaseRoot (".payload-$buildId.staging")
$payloadBackupRoot = Join-Path $releaseRoot (".payload-$buildId.backup")
$manifestPath = Join-Path $releaseRoot "payload-manifest.json"
$manifestStagingPath = Join-Path $releaseRoot (".payload-manifest-$buildId.tmp")
$manifestBackupPath = Join-Path $releaseRoot (".payload-manifest-$buildId.backup")
New-Item -ItemType Directory -Path $stagingRoot -ErrorAction Stop | Out-Null

$included = New-Object System.Collections.Generic.List[string]
$workerSignatures = [ordered]@{}
$workerBuildProvenance = [ordered]@{ method = 'not-built'; target = $null; tool = $null }
try {
    foreach ($relativePath in $sourceFiles) {
        $normalizedPath = $relativePath -replace '\\', '/'
        $source = Assert-HaiInstallerSourceFileSafe -RepositoryRoot $repositoryRoot -RelativePath $relativePath
        $destination = Join-Path $stagingRoot $relativePath
        $destinationDirectory = Split-Path -Parent $destination
        New-Item -ItemType Directory -Path $destinationDirectory -Force | Out-Null
        Copy-Item -LiteralPath $source -Destination $destination -Force
        $included.Add($normalizedPath)
    }

    if (-not $SkipCompile) {
    $workers = @(
        @{ Binary = 'hai-dsh-bridge.exe'; Package = './cmd/hai-dsh-bridge' },
        @{ Binary = 'hai-openclaw-maintenance.exe'; Package = './cmd/hai-openclaw-maintenance' }
    )
    $go = Get-Command go -ErrorAction SilentlyContinue
    if ($go) {
        $goVersionOutput = @(& $go.Source version 2>&1)
        if ($LASTEXITCODE -ne 0 -or $goVersionOutput.Count -eq 0) {
            throw 'Could not determine the Go compiler version for installer provenance.'
        }
        $workerBuildProvenance = [ordered]@{
            method = 'go'
            target = 'windows/amd64, CGO_ENABLED=0, -trimpath'
            tool = ($goVersionOutput -join ' ').Trim()
        }
        Push-Location (Join-Path $stagingRoot "backend")
        try {
            $previousCGO = $env:CGO_ENABLED
            $previousGOOS = $env:GOOS
            $previousGOARCH = $env:GOARCH
            $env:CGO_ENABLED = "0"
            $env:GOOS = "windows"
            $env:GOARCH = "amd64"
            foreach ($worker in $workers) {
                $output = Join-Path $stagingRoot "installer\windows\$($worker.Binary)"
                & $go.Source build -trimpath -o $output $worker.Package
                if ($LASTEXITCODE -ne 0) { throw "Could not build bundled worker $($worker.Binary)." }
            }
        } finally {
            $env:CGO_ENABLED = $previousCGO
            $env:GOOS = $previousGOOS
            $env:GOARCH = $previousGOARCH
            Pop-Location
        }
    } else {
        if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
            throw "A Go toolchain or Docker Desktop is required to build the bundled HAI host runtime worker."
        }
        $dockerServer = & docker version --format '{{.Server.Version}}' 2>$null
        if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($dockerServer)) {
            throw "Docker Desktop is installed but its Linux engine is not ready; it is required when Go is unavailable."
        }
        $workerBuildProvenance = [ordered]@{
            method = 'docker'
            target = 'windows/amd64, CGO_ENABLED=0, -trimpath'
            tool = $goBuilderImage
        }
        & docker run --rm `
            --platform linux/amd64 `
            -v "${stagingRoot}:/src" `
            -w /src/backend `
            $goBuilderImage `
            sh -c 'CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o /src/installer/windows/hai-dsh-bridge.exe ./cmd/hai-dsh-bridge && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o /src/installer/windows/hai-openclaw-maintenance.exe ./cmd/hai-openclaw-maintenance'
    }
    if ($LASTEXITCODE -ne 0) { throw 'Could not build the bundled HAI workers.' }
    foreach ($worker in $workers) {
        if (-not (Test-Path -LiteralPath (Join-Path $stagingRoot "installer\windows\$($worker.Binary)") -PathType Leaf)) {
            throw "Missing bundled worker $($worker.Binary)."
        }
        Test-HaiWindowsExecutablePayload -Path (Join-Path $stagingRoot "installer\windows\$($worker.Binary)") | Out-Null
        if ($Production) {
            $workerSignatures[$worker.Binary] = Protect-HaiInstallerProductionFile `
                -Path (Join-Path $stagingRoot "installer\windows\$($worker.Binary)") -Policy $signingPolicy
        }
        $included.Add("installer/windows/$($worker.Binary)")
    }
    }

    $manifest = [ordered]@{
        formatVersion = 1
        version = $Version
        commit = $sourceCommit
        worktree = [ordered]@{
            clean = ($dirtyPaths.Count -eq 0)
            dirtyPaths = @($dirtyPaths)
        }
        generatedAtUtc = [DateTimeOffset]::UtcNow.ToString("o")
        fileCount = $included.Count
        files = $included
        fileIntegrity = @(Get-HaiInstallerFileProvenance -PayloadRoot $stagingRoot -RelativePaths @($included.ToArray()))
        workerBuild = $workerBuildProvenance
        releaseChannel = $(if ($Production) { 'production' } else { 'developer-preview' })
        signing = [ordered]@{
            required = [bool]$Production
            signerThumbprint = $(if ($Production) { $signingPolicy.Thumbprint } else { $null })
            workers = $workerSignatures
        }
    }
    $manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $manifestStagingPath -Encoding utf8

    $payloadMovedToBackup = $false
    $manifestMovedToBackup = $false
    $payloadPromotionAttempted = $false
    $manifestPromotionAttempted = $false
    try {
        if (Test-Path -LiteralPath $payloadRoot -PathType Container) {
            Move-Item -LiteralPath $payloadRoot -Destination $payloadBackupRoot -ErrorAction Stop
            $payloadMovedToBackup = $true
        }
        if (Test-Path -LiteralPath $manifestPath -PathType Leaf) {
            Move-Item -LiteralPath $manifestPath -Destination $manifestBackupPath -ErrorAction Stop
            $manifestMovedToBackup = $true
        }
        $payloadPromotionAttempted = $true
        Move-Item -LiteralPath $stagingRoot -Destination $payloadRoot -ErrorAction Stop
        $manifestPromotionAttempted = $true
        Move-Item -LiteralPath $manifestStagingPath -Destination $manifestPath -ErrorAction Stop
    } catch {
        $promotionFailure = $_.Exception.Message
        $rollbackFailures = New-Object 'System.Collections.Generic.List[string]'

        if ($payloadMovedToBackup) {
            try {
                if (Test-Path -LiteralPath $payloadRoot -PathType Container) {
                    Remove-Item -LiteralPath $payloadRoot -Recurse -Force -ErrorAction Stop
                }
                if (-not (Test-Path -LiteralPath $payloadBackupRoot -PathType Container)) {
                    throw 'The prior payload backup is missing.'
                }
                Move-Item -LiteralPath $payloadBackupRoot -Destination $payloadRoot -ErrorAction Stop
            } catch {
                $rollbackFailures.Add("payload restore failed: $($_.Exception.Message)")
            }
        } elseif ($payloadPromotionAttempted -and (Test-Path -LiteralPath $payloadRoot -PathType Container)) {
            try {
                Remove-Item -LiteralPath $payloadRoot -Recurse -Force -ErrorAction Stop
            } catch {
                $rollbackFailures.Add("partial payload cleanup failed: $($_.Exception.Message)")
            }
        }

        if ($manifestMovedToBackup) {
            try {
                if (Test-Path -LiteralPath $manifestPath -PathType Leaf) {
                    Remove-Item -LiteralPath $manifestPath -Force -ErrorAction Stop
                }
                if (-not (Test-Path -LiteralPath $manifestBackupPath -PathType Leaf)) {
                    throw 'The prior manifest backup is missing.'
                }
                Move-Item -LiteralPath $manifestBackupPath -Destination $manifestPath -ErrorAction Stop
            } catch {
                $rollbackFailures.Add("manifest restore failed: $($_.Exception.Message)")
            }
        } elseif ($manifestPromotionAttempted -and (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
            try {
                Remove-Item -LiteralPath $manifestPath -Force -ErrorAction Stop
            } catch {
                $rollbackFailures.Add("partial manifest cleanup failed: $($_.Exception.Message)")
            }
        }

        if ($rollbackFailures.Count -gt 0) {
            throw "Installer payload promotion failed: $promotionFailure Rollback was incomplete: $($rollbackFailures -join '; '). Any remaining backup was retained for manual recovery."
        }
        throw "Installer payload promotion failed and the previous payload was restored: $promotionFailure"
    }

    foreach ($backupPath in @($payloadBackupRoot, $manifestBackupPath)) {
        if (Test-Path -LiteralPath $backupPath) {
            Remove-Item -LiteralPath $backupPath -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
} finally {
    if (Test-Path -LiteralPath $stagingRoot -PathType Container) {
        Remove-Item -LiteralPath $stagingRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $manifestStagingPath -PathType Leaf) {
        Remove-Item -LiteralPath $manifestStagingPath -Force -ErrorAction SilentlyContinue
    }
}

if ($SkipCompile) {
    Write-Host "Prepared installer payload with $($included.Count) source files at $payloadRoot"
    return
}

$compileOutputDirectory = $OutputDirectory
$compilerArguments = @()
$signedUninstallerDirectory = ''
if ($Production) {
    # A unique directory prevents stale Setup reuse and retains rejected output
    # for diagnosis without replacing any previously accepted installer.
    $compileOutputDirectory = Join-Path $OutputDirectory (".production-$buildId.staging")
    New-Item -ItemType Directory -Path $compileOutputDirectory -ErrorAction Stop | Out-Null
    $signedUninstallerDirectory = Join-Path $compileOutputDirectory 'signed-uninstaller'
    New-Item -ItemType Directory -Path $signedUninstallerDirectory -ErrorAction Stop | Out-Null
    $callbackPowerShell = Join-Path ([Environment]::GetFolderPath('System')) 'WindowsPowerShell\v1.0\powershell.exe'
    if (-not (Test-Path -LiteralPath $callbackPowerShell -PathType Leaf)) {
        throw 'Windows PowerShell is required for the production Inno signing callback.'
    }
    $compilerArguments += Get-HaiInnoSigningCommand -PowerShellPath $callbackPowerShell `
        -BuildScriptPath (Join-Path $repositoryRoot 'scripts\build-windows-installer.ps1')
}
$previousInstallerVersion = $env:HAI_INSTALLER_VERSION
$previousInstallerOutputDirectory = $env:HAI_INSTALLER_OUTPUT_DIR
$signingEnvironmentNames = @(
    'HAI_INSTALLER_PRODUCTION_SIGNING', 'HAI_INSTALLER_SIGNED_UNINSTALLER_DIR',
    'HAI_INSTALLER_SIGNTOOL_PATH', 'HAI_INSTALLER_SIGNING_THUMBPRINT', 'HAI_INSTALLER_TIMESTAMP_SERVER'
)
$previousSigningEnvironment = @{}
foreach ($name in $signingEnvironmentNames) {
    $previousSigningEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
try {
    $env:HAI_INSTALLER_VERSION = $Version
    $env:HAI_INSTALLER_OUTPUT_DIR = $compileOutputDirectory
    # Clear inherited release settings even for previews; ambient environment
    # must not turn a developer compilation into a signing operation.
    foreach ($name in $signingEnvironmentNames) {
        [Environment]::SetEnvironmentVariable($name, $null, 'Process')
    }
    $compilerOutput = @()
    $compileExitCode = -1
    try {
    if ($Production) {
        $env:HAI_INSTALLER_PRODUCTION_SIGNING = '1'
        $env:HAI_INSTALLER_SIGNED_UNINSTALLER_DIR = $signedUninstallerDirectory
        $env:HAI_INSTALLER_SIGNTOOL_PATH = $signingPolicy.SignToolPath
        $env:HAI_INSTALLER_SIGNING_THUMBPRINT = $signingPolicy.Thumbprint
        $env:HAI_INSTALLER_TIMESTAMP_SERVER = $signingPolicy.TimestampServer
        & $ISCCPath @compilerArguments $installerScript 2>&1 | Out-Null
    } else {
        $compilerOutput = @(& $ISCCPath $installerScript 2>&1 | ForEach-Object { [string]$_ })
    }
    $compileExitCode = $LASTEXITCODE
    } catch {
        throw 'Inno Setup compiler could not execute; compiler output is withheld.'
    }
    if ($compileExitCode -ne 0) {
        if ($Production) {
            throw "Inno Setup production compilation failed (exit code $compileExitCode); compiler output is withheld."
        }
        throw "Inno Setup preview compilation failed (exit code $compileExitCode)."
    }
    if (-not $Production) {
        foreach ($line in $compilerOutput) { Write-Output $line }
    }

    $compiledInstallerPath = Join-Path $compileOutputDirectory "HAI-Setup-$Version.exe"
    if (-not (Test-Path -LiteralPath $compiledInstallerPath -PathType Leaf)) {
        throw "Inno Setup did not produce the expected installer: $compiledInstallerPath"
    }
    if ($Production) {
        $uninstallerSignature = Assert-HaiInnoSignedUninstaller -Directory $signedUninstallerDirectory -Policy $signingPolicy
        $installerSignature = Protect-HaiInstallerProductionFile -Path $compiledInstallerPath -Policy $signingPolicy
        if ((Get-FileHash -LiteralPath $uninstallerSignature.Path -Algorithm SHA256 -ErrorAction Stop).Hash -ne $uninstallerSignature.Signature.sha256) {
            throw 'Production uninstaller bytes changed after signature verification; output is not accepted.'
        }
        # File.Move has no overwrite mode: a concurrent output collision fails closed.
        [IO.File]::Move($compiledInstallerPath, $installerPath)
        if ((Get-FileHash -LiteralPath $installerPath -Algorithm SHA256 -ErrorAction Stop).Hash -ne $installerSignature.sha256) {
            throw 'Production installer bytes changed after signature verification; output is not accepted.'
        }
    }
} finally {
    $env:HAI_INSTALLER_VERSION = $previousInstallerVersion
    $env:HAI_INSTALLER_OUTPUT_DIR = $previousInstallerOutputDirectory
    foreach ($name in $signingEnvironmentNames) {
        [Environment]::SetEnvironmentVariable($name, $previousSigningEnvironment[$name], 'Process')
    }
}
Write-Host "Created Windows installer: $installerPath" -ForegroundColor Green
