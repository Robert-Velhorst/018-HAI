#!/usr/bin/env bash
#
# Background Operations smoke test (HAI Phase 2A).
#
# Boots a throwaway PostgreSQL, runs the backend against it, and exercises the
# autonomous back-office vertical slice end-to-end: a local account feed is
# ingested into the Operation Ledger; the background loop classifies each item,
# auto-executes the low-risk one through the local safe worker and verifies it,
# and routes the high-risk one to human approval. Proves the anti-fake rules:
# completion requires passing verification, and a non-safe operation cannot be
# executed (no real runtime exists in 2A). Tears everything down on exit.
#
# Requires: a local `postgres`/`initdb`/`pg_ctl`/`createdb`, Go, curl, jq. Does
# NOT require Docker.
#
# Usage: scripts/smoke-background-operations.sh
set -euo pipefail

PG_PORT="${PG_PORT:-55433}"
API_PORT="${API_PORT:-18081}"
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

echo "==> Preparing local account feed"
mkdir -p "${FEEDS}" "${WORKSPACE}"
cat > "${FEEDS}/inbox.json" <<'JSON'
[
  {"externalId":"note-1","title":"Organize workspace notes","body":"Consolidate personal notes into a local file"},
  {"externalId":"pay-1","title":"Pay invoice to landlord","body":"Send payment for the rent invoice"}
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
( cd "${ROOT}/backend" && go build -buildvcs=false -o "${BIN}" ./cmd )

start_backend() {
  DB_HOST=127.0.0.1 DB_PORT="${PG_PORT}" DB_USER="$(whoami)" DB_PASSWORD=smoke-local-postgres-password \
    DB_NAME=automation SERVER_PORT="${API_PORT}" BASE_URL=/api \
    BACKEND_API_SHARED_KEY="${API_KEY}" IMAGE_SAVE_DIR="${IMAGES}" \
    RUN_MODE=production KAFKA_BROKERS="" JWT_SECRET="${JWT_SECRET}" \
    HAI_PHASE2_FEEDS_DIR="${FEEDS}" HAI_PHASE2_WORKSPACE_DIR="${WORKSPACE}" \
    HAI_PHASE2_FEED_FILES="inbox.json" HAI_PHASE2_MODE="autonomous_safe" \
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
forged_jwt="$(hai_smoke_mint_jwt owner "not-${JWT_SECRET}" forged-operator)"
key_hdr=(-H "X-HAI-Backend-Key: ${API_KEY}" -H "Content-Type: application/json")
jwt_hdr=(-H "Content-Type: application/json" -H "Authorization: Bearer ${owner_jwt}")
hdr=("${key_hdr[@]}" -H "Authorization: Bearer ${owner_jwt}")

echo "==> Authentication boundary"
check "API key alone is rejected" '401' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${key_hdr[@]}" "${BASE}/operations")"
check "owner JWT alone is rejected" '401' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${jwt_hdr[@]}" "${BASE}/operations")"
check "wrongly signed owner JWT is rejected" '401' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${key_hdr[@]}" \
    -H "Authorization: Bearer ${forged_jwt}" "${BASE}/operations")"

echo "==> Owner activates the durable local execution baseline"
hai_smoke_activate_baseline_constitution "${BASE}" "${API_KEY}" "${owner_jwt}"
kill "${BACKEND_PID}" 2>/dev/null; wait "${BACKEND_PID}" 2>/dev/null || true
start_backend; wait_live

echo "==> Account feed registered"
check "inbox feed listed" '"name":"inbox"' "$(curl -sS "${hdr[@]}" "${BASE}/account-feeds")"

echo "==> Background loop: ingest -> classify -> execute+verify / approve"
report="$(curl -sS "${hdr[@]}" -X POST "${BASE}/background/run")"
check "two operations created from the feed" 'true' \
  "$(echo "${report}" | jq -r '.operationsCreated == 2')"
check "one low-risk op auto-executed and verified" 'true' \
  "$(echo "${report}" | jq -r '(.autoExecuted == 1) and (.verified == 1)')"
check "one high-risk op routed to approval" 'true' \
  "$(echo "${report}" | jq -r '.awaitingApproval == 1')"

echo "==> Dashboard roll-up"
dash="$(curl -sS "${hdr[@]}" "${BASE}/operations/dashboard")"
check "dashboard shows work done while away" 'true' "$(echo "${dash}" | jq -r '.doneWhileAway >= 1')"
check "dashboard shows an item needing Robert" 'true' "$(echo "${dash}" | jq -r '.needsRobert >= 1')"

echo "==> Completion requires passing verification (no fake completion)"
completed="$(curl -sS "${hdr[@]}" "${BASE}/operations?status=completed")"
comp_id="$(echo "${completed}" | jq -r '.operations[0].id // empty')"
check "completed operation exists" 'true' "$([ -n "${comp_id}" ] && echo true)"
check "completed operation is verification-passed" 'passed' \
  "$(echo "${completed}" | jq -r '.operations[0].verificationStatus')"
check "completed operation records the runtime" 'hai-local-safe-worker' \
  "$(echo "${completed}" | jq -r '.operations[0].runtimeId')"
check "operation carries an audit trail" 'true' \
  "$(curl -sS "${hdr[@]}" "${BASE}/operations/${comp_id}/events" | jq -r '(.events | length) > 1')"

echo "==> Safe artifact was actually written to the confined workspace"
check "workspace artifact present" 'operation-' "$(ls "${WORKSPACE}" 2>/dev/null | tr '\n' ' ')"

echo "==> Anti-fake: a non-safe operation cannot be executed in 2A"
await="$(curl -sS "${hdr[@]}" "${BASE}/operations?status=awaiting_approval")"
await_id="$(echo "${await}" | jq -r '.operations[0].id // empty')"
check "high-risk op is awaiting approval" 'true' "$([ -n "${await_id}" ] && echo true)"
check "running a non-safe op is refused (409)" '409' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${hdr[@]}" -X POST "${BASE}/operations/${await_id}/run")"

echo "==> Idempotency: re-running the loop creates no duplicates"
report2="$(curl -sS "${hdr[@]}" -X POST "${BASE}/background/run")"
check "second pass creates no duplicate operations" 'true' \
  "$(echo "${report2}" | jq -r '.operationsCreated == 0')"

echo ""
echo "==> Result: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
