[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile((Join-Path $root 'installer/windows/Hai-InstallerSupport.ps1'), [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw 'Installer support source did not parse.' }
foreach ($name in @('New-HaiRestrictedEnvironmentFileSecurity', 'Get-HaiComparableFileAccessDescriptor', 'Set-HaiFileDacl', 'Set-HaiExistingFileAccessRules')) {
    $nodes = @($ast.FindAll({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true))
    if ($nodes.Count -ne 1) { throw "Missing unique function: $name" }
    if ($name -eq 'Set-HaiExistingFileAccessRules' -and $nodes[0].Extent.Text -match '(?i)/reset|/inheritance:|/grant:') {
        throw 'Existing-file ACL update still uses sequential reset/restriction operations.'
    }
    . ([scriptblock]::Create($nodes[0].Extent.Text))
}
if ($env:OS -ne 'Windows_NT') { throw 'This native file-ACL contract requires Windows.' }

$temporaryParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\', '/')
$fixture = Join-Path $temporaryParent ('hai-acl-contract-' + [Guid]::NewGuid().ToString('N'))
$created = New-Object 'System.Collections.Generic.List[string]'
$script:setCalls = 0
$script:failWrite = $false
$script:nativeDaclWriter = ${function:Set-HaiFileDacl}
function Set-HaiFileDacl {
    [CmdletBinding()]
    param([string]$Path, $AccessRules)
    $script:setCalls++
    $descriptor = $AccessRules.GetSecurityDescriptorSddlForm([Security.AccessControl.AccessControlSections]::All)
    if ($descriptor -match '(?:^|[)])(?:O:|G:|S:)') { throw 'Update carries non-DACL security sections.' }
    if ($script:failWrite) { throw 'Synthetic access update failure.' }
    & $script:nativeDaclWriter -Path $Path -AccessRules $AccessRules
}

try {
    New-Item -ItemType Directory -Path $fixture -ErrorAction Stop | Out-Null
    foreach ($kind in @('inherited', 'already-protected', 'write-failure')) {
        $path = Join-Path $fixture ($kind + '.txt')
        [IO.File]::WriteAllText($path, 'synthetic non-secret fixture')
        $created.Add($path)
        $desired = New-HaiRestrictedEnvironmentFileSecurity
        if ($kind -eq 'already-protected') {
            Microsoft.PowerShell.Security\Set-Acl -LiteralPath $path -AclObject $desired -ErrorAction Stop
        }
        $original = Get-Acl -LiteralPath $path
        $owner = $original.GetOwner([Security.Principal.SecurityIdentifier]).Value
        $group = $original.GetGroup([Security.Principal.SecurityIdentifier]).Value
        $beforeDacl = Get-HaiComparableFileAccessDescriptor -FileSecurity $original
        # An intentionally different desired owner must not be applied.
        $desired.SetOwner((New-Object Security.Principal.SecurityIdentifier('S-1-5-18')))
        $beforeCalls = $script:setCalls
        $script:failWrite = $kind -eq 'write-failure'
        $failed = $false
        $failureDetail = ''
        try { Set-HaiExistingFileAccessRules -Path $path -DesiredSecurity $desired } catch { $failed = $true; $failureDetail = $_.Exception.Message }
        if ($failed -ne $script:failWrite) { throw "Unexpected ACL outcome: $kind ($failureDetail)" }
        if ($script:setCalls -ne $beforeCalls + 1) { throw 'ACL update must attempt exactly one persistence call.' }
        $actual = Get-Acl -LiteralPath $path
        if ($actual.GetOwner([Security.Principal.SecurityIdentifier]).Value -cne $owner -or
            $actual.GetGroup([Security.Principal.SecurityIdentifier]).Value -cne $group) { throw 'Owner/group changed.' }
        $expectedDacl = if ($failed) { $beforeDacl } else { Get-HaiComparableFileAccessDescriptor -FileSecurity $desired }
        if ((Get-HaiComparableFileAccessDescriptor -FileSecurity $actual) -cne $expectedDacl) { throw 'Unexpected persisted DACL.' }
        if ([IO.File]::ReadAllText($path) -cne 'synthetic non-secret fixture') { throw 'ACL update changed file content.' }
    }
    Write-Output 'Installer environment ACL contract: 3 PASS; native synthetic files only.'
} finally {
    # Delete only exact files created here, never recurse or follow a reparse point.
    if ([IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($fixture)) -cne $temporaryParent -or
        [IO.Path]::GetFileName($fixture) -notmatch '^hai-acl-contract-[0-9a-f]{32}$') { throw 'Unsafe fixture cleanup root.' }
    if (Test-Path -LiteralPath $fixture) {
        $item = Get-Item -LiteralPath $fixture -Force -ErrorAction Stop
        if (-not $item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Unsafe fixture root type.' }
        foreach ($path in $created) {
            $item = Get-Item -LiteralPath $path -Force -ErrorAction Stop
            if ([IO.Path]::GetDirectoryName($item.FullName) -cne $fixture -or $item.PSIsContainer -or
                ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Unsafe fixture file.' }
            Remove-Item -LiteralPath $item.FullName -Force -ErrorAction Stop
        }
        Remove-Item -LiteralPath $fixture -ErrorAction Stop
    }
}
