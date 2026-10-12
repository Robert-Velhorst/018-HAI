[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$validatorPath = Join-Path $repositoryRoot 'installer\windows\Hai-WindowsExecutable.ps1'
. $validatorPath

function Set-TestUInt16 {
    param([byte[]]$Bytes, [int]$Offset, [uint16]$Value)
    [Array]::Copy([BitConverter]::GetBytes($Value), 0, $Bytes, $Offset, 2)
}

function Set-TestUInt32 {
    param([byte[]]$Bytes, [int]$Offset, [uint32]$Value)
    [Array]::Copy([BitConverter]::GetBytes($Value), 0, $Bytes, $Offset, 4)
}

function New-TestWindowsExecutableBytes {
    $bytes = New-Object byte[] 1024
    Set-TestUInt16 $bytes 0 0x5A4D
    Set-TestUInt32 $bytes 0x3C 128
    Set-TestUInt32 $bytes 128 0x00004550
    Set-TestUInt16 $bytes 132 0x8664
    Set-TestUInt16 $bytes 134 1
    Set-TestUInt16 $bytes 148 0x00F0
    Set-TestUInt16 $bytes 150 0x0022
    Set-TestUInt16 $bytes 152 0x020B
    Set-TestUInt32 $bytes 168 0x1000
    Set-TestUInt32 $bytes 208 0x2000
    Set-TestUInt32 $bytes 212 0x0200
    Set-TestUInt32 $bytes 400 0x0200
    Set-TestUInt32 $bytes 404 0x1000
    Set-TestUInt32 $bytes 408 0x0200
    Set-TestUInt32 $bytes 412 0x0200
    Set-TestUInt32 $bytes 428 0x60000020
    return ,$bytes
}

$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ('hai-pe-validation-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temporaryRoot | Out-Null
try {
    $candidatePath = Join-Path $temporaryRoot 'worker.exe'
    $valid = New-TestWindowsExecutableBytes
    [IO.File]::WriteAllBytes($candidatePath, $valid)
    if (-not (Test-HaiWindowsExecutablePayload -Path $candidatePath)) {
        throw 'A structurally valid x64 PE worker was rejected.'
    }
    $validatedHash = Test-HaiWindowsExecutablePayload -Path $candidatePath -PassThruHash
    if ($validatedHash -cne (Get-FileHash -LiteralPath $candidatePath -Algorithm SHA256).Hash) {
        throw 'Payload validation did not hash the exact validated executable bytes.'
    }
    $fullDirectories = [byte[]]$valid.Clone()
    Set-TestUInt32 $fullDirectories 260 16
    [IO.File]::WriteAllBytes($candidatePath, $fullDirectories)
    if (-not (Test-HaiWindowsExecutablePayload -Path $candidatePath)) {
        throw 'An optional header containing all declared directories was rejected.'
    }
    $minimalOptionalHeader = [byte[]]$valid.Clone()
    [Array]::Copy($minimalOptionalHeader, 392, $minimalOptionalHeader, 264, 40)
    Set-TestUInt16 $minimalOptionalHeader 148 112
    [IO.File]::WriteAllBytes($candidatePath, $minimalOptionalHeader)
    if (-not (Test-HaiWindowsExecutablePayload -Path $candidatePath)) {
        throw 'A complete PE32+ fixed header with no data directories was rejected.'
    }

    $wrongMachine = [byte[]]$valid.Clone()
    Set-TestUInt16 $wrongMachine 132 0x014C
    $badOptionalHeader = [byte[]]$valid.Clone()
    Set-TestUInt16 $badOptionalHeader 152 0x010B
    $badSectionBounds = [byte[]]$valid.Clone()
    Set-TestUInt32 $badSectionBounds 408 0x1000
    $nonExecutableEntryPoint = [byte[]]$valid.Clone()
    Set-TestUInt32 $nonExecutableEntryPoint 428 0x40000040
    $badHeaderOffset = [byte[]]$valid.Clone()
    Set-TestUInt32 $badHeaderOffset 0x3C 1
    $zeroFilledEntryPoint = [byte[]]$valid.Clone()
    Set-TestUInt32 $zeroFilledEntryPoint 400 0x1000
    Set-TestUInt32 $zeroFilledEntryPoint 168 0x1200
    $shortOptionalHeader = [byte[]]$valid.Clone()
    [Array]::Copy($shortOptionalHeader, 392, $shortOptionalHeader, 216, 40)
    Set-TestUInt16 $shortOptionalHeader 148 64
    $truncatedDirectories = [byte[]]$valid.Clone()
    Set-TestUInt32 $truncatedDirectories 260 17
    $overflowDirectories = [byte[]]$valid.Clone()
    Set-TestUInt32 $overflowDirectories 260 ([uint32]::MaxValue)

    $invalidCases = [ordered]@{
        'empty file' = [byte[]]@()
        'text disguised as an executable' = [Text.Encoding]::ASCII.GetBytes('not a worker executable')
        'wrong architecture' = $wrongMachine
        'PE32 instead of PE32+' = $badOptionalHeader
        'out-of-file section data' = $badSectionBounds
        'entry point outside executable code' = $nonExecutableEntryPoint
        'invalid PE header offset' = $badHeaderOffset
        'entry point in zero-filled section tail' = $zeroFilledEntryPoint
        'truncated PE32+ fixed optional header' = $shortOptionalHeader
        'data directories outside optional header' = $truncatedDirectories
        'overflowing data-directory count' = $overflowDirectories
    }
    foreach ($name in $invalidCases.Keys) {
        [IO.File]::WriteAllBytes($candidatePath, [byte[]]$invalidCases[$name])
        $rejected = $false
        try {
            Test-HaiWindowsExecutablePayload -Path $candidatePath | Out-Null
        } catch {
            if ($_.Exception.Message -notmatch 'could not be verified as a valid 64-bit Windows executable') { throw }
            $rejected = $true
        }
        if (-not $rejected) { throw "The payload validator accepted $name." }
    }

    $rejected = $false
    try { Test-HaiWindowsExecutablePayload -Path $temporaryRoot | Out-Null } catch { $rejected = $true }
    if (-not $rejected) { throw 'The payload validator accepted a directory as an executable.' }
    Write-Output "Windows installer executable-payload validation tests passed (3 accepted x64 PE header shapes and $($invalidCases.Count + 1) rejected payload states)."
} finally {
    $resolvedTemporaryRoot = [IO.Path]::GetFullPath($temporaryRoot)
    $resolvedTempParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if ($resolvedTemporaryRoot.StartsWith($resolvedTempParent, [StringComparison]::OrdinalIgnoreCase) -and
        [IO.Path]::GetFileName($resolvedTemporaryRoot) -match '\Ahai-pe-validation-[a-f0-9]{32}\z' -and
        (Test-Path -LiteralPath $resolvedTemporaryRoot -PathType Container)) {
        Remove-Item -LiteralPath $resolvedTemporaryRoot -Recurse -Force
    }
}
