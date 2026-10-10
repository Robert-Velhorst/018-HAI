$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'backup-windows.ps1') -LibraryOnly
. (Join-Path $PSScriptRoot 'archive-hai-detached-volume.ps1') -VolumeName '018-hai-ollama-local-data' -LibraryOnly

$global:HaiDetachedVolumeMock = @{
    Attached = $false
    Changed = $false
    FailQuery = $false
    ContainerFailure = $false
}

function docker {
    $argsText = @($args | ForEach-Object { [string]$_ })
    $global:LASTEXITCODE = 0
    if ($global:HaiDetachedVolumeMock.FailQuery) { $global:LASTEXITCODE = 1; return }
    if ($argsText[0] -eq 'run' -and $global:HaiDetachedVolumeMock.ContainerFailure) {
        $global:LASTEXITCODE = 2
        return '/bin/tar: ./private-file: Cannot open: Permission denied'
    }
    if ($argsText[0] -eq 'ps') {
        if ($global:HaiDetachedVolumeMock.Attached) { return 'container-id|fixture-container' }
        return
    }
    if ($argsText[0] -eq 'volume' -and $argsText[1] -eq 'inspect') {
        $name = $argsText[-1]
        if ($name -eq '018-hai-ollama-local-data') {
            $createdAt = if ($global:HaiDetachedVolumeMock.Changed) { '2026-10-11T00:00:00Z' } else { '2026-10-10T00:00:00Z' }
            return ([pscustomobject]@{
                Name = $name
                Driver = 'local'
                CreatedAt = $createdAt
                Labels = @{}
            } | ConvertTo-Json -Compress -Depth 5)
        }
        $global:LASTEXITCODE = 1
        return
    }
    throw "Unexpected Docker contract command: $($argsText -join ' ')"
}

$createdAt = [string](Get-HaiDetachedVolumeInspect '018-hai-ollama-local-data').CreatedAt
Assert-HaiDetachedVolume '018-hai-ollama-local-data' $createdAt 'local' | Out-Null
$global:HaiDetachedVolumeMock.Attached = $true
try {
    Assert-HaiDetachedVolume '018-hai-ollama-local-data' $createdAt 'local' | Out-Null
    throw 'Attached volume unexpectedly passed the archive gate.'
} catch {
    if ($_.Exception.Message -notmatch 'attached to a container') { throw }
}
$global:HaiDetachedVolumeMock.Attached = $false
$global:HaiDetachedVolumeMock.Changed = $true
try {
    Assert-HaiDetachedVolume '018-hai-ollama-local-data' $createdAt 'local' | Out-Null
    throw 'Changed volume identity unexpectedly passed the archive gate.'
} catch {
    if ($_.Exception.Message -notmatch 'identity changed') { throw }
}
$global:HaiDetachedVolumeMock.Changed = $false

$global:HaiDetachedVolumeMock.ContainerFailure = $true
try {
    Invoke-HaiArchiveContainer @('run') 'archive step failed'
    throw 'Failed archive command unexpectedly passed.'
} catch {
    if ($_.Exception.Message -notmatch 'exit code 2; permission denied' -or
        $_.Exception.Message -match 'private-file') {
        throw 'Archive failure diagnostics must expose a redacted category and exit code only.'
    }
}

$source = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'archive-hai-detached-volume.ps1'))
if (-not $source.Contains('sourceVolumeRemoved = $false') -or
    -not $source.Contains('type=volume,source=$VolumeName,target=/source,readonly') -or
    -not $source.Contains("'--cap-add', 'DAC_READ_SEARCH'") -or
    -not $source.Contains("'--tmpfs', '/restore:rw,size=1g'") -or
    -not $source.Contains('restoreTargetDisposed = $true') -or
    $source.Contains('docker volume create') -or $source.Contains('docker volume rm') -or
    -not $source.Contains('tar -dzf') -or
    -not $source.Contains('tar -xzf ''/backup/$artifact'' -C /restore') -or
    -not $source.Contains('tar -dzf ''/backup/$artifact'' -C /source') -or
    ([regex]::Matches($source, 'New-HaiPrivateEnvironmentFile')).Count -lt 2 -or
    -not $source.Contains('Assert-HaiPrivateEnvironmentAcl $archivePath')) {
    throw 'Detached volume archive lost source preservation, bounded ephemeral restore, or full comparison checks.'
}
if ($source -match 'docker volume rm \$VolumeName') { throw 'Detached volume archive may not remove its source volume.' }

$verifier = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'verify-hai-detached-volume-archive.ps1'))
if (-not $verifier.Contains('safeToRemove = $false') -or
    -not $verifier.Contains('cleanupAuthorized = $false') -or
    -not $verifier.Contains("'--cap-add', 'DAC_READ_SEARCH'") -or
    -not $verifier.Contains("'-dzf'") -or
    -not $verifier.Contains('target=/source,readonly') -or
    -not $verifier.Contains('Get-FileHash') -or
    -not $verifier.Contains('Assert-HaiVolumeVerifierDetached $VolumeName $sourceMetadata')) {
    throw 'Detached-volume verifier lost its fail-closed, archive-integrity, detach, or read-only source checks.'
}
if ($verifier -match 'docker\s+volume\s+rm|Remove-Item|docker\s+volume\s+prune') {
    throw 'Detached-volume verifier must be read-only and may not remove files or volumes.'
}

Write-Output 'HAI detached-volume archive and verifier contracts: PASS'
$global:LASTEXITCODE = 0
