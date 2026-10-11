function Test-HaiWindowsExecutablePayload {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [switch]$PassThruHash
    )

    $stream = $null
    $reader = $null
    try {
        $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
        if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw 'The worker path is not a regular file.'
        }

        $stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
        if ($stream.Length -lt 512) { throw 'The worker file is truncated.' }
        $reader = New-Object IO.BinaryReader($stream)
        if ($reader.ReadUInt16() -ne 0x5A4D) { throw 'The DOS signature is missing.' }

        $stream.Position = 0x3C
        $peOffset = [int64]$reader.ReadInt32()
        if ($peOffset -lt 0x40 -or $peOffset -gt ($stream.Length - 24)) { throw 'The PE header offset is invalid.' }
        $stream.Position = $peOffset
        if ($reader.ReadUInt32() -ne 0x00004550) { throw 'The PE signature is missing.' }

        $machine = $reader.ReadUInt16()
        $sectionCount = $reader.ReadUInt16()
        $stream.Position += 12
        $optionalHeaderSize = $reader.ReadUInt16()
        $characteristics = $reader.ReadUInt16()
        if ($machine -ne 0x8664 -or $sectionCount -lt 1 -or $sectionCount -gt 96 -or
            $optionalHeaderSize -lt 112 -or ($characteristics -band 0x0002) -eq 0 -or
            ($characteristics -band 0x2000) -ne 0) {
            throw 'The COFF header is not a supported x64 executable.'
        }

        $optionalHeaderStart = $stream.Position
        $optionalHeaderEnd = $optionalHeaderStart + $optionalHeaderSize
        if ($optionalHeaderEnd -gt $stream.Length) { throw 'The optional header is truncated.' }
        if ($reader.ReadUInt16() -ne 0x20B) { throw 'The optional header is not PE32+.' }

        $stream.Position = $optionalHeaderStart + 108
        $directoryCount = [uint64]$reader.ReadUInt32()
        if (112 + ($directoryCount * 8) -gt $optionalHeaderSize) {
            throw 'The PE data directories exceed the optional header.'
        }

        $stream.Position = $optionalHeaderStart + 16
        $entryPoint = [uint64]$reader.ReadUInt32()
        $stream.Position = $optionalHeaderStart + 56
        $imageSize = [uint64]$reader.ReadUInt32()
        $headersSize = [uint64]$reader.ReadUInt32()
        $sectionTableStart = $optionalHeaderEnd
        $sectionTableEnd = $sectionTableStart + ([int64]$sectionCount * 40)
        if ($entryPoint -eq 0 -or $imageSize -eq 0 -or $headersSize -lt $sectionTableEnd -or
            $headersSize -gt $stream.Length -or $sectionTableEnd -gt $stream.Length) {
            throw 'The PE image bounds are invalid.'
        }

        $entryPointIsExecutable = $false
        $stream.Position = $sectionTableStart
        for ($index = 0; $index -lt $sectionCount; $index++) {
            $section = $reader.ReadBytes(40)
            if ($section.Length -ne 40) { throw 'A PE section header is truncated.' }
            $virtualSize = [uint64][BitConverter]::ToUInt32($section, 8)
            $virtualAddress = [uint64][BitConverter]::ToUInt32($section, 12)
            $rawSize = [uint64][BitConverter]::ToUInt32($section, 16)
            $rawOffset = [uint64][BitConverter]::ToUInt32($section, 20)
            $sectionCharacteristics = [BitConverter]::ToUInt32($section, 36)
            $virtualSpan = [Math]::Max($virtualSize, $rawSize)

            if (($rawSize -gt 0 -and ($rawOffset -lt $headersSize -or $rawOffset + $rawSize -gt $stream.Length)) -or
                $virtualAddress + $virtualSpan -gt $imageSize) {
                throw 'A PE section exceeds the declared file or image bounds.'
            }
            if (($sectionCharacteristics -band 0x20000000) -ne 0 -and $rawSize -gt 0 -and
                $entryPoint -ge $virtualAddress -and $entryPoint -lt ($virtualAddress + $rawSize)) {
                $entryPointIsExecutable = $true
            }
        }
        if (-not $entryPointIsExecutable) { throw 'The PE entry point is not in an executable section.' }
        if ($PassThruHash) {
            $stream.Position = 0
            $sha256 = [Security.Cryptography.SHA256]::Create()
            try {
                return [BitConverter]::ToString($sha256.ComputeHash($stream)).Replace('-', '')
            } finally { $sha256.Dispose() }
        }
        return $true
    } catch {
        throw 'The maintenance worker payload could not be verified as a valid 64-bit Windows executable.'
    } finally {
        if ($null -ne $reader) { $reader.Dispose() }
        elseif ($null -ne $stream) { $stream.Dispose() }
    }
}
