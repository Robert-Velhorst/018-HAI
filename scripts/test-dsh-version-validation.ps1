$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$supportScript = Join-Path $repositoryRoot "installer\windows\Hai-InstallerSupport.ps1"
$tokens = $null
$parseErrors = $null
[Management.Automation.Language.Parser]::ParseFile($supportScript, [ref]$tokens, [ref]$parseErrors) | Out-Null
if ($parseErrors.Count -gt 0) {
    throw "PowerShell syntax error in ${supportScript}: $($parseErrors[0].Message)"
}

. $supportScript

$cases = @(
    @{ Name = "official bare prerelease token"; Expected = "0.1.7-alpha.2"; Reported = "0.1.7-alpha.2`r`n"; Matches = $true },
    @{ Name = "optional v prefix on pin"; Expected = "v0.1.7-alpha.2"; Reported = "0.1.7-alpha.2"; Matches = $true },
    @{ Name = "optional v prefix on output"; Expected = "0.1.7-alpha.2"; Reported = "v0.1.7-alpha.2`n"; Matches = $true },
    @{ Name = "exact build metadata"; Expected = "1.2.3+build.1"; Reported = "1.2.3+build.1"; Matches = $true },
    @{ Name = "prerelease suffix impostor"; Expected = "0.1.7-alpha.2"; Reported = "0.1.7-alpha.20"; Matches = $false },
    @{ Name = "text prefix impostor"; Expected = "0.1.7"; Reported = "not-0.1.7"; Matches = $false },
    @{ Name = "version suffix impostor"; Expected = "0.1.7"; Reported = "0.1.7-malicious"; Matches = $false },
    @{ Name = "decorated CLI output"; Expected = "0.1.7"; Reported = "dsh 0.1.7"; Matches = $false },
    @{ Name = "trailing output"; Expected = "0.1.7"; Reported = "0.1.7 extra"; Matches = $false },
    @{ Name = "multiple output lines"; Expected = "0.1.7"; Reported = "0.1.7`n0.1.7"; Matches = $false },
    @{ Name = "wildcard pin"; Expected = "0.1.*"; Reported = "0.1.7"; Matches = $false },
    @{ Name = "leading-zero pin"; Expected = "01.1.7"; Reported = "01.1.7"; Matches = $false },
    @{ Name = "build metadata mismatch"; Expected = "1.2.3+build.1"; Reported = "1.2.3+build.2"; Matches = $false },
    @{ Name = "case-sensitive prerelease mismatch"; Expected = "1.2.3-alpha"; Reported = "1.2.3-ALPHA"; Matches = $false }
)

foreach ($case in $cases) {
    $matches = Test-HaiDshVersionMatch -Expected $case.Expected -Reported $case.Reported
    if ($matches -ne $case.Matches) {
        throw "DeepSeek Harness version validation case '$($case.Name)' returned '$matches'; expected '$($case.Matches)'."
    }
}

Write-Host "DeepSeek Harness version validation tests passed ($($cases.Count) cases)."
