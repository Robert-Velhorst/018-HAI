[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$library = Join-Path $PSScriptRoot "windows-recovery-contract.ps1"
if (-not (Test-Path -LiteralPath $library -PathType Leaf)) {
    throw "Windows recovery contract library is missing."
}
. $library

function Assert-Throws([string]$Name, [scriptblock]$Action, [string]$Pattern) {
    try {
        & $Action
        throw "$Name unexpectedly passed."
    } catch {
        if ($_.Exception.Message -eq "$Name unexpectedly passed." -or
            $_.Exception.Message -notmatch $Pattern) {
            throw
        }
    }
}

function New-ManifestFile([string]$Path) {
    $item = Get-Item -LiteralPath $Path
    return [pscustomobject]@{
        name = $item.Name
        bytes = $item.Length
        sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
    }
}

$testRoot = Join-Path ([IO.Path]::GetTempPath()) ("hai-recovery-contract-" + [Guid]::NewGuid().ToString("N"))
[IO.Directory]::CreateDirectory($testRoot) | Out-Null
try {
    foreach ($name in @("automation.dump", "identity.dump", "media.zip", "phase2-control-state.tar.gz")) {
        [IO.File]::WriteAllText((Join-Path $testRoot $name), "fixture-$name", [Text.UTF8Encoding]::new($false))
    }
    $validManifest = [pscustomobject]@{
        formatVersion = 2
        controlStateSource = "018-hai-phase2-control-state"
        files = @(
            New-ManifestFile (Join-Path $testRoot "automation.dump")
            New-ManifestFile (Join-Path $testRoot "identity.dump")
            New-ManifestFile (Join-Path $testRoot "media.zip")
            New-ManifestFile (Join-Path $testRoot "phase2-control-state.tar.gz")
        )
    }
    Assert-HaiRecoveryManifest $validManifest $testRoot

    $versionOne = $validManifest | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    $versionOne.formatVersion = 1
    Assert-Throws "version-one manifest" { Assert-HaiRecoveryManifest $versionOne $testRoot } "Version 2 is required"

    $missingState = $validManifest | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    $missingState.files = @($missingState.files | Where-Object { $_.name -ne "phase2-control-state.tar.gz" })
    Assert-Throws "missing safety state" { Assert-HaiRecoveryManifest $missingState $testRoot } "exactly four"

    $wrongSource = $validManifest | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    $wrongSource.controlStateSource = "other-volume"
    Assert-Throws "wrong safety source" { Assert-HaiRecoveryManifest $wrongSource $testRoot } "expected safety control-state volume"

    $badChecksum = $validManifest | ConvertTo-Json -Depth 5 | ConvertFrom-Json
    $badChecksum.files[3].sha256 = "0" * 64
    Assert-Throws "bad safety checksum" { Assert-HaiRecoveryManifest $badChecksum $testRoot } "Checksum mismatch"

    Assert-HaiArchiveEntries @("./", "./background_mode.json", "./emergency_stop.json")
    Assert-Throws "unsafe archive" { Assert-HaiArchiveEntries @("./", "../escape") } "unsafe path"

    $mode = '{"mode":"read_only"}'
    $stop = '{"engaged":false,"updatedAt":"2026-08-15T00:00:00Z","revision":1}'
    Assert-HaiControlStateDocuments $mode $stop
    Assert-Throws "missing mode" { Assert-HaiControlStateDocuments '{}' $stop } "background mode"
    Assert-Throws "invalid mode" { Assert-HaiControlStateDocuments '{"mode":"unrestricted"}' $stop } "background mode"
    Assert-Throws "malformed stop" { Assert-HaiControlStateDocuments $mode '{not-json' } "emergency-stop JSON"
    Assert-Throws "missing stop revision" { Assert-HaiControlStateDocuments $mode '{"engaged":false,"updatedAt":"2026-08-15T00:00:00Z"}' } "revision"
    Assert-Throws "engaged stop without evidence" { Assert-HaiControlStateDocuments $mode '{"engaged":true,"updatedAt":"2026-08-15T00:00:00Z","revision":1}' } "engaged state"

    $restore = [IO.File]::ReadAllText((Join-Path $PSScriptRoot "test-restore-windows.ps1"))
    if ($restore -match '018-hai-phase2-control-state:/restore') {
        throw "Restore drill must never mount the live safety volume as its restore target."
    }
    if ($restore -notmatch '\$scratchControlVolume' -or $restore -notmatch 'docker volume rm') {
        throw "Restore drill does not own and remove a disposable safety volume."
    }
    if ($restore -notmatch '--cap-add CHOWN' -or
        $restore -notmatch 'tar -oxzf' -or
        $restore -notmatch 'chown -R 10001:10001 /restore') {
        throw "Restore drill does not normalize safety-state ownership for the HAI service account."
    }
} finally {
    $resolved = [IO.Path]::GetFullPath($testRoot)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    if (-not $resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to remove recovery fixture outside the temp directory."
    }
    if ([IO.Directory]::Exists($resolved)) { [IO.Directory]::Delete($resolved, $true) }
}

Write-Host "Windows recovery behavioral contracts passed."
