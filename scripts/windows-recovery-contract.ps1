function Assert-HaiRecoveryManifest {
    param(
        [Parameter(Mandatory = $true)]$Manifest,
        [Parameter(Mandatory = $true)][string]$Bundle,
        $Selection = $null
    )

    if ($Manifest.formatVersion -notin @(2, 3)) {
        throw "Unsupported or safety-incomplete backup format version: $($Manifest.formatVersion). Versions 2 and 3 are supported."
    }
    $expectedControl = '018-hai-phase2-control-state'
    if ($null -ne $Selection) {
        Assert-HaiIsolatedManifest $Selection.Manifest
        $expectedControl = $Selection.Manifest.project + '-control'
        if ($Manifest.recoveryOwner -cne $Selection.Manifest.owner -or $Manifest.recoveryProject -cne $Selection.Manifest.project) {
            throw 'Backup is not owned by the selected isolated recovery manifest.'
        }
    }
    if ($Manifest.controlStateSource -cne $expectedControl) {
        throw "Backup manifest does not identify the expected safety control-state volume."
    }
    if (@($Manifest.databases).Count -ne 2 -or
        [string]$Manifest.databases[0] -notmatch '^[A-Za-z_][A-Za-z0-9_]{0,62}$' -or
        [string]$Manifest.databases[1] -notmatch '^[A-Za-z_][A-Za-z0-9_]{0,62}$' -or
        [string]$Manifest.databases[0] -ceq [string]$Manifest.databases[1]) {
        throw 'Backup manifest database identity is invalid.'
    }

    $requiredFiles = @("automation.dump", "identity.dump", "media.zip", "phase2-control-state.tar.gz")
    if ($Manifest.formatVersion -eq 3) {
        $requiredFiles += 'environment.dpapi'
        $protection = $Manifest.protectedEnvironment
        $properties = @('file', 'scheme', 'version', 'windowsSid', 'bundleId')
        if ([string]$Manifest.backupId -notmatch '^[0-9a-f]{32}$' -or
            $null -eq $protection -or @($protection.PSObject.Properties.Name | Where-Object { $_ -cnotin $properties }).Count -gt 0 -or
            @($protection.PSObject.Properties.Name).Count -ne $properties.Count -or
            $protection.file -cne 'environment.dpapi' -or $protection.scheme -cne 'DPAPI-CurrentUser' -or
            $protection.version -ne 1 -or [string]$protection.windowsSid -notmatch '^S-1-(\d+-)*\d+$' -or
            $protection.bundleId -cne $Manifest.backupId) {
            throw 'Version-3 manifest has invalid Windows-user-bound environment protection metadata.'
        }
    } elseif ($null -ne $Manifest.protectedEnvironment) {
        throw 'Version-2 manifests cannot claim a protected environment artifact.'
    }
    if (@($Manifest.files).Count -ne $requiredFiles.Count) {
        throw "Version-$($Manifest.formatVersion) manifest must contain exactly $($requiredFiles.Count) checksummed files."
    }
    foreach ($name in $requiredFiles) {
        $record = @($Manifest.files | Where-Object { $_.name -eq $name })
        if ($record.Count -ne 1) { throw "Manifest must contain exactly one $name record." }
        $path = Join-Path $Bundle $name
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Backup file is missing: $name" }
        if ([string]$record[0].sha256 -notmatch '^[0-9a-fA-F]{64}$') {
            throw "Manifest contains an invalid SHA-256 value: $name"
        }
        $item = Get-Item -LiteralPath $path
        if ([long]$record[0].bytes -ne $item.Length) { throw "Size mismatch: $name" }
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
        if ($actual -ne ([string]$record[0].sha256).ToLowerInvariant()) { throw "Checksum mismatch: $name" }
    }
}

function Assert-HaiIsolatedManifest($Manifest) {
    $fields = @('contract', 'createdUtc', 'owner', 'project', 'user', 'image', 'daemonId', 'containers', 'seeded', 'verified', 'cleaned')
    if (@($Manifest.PSObject.Properties.Name | Where-Object { $_ -cnotin $fields }).Count) {
        throw 'Isolated resource manifests must contain only the non-secret ownership contract.'
    }
    if ($Manifest.contract -cne 'hai-isolated-recovery.v1' -or $Manifest.owner -cnotmatch '^[0-9a-f]{32}$' -or
        $Manifest.project -cne ('hai-recovery-' + $Manifest.owner) -or $Manifest.user -cne 'hai_recovery' -or
        $Manifest.image -cnotmatch '^sha256:[0-9a-f]{64}$' -or @($Manifest.containers).Count -ne 2) {
        throw 'Invalid isolated recovery owner/project manifest.'
    }
    foreach ($flag in @('seeded', 'verified', 'cleaned')) {
        if ($Manifest.PSObject.Properties[$flag] -and $Manifest.$flag -isnot [bool]) { throw 'Invalid isolated recovery lifecycle flag.' }
    }
    try {
        if ($Manifest.createdUtc -is [DateTime] -or $Manifest.createdUtc -is [DateTimeOffset]) { $created = [DateTimeOffset]$Manifest.createdUtc }
        else { $created = [DateTimeOffset]::Parse($Manifest.createdUtc, [Globalization.CultureInfo]::InvariantCulture) }
    }
    catch { throw 'Invalid isolated recovery creation time.' }
    $age = [DateTimeOffset]::UtcNow - $created
    if ($age.TotalHours -gt 12 -or $age.TotalSeconds -lt -60) { throw 'Isolated recovery manifest is not fresh (12-hour limit).' }
    foreach ($kind in @('automation', 'identity')) {
        $records = @($Manifest.containers | Where-Object { $_.kind -ceq $kind })
        if ($records.Count -ne 1) { throw 'Invalid isolated recovery container inventory.' }
        $r = $records[0]
        if (@($r.PSObject.Properties.Name | Where-Object { $_ -cnotin @('kind', 'name', 'database', 'id', 'databases', 'removed') }).Count) {
            throw 'Unexpected isolated container manifest property.'
        }
        if ($r.PSObject.Properties['removed'] -and $r.removed -isnot [bool]) { throw 'Invalid isolated removal flag.' }
        $short = if ($kind -eq 'automation') { 'a' } else { 'i' }
        if ($r.name -cne "$($Manifest.project)-$kind" -or $r.database -cne "hai_fixture_${short}_$($Manifest.owner)" -or
            ($r.id -and $r.id -cnotmatch '^[0-9a-f]{64}$')) { throw 'Invalid isolated recovery exact identities.' }
        foreach ($db in @($r.databases)) {
            if ($null -eq $db) { continue }
            if (@($db.PSObject.Properties.Name | Where-Object { $_ -cnotin @('name', 'oid', 'owner', 'marker') }).Count) {
                throw 'Unexpected isolated database receipt property.'
            }
            $allowed = @($r.database, "hai_restore_${short}_$($Manifest.owner)")
            if ($db.name -cnotin $allowed -or [string]$db.oid -cnotmatch '^[1-9][0-9]*$' -or
                $db.owner -cne $Manifest.user -or $db.marker -cne "$($Manifest.owner):$($db.name)") {
                throw 'Invalid isolated recovery database identity receipt.'
            }
        }
        if (@($r.databases | Group-Object name | Where-Object Count -ne 1).Count) { throw 'Duplicate isolated database identity.' }
    }
    if ($Manifest.containers[0].id -and $Manifest.containers[0].id -ceq $Manifest.containers[1].id) { throw 'Duplicate isolated container identity.' }
}

function Assert-HaiIsolatedDatabaseReceipts([object[]]$Actual, [object[]]$Expected) {
    if ($Actual.Count -ne $Expected.Count -or $Expected.Count -lt 1) { throw 'Isolated recovery database inventory changed or is uninitialized.' }
    foreach ($db in $Expected) {
        $match = @($Actual | Where-Object { $_.name -ceq $db.name -and $_.oid -ceq $db.oid -and $_.owner -ceq $db.owner -and $_.marker -ceq $db.marker })
        if ($match.Count -ne 1) { throw 'Isolated recovery database OID/owner/marker changed.' }
    }
}

function Assert-HaiIsolatedContainer($Actual, $Record, $Manifest) {
    Assert-HaiIsolatedManifest $Manifest
    if ($Actual.Id -cne $Record.id -or $Actual.Name -cne ('/' + $Record.name) -or $Actual.Image -cne $Manifest.image -or
        $Actual.Config.Labels.'hai.recovery.owner' -cne $Manifest.owner -or
        $Actual.Config.Labels.'com.docker.compose.project' -cne $Manifest.project -or
        $Actual.Config.Labels.'hai.recovery.kind' -cne $Record.kind) { throw 'Isolated recovery container identity/labels changed.' }
    $h = $Actual.HostConfig
    if ($h.NetworkMode -cne 'none' -or $h.Privileged -or $h.PublishAllPorts -or
        @($h.PortBindings.PSObject.Properties).Count -gt 0 -or $h.Binds -or $h.Devices -or $h.CapAdd -or
        $h.VolumesFrom -or $h.PidMode -eq 'host' -or $h.IpcMode -eq 'host' -or $h.RestartPolicy.Name -cne 'no' -or
        [long]$h.Memory -ne 268435456 -or [long]$h.MemorySwap -ne 268435456 -or [long]$h.NanoCpus -ne 500000000 -or [long]$h.PidsLimit -ne 64) {
        throw 'Isolated recovery container lost its bounded/no-host-access configuration.'
    }
    $tmpfs = $h.Tmpfs
    if (@($tmpfs.PSObject.Properties).Count -ne 2 -or
        $tmpfs.'/var/lib/postgresql/data' -cne 'rw,size=134217728' -or $tmpfs.'/hai-control' -cne 'rw,size=16777216') {
        throw 'Isolated recovery requires exactly its bounded PostgreSQL/control tmpfs mounts.'
    }
    foreach ($mount in @($Actual.Mounts)) {
        if ($null -eq $mount) { continue }
        if ($mount.Type -cne 'tmpfs' -or $mount.Destination -cnotin @('/var/lib/postgresql/data', '/hai-control')) {
            throw 'Isolated recovery refuses personal/bind/volume mounts.'
        }
    }
    $expectedEnv = @('POSTGRES_USER=hai_recovery', "POSTGRES_DB=$($Record.database)", 'POSTGRES_HOST_AUTH_METHOD=trust')
    foreach ($env in $expectedEnv) { if (@($Actual.Config.Env | Where-Object { $_ -ceq $env }).Count -ne 1) { throw 'Isolated synthetic database configuration changed.' } }
    if (@($Actual.Config.Env | Where-Object { $_ -cmatch '^PGDATA=' -and $_ -cne 'PGDATA=/var/lib/postgresql/data' }).Count -or
        @($Actual.Config.Env | Where-Object { $_ -match '^(PGHOST|PGSERVICE|PGPASSFILE|POSTGRES_PASSWORD|POSTGRES_INITDB_ARGS|POSTGRES_USER_FILE|POSTGRES_DB_FILE)=' }).Count) {
        throw 'Isolated recovery refuses ambient database/secret overrides.'
    }
    $expectedCommand = @('postgres', '-c', 'shared_buffers=16MB', '-c', 'work_mem=1MB', '-c', 'max_connections=10', '-c', 'temp_file_limit=32768', '-c', 'log_statement=none')
    if ((@($Actual.Config.Cmd) -join '|') -cne ($expectedCommand -join '|') -or [long]$h.ShmSize -ne 16777216) {
        throw 'Isolated recovery PostgreSQL command/resource contract changed.'
    }
}

function Assert-HaiArchiveEntries {
    param([Parameter(Mandatory = $true)][object[]]$Entries)

    $normalized = @()
    foreach ($raw in @($Entries)) {
        $entry = ([string]$raw).Trim()
        if ([string]::IsNullOrWhiteSpace($entry) -or
            $entry.Contains("\") -or
            $entry -match '(^/)|(^|/)\.\.(/|$)') {
            throw "Safety control-state archive contains an unsafe path: $entry"
        }
        $normalized += $entry.TrimStart("./")
    }
    foreach ($required in @("background_mode.json", "emergency_stop.json")) {
        if ($normalized -notcontains $required) {
            throw "Safety control-state archive is missing required record: $required"
        }
    }
}

function ConvertFrom-HaiStateJson([string]$Label, [string]$Json) {
    if ([string]::IsNullOrWhiteSpace($Json)) { throw "$Label JSON is empty." }
    try {
        return $Json | ConvertFrom-Json -ErrorAction Stop
    } catch {
        throw "$Label JSON is invalid: $($_.Exception.Message)"
    }
}

function Assert-HaiTimestamp([string]$Label, $Value) {
    if ($Value -is [DateTime] -or $Value -is [DateTimeOffset]) { return }
    if ($Value -isnot [string] -or [string]::IsNullOrWhiteSpace($Value)) {
        throw "$Label must be a timestamp."
    }
    try {
        $null = [DateTimeOffset]::Parse(
            $Value,
            [Globalization.CultureInfo]::InvariantCulture,
            [Globalization.DateTimeStyles]::RoundtripKind
        )
    } catch {
        throw "$Label must be a valid round-trip timestamp."
    }
}

function Assert-HaiControlStateDocuments {
    param(
        [Parameter(Mandatory = $true)][string]$ModeJson,
        [Parameter(Mandatory = $true)][string]$EmergencyJson
    )

    $modeDocument = ConvertFrom-HaiStateJson "Background mode" $ModeJson
    $modeProperty = $modeDocument.PSObject.Properties["mode"]
    $allowedModes = @("paused", "read_only", "draft_only", "approval_required", "autonomous_safe", "emergency_stopped")
    if ($null -eq $modeProperty -or $modeProperty.Value -isnot [string] -or
        $allowedModes -notcontains $modeProperty.Value) {
        throw "Persisted background mode is missing or invalid."
    }

    $emergency = ConvertFrom-HaiStateJson "Emergency-stop" $EmergencyJson
    $engaged = $emergency.PSObject.Properties["engaged"]
    if ($null -eq $engaged -or $engaged.Value -isnot [bool]) {
        throw "Persisted emergency-stop engaged state must be boolean."
    }
    $revision = $emergency.PSObject.Properties["revision"]
    $parsedRevision = 0L
    if ($null -eq $revision -or $revision.Value -is [string] -or
        -not [long]::TryParse([string]$revision.Value, [ref]$parsedRevision) -or
        $parsedRevision -lt 1) {
        throw "Persisted emergency-stop revision must be a positive integer."
    }
    $updatedAt = $emergency.PSObject.Properties["updatedAt"]
    if ($null -eq $updatedAt) { throw "Persisted emergency-stop updatedAt is required." }
    Assert-HaiTimestamp "Persisted emergency-stop updatedAt" $updatedAt.Value

    if ($engaged.Value) {
        $reason = $emergency.PSObject.Properties["reason"]
        $actor = $emergency.PSObject.Properties["actor"]
        $engagedAt = $emergency.PSObject.Properties["engagedAt"]
        if ($null -eq $reason -or [string]::IsNullOrWhiteSpace([string]$reason.Value) -or
            $null -eq $actor -or [string]::IsNullOrWhiteSpace([string]$actor.Value) -or
            $null -eq $engagedAt) {
            throw "Persisted engaged state requires reason, actor, and engagedAt evidence."
        }
        Assert-HaiTimestamp "Persisted emergency-stop engagedAt" $engagedAt.Value
    }
}

function Read-HaiDockerVolumeDocument {
    param(
        [Parameter(Mandatory = $true)][string]$Volume,
        [Parameter(Mandatory = $true)][ValidateSet("background_mode.json", "emergency_stop.json")][string]$Name,
        [Parameter(Mandatory = $true)][string]$Image
    )

    $command = "test -f /state/$Name && test ! -L /state/$Name && cat /state/$Name"
    $content = @(& docker run --rm --network none --read-only --cap-drop ALL --user 10001:10001 `
        -v "${Volume}:/state:ro" `
        --entrypoint /bin/sh `
        $Image `
        -c $command)
    if ($LASTEXITCODE -ne 0) {
        throw "Safety control-state record is missing, unreadable, or not a regular file: $Name"
    }
    return $content -join [Environment]::NewLine
}
