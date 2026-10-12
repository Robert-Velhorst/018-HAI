[CmdletBinding()]
param([string]$UnsignedFixturePath = '')

$ErrorActionPreference = 'Stop'
# Load function-exporting modules before defining doubles (notably the 5.1
# Get-FileHash implementation), so module autoload cannot replace a test double.
Import-Module Microsoft.PowerShell.Utility -ErrorAction Stop
$buildPath = Join-Path $PSScriptRoot 'build-windows-installer.ps1'
$tokens = $null
$parseErrors = $null
$buildAst = [Management.Automation.Language.Parser]::ParseFile($buildPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw "Build script syntax error: $($parseErrors[0].Message)" }
$buildSource = [IO.File]::ReadAllText($buildPath)

# Load only real signing functions, never the build/staging/toolchain entry point.
$functionNames = @(
    'Resolve-HaiInstallerSigningPolicy', 'Invoke-HaiInstallerSignTool',
    'Assert-HaiInstallerAuthenticode', 'Protect-HaiInstallerProductionFile',
    'ConvertTo-HaiInnoQuotedArgument', 'Get-HaiInnoSigningCommand',
    'Copy-HaiInstallerEvidenceBytes', 'Invoke-HaiInnoSigningCallback', 'Assert-HaiInnoSignedUninstaller'
)
foreach ($name in $functionNames) {
    $definition = $buildAst.Find({ param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name
    }, $true)
    if ($null -eq $definition) { throw "Missing production signing function: $name" }
    . ([ScriptBlock]::Create($definition.Extent.Text))
}

$script:passed = 0
$script:pin = '0123456789ABCDEF0123456789ABCDEF01234567'
$script:fixturePath = 'C:\hai-signing-fixture\worker.exe'
$script:toolPath = 'C:\hai-signing-fixture\signtool.exe'
$script:hash = 'A' * 64
$script:uninstallerDirectory = 'C:\hai-signing-fixture\signed-uninstaller'
$script:uninstallerPath = Join-Path $script:uninstallerDirectory 'uninst-fixture.exe'
$script:retainedUninstallerPath = Join-Path $script:uninstallerDirectory 'HAI-signed-uninstaller.exe'

function Reset-SigningDoubles {
    $script:certificate = [pscustomobject]@{
        Thumbprint = $script:pin
        HasPrivateKey = $true
        NotBefore = [DateTime]::Now.AddDays(-1)
        NotAfter = [DateTime]::Now.AddDays(1)
        EnhancedKeyUsageList = @([pscustomobject]@{ ObjectId = '1.3.6.1.5.5.7.3.3' })
    }
    $script:signature = [pscustomobject]@{
        Status = 'Valid'
        SignatureType = 'Authenticode'
        SignerCertificate = [pscustomobject]@{ Thumbprint = $script:pin }
        TimeStamperCertificate = [pscustomobject]@{ Thumbprint = 'B' * 40 }
    }
    $script:missingPath = ''
    $script:certificateMissing = $false
    $script:signatureThrows = $false
    $script:toolExitCode = 0
    $script:toolCalls = New-Object 'System.Collections.Generic.List[object]'
    $script:signatureReads = 0
    $script:signaturePaths = New-Object 'System.Collections.Generic.List[string]'
    $script:compileExitCode = 0
    $script:compilerCalls = 0
    $script:evidenceCopies = New-Object 'System.Collections.Generic.List[object]'
    $script:uninstallerOutputs = @([pscustomobject]@{
        FullName = $script:uninstallerPath
        PSIsContainer = $false
        Attributes = [IO.FileAttributes]::Normal
    })
    $script:policy = [pscustomobject]@{
        SignToolPath = 'Invoke-HaiFakeSignTool'
        Thumbprint = $script:pin
        TimestampServer = 'https://timestamp.example.invalid/'
    }
}

function Test-Path {
    param([string]$LiteralPath, [string]$PathType)
    if ($LiteralPath -in @($script:toolPath, $script:fixturePath, $script:uninstallerDirectory, $script:uninstallerPath, $script:retainedUninstallerPath)) { return $LiteralPath -ne $script:missingPath }
    return Microsoft.PowerShell.Management\Test-Path -LiteralPath $LiteralPath -PathType $PathType
}

function Get-Item {
    param([string]$LiteralPath, [string]$ErrorAction)
    if ($LiteralPath -like 'Cert:\CurrentUser\My\*') {
        if ($script:certificateMissing) { throw 'Fixture certificate is missing.' }
        return $script:certificate
    }
    throw "Unexpected Get-Item in isolated signing test: $LiteralPath"
}

function Get-AuthenticodeSignature {
    param([string]$LiteralPath, [string]$ErrorAction)
    $script:signatureReads++
    $script:signaturePaths.Add($LiteralPath)
    if ($script:signatureThrows) { throw 'Fixture signature inspection failed.' }
    return $script:signature
}

function Get-FileHash {
    param([string]$LiteralPath, [string]$Algorithm, [string]$ErrorAction)
    if ($Algorithm -ne 'SHA256') { throw 'Expected SHA256 output hash.' }
    return [pscustomobject]@{ Hash = $script:hash }
}

# The copy is isolated; production uses File.Copy with overwrite disabled.
function Copy-HaiInstallerEvidenceBytes {
    param([string]$Source, [string]$Destination)
    $script:evidenceCopies.Add(@($Source, $Destination))
}

function Invoke-HaiFakeSignTool {
    $script:toolCalls.Add(@($args))
    $global:LASTEXITCODE = $script:toolExitCode
}

function Get-ChildItem {
    param([string]$LiteralPath, [switch]$Force, [string]$ErrorAction)
    if ($LiteralPath -ne $script:uninstallerDirectory) { throw 'Unexpected directory enumeration in signing test.' }
    return $script:uninstallerOutputs
}

function Invoke-WithInnoSigningEnvironment {
    param([scriptblock]$Action)

    $values = @{
        HAI_INSTALLER_PRODUCTION_SIGNING = '1'
        HAI_INSTALLER_SIGNED_UNINSTALLER_DIR = $script:uninstallerDirectory
        HAI_INSTALLER_OUTPUT_DIR = 'C:\hai-signing-fixture'
        HAI_INSTALLER_SIGNTOOL_PATH = $script:toolPath
        HAI_INSTALLER_SIGNING_THUMBPRINT = $script:pin
        HAI_INSTALLER_TIMESTAMP_SERVER = 'https://timestamp.example.invalid/'
    }
    $previous = @{}
    foreach ($name in $values.Keys) {
        $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
    }
    try {
        foreach ($name in $values.Keys) {
            [Environment]::SetEnvironmentVariable($name, $values[$name], 'Process')
        }
        & $Action
    } finally {
        foreach ($name in $values.Keys) {
            [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process')
        }
    }
}

function Invoke-FixtureCompilerTail {
    param([bool]$Production = $true, [switch]$ChangeUninstallerBytes)

    function Test-Path { param([string]$LiteralPath, [string]$PathType) return $true }
    function New-Item { param([string]$ItemType, [string]$Path, [string]$ErrorAction) }
    function Invoke-HaiFakeCompiler {
        $script:compilerCalls++
        $script:compilerArguments = @($args)
        $script:compilerProduction = $env:HAI_INSTALLER_PRODUCTION_SIGNING
        $script:compilerUninstallerDirectory = $env:HAI_INSTALLER_SIGNED_UNINSTALLER_DIR
        $script:compilerTool = $env:HAI_INSTALLER_SIGNTOOL_PATH
        Write-Output "fixture signing output $script:toolPath $script:pin https://timestamp.example.invalid/"
        $global:LASTEXITCODE = $script:compileExitCode
    }
    function Get-ChildItem {
        param([string]$LiteralPath, [switch]$Force, [string]$ErrorAction)
        # The actual compiler tail chooses the isolated output directory.
        foreach ($output in $script:uninstallerOutputs) {
            $output.FullName = Join-Path $LiteralPath 'uninst-fixture.exe'
        }
        return $script:uninstallerOutputs
    }
    function Get-FileHash {
        param([string]$LiteralPath, [string]$Algorithm, [string]$ErrorAction)
        if ($ChangeUninstallerBytes -and $LiteralPath -match 'uninst-fixture.exe$') {
            $script:uninstallerHashReads++
            if ($script:uninstallerHashReads -gt 1) { return [pscustomobject]@{ Hash = 'C' * 64 } }
        }
        return [pscustomobject]@{ Hash = $script:hash }
    }
    $script:uninstallerHashReads = 0
    $Version = 'signing-test'
    $buildId = 'synthetic'
    $OutputDirectory = 'C:\hai-signing-fixture'
    $installerPath = Join-Path $OutputDirectory 'HAI-Setup-signing-test.exe'
    $installerScript = 'C:\hai-signing-fixture\HAI.iss'
    $ISCCPath = 'Invoke-HaiFakeCompiler'
    $signingPolicy = $script:policy
    $repositoryRoot = 'C:\hai-signing-fixture'
    . ([ScriptBlock]::Create($buildSource.Substring($buildSource.IndexOf('$compileOutputDirectory = $OutputDirectory'))))
}

function Assert-SigningTest {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Assert-SigningThrows {
    param([scriptblock]$Action, [string]$MessagePattern)
    $failure = $null
    try { & $Action | Out-Null } catch { $failure = $_.Exception.Message }
    if ($null -eq $failure -or $failure -notmatch $MessagePattern) {
        throw "Expected failure matching '$MessagePattern', got '$failure'."
    }
}

function Test-SigningCase {
    param([string]$Name, [scriptblock]$Action)
    Reset-SigningDoubles
    & $Action
    $script:passed++
    Write-Host "PASS $Name"
}

function Get-FixtureProductionArguments {
    return @{
        Production = $true
        SignToolPath = $script:toolPath
        SigningCertificateThumbprint = $script:pin
        TimestampServer = 'https://timestamp.example.invalid/'
    }
}

Test-SigningCase 'default developer preview needs no certificate' {
    Assert-SigningTest ($null -eq (Resolve-HaiInstallerSigningPolicy)) 'Default must remain a preview.'
}
Test-SigningCase 'dirty source-only developer preview remains supported' {
    Assert-SigningTest ($null -eq (Resolve-HaiInstallerSigningPolicy -AllowDirtyWorktree -SkipCompile)) 'Preview switches must remain supported.'
}
foreach ($option in @('SignToolPath', 'SigningCertificateThumbprint', 'TimestampServer')) {
    Test-SigningCase "preview refuses accidentally supplied $option" {
        $arguments = @{}
        $arguments[$option] = 'provided'
        Assert-SigningThrows { Resolve-HaiInstallerSigningPolicy @arguments } 'Signing options require -Production'
    }
}
foreach ($option in @('AllowDirtyWorktree', 'SkipCompile')) {
    Test-SigningCase "production forbids $option" {
        $arguments = Get-FixtureProductionArguments
        $arguments[$option] = $true
        Assert-SigningThrows { Resolve-HaiInstallerSigningPolicy @arguments } 'Production signing forbids'
    }
}
foreach ($invalidPin in @('', 'garbage', ('A' * 39), ('A' * 41), ('G' * 40), ($script:pin + ';exit'))) {
    Test-SigningCase "production rejects invalid pin '$invalidPin'" {
        $arguments = Get-FixtureProductionArguments
        $arguments.SigningCertificateThumbprint = $invalidPin
        Assert-SigningThrows { Resolve-HaiInstallerSigningPolicy @arguments } '40-hex'
    }
}
foreach ($path in @('', 'signtool.exe', 'C:signtool.exe', '\signtool.exe', 'C:\hai-signing-fixture\tool.ps1')) {
    Test-SigningCase "production rejects non-explicit executable '$path'" {
        function Test-Path { param([string]$LiteralPath, [string]$PathType) return $true }
        $arguments = Get-FixtureProductionArguments
        $arguments.SignToolPath = $path
        Assert-SigningThrows { Resolve-HaiInstallerSigningPolicy @arguments } 'absolute path'
    }
}
Test-SigningCase 'production rejects missing SignTool' {
    $script:missingPath = $script:toolPath
    $arguments = Get-FixtureProductionArguments
    Assert-SigningThrows { Resolve-HaiInstallerSigningPolicy @arguments } 'absolute path'
}
foreach ($url in @('', 'not-a-url', 'http://timestamp.example.invalid', 'file:///C:/timestamp',
    'https://user:password@timestamp.example.invalid', 'https://timestamp.example.invalid/#fragment')) {
    Test-SigningCase "production rejects timestamp URL '$url'" {
        $arguments = Get-FixtureProductionArguments
        $arguments.TimestampServer = $url
        Assert-SigningThrows { Resolve-HaiInstallerSigningPolicy @arguments } 'HTTPS RFC 3161'
    }
}
Test-SigningCase 'production rejects missing certificate' {
    $script:certificateMissing = $true
    $arguments = Get-FixtureProductionArguments
    Assert-SigningThrows { Resolve-HaiInstallerSigningPolicy @arguments } 'certificate is missing'
}
foreach ($state in @('wrong-pin', 'no-private-key', 'not-yet-valid', 'expired', 'wrong-EKU', 'no-EKU')) {
    Test-SigningCase "production rejects certificate $state" {
        switch ($state) {
            'wrong-pin' { $script:certificate.Thumbprint = 'C' * 40 }
            'no-private-key' { $script:certificate.HasPrivateKey = $false }
            'not-yet-valid' { $script:certificate.NotBefore = [DateTime]::Now.AddDays(1) }
            'expired' { $script:certificate.NotAfter = [DateTime]::Now.AddDays(-1) }
            'wrong-EKU' { $script:certificate.EnhancedKeyUsageList = @([pscustomobject]@{ ObjectId = '1.3.6.1.5.5.7.3.1' }) }
            'no-EKU' { $script:certificate.EnhancedKeyUsageList = @() }
        }
        $arguments = Get-FixtureProductionArguments
        Assert-SigningThrows { Resolve-HaiInstallerSigningPolicy @arguments } 'pinned signing certificate must'
    }
}
Test-SigningCase 'production normalizes an explicit certificate pin' {
    $arguments = Get-FixtureProductionArguments
    $arguments.SigningCertificateThumbprint = " $($script:pin.ToLowerInvariant()) "
    $actual = Resolve-HaiInstallerSigningPolicy @arguments
    Assert-SigningTest ($actual.Thumbprint -ceq $script:pin) 'Pin normalization failed.'
    Assert-SigningTest ($actual.SignToolPath -eq $script:toolPath) 'Unexpected tool path.'
}
Test-SigningCase 'valid signed output has independent verification and byte hash' {
    $actual = Protect-HaiInstallerProductionFile -Path $script:fixturePath -Policy $script:policy
    Assert-SigningTest ($script:toolCalls.Count -eq 2) 'Expected sign then independent verify.'
    Assert-SigningTest (($script:toolCalls[0] -join '|') -eq "sign|/s|My|/sha1|$($script:pin)|/fd|SHA256|/tr|$($script:policy.TimestampServer)|/td|SHA256|$($script:fixturePath)") 'Signing arguments must pin identity and SHA256/RFC3161 algorithms.'
    Assert-SigningTest (($script:toolCalls[1] -join '|') -eq "verify|/pa|/all|/tw|$($script:fixturePath)") 'Verification must use embedded Authenticode policy with timestamps.'
    Assert-SigningTest ($script:signatureReads -eq 1 -and $actual.sha256 -eq $script:hash) 'Missing actual-output inspection/hash.'
}
foreach ($exitCode in @(1, 2)) {
    Test-SigningCase "signing exit $exitCode is rejected before verification" {
        $script:toolExitCode = $exitCode
        Assert-SigningThrows { Protect-HaiInstallerProductionFile -Path $script:fixturePath -Policy $script:policy } 'failed or returned warnings'
        Assert-SigningTest ($script:signatureReads -eq 0 -and $script:toolCalls.Count -eq 1) 'Sign failure must stop immediately.'
    }
    Test-SigningCase "verification exit $exitCode is rejected" {
        $script:toolExitCode = $exitCode
        Assert-SigningThrows { Assert-HaiInstallerAuthenticode -Path $script:fixturePath -Policy $script:policy } 'failed or returned warnings'
    }
}
foreach ($status in @('NotSigned', 'HashMismatch', 'NotTrusted', 'UnknownError', 'NotSupportedFileFormat', 'Incompatible')) {
    Test-SigningCase "successful signer exit cannot hide Authenticode $status" {
        $script:signature.Status = $status
        Assert-SigningThrows { Protect-HaiInstallerProductionFile -Path $script:fixturePath -Policy $script:policy } 'Authenticode verification failed'
        Assert-SigningTest ($script:toolCalls.Count -eq 1) 'Invalid signature must not reach further verification.'
    }
}
foreach ($state in @('catalog-only', 'no-signer', 'wrong-signer', 'no-timestamp')) {
    Test-SigningCase "valid status cannot bypass $state" {
        switch ($state) {
            'catalog-only' { $script:signature.SignatureType = 'Catalog' }
            'no-signer' { $script:signature.SignerCertificate = $null }
            'wrong-signer' { $script:signature.SignerCertificate.Thumbprint = 'C' * 40 }
            'no-timestamp' { $script:signature.TimeStamperCertificate = $null }
        }
        Assert-SigningThrows { Protect-HaiInstallerProductionFile -Path $script:fixturePath -Policy $script:policy } 'Authenticode verification failed'
    }
}
Test-SigningCase 'missing signing input is rejected before SignTool' {
    $script:missingPath = $script:fixturePath
    Assert-SigningThrows { Protect-HaiInstallerProductionFile -Path $script:fixturePath -Policy $script:policy } 'Missing production signing input'
    Assert-SigningTest ($script:toolCalls.Count -eq 0) 'Missing file must not reach SignTool.'
}
Test-SigningCase 'missing verification output is rejected' {
    $script:missingPath = $script:fixturePath
    Assert-SigningThrows { Assert-HaiInstallerAuthenticode -Path $script:fixturePath -Policy $script:policy } 'Missing production signing output'
}
Test-SigningCase 'signature inspection errors are not swallowed' {
    $script:signatureThrows = $true
    Assert-SigningThrows { Protect-HaiInstallerProductionFile -Path $script:fixturePath -Policy $script:policy } 'signature inspection failed'
}
Test-SigningCase 'signer console output never leaks into signing results' {
    function Invoke-HaiFakeSignTool {
        $script:toolCalls.Add(@($args))
        Write-Output 'PRIVATE-SIGNING-PATH-SENTINEL'
        $global:LASTEXITCODE = 0
    }
    $actual = @(Protect-HaiInstallerProductionFile -Path $script:fixturePath -Policy $script:policy)
    Assert-SigningTest ($actual.Count -eq 1 -and $actual[0].sha256 -eq $script:hash) 'Raw signer output must be discarded, not returned or logged.'
}
Test-SigningCase 'signer launch errors redact private executable paths' {
    function Invoke-HaiFakeSignTool { throw 'PRIVATE-SIGNING-PATH-SENTINEL' }
    Assert-SigningThrows { Protect-HaiInstallerProductionFile -Path $script:fixturePath -Policy $script:policy } '^SignTool could not execute; production signing output is not accepted\.$'
}

Test-SigningCase 'Inno callback command quotes spaces and escapes dollar expansions without credentials' {
    $command = Get-HaiInnoSigningCommand -PowerShellPath 'C:\Program Files\PowerShell\powershell.exe' `
        -BuildScriptPath 'C:\source $q & (review)\build.ps1'
    $expected = '/Shai=$qC:\Program Files\PowerShell\powershell.exe$q -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $qC:\source $$q & (review)\build.ps1$q -InnoSigningTarget $f'
    Assert-SigningTest ($command -ceq $expected) 'Inno tokens must quote each argument and escape literal dollar signs.'
    Assert-SigningTest ($command -notmatch 'cmd.exe|/sha1|/tr|signtool|timestamp|01234567') 'Signing configuration must not appear in compiler arguments.'
}
foreach ($value in @('C:\bad"path\build.ps1', "C:\bad`rpath", "C:\bad`npath", "C:\bad$([char]0)path", 'C:\trailing\')) {
    Test-SigningCase 'Inno callback rejects unrepresentable command arguments' {
        Assert-SigningThrows { ConvertTo-HaiInnoQuotedArgument $value } 'Unsupported character'
    }
}
Test-SigningCase 'Inno callback requires an active production compilation' {
    Invoke-WithInnoSigningEnvironment {
        $env:HAI_INSTALLER_PRODUCTION_SIGNING = $null
        Assert-SigningThrows { Invoke-HaiInnoSigningCallback -Path $script:uninstallerPath } 'active production'
        Assert-SigningTest ($script:toolCalls.Count -eq 0) 'Preview callback must never sign.'
    }
}
foreach ($target in @('C:\elsewhere\uninst.exe', 'C:\hai-signing-fixture\signed-uninstaller-extra\uninst.exe',
    'C:\hai-signing-fixture\signed-uninstaller\..\..\uninst.exe', 'relative.exe', 'C:uninst.exe', '\uninst.exe')) {
    Test-SigningCase "Inno callback refuses out-of-compilation target '$target'" {
        Invoke-WithInnoSigningEnvironment {
            Assert-SigningThrows { Invoke-HaiInnoSigningCallback -Path $target } 'outside this production'
            Assert-SigningTest ($script:toolCalls.Count -eq 0) 'An unrelated file must never reach signing.'
        }
    }
}
Test-SigningCase 'Inno callback signs and independently verifies its exact generated target' {
    Invoke-WithInnoSigningEnvironment {
        function Resolve-HaiInstallerSigningPolicy {
            param([switch]$Production, [string]$SignToolPath, [string]$SigningCertificateThumbprint, [string]$TimestampServer)
            Assert-SigningTest ($Production -and $SignToolPath -eq $script:toolPath -and
                $SigningCertificateThumbprint -eq $script:pin -and $TimestampServer -eq $script:policy.TimestampServer) 'Callback must receive the inherited production policy.'
            return $script:policy
        }
        Invoke-HaiInnoSigningCallback -Path $script:uninstallerPath
        $retained = Join-Path $script:uninstallerDirectory 'HAI-signed-uninstaller.exe'
        Assert-SigningTest ($script:toolCalls.Count -eq 3 -and $script:signaturePaths[0] -eq $script:uninstallerPath -and $script:signaturePaths[1] -eq $retained) 'Callback must verify both the compiler target and the retained signed bytes.'
        Assert-SigningTest ($script:evidenceCopies.Count -eq 1 -and $script:evidenceCopies[0][0] -eq $script:uninstallerPath -and $script:evidenceCopies[0][1] -eq $retained) 'Retain the exact signed uninstaller before Inno removes its temporary signing target.'
    }
}
Test-SigningCase 'Inno callback refuses successful signer exit with an unsigned generated target' {
    Invoke-WithInnoSigningEnvironment {
        function Resolve-HaiInstallerSigningPolicy { return $script:policy }
        $script:signature.Status = 'NotSigned'
        Assert-SigningThrows { Invoke-HaiInnoSigningCallback -Path $script:uninstallerPath } 'Authenticode verification failed'
        Assert-SigningTest ($script:toolCalls.Count -eq 1) 'Compiler callback must fail instead of accepting a signer-only success.'
    }
}
Test-SigningCase 'Inno callback rejects changed retained uninstaller bytes' {
    Invoke-WithInnoSigningEnvironment {
        function Resolve-HaiInstallerSigningPolicy { return $script:policy }
        function Copy-HaiInstallerEvidenceBytes { $script:hash = 'B' * 64 }
        Assert-SigningThrows { Invoke-HaiInnoSigningCallback -Path $script:uninstallerPath } 'differs from the compiler signing target'
    }
}
Test-SigningCase 'Inno callback does not overwrite prior retained evidence' {
    Invoke-WithInnoSigningEnvironment {
        function Resolve-HaiInstallerSigningPolicy { return $script:policy }
        function Copy-HaiInstallerEvidenceBytes { throw 'Existing retained evidence must not be overwritten.' }
        Assert-SigningThrows { Invoke-HaiInnoSigningCallback -Path $script:uninstallerPath } 'must not be overwritten'
        Assert-SigningTest ($script:signatureReads -eq 1) 'A copy conflict must stop before accepting retained evidence.'
    }
    $copy = $buildAst.Find({ param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Copy-HaiInstallerEvidenceBytes'
    }, $true)
    Assert-SigningTest ($copy.Extent.Text -match '\[IO\.File\]::Copy\(\$Source, \$Destination, \$false\)') 'Production evidence copying must use the no-overwrite native contract.'
}
Test-SigningCase 'generated uninstaller requires independent exact signature and hash inspection' {
    $actual = Assert-HaiInnoSignedUninstaller -Directory $script:uninstallerDirectory -Policy $script:policy
    Assert-SigningTest ($actual.Path -eq $script:uninstallerPath -and $actual.Signature.sha256 -eq $script:hash) 'Missing generated uninstaller hash evidence.'
    Assert-SigningTest ($script:signaturePaths.Count -eq 1 -and $script:signaturePaths[0] -eq $script:uninstallerPath) 'Must inspect the exact compiler-retained output.'
    Assert-SigningTest ($script:toolCalls.Count -eq 1 -and ($script:toolCalls[0] -join '|') -eq "verify|/pa|/all|/tw|$($script:uninstallerPath)") 'Post-compile uninstaller verification must not re-sign or accept signer exit alone.'
}
Test-SigningCase 'generated uninstaller directory must exist' {
    $script:missingPath = $script:uninstallerDirectory
    Assert-SigningThrows { Assert-HaiInnoSignedUninstaller -Directory $script:uninstallerDirectory -Policy $script:policy } 'Missing production signed-uninstaller'
}
foreach ($state in @('empty', 'multiple', 'directory', 'reparse-point', 'wrong-directory', 'missing-file')) {
    Test-SigningCase "generated uninstaller rejects $state output" {
        switch ($state) {
            'empty' { $script:uninstallerOutputs = @() }
            'multiple' { $script:uninstallerOutputs = @($script:uninstallerOutputs[0], $script:uninstallerOutputs[0]) }
            'directory' { $script:uninstallerOutputs[0].PSIsContainer = $true }
            'reparse-point' { $script:uninstallerOutputs[0].Attributes = [IO.FileAttributes]::ReparsePoint }
            'wrong-directory' { $script:uninstallerOutputs[0].FullName = $script:fixturePath }
            'missing-file' { $script:missingPath = $script:uninstallerPath }
        }
        Assert-SigningThrows { Assert-HaiInnoSignedUninstaller -Directory $script:uninstallerDirectory -Policy $script:policy } 'exactly one regular|Missing production signing output'
        Assert-SigningTest ($script:toolCalls.Count -eq 0) 'Invalid generated output must not reach verification.'
    }
}
foreach ($state in @('unsigned', 'wrong-signer', 'catalog-only', 'no-timestamp', 'hash-mismatch', 'verifier-warning')) {
    Test-SigningCase "generated uninstaller independently rejects $state" {
        switch ($state) {
            'unsigned' { $script:signature.Status = 'NotSigned' }
            'wrong-signer' { $script:signature.SignerCertificate.Thumbprint = 'C' * 40 }
            'catalog-only' { $script:signature.SignatureType = 'Catalog' }
            'no-timestamp' { $script:signature.TimeStamperCertificate = $null }
            'hash-mismatch' { $script:signature.Status = 'HashMismatch' }
            'verifier-warning' { $script:toolExitCode = 2 }
        }
        Assert-SigningThrows { Assert-HaiInnoSignedUninstaller -Directory $script:uninstallerDirectory -Policy $script:policy } 'Authenticode verification failed|failed or returned warnings'
    }
}
Test-SigningCase 'Inno source selects a real production hook and explicitly unsigned preview' {
    $innoSource = [IO.File]::ReadAllText((Join-Path $PSScriptRoot '..\installer\windows\HAI.iss'))
    Assert-SigningTest ($innoSource -match '(?s)#if GetEnv\("HAI_INSTALLER_PRODUCTION_SIGNING"\) == "1".*SignTool=hai.*SignedUninstaller=yes.*SignedUninstallerDir=\{#GetEnv\("HAI_INSTALLER_SIGNED_UNINSTALLER_DIR"\)\}.*#else\s+SignedUninstaller=no\s+#endif') 'Production uninstaller signing and preview branches must remain explicit.'
    $callbackOffset = $buildSource.IndexOf("if (`$PSBoundParameters.ContainsKey('InnoSigningTarget'))")
    Assert-SigningTest ($callbackOffset -ge 0 -and $callbackOffset -lt $buildSource.IndexOf('$signingPolicy = Resolve-HaiInstallerSigningPolicy')) 'Compiler callback must exit before build entry point/staging.'
    $callbackBranch = $buildSource.Substring($callbackOffset, $buildSource.IndexOf('$signingPolicy = Resolve-HaiInstallerSigningPolicy') - $callbackOffset)
    Assert-SigningTest ($callbackBranch -match 'exit 0' -and $callbackBranch -match 'exit 1' -and $callbackBranch -notmatch '\$_\.Exception') 'Callback must provide fatal process failure without private exception output.'
    $tail = $buildSource.Substring($buildSource.IndexOf('$compileOutputDirectory = $OutputDirectory'))
    Assert-SigningTest ($tail.IndexOf('Assert-HaiInnoSignedUninstaller') -lt $tail.IndexOf('Protect-HaiInstallerProductionFile') -and
        $tail.IndexOf('uninstaller bytes changed') -lt $tail.IndexOf('[IO.File]::Move')) 'Independent generated uninstaller acceptance and hash recheck must precede Setup promotion.'
    Assert-SigningTest ($tail -match '& \$ISCCPath @compilerArguments \$installerScript 2>&1 \| Out-Null') 'Production compiler output must not leak signer paths/configuration.'
}
Test-SigningCase 'successful compiler exit cannot hide a missing generated uninstaller' {
    $script:uninstallerOutputs = @()
    Invoke-WithInnoSigningEnvironment {
        $before = $env:HAI_INSTALLER_SIGNTOOL_PATH
        Assert-SigningThrows { Invoke-FixtureCompilerTail } 'exactly one regular'
        Assert-SigningTest ($script:compilerCalls -eq 1 -and $script:toolCalls.Count -eq 0) 'Missing uninstaller must stop before Setup signing/promotion.'
        Assert-SigningTest ($env:HAI_INSTALLER_SIGNTOOL_PATH -eq $before) 'Compiler signing environment must be restored after rejection.'
    }
}
Test-SigningCase 'compiler tail rejects generated uninstaller bytes changed after verification' {
    Invoke-WithInnoSigningEnvironment {
        Assert-SigningThrows { Invoke-FixtureCompilerTail -ChangeUninstallerBytes } 'uninstaller bytes changed'
        Assert-SigningTest ($script:toolCalls.Count -eq 3 -and $script:signatureReads -eq 2) 'Uninstaller independently verified before Setup sign/verify and byte rejection.'
        Assert-SigningTest ($script:compilerArguments[0] -like '/Shai=*' -and
            $script:compilerProduction -eq '1' -and $script:compilerUninstallerDirectory -match '\.production-synthetic\.staging' -and
            $script:compilerTool -eq $script:policy.SignToolPath) 'Compiler must use an isolated production hook/cache and inherited signing policy.'
    }
}
Test-SigningCase 'compiler failure restores every inherited signing environment setting' {
    $script:compileExitCode = 1
    Invoke-WithInnoSigningEnvironment {
        $names = @('HAI_INSTALLER_VERSION', 'HAI_INSTALLER_OUTPUT_DIR', 'HAI_INSTALLER_PRODUCTION_SIGNING',
            'HAI_INSTALLER_SIGNED_UNINSTALLER_DIR', 'HAI_INSTALLER_SIGNTOOL_PATH', 'HAI_INSTALLER_SIGNING_THUMBPRINT', 'HAI_INSTALLER_TIMESTAMP_SERVER')
        $before = @{}
        foreach ($name in $names) { $before[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
        $failure = $null
        try { Invoke-FixtureCompilerTail | Out-Null } catch { $failure = $_.Exception.Message }
        Assert-SigningTest ($failure -match 'production compilation failed' -and
            $failure -notmatch [Regex]::Escape($script:toolPath) -and
            $failure -notmatch [Regex]::Escape($script:pin) -and
            $failure -notmatch 'timestamp\.example') 'Production compiler failure must fail closed without exposing signing configuration.'
        foreach ($name in $names) {
            Assert-SigningTest ([Environment]::GetEnvironmentVariable($name, 'Process') -eq $before[$name]) "Compiler environment leaked: $name"
        }
        Assert-SigningTest ($script:toolCalls.Count -eq 0) 'Compiler failure must stop signing acceptance.'
    }
}
Test-SigningCase 'preview compiler clears ambient signing configuration and restores it without signing' {
    Invoke-WithInnoSigningEnvironment {
        Invoke-FixtureCompilerTail -Production $false
        Assert-SigningTest ($script:compilerArguments.Count -eq 1 -and [string]::IsNullOrEmpty($script:compilerProduction) -and
            [string]::IsNullOrEmpty($script:compilerUninstallerDirectory) -and [string]::IsNullOrEmpty($script:compilerTool)) 'Preview must not inherit a production hook/configuration.'
        Assert-SigningTest ($env:HAI_INSTALLER_PRODUCTION_SIGNING -eq '1' -and $script:toolCalls.Count -eq 0) 'Preview must restore environment and never sign.'
    }
}

Test-SigningCase 'actual worker loop signs both binaries before payload promotion' {
    function Test-HaiWindowsExecutablePayload { param([string]$Path) return $true }
    $loop = $buildAst.Find({ param($node)
        $node -is [Management.Automation.Language.ForEachStatementAst] -and
        $node.Extent.Text -match 'Test-HaiWindowsExecutablePayload'
    }, $true)
    if ($null -eq $loop) { throw 'Missing worker signing loop.' }
    $workers = @(@{ Binary = 'hai-dsh-bridge.exe' }, @{ Binary = 'hai-openclaw-maintenance.exe' })
    $stagingRoot = 'C:\hai-signing-fixture'
    $included = New-Object 'System.Collections.Generic.List[string]'
    $workerSignatures = [ordered]@{}
    $Production = $true
    $signingPolicy = $script:policy
    # Only this isolated loop needs virtual worker paths.
    function Test-Path { param([string]$LiteralPath, [string]$PathType) return $true }
    . ([ScriptBlock]::Create($loop.Extent.Text))
    Assert-SigningTest ($included.Count -eq 2 -and $workerSignatures.Count -eq 2 -and $script:toolCalls.Count -eq 4) 'Both workers must be signed and verified.'
    Assert-SigningTest ($loop.Extent.StartOffset -lt $buildSource.IndexOf('$payloadPromotionAttempted = $true')) 'Workers must be verified before payload promotion.'
}
Test-SigningCase 'actual worker loop stops at invalid output before including it' {
    function Test-HaiWindowsExecutablePayload { param([string]$Path) return $true }
    function Test-Path { param([string]$LiteralPath, [string]$PathType) return $true }
    $loop = $buildAst.Find({ param($node)
        $node -is [Management.Automation.Language.ForEachStatementAst] -and
        $node.Extent.Text -match 'Test-HaiWindowsExecutablePayload'
    }, $true)
    $workers = @(@{ Binary = 'hai-dsh-bridge.exe' }, @{ Binary = 'hai-openclaw-maintenance.exe' })
    $stagingRoot = 'C:\hai-signing-fixture'
    $included = New-Object 'System.Collections.Generic.List[string]'
    $workerSignatures = [ordered]@{}
    $Production = $true
    $signingPolicy = $script:policy
    $script:signature.Status = 'NotSigned'
    Assert-SigningThrows { . ([ScriptBlock]::Create($loop.Extent.Text)) } 'Authenticode verification failed'
    Assert-SigningTest ($included.Count -eq 0 -and $workerSignatures.Count -eq 0) 'Rejected worker must not enter the manifest.'
}
Test-SigningCase 'entry point gate precedes staging and forbids unsigned production bypass' {
    $gateOffset = $buildSource.IndexOf('$signingPolicy = Resolve-HaiInstallerSigningPolicy')
    $firstWrite = $buildSource.IndexOf('New-Item -ItemType Directory')
    Assert-SigningTest ($gateOffset -ge 0 -and $gateOffset -lt $firstWrite) 'Production preflight must precede all staging writes.'
    Assert-SigningTest ($buildSource -match 'releaseChannel = .*developer-preview') 'Manifest must distinguish developer previews.'
    $tailOffset = $buildSource.IndexOf('$compileOutputDirectory = $OutputDirectory')
    $tail = $buildSource.Substring($tailOffset)
    Assert-SigningTest ($tail.IndexOf('Protect-HaiInstallerProductionFile') -lt $tail.IndexOf('[IO.File]::Move')) 'Setup must be verified before publication to final filename.'
    Assert-SigningTest ($tail.IndexOf('Protect-HaiInstallerProductionFile') -lt $tail.IndexOf('Created Windows installer:')) 'Success must follow signing acceptance.'
    Assert-SigningTest ($tail -match '\.production-\$buildId\.staging' -and $tail -match 'finally') 'Production must isolate new Setup and restore compiler environment.'
}
Test-SigningCase 'actual compiler tail fails closed on unsigned Setup and restores environment' {
    function Test-Path { param([string]$LiteralPath, [string]$PathType) return $true }
    function New-Item { param([string]$ItemType, [string]$Path, [string]$ErrorAction) }
    function Invoke-HaiFakeCompiler { $global:LASTEXITCODE = 0 }
    function Assert-HaiInnoSignedUninstaller {
        param([string]$Directory, $Policy)
        return [pscustomobject]@{ Path = $script:uninstallerPath; Signature = [pscustomobject]@{ sha256 = $script:hash } }
    }
    $Production = $true
    $Version = 'signing-test'
    $buildId = 'synthetic'
    $OutputDirectory = 'C:\hai-signing-fixture'
    $repositoryRoot = 'C:\hai-signing-fixture'
    $installerPath = Join-Path $OutputDirectory 'HAI-Setup-signing-test.exe'
    $installerScript = 'C:\hai-signing-fixture\HAI.iss'
    $ISCCPath = 'Invoke-HaiFakeCompiler'
    $signingPolicy = $script:policy
    $script:signature.Status = 'NotSigned'
    $beforeVersion = $env:HAI_INSTALLER_VERSION
    $beforeOutput = $env:HAI_INSTALLER_OUTPUT_DIR
    $tail = [ScriptBlock]::Create($buildSource.Substring($buildSource.IndexOf('$compileOutputDirectory = $OutputDirectory')))
    Assert-SigningThrows { . $tail } 'Authenticode verification failed'
    Assert-SigningTest ($env:HAI_INSTALLER_VERSION -eq $beforeVersion -and $env:HAI_INSTALLER_OUTPUT_DIR -eq $beforeOutput) 'Compiler environment leaked after signing failure.'
    Assert-SigningTest ($script:toolCalls.Count -eq 1) 'Unsigned Setup must stop before filesystem promotion.'
}

if (-not [string]::IsNullOrWhiteSpace($UnsignedFixturePath)) {
    Test-SigningCase 'real Windows Authenticode rejects operator-selected unsigned artifact' {
        function Test-Path {
            param([string]$LiteralPath, [string]$PathType)
            return Microsoft.PowerShell.Management\Test-Path -LiteralPath $LiteralPath -PathType $PathType
        }
        function Get-AuthenticodeSignature {
            param([string]$LiteralPath, [string]$ErrorAction)
            return Microsoft.PowerShell.Security\Get-AuthenticodeSignature -LiteralPath $LiteralPath -ErrorAction Stop
        }
        $nativeSignature = Microsoft.PowerShell.Security\Get-AuthenticodeSignature -LiteralPath $UnsignedFixturePath -ErrorAction Stop
        Assert-SigningTest ($nativeSignature.Status -eq 'NotSigned') 'Optional fixture must really be unsigned; no signatures are removed by this test.'
        Assert-SigningThrows { Assert-HaiInstallerAuthenticode -Path $UnsignedFixturePath -Policy $script:policy } 'Authenticode verification failed.*NotSigned'
        Assert-SigningTest ($script:toolCalls.Count -eq 0) 'Native unsigned rejection must not invoke SignTool.'
    }
}

Write-Host "Windows installer signing tests passed: $script:passed cases. All signer/certificate/compiler success paths used in-memory doubles; no binaries were built, signed, installed, or deleted."
