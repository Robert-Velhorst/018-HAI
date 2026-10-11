function Assert-HaiRecoveryManifest {
    param(
        [Parameter(Mandatory = $true)]$Manifest,
        [Parameter(Mandatory = $true)][string]$Bundle
    )

    if ($Manifest.formatVersion -ne 2) {
        throw "Unsupported or safety-incomplete backup format version: $($Manifest.formatVersion). Version 2 is required."
    }
    if ($Manifest.controlStateSource -ne "018-hai-phase2-control-state") {
        throw "Backup manifest does not identify the expected safety control-state volume."
    }

    $requiredFiles = @("automation.dump", "identity.dump", "media.zip", "phase2-control-state.tar.gz")
    if (@($Manifest.files).Count -ne $requiredFiles.Count) {
        throw "Version-2 manifest must contain exactly four checksummed files."
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
