[CmdletBinding()]
param(
    [string]$EnvFile = "",
    [string]$AdminEmail = "",
    [string]$AdminPasswordPlainText = "",
    [ValidateRange(1, 65535)]
    [int]$GatewayPort = 8088,
    [ValidatePattern('^[a-z0-9][a-z0-9_-]*$')]
    [string]$ComposeProjectName = "018-hai",
    [switch]$EnableA2ABridge,
    [switch]$Force,
    [switch]$MigrateExisting
)

$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$examplePath = Join-Path $repositoryRoot ".env.example"
if (-not (Test-Path -LiteralPath $examplePath -PathType Leaf)) {
    throw "Missing environment template: $examplePath"
}

if ([string]::IsNullOrWhiteSpace($EnvFile)) {
    $EnvFile = Join-Path $repositoryRoot ".env.local"
} elseif (-not [System.IO.Path]::IsPathRooted($EnvFile)) {
    $EnvFile = Join-Path $repositoryRoot $EnvFile
}
$EnvFile = [System.IO.Path]::GetFullPath($EnvFile)

if (-not $MigrateExisting) {
    if ((Test-Path -LiteralPath $EnvFile) -and -not $Force) {
        throw "$EnvFile already exists. Re-run with -Force only when replacing it is intentional."
    }

    if ([string]::IsNullOrWhiteSpace($AdminEmail)) {
        $AdminEmail = Read-Host "First-run owner email"
    }
    $AdminEmail = $AdminEmail.Trim()
    if ($AdminEmail -notmatch '^[^\s@]+@[^\s@]+\.[^\s@]+$') {
        throw "AdminEmail must be a valid email address."
    }

    if ([string]::IsNullOrEmpty($AdminPasswordPlainText)) {
        $securePassword = Read-Host "First-run owner password (at least 12 characters)" -AsSecureString
        $passwordPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($securePassword)
        try {
            $AdminPasswordPlainText = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($passwordPointer)
        } finally {
            [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($passwordPointer)
        }
    }
    if ($AdminPasswordPlainText.Length -lt 12) {
        throw "The first-run owner password must contain at least 12 characters."
    }
    if ($AdminPasswordPlainText -match "[\r\n']") {
        throw "The first-run owner password cannot contain a line break or single quote."
    }
}

function New-HaiSecret {
    $bytes = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    return ([BitConverter]::ToString($bytes)).Replace("-", "").ToLowerInvariant()
}

function Test-HaiCredentialFreeA2ABridgeUrl {
    param([AllowEmptyString()][string]$Url)

    if ([string]::IsNullOrWhiteSpace($Url) -or $Url -ne $Url.Trim() -or
        $Url -match '[\r\n\\?#]') {
        return $false
    }
    $parsedUrl = $null
    if (-not [Uri]::TryCreate($Url, [UriKind]::Absolute, [ref]$parsedUrl) -or
        $parsedUrl.Scheme -notin @('http', 'https') -or
        [string]::IsNullOrEmpty($parsedUrl.Host) -or
        -not [string]::IsNullOrEmpty($parsedUrl.UserInfo) -or
        -not [string]::IsNullOrEmpty($parsedUrl.Query) -or
        -not [string]::IsNullOrEmpty($parsedUrl.Fragment)) {
        return $false
    }
    try {
        $decodedPath = [Uri]::UnescapeDataString($parsedUrl.AbsolutePath)
    } catch {
        return $false
    }
    return -not ($decodedPath -match '[?#]' -or
        $decodedPath -match '(?i)(?:^|/)(?:access[_-]?token|token|api[_-]?key|apikey|secret|password|credential|authorization|auth)(?:/|=|;|$)' -or
        $decodedPath -match '(?i)(?:^|/)[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}(?:/|$)')
}

function Set-DotEnvValue {
    param(
        [Parameter(Mandatory = $true)][string]$Content,
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][AllowEmptyString()][string]$Value
    )
    $pattern = "(?m)^" + [Regex]::Escape($Name) + "=.*$"
    if (-not [Regex]::IsMatch($Content, $pattern)) {
        throw "The environment template does not define $Name."
    }
    $replacement = [Text.RegularExpressions.MatchEvaluator]{
        param($match)
        return "$Name=$Value"
    }
    return [Regex]::Replace($Content, $pattern, $replacement)
}

function Get-HaiDotEnvEntry {
    param(
        [Parameter(Mandatory = $true)][AllowEmptyCollection()][AllowEmptyString()][string[]]$Lines,
        [Parameter(Mandatory = $true)][string]$Name
    )

    $escapedName = [Regex]::Escape($Name)
    $assignmentPattern = '^' + $escapedName + '=(?<value>.*)$'
    $ambiguousPattern = '^\s*' + $escapedName + '\s*='
    $found = $null
    for ($index = 0; $index -lt $Lines.Count; $index++) {
        $match = [Regex]::Match($Lines[$index], $assignmentPattern)
        if (-not $match.Success) {
            if ([Regex]::IsMatch($Lines[$index], $ambiguousPattern)) {
                throw "The existing environment file has an ambiguous $Name setting. Correct that setting before starting HAI."
            }
            continue
        }

        if ($null -ne $found) {
            throw "The existing environment file has duplicate $Name settings. Correct the duplicates before starting HAI."
        }

        $rawValue = $match.Groups['value'].Value.Trim()
        $value = $rawValue
        $wellFormed = $true
        if ($rawValue.StartsWith('"') -or $rawValue.StartsWith("'")) {
            $quote = $rawValue.Substring(0, 1)
            if ($rawValue.Length -lt 2 -or -not $rawValue.EndsWith($quote)) {
                $wellFormed = $false
            } else {
                $value = $rawValue.Substring(1, $rawValue.Length - 2)
            }
        } elseif ($rawValue.EndsWith('"') -or $rawValue.EndsWith("'")) {
            $wellFormed = $false
        }

        $found = [pscustomobject]@{
            Index = $index
            Value = $value
            WellFormed = $wellFormed
        }
    }

    if ($null -eq $found) {
        return [pscustomobject]@{ Found = $false; Index = -1; Value = ''; WellFormed = $true }
    }
    return [pscustomobject]@{ Found = $true; Index = $found.Index; Value = $found.Value; WellFormed = $found.WellFormed }
}

function Get-HaiSavedA2AConfiguration {
    param([Parameter(Mandatory = $true)][string]$Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return $null
    }

    $lines = [Regex]::Split([IO.File]::ReadAllText($Path), "\r\n|\n|\r")
    $enabled = Get-HaiDotEnvEntry -Lines $lines -Name 'HAI_A2A_BRIDGE_ENABLED'
    if (-not $enabled.Found) {
        return $null
    }
    if (-not $enabled.WellFormed -or ($enabled.Value -ine 'true' -and $enabled.Value -ine 'false')) {
        throw 'The existing HAI_A2A_BRIDGE_ENABLED value is invalid; refusing to replace saved A2A settings without an explicit choice.'
    }
    if ($enabled.Value -ieq 'false') {
        return [pscustomobject]@{ Enabled = $false }
    }

    $owner = Get-HaiDotEnvEntry -Lines $lines -Name 'HAI_A2A_BRIDGE_OWNER_ID'
    $token = Get-HaiDotEnvEntry -Lines $lines -Name 'HAI_A2A_BRIDGE_TOKEN'
    $localPort = Get-HaiDotEnvEntry -Lines $lines -Name 'HAI_A2A_LOCAL_PORT'
    $bridgeUrl = Get-HaiDotEnvEntry -Lines $lines -Name 'HAI_A2A_BRIDGE_URL'
    $publicBridge = Get-HaiDotEnvEntry -Lines $lines -Name 'HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED'

    $port = 0
    $parsedUrl = $null
    $validToken = $token.Found -and $token.WellFormed -and $token.Value.Length -ge 32 -and $token.Value -notmatch '[\r\n]'
    $validPort = $localPort.Found -and $localPort.WellFormed -and [int]::TryParse($localPort.Value, [ref]$port) -and $port -ge 1 -and $port -le 65535
    $validUrl = $bridgeUrl.Found -and $bridgeUrl.WellFormed -and
        (Test-HaiCredentialFreeA2ABridgeUrl -Url $bridgeUrl.Value)
    $validPublicBridge = -not $publicBridge.Found -or ($publicBridge.WellFormed -and ($publicBridge.Value -ieq 'true' -or $publicBridge.Value -ieq 'false'))
    if (-not $owner.Found -or -not $owner.WellFormed -or [string]::IsNullOrWhiteSpace($owner.Value) -or
        -not $validToken -or -not $validPort -or -not $validUrl -or -not $validPublicBridge) {
        throw 'The saved A2A bridge is enabled but its owner, token, port, or URL is invalid; refusing to silently disable it during replacement.'
    }

    return [pscustomobject]@{
        Enabled = $true
        OwnerId = $owner.Value
        Token = $token.Value
        LocalPort = $port
        BridgeUrl = $bridgeUrl.Value
        PublicBridgeEnabled = if ($publicBridge.Found) { $publicBridge.Value.ToLowerInvariant() } else { 'false' }
    }
}

function Test-HaiMaintenanceToken {
    param(
        [AllowEmptyString()][string]$Token,
        [Parameter(Mandatory = $true)][AllowEmptyCollection()][string[]]$OtherSecrets
    )

    if ($Token -notmatch '\A[A-Za-z0-9_+/=.-]{32,}\z') {
        return $false
    }
    foreach ($otherSecret in $OtherSecrets) {
        if ([string]::Equals($Token, $otherSecret, [StringComparison]::Ordinal)) {
            return $false
        }
    }
    return $true
}

function Get-HaiEnvironmentMigrationMutexName {
    param([Parameter(Mandatory = $true)][string]$Path)

    $canonicalPath = [IO.Path]::GetFullPath($Path).TrimEnd([IO.Path]::DirectorySeparatorChar).ToUpperInvariant()
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        $digest = $sha256.ComputeHash([Text.Encoding]::UTF8.GetBytes($canonicalPath))
    } finally {
        $sha256.Dispose()
    }
    $pathHash = [BitConverter]::ToString($digest).Replace('-', '')
    return "Global\HAI.EnvironmentMigration.$pathHash"
}

function Invoke-HaiExistingEnvironmentMigrationLocked {
    param([Parameter(Mandatory = $true)][string]$Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw 'The existing HAI environment file was not found; refusing to run an existing-install migration.'
    }

    $bytes = [IO.File]::ReadAllBytes($Path)
    $hasUtf8Bom = $bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF
    $offset = if ($hasUtf8Bom) { 3 } else { 0 }
    $utf8 = New-Object Text.UTF8Encoding($false, $true)
    try {
        $content = $utf8.GetString($bytes, $offset, $bytes.Length - $offset)
    } catch {
        throw 'The existing HAI environment file is not valid UTF-8; refusing to rewrite it.'
    }

    $newlineMatch = [Regex]::Match($content, "\r\n|\n|\r")
    $newline = if ($newlineMatch.Success) { $newlineMatch.Value } else { [Environment]::NewLine }
    $hasTrailingNewline = [Regex]::IsMatch($content, "(?:\r\n|\n|\r)\z")
    $lines = New-Object 'System.Collections.Generic.List[string]'
    if ($content.Length -gt 0) {
        foreach ($line in [Regex]::Split($content, "\r\n|\n|\r")) {
            $lines.Add($line)
        }
        if ($hasTrailingNewline) {
            $lines.RemoveAt($lines.Count - 1)
        }
    }

    $enabled = Get-HaiDotEnvEntry -Lines $lines.ToArray() -Name 'HAI_OPENCLAW_MAINTENANCE_ENABLED'
    if (-not $enabled.Found) {
        $lines.Add('HAI_OPENCLAW_MAINTENANCE_ENABLED=true')
        $maintenanceEnabled = $true
        $changed = $true
    } elseif (-not $enabled.WellFormed) {
        throw 'The existing HAI_OPENCLAW_MAINTENANCE_ENABLED value is malformed; refusing to change it.'
    } elseif ($enabled.Value -ieq 'false') {
        Write-Host 'The existing explicit OpenClaw maintenance opt-out was preserved.'
        return
    } elseif ($enabled.Value -ieq 'true') {
        $maintenanceEnabled = $true
        $changed = $false
    } else {
        throw 'The existing HAI_OPENCLAW_MAINTENANCE_ENABLED value must be true or false; refusing to change it.'
    }

    if ($maintenanceEnabled) {
        $token = Get-HaiDotEnvEntry -Lines $lines.ToArray() -Name 'HAI_OPENCLAW_MAINTENANCE_TOKEN'
        $otherSecretNames = @(
            'BACKEND_API_SHARED_KEY',
            'HAI_HOST_RUNTIME_BRIDGE_TOKEN',
            'OPENCLAW_GATEWAY_TOKEN',
            'OPENCLAW_GATEWAY_DELEGATION_TOKEN'
        )
        $otherSecrets = New-Object 'System.Collections.Generic.List[string]'
        foreach ($name in $otherSecretNames) {
            $entry = Get-HaiDotEnvEntry -Lines $lines.ToArray() -Name $name
            if ($entry.Found) {
                if (-not $entry.WellFormed) {
                    throw "The existing $name value is malformed; refusing to validate the maintenance token."
                }
                if (-not [string]::IsNullOrEmpty($entry.Value)) {
                    $otherSecrets.Add($entry.Value)
                }
            }
        }

        if (-not $token.Found -or -not $token.WellFormed -or
            -not (Test-HaiMaintenanceToken -Token $token.Value -OtherSecrets $otherSecrets.ToArray())) {
            do {
                $newToken = New-HaiSecret
            } while (-not (Test-HaiMaintenanceToken -Token $newToken -OtherSecrets $otherSecrets.ToArray()))

            if ($token.Found) {
                $lines[$token.Index] = 'HAI_OPENCLAW_MAINTENANCE_TOKEN=' + $newToken
            } else {
                $lines.Add('HAI_OPENCLAW_MAINTENANCE_TOKEN=' + $newToken)
            }
            $changed = $true
        }
    }

    if (-not $changed) {
        Write-Host 'The existing OpenClaw maintenance configuration is already current.'
        return
    }

    $updatedContent = [string]::Join($newline, $lines.ToArray())
    if ($hasTrailingNewline -and $lines.Count -gt 0) {
        $updatedContent += $newline
    }
    $updatedBytes = $utf8.GetBytes($updatedContent)
    if ($hasUtf8Bom) {
        $withBom = New-Object byte[] ($updatedBytes.Length + 3)
        $withBom[0] = 0xEF
        $withBom[1] = 0xBB
        $withBom[2] = 0xBF
        [Array]::Copy($updatedBytes, 0, $withBom, 3, $updatedBytes.Length)
        $updatedBytes = $withBom
    }

    $temporaryPath = Join-Path ([IO.Path]::GetDirectoryName($Path)) ('.hai-maintenance-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    $backupPath = Join-Path ([IO.Path]::GetDirectoryName($Path)) ('.hai-maintenance-backup-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    $rollbackPath = Join-Path ([IO.Path]::GetDirectoryName($Path)) ('.hai-maintenance-rollback-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    $migrationFailure = $null
    $temporaryFilesRemoved = $true
    $preserveBackup = $false
    $preserveRollbackData = $false
    try {
        $installerSupport = Join-Path $repositoryRoot 'installer\windows\Hai-InstallerSupport.ps1'
        . $installerSupport
        $environmentAcl = Get-Acl -LiteralPath $Path
        $expectedDacl = Get-HaiComparableFileAccessDescriptor -FileSecurity $environmentAcl
        Write-HaiAclProtectedFile -Path $temporaryPath -Bytes $updatedBytes -FileSecurity $environmentAcl
        [IO.File]::Replace($temporaryPath, $Path, $backupPath)
        try {
            # File.Replace can materialize the replacement with a different
            # inherited DACL on some Windows/filesystem combinations. Restore
            # and verify the original policy before discarding rollback data.
            Set-Acl -LiteralPath $Path -AclObject $environmentAcl
            $actualDacl = Get-HaiComparableFileAccessDescriptor -FileSecurity (Get-Acl -LiteralPath $Path)
            $actualBytes = [IO.File]::ReadAllBytes($Path)
            if (-not [string]::Equals($expectedDacl, $actualDacl, [StringComparison]::Ordinal) -or
                -not [string]::Equals([Convert]::ToBase64String($updatedBytes), [Convert]::ToBase64String($actualBytes), [StringComparison]::Ordinal)) {
                throw 'The migrated environment file did not retain the original DACL and complete updated contents.'
            }
        } catch {
            $verificationFailure = $_.Exception.GetBaseException()
            if (Test-Path -LiteralPath $backupPath -PathType Leaf) {
                try {
                    Write-HaiAclProtectedFile -Path $temporaryPath -Bytes $bytes -FileSecurity $environmentAcl
                    [IO.File]::Replace($temporaryPath, $Path, $rollbackPath)
                    $restoredDacl = Get-HaiComparableFileAccessDescriptor -FileSecurity (Get-Acl -LiteralPath $Path)
                    $restoredBytes = [IO.File]::ReadAllBytes($Path)
                    if (-not [string]::Equals($expectedDacl, $restoredDacl, [StringComparison]::Ordinal) -or
                        -not [string]::Equals([Convert]::ToBase64String($bytes), [Convert]::ToBase64String($restoredBytes), [StringComparison]::Ordinal)) {
                        throw 'The original environment file could not be verified after rollback.'
                    }
                } catch {
                    $preserveBackup = Test-Path -LiteralPath $backupPath -PathType Leaf
                    $preserveRollbackData = $preserveBackup -or (Test-Path -LiteralPath $rollbackPath -PathType Leaf)
                    $rollbackMessage = $_.Exception.GetBaseException().Message
                    throw "Migration verification failed and automatic rollback could not be verified. $rollbackMessage"
                }
            }
            throw "Migration verification failed; the original environment was restored. $($verificationFailure.Message)"
        }
    } catch {
        $failure = $_.Exception.GetBaseException()
        $failureType = $failure.GetType().Name
        $failureMessage = $failure.Message.Replace($Path, '[environment file]').Replace($temporaryPath, '[temporary file]').Replace($backupPath, '[temporary backup]')
        $migrationFailure = "The existing HAI environment could not be updated safely ($failureType); no maintenance task was started. $failureMessage"
    } finally {
        foreach ($temporaryFile in @($temporaryPath)) {
            if (-not (Remove-HaiSensitiveTemporaryFile -Path $temporaryFile)) { $temporaryFilesRemoved = $false }
        }
        if (-not $preserveBackup -and -not (Remove-HaiSensitiveTemporaryFile -Path $backupPath)) { $temporaryFilesRemoved = $false }
        if (-not $preserveRollbackData -and -not (Remove-HaiSensitiveTemporaryFile -Path $rollbackPath)) { $temporaryFilesRemoved = $false }
    }
    if ($null -ne $migrationFailure) {
        if ($preserveBackup) { $migrationFailure += " The original recovery file was preserved at '$backupPath'; do not remove it until the environment is recovered." }
        if ($preserveRollbackData -and (Test-Path -LiteralPath $rollbackPath -PathType Leaf)) { $migrationFailure += " The prior target file was preserved at '$rollbackPath'." }
        if (-not $temporaryFilesRemoved) { $migrationFailure += ' A sensitive temporary file may remain and must be removed before continuing.' }
        throw $migrationFailure
    }
    if (-not $temporaryFilesRemoved) {
        throw 'The HAI environment was updated, but a sensitive temporary file could not be removed. Remove the named file before continuing.'
    }

    Write-Host 'The existing HAI environment was safely updated for verified OpenClaw maintenance.'
}

function Invoke-HaiExistingEnvironmentMigration {
    param([Parameter(Mandatory = $true)][string]$Path)

    $mutexName = Get-HaiEnvironmentMigrationMutexName -Path $Path
    $mutex = New-Object Threading.Mutex($false, $mutexName)
    $locked = $false
    try {
        try {
            $locked = $mutex.WaitOne([TimeSpan]::FromSeconds(30))
        } catch [Threading.AbandonedMutexException] {
            # The previous owner exited mid-migration. Atomic replacement means
            # the file is either the old version or the complete new version.
            $locked = $true
        }
        if (-not $locked) {
            throw 'Timed out waiting for another HAI startup to finish environment migration; no file changes were made.'
        }

        Invoke-HaiExistingEnvironmentMigrationLocked -Path $Path
    } finally {
        if ($locked) {
            $mutex.ReleaseMutex()
        }
        $mutex.Dispose()
    }
}

if ($MigrateExisting) {
    if ($Force) {
        throw "-Force cannot be combined with -MigrateExisting."
    }
    Invoke-HaiExistingEnvironmentMigration -Path $EnvFile
    return
}

$savedA2AConfiguration = $null
if (-not ($PSBoundParameters.ContainsKey('EnableA2ABridge') -and -not $EnableA2ABridge)) {
    $savedA2AConfiguration = Get-HaiSavedA2AConfiguration -Path $EnvFile
}
$savedNetworkSubnets = @{
    HAI_A2A_LOCAL_SUBNET = '10.255.0.0/24'
    HAI_HOST_RUNTIME_INTERNAL_SUBNET = '10.255.1.0/24'
}
if (Test-Path -LiteralPath $EnvFile -PathType Leaf) {
    $existingEnvironmentLines = [Regex]::Split([IO.File]::ReadAllText($EnvFile), "\r\n|\n|\r")
    foreach ($settingName in @('HAI_A2A_LOCAL_SUBNET', 'HAI_HOST_RUNTIME_INTERNAL_SUBNET')) {
        $savedSetting = Get-HaiDotEnvEntry -Lines $existingEnvironmentLines -Name $settingName
        if ($savedSetting.Found) {
            if (-not $savedSetting.WellFormed -or [string]::IsNullOrWhiteSpace($savedSetting.Value)) {
                throw "The existing $settingName value is invalid; refusing to reset the configured Docker network range."
            }
            $savedNetworkSubnets[$settingName] = $savedSetting.Value
        }
    }
}

function Assert-HaiComposeProjectAvailable {
    param(
        [Parameter(Mandatory = $true)][string]$ProjectName,
        [Parameter(Mandatory = $true)][string]$ExpectedWorkingDirectory
    )

    if ($null -eq (Get-Command docker -ErrorAction SilentlyContinue)) {
        throw "Docker is unavailable, so existing HAI data volumes cannot be checked. Refusing to generate replacement credentials."
    }

    $projectVolumes = @(& docker volume ls --quiet --filter "label=com.docker.compose.project=$ProjectName" 2>$null)
    if ($LASTEXITCODE -ne 0) {
        throw "Could not verify existing data volumes for Docker project '$ProjectName'; refusing to initialize replacement credentials."
    }
    $allVolumes = @(& docker volume ls --quiet 2>$null)
    if ($LASTEXITCODE -ne 0) {
        throw "Could not verify fixed-name HAI data volumes; refusing to initialize replacement credentials."
    }
    $existingVolumes = @(@($projectVolumes) + @($allVolumes | Where-Object { $_ -match '^018-hai-' }) |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Sort-Object -Unique)
    if ($existingVolumes.Count -gt 0) {
        throw "Existing HAI data volumes were found ('$($existingVolumes -join ', ')'). Restore the matching protected hai.env or explicitly migrate the existing installation before generating any credentials. No volumes were changed."
    }

    $containerIds = @(& docker ps -aq --filter "label=com.docker.compose.project=$ProjectName" 2>$null)
    if ($LASTEXITCODE -ne 0) {
        throw "Could not verify ownership of Docker project '$ProjectName'; refusing to initialize a competing HAI installation."
    }
    $containerIds = @($containerIds | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if ($containerIds.Count -eq 0) {
        return
    }

    foreach ($containerId in $containerIds) {
        $workingDirectory = & docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' $containerId 2>$null
        if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($workingDirectory)) {
            throw "Could not verify the owner path for HAI container '$containerId'; refusing to initialize an unknown Docker project."
        }

        if (-not [string]::Equals(
            [IO.Path]::GetFullPath($workingDirectory),
            [IO.Path]::GetFullPath($ExpectedWorkingDirectory),
            [StringComparison]::OrdinalIgnoreCase
        )) {
            throw "Another HAI installation already owns Docker project '$ProjectName' at '$workingDirectory'. Stop it first or rerun with a different -ComposeProjectName."
        }
    }
}

$content = [IO.File]::ReadAllText($examplePath)
$content = Set-DotEnvValue $content 'HAI_A2A_LOCAL_SUBNET' $savedNetworkSubnets.HAI_A2A_LOCAL_SUBNET
$content = Set-DotEnvValue $content 'HAI_HOST_RUNTIME_INTERNAL_SUBNET' $savedNetworkSubnets.HAI_HOST_RUNTIME_INTERNAL_SUBNET
Assert-HaiComposeProjectAvailable -ProjectName $ComposeProjectName -ExpectedWorkingDirectory $repositoryRoot
$content = Set-DotEnvValue $content "COMPOSE_PROJECT_NAME" $ComposeProjectName
$content = Set-DotEnvValue $content "BACKEND_API_SHARED_KEY" (New-HaiSecret)
$maintenanceEnabled = Get-HaiDotEnvEntry -Lines ([Regex]::Split($content, "\r\n|\n|\r")) -Name 'HAI_OPENCLAW_MAINTENANCE_ENABLED'
if (-not $maintenanceEnabled.Found -or -not $maintenanceEnabled.WellFormed -or
    ($maintenanceEnabled.Value -ine 'true' -and $maintenanceEnabled.Value -ine 'false')) {
    throw 'The environment template must define HAI_OPENCLAW_MAINTENANCE_ENABLED as true or false.'
}
if ($maintenanceEnabled.Value -ieq 'true') {
    $content = Set-DotEnvValue $content "HAI_OPENCLAW_MAINTENANCE_TOKEN" (New-HaiSecret)
}
$content = Set-DotEnvValue $content "LOCAL_PREVIEW_GATEWAY_SECRET" (New-HaiSecret)
$content = Set-DotEnvValue $content "HAI_MEMORY_ENCRYPTION_KEY" (New-HaiSecret)
$content = Set-DotEnvValue $content "JWT_SECRET" (New-HaiSecret)
$content = Set-DotEnvValue $content "HAI_APPROVAL_PROOF_SIGNING_KEY" (New-HaiSecret)
$databasePassword = New-HaiSecret
$content = Set-DotEnvValue $content "DB_PASSWORD" $databasePassword
# Compose applies migrations with DB_USER, then creates this DML-only API role
# before backend starts. Keep the runtime password independent from the schema
# owner secret even on a local first run.
$content = Set-DotEnvValue $content "BACKEND_DB_USER" "hai_runtime"
$content = Set-DotEnvValue $content "BACKEND_DB_PASSWORD" (New-HaiSecret)
$content = Set-DotEnvValue $content "DB_MIGRATIONS_ENABLED" "false"
$content = Set-DotEnvValue $content "FIRST_RUN_ADMIN_EMAIL" $AdminEmail
$content = Set-DotEnvValue $content "FIRST_RUN_ADMIN_PASSWORD" ("'" + $AdminPasswordPlainText + "'")
$a2aLocalPort = 8091
$a2aBridgeUrl = "http://127.0.0.1:$a2aLocalPort/api/v1/a2a"
$a2aBridgeEnabled = $false
$a2aBridgeOwnerId = ''
$a2aBridgeToken = ''
$a2aPublicBridgeEnabled = 'false'
$preserveSavedA2AConfiguration = $null -ne $savedA2AConfiguration -and $savedA2AConfiguration.Enabled -and
    (-not $PSBoundParameters.ContainsKey('EnableA2ABridge') -or $EnableA2ABridge)
if ($EnableA2ABridge -and -not $preserveSavedA2AConfiguration) {
    $a2aBridgeEnabled = $true
    $a2aBridgeOwnerId = $AdminEmail
    $a2aBridgeToken = New-HaiSecret
} elseif ($preserveSavedA2AConfiguration) {
    $a2aBridgeEnabled = $true
    $a2aBridgeOwnerId = $savedA2AConfiguration.OwnerId
    $a2aBridgeToken = $savedA2AConfiguration.Token
    $a2aLocalPort = $savedA2AConfiguration.LocalPort
    $a2aBridgeUrl = $savedA2AConfiguration.BridgeUrl
    $a2aPublicBridgeEnabled = $savedA2AConfiguration.PublicBridgeEnabled
}
$content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_ENABLED" $a2aBridgeEnabled.ToString().ToLowerInvariant()
$content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_OWNER_ID" $a2aBridgeOwnerId
$content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_TOKEN" $a2aBridgeToken
$content = Set-DotEnvValue $content "HAI_A2A_LOCAL_PORT" $a2aLocalPort.ToString()
$content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_URL" $a2aBridgeUrl
$content = Set-DotEnvValue $content "HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED" $a2aPublicBridgeEnabled
$content = Set-DotEnvValue $content "GATEWAY_HOST_PORT" $GatewayPort.ToString()
$content = Set-DotEnvValue $content "GATEWAY_HOST_BIND" "127.0.0.1"
$content = Set-DotEnvValue $content "LOCAL_LOGIN_BYPASS_ENABLED" "false"
$content = Set-DotEnvValue $content "RUN_MODE" "production"

$parent = Split-Path -Parent $EnvFile
if (-not (Test-Path -LiteralPath $parent -PathType Container)) {
    New-Item -ItemType Directory -Path $parent -Force | Out-Null
}
$installerSupport = Join-Path $repositoryRoot 'installer\windows\Hai-InstallerSupport.ps1'
. $installerSupport
$temporaryPath = Join-Path $parent ('.hai-env-' + [Guid]::NewGuid().ToString('N') + '.tmp')
$targetExists = Test-Path -LiteralPath $EnvFile -PathType Leaf
if ($targetExists -and $MigrateExisting) {
    $environmentAcl = Get-Acl -LiteralPath $EnvFile
} else {
    $environmentAcl = New-HaiRestrictedEnvironmentFileSecurity
    if ($targetExists) {
        # ReplaceFile may preserve the destination ACL. Tighten and verify it
        # before replacement so the new secrets never inherit broad access.
        Set-HaiExistingFileAccessRules -Path $EnvFile -DesiredSecurity $environmentAcl
    }
}
$utf8WithoutBom = New-Object Text.UTF8Encoding($false)
$environmentBytes = $utf8WithoutBom.GetBytes($content)
$backupPath = Join-Path $parent ('.hai-env-backup-' + [Guid]::NewGuid().ToString('N') + '.tmp')
$initializationFailure = $null
$temporaryFilesRemoved = $true
try {
    Write-HaiAclProtectedFile -Path $temporaryPath -Bytes $environmentBytes -FileSecurity $environmentAcl
    if ($targetExists) {
        [IO.File]::Replace($temporaryPath, $EnvFile, $backupPath)
        if (Test-Path -LiteralPath $backupPath -PathType Leaf) {
            [IO.File]::Delete($backupPath)
        }
    } else {
        [IO.File]::Move($temporaryPath, $EnvFile)
    }
} catch {
    $initializationFailure = $_.Exception
} finally {
    foreach ($temporaryFile in @($temporaryPath, $backupPath)) {
        if (-not (Remove-HaiSensitiveTemporaryFile -Path $temporaryFile)) { $temporaryFilesRemoved = $false }
    }
}
if ($null -ne $initializationFailure) {
    $type = $initializationFailure.GetType().Name
    if (-not $temporaryFilesRemoved) { throw "Could not safely create the HAI environment file ($type); a sensitive temporary file may remain. Remove the named file before retrying." }
    throw "Could not safely create the HAI environment file ($type). No credentials were printed."
}
if (-not $temporaryFilesRemoved) {
    throw 'The HAI environment was created, but a sensitive temporary file could not be removed. Remove the named file before continuing.'
}

Write-Host "Created $EnvFile"
Write-Host "Owner: $AdminEmail"
Write-Host "Gateway: http://127.0.0.1:$GatewayPort"
if ($a2aBridgeEnabled) {
    Write-Host "Optional local A2A planning bridge: enabled (endpoint and credentials withheld)."
} else {
    Write-Host "Optional local A2A planning bridge: disabled by default. To opt in on first run, use -EnableA2ABridge; an existing install is controlled by HAI_A2A_BRIDGE_ENABLED in its environment file."
}
Write-Host "Secrets and the owner password were written to the ignored environment file and were not printed."
