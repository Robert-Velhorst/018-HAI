Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$path = Join-Path $PSScriptRoot 'test-windows-native-runtime.ps1'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw 'Native acceptance runner does not parse.' }
$function = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Resolve-NativeAcceptanceGo' }, $true)
if (-not $function) { throw 'Native Go discovery helper missing.' }
# Load only the pure discovery helper, never runner initialization or builds.
. ([scriptblock]::Create($function.Extent.Text))
$script:commandSource = $null
$script:existingFiles = @{}
function Get-Command { param($Name, $CommandType, $ErrorAction) foreach ($source in $script:commandSource) { if ($source) { [pscustomobject]@{ Source = $source } } } }
function Test-Path { param($LiteralPath, $PathType) return $script:existingFiles.ContainsKey($LiteralPath) }
function Resolve-Path { param($LiteralPath, $ErrorAction) [pscustomobject]@{ Path = $LiteralPath } }
function Assert-Equal($Actual, $Expected) { if ($Actual -ne $Expected) { throw 'Native Go discovery returned the wrong path.' } }
function Assert-Rejected([scriptblock]$Action) {
    $rejected = $false
    try { & $Action | Out-Null } catch { $rejected = $true }
    if (-not $rejected) { throw 'Unsafe or unavailable toolchain was accepted.' }
}
$savedProfile = $env:USERPROFILE
try {
    $env:USERPROFILE = [IO.Path]::GetFullPath($PSScriptRoot)
    $explicit = Join-Path $env:USERPROFILE 'synthetic-go.exe'
    $cached = Join-Path $env:USERPROFILE 'go\pkg\mod\golang.org\toolchain@v0.0.1-go1.27.2.windows-amd64\bin\go.exe'
    $script:existingFiles[$explicit] = $true
    $script:existingFiles[$cached] = $true
    $script:commandSource = 'path-go.exe'
    Assert-Equal (Resolve-NativeAcceptanceGo $explicit) $explicit
    Assert-Equal (Resolve-NativeAcceptanceGo '') 'path-go.exe'
    $script:commandSource = @('first-go.exe', 'second-go.exe')
    Assert-Equal (Resolve-NativeAcceptanceGo '') 'first-go.exe'
    $script:commandSource = $null
    Assert-Equal (Resolve-NativeAcceptanceGo '') $cached
    Assert-Rejected { Resolve-NativeAcceptanceGo 'relative-go.exe' }
    Assert-Rejected { Resolve-NativeAcceptanceGo (Join-Path $env:USERPROFILE 'missing-go.exe') }
    $script:existingFiles.Clear()
    Assert-Rejected { Resolve-NativeAcceptanceGo '' }
    $source = [IO.File]::ReadAllText($path)
    foreach ($required in @("'GOMAXPROCS', 'GOMEMLIMIT'", "`$env:GOMAXPROCS = '1'", "`$env:GOMEMLIMIT = '384MiB'", "@('test', '-p', '1', '-mod=readonly'", "'--cpus', '1', '--memory', '512m', '--memory-swap', '512m', '--pids-limit', '128'", 'An explicit native Go executable cannot be combined')) {
        if (-not $source.Contains($required)) { throw "Missing native runner resource/discovery contract: $required" }
    }
    Write-Host 'Native Go discovery: seven mocked cases and resource configuration contracts passed; no toolchain or runner executed.'
} finally {
    [Environment]::SetEnvironmentVariable('USERPROFILE', $savedProfile, 'Process')
}
