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
API_KEY="${API_KEY:-smoke-key}"
JWT_SECRET="${JWT_SECRET:-smoke-jwt-secret}"
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
  {"externalId":"n1","title":"Organize notes","body":"Consolidate personal notes into a local file"},
  {"externalId":"n2","title":"Tidy workspace","body":"Reorganize the local scratch files"}
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
  DB_HOST=127.0.0.1 DB_PORT="${PG_PORT}" DB_USER="$(whoami)" DB_PASSWORD=postgres \
    DB_NAME=automation SERVER_PORT="${API_PORT}" BASE_URL=/api \
    BACKEND_API_SHARED_KEY="${API_KEY}" IMAGE_SAVE_DIR="${IMAGES}" \
    RUN_MODE=production KAFKA_BROKERS="" JWT_SECRET="${JWT_SECRET}" \
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

echo "==> Resume requires and consumes one exact owner approval"
check "empty resume authorization is rejected" '403' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${hdr[@]}" -X POST "${BASE}/background/resume" -d '{}')"
check "failed resume leaves emergency stop engaged" 'true' \
  "$(curl -sS "${hdr[@]}" "${BASE}/background/status" | jq -r '.emergencyStop.engaged==true')"
resume_request="$(curl -fsS "${hdr[@]}" -X POST "${BASE}/background/control-approvals" -d '{"action":"resume"}')"
check "resume approval binds the current stop revision" 'true' \
  "$(echo "${resume_request}" | jq -r '(.action=="opscontrol.emergency-stop.clear") and (.resourceId|startswith("emergency-stop:revision-")) and (.bindingDigest|length==64)')"
resume_request_id="$(echo "${resume_request}" | jq -r '.requestId')"
resume_decision="$(curl -fsS "${hdr[@]}" -X POST "${BASE}/background/control-approvals/${resume_request_id}/decision" -d '{"decision":"approved","reason":"Smoke owner approved this exact recovery."}')"
check "approved recovery returns server-issued references" 'true' \
  "$(echo "${resume_decision}" | jq -r '(.decision=="approved") and (.approvalSourceId|startswith("control-decision:")) and (.approvalBindingDigest|length==64)')"
resume_authorization="$(echo "${resume_decision}" | jq -c '{idempotencyKey,taskId,approvalSourceId,approvalBindingDigest}')"
curl -fsS "${hdr[@]}" -X POST "${BASE}/background/resume" -d "${resume_authorization}" >/dev/null
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

mode_request="$(curl -fsS "${hdr[@]}" -X POST "${BASE}/background/control-approvals" -d '{"action":"set_mode","targetMode":"approval_required"}')"
mode_request_id="$(echo "${mode_request}" | jq -r '.requestId')"
mode_decision="$(curl -fsS "${hdr[@]}" -X POST "${BASE}/background/control-approvals/${mode_request_id}/decision" -d '{"decision":"approved","reason":"Smoke owner approved this exact mode increase."}')"
mode_payload="$(echo "${mode_decision}" | jq -c '{mode:"approval_required",idempotencyKey,taskId,approvalSourceId,approvalBindingDigest}')"
curl -fsS "${hdr[@]}" -X PATCH "${BASE}/background/mode" -d "${mode_payload}" >/dev/null
check "approved autonomy increase is applied" 'approval_required' \
  "$(curl -sS "${hdr[@]}" "${BASE}/background/status" | jq -r '.storedMode')"
curl -fsS "${hdr[@]}" -X PATCH "${BASE}/background/mode" -d '{"mode":"read_only"}' >/dev/null
check "consumed mode approval cannot be replayed" '403' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${hdr[@]}" -X PATCH "${BASE}/background/mode" -d "${mode_payload}")"
check "replayed approval leaves restrictive mode in place" 'read_only' \
  "$(curl -sS "${hdr[@]}" "${BASE}/background/status" | jq -r '.storedMode')"

echo "==> Crash/reboot recovery reconciles stuck operations"
rec="$(curl -sS "${hdr[@]}" -X POST "${BASE}/windows-runtime/recovery")"
check "recovery endpoint responds with a report" 'true' \
  "$(echo "${rec}" | jq -r 'has("recovered")')"

echo ""
echo "==> Result: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
