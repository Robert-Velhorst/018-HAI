[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [string]$BaseUrl = 'http://localhost',
    [string]$EnvFile = (Join-Path $PSScriptRoot '..\.env.local'),
    [switch]$Apply
)

$ErrorActionPreference = 'Stop'

function Read-DotEnv {
    param([Parameter(Mandatory = $true)][string]$Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Environment file not found: $Path"
    }

    $values = @{}
    foreach ($rawLine in Get-Content -LiteralPath $Path) {
        $line = $rawLine.Trim()
        if (-not $line -or $line.StartsWith('#') -or -not $line.Contains('=')) {
            continue
        }
        $parts = $line.Split('=', 2)
        $value = $parts[1].Trim()
        if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$parts[0].Trim()] = $value
    }
    return $values
}

$configuration = Read-DotEnv -Path $EnvFile
$email = $configuration['FIRST_RUN_ADMIN_EMAIL']
$password = $configuration['FIRST_RUN_ADMIN_PASSWORD']
if (-not $email -or -not $password) {
    throw 'FIRST_RUN_ADMIN_EMAIL and FIRST_RUN_ADMIN_PASSWORD are required in the environment file.'
}

$baseUri = $null
if (-not [Uri]::TryCreate($BaseUrl, [UriKind]::Absolute, [ref]$baseUri) -or
    $baseUri.Scheme -notin @('http', 'https') -or
    -not $baseUri.IsLoopback -or
    $baseUri.UserInfo -or
    $baseUri.AbsolutePath -ne '/' -or
    $baseUri.Query -or
    $baseUri.Fragment) {
    throw 'BaseUrl must be a loopback HTTP(S) origin without credentials, path, query, or fragment.'
}

$root = $baseUri.AbsoluteUri.TrimEnd('/')
$loginBody = @{ email = $email; password = $password } | ConvertTo-Json -Compress
Invoke-RestMethod -Uri "$root/api/v1/auth/login" -Method Post -ContentType 'application/json' -Body $loginBody -SessionVariable haiSession | Out-Null

function Invoke-HaiApi {
    param(
        [Parameter(Mandatory = $true)][ValidateSet('GET', 'POST', 'DELETE')][string]$Method,
        [Parameter(Mandatory = $true)][string]$Path,
        [object]$Body
    )

    $request = @{
        Uri = "$root$Path"
        Method = $Method
        WebSession = $haiSession
        ErrorAction = 'Stop'
    }
    if ($null -ne $Body) {
        $request.ContentType = 'application/json'
        $request.Body = $Body | ConvertTo-Json -Depth 8 -Compress
    }
    return Invoke-RestMethod @request
}

function Get-E2EInventory {
    $automations = @((Invoke-HaiApi -Method GET -Path '/api/v1/automation/') | Where-Object { $_.name -like 'E2E backend readiness *' })
    $sources = @((Invoke-HaiApi -Method GET -Path '/api/v1/sources/?includeDisabled=true') | Where-Object { $_.name -like 'E2E local source *' -and $_.enabled })
    $pursuits = @((Invoke-HaiApi -Method GET -Path '/api/v1/pursuits/?includeArchived=false') | Where-Object { $_.title -like 'E2E governed pursuit *' -and -not $_.archived })
    $workflows = @((Invoke-HaiApi -Method GET -Path '/api/v1/workflow/?includeArchived=false') | Where-Object { $_.projectKey -like 'e2e-governed-*' -and -not $_.archived })
    return [pscustomobject]@{
        Automations = $automations
        Sources = $sources
        Pursuits = $pursuits
        Workflows = $workflows
    }
}

function Write-Inventory {
    param([Parameter(Mandatory = $true)]$Inventory, [string]$Label)

    [pscustomobject]@{
        Stage = $Label
        DisposableAutomations = $Inventory.Automations.Count
        ActiveTestSources = $Inventory.Sources.Count
        ActiveTestPursuits = $Inventory.Pursuits.Count
        ActiveTestWorkflows = $Inventory.Workflows.Count
    } | Format-List
}

$inventory = Get-E2EInventory
Write-Inventory -Inventory $inventory -Label 'Before'

if (-not $Apply) {
    Write-Host 'Dry run only. Re-run with -Apply to retire exactly these strict-prefix test artifacts.'
    return
}

if (-not $PSCmdlet.ShouldProcess($root, 'Archive E2E workflows and pursuits, pause E2E sources, and delete disposable E2E automations')) {
    return
}

foreach ($workflow in $inventory.Workflows) {
    $state = [string]$workflow.currentState
    if ($state -eq 'needs_approval') {
        Invoke-HaiApi -Method POST -Path "/api/v1/workflow/$($workflow.id)/approval" -Body @{
            approved = $false
            note = 'Historical acceptance-test cleanup before archival.'
        } | Out-Null
        $state = 'blocked'
    }
    if ($state -ne 'blocked' -and $state -ne 'completed') {
        Invoke-HaiApi -Method POST -Path "/api/v1/workflow/$($workflow.id)/transition" -Body @{
            targetState = 'blocked'
            message = 'Historical acceptance-test cleanup before archival.'
        } | Out-Null
    }
    Invoke-HaiApi -Method POST -Path "/api/v1/workflow/$($workflow.id)/transition" -Body @{
        targetState = 'archived'
        message = 'Historical acceptance-test artifact archived by owner maintenance.'
    } | Out-Null
}

foreach ($pursuit in $inventory.Pursuits) {
    Invoke-HaiApi -Method POST -Path "/api/v1/pursuits/$($pursuit.id)/archive" -Body @{ archived = $true } | Out-Null
}

foreach ($source in $inventory.Sources) {
    Invoke-HaiApi -Method POST -Path "/api/v1/sources/$($source.id)/pause" -Body @{} | Out-Null
}

foreach ($automation in $inventory.Automations) {
    Invoke-HaiApi -Method DELETE -Path "/api/v1/automation/$($automation.id)" | Out-Null
}

$remaining = Get-E2EInventory
Write-Inventory -Inventory $remaining -Label 'After'
if ($remaining.Automations.Count -or $remaining.Sources.Count -or $remaining.Pursuits.Count -or $remaining.Workflows.Count) {
    throw 'One or more active E2E artifacts remain after cleanup.'
}
