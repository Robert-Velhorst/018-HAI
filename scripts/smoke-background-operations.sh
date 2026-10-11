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
  {"externalId":"note-1","title":"Organize workspace notes","content":"Consolidate personal notes into a local file","itemType":"email","provider":"generic_json_feed"},
  {"externalId":"pay-1","title":"Pay invoice to landlord","content":"Send payment for the rent invoice","itemType":"email","provider":"generic_json_feed"}
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

DB_HOST=127.0.0.1 DB_PORT="${PG_PORT}" DB_USER="$(whoami)" DB_PASSWORD=hai-ci-smoke-postgres-password-0123456789abcdef \
  DB_NAME=automation SERVER_PORT="${API_PORT}" BASE_URL=/api \
  BACKEND_API_SHARED_KEY="${API_KEY}" IMAGE_SAVE_DIR="${IMAGES}" \
  RUN_MODE=test HAI_PHASE2_TEST_DIAGNOSTICS=1 KAFKA_BROKERS="" JWT_SECRET="${JWT_SECRET}" \
  HAI_PHASE2_FEEDS_DIR="${FEEDS}" HAI_PHASE2_WORKSPACE_DIR="${WORKSPACE}" \
  HAI_PHASE2_FEED_FILES="inbox.json" HAI_PHASE2_MODE="autonomous_safe" \
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
forged_jwt="$(hai_smoke_mint_jwt owner "not-${JWT_SECRET}" forged-operator)"
key_hdr=(-H "X-HAI-Backend-Key: ${API_KEY}" -H "Content-Type: application/json")
jwt_hdr=(-H "Content-Type: application/json" -H "Authorization: Bearer ${owner_jwt}")
hdr=("${key_hdr[@]}" -H "Authorization: Bearer ${owner_jwt}")

echo "==> Owner activates the bounded smoke execution policy"
hai_smoke_activate_execution_policy "${BASE}" "${hdr[@]}"
kill "${BACKEND_PID}" 2>/dev/null; wait "${BACKEND_PID}" 2>/dev/null || true
BACKEND_PID=""
DB_HOST=127.0.0.1 DB_PORT="${PG_PORT}" DB_USER="$(whoami)" DB_PASSWORD=hai-ci-smoke-postgres-password-0123456789abcdef \
  DB_NAME=automation SERVER_PORT="${API_PORT}" BASE_URL=/api \
  BACKEND_API_SHARED_KEY="${API_KEY}" IMAGE_SAVE_DIR="${IMAGES}" \
  RUN_MODE=test HAI_PHASE2_TEST_DIAGNOSTICS=1 KAFKA_BROKERS="" JWT_SECRET="${JWT_SECRET}" \
  HAI_PHASE2_FEEDS_DIR="${FEEDS}" HAI_PHASE2_WORKSPACE_DIR="${WORKSPACE}" \
  HAI_PHASE2_FEED_FILES="inbox.json" HAI_PHASE2_MODE="autonomous_safe" \
  "${BIN}" > "${WORKDIR}/backend.log" 2>&1 &
BACKEND_PID=$!
ready=""
for i in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:${API_PORT}/healthz" >/dev/null 2>&1; then ready=1; break; fi
  if ! kill -0 "${BACKEND_PID}" 2>/dev/null; then
    echo "backend exited after execution policy activation; log:"; tail -20 "${WORKDIR}/backend.log"; exit 1
  fi
  sleep 1
done
[ -n "${ready}" ] || { echo "backend did not restart after execution policy activation"; exit 1; }

echo "==> Authentication boundary"
check "API key alone is rejected" '401' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${key_hdr[@]}" "${BASE}/operations")"
check "owner JWT alone is rejected" '401' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${jwt_hdr[@]}" "${BASE}/operations")"
check "wrongly signed owner JWT is rejected" '401' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${key_hdr[@]}" \
    -H "Authorization: Bearer ${forged_jwt}" "${BASE}/operations")"

echo "==> Account feed registered"
check "inbox feed listed" '"name":"inbox"' "$(curl -sS "${hdr[@]}" "${BASE}/account-feeds")"

echo "==> Background loop: ingest -> classify -> execute+verify / approve"
run_response="$(curl -sS -w $'\n%{http_code}' "${hdr[@]}" -X POST "${BASE}/background/run")"
run_status="${run_response##*$'\n'}"
report="${run_response%$'\n'*}"
if [ "${run_status}" != "200" ]; then
  echo "background run failed with HTTP ${run_status}: $(echo "${report}" | jq -c '{error, reasonCode, code}')" >&2
fi
check "background run returns HTTP 200" '200' "${run_status}"
check "two operations created from the feed" 'true' \
  "$(echo "${report}" | jq -r '.operationsCreated == 2')"
await="$(curl -sS "${hdr[@]}" "${BASE}/operations?status=awaiting_approval")"
safe_id="$(echo "${await}" | jq -r '(.operations // [])[] | select(.title == "Organize workspace notes") | .id' | head -n 1)"
high_id="$(echo "${await}" | jq -r '(.operations // [])[] | select(.title == "Pay invoice to landlord") | .id' | head -n 1)"
check "both source-derived operations require owner review" '2' "$(echo "${report}" | jq -r '.awaitingApproval')"
check "safe candidate is in the owner review queue" 'true' "$([ -n "${safe_id}" ] && echo true)"
check "high-risk candidate is in the owner review queue" 'true' "$([ -n "${high_id}" ] && echo true)"
check "unreviewed source work did not execute" 'true' \
  "$(echo "${report}" | jq -r '(.autoExecuted == 0) and (.verified == 0)')"

echo "==> Owner reviews and approves the exact safe-source revision"
preview="$(curl -sS "${hdr[@]}" "${BASE}/operations/${safe_id}/approval-preview")"
reviewed_version="$(echo "${preview}" | jq -r '.revision.version // 0')"
revision_digest="$(echo "${preview}" | jq -r '.revision.revisionDigest // empty')"
approval_body="$(jq -nc --argjson expectedVersion "${reviewed_version}" --arg revisionDigest "${revision_digest}" '{expectedVersion:$expectedVersion,revisionDigest:$revisionDigest}')"
approved="$(curl -sS "${hdr[@]}" -X POST "${BASE}/operations/${safe_id}/approve" -d "${approval_body}")"
check "approval is bound to the reviewed revision" 'true' \
  "$(echo "${approved}" | jq -r --arg id "${safe_id}" --arg digest "${revision_digest}" '(.id == $id) and (.status == "approved") and (.approvalReceipt.operationId == $id) and (.approvalReceipt.revisionDigest == $digest)')"

echo "==> Background worker executes only the explicitly approved safe item"
process_response="$(curl -sS -w $'\n%{http_code}' "${hdr[@]}" -X POST "${BASE}/background/run")"
process_status="${process_response##*$'\n'}"
process_report="${process_response%$'\n'*}"
if [ "${process_status}" != "200" ]; then
  echo "approved background run failed with HTTP ${process_status}: $(echo "${process_report}" | jq -c '{error, diagnostics}')" >&2
  if [ -n "${safe_id}" ]; then
    if recent_events="$(curl -fsS "${hdr[@]}" "${BASE}/operations/${safe_id}/events" | \
      jq -c '{events: [.events[-5:][]? | {eventType, beforeStatus, afterStatus, message}]}')"; then
      echo "approved operation recent audit events: ${recent_events}" >&2
    else
      echo "approved operation audit events were unavailable" >&2
    fi
  fi
fi
check "approved background run returns HTTP 200" '200' "${process_status}"
check "one approved safe operation executed and verified" 'true' \
  "$(echo "${process_report}" | jq -r '(.autoExecuted == 1) and (.verified == 1) and (.operationsCreated == 0)')"
remaining="$(curl -sS "${hdr[@]}" "${BASE}/operations?status=awaiting_approval")"
check "high-risk operation remains approval-gated" 'true' \
  "$(echo "${remaining}" | jq -r --arg id "${high_id}" 'any(.operations[]?; .id == $id and .status == "awaiting_approval")')"

echo "==> Dashboard roll-up and verified completion"
dash="$(curl -sS "${hdr[@]}" "${BASE}/operations/dashboard")"
check "dashboard shows work done after approval" 'true' "$(echo "${dash}" | jq -r '.doneWhileAway >= 1')"
check "dashboard still shows work needing Robert" 'true' "$(echo "${dash}" | jq -r '.needsRobert >= 1')"
completed="$(curl -sS "${hdr[@]}" "${BASE}/operations?status=completed")"
check "approved operation completed with passing verification" 'true' \
  "$(echo "${completed}" | jq -r --arg id "${safe_id}" 'any(.operations[]?; .id == $id and .verificationStatus == "passed")')"
check "completed operation records the runtime" 'true' \
  "$(echo "${completed}" | jq -r --arg id "${safe_id}" 'any(.operations[]?; .id == $id and .runtimeId == "hai-local-safe-worker")')"
check "operation carries an audit trail" 'true' \
  "$(curl -sS "${hdr[@]}" "${BASE}/operations/${safe_id}/events" | jq -r '(.events | length) > 1')"

echo "==> Safe artifact was actually written to the confined workspace"
check "workspace artifact present" 'operation-' "$(ls "${WORKSPACE}" 2>/dev/null | tr '\n' ' ')"

echo "==> Anti-fake: a high-risk operation cannot be executed in 2A"
check "running the high-risk operation is refused (409)" '409' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${hdr[@]}" -X POST "${BASE}/operations/${high_id}/run")"

echo "==> Idempotency: re-running the loop creates no duplicates"
report3="$(curl -sS "${hdr[@]}" -X POST "${BASE}/background/run")"
check "later pass creates no duplicate operations" 'true' \
  "$(echo "${report3}" | jq -r '.operationsCreated == 0')"

echo ""
echo "==> Result: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
