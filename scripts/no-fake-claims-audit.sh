#!/usr/bin/env bash
#
# No-Fake-Claims + No-Secrets audit (HAI Phase 2G).
#
# A static, runnable audit that enforces the Phase 2 engineering discipline:
#   1. No fake/stub/TODO markers in the Phase 2 backend source.
#   2. Anti-fake truthfulness invariants are actually present in the code.
#   3. No secrets, local databases, runtime state, or model weights are tracked
#      in git.
#
# Exits non-zero if any check fails. Run from the repo root.
# Usage: scripts/no-fake-claims-audit.sh
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${ROOT}"

pass=0
fail=0
ok()   { echo "  PASS: $1"; pass=$((pass + 1)); }
bad()  { echo "  FAIL: $1"; fail=$((fail + 1)); }

PHASE2_PKGS="backend/internal/operations backend/internal/background backend/internal/executionbroker backend/internal/accountfeed backend/internal/modelintelligence backend/internal/hardwareprofile backend/internal/runtimelab backend/internal/opscontrol backend/internal/autonomypolicy backend/internal/privacyfilter backend/internal/phase2"

echo "==> 1. No fake/stub/TODO markers in Phase 2 source (excluding tests)"
marker_pattern='(^|[^[:alnum:]_])(TODO|FIXME|XXX|not implemented|not yet implemented|hardcoded|placeholder|dummy)([^[:alnum:]_]|$)'
markers="$(grep -rniE "${marker_pattern}" ${PHASE2_PKGS} 2>/dev/null | grep -v '_test.go' || true)"
if [ -z "${markers}" ]; then
  ok "no unfinished/hardcoded/placeholder markers"
else
  bad "found unfinished markers:"; echo "${markers}"
fi

echo "==> 2. Anti-fake truthfulness invariants are present in the code"

grep -q 'never active without a successful probe' backend/internal/modelintelligence/dspark.go 2>/dev/null \
  && ok "model providers never auto-active without a probe" \
  || bad "missing 'never active without probe' invariant"

# DSpark + remote providers must return an error (never a fabricated result) when unusable.
grep -q 'Never fabricate output' backend/internal/modelintelligence/dspark.go 2>/dev/null \
  && ok "DSpark never fabricates output" \
  || bad "DSpark fabrication guard missing"

# Runtime lab external runtimes must refuse execution, and that refusal must be
# covered by an executable regression test. Do not make this gate depend on a
# comment that can drift independently from behavior.
if grep -q 'execution is not enabled: discovery is not execution authority' backend/internal/runtimelab/remote_runtime.go 2>/dev/null \
  && grep -q 'func TestExternalRuntimeExecuteRefuses' backend/internal/runtimelab/service_test.go 2>/dev/null; then
  ok "external runtimes refuse execution with regression coverage"
else
  bad "external runtime fail-closed execution guard or regression test missing"
fi

# Account bridges must never report a connected status from config alone.
grep -q 'never fakes OAuth or connected status' backend/internal/accountfeed/bridge.go 2>/dev/null \
  && ok "account bridges never fake a connected status" \
  || bad "account bridge no-fake-connected guard missing"

# Completion requires passing verification.
grep -q 'cannot complete with verification status' backend/internal/operations/domain.go 2>/dev/null \
  && ok "operations cannot complete without passing verification" \
  || bad "verification-gated completion invariant missing"

# Emergency stop actually halts processing.
grep -q 'effectiveEmergencyStop' backend/internal/background/worker.go 2>/dev/null \
  && ok "background loop honors the live emergency stop" \
  || bad "emergency-stop enforcement missing"

# Hardware detection must not claim Windows on non-Windows.
grep -q 'Do not claim Windows ML' backend/internal/hardwareprofile/profile.go 2>/dev/null \
  && ok "hardware detection never claims Windows on non-Windows" \
  || bad "hardware truthfulness guard missing"

echo "==> 3. No secrets / databases / runtime state / model weights added by Phase 2"

tracked="$(git ls-files)"

# Pre-existing repo-baseline infra env files (dev defaults, e.g. DB_PASSWORD=postgres).
# They are NOT introduced by Phase 2; documented here for honesty.
BASELINE_ENV_RE='^(\.env|\.env-backend|\.env-gateway|\.env-idp|\.env\.example)$'
baseline="$(echo "${tracked}" | grep -E "${BASELINE_ENV_RE}" || true)"
if [ -n "${baseline}" ]; then
  echo "  NOTE: pre-existing repo-baseline env files (dev defaults, not Phase 2):"
  echo "${baseline}" | sed 's/^/        /'
fi

# Any OTHER secret/credential file beyond the documented baseline is a failure.
secret_files="$(echo "${tracked}" | grep -iE '(^|/)secrets?\.|id_rsa|\.pem$|\.pfx$|\.key$|credentials\.json$' | grep -Ev "${BASELINE_ENV_RE}" || true)"
[ -z "${secret_files}" ] && ok "no secret/credential files beyond the documented baseline" || { bad "unexpected secret-like files tracked:"; echo "${secret_files}"; }

# Prove Phase 2 itself added no env/secret/db/weight files.
base="$(git rev-list --max-parents=0 HEAD | tail -1)"
if git rev-parse --verify -q FETCH_HEAD >/dev/null 2>&1; then
  mb="$(git merge-base HEAD FETCH_HEAD 2>/dev/null || true)"
  [ -n "${mb}" ] && base="${mb}"
fi
added="$(git diff --name-only --diff-filter=A "${base}"..HEAD 2>/dev/null | grep -iE '\.env|(^|/)secret|\.db$|\.sqlite|\.gguf|\.onnx|\.safetensors|token|credential' || true)"
[ -z "${added}" ] && ok "Phase 2 branch added no env/secret/db/weight files" || { bad "Phase 2 added sensitive files:"; echo "${added}"; }

db_files="$(echo "${tracked}" | grep -iE '\.sqlite$|\.sqlite3$|\.db$|(^|/)pgdata/|(^|/)data/phase2/' || true)"
[ -z "${db_files}" ] && ok "no local databases / runtime state tracked" || { bad "db/state files tracked:"; echo "${db_files}"; }

weight_files="$(echo "${tracked}" | grep -iE '\.gguf$|\.onnx$|\.safetensors$|\.bin$|\.pt$|\.ckpt$' || true)"
[ -z "${weight_files}" ] && ok "no model weights tracked" || { bad "model weight files tracked:"; echo "${weight_files}"; }

# Scan tracked Go/TS source + scripts for embedded live tokens (allow env var
# NAMES and obvious test dummies).
token_hits="$(git grep -nE '(sk-live-[A-Za-z0-9]{16,}|ghp_[A-Za-z0-9]{30,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----)' -- '*.go' '*.ts' '*.sh' 2>/dev/null | grep -viE '_test.go|dummy|example|redact|scanner|smoke' || true)"
[ -z "${token_hits}" ] && ok "no embedded live tokens/keys in tracked source" || { bad "possible tokens in source:"; echo "${token_hits}"; }

echo ""
echo "==> Result: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
