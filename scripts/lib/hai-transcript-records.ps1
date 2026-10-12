function Read-HaiBoundedTranscriptRecords {
    param(
        [Parameter(Mandatory)][IO.StreamReader]$Reader,
        [ValidateRange(1, 10485760)][int]$MaximumCharacters = 10485760
    )

    $buffer = [char[]]::new(65536)
    $builder = [Text.StringBuilder]::new()
    $skippingOversized = $false
    while (($readCharacters = $Reader.Read($buffer, 0, $buffer.Length)) -gt 0) {
        $offset = 0
        while ($offset -lt $readCharacters) {
            $newline = [Array]::IndexOf($buffer, [char]10, $offset, $readCharacters - $offset)
            $segmentLength = if ($newline -ge 0) { $newline - $offset } else { $readCharacters - $offset }
            if (-not $skippingOversized) {
                if ($builder.Length + $segmentLength -gt $MaximumCharacters) {
                    [void]$builder.Clear()
                    $skippingOversized = $true
                } elseif ($segmentLength -gt 0) {
                    [void]$builder.Append($buffer, $offset, $segmentLength)
                }
            }

            if ($newline -ge 0) {
                if ($skippingOversized) {
                    [pscustomobject]@{ Text = ''; Oversized = $true }
                } else {
                    [pscustomobject]@{ Text = $builder.ToString().TrimEnd("`r"); Oversized = $false }
                }
                [void]$builder.Clear()
                $skippingOversized = $false
                $offset = $newline + 1
            } else {
                $offset += $segmentLength
            }
        }
    }

    if ($skippingOversized) {
        [pscustomobject]@{ Text = ''; Oversized = $true }
    } elseif ($builder.Length -gt 0) {
        [pscustomobject]@{ Text = $builder.ToString().TrimEnd("`r"); Oversized = $false }
    }
}
