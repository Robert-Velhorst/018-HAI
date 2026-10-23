[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$source = Join-Path $root 'installer/windows/Hai-InstallerSupport.ps1'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($source, [ref]$tokens, [ref]$errors)
if ($errors.Count -gt 0) { throw 'Installer support source did not parse.' }
foreach ($name in @('Test-HaiCredentialFreeA2ABridgeUrl', 'Get-HaiA2AComposeSettings')) {
    $functions = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name
    }, $true))
    if ($functions.Count -ne 1) { throw "Expected one installer function: $name" }
    . ([scriptblock]::Create($functions[0].Extent.Text))
}

# Read checked-in wiring, never render Compose or inspect an installed env file.
$compose = [IO.File]::ReadAllText((Join-Path $root 'docker-compose.local.yml'))
$service = [Regex]::Match($compose, '(?ms)^  a2a-gateway:\r?\n(.*?)(?=^  [A-Za-z0-9_-]+:\r?\n|\z)')
$mapping = [Regex]::Match($service.Groups[1].Value, '127\.0\.0\.1:\$\{HAI_A2A_LOCAL_PORT:-([0-9]+)\}:([0-9]+)')
if (-not $service.Success -or -not $mapping.Success) { throw 'Canonical A2A port mapping was not found.' }
$targetPort = [int]$mapping.Groups[2].Value
$template = [IO.File]::ReadAllText((Join-Path $root 'nginx-config/a2a-local.conf.template'))
if (-not [Regex]::IsMatch($template, '(?m)^\s*listen\s+' + $targetPort + ';\s*$')) {
    throw 'Compose target and Nginx listener disagree.'
}

$script:dockerCalls = New-Object 'System.Collections.Generic.List[string]'
function docker {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$DockerArguments)
    if (($DockerArguments -join ' ') -cne 'compose --project-name synthetic-contract --profile local-a2a config --format json') {
        throw 'Unexpected Docker operation in isolated installer contract.'
    }
    $script:dockerCalls.Add(($DockerArguments -join ' ')) | Out-Null
    $global:LASTEXITCODE = 0
    return ($script:configuration | ConvertTo-Json -Depth 12 -Compress)
}

function New-ContractConfiguration {
    param([int]$Target, [string]$HostIp = '127.0.0.1', [string]$Published = '18091', [string]$Protocol = 'tcp')
    return @{
        services = @{
            backend = @{ environment = @{ HAI_A2A_BRIDGE_ENABLED = 'true'; HAI_A2A_BRIDGE_URL = 'http://127.0.0.1:18091/api/v1/a2a' } }
            'a2a-gateway' = @{ ports = @(@{ target = $Target; host_ip = $HostIp; published = $Published; protocol = $Protocol }) }
        }
    }
}

$cases = @(
    @{ Name = 'canonical-ipv4'; Target = $targetPort; HostIp = '127.0.0.1'; Published = '18091'; Protocol = 'tcp'; Accept = $true; Expected = 'http://127.0.0.1:18091/.well-known/agent-card.json' },
    @{ Name = 'canonical-ipv6'; Target = $targetPort; HostIp = '::1'; Published = '18092'; Protocol = 'tcp'; Accept = $true; Expected = 'http://[::1]:18092/.well-known/agent-card.json' },
    @{ Name = 'legacy-port'; Target = 80; Accept = $false },
    @{ Name = 'wildcard-host'; Target = $targetPort; HostIp = '0.0.0.0'; Accept = $false },
    @{ Name = 'remote-host'; Target = $targetPort; HostIp = '192.0.2.1'; Accept = $false },
    @{ Name = 'udp'; Target = $targetPort; Protocol = 'udp'; Accept = $false },
    @{ Name = 'zero-published'; Target = $targetPort; Published = '0'; Accept = $false },
    @{ Name = 'invalid-published'; Target = $targetPort; Published = 'invalid'; Accept = $false },
    @{ Name = 'duplicate-tcp'; Target = $targetPort; Duplicate = $true; Accept = $false },
    @{ Name = 'disabled'; Target = $targetPort; Disabled = $true; Accept = $true },
    @{ Name = 'remote-bridge'; Target = $targetPort; BridgeUrl = 'https://example.test/api/v1/a2a'; Accept = $false },
    @{ Name = 'missing-gateway'; Target = $targetPort; MissingGateway = $true; Accept = $false }
)
$failures = New-Object 'System.Collections.Generic.List[string]'
foreach ($case in $cases) {
    $script:configuration = New-ContractConfiguration -Target $case.Target
    foreach ($key in @('HostIp', 'Published', 'Protocol')) {
        if ($case.ContainsKey($key)) {
            $property = @{ HostIp = 'host_ip'; Published = 'published'; Protocol = 'protocol' }[$key]
            $script:configuration.services.'a2a-gateway'.ports[0][$property] = $case[$key]
        }
    }
    if ($case.Duplicate) { $script:configuration.services.'a2a-gateway'.ports += $script:configuration.services.'a2a-gateway'.ports[0].Clone() }
    if ($case.Disabled) { $script:configuration.services.backend.environment.HAI_A2A_BRIDGE_ENABLED = 'false' }
    if ($case.BridgeUrl) { $script:configuration.services.backend.environment.HAI_A2A_BRIDGE_URL = $case.BridgeUrl }
    if ($case.MissingGateway) { $script:configuration.services.Remove('a2a-gateway') }
    $accepted = $false
    $result = $null
    try {
        $result = Get-HaiA2AComposeSettings -ComposeArguments @('compose', '--project-name', 'synthetic-contract')
        $accepted = $true
    } catch { }
    if ($accepted -ne $case.Accept) {
        $failures.Add($case.Name)
    } elseif ($accepted -and $case.Disabled) {
        if ($result.Enabled -or $null -ne $result.AgentCardUrl) { $failures.Add($case.Name) }
    } elseif ($accepted -and ($result.AgentCardUrl -cne $case.Expected -or -not $result.Enabled)) {
        $failures.Add($case.Name)
    }
}
if ($script:dockerCalls.Count -ne $cases.Count) { throw 'The contract did not exercise every configured read.' }
if ($failures.Count -gt 0) { throw "Installer A2A contract failed: $($failures -join ', ')" }
Write-Output "Installer A2A Compose contract: $($cases.Count) PASS; no real Docker, HTTP, environment or process mutation."
