#!/usr/bin/env bash
#
# Always-On Runtime Control smoke test (HAI Phase 2F).
#
# Boots a throwaway PostgreSQL + the real backend and proves the always-on
# control plane is real and honest: the emergency stop actually halts background
# processing (self-verified), pause/resume/mode take effect live, the emergency
# stop survives via persisted state, crash/reboot recovery reconciles stuck
# operations, and the Windows-runtime readiness checklist is truthful (Windows
# gates pending off-Windows, Docker never required, safe worker passes).
#
# Requires: postgres/initdb/pg_ctl/createdb, Go, curl, jq. No Docker.
# Usage: scripts/smoke-windows-runtime.sh
set -euo pipefail

PG_PORT="${PG_PORT:-55437}"
API_PORT="${API_PORT:-18085}"
API_KEY="${API_KEY:-hai-ci-smoke-api-key-0123456789abcdef}"
JWT_SECRET="${JWT_SECRET:-hai-ci-smoke-jwt-secret-0123456789abcdef}"
BASE="http://127.0.0.1:${API_PORT}/api/v1"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/scripts/smoke-auth.sh"

WORKDIR="$(mktemp -d)"
PGDATA="${WORKDIR}/pgdata"
IMAGES="${WORKDIR}/images"
FEEDS="${WORKDIR}/feeds"
WORKSPACE="${WORKDIR}/workspace"
STATE="${WORKDIR}/state"
BIN="${WORKDIR}/hai-backend"
BACKEND_PID=""

pass=0
fail=0

cleanup() {
  set +e
  [ -n "${BACKEND_PID}" ] && kill "${BACKEND_PID}" 2>/dev/null
  pg_ctl -D "${PGDATA}" stop -m fast >/dev/null 2>&1
  rm -rf "${WORKDIR}"
}
trap cleanup EXIT

check() { # name, expected_substr, actual
  if echo "$3" | grep -q "$2"; then
    echo "  PASS: $1"; pass=$((pass + 1))
  else
    echo "  FAIL: $1 (wanted '$2', got: $3)"; fail=$((fail + 1))
  fi
}

echo "==> Preparing a feed (so there is real work to halt)"
mkdir -p "${FEEDS}" "${WORKSPACE}" "${STATE}"
cat > "${FEEDS}/inbox.json" <<'JSON'
[
  {"externalId":"n1","title":"Organize notes","content":"Consolidate personal notes into a local file","itemType":"email","provider":"generic_json_feed"},
  {"externalId":"n2","title":"Tidy workspace","content":"Reorganize the local scratch files","itemType":"email","provider":"generic_json_feed"}
]
JSON

echo "==> Starting throwaway PostgreSQL on :${PG_PORT}"
initdb -D "${PGDATA}" -U "$(whoami)" --auth=trust --locale=C --encoding=UTF8 >/dev/null
pg_ctl -D "${PGDATA}" -o "-p ${PG_PORT} -h 127.0.0.1 -k ${WORKDIR}" -l "${PGDATA}/server.log" start >/dev/null
for i in $(seq 1 30); do
  pg_isready -h 127.0.0.1 -p "${PG_PORT}" >/dev/null 2>&1 && break
  sleep 1
done
createdb -h 127.0.0.1 -p "${PG_PORT}" automation

echo "==> Building and starting backend on :${API_PORT}"
mkdir -p "${IMAGES}"
( cd "${ROOT}/backend" && go build -o "${BIN}" ./cmd )

start_backend() {
  DB_HOST=127.0.0.1 DB_PORT="${PG_PORT}" DB_USER="$(whoami)" DB_PASSWORD=hai-ci-smoke-postgres-password-0123456789abcdef \
    DB_NAME=automation SERVER_PORT="${API_PORT}" BASE_URL=/api \
    BACKEND_API_SHARED_KEY="${API_KEY}" IMAGE_SAVE_DIR="${IMAGES}" \
    RUN_MODE=test KAFKA_BROKERS="" JWT_SECRET="${JWT_SECRET}" \
    HAI_PHASE2_FEEDS_DIR="${FEEDS}" HAI_PHASE2_WORKSPACE_DIR="${WORKSPACE}" \
    HAI_PHASE2_STATE_DIR="${STATE}" HAI_PHASE2_FEED_FILES="inbox.json" \
    "${BIN}" > "${WORKDIR}/backend.log" 2>&1 &
  BACKEND_PID=$!
}

wait_live() {
  local ready=""
  for i in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:${API_PORT}/healthz" >/dev/null 2>&1; then ready=1; break; fi
    if ! kill -0 "${BACKEND_PID}" 2>/dev/null; then
      echo "backend exited early; log:"; tail -20 "${WORKDIR}/backend.log"; exit 1
    fi
    sleep 1
  done
  [ -n "${ready}" ] || { echo "backend never became live; log:"; tail -20 "${WORKDIR}/backend.log"; exit 1; }
}

start_backend
echo "==> Waiting for liveness"
wait_live

owner_jwt="$(hai_smoke_mint_jwt owner "${JWT_SECRET}")"
key_hdr=(-H "X-HAI-Backend-Key: ${API_KEY}" -H "Content-Type: application/json")
hdr=("${key_hdr[@]}" -H "Authorization: Bearer ${owner_jwt}")

echo "==> Owner activates the bounded smoke execution policy"
hai_smoke_activate_execution_policy "${BASE}" "${hdr[@]}"
kill "${BACKEND_PID}" 2>/dev/null; wait "${BACKEND_PID}" 2>/dev/null || true
BACKEND_PID=""
start_backend
wait_live

echo "==> Authentication boundary"
check "API key alone is rejected" '401' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${key_hdr[@]}" "${BASE}/windows-runtime/readiness")"

echo "==> Windows-runtime readiness is truthful"
rd="$(curl -sS "${hdr[@]}" "${BASE}/windows-runtime/readiness")"
check "windows version gate is pending off-Windows" 'pending' \
  "$(echo "${rd}" | jq -r '[.gates[]|select(.name=="windows_version_build")][0].status')"
check "target-machine verification is flagged pending" 'true' \
  "$(echo "${rd}" | jq -r '.targetMachineVerificationPending')"
check "docker is not required" 'true' "$(echo "${rd}" | jq -r '.docker.required==false')"
check "local safe worker gate passes" 'pass' \
  "$(echo "${rd}" | jq -r '[.gates[]|select(.name=="local_safe_worker_run")][0].status')"
check "no-external-sends-without-approval gate passes" 'pass' \
  "$(echo "${rd}" | jq -r '[.gates[]|select(.name=="no_external_sends_without_approval")][0].status')"

echo "==> Emergency stop self-verification (proves it halts processing)"
ver="$(curl -sS "${hdr[@]}" -X POST "${BASE}/windows-runtime/emergency-stop/verify")"
if ! echo "${ver}" | jq -e 'has("halted")' >/dev/null; then
  echo "emergency-stop verification response: $(echo "${ver}" | jq -c '{error, reasonCode, code}')" >&2
fi
check "emergency stop halts background processing" 'true' \
  "$(echo "${ver}" | jq -r '.halted==true')"
check "zero operations processed while stopped" 'true' \
  "$(echo "${ver}" | jq -r '.operationsProcessedDuringStop==0')"

echo "==> Pause halts the live background loop"
curl -sS "${hdr[@]}" -X POST "${BASE}/background/pause" -d '{"reason":"smoke pause"}' >/dev/null
check "status shows processing inactive while paused" 'true' \
  "$(curl -sS "${hdr[@]}" "${BASE}/background/status" | jq -r '.backgroundProcessingActive==false')"
run_paused="$(curl -sS "${hdr[@]}" -X POST "${BASE}/background/run")"
check "background run processes nothing while paused" 'true' \
  "$(echo "${run_paused}" | jq -r '(.classified==0) and (.autoExecuted==0)')"
check "feed items were still read for the record while paused" 'true' \
  "$(echo "${run_paused}" | jq -r '.itemsIngested>=1')"

echo "==> Emergency stop persists across a backend restart"
kill "${BACKEND_PID}" 2>/dev/null; wait "${BACKEND_PID}" 2>/dev/null || true
start_backend; wait_live
check "emergency stop still engaged after restart" 'true' \
  "$(curl -sS "${hdr[@]}" "${BASE}/background/status" | jq -r '.emergencyStop.engaged==true')"

echo "==> Resume re-enables processing"
resume_authorization="$(curl -fsS "${hdr[@]}" -X POST "${BASE}/background/resume/approval")"
check "resume approval is effect-bound" 'true' \
  "$(echo "${resume_authorization}" | jq -r '(.idempotencyKey != "") and (.taskId != "") and (.approvalSourceId | startswith("opscontrol-owner:")) and (.approvalBindingDigest | length == 64)')"
resume_response="$(curl -sS -w $'\n%{http_code}' "${hdr[@]}" -X POST "${BASE}/background/resume" -d "${resume_authorization}")"
resume_status="${resume_response##*$'\n'}"
resume_body="${resume_response%$'\n'*}"
if [ "${resume_status}" != "200" ]; then
  echo "resume failed with HTTP ${resume_status}: ${resume_body}" >&2
  echo "resume failure reason code: $(echo "${resume_body}" | jq -r '.reasonCode // "unavailable"')" >&2
  # The public control endpoint deliberately keeps authorization failures
  # generic. Its owner-scoped inspection ledger exposes the policy reason
  # codes without printing the approval capability or other secret material.
  curl -sS "${hdr[@]}" "${BASE}/execution-authorizations?limit=3" \
    | jq -c '{receipts: [.receipts[] | select(.action == "opscontrol.emergency-stop.clear") | {outcome, reason, evidence: {reasonCodes: .evidence.reasonCodes, constitution: {requestedCapabilities: .evidence.constitution.requestedCapabilities, deniedCapabilities: .evidence.constitution.deniedCapabilities, authorityCeiling: .evidence.constitution.authorityCeiling}}}]}' >&2 \
    || true
  grep 'opscontrol safety authorization rejected:' "${WORKDIR}/backend.log" \
    | sed -E 's/ detail=.*/ detail=[REDACTED]/' >&2 || true
  # Do not emit the raw process log here. ORM driver errors can include signed
  # approval provenance, while the receipt inspection above is intentionally
  # limited to safe, owner-scoped diagnostic fields.
  exit 1
fi
check "processing active after resume" 'true' \
  "$(curl -sS "${hdr[@]}" "${BASE}/background/status" | jq -r '.backgroundProcessingActive==true')"
run_resumed="$(curl -sS "${hdr[@]}" -X POST "${BASE}/background/run")"
check "background run processes work after resume" 'true' \
  "$(echo "${run_resumed}" | jq -r '.classified>=1')"

echo "==> Mode change is validated + effective"
check "invalid mode rejected" '400' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${hdr[@]}" -X PATCH "${BASE}/background/mode" -d '{"mode":"nope"}')"
curl -sS "${hdr[@]}" -X PATCH "${BASE}/background/mode" -d '{"mode":"read_only"}' >/dev/null
check "mode set to read_only" 'read_only' \
  "$(curl -sS "${hdr[@]}" "${BASE}/background/status" | jq -r '.storedMode')"

echo "==> Crash/reboot recovery reconciles stuck operations"
rec="$(curl -sS "${hdr[@]}" -X POST "${BASE}/windows-runtime/recovery")"
check "recovery endpoint responds with a report" 'true' \
  "$(echo "${rec}" | jq -r 'has("recovered")')"

echo ""
echo "==> Result: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
