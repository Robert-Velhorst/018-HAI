$ErrorActionPreference = 'Stop'
$composePath = Join-Path (Split-Path -Parent $PSScriptRoot) 'docker-compose.local.yml'
$compose = [IO.File]::ReadAllText($composePath).Replace("`r`n", "`n")
$dockerfilePath = Join-Path (Split-Path -Parent $PSScriptRoot) 'backend\Dockerfile'
$dockerfile = [IO.File]::ReadAllText($dockerfilePath).Replace("`r`n", "`n")
if ($dockerfile -notmatch '(?m)^USER 10001:10001$' -or $dockerfile -notmatch '(?m)^\s+&& install -d -o 10001 -g 10001 .* /root/phase2-control-state$') {
    throw 'Backend image must run as UID/GID 10001 and seed the state mountpoint with that ownership.'
}

function Get-ServiceBlock([string]$Text, [string]$Name) {
    $startMatch = [regex]::Match($Text, "(?m)^  $([regex]::Escape($Name)):$")
    if (-not $startMatch.Success) {
        throw "Compose service '$Name' was not found."
    }
    $nextMatch = [regex]::Match($Text.Substring($startMatch.Index + $startMatch.Length), '(?m)^  [a-zA-Z0-9_-]+:$')
    $length = if ($nextMatch.Success) { $nextMatch.Index } else { $Text.Length - $startMatch.Index - $startMatch.Length }
    return $Text.Substring($startMatch.Index, $startMatch.Length + $length)
}

$backend = Get-ServiceBlock $compose 'backend'
$migration = Get-ServiceBlock $compose 'backend-state-permissions'
$image = '"${COMPOSE_PROJECT_NAME:-hai}-backend:local"'
foreach ($required in @(
    "    image: $image`n",
    "      backend-state-permissions:`n        condition: service_completed_successfully`n"
)) {
    if (-not $backend.Contains($required)) {
        throw "Backend image/dependency contract is missing: $($required.Trim())"
    }
}

foreach ($required in @(
    "    image: $image`n",
    "    user: `"0:0`"`n",
    "    entrypoint: [`"/usr/bin/chown`"]`n",
    "    command: [`"-R`", `"--no-dereference`", `"10001:10001`", `"/root/phase2-control-state`"]`n",
    "    restart: `"no`"`n",
    "    mem_limit: 64m`n",
    "    cpus: 0.10`n",
    "    pids_limit: 16`n",
    "    network_mode: none`n",
    "    read_only: true`n",
    "      - no-new-privileges:true`n",
    "    cap_drop:`n      - ALL`n    cap_add:`n      - CHOWN`n"
)) {
    if (-not $migration.Contains($required)) {
        throw "State ownership migration contract is missing: $($required.Trim())"
    }
}

if ($migration -match '(?m)^    (environment|env_file|secrets|networks|ports):') {
    throw 'State ownership migration must not receive environment, secrets, network attachments, or ports.'
}
if ($migration -match '(?m)^    build:') {
    throw 'State ownership migration must use the backend image, not build a separate image.'
}

$volumeMatch = [regex]::Match($migration, '(?m)^    volumes:\n(?<mounts>(?:^      [^\n]+\n)+)')
if (-not $volumeMatch.Success -or $volumeMatch.Groups['mounts'].Value -cne "      - phase2-control-state:/root/phase2-control-state`n") {
    throw 'State ownership migration must mount only the phase2-control-state named volume.'
}

Write-Host 'HAI phase2 state ownership migration static contract passed.'
