[CmdletBinding()]
param(
    [string]$EnvFile = ".env.local",
    [string]$OutputDirectory = "backups",
    [switch]$ValidateOnly,
    [string]$RecoveryResourceManifest,
    [switch]$LibraryOnly
)

function Get-HaiWindowsUserSid {
    if (-not $IsWindows -and [Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        throw 'Windows-user-bound environment protection is available only on Windows.'
    }
    return [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
}

function Import-HaiProtectedData {
    try { Add-Type -AssemblyName System.Security.Cryptography.ProtectedData -ErrorAction Stop }
    catch { Add-Type -AssemblyName System.Security -ErrorAction Stop }
}

function Get-HaiEnvironmentEntropy([string]$BundleId, [string]$WindowsSid, [int]$Version = 1) {
    return [Text.Encoding]::UTF8.GetBytes("018-HAI|windows-environment|$Version|$BundleId|$WindowsSid")
}

function Protect-HaiEnvironmentFile([string]$SourcePath, [string]$DestinationPath, [string]$BundleId) {
    if (-not (Test-Path -LiteralPath $SourcePath -PathType Leaf)) { throw 'Environment file is missing.' }
    $source = Get-Item -LiteralPath $SourcePath -Force
    if (($source.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Environment file must not be a reparse point.' }
    Import-HaiProtectedData
    $sid = Get-HaiWindowsUserSid
    $plain = [IO.File]::ReadAllBytes($SourcePath)
    $entropy = Get-HaiEnvironmentEntropy $BundleId $sid
    $cipher = $null
    try {
        $cipher = [Security.Cryptography.ProtectedData]::Protect($plain, $entropy, [Security.Cryptography.DataProtectionScope]::CurrentUser)
        $stream = [IO.File]::Open($DestinationPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        try { $stream.Write($cipher, 0, $cipher.Length); $stream.Flush($true) } finally { $stream.Dispose() }
        return [pscustomobject][ordered]@{
            file = 'environment.dpapi'
            scheme = 'DPAPI-CurrentUser'
            version = 1
            windowsSid = $sid
            bundleId = $BundleId
        }
    } finally {
        [Array]::Clear($plain, 0, $plain.Length)
        if ($null -ne $cipher) { [Array]::Clear($cipher, 0, $cipher.Length) }
        [Array]::Clear($entropy, 0, $entropy.Length)
    }
}

function ConvertFrom-HaiEnvironmentBytes([byte[]]$Bytes) {
    try { $text = [Text.UTF8Encoding]::new($false, $true).GetString($Bytes) }
    catch { throw 'Protected environment file is not valid UTF-8.' }
    $values = @{}
    foreach ($line in ($text -split "`r?`n")) {
        if ($line -match '^\s*#' -or [string]::IsNullOrWhiteSpace($line)) { continue }
        $separator = $line.IndexOf('=')
        if ($separator -lt 1) { continue }
        $name = $line.Substring(0, $separator).Trim()
        $value = $line.Substring($separator + 1).Trim()
        if (($value.StartsWith("'") -and $value.EndsWith("'")) -or ($value.StartsWith('"') -and $value.EndsWith('"'))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$name] = $value
    }
    return $values
}

function New-HaiPrivateDirectorySecurity([Security.Principal.SecurityIdentifier]$Sid) {
    $security = [Security.AccessControl.DirectorySecurity]::new()
    $security.SetAccessRuleProtection($true, $false)
    $security.SetOwner($Sid)
    foreach ($principal in @($Sid, [Security.Principal.SecurityIdentifier]::new('S-1-5-18'))) {
        $rule = [Security.AccessControl.FileSystemAccessRule]::new($principal, [Security.AccessControl.FileSystemRights]::FullControl,
            [Security.AccessControl.InheritanceFlags]::ContainerInherit -bor [Security.AccessControl.InheritanceFlags]::ObjectInherit,
            [Security.AccessControl.PropagationFlags]::None, [Security.AccessControl.AccessControlType]::Allow)
        $null = $security.AddAccessRule($rule)
    }
    return $security
}

function New-HaiPrivateFileSecurity([Security.Principal.SecurityIdentifier]$Sid) {
    $security = [Security.AccessControl.FileSecurity]::new()
    $security.SetAccessRuleProtection($true, $false)
    $security.SetOwner($Sid)
    foreach ($principal in @($Sid, [Security.Principal.SecurityIdentifier]::new('S-1-5-18'))) {
        $rule = [Security.AccessControl.FileSystemAccessRule]::new($principal, [Security.AccessControl.FileSystemRights]::FullControl,
            [Security.AccessControl.AccessControlType]::Allow)
        $null = $security.AddAccessRule($rule)
    }
    return $security
}

function New-HaiPrivateEnvironmentDirectory([string]$Path, [Security.AccessControl.DirectorySecurity]$Security) {
    $directory = [IO.DirectoryInfo]::new($Path)
    $method = [IO.DirectoryInfo].GetMethod('Create', [type[]]@([Security.AccessControl.DirectorySecurity]))
    if ($null -ne $method) { $directory.Create($Security) }
    else { [System.IO.FileSystemAclExtensions]::Create($directory, $Security) }
}

function New-HaiPrivateEnvironmentFile([string]$Path, [Security.AccessControl.FileSecurity]$Security) {
    $constructor = [IO.FileStream].GetConstructors() | Where-Object {
        $parameters = $_.GetParameters()
        $parameters.Count -eq 7 -and $parameters[6].ParameterType -eq [Security.AccessControl.FileSecurity]
    } | Select-Object -First 1
    if ($null -ne $constructor) {
        return $constructor.Invoke(@($Path, [IO.FileMode]::CreateNew, [Security.AccessControl.FileSystemRights]::FullControl,
            [IO.FileShare]::None, 4096, [IO.FileOptions]::WriteThrough, $Security))
    }
    return [System.IO.FileSystemAclExtensions]::Create([IO.FileInfo]::new($Path), [IO.FileMode]::CreateNew,
        [Security.AccessControl.FileSystemRights]::FullControl, [IO.FileShare]::None, 4096, [IO.FileOptions]::WriteThrough, $Security)
}

function Remove-HaiProtectedEnvironmentStage([string]$StageDirectory) {
    if ([string]::IsNullOrWhiteSpace($StageDirectory) -or -not (Test-Path -LiteralPath $StageDirectory -PathType Container)) { return }
    $item = Get-Item -LiteralPath $StageDirectory -Force
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        [IO.Path]::GetFileName($StageDirectory) -notmatch '^\.hai-env-recovery-[0-9a-f]{32}$') {
        throw 'Refusing to remove an unowned protected-environment staging directory.'
    }
    Assert-HaiPrivateEnvironmentAcl $StageDirectory -Directory
    $children = @(Get-ChildItem -LiteralPath $StageDirectory -Force)
    if (@($children | Where-Object { $_.Name -cne 'environment.tmp' -or $_.PSIsContainer -or ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 }).Count -gt 0) {
        throw 'Protected-environment staging directory contains unexpected entries.'
    }
    Remove-Item -LiteralPath $StageDirectory -Force -Recurse
}

function Assert-HaiPrivateEnvironmentAcl([string]$Path, [switch]$Directory) {
    $acl = Get-Acl -LiteralPath $Path
    $sid = Get-HaiWindowsUserSid
    $ownerSid = try { [Security.Principal.SecurityIdentifier]::new([string]$acl.Owner).Value }
        catch { ([Security.Principal.NTAccount]::new([string]$acl.Owner)).Translate([Security.Principal.SecurityIdentifier]).Value }
    if (-not $acl.AreAccessRulesProtected -or $ownerSid -cne $sid) {
        throw 'Protected environment staging ACL is not owned and isolated for the current Windows user.'
    }
    $allowed = @($sid, 'S-1-5-18')
    $rules = @($acl.Access)
    if ($rules.Count -ne 2) { throw 'Protected environment staging ACL contains unexpected principals.' }
    foreach ($rule in $rules) {
        $ruleSid = $rule.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value
        if ($ruleSid -notin $allowed -or $rule.AccessControlType -ne [Security.AccessControl.AccessControlType]::Allow -or
            ($rule.FileSystemRights -band [Security.AccessControl.FileSystemRights]::FullControl) -ne [Security.AccessControl.FileSystemRights]::FullControl -or
            ($Directory -and (($rule.InheritanceFlags -band ([Security.AccessControl.InheritanceFlags]::ContainerInherit -bor [Security.AccessControl.InheritanceFlags]::ObjectInherit)) -ne
                ([Security.AccessControl.InheritanceFlags]::ContainerInherit -bor [Security.AccessControl.InheritanceFlags]::ObjectInherit)))) {
            throw 'Protected environment staging ACL grants unexpected access.'
        }
    }
}

function Restore-HaiProtectedEnvironmentFile([string]$Bundle, $Manifest, [string]$TargetPath, [switch]$ValidateOnly, [scriptblock]$ValidationAction) {
    if (Test-Path -LiteralPath $TargetPath) { throw 'Environment file already exists; refusing to overwrite it.' }
    if ($Manifest.formatVersion -ne 3) { throw 'This backup has no recoverable protected environment file.' }
    $protection = $Manifest.protectedEnvironment
    if ($null -eq $protection -or $protection.scheme -cne 'DPAPI-CurrentUser' -or $protection.version -ne 1 -or
        $protection.file -cne 'environment.dpapi' -or $protection.bundleId -cne $Manifest.backupId -or
        [string]$protection.windowsSid -notmatch '^S-1-(\d+-)*\d+$') { throw 'Protected environment metadata is invalid or unsupported.' }
    $sidText = Get-HaiWindowsUserSid
    if ($protection.windowsSid -cne $sidText) { throw 'Protected environment belongs to a different Windows user.' }
    $parent = [IO.Path]::GetFullPath([IO.Path]::GetDirectoryName($TargetPath))
    if (-not [IO.Directory]::Exists($parent)) { throw 'Environment target directory does not exist; refusing to create it.' }
    $parentItem = Get-Item -LiteralPath $parent -Force
    while ($null -ne $parentItem) {
        if (($parentItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Environment target path must not traverse a reparse point.' }
        $parentItem = $parentItem.Parent
    }
    $cipherPath = Join-Path $Bundle 'environment.dpapi'
    $cipher = [IO.File]::ReadAllBytes($cipherPath)
    $entropy = Get-HaiEnvironmentEntropy ([string]$protection.bundleId) $sidText ([int]$protection.version)
    $plain = $null
    $stageDirectory = Join-Path $parent ('.hai-env-recovery-' + [Guid]::NewGuid().ToString('N'))
    try {
        Import-HaiProtectedData
        try { $plain = [Security.Cryptography.ProtectedData]::Unprotect($cipher, $entropy, [Security.Cryptography.DataProtectionScope]::CurrentUser) }
        catch { throw 'Protected environment could not be decrypted for this Windows user or bundle; no target file was created.' }
        $settings = ConvertFrom-HaiEnvironmentBytes $plain
        if ([string]$settings.AUTOMATION_DB_NAME -cne [string]$Manifest.databases[0] -or
            [string]$settings.IDP_DB_NAME -cne [string]$Manifest.databases[1] -or
            [string]::IsNullOrWhiteSpace([string]$settings.DB_USER) -or
            [string]::IsNullOrWhiteSpace([string]$settings.IMAGE_SAVE_DIR) -or [string]$settings.IMAGE_SAVE_DIR.TrimEnd('/') -cne '/root/images') {
            throw 'Protected environment database or media identity does not match the backup manifest.'
        }
        $sid = [Security.Principal.SecurityIdentifier]::new($sidText)
        $directorySecurity = New-HaiPrivateDirectorySecurity $sid
        $null = New-HaiPrivateEnvironmentDirectory $stageDirectory $directorySecurity
        Assert-HaiPrivateEnvironmentAcl $stageDirectory -Directory
        $stagePath = Join-Path $stageDirectory 'environment.tmp'
        $fileSecurity = New-HaiPrivateFileSecurity $sid
        $stage = New-HaiPrivateEnvironmentFile $stagePath $fileSecurity
        try { $stage.Write($plain, 0, $plain.Length); $stage.Flush($true) } finally { $stage.Dispose() }
        Assert-HaiPrivateEnvironmentAcl $stagePath
        if ($ValidateOnly) {
            if ($null -ne $ValidationAction) { & $ValidationAction $stagePath }
            return $settings
        }
        if (Test-Path -LiteralPath $TargetPath) { throw 'Environment file appeared during recovery; refusing to overwrite it.' }
        [IO.File]::Move($stagePath, $TargetPath)
    } finally {
        [Array]::Clear($cipher, 0, $cipher.Length)
        [Array]::Clear($entropy, 0, $entropy.Length)
        if ($null -ne $plain) { [Array]::Clear($plain, 0, $plain.Length) }
        Remove-HaiProtectedEnvironmentStage $stageDirectory
    }
}

function Get-HaiTextDigest([string]$Text) {
    $hash = [Security.Cryptography.SHA256]::Create()
    try {
        return ([BitConverter]::ToString($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($Text)))).Replace('-', '').ToLowerInvariant()
    } finally { $hash.Dispose() }
}

function Assert-HaiLocalDockerEngine {
    if (-not [string]::IsNullOrWhiteSpace($env:DOCKER_HOST)) {
        throw 'Windows backup and restore refuse a DOCKER_HOST override; select the local Docker Desktop engine.'
    }
    $contexts = @(& docker context inspect 2>$null | ConvertFrom-Json)
    if ($LASTEXITCODE -ne 0 -or $contexts.Count -ne 1 -or
        [string]$contexts[0].Endpoints.docker.Host -notmatch '^(npipe|unix)://') {
        throw 'Windows backup and restore require one verified local Docker engine context; no containers or volumes were changed.'
    }
}

function Invoke-HaiRecoveryQuery([string]$Container, [string]$User, [string]$Database, [string]$Sql, $Selection = $null) {
    if ($null -ne $Selection) {
        $result = @(Invoke-HaiIsolatedExec $Selection $Container @('psql', '-X', '--no-password', '-U', $User, '-d', $Database, '-Atq', '-v', 'ON_ERROR_STOP=1', '-c', $Sql))
    } else {
        $result = @(& docker exec $Container psql -X --no-password -U $User -d $Database -Atq -v ON_ERROR_STOP=1 -c $Sql 2>$null)
    }
    if ($LASTEXITCODE -ne 0) { throw "Recovery evidence query failed; database output suppressed to protect private records." }
    return $result
}

function Get-HaiDatabaseIntegrity([string]$Container, [string]$User, [string]$Database, [ValidateSet('automation', 'identity')][string]$Kind, $Selection = $null) {
    $inventorySql = @'
SELECT json_build_object('name', t.tablename, 'columns', (
  SELECT json_agg(json_build_object('name', column_name, 'type', udt_name,
    'nullable', is_nullable, 'default', column_default) ORDER BY ordinal_position)
  FROM information_schema.columns c WHERE c.table_schema = 'public' AND c.table_name = t.tablename
))::text FROM pg_catalog.pg_tables t WHERE t.schemaname = 'public' ORDER BY t.tablename COLLATE "C";
'@
    $inventory = @(Invoke-HaiRecoveryQuery $Container $User $Database $inventorySql $Selection | ForEach-Object { $_ | ConvertFrom-Json })
    $required = if ($Kind -eq 'automation') {
        @('context_memories', 'workflow_items', 'workflow_events', 'workflow_decisions', 'life_ledger_commitment_revisions', 'life_ledger_cost_entries')
    } else { @('users') }
    foreach ($name in $required) {
        if (@($inventory | Where-Object { $_.name -ceq $name }).Count -ne 1) { throw "Canonical $Kind recovery table is missing: $name" }
    }
    $tables = @()
    foreach ($table in $inventory) {
        $identifier = 'public."' + ([string]$table.name).Replace('"', '""') + '"'
        # Hex-encoded UTF-8 keeps Windows console codepages out of the digest. Stream one row at a time.
        # A SELECT alias cannot be referenced inside a COLLATE expression at the
        # same query level. The outer query gives canonical_row a real binding.
        $sql = "BEGIN READ ONLY; SET LOCAL timezone = 'UTC'; SET LOCAL datestyle = 'ISO, YMD'; SET LOCAL extra_float_digits = 3; SET LOCAL work_mem = '4MB'; SET LOCAL statement_timeout = '120s'; COPY (SELECT canonical_row FROM (SELECT encode(convert_to(to_jsonb(t)::text, 'UTF8'), 'hex') AS canonical_row FROM $identifier t) canonical_rows ORDER BY canonical_row COLLATE ""C"") TO STDOUT; COMMIT;"
        $hash = [Security.Cryptography.SHA256]::Create()
        $rows = 0L
        try {
            $readRows = {
                if ($null -ne $Selection) {
                    Invoke-HaiIsolatedExec $Selection $Container @('psql', '-X', '--no-password', '-U', $User, '-d', $Database, '-Atq', '-v', 'ON_ERROR_STOP=1', '-c', $sql)
                } else {
                    & docker exec $Container psql -X --no-password -U $User -d $Database -Atq -v ON_ERROR_STOP=1 -c $sql 2>$null
                }
            }
            & $readRows | ForEach-Object {
                if ([string]$_ -notmatch '^[0-9a-f]+$') { throw "Invalid canonical recovery row encoding." }
                $bytes = [Text.Encoding]::ASCII.GetBytes(([string]$_) + "`n")
                $null = $hash.TransformBlock($bytes, 0, $bytes.Length, $bytes, 0)
                $rows++
            }
            if ($LASTEXITCODE -ne 0) { throw "Canonical $Kind recovery read failed; private database output suppressed." }
            $null = $hash.TransformFinalBlock([byte[]]@(), 0, 0)
            $tables += [pscustomobject][ordered]@{
                name = [string]$table.name
                columns = $table.columns
                rows = $rows
                sha256 = ([BitConverter]::ToString($hash.Hash)).Replace('-', '').ToLowerInvariant()
            }
        } finally { $hash.Dispose() }
    }
    $sequences = @()
    $names = @(Invoke-HaiRecoveryQuery $Container $User $Database "SELECT sequencename FROM pg_catalog.pg_sequences WHERE schemaname='public' ORDER BY sequencename COLLATE ""C"";" $Selection)
    foreach ($name in $names) {
        $identifier = 'public."' + ([string]$name).Replace('"', '""') + '"'
        $value = @(Invoke-HaiRecoveryQuery $Container $User $Database "SELECT json_build_object('lastValue', last_value::text, 'isCalled', is_called)::text FROM $identifier;" $Selection)
        if ($value.Count -ne 1) { throw "Recovery sequence evidence is missing." }
        $sequences += [pscustomobject][ordered]@{ name = [string]$name; state = ($value[0] | ConvertFrom-Json) }
    }
    return [pscustomobject][ordered]@{ tables = $tables; sequences = $sequences }
}

function Assert-HaiRecoveryEvidence($Expected, $Actual, [string]$Label) {
    if ($null -eq $Expected -or $null -eq $Actual -or
        (Get-HaiTextDigest ($Expected | ConvertTo-Json -Depth 30 -Compress)) -cne
        (Get-HaiTextDigest ($Actual | ConvertTo-Json -Depth 30 -Compress))) {
        throw "$Label recovery integrity evidence does not match."
    }
}

function Get-HaiControlDigests([string]$Volume, [string]$Image) {
    $lines = @(& docker run --rm --network none --read-only --cap-drop ALL --user 10001:10001 `
        -v "${Volume}:/state:ro" --entrypoint /bin/sh $Image `
        -c 'test -f /state/background_mode.json && test ! -L /state/background_mode.json && test -f /state/emergency_stop.json && test ! -L /state/emergency_stop.json && sha256sum /state/background_mode.json /state/emergency_stop.json' 2>$null)
    if ($LASTEXITCODE -ne 0 -or $lines.Count -ne 2) { throw "Safety-control digest read failed." }
    $result = [ordered]@{}
    foreach ($line in $lines) {
        if ($line -notmatch '^([0-9a-f]{64})  /state/(background_mode\.json|emergency_stop\.json)$') { throw "Invalid safety-control digest evidence." }
        $result[$Matches[2]] = $Matches[1]
    }
    if ($result.Count -ne 2) { throw "Duplicate safety-control digest evidence." }
    return [pscustomobject]$result
}

function Assert-HaiStrictControlArchive([object[]]$Entries, [object[]]$Details) {
    Assert-HaiArchiveEntries $Entries
    if ($Entries.Count -ne 3 -or $Details.Count -ne 3) { throw "Safety archive must contain only its directory and two regular JSON files." }
    $seen = @{}
    for ($i = 0; $i -lt $Entries.Count; $i++) {
        $entry = [string]$Entries[$i]
        $type = if ($entry -ceq './') { 'd' } elseif ($entry -ceq './background_mode.json' -or $entry -ceq './emergency_stop.json') { '-' } else { throw "Unexpected safety archive entry." }
        if ($seen.ContainsKey($entry) -or -not ([string]$Details[$i]).StartsWith($type, [StringComparison]::Ordinal)) { throw "Safety archive contains a duplicate or non-regular entry." }
        $seen[$entry] = $true
    }
}

function Get-HaiOptionalRecoveryItem([string]$Path) {
    try { return Get-Item -LiteralPath $Path -Force -ErrorAction Stop }
    catch {
        if ($_.CategoryInfo.Category -eq [Management.Automation.ErrorCategory]::ObjectNotFound -or
            $_.Exception -is [IO.FileNotFoundException] -or $_.Exception -is [IO.DirectoryNotFoundException]) { return $null }
        throw 'Could not reliably inspect optional recovery state; refusing to certify a complete backup.'
    }
}

function Get-HaiOpenClawManagedStoreState([string]$Path) {
    $fullPath = [IO.Path]::GetFullPath($Path)
    $cursor = [IO.DirectoryInfo]::new([IO.Path]::GetDirectoryName($fullPath))
    while ($null -ne $cursor) {
        $ancestor = Get-HaiOptionalRecoveryItem $cursor.FullName
        if ($null -ne $ancestor) {
            if (($ancestor.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or -not $ancestor.PSIsContainer) {
                throw 'OpenClaw managed archive path traverses a reparse point or non-directory.'
            }
        }
        $cursor = $cursor.Parent
    }
    $store = Get-HaiOptionalRecoveryItem $fullPath
    if ($null -eq $store) { return 'absent' }
    if (-not $store.PSIsContainer -or ($store.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'OpenClaw managed archive store is not a regular directory.'
    }
    $entries = @(Get-ChildItem -LiteralPath $fullPath -Force -ErrorAction Stop)
    if ($entries.Count -gt 0) {
        throw 'OpenClaw managed archive selection/rollback data exists, but this recovery format cannot safely archive and restore it; no complete backup may be created.'
    }
    return 'empty'
}

function Assert-HaiOptionalRecoveryAssetsAbsent([string[]]$VolumeNames, [string]$OpenClawStorePath) {
    $temporalVolume = '018-hai-temporal-postgres-data'
    if (@($VolumeNames | Where-Object { [string]$_ -ceq $temporalVolume }).Count -gt 0) {
        throw 'Temporal persistence volume exists, but this recovery format cannot safely archive and restore it; no complete backup may be created.'
    }
    $coveredVolumes = @(
        [pscustomobject][ordered]@{ volume = '018-hai-postgres-automation-data'; artifact = 'automation.dump' }
        [pscustomobject][ordered]@{ volume = '018-hai-postgres-idp-data'; artifact = 'identity.dump' }
        [pscustomobject][ordered]@{ volume = '018-hai-phase2-control-state'; artifact = 'phase2-control-state.tar.gz' }
    )
    $coveredNames = @($coveredVolumes | ForEach-Object { $_.volume })
    $uncoveredVolumes = @(
        $VolumeNames | Where-Object {
            ([string]$_).StartsWith('018-hai-', [StringComparison]::Ordinal) -and
            [string]$_ -cnotin $coveredNames
        } | Sort-Object -Unique
    )
    if ($uncoveredVolumes.Count -gt 0) {
        throw "HAI persistent volume(s) are not included in this recovery format: $($uncoveredVolumes -join ', '). No complete backup may be created or these volumes removed."
    }
    foreach ($volume in $coveredVolumes) {
        $volume | Add-Member -NotePropertyName state -NotePropertyValue $(if (@($VolumeNames | Where-Object { [string]$_ -ceq $volume.volume }).Count -gt 0) { 'present' } else { 'absent' })
    }
    return [pscustomobject][ordered]@{
        contract = 'hai-extended-recovery.v2'
        temporal = [pscustomobject][ordered]@{ state = 'absent'; volume = $temporalVolume }
        openClawManagedArchives = [pscustomobject][ordered]@{
            state = Get-HaiOpenClawManagedStoreState $OpenClawStorePath
            path = 'agent-workspaces/.hai-openclaw-ecosystem'
        }
        haiVolumes = $coveredVolumes
    }
}

function Assert-HaiExtendedRecoveryCoverage($Coverage) {
    $contract = if ($null -eq $Coverage) { '' } else { [string]$Coverage.contract }
    $expectedProperties = if ($contract -ceq 'hai-extended-recovery.v1') {
        @('contract', 'temporal', 'openClawManagedArchives')
    } elseif ($contract -ceq 'hai-extended-recovery.v2') {
        @('contract', 'temporal', 'openClawManagedArchives', 'haiVolumes')
    } else {
        @()
    }
    if ($null -eq $Coverage -or $expectedProperties.Count -eq 0 -or
        @($Coverage.PSObject.Properties.Name | Where-Object { $_ -cnotin $expectedProperties }).Count -gt 0 -or
        @($Coverage.PSObject.Properties.Name).Count -ne $expectedProperties.Count) {
        throw 'Backup lacks explicit optional recovery coverage; it is incomplete and must not be restored.'
    }
    $temporal = $Coverage.temporal
    if ($null -eq $temporal -or @($temporal.PSObject.Properties.Name | Where-Object { $_ -cnotin @('state', 'volume') }).Count -gt 0 -or
        @($temporal.PSObject.Properties.Name).Count -ne 2 -or $temporal.state -cne 'absent' -or
        $temporal.volume -cne '018-hai-temporal-postgres-data') {
        throw 'Backup contains unsupported or ambiguous Temporal recovery state; refusing to treat it as complete.'
    }
    $openClaw = $Coverage.openClawManagedArchives
    if ($null -eq $openClaw -or @($openClaw.PSObject.Properties.Name | Where-Object { $_ -cnotin @('state', 'path') }).Count -gt 0 -or
        @($openClaw.PSObject.Properties.Name).Count -ne 2 -or $openClaw.state -notin @('absent', 'empty') -or
        $openClaw.path -cne 'agent-workspaces/.hai-openclaw-ecosystem') {
        throw 'Backup contains unsupported or ambiguous OpenClaw archive recovery state; refusing to treat it as complete.'
    }
    if ($contract -ceq 'hai-extended-recovery.v2') {
        $expectedVolumes = @{
            '018-hai-postgres-automation-data' = 'automation.dump'
            '018-hai-postgres-idp-data' = 'identity.dump'
            '018-hai-phase2-control-state' = 'phase2-control-state.tar.gz'
        }
        if (@($Coverage.haiVolumes).Count -ne $expectedVolumes.Count) {
            throw 'Backup contains incomplete or ambiguous HAI persistent-volume coverage.'
        }
        foreach ($volume in @($Coverage.haiVolumes)) {
            if (@($volume.PSObject.Properties.Name | Where-Object { $_ -cnotin @('volume', 'artifact', 'state') }).Count -gt 0 -or
                @($volume.PSObject.Properties.Name).Count -ne 3 -or
                -not $expectedVolumes.ContainsKey([string]$volume.volume) -or
                $volume.artifact -cne $expectedVolumes[[string]$volume.volume] -or
                $volume.state -notin @('present', 'absent')) {
                throw 'Backup contains unsupported or ambiguous HAI persistent-volume coverage.'
            }
        }
        if (@($Coverage.haiVolumes | Group-Object volume | Where-Object Count -ne 1).Count -gt 0 -or
            @($Coverage.haiVolumes | Where-Object { $_.state -eq 'present' -and $_.volume -eq '018-hai-phase2-control-state' }).Count -ne 1) {
            throw 'Backup contains duplicate HAI volume coverage or omits the required safety-control volume.'
        }
    }
}

function Assert-HaiScratchTargets([string]$Suffix, [string]$Automation, [string]$Identity, [string]$Volume, [string[]]$LiveDatabases) {
    if ($Suffix -cnotmatch '^[0-9a-f]{32}$' -or $Automation -cne "hai_restore_automation_$Suffix" -or
        $Identity -cne "hai_restore_identity_$Suffix" -or $Volume -cne "018-hai-phase2-restore-drill-$Suffix" -or
        $LiveDatabases -contains $Automation -or $LiveDatabases -contains $Identity -or
        $Volume -eq '018-hai-phase2-control-state') { throw "Refusing unowned or live recovery scratch targets." }
}

function Assert-HaiOwnedDirectory([string]$Path, [string]$Parent, [string]$Name) {
    $resolved = [IO.Path]::GetFullPath($Path)
    $expected = [IO.Path]::GetFullPath((Join-Path $Parent $Name))
    if ($resolved -cne $expected -or [IO.Path]::GetFileName($resolved) -cne $Name) { throw "Refusing unowned recovery directory." }
    $ancestor = Get-Item -LiteralPath $resolved -Force -ErrorAction SilentlyContinue
    if ($null -eq $ancestor) { $ancestor = Get-Item -LiteralPath $Parent -Force }
    while ($null -ne $ancestor) {
        if (($ancestor.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "Recovery directory must not traverse a reparse point." }
        $ancestor = $ancestor.Parent
    }
    if (Test-Path -LiteralPath $resolved -PathType Container) { Assert-HaiNoReparseTree $resolved }
}

function Assert-HaiNoReparseTree([string]$Directory) {
    $pending = [Collections.Generic.Queue[string]]::new()
    $pending.Enqueue($Directory)
    while ($pending.Count -gt 0) {
        foreach ($item in @(Get-ChildItem -LiteralPath $pending.Dequeue() -Force)) {
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw "Recovery directory contains a reparse point." }
            if ($item.PSIsContainer) { $pending.Enqueue($item.FullName) }
        }
    }
}

function Assert-HaiMediaEntries($Archive) {
    $seen = @{}
    foreach ($entry in $Archive.Entries) {
        $name = [string]$entry.FullName
        if ([string]::IsNullOrWhiteSpace($name) -or $name.StartsWith('/') -or $name.Contains('\') -or $name.Contains(':') -or
            $name -match '(^|/)\.{1,2}(/|$)' -or (($entry.ExternalAttributes -shr 16) -band 0xF000) -eq 0xA000) { throw "Unsafe media archive entry." }
        foreach ($segment in $name.TrimEnd('/').Split('/')) {
            if ($segment -eq '' -or $segment -match '[<>"|?*\x00-\x1f]' -or $segment -match '[. ]$' -or
                $segment -match '^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\.|$)') { throw "Unsafe Windows media archive entry." }
        }
        $key = $name.TrimEnd('/')
        if ($seen.ContainsKey($key)) { throw "Duplicate media archive entry." }
        $segments = $key.Split('/')
        $ancestor = ''
        for ($i = 0; $i -lt $segments.Length - 1; $i++) {
            $ancestor = if ($ancestor) { "$ancestor/$($segments[$i])" } else { $segments[$i] }
            if ($seen.ContainsKey($ancestor) -and -not $seen[$ancestor]) {
                throw 'Media archive file and directory paths collide.'
            }
        }
        $isDirectory = $name.EndsWith('/')
        if (-not $isDirectory) {
            $prefix = "$key/"
            foreach ($existing in $seen.Keys) {
                if ($existing.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) {
                    throw 'Media archive file and directory paths collide.'
                }
            }
        }
        $seen[$key] = $isDirectory
    }
}

function Assert-HaiMediaContents($Archive, [string]$Directory) {
    Assert-HaiMediaEntries $Archive
    foreach ($entry in $Archive.Entries) {
        $path = Join-Path $Directory $entry.FullName.Replace('/', [IO.Path]::DirectorySeparatorChar)
        if ($entry.FullName.EndsWith('/')) {
            if (-not (Test-Path -LiteralPath $path -PathType Container)) { throw "Restored media directory is missing." }
            continue
        }
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Restored media file is missing." }
        $stream = $entry.Open()
        $hash = [Security.Cryptography.SHA256]::Create()
        try {
            $expected = ([BitConverter]::ToString($hash.ComputeHash($stream))).Replace('-', '').ToLowerInvariant()
        } finally { $hash.Dispose(); $stream.Dispose() }
        $item = Get-Item -LiteralPath $path -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.Length -ne $entry.Length -or
            (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -cne $expected) { throw "Media recovery content does not match its archive." }
    }
    $expectedFiles = @($Archive.Entries | Where-Object { -not $_.FullName.EndsWith('/') }).Count
    if (@(Get-ChildItem -LiteralPath $Directory -Recurse -Force -File).Count -ne $expectedFiles) { throw "Media source or restore contains unexpected files." }
}

# Dot-sourcing imports only the evidence helpers, never backup operations.
if ($LibraryOnly -or $MyInvocation.InvocationName -eq '.') { return }

$ErrorActionPreference = "Stop"
if ($PSBoundParameters.ContainsKey('RecoveryResourceManifest')) {
    if ([string]::IsNullOrWhiteSpace($RecoveryResourceManifest)) { throw 'An explicit isolated recovery manifest must not be empty.' }
    if ($PSBoundParameters.ContainsKey('EnvFile') -or $PSBoundParameters.ContainsKey('OutputDirectory')) {
        throw 'Isolated recovery does not accept caller environment or output paths.'
    }
    $isolatedManifestPath = $RecoveryResourceManifest
    $isolatedValidateOnly = [bool]$ValidateOnly
    . (Join-Path $PSScriptRoot 'windows-recovery-contract.ps1')
    . (Join-Path $PSScriptRoot 'isolated-recovery-rehearsal.ps1')
    Invoke-HaiIsolatedBackup (Read-HaiIsolatedSelection $isolatedManifestPath) -ValidateOnly:$isolatedValidateOnly
    return
}
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$compose = Join-Path $root "docker-compose.local.yml"
$controlStateVolume = "018-hai-phase2-control-state"
$archiveImage = "018-hai-backend:local"
. (Join-Path $PSScriptRoot "windows-recovery-contract.ps1")

function Resolve-RepoPath([string]$Path) {
    if ([IO.Path]::IsPathRooted($Path)) { return [IO.Path]::GetFullPath($Path) }
    return [IO.Path]::GetFullPath((Join-Path $root $Path))
}

function Read-DotEnv([string]$Path) {
    $values = @{}
    foreach ($line in [IO.File]::ReadAllLines($Path)) {
        if ($line -match '^\s*#' -or [string]::IsNullOrWhiteSpace($line)) { continue }
        $separator = $line.IndexOf('=')
        if ($separator -lt 1) { continue }
        $name = $line.Substring(0, $separator).Trim()
        $value = $line.Substring($separator + 1).Trim()
        if (($value.StartsWith("'") -and $value.EndsWith("'")) -or
            ($value.StartsWith('"') -and $value.EndsWith('"'))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$name] = $value
    }
    return $values
}

function Require-Setting($Settings, [string]$Name) {
    $value = [string]$Settings[$Name]
    if ([string]::IsNullOrWhiteSpace($value)) { throw "$Name is required in the environment file." }
    return $value
}

function Wait-ContainerHealthy([string]$Container, [int]$TimeoutSeconds = 90) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $state = (& docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' $Container 2>$null | Out-String).Trim()
        if ($state -eq 'healthy' -or $state -eq 'running') { return }
        if ($state -eq 'unhealthy' -or $state -eq 'exited' -or $state -eq 'dead') {
            throw "$Container entered state '$state' after backup."
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "$Container did not become healthy within $TimeoutSeconds seconds after backup."
}

$envPath = Resolve-RepoPath $EnvFile
$outputPath = Resolve-RepoPath $OutputDirectory
if (-not (Test-Path -LiteralPath $envPath -PathType Leaf)) { throw "Environment file not found: $envPath" }
if (-not (Test-Path -LiteralPath $compose -PathType Leaf)) { throw "Compose file not found: $compose" }
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw "Docker Desktop is required." }
Assert-HaiLocalDockerEngine

$settings = Read-DotEnv $envPath
$automationDB = Require-Setting $settings "AUTOMATION_DB_NAME"
$identityDB = Require-Setting $settings "IDP_DB_NAME"
$dbUser = Require-Setting $settings "DB_USER"
$configuredMedia = Require-Setting $settings "IMAGE_SAVE_DIR"
if ($configuredMedia.TrimEnd('/') -cne '/root/images') { throw "Complete Windows backup requires IMAGE_SAVE_DIR=/root/images, matching the Compose bind mount." }
if ($settings.ContainsKey('HAI_PHASE2_STATE_DIR') -and $settings.HAI_PHASE2_STATE_DIR.TrimEnd('/') -cne '/root/phase2-control-state') {
    throw "Complete Windows backup requires the mounted safety-control directory."
}
$mediaPath = Join-Path $root "images"
if (-not (Test-Path -LiteralPath $mediaPath -PathType Container)) { throw "Media directory is missing; refusing to create an empty replacement." }
Assert-HaiOwnedDirectory $mediaPath $root 'images'

& docker compose --env-file $envPath -f $compose config --quiet
if ($LASTEXITCODE -ne 0) { throw "Docker Compose validation failed." }
$temporalVolumes = @(& docker volume ls --format '{{.Name}}' 2>$null | ForEach-Object { ([string]$_).Trim() } | Where-Object { $_ })
if ($LASTEXITCODE -ne 0) { throw 'Could not reliably inventory local Docker volumes; refusing to certify a complete backup.' }
$optionalRecoveryCoverage = Assert-HaiOptionalRecoveryAssetsAbsent $temporalVolumes (Join-Path $root 'agent-workspaces\.hai-openclaw-ecosystem')
& docker volume inspect $controlStateVolume | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Safety control-state volume is unavailable: $controlStateVolume" }
& docker image inspect $archiveImage | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Local backend image is unavailable: $archiveImage. Run docker compose up --build first." }

if ($ValidateOnly) {
    Write-Host "Backup preflight passed for Compose, configured media, local image, explicit HAI volume coverage, and absence of unsupported Temporal/OpenClaw recovery state. Database contents and safety documents have not been validated."
    return
}

$running = @{}
foreach ($service in @("backend", "idp")) {
    $ids = (& docker compose --env-file $envPath -f $compose ps --status running -q $service | Out-String)
    if ($LASTEXITCODE -ne 0) { throw "Could not determine the original $service running state." }
    $running[$service] = -not [string]::IsNullOrWhiteSpace($ids)
}

$backupId = [Guid]::NewGuid().ToString('N')
$stamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ") + '-' + $backupId
$bundle = Join-Path $outputPath "hai-backup-$stamp"
$automationDump = Join-Path $bundle "automation.dump"
$identityDump = Join-Path $bundle "identity.dump"
$mediaArchive = Join-Path $bundle "media.zip"
$controlStateArchive = Join-Path $bundle "phase2-control-state.tar.gz"
$protectedEnvironmentFile = Join-Path $bundle 'environment.dpapi'
$temporaryFiles = @(
    @{ Container = "018-hai-postgres-automation"; Path = "/tmp/hai-automation-$stamp.dump"; Owned = $false },
    @{ Container = "018-hai-postgres-idp"; Path = "/tmp/hai-identity-$stamp.dump"; Owned = $false }
)

if (-not (Test-Path -LiteralPath $outputPath -PathType Container)) { New-Item -ItemType Directory -Path $outputPath | Out-Null }
Assert-HaiOwnedDirectory $bundle $outputPath ([IO.Path]::GetFileName($bundle))
$backupSid = [Security.Principal.SecurityIdentifier]::new((Get-HaiWindowsUserSid))
$backupDirectorySecurity = New-HaiPrivateDirectorySecurity $backupSid
$null = New-HaiPrivateEnvironmentDirectory $bundle $backupDirectorySecurity
Assert-HaiPrivateEnvironmentAcl $bundle -Directory
$completed = $false
$cleanupFailed = $false
$shutdownTimeoutSeconds = @{ backend = 60; idp = 120 }
try {
    foreach ($service in @("backend", "idp")) {
        if ($running[$service]) {
            $timeout = $shutdownTimeoutSeconds[$service]
            & docker compose --env-file $envPath -f $compose stop --timeout $timeout $service | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "Could not stop $service for a consistent backup." }
        }
    }

    $automationEvidence = Get-HaiDatabaseIntegrity '018-hai-postgres-automation' $dbUser $automationDB 'automation'
    $identityEvidence = Get-HaiDatabaseIntegrity '018-hai-postgres-idp' $dbUser $identityDB 'identity'
    $controlEvidence = Get-HaiControlDigests $controlStateVolume $archiveImage
    foreach ($temporary in $temporaryFiles) {
        & docker exec $temporary.Container /bin/sh -c "test ! -e '$($temporary.Path)' && test ! -L '$($temporary.Path)' && (set -C; : > '$($temporary.Path)')" 2>$null | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "Could not exclusively reserve a backup temporary file." }
        $temporary.Owned = $true
    }

    & docker exec 018-hai-postgres-automation pg_dump -U $settings.DB_USER -d $automationDB -Fc -f $temporaryFiles[0].Path
    if ($LASTEXITCODE -ne 0) { throw "Automation database dump failed." }
    & docker exec 018-hai-postgres-automation pg_restore --list $temporaryFiles[0].Path | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Automation database dump is not readable by pg_restore." }
    & docker cp "$($temporaryFiles[0].Container):$($temporaryFiles[0].Path)" $automationDump
    if ($LASTEXITCODE -ne 0) { throw "Could not copy the automation database dump." }

    & docker exec 018-hai-postgres-idp pg_dump -U $settings.DB_USER -d $identityDB -Fc -f $temporaryFiles[1].Path
    if ($LASTEXITCODE -ne 0) { throw "Identity database dump failed." }
    & docker exec 018-hai-postgres-idp pg_restore --list $temporaryFiles[1].Path | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Identity database dump is not readable by pg_restore." }
    & docker cp "$($temporaryFiles[1].Container):$($temporaryFiles[1].Path)" $identityDump
    if ($LASTEXITCODE -ne 0) { throw "Could not copy the identity database dump." }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    Assert-HaiNoReparseTree $mediaPath
    [IO.Compression.ZipFile]::CreateFromDirectory($mediaPath, $mediaArchive, [IO.Compression.CompressionLevel]::Optimal, $false)
    $media = [IO.Compression.ZipFile]::OpenRead($mediaArchive)
    try { Assert-HaiMediaContents $media $mediaPath } finally { $media.Dispose() }

    $modeJson = Read-HaiDockerVolumeDocument $controlStateVolume "background_mode.json" $archiveImage
    $emergencyJson = Read-HaiDockerVolumeDocument $controlStateVolume "emergency_stop.json" $archiveImage
    Assert-HaiControlStateDocuments $modeJson $emergencyJson

    & docker run --rm --network none --read-only --cap-drop ALL --user 10001:10001 `
        -v "${controlStateVolume}:/source:ro" `
        -v "${bundle}:/backup" `
        --entrypoint /bin/tar `
        $archiveImage `
        -czf "/backup/$([IO.Path]::GetFileName($controlStateArchive))" -C /source .
    if ($LASTEXITCODE -ne 0) { throw "Persisted safety control-state backup failed." }
    $controlStateEntries = @(& docker run --rm --network none --read-only --cap-drop ALL `
        -v "${bundle}:/backup:ro" `
        --entrypoint /bin/tar `
        $archiveImage `
        -tzf /backup/phase2-control-state.tar.gz)
    if ($LASTEXITCODE -ne 0) { throw "Persisted safety control-state archive is not readable." }
    $controlStateDetails = @(& docker run --rm --network none --read-only --cap-drop ALL `
        -v "${bundle}:/backup:ro" --entrypoint /bin/tar $archiveImage -tvzf /backup/phase2-control-state.tar.gz)
    if ($LASTEXITCODE -ne 0) { throw "Safety archive type inspection failed." }
    Assert-HaiStrictControlArchive $controlStateEntries $controlStateDetails

    Assert-HaiRecoveryEvidence $automationEvidence (Get-HaiDatabaseIntegrity '018-hai-postgres-automation' $dbUser $automationDB 'automation') 'Automation source changed during backup'
    Assert-HaiRecoveryEvidence $identityEvidence (Get-HaiDatabaseIntegrity '018-hai-postgres-idp' $dbUser $identityDB 'identity') 'Identity source changed during backup'
    Assert-HaiRecoveryEvidence $controlEvidence (Get-HaiControlDigests $controlStateVolume $archiveImage) 'Safety source changed during backup'
    $latestTemporalVolumes = @(& docker volume ls --format '{{.Name}}' 2>$null | ForEach-Object { ([string]$_).Trim() } | Where-Object { $_ })
    if ($LASTEXITCODE -ne 0) { throw 'Could not recheck local Docker volumes before completion; refusing to certify a complete backup.' }
    $latestOptionalCoverage = Assert-HaiOptionalRecoveryAssetsAbsent $latestTemporalVolumes (Join-Path $root 'agent-workspaces\.hai-openclaw-ecosystem')
    if ((Get-HaiTextDigest ($optionalRecoveryCoverage | ConvertTo-Json -Depth 10 -Compress)) -cne
        (Get-HaiTextDigest ($latestOptionalCoverage | ConvertTo-Json -Depth 10 -Compress))) {
        throw 'Optional Temporal/OpenClaw state changed during backup; refusing to certify a complete bundle.'
    }

    $protectedEnvironment = Protect-HaiEnvironmentFile $envPath $protectedEnvironmentFile $backupId
    $commit = (& git -C $root rev-parse HEAD 2>$null | Out-String).Trim()
    $files = @($automationDump, $identityDump, $mediaArchive, $controlStateArchive, $protectedEnvironmentFile) | ForEach-Object {
        $item = Get-Item -LiteralPath $_
        [ordered]@{ name = $item.Name; bytes = $item.Length; sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $_).Hash.ToLowerInvariant() }
    }
    $manifest = [ordered]@{
        formatVersion = 3
        backupId = $backupId
        createdAt = (Get-Date).ToUniversalTime().ToString("o")
        gitCommit = $commit
        databases = @($automationDB, $identityDB)
        mediaSource = "images"
        controlStateSource = $controlStateVolume
        extendedRecoveryCoverage = $optionalRecoveryCoverage
        protectedEnvironment = $protectedEnvironment
        integrity = [ordered]@{
            contract = 'hai-recovery-integrity.v1'
            automation = $automationEvidence
            identity = $identityEvidence
            controls = $controlEvidence
        }
        files = $files
    }
    Assert-HaiRecoveryManifest ([pscustomobject]$manifest) $bundle
    $manifest | ConvertTo-Json -Depth 30 | Set-Content -LiteralPath (Join-Path $bundle "manifest.json") -Encoding utf8
    $completed = $true
} catch {
    Write-Warning "Backup failed; incomplete owned bundle retained for diagnosis. Do not accept a directory without a completed manifest."
    throw
} finally {
    foreach ($temporary in $temporaryFiles) {
        if ($temporary.Owned) {
            & docker exec $temporary.Container rm -f $temporary.Path 2>$null | Out-Null
            if ($LASTEXITCODE -ne 0) { $cleanupFailed = $true }
        }
    }
    foreach ($service in @("idp", "backend")) {
        if ($running[$service]) {
            & docker compose --env-file $envPath -f $compose start $service | Out-Null
            if ($LASTEXITCODE -ne 0) { $cleanupFailed = $true }
        }
    }
    foreach ($service in @('idp', 'backend')) {
        if ($running[$service]) {
            try { Wait-ContainerHealthy "018-hai-$service" } catch { $cleanupFailed = $true }
        }
    }
    Assert-HaiExtendedRecoveryCoverage $manifest.extendedRecoveryCoverage
    if ($cleanupFailed) { Write-Warning "Backup temporary-file cleanup or original-service recovery failed; inspect locally before accepting availability." }
}
if ($cleanupFailed -or -not $completed) { throw "Backup did not finish with clean temporary-file cleanup and original-service recovery." }
Write-Host "Backup created: $bundle"
