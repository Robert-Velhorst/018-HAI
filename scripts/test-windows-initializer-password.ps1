[CmdletBinding()]
param([string]$InitializerPath = '')

$ErrorActionPreference = 'Stop'
if (-not $InitializerPath) { $InitializerPath = Join-Path $PSScriptRoot 'initialize-windows.ps1' }
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($InitializerPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw 'Initializer password test cannot parse its source.' }
$validation = @($ast.FindAll({
    param($node)
    $node -is [Management.Automation.Language.IfStatementAst] -and
    $node.Extent.Text.StartsWith('if (-not $MigrateExisting)')
}, $false))
if ($validation.Count -ne 1) { throw 'Expected one first-run input-validation block.' }
$validator = @($ast.FindAll({
    param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and
    $node.Name -eq 'Assert-HaiFirstRunOwnerPassword'
}, $false))
foreach ($definition in $validator) { . ([scriptblock]::Create($definition.Extent.Text)) }
$validationBlock = [scriptblock]::Create($validation[0].Extent.Text)

# Exercise the actual preflight block without launching the rest of the installer,
# creating credentials, changing ACLs, invoking Docker or writing fixture files.
$MigrateExisting = $false
$Force = $false
$EnvFile = Join-Path $PSScriptRoot ('.nonexistent-password-test-' + [Guid]::NewGuid().ToString('N'))
$AdminEmail = 'operator@example.test'
function Read-Host { throw 'Password preflight must not prompt in this test.' }
$astral = [char]::ConvertFromUtf32(0x1F680)
$accent = [string][char]0xE9
$cases = @(
    @{ Name = 'ASCII minimum'; Value = 'q' * 12; Valid = $true },
    @{ Name = 'ASCII bcrypt boundary'; Value = 'q' * 72; Valid = $true },
    @{ Name = 'Unicode bcrypt boundary'; Value = $accent * 36; Valid = $true },
    @{ Name = 'Astral scalar minimum'; Value = $astral * 12; Valid = $true },
    @{ Name = 'Literal interpolation characters'; Value = 'safe-$-#-value-123'; Valid = $true },
    @{ Name = 'Short ASCII'; Value = 'q' * 11; Valid = $false },
    @{ Name = 'ASCII over bcrypt boundary'; Value = 'q' * 73; Valid = $false },
    @{ Name = 'Unicode over bcrypt boundary'; Value = $accent * 37; Valid = $false },
    @{ Name = 'UTF16 units are not scalars'; Value = $astral * 6; Valid = $false },
    @{ Name = 'NUL cannot enter dotenv'; Value = 'q' * 12 + [char]0; Valid = $false },
    @{ Name = 'Single quote cannot enter dotenv'; Value = "qqqqqqqqqqqq'"; Valid = $false },
    @{ Name = 'Line break cannot enter dotenv'; Value = "qqqqqqqqqqqq`n"; Valid = $false },
    @{ Name = 'Leading whitespace'; Value = ' ' + 'q' * 12; Valid = $false },
    @{ Name = 'Trailing whitespace'; Value = 'q' * 12 + ' '; Valid = $false },
    @{ Name = 'Unicode boundary whitespace'; Value = [string][char]0x2003 + 'q' * 12; Valid = $false },
    @{ Name = 'Unpaired surrogate'; Value = [string][char]0xD800 + 'q' * 12; Valid = $false },
    @{ Name = 'Change-this placeholder'; Value = 'change-this-owner-value'; Valid = $false },
    @{ Name = 'Changeme placeholder'; Value = 'owner-CHANGEME-value'; Valid = $false },
    @{ Name = 'Placeholder marker'; Value = 'owner-placeholder-value'; Valid = $false },
    @{ Name = 'Example marker'; Value = 'owner-example-value'; Valid = $false }
)
$failures = New-Object 'System.Collections.Generic.List[string]'
foreach ($case in $cases) {
    $AdminPasswordPlainText = $case.Value
    $accepted = $true
    $output = ''
    try { $output = (& $validationBlock *>&1 | Out-String) } catch { $accepted = $false; $output = $_.Exception.Message }
    if ($accepted -ne $case.Valid) { $failures.Add($case.Name) }
    if ($output.Contains($case.Value)) { throw 'Password preflight exposed a supplied value.' }
    if ($AdminPasswordPlainText -cne $case.Value) { throw 'Password preflight changed a supplied value.' }
}
if ($failures.Count -gt 0) { throw ('Password preflight cases failed: ' + ($failures -join ', ')) }
Write-Host "Windows initializer password preflight passed all $($cases.Count) cases. No environment files were written."
