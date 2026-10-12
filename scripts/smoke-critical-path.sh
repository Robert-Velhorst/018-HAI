#!/usr/bin/env bash
#
# Critical-path smoke test (phases 003 / 011 / 043).
#
# Boots a throwaway PostgreSQL, runs the backend against it, and exercises the
# critical path end-to-end: readiness -> memory create -> memory search ->
# workflow overview -> grounded/system surfaces. Tears everything down on exit.
#
# Requires: a local `postgres`/`initdb`/`pg_ctl`/`createdb`, Go, curl, and jq.
# Does NOT require Docker.
#
# Usage: scripts/smoke-critical-path.sh
set -euo pipefail

PG_PORT="${PG_PORT:-55432}"
API_PORT="${API_PORT:-18080}"
API_KEY="${API_KEY:-smoke-key}"
JWT_SECRET="${JWT_SECRET:-smoke-jwt-secret}"
BASE="http://127.0.0.1:${API_PORT}/api/v1"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/scripts/smoke-auth.sh"

WORKDIR="$(mktemp -d)"
PGDATA="${WORKDIR}/pgdata"
IMAGES="${WORKDIR}/images"
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
( cd "${ROOT}/backend" && go build -buildvcs=false -o "${BIN}" ./cmd )

# Trust auth ignores the password, but libpq needs a non-empty, non-default value in the DSN.
DB_HOST=127.0.0.1 DB_PORT="${PG_PORT}" DB_USER="$(whoami)" DB_PASSWORD=smoke-local-postgres-password \
  DB_NAME=automation SERVER_PORT="${API_PORT}" BASE_URL=/api \
  BACKEND_API_SHARED_KEY="${API_KEY}" IMAGE_SAVE_DIR="${IMAGES}" \
  RUN_MODE=production KAFKA_BROKERS="" JWT_SECRET="${JWT_SECRET}" \
  "${BIN}" > "${WORKDIR}/backend.log" 2>&1 &
BACKEND_PID=$!

echo "==> Waiting for liveness"
ready=""
for i in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:${API_PORT}/healthz" >/dev/null 2>&1; then ready=1; break; fi
  if ! kill -0 "${BACKEND_PID}" 2>/dev/null; then
    echo "backend exited early; log:"; tail -20 "${WORKDIR}/backend.log"; exit 1
  fi
  sleep 1
done
[ -n "${ready}" ] || { echo "backend never became live; log:"; tail -20 "${WORKDIR}/backend.log"; exit 1; }

owner_jwt="$(hai_smoke_mint_jwt owner "${JWT_SECRET}")"
key_hdr=(-H "X-HAI-Backend-Key: ${API_KEY}" -H "Content-Type: application/json")
hdr=("${key_hdr[@]}" -H "Authorization: Bearer ${owner_jwt}")

echo "==> Critical path"
check "healthz ok" '"status":"ok"' "$(curl -fsS http://127.0.0.1:${API_PORT}/healthz)"
readyz_http="$(curl -sS -o "${WORKDIR}/readyz.json" -w '%{http_code}' "http://127.0.0.1:${API_PORT}/readyz")"
check "readyz serves an operational response" '200' "${readyz_http}"
check "readyz is ready or degraded, never not_ready" 'true' \
  "$(jq -r '(.status == "ready") or (.status == "degraded")' "${WORKDIR}/readyz.json")"
check "API key alone is rejected on protected routes" '401' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${key_hdr[@]}" "${BASE}/memory/query?projectKey=smoke&q=angular")"

created="$(curl -sS "${hdr[@]}" -X POST "${BASE}/memory/" \
  -d '{"projectKey":"smoke","kind":"note","content":"Angular dashboard with Go backend","tags":["stack"]}')"
check "memory create" 'Angular dashboard' "${created}"

check "memory search finds it" 'Angular dashboard' \
  "$(curl -sS "${hdr[@]}" "${BASE}/memory/query?projectKey=smoke&q=angular%20backend")"

check "workflow overview" '{' "$(curl -sS "${hdr[@]}" "${BASE}/workflow/overview")"
check "os overview" '{' "$(curl -sS "${hdr[@]}" "${BASE}/os/overview")"
check "system info build" 'goVersion' "$(curl -sS "${hdr[@]}" "${BASE}/system/info")"

echo "==> Workflow lifecycle (intake -> approval gate -> resolve -> audit trail)"
intake="$(curl -sS "${hdr[@]}" -X POST "${BASE}/workflow/intake" \
  -d '{"input":"Email from lawyer about legal hearing. Draft a formal reply for review only."}')"
wf_id="$(echo "${intake}" | jq -r '.item.id // empty')"
check "intake created a workflow item" 'true' "$([ -n "${wf_id}" ] && echo true)"
check "item carries an approval gate" 'true' "$(echo "${intake}" | jq -r '(.item|has("approvalStatus"))')"
check "item persisted & fetchable by id" "${wf_id}" "$(curl -sS "${hdr[@]}" "${BASE}/workflow/${wf_id}" | jq -r '.item.id')"
check "approvals queue reachable" '200' "$(curl -sS -o /dev/null -w '%{http_code}' "${hdr[@]}" "${BASE}/workflow/approvals")"
check "workflow dashboard reachable" '200' "$(curl -sS -o /dev/null -w '%{http_code}' "${hdr[@]}" "${BASE}/workflow/dashboard")"

resolved="$(curl -sS "${hdr[@]}" -X POST "${BASE}/workflow/${wf_id}/approval" \
  -d '{"approved":true,"note":"Reviewed and approved this exact draft.","actor":"smoke"}')"
check "approval gate resolved" 'true' "$(echo "${resolved}" | jq -r 'has("item")')"
check "audit trail recorded (events/transitions/decisions)" 'true' \
  "$(echo "${resolved}" | jq -r '(((.events // [])|length) + ((.transitions // [])|length) + ((.decisions // [])|length)) > 0')"
check "verification runs surface reachable" '200' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${hdr[@]}" "${BASE}/verification/runs")"

echo "==> Grounded verification (anti-hallucination engine)"
ver_grounded="$(curl -sS "${hdr[@]}" -X POST "${BASE}/verification/answer" -d '{
  "question":"What is the dashboard built with?",
  "draftAnswer":"The dashboard is built with Angular and the backend in Go.",
  "projectKey":"smoke",
  "externalEvidence":[{"sourceType":"note","snippet":"The dashboard is built with Angular and the backend in Go.","official":true,"primary":true}]
}')"
check "grounded run created with claims" 'true' \
  "$(echo "${ver_grounded}" | jq -r '(.run.id != null) and ((.claims|length) > 0)')"
ver_none="$(curl -sS "${hdr[@]}" -X POST "${BASE}/verification/answer" \
  -d '{"question":"What is the private home address of the CEO?","projectKey":"no-evidence-here"}')"
check "refuses to fabricate without evidence (anti-hallucination)" 'No grounded answer' \
  "$(echo "${ver_none}" | jq -r '.run.answer')"

echo "==> Authorization (per-user RBAC via IDP-style JWT)"
viewer_jwt="$(hai_smoke_mint_jwt viewer "${JWT_SECRET}" smoke-viewer)"
check "viewer JWT denied on admin support-bundle (403)" '403' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${key_hdr[@]}" -H "Authorization: Bearer ${viewer_jwt}" "${BASE}/system/support-bundle")"
check "owner JWT allowed on admin support-bundle (200)" '200' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${hdr[@]}" "${BASE}/system/support-bundle")"

echo ""
echo "==> Result: ${pass} passed, ${fail} failed"
echo "Note: the grounded-verification (evidence→claim) engine IS exercised above. LLM answer *generation* (task planning) is not — no LLM provider is configured in this smoke."
[ "${fail}" -eq 0 ]
