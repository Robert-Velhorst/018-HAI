[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$path = Join-Path $PSScriptRoot 'isolated-acceptance-stack.ps1'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw 'Acceptance launcher has syntax errors.' }
# Extract only pure guards: never execute the launcher or contact Docker.
foreach ($name in @('Get-AcceptanceResourceLimits', 'Assert-AcceptanceResourceLimits')) {
    $functions = @($ast.FindAll({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name }, $true))
    if ($functions.Count -ne 1) { throw "Expected one resource guard: $name" }
    . ([scriptblock]::Create($functions[0].Extent.Text))
}
$names = @('idp', 'backend', 'backend-migrate', 'backend-runtime-role', 'frontend', 'nginx', 'postgres-idp', 'postgres-automation', 'redis')
$memory = 0L
$cpu = 0.0
$checks = 0
foreach ($name in $names) {
    $limit = Get-AcceptanceResourceLimits $name
    $memory += $limit.memory
    $cpu += $limit.cpus
    $baseline = [pscustomobject]@{ mem_limit = $limit.memory; memswap_limit = $limit.memory; cpus = $limit.cpus; pids_limit = $limit.pids }
    Assert-AcceptanceResourceLimits $baseline $name
    $checks++
    foreach ($property in @('mem_limit', 'memswap_limit', 'cpus', 'pids_limit')) {
        foreach ($invalid in @('missing', 'zero', 'excessive')) {
            $candidate = $baseline | ConvertTo-Json | ConvertFrom-Json
            if ($invalid -eq 'missing') { $candidate.PSObject.Properties.Remove($property) }
            elseif ($invalid -eq 'zero') { $candidate.$property = 0 }
            else { $candidate.$property = $candidate.$property * 2 }
            $refused = $false
            try { Assert-AcceptanceResourceLimits $candidate $name }
            catch { if ($_.Exception.Message -notmatch 'resource limits changed or missing') { throw }; $refused = $true }
            if (-not $refused) { throw "Accepted unsafe limit: $name/$property/$invalid" }
            $checks++
        }
    }
    foreach ($property in @('deploy', 'cpu_quota', 'cpu_period', 'cpu_count', 'cpu_percent', 'mem_reservation')) {
        $candidate = $baseline | ConvertTo-Json | ConvertFrom-Json
        $candidate | Add-Member NoteProperty $property 0
        $refused = $false
        try { Assert-AcceptanceResourceLimits $candidate $name }
        catch { if ($_.Exception.Message -notmatch 'Conflicting acceptance resource setting') { throw }; $refused = $true }
        if (-not $refused) { throw "Accepted conflicting resource setting: $name/$property" }
        $checks++
    }
}
if ($memory -ne 1792MB -or $cpu -ne 3.75) { throw 'Aggregate runtime budget changed.' }
$refused = $false
try { Get-AcceptanceResourceLimits 'foreign-service' | Out-Null }
catch { if ($_.Exception.Message -notmatch 'Unknown acceptance service') { throw }; $refused = $true }
if (-not $refused) { throw 'Unknown service accepted.' }
Write-Output "PASS: $($checks + 1) offline resource checks; runtime ceiling 1792 MiB, 3.75 CPUs, no swap. No Docker calls. Build resources and live enforcement remain unverified."
