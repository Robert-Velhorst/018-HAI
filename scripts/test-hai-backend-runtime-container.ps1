param(
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$Image
)

$ErrorActionPreference = 'Stop'
if ($Image.StartsWith('-') -or $Image -match '\s') {
    throw 'Image must be a single Docker image reference.'
}

$probe = 'set -eu; test "$(id -u)" = 10001; test "$(id -g)" = 10001; for p in /root/images /root/agent-workspaces /root/phase2-control-state /tmp; do f="$p/.hai-runtime-write-test-$$"; : > "$f"; rm "$f"; done'
$volume = "hai-runtime-contract-$([guid]::NewGuid().ToString('N'))"
$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) "hai-runtime-contract-$([guid]::NewGuid().ToString('N'))"
$tempRootFull = [System.IO.Path]::GetFullPath($tempRoot)
$systemTempFull = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$systemTempPrefix = $systemTempFull.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar
if (-not $tempRootFull.StartsWith($systemTempPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'Refusing to create runtime test directories outside the system temporary directory.'
}
$images = Join-Path $tempRoot 'images'
$workspaces = Join-Path $tempRoot 'workspaces'
$volumeCreated = $false
$tempCreated = $false
$runExitCode = 1
$removeExitCode = 0

try {
    New-Item -ItemType Directory -Path $images,$workspaces -Force | Out-Null
    $tempCreated = $true
    if ($env:OS -eq 'Windows_NT') {
        & icacls.exe $tempRoot /grant '*S-1-1-0:(OI)(CI)M' /T /C | Out-Null
        if ($LASTEXITCODE -ne 0) { throw 'Could not grant the temporary bind mounts writable test permissions.' }
    }
    else {
        & chmod -R 0777 $tempRoot
        if ($LASTEXITCODE -ne 0) { throw 'Could not grant the temporary bind mounts writable test permissions.' }
    }

    & docker volume create $volume | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not create the temporary runtime contract volume.' }
    $volumeCreated = $true

    & docker run --pull=never --rm --network none --read-only `
        --tmpfs /tmp:rw,noexec,nosuid,size=64m `
        --mount "type=bind,source=$images,target=/root/images" `
        --mount "type=bind,source=$workspaces,target=/root/agent-workspaces" `
        --mount "type=volume,source=$volume,target=/root/phase2-control-state" `
        $Image /bin/sh -ec $probe
    $runExitCode = $LASTEXITCODE
}
finally {
    if ($volumeCreated) {
        & docker volume rm $volume | Out-Null
        $removeExitCode = $LASTEXITCODE
    }
    if ($tempCreated) {
        Remove-Item -LiteralPath $tempRootFull -Recurse -Force
    }
}
if ($runExitCode -ne 0) {
    throw "Backend runtime image contract probe failed with exit code $runExitCode."
}
if ($volumeCreated -and $removeExitCode -ne 0) {
    throw "Could not remove temporary runtime contract volume $volume."
}
