[CmdletBinding()]
param(
    [switch]$Library
)

$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$pass = 0
$fail = 0

function Write-Pass {
    param([string]$Message)

    Write-Host "  PASS: $Message"
    $script:pass++
}

function Write-Fail {
    param([string]$Message)

    Write-Host "  FAIL: $Message"
    $script:fail++
}

function Get-TrackedFiles {
    $files = @(& git ls-files)
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to list tracked Git files."
    }

    return $files
}

function Test-Phase2SensitiveAddedFile {
    param([string]$Path)

    return $Path -match "(^|/)\.env($|\.)|(^|/)(secrets?|credentials?)(/|\.|$)|\.(db|sqlite|sqlite3|gguf|onnx|safetensors)$|(^|/)[^/]*(token|credential)[^/]*\.(json|ya?ml|toml|ini|txt)$"
}

function Test-RequiredText {
    param(
        [string]$Path,
        [string]$Text,
        [string]$PassMessage,
        [string]$FailMessage
    )

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        Write-Fail $FailMessage
        return
    }

    if ([IO.File]::ReadAllText($Path).Contains($Text)) {
        Write-Pass $PassMessage
    } else {
        Write-Fail $FailMessage
    }
}

if ($Library) {
    return
}

Push-Location $repositoryRoot
try {
    $phase2Packages = @(
        "backend/internal/operations",
        "backend/internal/background",
        "backend/internal/executionbroker",
        "backend/internal/accountfeed",
        "backend/internal/modelintelligence",
        "backend/internal/hardwareprofile",
        "backend/internal/runtimelab",
        "backend/internal/opscontrol",
        "backend/internal/autonomypolicy",
        "backend/internal/privacyfilter",
        "backend/internal/phase2"
    )
    $markerPattern = "\bTODO\b|\bFIXME\b|\bXXX\b|\bnot implemented\b|\bnot yet implemented\b|\bhardcoded\b|\bplaceholder\b|\bdummy\b"

    Write-Host "==> 1. No fake/stub/TODO markers in Phase 2 source (excluding tests)"
    $markers = @(
        foreach ($package in $phase2Packages) {
            if (-not (Test-Path -LiteralPath $package -PathType Container)) {
                continue
            }

            Get-ChildItem -LiteralPath $package -Recurse -File -Filter "*.go" |
                Where-Object { $_.Name -notlike "*_test.go" } |
                Select-String -Pattern $markerPattern -CaseSensitive:$false
        }
    )
    if ($markers.Count -eq 0) {
        Write-Pass "no unfinished/hardcoded/placeholder markers"
    } else {
        Write-Fail "found unfinished markers"
        $markers | ForEach-Object { Write-Host "        $($_.Path):$($_.LineNumber)" }
    }

    Write-Host "==> 2. Anti-fake truthfulness invariants are present in the code"
    Test-RequiredText "backend/internal/modelintelligence/dspark.go" "never active without a successful probe" "model providers never auto-active without a probe" "missing 'never active without probe' invariant"
    Test-RequiredText "backend/internal/modelintelligence/dspark.go" "Never fabricate output" "DSpark never fabricates output" "DSpark fabrication guard missing"
    Test-RequiredText "backend/internal/runtimelab/service.go" "never fake execution" "external runtimes never fake execution" "external runtime no-fake-execution guard missing"
    Test-RequiredText "backend/internal/accountfeed/bridge.go" "never fakes OAuth or connected status" "account bridges never fake a connected status" "account bridge no-fake-connected guard missing"
    Test-RequiredText "backend/internal/operations/domain.go" "cannot complete with verification status" "operations cannot complete without passing verification" "verification-gated completion invariant missing"
    Test-RequiredText "backend/internal/background/worker.go" "effectiveEmergencyStop" "background loop honors the live emergency stop" "emergency-stop enforcement missing"
    Test-RequiredText "backend/internal/hardwareprofile/profile.go" "Do not claim Windows ML" "hardware detection never claims Windows on non-Windows" "hardware truthfulness guard missing"

    Write-Host "==> 3. No secrets / databases / runtime state / model weights added by Phase 2"
    $trackedFiles = Get-TrackedFiles
    $baselineEnvironmentPattern = "^(\.env|\.env-backend|\.env-gateway|\.env-idp|\.env\.example)$"
    $baselineEnvironmentFiles = @($trackedFiles | Where-Object { $_ -match $baselineEnvironmentPattern })
    if ($baselineEnvironmentFiles.Count -gt 0) {
        Write-Host "  NOTE: pre-existing repo-baseline env files (dev defaults, not Phase 2):"
        $baselineEnvironmentFiles | ForEach-Object { Write-Host "        $_" }
    }

    $secretFiles = @(
        $trackedFiles |
            Where-Object { $_ -match "(^|/)secrets?\.|id_rsa|\.pem$|\.pfx$|\.key$|credentials\.json$" } |
            Where-Object { $_ -notmatch $baselineEnvironmentPattern }
    )
    if ($secretFiles.Count -eq 0) {
        Write-Pass "no secret/credential files beyond the documented baseline"
    } else {
        Write-Fail "unexpected secret-like files tracked"
        $secretFiles | ForEach-Object { Write-Host "        $_" }
    }

    $base = @(& git rev-list --max-parents=0 HEAD | Select-Object -Last 1)[0]
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($base)) {
        throw "Unable to resolve the Git repository base commit."
    }
    $fetchHead = & git rev-parse --verify -q FETCH_HEAD 2>$null
    if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($fetchHead)) {
        $mergeBase = & git merge-base HEAD FETCH_HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($mergeBase)) {
            $base = $mergeBase
        }
    }
    $addedFiles = @(& git diff --name-only --diff-filter=A "$base..HEAD")
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to inspect files added by this Git branch."
    }
    $sensitiveAddedFiles = @($addedFiles | Where-Object { Test-Phase2SensitiveAddedFile $_ })
    if ($sensitiveAddedFiles.Count -eq 0) {
        Write-Pass "Phase 2 branch added no env/secret/db/weight files"
    } else {
        Write-Fail "Phase 2 added sensitive files"
        $sensitiveAddedFiles | ForEach-Object { Write-Host "        $_" }
    }

    $databaseFiles = @($trackedFiles | Where-Object { $_ -match "\.sqlite$|\.sqlite3$|\.db$|(^|/)pgdata/|(^|/)data/phase2/" })
    if ($databaseFiles.Count -eq 0) {
        Write-Pass "no local databases / runtime state tracked"
    } else {
        Write-Fail "db/state files tracked"
        $databaseFiles | ForEach-Object { Write-Host "        $_" }
    }

    $weightFiles = @($trackedFiles | Where-Object { $_ -match "\.gguf$|\.onnx$|\.safetensors$|\.bin$|\.pt$|\.ckpt$" })
    if ($weightFiles.Count -eq 0) {
        Write-Pass "no model weights tracked"
    } else {
        Write-Fail "model weight files tracked"
        $weightFiles | ForEach-Object { Write-Host "        $_" }
    }

    $tokenOutput = @(& git grep -n -E "(sk-live-[A-Za-z0-9]{16,}|ghp_[A-Za-z0-9]{30,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----)" -- "*.go" "*.ts" "*.sh" 2>$null)
    if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne 1) {
        throw "Unable to scan tracked source for embedded tokens."
    }
    $tokenHits = @($tokenOutput | Where-Object { $_ -notmatch "_test\.go|dummy|example|redact|scanner|smoke" })
    if ($tokenHits.Count -eq 0) {
        Write-Pass "no embedded live tokens/keys in tracked source"
    } else {
        Write-Fail "possible tokens in source"
        $tokenHits | ForEach-Object {
            $location = $_ -replace ":.*$", ""
            Write-Host "        $location"
        }
    }

    Write-Host ""
    Write-Host "==> Result: $pass passed, $fail failed"
    if ($fail -gt 0) {
        exit 1
    }
} finally {
    Pop-Location
}
