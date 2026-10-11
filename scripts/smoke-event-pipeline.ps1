[CmdletBinding()]
param(
    [string]$BaseUrl = "http://127.0.0.1",
    [string]$BrokerContainer = "018-hai-kafka",
    [string]$Topic = "automation-events",
    [string]$ConfigDirectory = "",
    [int]$TimeoutSeconds = 15
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($ConfigDirectory)) {
    $ConfigDirectory = Join-Path $PSScriptRoot "..\nginx-config\sites-enabled"
}
$ConfigDirectory = [IO.Path]::GetFullPath($ConfigDirectory)

function Get-TopicHighWatermark {
    $raw = docker exec $BrokerContainer rpk topic describe $Topic -p --format json
    if ($LASTEXITCODE -ne 0) {
        throw "Could not inspect Kafka-compatible topic '$Topic'."
    }
    $description = $raw | ConvertFrom-Json
    if (-not $description -or -not $description[0].partitions) {
        throw "Topic '$Topic' did not expose partition state."
    }
    return [int64]$description[0].partitions[0].high_watermark
}

function Wait-ForPathState {
    param(
        [Parameter(Mandatory)] [string]$LiteralPath,
        [Parameter(Mandatory)] [bool]$Exists
    )

    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        if ((Test-Path -LiteralPath $LiteralPath) -eq $Exists) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)

    $expected = if ($Exists) { "appear" } else { "be removed" }
    throw "Timed out waiting for '$LiteralPath' to $expected."
}

function Invoke-CurlStatus {
    param([Parameter(ValueFromRemainingArguments)] [string[]]$Arguments)
    $status = & curl.exe -sS -o NUL -w "%{http_code}" @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "HTTP request failed before a response was received."
    }
    return [string]$status
}

$cookieJar = Join-Path $env:TEMP ("hai-event-pipeline-" + [guid]::NewGuid().ToString("N") + ".cookies")
$responsePath = Join-Path $env:TEMP ("hai-event-pipeline-" + [guid]::NewGuid().ToString("N") + ".json")
$automationID = ""
$configPath = ""
$created = $false
$deleted = $false

try {
    $before = Get-TopicHighWatermark
    $previewStatus = Invoke-CurlStatus -Arguments @(
        "-c", $cookieJar, "-X", "POST", "$BaseUrl/api/v1/auth/local-preview"
    )
    if ($previewStatus -ne "204") {
        throw "Local preview session failed with HTTP $previewStatus. Enable the loopback-only local preview before running this smoke."
    }

    $suffix = Get-Date -Format "yyyyMMddHHmmssfff"
    $name = "Acceptance Route $suffix"
    $createStatus = & curl.exe -sS -o $responsePath -w "%{http_code}" `
        -b $cookieJar -c $cookieJar -X POST `
        -F "name=$name" -F "host=backend" -F "port=80" -F "position=0" -F "removeImage=false" `
        -F "launchType=api" -F "launchTarget=GET http://backend/readyz" `
        -F "healthCheckType=http" -F "healthCheckUrl=http://backend/readyz" -F "expectedHttpStatus=200" `
        "$BaseUrl/api/v1/automation/"
    if ($LASTEXITCODE -ne 0 -or $createStatus -ne "201") {
        $body = if (Test-Path $responsePath) { Get-Content -Raw $responsePath } else { "" }
        throw "Automation create failed with HTTP $createStatus. $body"
    }

    $automation = Get-Content -Raw $responsePath | ConvertFrom-Json
    $automationID = [string]$automation.id
    $urlPath = [string]$automation.urlPath
    if ([string]::IsNullOrWhiteSpace($automationID) -or [string]::IsNullOrWhiteSpace($urlPath)) {
        throw "Automation create response omitted id or urlPath."
    }
    $created = $true
    $configPath = Join-Path $ConfigDirectory ($urlPath + ".conf")
    Wait-ForPathState -LiteralPath $configPath -Exists $true

    $readStatus = Invoke-CurlStatus -Arguments @(
        "-b", $cookieJar, "$BaseUrl/api/v1/automation/$automationID"
    )
    if ($readStatus -ne "200") {
        throw "Automation read failed with HTTP $readStatus."
    }

    $deleteStatus = Invoke-CurlStatus -Arguments @(
        "-b", $cookieJar, "-X", "DELETE", "$BaseUrl/api/v1/automation/$automationID"
    )
    if ($deleteStatus -ne "204") {
        throw "Automation delete failed with HTTP $deleteStatus."
    }
    $deleted = $true
    Wait-ForPathState -LiteralPath $configPath -Exists $false

    $deletedReadStatus = Invoke-CurlStatus -Arguments @(
        "-b", $cookieJar, "$BaseUrl/api/v1/automation/$automationID"
    )
    if ($deletedReadStatus -ne "404") {
        throw "Deleted automation read returned HTTP $deletedReadStatus."
    }

    $after = Get-TopicHighWatermark
    if ($after -lt ($before + 2)) {
        throw "Topic offset advanced from $before to $after; expected create and delete events."
    }

    [pscustomobject]@{
        session       = $previewStatus
        create        = $createStatus
        read          = $readStatus
        delete        = $deleteStatus
        deletedRead   = $deletedReadStatus
        configCreated = $true
        configRemoved = $true
        offsetBefore  = $before
        offsetAfter   = $after
        eventsAdded   = $after - $before
        automationId  = $automationID
        route         = $urlPath
    } | ConvertTo-Json -Compress
} finally {
    if ($created -and -not $deleted -and -not [string]::IsNullOrWhiteSpace($automationID) -and (Test-Path $cookieJar)) {
        & curl.exe -sS -o NUL -b $cookieJar -X DELETE "$BaseUrl/api/v1/automation/$automationID" | Out-Null
    }
    if (-not [string]::IsNullOrWhiteSpace($configPath) -and (Test-Path -LiteralPath $configPath)) {
        Write-Warning "Disposable route config remains at '$configPath'; investigate the event pipeline before removing it."
    }
    Remove-Item -LiteralPath $cookieJar, $responsePath -Force -ErrorAction SilentlyContinue
}
