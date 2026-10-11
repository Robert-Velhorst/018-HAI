[CmdletBinding()]
param(
    [switch]$ConfigureSilentUpgrade,
    [switch]$PromoteMaintenanceWorker,
    [switch]$StartMaintenanceTask,
    [switch]$StopRuntimeForUninstall,
    [ValidateRange(1, 300)][int]$MaintenanceWorkerWaitSeconds = 60
)

$ErrorActionPreference = "Stop"
$script:HaiOpenClawMaintenanceMutexName = 'Global\HAI.OpenClawMaintenance'

function New-HaiRestrictedEnvironmentFileSecurity {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    if ($null -eq $identity.User) {
        throw 'Could not determine the current Windows account for local environment-file security.'
    }

    $security = New-Object Security.AccessControl.FileSecurity
    $security.SetAccessRuleProtection($true, $false)
    $security.SetOwner($identity.User)
    foreach ($sidValue in @($identity.User.Value, 'S-1-5-18', 'S-1-5-32-544')) {
        $sid = New-Object Security.Principal.SecurityIdentifier($sidValue)
        $rule = New-Object Security.AccessControl.FileSystemAccessRule(
            $sid,
            [Security.AccessControl.FileSystemRights]::FullControl,
            [Security.AccessControl.AccessControlType]::Allow
        )
        $security.SetAccessRule($rule)
    }
    return $security
}

function Get-HaiComparableFileAccessDescriptor {
    param([Parameter(Mandatory = $true)][Security.AccessControl.FileSecurity]$FileSecurity)

    $sddl = $FileSecurity.GetSecurityDescriptorSddlForm([Security.AccessControl.AccessControlSections]::Access)
    $firstAce = $sddl.IndexOf('(', [StringComparison]::Ordinal)
    $prefixLength = if ($firstAce -ge 0) { $firstAce } else { $sddl.Length }
    $prefix = $sddl.Substring(0, $prefixLength)
    if (-not $prefix.StartsWith('D:', [StringComparison]::Ordinal)) {
        throw 'The environment-file security descriptor did not contain a DACL.'
    }

    # Windows can add the auto-inherited control bit while applying an otherwise
    # identical protected DACL. Compare policy and ACEs, not that derived bit.
    return 'D:' + $prefix.Substring(2).Replace('AI', '') + $sddl.Substring($prefixLength)
}

function Set-HaiExistingFileAccessRules {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][Security.AccessControl.FileSecurity]$DesiredSecurity
    )

    # Preserve the existing owner, group, and audit rules. icacls edits only
    # the DACL, avoiding a privileged rewrite of the file's audit descriptor.
    $currentUserSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $desiredSids = @($currentUserSID, 'S-1-5-18', 'S-1-5-32-544')
    $icacls = Join-Path $env:SystemRoot 'System32\icacls.exe'
    if (-not (Test-Path -LiteralPath $icacls -PathType Leaf)) {
        throw 'Windows icacls is unavailable; the existing environment-file ACL was not changed.'
    }

    $arguments = @($Path, '/reset')
    $null = @(& $icacls @arguments 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Windows could not reset the existing environment-file access rules (icacls exit code $LASTEXITCODE); no new secrets were written."
    }

    $arguments = @($Path, '/inheritance:r')
    $null = @(& $icacls @arguments 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Windows could not disable broad environment-file ACL inheritance (icacls exit code $LASTEXITCODE); no new secrets were written."
    }

    $arguments = @($Path, '/grant:r') + @($desiredSids | ForEach-Object { "*${_}:(F)" })
    $null = @(& $icacls @arguments 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "Windows could not grant the restricted environment-file access rules (icacls exit code $LASTEXITCODE); no new secrets were written."
    }

    $expected = Get-HaiComparableFileAccessDescriptor -FileSecurity $DesiredSecurity
    $actual = Get-HaiComparableFileAccessDescriptor -FileSecurity (Get-Acl -LiteralPath $Path)
    if (-not [string]::Equals($expected, $actual, [StringComparison]::Ordinal)) {
        throw 'The existing file access rules did not match the requested restricted policy.'
    }
}

function Write-HaiAclProtectedFile {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][byte[]]$Bytes,
        [Parameter(Mandatory = $true)][Security.AccessControl.FileSecurity]$FileSecurity,
        [scriptblock]$BeforeWrite
    )

    $expectedSddl = Get-HaiComparableFileAccessDescriptor -FileSecurity $FileSecurity
    $stream = New-Object IO.FileStream(
        $Path,
        [IO.FileMode]::CreateNew,
        [IO.FileAccess]::Write,
        [IO.FileShare]::Read,
        4096,
        [IO.FileOptions]::WriteThrough
    )
    try {
        # The file is empty while its inherited ACL is replaced. Keep the handle
        # open without delete sharing to prevent swapping the path before write.
        Set-Acl -LiteralPath $Path -AclObject $FileSecurity
        $actualSecurity = Get-Acl -LiteralPath $Path
        $actualSddl = Get-HaiComparableFileAccessDescriptor -FileSecurity $actualSecurity
        if (-not [string]::Equals($expectedSddl, $actualSddl, [StringComparison]::Ordinal)) {
            throw 'The temporary environment-file ACL did not match the requested ACL before writing.'
        }
        if ($null -ne $BeforeWrite) {
            & $BeforeWrite $Path $stream $actualSecurity
        }
        if ($Bytes.Length -gt 0) {
            $stream.Write($Bytes, 0, $Bytes.Length)
        }
        $stream.Flush($true)
    } finally {
        $stream.Dispose()
    }
}

function Remove-HaiSensitiveTemporaryFile {
    param([Parameter(Mandatory = $true)][string]$Path)

    $fullPath = $Path
    try {
        $fullPath = [IO.Path]::GetFullPath($Path)
        if (-not (Test-Path -LiteralPath $fullPath -PathType Leaf -ErrorAction Stop)) {
            if (Test-Path -LiteralPath $fullPath -ErrorAction Stop) {
                throw 'The sensitive temporary path exists but is not a regular file.'
            }
            return $true
        }
        [IO.File]::Delete($fullPath)
        if (Test-Path -LiteralPath $fullPath -ErrorAction Stop) {
            throw 'The temporary file still exists after deletion.'
        }
        return $true
    } catch {
        throw [IO.IOException]::new(
            "Could not securely remove sensitive temporary file '$fullPath'. It may contain credentials; close any process holding it and remove it before continuing.",
            $_.Exception
        )
    }
}

function Get-HaiInstallRoot {
    return (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..\..")).Path
}

function Test-HaiMaintenanceWorkerRunning {
    param([Parameter(Mandatory = $true)][string]$WorkerPath)

    $targetPath = [IO.Path]::GetFullPath($WorkerPath)
    $processes = @(Get-CimInstance -ClassName Win32_Process -Filter "Name = 'hai-openclaw-maintenance.exe'" -ErrorAction Stop)
    foreach ($process in $processes) {
        if ([string]::IsNullOrWhiteSpace([string]$process.ExecutablePath)) {
            return $true
        }
        if ([string]::Equals([IO.Path]::GetFullPath([string]$process.ExecutablePath), $targetPath, [StringComparison]::OrdinalIgnoreCase)) {
            return $true
        }
    }
    return $false
}

function Promote-HaiMaintenanceWorker {
    param([ValidateRange(1, 300)][int]$WaitSeconds = 60)

    $supportDirectory = Join-Path (Get-HaiInstallRoot) 'installer\windows'
    $workerPath = Join-Path $supportDirectory 'hai-openclaw-maintenance.exe'
    $pendingPath = Join-Path $supportDirectory 'hai-openclaw-maintenance.pending.exe'
    if (-not (Test-Path -LiteralPath $pendingPath -PathType Leaf)) {
        throw 'The staged OpenClaw maintenance worker is missing; the existing worker was not changed.'
    }
    $payloadValidationScript = Join-Path $supportDirectory 'Hai-WindowsExecutable.ps1'
    if (-not (Test-Path -LiteralPath $payloadValidationScript -PathType Leaf)) {
        throw 'OpenClaw maintenance worker validation support is missing; the existing worker was not changed.'
    }
    . $payloadValidationScript
    $pendingHash = Test-HaiWindowsExecutablePayload -Path $pendingPath -PassThruHash

    $mutex = $null
    $locked = $false
    $deadline = [DateTimeOffset]::UtcNow.AddSeconds($WaitSeconds)
    try {
        $mutex = New-Object Threading.Mutex($false, $script:HaiOpenClawMaintenanceMutexName)
        try {
            $remaining = $deadline - [DateTimeOffset]::UtcNow
            if ($remaining -gt [TimeSpan]::Zero) {
                $locked = $mutex.WaitOne($remaining)
            }
        } catch [Threading.AbandonedMutexException] {
            $locked = $true
        }
        if (-not $locked) {
            throw 'OpenClaw maintenance is still active. No worker file was replaced; wait for its current run to finish, then retry the HAI upgrade.'
        }

        do {
            if ([DateTimeOffset]::UtcNow -ge $deadline) {
                throw "The staged OpenClaw maintenance worker could not be promoted within $WaitSeconds seconds. No worker file was replaced; wait for maintenance or installer contention to clear, then retry the HAI upgrade."
            }
            if (-not (Test-HaiMaintenanceWorkerRunning -WorkerPath $workerPath)) {
                # Contention can outlive the initial validation. Bind every
                # replacement attempt to the originally checked payload bytes.
                $currentHash = Test-HaiWindowsExecutablePayload -Path $pendingPath -PassThruHash
                if ($currentHash -cne $pendingHash) {
                    throw 'The staged maintenance worker changed while waiting for promotion. The existing worker was not changed; retry setup with the intended payload.'
                }
                try {
                    if (Test-Path -LiteralPath $workerPath) {
                        $workerItem = Get-Item -LiteralPath $workerPath -Force -ErrorAction Stop
                        if ($workerItem.PSIsContainer -or ($workerItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                            throw 'The installed maintenance worker destination is not a regular file. No worker file was replaced; inspect the installation before retrying setup.'
                        }
                    }
                    if (Test-Path -LiteralPath $workerPath -PathType Leaf) {
                        $backupPath = Join-Path $supportDirectory ('hai-openclaw-maintenance.' + [Guid]::NewGuid().ToString('N') + '.backup')
                        [IO.File]::Replace($pendingPath, $workerPath, $backupPath)
                        if (Test-Path -LiteralPath $backupPath -PathType Leaf) {
                            try {
                                [IO.File]::Delete($backupPath)
                            } catch {
                                Write-Warning 'The maintenance worker was updated, but its temporary recovery copy could not be removed.'
                            }
                        }
                    } else {
                        [IO.File]::Move($pendingPath, $workerPath)
                    }
                    return $true
                } catch [IO.IOException] {
                    $nativeError = $_.Exception.HResult -band 0xFFFF
                    if ($nativeError -notin @(32, 33)) {
                        throw 'The staged OpenClaw maintenance worker could not be installed safely. The existing worker was left in place; check folder permissions and retry the HAI upgrade.'
                    }
                }
            }

            if ([DateTimeOffset]::UtcNow -ge $deadline) {
                throw "The staged OpenClaw maintenance worker could not be promoted within $WaitSeconds seconds. No worker file was replaced; wait for maintenance or installer contention to clear, then retry the HAI upgrade."
            }
            Start-Sleep -Milliseconds 250
        } while ($true)
    } finally {
        if ($locked -and $null -ne $mutex) { $mutex.ReleaseMutex() }
        if ($null -ne $mutex) { $mutex.Dispose() }
    }
}

function Get-HaiDataRoot {
    $dataRoot = Join-Path $env:LOCALAPPDATA "HAI"
    if (-not (Test-Path -LiteralPath $dataRoot -PathType Container)) {
        New-Item -ItemType Directory -Path $dataRoot -Force | Out-Null
    }
    return $dataRoot
}

function Get-HaiEnvironmentFile {
    if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        throw 'The signed-in Windows profile is unavailable; the HAI environment file cannot be identified safely.'
    }

    $profileRoot = [IO.Path]::GetFullPath($env:LOCALAPPDATA)
    return (Join-Path (Join-Path $profileRoot 'HAI') 'hai.env')
}

function Test-HaiEnvironmentFileAvailable {
    $environmentFile = Get-HaiEnvironmentFile
    if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
        if (Test-Path -LiteralPath $environmentFile) {
            throw 'The HAI environment path exists but is not a regular file. Restore the original protected hai.env before starting HAI; no credentials or containers were changed.'
        }
        return $false
    }

    $environmentItem = Get-Item -LiteralPath $environmentFile -Force -ErrorAction Stop
    if (($environmentItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'The HAI environment file is a reparse point; its protected settings cannot be verified safely. Restore the original regular file before starting HAI. No containers or data were changed.'
    }

    $environmentStream = $null
    try {
        $environmentStream = [IO.File]::Open($environmentFile, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
    } catch {
        throw 'The HAI environment file exists but cannot be read. Restore its protected file permissions or recover the original file from backup before starting HAI. No credentials or containers were changed.'
    } finally {
        if ($null -ne $environmentStream) { $environmentStream.Dispose() }
    }
    return $true
}

function Get-HaiEnvironmentValue {
    param([Parameter(Mandatory = $true)][string]$Name)

    $environmentFile = Get-HaiEnvironmentFile
    if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
        throw "HAI is not initialized. Use Start HAI before reading local connector settings."
    }

    $pattern = "(?m)^$([Regex]::Escape($Name))=(?<value>.*)$"
    $match = [Regex]::Match([IO.File]::ReadAllText($environmentFile), $pattern)
    if (-not $match.Success) {
        throw "HAI environment file is missing $Name. Restore the matching protected hai.env backup before starting this existing installation."
    }

    return $match.Groups["value"].Value.Trim().Trim("'").Trim('"')
}

function Assert-HaiRequiredEnvironment {
    $environmentFile = Get-HaiEnvironmentFile
    $lines = [Regex]::Split([IO.File]::ReadAllText($environmentFile), "\r\n|\n|\r")
    $requiredSettings = @(
        'BACKEND_API_SHARED_KEY',
        'JWT_SECRET',
        'HAI_MEMORY_ENCRYPTION_KEY',
        'HAI_APPROVAL_PROOF_SIGNING_KEY',
        'DB_PASSWORD',
        'BACKEND_DB_PASSWORD',
        'FIRST_RUN_ADMIN_EMAIL',
        'FIRST_RUN_ADMIN_PASSWORD'
    )
    foreach ($name in $requiredSettings) {
        $escapedName = [Regex]::Escape($name)
        $matchingLines = @($lines | Where-Object { $_ -match "^$escapedName=" })
        if ($matchingLines.Count -eq 0) {
            throw "HAI cannot start because required setting '$name' is missing. Restore the matching protected hai.env backup; no containers or data were changed."
        }
        if ($matchingLines.Count -ne 1) {
            throw "HAI cannot start because required setting '$name' appears more than once. Correct the protected environment file before starting; no containers or data were changed."
        }

        $rawValue = $matchingLines[0].Substring($name.Length + 1).Trim()
        $value = $rawValue
        if ($rawValue.StartsWith('"') -or $rawValue.StartsWith("'")) {
            $quote = $rawValue.Substring(0, 1)
            if ($rawValue.Length -lt 2 -or -not $rawValue.EndsWith($quote)) {
                throw "HAI cannot start because required setting '$name' has malformed quoting. Correct the protected environment file before starting; no containers or data were changed."
            }
            $value = $rawValue.Substring(1, $rawValue.Length - 2)
        } elseif ($rawValue.EndsWith('"') -or $rawValue.EndsWith("'")) {
            throw "HAI cannot start because required setting '$name' has malformed quoting. Correct the protected environment file before starting; no containers or data were changed."
        }

        if ([string]::IsNullOrWhiteSpace($value)) {
            throw "HAI cannot start because required setting '$name' is missing or empty. Restore the matching protected hai.env backup; no containers or data were changed."
        }
        if ($name -match 'SECRET|KEY|PASSWORD' -and
            $value -match '(?i)(change[-_ ]?me|replace[-_ ]?me|your[-_ ]?(secret|key|password)|placeholder|<[^>]+>)') {
            throw "HAI cannot start because required setting '$name' still contains a template placeholder. Restore the matching protected hai.env backup; no containers or data were changed."
        }
        if ($name -eq 'FIRST_RUN_ADMIN_EMAIL' -and $value -notmatch '^[^\s@]+@[^\s@]+\.[^\s@]+$') {
            throw "HAI cannot start because required setting '$name' is not a valid email address. Correct the protected environment file before starting; no containers or data were changed."
        }
        if ($name -eq 'FIRST_RUN_ADMIN_PASSWORD' -and $value.Length -lt 12) {
            throw "HAI cannot start because required setting '$name' is shorter than 12 characters. Restore the matching protected hai.env backup; no containers or data were changed."
        }
    }
}

function Get-HaiComposeFile {
    $composeFile = Join-Path (Get-HaiInstallRoot) "docker-compose.local.yml"
    if (-not (Test-Path -LiteralPath $composeFile -PathType Leaf)) {
        throw "HAI installation is incomplete: missing $composeFile"
    }
    return $composeFile
}

function Get-HaiConfiguredLocalPort {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][ValidateRange(1, 65535)][int]$DefaultPort
    )

    $environmentFile = Get-HaiEnvironmentFile
    if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
        return $DefaultPort
    }

    $setting = Get-HaiA2AEnvironmentValue -Lines ([IO.File]::ReadAllLines($environmentFile)) -Name $Name
    if (-not $setting.Found) { return $DefaultPort }
    $port = 0
    if (-not $setting.WellFormed -or $setting.Value -notmatch '\A[0-9]{1,5}\z' -or
        -not [int]::TryParse($setting.Value, [ref]$port) -or $port -lt 1 -or $port -gt 65535) {
        throw "HAI environment file contains an invalid $Name port; no default was substituted."
    }
    return $port
}

function Get-HaiGatewayPort {
    return (Get-HaiConfiguredLocalPort -Name 'GATEWAY_HOST_PORT' -DefaultPort 8088)
}

function Get-HaiA2ALocalPort {
    return (Get-HaiConfiguredLocalPort -Name 'HAI_A2A_LOCAL_PORT' -DefaultPort 8091)
}

function Get-HaiUrl {
    return "http://127.0.0.1:$(Get-HaiGatewayPort)"
}

function Get-HaiA2AUrl {
    return "http://127.0.0.1:$(Get-HaiA2ALocalPort)"
}

function Test-HaiA2ABridgeEnabled {
    $environmentFile = Get-HaiEnvironmentFile
    if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
        return $false
    }

    $settingMatches = [Regex]::Matches(
        [IO.File]::ReadAllText($environmentFile),
        '(?m)^[ \t]*HAI_A2A_BRIDGE_ENABLED[ \t]*=[ \t]*(?<value>[^\r\n]*?)[ \t]*\r?$'
    )
    if ($settingMatches.Count -ne 1) {
        return $false
    }

    $value = $settingMatches[0].Groups['value'].Value.Trim()
    if ($value.Length -ge 2 -and
        (($value.StartsWith('"', [StringComparison]::Ordinal) -and $value.EndsWith('"', [StringComparison]::Ordinal)) -or
         ($value.StartsWith("'", [StringComparison]::Ordinal) -and $value.EndsWith("'", [StringComparison]::Ordinal)))) {
        $value = $value.Substring(1, $value.Length - 2).Trim()
    } elseif ($value.StartsWith('"', [StringComparison]::Ordinal) -or
        $value.EndsWith('"', [StringComparison]::Ordinal) -or
        $value.StartsWith("'", [StringComparison]::Ordinal) -or
        $value.EndsWith("'", [StringComparison]::Ordinal)) {
        return $false
    }

    return [string]::Equals($value, 'true', [StringComparison]::OrdinalIgnoreCase)
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
    if ($decodedPath -match '[?#]' -or
        $decodedPath -match '(?i)(?:^|/)(?:access[_-]?token|token|api[_-]?key|apikey|secret|password|credential|authorization|auth)(?:/|=|;|$)' -or
        $decodedPath -match '(?i)(?:^|/)[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}(?:/|$)') {
        return $false
    }
    return $true
}

function Get-HaiA2AEnvironmentValue {
    param(
        [Parameter(Mandatory = $true)][AllowEmptyCollection()][string[]]$Lines,
        [Parameter(Mandatory = $true)][string]$Name
    )

    $pattern = '^\s*' + [Regex]::Escape($Name) + '\s*=(?<value>.*)$'
    $matches = @()
    foreach ($line in $Lines) {
        $match = [Regex]::Match($line, $pattern)
        if ($match.Success) { $matches += $match }
    }
    if ($matches.Count -gt 1) {
        throw "The A2A environment contains duplicate $Name settings; refusing to change bridge credentials."
    }
    if ($matches.Count -eq 0) {
        return [pscustomobject]@{ Found = $false; Value = ''; WellFormed = $true }
    }

    $rawValue = $matches[0].Groups['value'].Value.Trim()
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
    if ($value -match '[\r\n]') { $wellFormed = $false }
    return [pscustomobject]@{ Found = $true; Value = $value; WellFormed = $wellFormed }
}

function Get-HaiA2AEnvironmentMutexName {
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

function Set-HaiA2ABridgeConfiguration {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][bool]$Enabled
    )

    $mutex = New-Object Threading.Mutex($false, (Get-HaiA2AEnvironmentMutexName -Path $Path))
    $locked = $false
    try {
        try {
            $locked = $mutex.WaitOne([TimeSpan]::FromSeconds(30))
        } catch [Threading.AbandonedMutexException] {
            $locked = $true
        }
        if (-not $locked) { throw 'Timed out waiting to update the local A2A configuration.' }
        if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
            throw 'The existing HAI environment file was not found; the A2A setting was not changed.'
        }

        $originalBytes = [IO.File]::ReadAllBytes($Path)
        $hasUtf8Bom = $originalBytes.Length -ge 3 -and $originalBytes[0] -eq 0xEF -and $originalBytes[1] -eq 0xBB -and $originalBytes[2] -eq 0xBF
        $byteOffset = if ($hasUtf8Bom) { 3 } else { 0 }
        $utf8 = New-Object Text.UTF8Encoding($false, $true)
        try {
            $content = $utf8.GetString($originalBytes, $byteOffset, $originalBytes.Length - $byteOffset)
        } catch {
            throw 'The HAI environment is not valid UTF-8; the A2A setting was not changed.'
        }

        $newlineMatch = [Regex]::Match($content, "\r\n|\n|\r")
        $newline = if ($newlineMatch.Success) { $newlineMatch.Value } else { [Environment]::NewLine }
        $hadTrailingNewline = [Regex]::IsMatch($content, "(?:\r\n|\n|\r)\z")
        $lines = New-Object 'System.Collections.Generic.List[string]'
        if ($content.Length -gt 0) {
            foreach ($line in [Regex]::Split($content, "\r\n|\n|\r")) { $lines.Add($line) }
            if ($hadTrailingNewline) { $lines.RemoveAt($lines.Count - 1) }
        }

        $names = @(
            'HAI_A2A_BRIDGE_ENABLED',
            'HAI_A2A_BRIDGE_OWNER_ID',
            'HAI_A2A_BRIDGE_TOKEN',
            'HAI_A2A_LOCAL_PORT',
            'HAI_A2A_BRIDGE_URL',
            'HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED'
        )
        $settings = @{}
        foreach ($name in $names) { $settings[$name] = Get-HaiA2AEnvironmentValue -Lines $lines.ToArray() -Name $name }

        if ($Enabled -and $settings.HAI_A2A_BRIDGE_ENABLED.Found -and
            $settings.HAI_A2A_BRIDGE_ENABLED.WellFormed -and
            $settings.HAI_A2A_BRIDGE_ENABLED.Value -ieq 'true') {
            $port = 0
            $publicValue = $settings.HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED
            $validConfiguration =
                $settings.HAI_A2A_BRIDGE_OWNER_ID.Found -and $settings.HAI_A2A_BRIDGE_OWNER_ID.WellFormed -and
                -not [string]::IsNullOrWhiteSpace($settings.HAI_A2A_BRIDGE_OWNER_ID.Value) -and
                $settings.HAI_A2A_BRIDGE_TOKEN.Found -and $settings.HAI_A2A_BRIDGE_TOKEN.WellFormed -and
                $settings.HAI_A2A_BRIDGE_TOKEN.Value -match '\A[A-Za-z0-9_+/=.-]{32,}\z' -and
                $settings.HAI_A2A_LOCAL_PORT.Found -and $settings.HAI_A2A_LOCAL_PORT.WellFormed -and
                [int]::TryParse($settings.HAI_A2A_LOCAL_PORT.Value, [ref]$port) -and $port -ge 1 -and $port -le 65535 -and
                $settings.HAI_A2A_BRIDGE_URL.Found -and $settings.HAI_A2A_BRIDGE_URL.WellFormed -and
                (Test-HaiCredentialFreeA2ABridgeUrl -Url $settings.HAI_A2A_BRIDGE_URL.Value) -and
                (-not $publicValue.Found -or ($publicValue.WellFormed -and ($publicValue.Value -ieq 'true' -or $publicValue.Value -ieq 'false')))
            if (-not $validConfiguration) {
                throw 'The saved enabled A2A configuration is invalid; refusing to rotate or replace its credentials.'
            }
            return
        }

        $port = 8091
        $savedPort = $settings.HAI_A2A_LOCAL_PORT
        $parsedPort = 0
        if ($savedPort.Found -and $savedPort.WellFormed -and
            [int]::TryParse($savedPort.Value, [ref]$parsedPort) -and $parsedPort -ge 1 -and $parsedPort -le 65535) {
            $port = $parsedPort
        }

        $values = @{}
        if ($Enabled) {
            $adminEmail = Get-HaiA2AEnvironmentValue -Lines $lines.ToArray() -Name 'FIRST_RUN_ADMIN_EMAIL'
            if (-not $adminEmail.Found -or -not $adminEmail.WellFormed -or $adminEmail.Value -notmatch '^[^\s@]+@[^\s@]+\.[^\s@]+$') {
                throw 'A valid first-run admin email is required before enabling the A2A bridge.'
            }
            $tokenBytes = New-Object byte[] 32
            $random = [Security.Cryptography.RandomNumberGenerator]::Create()
            try { $random.GetBytes($tokenBytes) } finally { $random.Dispose() }
            $token = ([BitConverter]::ToString($tokenBytes)).Replace('-', '').ToLowerInvariant()
            $values['HAI_A2A_BRIDGE_ENABLED'] = 'true'
            $values['HAI_A2A_BRIDGE_OWNER_ID'] = $adminEmail.Value
            $values['HAI_A2A_BRIDGE_TOKEN'] = $token
        } else {
            $values['HAI_A2A_BRIDGE_ENABLED'] = 'false'
            $values['HAI_A2A_BRIDGE_OWNER_ID'] = ''
            $values['HAI_A2A_BRIDGE_TOKEN'] = ''
        }
        $values['HAI_A2A_LOCAL_PORT'] = [string]$port
        $values['HAI_A2A_BRIDGE_URL'] = "http://127.0.0.1:$port/api/v1/a2a"
        $values['HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED'] = 'false'

        $updatedLines = New-Object 'System.Collections.Generic.List[string]'
        $written = @{}
        foreach ($line in $lines) {
            $replaced = $false
            foreach ($name in $names) {
                if ([Regex]::IsMatch($line, '^\s*' + [Regex]::Escape($name) + '\s*=')) {
                    if (-not $written.ContainsKey($name)) {
                        $updatedLines.Add($name + '=' + $values[$name])
                        $written[$name] = $true
                    }
                    $replaced = $true
                    break
                }
            }
            if (-not $replaced) { $updatedLines.Add($line) }
        }
        foreach ($name in $names) {
            if (-not $written.ContainsKey($name)) { $updatedLines.Add($name + '=' + $values[$name]) }
        }
        $updatedContent = [string]::Join($newline, $updatedLines.ToArray())
        if ($hadTrailingNewline) { $updatedContent += $newline }
        $updatedBytes = $utf8.GetBytes($updatedContent)
        if ($hasUtf8Bom) {
            $withBom = New-Object byte[] ($updatedBytes.Length + 3)
            $withBom[0] = 0xEF; $withBom[1] = 0xBB; $withBom[2] = 0xBF
            [Array]::Copy($updatedBytes, 0, $withBom, 3, $updatedBytes.Length)
            $updatedBytes = $withBom
        }

        $directory = [IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($Path))
        $temporaryPath = Join-Path $directory ('.hai-a2a-' + [Guid]::NewGuid().ToString('N') + '.tmp')
        $backupPath = Join-Path $directory ('.hai-a2a-backup-' + [Guid]::NewGuid().ToString('N') + '.tmp')
        $operationFailure = $null
        $replacementCompleted = $false
        $cleanupFailures = New-Object 'System.Collections.Generic.List[string]'
        try {
            $environmentAcl = Get-Acl -LiteralPath $Path
            Write-HaiAclProtectedFile -Path $temporaryPath -Bytes $updatedBytes -FileSecurity $environmentAcl
            [IO.File]::Replace($temporaryPath, $Path, $backupPath)
            $replacementCompleted = $true
        } catch {
            $operationFailure = $_.Exception
        } finally {
            foreach ($temporaryFile in @($temporaryPath, $backupPath) | Select-Object -Unique) {
                try {
                    [void](Remove-HaiSensitiveTemporaryFile -Path $temporaryFile)
                } catch {
                    $cleanupFailures.Add($temporaryFile)
                }
            }
        }
        if ($cleanupFailures.Count -gt 0) {
            $failedPaths = @($cleanupFailures | Select-Object -Unique | ForEach-Object { "'$_'" }) -join ', '
            $updateState = if ($replacementCompleted) { 'The A2A configuration update completed' } else { 'The A2A configuration update did not complete' }
            throw "$updateState, but secure cleanup failed or could not be confirmed for: $failedPaths. These file(s) may contain credentials; do not share them. Close any process holding a file and remove it before retrying."
        }
        if ($null -ne $operationFailure) {
            throw $operationFailure
        }
    } finally {
        if ($locked) { $mutex.ReleaseMutex() }
        $mutex.Dispose()
    }
}

function Test-HaiA2AAgentCard {
    param(
        [Parameter(Mandatory = $true)]$AgentCard,
        [Parameter(Mandatory = $true)][ValidateNotNullOrEmpty()][string]$BridgeUrl
    )

    if ([string]::IsNullOrWhiteSpace([string]$AgentCard.name)) {
        return $false
    }

    $planningUrl = $BridgeUrl.Trim()
    foreach ($interface in @($AgentCard.supportedInterfaces)) {
        if ([string]$interface.protocolBinding -eq 'JSONRPC' -and
            [string]$interface.protocolVersion -eq '1.0' -and
            [string]$interface.url -eq $planningUrl) {
            return $true
        }
    }
    return $false
}

function Get-HaiA2AComposeSettings {
    param([Parameter(Mandatory = $true)][string[]]$ComposeArguments)

    $configurationArguments = @($ComposeArguments) + @('--profile', 'local-a2a', 'config', '--format', 'json')
    $configurationOutput = & docker @configurationArguments 2>$null
    $configurationExitCode = $LASTEXITCODE
    if ($configurationExitCode -ne 0) {
        throw "Docker Compose could not resolve the local A2A settings (exit code $configurationExitCode)."
    }

    try {
        $configurationJson = (@($configurationOutput) -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
    } catch {
        throw 'Docker Compose returned invalid configuration while resolving local A2A settings.'
    }

    $backendEnvironment = $configurationJson.services.backend.environment
    if ($null -eq $backendEnvironment) {
        throw 'The resolved Compose configuration does not contain backend A2A settings.'
    }
    $enabledValue = ([string]$backendEnvironment.HAI_A2A_BRIDGE_ENABLED).Trim()
    if ($enabledValue -ine 'true' -and $enabledValue -ine 'false') {
        throw 'The resolved HAI_A2A_BRIDGE_ENABLED setting must be true or false.'
    }

    $bridgeUrl = ([string]$backendEnvironment.HAI_A2A_BRIDGE_URL).Trim()
    $agentCardUrl = $null
    if ($enabledValue -ieq 'true') {
        if (-not (Test-HaiCredentialFreeA2ABridgeUrl -Url $bridgeUrl)) {
            throw 'The resolved HAI_A2A_BRIDGE_URL must be an absolute credential-free HTTP or HTTPS URL without query, fragment, or credential-like path segments.'
        }

        $gateway = $configurationJson.services.'a2a-gateway'
        $publishedPorts = @($gateway.ports | Where-Object {
            [int]$_.target -eq 80 -and ([string]$_.protocol -in @('', 'tcp'))
        })
        if ($publishedPorts.Count -ne 1) {
            throw 'The resolved local A2A gateway must publish exactly one TCP host port for its Agent Card.'
        }

        $hostIp = [string]$publishedPorts[0].host_ip
        $parsedHostIp = $null
        if (-not [Net.IPAddress]::TryParse($hostIp, [ref]$parsedHostIp) -or
            -not [Net.IPAddress]::IsLoopback($parsedHostIp)) {
            throw 'The resolved local A2A gateway must be bound to a loopback host address.'
        }

        $hostPort = 0
        if (-not [int]::TryParse([string]$publishedPorts[0].published, [ref]$hostPort) -or
            $hostPort -lt 1 -or $hostPort -gt 65535) {
            throw 'The resolved local A2A gateway has an invalid published host port.'
        }
        $probeHost = if ($parsedHostIp.AddressFamily -eq [Net.Sockets.AddressFamily]::InterNetworkV6) { "[$hostIp]" } else { $hostIp }
        $agentCardUrl = "http://${probeHost}:$hostPort/.well-known/agent-card.json"
    }

    return [pscustomobject]@{
        Enabled = ($enabledValue -ieq 'true')
        BridgeUrl = $bridgeUrl
        AgentCardUrl = $agentCardUrl
    }
}

function Suspend-HaiComposeA2AOverrides {
    $overrides = @{}
    foreach ($entry in Get-ChildItem Env:) {
        if ($entry.Name -match '^(?i:HAI_A2A_)' -or $entry.Name -ieq 'COMPOSE_PROFILES') {
            $overrides[$entry.Name] = $entry.Value
            Remove-Item -LiteralPath ("Env:" + $entry.Name) -Force
        }
    }
    return $overrides
}

function Restore-HaiComposeA2AOverrides {
    param([Parameter(Mandatory = $true)][hashtable]$Overrides)

    foreach ($name in $Overrides.Keys) {
        Set-Item -LiteralPath ("Env:" + [string]$name) -Value ([string]$Overrides[$name])
    }
}

function ConvertTo-HaiDshVersionToken {
    param([AllowEmptyString()][string]$Value)

    if ($null -eq $Value) {
        return $null
    }

    $pattern = '\Av?((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)\z'
    $match = [Regex]::Match($Value.Trim(), $pattern, [System.Text.RegularExpressions.RegexOptions]::CultureInvariant)
    if (-not $match.Success) {
        return $null
    }
    return $match.Groups[1].Value
}

function Test-HaiDshVersionMatch {
    param(
        [AllowEmptyString()][string]$Expected,
        [AllowEmptyString()][string]$Reported
    )

    $expectedToken = ConvertTo-HaiDshVersionToken -Value $Expected
    $reportedToken = ConvertTo-HaiDshVersionToken -Value $Reported
    if ([string]::IsNullOrEmpty($expectedToken) -or [string]::IsNullOrEmpty($reportedToken)) {
        return $false
    }
    return [string]::Equals($expectedToken, $reportedToken, [System.StringComparison]::Ordinal)
}

function Assert-HaiHostRuntimeConfigured {
    throw (Get-HaiHostRuntimeIsolationBlockReason)
}

function Get-HaiHostRuntimeIsolationBlockReason {
    return "HAI's Windows DSH host runtime cannot be enabled: no verified OS-enforced sandbox, authenticated peer-verified bridge channel, or acknowledged start/cancellation protocol exists. A Windows Job Object only manages process lifetime; it does not isolate filesystem access, network access, credentials, or child runtimes. This release gate has no environment-variable override."
}

function Get-HaiHostRuntimePidFile {
    if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        throw 'The signed-in Windows profile is unavailable; the HAI host-runtime PID record cannot be located safely.'
    }
    return Join-Path ([IO.Path]::GetFullPath($env:LOCALAPPDATA)) 'HAI\hai-dsh-bridge.pid'
}

function Get-HaiHostRuntimeEnvironmentFile {
    if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        throw 'The signed-in Windows profile is unavailable; the HAI host-runtime environment file cannot be identified safely.'
    }
    return Join-Path ([IO.Path]::GetFullPath($env:LOCALAPPDATA)) 'HAI\hai.env'
}

function Stop-HaiHostRuntimeWorkerIfPresent {
    Stop-HaiHostRuntimeWorker
}

function Start-HaiHostRuntimeWorker {
    try {
        Stop-HaiHostRuntimeWorkerIfPresent
    } catch {
        throw "$(Get-HaiHostRuntimeIsolationBlockReason) A previous worker could not be safely stopped: $($_.Exception.Message) Its PID record and diagnostics were preserved."
    }
    throw (Get-HaiHostRuntimeIsolationBlockReason)
}

function Test-HaiHostRuntimeWorkerProcess {
    param(
        [Parameter(Mandatory = $true)]
        [int]$ProcessId
    )

    $process = Get-CimInstance -ClassName Win32_Process -Filter "ProcessId = $ProcessId" -ErrorAction Stop
    if (-not $process) {
        return $false
    }

    $windowsPowerShell = [IO.Path]::GetFullPath((Join-Path $env:WINDIR 'System32\WindowsPowerShell\v1.0\powershell.exe'))
    $expectedScript = Join-Path $PSScriptRoot "Run-HAI-DeepSeekBridge.ps1"
    $expectedEnvironment = Get-HaiHostRuntimeEnvironmentFile
    if ($process.Name -ine 'powershell.exe' -or
        [string]::IsNullOrWhiteSpace([string]$process.ExecutablePath) -or
        [string]::IsNullOrWhiteSpace([string]$process.CommandLine)) {
        return $false
    }

    try {
        $actualExecutable = [IO.Path]::GetFullPath([string]$process.ExecutablePath)
        $expectedScript = [IO.Path]::GetFullPath($expectedScript)
        $expectedEnvironment = [IO.Path]::GetFullPath($expectedEnvironment)
    } catch {
        return $false
    }
    if (-not [string]::Equals($actualExecutable, $windowsPowerShell, [StringComparison]::OrdinalIgnoreCase)) {
        return $false
    }

    $executableArgument = '(?:"' + [Regex]::Escape($windowsPowerShell) + '"|' + [Regex]::Escape($windowsPowerShell) + ')'
    $expectedCommandLine = '^\s*' + $executableArgument +
        '\s+-NoProfile\s+-ExecutionPolicy\s+Bypass\s+-File\s+"' + [Regex]::Escape($expectedScript) +
        '"\s+-EnvFile\s+"' + [Regex]::Escape($expectedEnvironment) + '"\s*$'
    return [Regex]::IsMatch([string]$process.CommandLine, $expectedCommandLine, [Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [Text.RegularExpressions.RegexOptions]::CultureInvariant)
}

function Get-HaiHostRuntimeBridgeProcesses {
    $bridgePath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "hai-dsh-bridge.exe"))
    $processes = @(Get-CimInstance -ClassName Win32_Process -Filter "Name = 'hai-dsh-bridge.exe'" -ErrorAction Stop)
    $owned = New-Object 'System.Collections.Generic.List[object]'
    foreach ($process in $processes) {
        if ([string]::IsNullOrWhiteSpace([string]$process.ExecutablePath)) {
            throw "Could not verify the path of a running HAI bridge-named process; refusing to continue startup while its ownership is unknown."
        }
        try {
            $actualPath = [IO.Path]::GetFullPath([string]$process.ExecutablePath)
        } catch {
            throw "Could not verify the path of a running HAI bridge-named process; refusing to continue startup while its ownership is unknown."
        }
        if ([string]::Equals($actualPath, $bridgePath, [StringComparison]::OrdinalIgnoreCase)) {
            $owned.Add($process)
        }
    }
    return @($owned.ToArray())
}

function Get-HaiHostRuntimeLauncherProcesses {
    $expectedScript = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot "Run-HAI-DeepSeekBridge.ps1"))
    $processes = @(Get-CimInstance -ClassName Win32_Process -Filter "Name = 'powershell.exe'" -ErrorAction Stop)
    $owned = New-Object 'System.Collections.Generic.List[object]'
    foreach ($process in $processes) {
        if ([string]::IsNullOrWhiteSpace([string]$process.CommandLine) -or
            ([string]$process.CommandLine).IndexOf($expectedScript, [StringComparison]::OrdinalIgnoreCase) -lt 0) {
            continue
        }
        if ([string]::IsNullOrWhiteSpace([string]$process.ExecutablePath) -or
            -not (Test-HaiHostRuntimeWorkerProcess -ProcessId ([int]$process.ProcessId))) {
            throw "Could not verify a process that references this HAI runtime launcher; refusing to continue startup while its ownership is unknown."
        }
        $owned.Add($process)
    }
    return @($owned.ToArray())
}

function Initialize-HaiVerifiedProcessTerminator {
    if ('Hai.Installer.VerifiedProcessTerminator' -as [type]) { return }

    Add-Type -Language CSharp -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Text;
using Microsoft.Win32.SafeHandles;

namespace Hai.Installer
{
    public static class VerifiedProcessTerminator
    {
        private const uint ProcessTerminate = 0x0001;
        private const uint ProcessQueryLimitedInformation = 0x1000;
        private const uint Synchronize = 0x00100000;
        private const uint WaitObject0 = 0x00000000;
        private const uint WaitTimeout = 0x00000102;
        private const long FileTimeTicksPerCimMicrosecond = 10;

        [StructLayout(LayoutKind.Sequential)]
        private struct NativeFileTime
        {
            public uint LowDateTime;
            public uint HighDateTime;

            public long ToInt64()
            {
                return ((long)HighDateTime << 32) | LowDateTime;
            }
        }

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern SafeProcessHandle OpenProcess(uint access, bool inheritHandle, uint processId);

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern bool QueryFullProcessImageName(SafeProcessHandle process, uint flags, StringBuilder name, ref uint size);

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool GetProcessTimes(SafeProcessHandle process, out NativeFileTime creation, out NativeFileTime exit, out NativeFileTime kernel, out NativeFileTime user);

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern bool TerminateProcess(SafeProcessHandle process, uint exitCode);

        [DllImport("kernel32.dll", SetLastError = true)]
        private static extern uint WaitForSingleObject(SafeProcessHandle handle, uint milliseconds);

        public static bool Stop(uint processId, string expectedPath, long expectedCreationFileTimeUtc, uint timeoutMilliseconds)
        {
            using (SafeProcessHandle process = OpenProcess(ProcessTerminate | ProcessQueryLimitedInformation | Synchronize, false, processId))
            {
                if (process == null || process.IsInvalid)
                {
                    int error = Marshal.GetLastWin32Error();
                    if (error == 87) return false;
                    throw new Win32Exception(error, "Could not open the verified HAI process for shutdown.");
                }

                StringBuilder imagePath = new StringBuilder(32768);
                uint pathLength = (uint)imagePath.Capacity;
                if (!QueryFullProcessImageName(process, 0, imagePath, ref pathLength))
                    throw new Win32Exception(Marshal.GetLastWin32Error(), "Could not verify the process image before shutdown.");
                if (!String.Equals(System.IO.Path.GetFullPath(imagePath.ToString()), System.IO.Path.GetFullPath(expectedPath), StringComparison.OrdinalIgnoreCase))
                    throw new InvalidOperationException("The process image changed before shutdown; refusing to terminate it.");

                NativeFileTime creation, exit, kernel, user;
                if (!GetProcessTimes(process, out creation, out exit, out kernel, out user))
                    throw new Win32Exception(Marshal.GetLastWin32Error(), "Could not verify the process creation time before shutdown.");
                // Win32_Process exposes CIM_DATETIME at microsecond precision, while FILETIME is 100ns.
                if (creation.ToInt64() / FileTimeTicksPerCimMicrosecond != expectedCreationFileTimeUtc / FileTimeTicksPerCimMicrosecond)
                    throw new InvalidOperationException("The process ID was reused before shutdown; refusing to terminate the new process.");

                if (!TerminateProcess(process, 1))
                {
                    uint exited = WaitForSingleObject(process, 0);
                    if (exited == WaitObject0) return true;
                    throw new Win32Exception(Marshal.GetLastWin32Error(), "Could not terminate the verified HAI process.");
                }

                uint waitResult = WaitForSingleObject(process, timeoutMilliseconds);
                if (waitResult == WaitObject0) return true;
                if (waitResult == WaitTimeout)
                    throw new TimeoutException("The verified HAI process did not exit before the shutdown timeout.");
                throw new Win32Exception(Marshal.GetLastWin32Error(), "Could not confirm that the verified HAI process exited.");
            }
        }
    }
}
'@ -ErrorAction Stop | Out-Null
}

function ConvertTo-HaiProcessCreationFileTime {
    param([Parameter(Mandatory = $true)][object]$CreationDate)

    if ($CreationDate -is [datetime]) {
        return ([datetime]$CreationDate).ToUniversalTime().ToFileTimeUtc()
    }
    return [Management.ManagementDateTimeConverter]::ToDateTime([string]$CreationDate).ToUniversalTime().ToFileTimeUtc()
}

function Invoke-HaiVerifiedProcessTermination {
    param(
        [Parameter(Mandatory = $true)][uint32]$ProcessId,
        [Parameter(Mandatory = $true)][string]$ExpectedExecutablePath,
        [Parameter(Mandatory = $true)][long]$ExpectedCreationFileTimeUtc,
        [ValidateRange(1, 60000)][uint32]$TimeoutMilliseconds
    )

    Initialize-HaiVerifiedProcessTerminator
    return [Hai.Installer.VerifiedProcessTerminator]::Stop(
        $ProcessId, $ExpectedExecutablePath, $ExpectedCreationFileTimeUtc, $TimeoutMilliseconds)
}

function Stop-HaiProcessAndWait {
    param(
        [Parameter(Mandatory = $true)][object]$ProcessSnapshot,
        [ValidateRange(1, 60)][int]$TimeoutSeconds = 10
    )

    $processId = [int]$ProcessSnapshot.ProcessId
    if ($processId -le 0 -or
        [string]::IsNullOrWhiteSpace([string]$ProcessSnapshot.ExecutablePath) -or
        $null -eq $ProcessSnapshot.CreationDate -or
        [string]::IsNullOrWhiteSpace([string]$ProcessSnapshot.CommandLine)) {
        throw 'A complete process identity was not available; refusing to terminate a process by PID alone.'
    }

    $current = @(Get-CimInstance -ClassName Win32_Process -Filter "ProcessId = $processId" -ErrorAction Stop)
    if ($current.Count -eq 0) { return }
    if ($current.Count -ne 1 -or
        [string]$current[0].ExecutablePath -ine [string]$ProcessSnapshot.ExecutablePath -or
        [string]$current[0].CommandLine -cne [string]$ProcessSnapshot.CommandLine -or
        [datetime]$current[0].CreationDate -ne [datetime]$ProcessSnapshot.CreationDate) {
        throw "Process $processId no longer matches its verified image, command line, and creation time; refusing to stop it."
    }

    try {
        $stopped = Invoke-HaiVerifiedProcessTermination -ProcessId ([uint32]$processId) `
            -ExpectedExecutablePath ([string]$ProcessSnapshot.ExecutablePath) `
            -ExpectedCreationFileTimeUtc (ConvertTo-HaiProcessCreationFileTime -CreationDate $ProcessSnapshot.CreationDate) `
            -TimeoutMilliseconds ([uint32]($TimeoutSeconds * 1000))
    } catch {
        if ($_.Exception -is [TimeoutException]) {
            throw "Process $processId did not exit within $TimeoutSeconds seconds."
        }
        throw
    }
    if (-not $stopped) { return }

    $remaining = @(Get-CimInstance -ClassName Win32_Process -Filter "ProcessId = $processId" -ErrorAction Stop)
    if ($remaining | Where-Object {
        [string]$_.ExecutablePath -ieq [string]$ProcessSnapshot.ExecutablePath -and
        [string]$_.CommandLine -ceq [string]$ProcessSnapshot.CommandLine -and
        [datetime]$_.CreationDate -eq [datetime]$ProcessSnapshot.CreationDate
    }) {
        throw "Process $processId is still running after the stop wait completed."
    }
}

function Get-HaiHostRuntimeWorkerStatus {
    $pidFile = Get-HaiHostRuntimePidFile
    if (-not (Test-Path -LiteralPath $pidFile -PathType Leaf)) {
        if (@(Get-HaiHostRuntimeBridgeProcesses).Count -gt 0) {
            return "bridge running without a verified launcher PID"
        }
        if (Test-Path -LiteralPath $pidFile) {
            return "pid record path exists but is not a regular file"
        }
        return "not started"
    }
    $workerPid = 0
    if (-not [int]::TryParse([IO.File]::ReadAllText($pidFile).Trim(), [ref]$workerPid) -or $workerPid -le 0) {
        if (@(Get-HaiHostRuntimeBridgeProcesses).Count -gt 0) {
            return "bridge running without a verified launcher PID (PID record invalid)"
        }
        return "pid record invalid"
    }
    if (Test-HaiHostRuntimeWorkerProcess -ProcessId $workerPid) {
        return "running (PID $workerPid)"
    }
    if (Get-Process -Id $workerPid -ErrorAction SilentlyContinue) {
        if (@(Get-HaiHostRuntimeBridgeProcesses).Count -gt 0) {
            return "bridge running without a verified launcher PID (PID record points to another process)"
        }
        return "pid record does not reference the HAI runtime worker"
    }
    if (@(Get-HaiHostRuntimeBridgeProcesses).Count -gt 0) {
        return "bridge running without a verified launcher PID"
    }
    return "stopped (inspect local bridge logs)"
}

function Stop-HaiHostRuntimeWorker {
    $pidFile = Get-HaiHostRuntimePidFile
    $pidFileExists = Test-Path -LiteralPath $pidFile -PathType Leaf
    if (-not $pidFileExists -and (Test-Path -LiteralPath $pidFile)) {
        throw 'The HAI host-runtime PID record path exists but is not a regular file. It was preserved; refusing to start or stop any process.'
    }
    $workerPid = 0
    $launcherOwned = $false
    if ($pidFileExists) {
        if (-not [int]::TryParse([IO.File]::ReadAllText($pidFile).Trim(), [ref]$workerPid) -or $workerPid -le 0) {
            throw "The HAI host runtime PID record is invalid. It was preserved so the worker can be investigated safely."
        }
        $launcherOwned = Test-HaiHostRuntimeWorkerProcess -ProcessId $workerPid
        if (-not $launcherOwned -and (Get-Process -Id $workerPid -ErrorAction SilentlyContinue)) {
            throw "The HAI host runtime PID record refers to a different process. It was preserved; refusing to stop an unrelated process."
        }
    }
    $launchers = @(Get-HaiHostRuntimeLauncherProcesses)
    if ($launcherOwned -and -not (@($launchers | Where-Object { [int]$_.ProcessId -eq $workerPid }).Count)) {
        throw "The HAI runtime PID record could not be matched to the installed launcher process. It was preserved; refusing to continue startup."
    }

    # Older launchers did not always write a PID record. Reconcile every bridge
    # whose executable is this install's exact binary path, including orphans,
    # before stopping a verified launcher and allowing HAI startup to continue.
    for ($pass = 0; $pass -lt 2; $pass++) {
        $bridgeProcesses = @(Get-HaiHostRuntimeBridgeProcesses)
        foreach ($bridgeProcess in $bridgeProcesses) {
            try {
                Stop-HaiProcessAndWait -ProcessSnapshot $bridgeProcess
            } catch {
                throw "The HAI host runtime bridge did not exit. Its PID record and diagnostics were preserved; refusing to continue installer startup."
            }
        }
        if ($pass -eq 0) {
            foreach ($launcher in $launchers) {
                try {
                    Stop-HaiProcessAndWait -ProcessSnapshot $launcher
                } catch {
                    throw "The HAI host runtime launcher did not exit. Its PID record and diagnostics were preserved; refusing to continue installer startup."
                }
            }
        }
    }
    if (@(Get-HaiHostRuntimeBridgeProcesses).Count -gt 0) {
        throw "A native HAI host runtime process remains after stop. Its PID record and diagnostics were preserved; refusing to continue installer startup."
    }
    if (@(Get-HaiHostRuntimeLauncherProcesses).Count -gt 0) {
        throw "The HAI host runtime launcher remains after stop. Its PID record and diagnostics were preserved; refusing to continue installer startup."
    }
    if ($pidFileExists) {
        Remove-Item -LiteralPath $pidFile -Force
    }
}

function Assert-HaiDockerReady {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        throw "Docker Desktop is required. Install and start Docker Desktop, then run Start HAI again."
    }

    $serverVersion = & docker version --format '{{.Server.Version}}' 2>$null
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($serverVersion)) {
        throw "Docker Desktop is installed but its Linux engine is not ready. Start Docker Desktop and wait for it to finish starting."
    }

    & docker compose version *> $null
    if ($LASTEXITCODE -ne 0) {
        throw "Docker Compose v2 is required. Update Docker Desktop, then run Start HAI again."
    }
}

function Get-HaiComposeProjectName {
    $projectName = "018-hai"
    $environmentFile = Get-HaiEnvironmentFile
    if (Test-Path -LiteralPath $environmentFile -PathType Leaf) {
        $environmentContent = [IO.File]::ReadAllText($environmentFile)
        $projectMatches = [Regex]::Matches($environmentContent, "(?m)^\s*COMPOSE_PROJECT_NAME\s*=(?<value>[^\r\n]*)\r?$")
        if ($projectMatches.Count -gt 1) {
            throw "The HAI environment contains duplicate COMPOSE_PROJECT_NAME settings; refusing to manage an ambiguous Docker project."
        }
        if ($projectMatches.Count -eq 1) {
            $projectName = $projectMatches[0].Groups["value"].Value.Trim()
            if ($projectName -cnotmatch '\A[a-z0-9][a-z0-9_-]*\z') {
                throw "The HAI environment contains an invalid COMPOSE_PROJECT_NAME setting; refusing to manage an unknown Docker project."
            }
        }
    }

    return $projectName
}

function Assert-HaiSingleInstallation {
    $installRoot = Get-HaiInstallRoot
    $projectName = Get-HaiComposeProjectName
    $composeFile = [IO.Path]::GetFullPath((Get-HaiComposeFile))
    $containerIds = @(& docker ps -aq --filter 'label=com.docker.compose.project.config_files')
    if ($LASTEXITCODE -ne 0) {
        throw "Could not enumerate Docker Compose containers; refusing to start or stop a potentially competing HAI installation."
    }
    # Compose selects by project name, not config-file labels or HAI service
    # markers. Include collisions even when their config-file label is absent.
    $projectContainerIds = @(& docker ps -aq --filter "label=com.docker.compose.project=$projectName")
    if ($LASTEXITCODE -ne 0) {
        throw "Could not enumerate Docker Compose containers for project '$projectName'; refusing to manage an unverified stack."
    }
    $containerIds = @(($containerIds + $projectContainerIds) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Unique)
    if ($containerIds.Count -eq 0) {
        return
    }

    $haiServiceMarkers = @(
        'backend-migrate', 'backend-runtime-role', 'browser-verifier',
        'gitleaks-runner', 'gosec-runner', 'syft-runner', 'grype-runner', 'trivy-runner',
        'ollama-miniswe', 'agent-framework-runner', 'crewai-runner', 'docling-runner', 'miniswe-runner',
        'nginx', 'nginxconfigmanager', 'ortools-solver', 'evidently-runner', 'guardrails-runner',
        'pydantic-ai-runner', 'fastmcp-bridge', 'lm-eval-runner', 'promptfoo-runner',
        'deepteam-runner', 'deepeval-runner', 'garak-runner', 'whispercpp-runner', 'wasi-runner',
        'temporal-postgres', 'temporal-schema', 'temporal', 'temporal-namespace',
        'postgres-idp', 'postgres-automation', 'redis', 'kafka',
        'generic-auto', 'provider-fixture'
    )
    $haiContainerMarkers = @(
        '018-hai-idp', '018-hai-backend', '018-hai-frontend', '018-hai-a2a-gateway',
        '018-hai-host-runtime-gateway', '018-hai-ngrok', '018-hai-nginx-config-manager',
        '018-hai-postgres-idp', '018-hai-postgres-automation', '018-hai-redis',
        '018-hai-kafka', '018-hai-generic-auto', '018-hai-provider-fixture'
    )
    $expectedRoot = [IO.Path]::GetFullPath($installRoot)

    foreach ($containerId in $containerIds) {
        $inspection = & docker inspect --format '{{.Name}}|{{json .Config.Labels}}' $containerId 2>$null
        if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace(($inspection -join ''))) {
            throw "Could not inspect Docker Compose container '$containerId'; refusing to manage an unknown HAI installation."
        }

        $inspectionText = [string]::Join("`n", [string[]]$inspection)
        $separatorIndex = $inspectionText.IndexOf('|')
        if ($separatorIndex -lt 0 -or $separatorIndex -eq ($inspectionText.Length - 1)) {
            throw "Docker returned incomplete ownership metadata for container '$containerId'; refusing to manage an unknown HAI installation."
        }

        $containerName = $inspectionText.Substring(0, $separatorIndex).Trim().TrimStart('/')
        try {
            $labels = $inspectionText.Substring($separatorIndex + 1) | ConvertFrom-Json -ErrorAction Stop
        } catch {
            throw "Docker returned invalid ownership metadata for container '$containerId'; refusing to manage an unknown HAI installation."
        }

        $labeledProject = [string]$labels.'com.docker.compose.project'
        $workingDirectory = [string]$labels.'com.docker.compose.project.working_dir'
        $configFiles = [string]$labels.'com.docker.compose.project.config_files'
        $serviceName = [string]$labels.'com.docker.compose.service'
        $usesConfiguredProject = [string]::Equals($labeledProject, $projectName, [StringComparison]::OrdinalIgnoreCase)
        if ([string]::IsNullOrWhiteSpace($configFiles)) {
            if ($usesConfiguredProject -or $containerId -in $projectContainerIds) {
                throw "Could not verify configuration-file ownership for container '$containerId' in Docker project '$projectName'; refusing to manage an unknown installation."
            }
            continue
        }

        $usesHaiComposeFile = $false
        foreach ($configFile in @($configFiles -split ',')) {
            $configFilePath = $configFile.Trim().Trim('"').Trim("'")
            if (-not [string]::IsNullOrWhiteSpace($configFilePath) -and
                [string]::Equals([IO.Path]::GetFileName($configFilePath), 'docker-compose.local.yml', [StringComparison]::OrdinalIgnoreCase)) {
                $usesHaiComposeFile = $true
                break
            }
        }
        $isHaiService = $serviceName -in $haiServiceMarkers
        $isHaiNamedContainer = $containerName -in $haiContainerMarkers
        if (-not $usesConfiguredProject -and $containerId -notin $projectContainerIds -and
            (-not $usesHaiComposeFile -or (-not $isHaiService -and -not $isHaiNamedContainer))) {
            continue
        }

        if ([string]::IsNullOrWhiteSpace($labeledProject) -or [string]::IsNullOrWhiteSpace($workingDirectory)) {
            throw "Could not verify the Compose project and owner path for HAI container '$containerId'; refusing to manage an unknown installation."
        }

        try {
            $actualRoot = [IO.Path]::GetFullPath($workingDirectory)
        } catch {
            throw "Docker returned an invalid owner path for HAI container '$containerId'; refusing to manage an unknown installation."
        }
        if (-not [string]::Equals($actualRoot, $expectedRoot, [StringComparison]::OrdinalIgnoreCase)) {
            throw "HAI is already managed from '$workingDirectory'. Use that installation so HAI keeps one canonical local stack and one set of Docker volumes. No containers or data were changed."
        }

        $exactComposeFileFound = $false
        foreach ($configFile in @($configFiles -split ',')) {
            $configFilePath = $configFile.Trim().Trim('"').Trim("'")
            if ([string]::IsNullOrWhiteSpace($configFilePath)) { continue }
            try {
                if ([string]::Equals([IO.Path]::GetFullPath($configFilePath), $composeFile, [StringComparison]::OrdinalIgnoreCase)) {
                    $exactComposeFileFound = $true
                    break
                }
            } catch { }
        }
        if (-not $exactComposeFileFound) {
            throw "A HAI Compose stack from '$workingDirectory' uses a different configuration file. Refusing to create a second stack; no containers or data were changed."
        }

        if (-not [string]::Equals($labeledProject, $projectName, [StringComparison]::OrdinalIgnoreCase)) {
            throw "The HAI installation already has a Compose stack named '$labeledProject', but its configured name is '$projectName'. Resolve that existing stack before starting or stopping HAI; no containers or data were changed."
        }
    }
}

function Get-HaiComposeArguments {
    return @("compose", "--project-name", (Get-HaiComposeProjectName), "--env-file", (Get-HaiEnvironmentFile), "-f", (Get-HaiComposeFile))
}

function Stop-HaiComposeRuntimeForUninstall {
    if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        throw 'The signed-in Windows profile is unavailable; HAI ownership could not be verified and uninstall must be cancelled.'
    }

    $environmentFile = Join-Path ([IO.Path]::GetFullPath($env:LOCALAPPDATA)) 'HAI\hai.env'
    if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
        throw 'The existing HAI environment file is missing; runtime ownership could not be verified and uninstall must be cancelled.'
    }
    $environmentItem = Get-Item -LiteralPath $environmentFile -Force -ErrorAction Stop
    if (($environmentItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'The HAI environment file is a reparse point; runtime ownership could not be verified and uninstall must be cancelled.'
    }

    Assert-HaiDockerReady
    # Include every optional profile so uninstall cannot leave a HAI-owned
    # service running simply because it was not part of the default profile.
    $composeArguments = @(Get-HaiComposeArguments) + @('--profile', '*')
    Assert-HaiSingleInstallation

    # Validate the exact installed Compose file and environment without emitting
    # their expanded configuration, which may contain sensitive values.
    $null = @(& docker @composeArguments config --quiet 2>&1)
    $configExitCode = $LASTEXITCODE
    if ($configExitCode -ne 0) {
        throw "HAI Compose configuration could not be verified (exit code $configExitCode); uninstall was cancelled without changing the runtime."
    }

    $null = @(& docker @composeArguments stop --timeout 30 2>&1)
    $stopExitCode = $LASTEXITCODE
    if ($stopExitCode -ne 0) {
        throw "The verified HAI Compose runtime could not be stopped (exit code $stopExitCode); uninstall was cancelled and HAI files and data were preserved."
    }

    $runningContainers = @(& docker @composeArguments ps --status running --quiet 2>$null)
    $verifyExitCode = $LASTEXITCODE
    if ($verifyExitCode -ne 0) {
        throw "The HAI Compose runtime stop could not be verified (exit code $verifyExitCode); uninstall was cancelled and HAI files and data were preserved."
    }
    $runningContainerIds = @($runningContainers | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) })
    if ($runningContainerIds.Count -gt 0) {
        throw 'HAI Compose services are still running; uninstall was cancelled and HAI files and data were preserved.'
    }
}

function Assert-HaiExistingDataRequiresEnvironment {
    if (Test-HaiEnvironmentFileAvailable) { return }

    $projectName = Get-HaiComposeProjectName
    $projectVolumes = @(& docker volume ls --quiet --filter "label=com.docker.compose.project=$projectName")
    if ($LASTEXITCODE -ne 0) {
        throw "Could not verify existing HAI data volumes for project '$projectName'; refusing to initialize replacement credentials."
    }
    $allVolumes = @(& docker volume ls --quiet)
    if ($LASTEXITCODE -ne 0) {
        throw "Could not verify fixed-name HAI data volumes; refusing to initialize replacement credentials."
    }

    $existingVolumes = @(@($projectVolumes) + @($allVolumes | Where-Object { $_ -match '^018-hai-' }) |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Sort-Object -Unique)
    if ($existingVolumes.Count -gt 0) {
        throw "HAI has existing data volumes for project '$projectName', but its protected environment file is missing. Restore the original hai.env from a secure backup before starting; HAI will not generate replacement database or signing credentials for existing data. No volumes were changed."
    }
}

function Initialize-HaiLocalEnvironment {
    param(
        [ValidateRange(1, 65535)][int]$GatewayPort = 8088,
        [switch]$EnableA2ABridge
    )

    $environmentFile = Get-HaiEnvironmentFile
    $initializer = Join-Path (Get-HaiInstallRoot) "scripts\initialize-windows.ps1"
    if (Test-Path -LiteralPath $environmentFile -PathType Leaf) {
        & $initializer -EnvFile $environmentFile -GatewayPort $GatewayPort -MigrateExisting
        if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
            throw "HAI environment migration failed. The existing environment file was not found afterward."
        }
        if ($PSBoundParameters.ContainsKey('EnableA2ABridge')) {
            Set-HaiA2ABridgeConfiguration -Path $environmentFile -Enabled ([bool]$EnableA2ABridge)
        }
        return
    }

    if ($PSBoundParameters.ContainsKey('EnableA2ABridge')) {
        & $initializer -EnvFile $environmentFile -GatewayPort $GatewayPort -EnableA2ABridge:$EnableA2ABridge
    } else {
        & $initializer -EnvFile $environmentFile -GatewayPort $GatewayPort
    }
    if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
        throw "HAI environment initialization failed. Check the setup prompts and try Start HAI again."
    }
}

function Get-HaiHttpProbeResult {
    param(
        [Parameter(Mandatory = $true)][ValidateNotNullOrEmpty()][string]$Name,
        [Parameter(Mandatory = $true)][ValidateNotNullOrEmpty()][string]$Path,
        [ValidateRange(1, 30)][int]$TimeoutSeconds = 3
    )

    $uri = "$(Get-HaiUrl)$Path"
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri $uri -TimeoutSec $TimeoutSeconds `
            -MaximumRedirection 0 -ErrorAction Stop
        $statusCode = [int]$response.StatusCode
        $contentType = ''
        $location = ''
        if ($null -ne $response.Headers) {
            $contentType = [string]$response.Headers['Content-Type']
            $location = [string]$response.Headers['Location']
        }
    } catch {
        $statusCode = 0
        $contentType = ''
        $location = ''
        try { $statusCode = [int]$_.Exception.Response.StatusCode } catch { }
        try { $contentType = [string]$_.Exception.Response.Headers['Content-Type'] } catch { }
        try { $location = [string]$_.Exception.Response.Headers['Location'] } catch { }
        $message = if ($statusCode -gt 0) { "HTTP $statusCode" } else { 'timed out or unavailable' }
        return [pscustomobject]@{
            Name = $Name; Uri = $uri; State = 'unhealthy'; StatusCode = $statusCode
            ContentType = $contentType; Location = $location; Detail = $message
        }
    }

    $state = if ($statusCode -ge 200 -and $statusCode -lt 300) { 'healthy' } else { 'unhealthy' }
    $detail = if ($statusCode -gt 0) { "HTTP $statusCode" } else { 'no HTTP response' }
    return [pscustomobject]@{
        Name = $Name; Uri = $uri; State = $state; StatusCode = $statusCode
        ContentType = $contentType; Location = $location; Detail = $detail
    }
}

function Get-HaiProtectedApiProbeResult {
    param([ValidateRange(1, 30)][int]$TimeoutSeconds = 3)

    # This read-only request intentionally carries no credentials. It checks
    # that the protected API path and its authentication boundary respond; it
    # cannot prove a signed-in user's API session is valid.
    $probe = Get-HaiHttpProbeResult -Name 'Protected API route' -Path '/api/v1/openclaw-maintenance' -TimeoutSeconds $TimeoutSeconds
    if ($probe.State -eq 'healthy') {
        if ($probe.ContentType -match '(?i)text/html' -or $probe.Location -match '^/login(?:\?|$)') {
            $probe.State = 'sign_in_required'
            $probe.Detail = 'API gateway is responding; sign-in is required. Authenticated API access was not tested.'
        } else {
            $probe.State = 'not_verified'
            $probe.Detail = 'API route responded without a supplied session; authenticated access was not verified.'
        }
        return $probe
    }

    if ($probe.StatusCode -in @(401, 403) -or
        ($probe.StatusCode -ge 300 -and $probe.StatusCode -lt 400 -and $probe.Location -match '^/login(?:\?|$)')) {
        $probe.State = 'sign_in_required'
        $probe.Detail = "API gateway is responding (HTTP $($probe.StatusCode)); sign-in is required. Authenticated API access was not tested."
    }
    return $probe
}

function Get-HaiStackHealthReport {
    param([ValidateRange(1, 30)][int]$ProbeTimeoutSeconds = 3)

    $backend = Get-HaiHttpProbeResult -Name 'backend' -Path '/readyz' -TimeoutSeconds $ProbeTimeoutSeconds
    $idp = Get-HaiHttpProbeResult -Name 'IDP' -Path '/_hai/idp-readyz' -TimeoutSeconds $ProbeTimeoutSeconds
    $login = Get-HaiHttpProbeResult -Name 'Login frontend shell' -Path '/login' -TimeoutSeconds $ProbeTimeoutSeconds
    $dashboard = Get-HaiHttpProbeResult -Name 'Dashboard frontend shell' -Path '/control-center' -TimeoutSeconds $ProbeTimeoutSeconds
    $protectedApi = Get-HaiProtectedApiProbeResult -TimeoutSeconds $ProbeTimeoutSeconds

    $coreChecks = @($backend, $idp, $login, $dashboard)
    $failed = @($coreChecks | Where-Object { $_.State -ne 'healthy' } | Select-Object -First 1)
    $authenticatedState = 'not_verified'
    $authenticatedDetail = 'No browser session is available to this status check. Sign in and confirm that dashboard data loads.'
    if ($protectedApi.State -eq 'unhealthy') {
        $authenticatedState = 'unhealthy'
        $authenticatedDetail = "The protected API route failed: $($protectedApi.Detail)."
    }
    $component = ''
    $reason = ''
    if ($failed.Count -gt 0) {
        $component = $failed[0].Name
        $reason = $failed[0].Detail
    } elseif ($protectedApi.State -eq 'unhealthy') {
        $component = $protectedApi.Name
        $reason = $protectedApi.Detail
    }

    return [pscustomobject]@{
        Ready = ($failed.Count -eq 0 -and $protectedApi.State -ne 'unhealthy')
        Component = $component
        Reason = $reason
        Backend = $backend
        IdentityProvider = $idp
        FrontendLogin = $login
        FrontendDashboard = $dashboard
        ProtectedApiRoute = $protectedApi
        AuthenticatedApi = [pscustomobject]@{ State = $authenticatedState; Detail = $authenticatedDetail }
    }
}

function Get-HaiMissingEnvironmentGuidance {
    $environmentFile = Get-HaiEnvironmentFile
    $restoreScript = Join-Path (Get-HaiInstallRoot) 'scripts\test-restore-windows.ps1'
    return [pscustomobject]@{
        EnvironmentFile = $environmentFile
        Summary = 'HAI protected local environment file is missing.'
        FirstRun = 'If this is a new installation with no existing HAI data, use Start HAI to initialize it.'
        Recovery = 'If HAI data already exists, do not initialize replacement credentials or remove volumes. Restore hai.env from a completed version-3 protected backup as the same Windows user/profile.'
        Command = "& '$restoreScript' -BackupDirectory '<version-3-backup-folder>' -EnvFile `"`$env:LOCALAPPDATA\HAI\hai.env`" -RestoreEnvironmentOnly"
        Documentation = 'docs\backup-restore.md (environment-only recovery)'
    }
}

function Get-HaiStackReadinessResult {
    param([ValidateRange(1, 30)][int]$ProbeTimeoutSeconds = 3)
    return Get-HaiStackHealthReport -ProbeTimeoutSeconds $ProbeTimeoutSeconds
}

function Wait-HaiReady {
    param([ValidateRange(30, 900)][int]$TimeoutSeconds = 600)

    $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
    $lastFailure = 'the readiness probes have not completed'
    do {
        $result = Get-HaiStackReadinessResult
        if ($result.Ready) {
            return
        }
        $lastFailure = "$($result.Component) readiness failed ($($result.Reason))"
        Start-Sleep -Seconds 3
    } while ([DateTimeOffset]::UtcNow -lt $deadline)

    throw "HAI did not become ready within $TimeoutSeconds seconds: $lastFailure. Open Docker Desktop and inspect the 018-hai containers."
}

function Wait-HaiA2AReady {
    param(
        [ValidateRange(30, 900)][int]$TimeoutSeconds = 600,
        [Parameter(Mandatory = $true)][ValidateNotNullOrEmpty()][string]$BridgeUrl,
        [Parameter(Mandatory = $true)][ValidateNotNullOrEmpty()][string]$AgentCardUrl
    )

    $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $agentCardUrl -TimeoutSec 5
            if ($response.StatusCode -eq 200) {
                $agentCard = $response.Content | ConvertFrom-Json -ErrorAction Stop
                if (Test-HaiA2AAgentCard -AgentCard $agentCard -BridgeUrl $BridgeUrl) {
                    return
                }
            }
        } catch {
            # The optional connector is still starting or returned an invalid card.
        }
        Start-Sleep -Seconds 3
    } while ([DateTimeOffset]::UtcNow -lt $deadline)

    throw "The local A2A Agent Card did not become ready within $TimeoutSeconds seconds. Check the local connector configuration and container logs."
}

function Start-HaiComposeStack {
    param(
        [Parameter(Mandatory = $true)][string[]]$ComposeArguments,
        [ValidateRange(30, 900)][int]$HealthTimeoutSeconds = 600
    )

    $environmentOverrides = Suspend-HaiComposeA2AOverrides
    $upAttempted = $false
    $startupArguments = @($ComposeArguments)
    try {
        $a2aSettings = Get-HaiA2AComposeSettings -ComposeArguments $ComposeArguments
        if (-not $a2aSettings.Enabled) {
            & docker @ComposeArguments --profile local-a2a stop a2a-gateway
            $staleBridgeStopExitCode = $LASTEXITCODE
            if ($staleBridgeStopExitCode -ne 0) {
                throw "The optional A2A bridge is disabled, but its stale service could not be stopped (exit code $staleBridgeStopExitCode). HAI startup was not attempted and core services were not stopped."
            }
        }

        if ($a2aSettings.Enabled) {
            $startupArguments += @('--profile', 'local-a2a')
        }
        $upAttempted = $true
        & docker @startupArguments up -d --build
        $upExitCode = $LASTEXITCODE
        if ($upExitCode -ne 0) {
            throw "HAI startup failed because Docker Compose exited with code $upExitCode."
        }
        Wait-HaiReady -TimeoutSeconds $HealthTimeoutSeconds
        if ($a2aSettings.Enabled) {
            Wait-HaiA2AReady -TimeoutSeconds $HealthTimeoutSeconds -BridgeUrl $a2aSettings.BridgeUrl -AgentCardUrl $a2aSettings.AgentCardUrl
        }
        return
    } catch {
        $startupFailure = $_.Exception.Message
        if (-not $upAttempted) {
            throw
        }

        throw "$startupFailure No containers were stopped automatically because concurrent Docker Compose activity cannot be distinguished safely; ownership by this startup attempt could not be proven. Inspect this HAI installation in Docker Desktop before retrying; local settings and volumes were preserved."
    } finally {
        Restore-HaiComposeA2AOverrides -Overrides $environmentOverrides
    }
}

if ($PromoteMaintenanceWorker) {
    [void](Promote-HaiMaintenanceWorker -WaitSeconds $MaintenanceWorkerWaitSeconds)
    Write-Host 'The staged OpenClaw maintenance worker was promoted safely.'
    return
}

if ($StartMaintenanceTask) {
    $maintenanceSupport = Join-Path (Get-HaiInstallRoot) 'installer\windows\Hai-OpenClawMaintenance.ps1'
    if (-not (Test-Path -LiteralPath $maintenanceSupport -PathType Leaf)) {
        throw 'OpenClaw maintenance support is missing; the scheduled task could not be started.'
    }
    . $maintenanceSupport
    if (Start-HaiOpenClawMaintenanceTask) {
        Write-Host 'The verified OpenClaw maintenance task was started after installer synchronization was released.'
    } else {
        Write-Host 'No HAI-owned OpenClaw maintenance task is registered; no task was started.'
    }
    return
}

if ($StopRuntimeForUninstall) {
    Stop-HaiComposeRuntimeForUninstall
    Write-Host 'The verified HAI Compose runtime was stopped. Containers and volumes were preserved.'
    return
}

if ($ConfigureSilentUpgrade) {
    if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        throw 'The signed-in Windows user profile is unavailable; silent-upgrade configuration was not applied.'
    }

    $environmentFile = Join-Path $env:LOCALAPPDATA 'HAI\hai.env'
    if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
        Write-Host 'No existing HAI environment was found. First-run setup remains deferred until Start HAI is opened.'
        return
    }

    Initialize-HaiLocalEnvironment -GatewayPort (Get-HaiGatewayPort)
    $maintenanceSupport = Join-Path (Get-HaiInstallRoot) 'installer\windows\Hai-OpenClawMaintenance.ps1'
    if (-not (Test-Path -LiteralPath $maintenanceSupport -PathType Leaf)) {
        throw 'OpenClaw maintenance support is missing; the silent upgrade could not register its scheduled task.'
    }
    . $maintenanceSupport
    [void](Register-HaiOpenClawMaintenanceTask -EnvFile $environmentFile `
        -SkipImmediateRun)
    Write-Host 'Silent-upgrade configuration completed without starting HAI or the maintenance worker.'
}
