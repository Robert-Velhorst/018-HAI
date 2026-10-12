#!/usr/bin/env bash
#
# Account Bridge Contracts smoke test (HAI Phase 2D).
#
# Boots a throwaway PostgreSQL + the real backend and proves the account-bridge
# layer is truthful: the generic JSON feed is a real production path (register ->
# sync -> operations in the ledger, dedupe-idempotent), while Gmail/GitHub/Trello/
# Drive/Calendar are read-only bridge contracts that are credentials_required and
# NEVER report a fake connected status or fake OAuth. The permission registry is
# read-only and grants nothing without a real credential.
#
# Requires: postgres/initdb/pg_ctl/createdb, Go, curl, jq. No Docker.
# Usage: scripts/smoke-account-bridges.sh
set -euo pipefail

allocate_loopback_port() {
  local python_bin=""
  if command -v python3 >/dev/null 2>&1; then
    python_bin="python3"
  elif command -v python >/dev/null 2>&1; then
    python_bin="python"
  else
    echo "A Python interpreter is required to allocate an isolated smoke-test port." >&2
    return 1
  fi

  "${python_bin}" - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
}

# Fixed ports make the independent smoke suites contend on shared runners. Keep
# explicit overrides for local debugging, otherwise use isolated loopback ports.
PG_PORT_OVERRIDE="${PG_PORT:-}"
PG_PORT="${PG_PORT_OVERRIDE:-$(allocate_loopback_port)}"
API_PORT="${API_PORT:-$(allocate_loopback_port)}"
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

echo "==> Preparing a generic JSON feed (envelope format)"
mkdir -p "${FEEDS}" "${WORKSPACE}"
cat > "${FEEDS}/inbox.json" <<'JSON'
{
  "cursor": "cursor-7",
  "items": [
    {"externalId":"e1","title":"Invoice from vendor","content":"Please review the attached invoice","itemType":"email","provider":"gmail","accountLabel":"primary"},
    {"externalId":"g1","title":"Crash on startup","content":"App crashes on boot","itemType":"issue","provider":"github","accountLabel":"repo"},
    {"externalId":"t1","title":"Design card","content":"Refine the dashboard layout","itemType":"card","provider":"trello","accountLabel":"board"}
  ]
}
JSON

echo "==> Starting throwaway PostgreSQL on :${PG_PORT}"
initdb -D "${PGDATA}" -U "$(whoami)" --auth=trust --locale=C --encoding=UTF8 >/dev/null

# A port selected by the OS can still be claimed before PostgreSQL starts. Retry
# only the implicit CI/default port; an explicit local debugging port should fail
# immediately and retain its diagnostic log.
pg_started=""
pg_attempts=1
[ -z "${PG_PORT_OVERRIDE}" ] && pg_attempts=5
for attempt in $(seq 1 "${pg_attempts}"); do
  [ "${attempt}" -eq 1 ] || PG_PORT="$(allocate_loopback_port)"
  if pg_ctl -D "${PGDATA}" -o "-p ${PG_PORT} -h 127.0.0.1 -k ${WORKDIR}" -l "${PGDATA}/server.log" start >/dev/null; then
    pg_started=1
    break
  fi
  echo "PostgreSQL could not start on :${PG_PORT} (attempt ${attempt}/${pg_attempts}); log:"
  cat "${PGDATA}/server.log" 2>/dev/null || true
done
[ -n "${pg_started}" ] || {
  echo "PostgreSQL could not start after ${pg_attempts} attempt(s)." >&2
  exit 1
}
for i in $(seq 1 30); do
  pg_isready -h 127.0.0.1 -p "${PG_PORT}" >/dev/null 2>&1 && break
  sleep 1
done
createdb -h 127.0.0.1 -p "${PG_PORT}" automation

echo "==> Building and starting backend on :${API_PORT}"
mkdir -p "${IMAGES}"
( cd "${ROOT}/backend" && go build -buildvcs=false -o "${BIN}" ./cmd )

DB_HOST=127.0.0.1 DB_PORT="${PG_PORT}" DB_USER="$(whoami)" DB_PASSWORD=smoke-local-postgres-password \
  DB_NAME=automation SERVER_PORT="${API_PORT}" BASE_URL=/api \
  BACKEND_API_SHARED_KEY="${API_KEY}" IMAGE_SAVE_DIR="${IMAGES}" \
  RUN_MODE=production KAFKA_BROKERS="" JWT_SECRET="${JWT_SECRET}" \
  HAI_PHASE2_FEEDS_DIR="${FEEDS}" HAI_PHASE2_WORKSPACE_DIR="${WORKSPACE}" \
  HAI_PHASE2_FEED_FILES="inbox.json" \
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

echo "==> Authentication boundary"
check "API key alone is rejected" '401' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${key_hdr[@]}" "${BASE}/account-feeds/bridges")"

echo "==> Bridge contracts are truthful (no fake OAuth / no fake connected)"
bridges="$(curl -sS "${hdr[@]}" "${BASE}/account-feeds/bridges")"
check "gmail is a read-only bridge" 'true' \
  "$(echo "${bridges}" | jq -r '[.bridges[]|select(.provider=="gmail")][0].readOnly')"
check "gmail is credentials_required (never connected from config)" 'credentials_required' \
  "$(echo "${bridges}" | jq -r '[.bridges[]|select(.provider=="gmail")][0].connectionStatus')"
check "no bridge reports a connected status" 'true' \
  "$(echo "${bridges}" | jq -r '[.bridges[]|select(.connectionStatus=="connected")]|length==0')"
check "gmail exposes exact setup requirements" 'true' \
  "$(echo "${bridges}" | jq -r '([.bridges[]|select(.provider=="gmail")][0].setupRequirements|length)>0')"
check "generic feed + local folder are available" 'true' \
  "$(echo "${bridges}" | jq -r '([.bridges[]|select(.connectionStatus=="available")]|length)>=2')"

echo "==> Permission registry is read-only and grants nothing without credentials"
perms="$(curl -sS "${hdr[@]}" "${BASE}/account-feeds/permissions")"
check "gmail permission is not granted" 'true' \
  "$(echo "${perms}" | jq -r '[.permissions[]|select(.provider=="gmail")][0].granted==false')"
check "all bridges are read-only" 'true' \
  "$(echo "${perms}" | jq -r '[.permissions[]|select(.readOnly==false)]|length==0')"

echo "==> Generic JSON feed is a real production path (seeded feed present)"
feeds="$(curl -sS "${hdr[@]}" "${BASE}/account-feeds")"
feed_id="$(echo "${feeds}" | jq -r '.feeds[0].feed.id')"
check "seeded generic feed is listed" 'true' "$([ -n "${feed_id}" ] && echo true)"
check "seeded feed connection status is available" 'available' \
  "$(echo "${feeds}" | jq -r '.feeds[0].connectionStatus')"

echo "==> Sync ingests items into the Operation Ledger"
sync="$(curl -sS "${hdr[@]}" -X POST "${BASE}/account-feeds/${feed_id}/sync")"
check "sync read 3 items -> 3 operations" 'true' \
  "$(echo "${sync}" | jq -r '(.itemsRead==3) and (.operationsCreated==3)')"
check "cursor is surfaced" 'cursor-7' "$(echo "${sync}" | jq -r '.cursor')"
check "operations landed in the ledger" 'true' \
  "$(curl -sS "${hdr[@]}" "${BASE}/operations?limit=50" | jq -r '(.operations|length)>=3')"

echo "==> Re-sync is dedupe-idempotent"
sync2="$(curl -sS "${hdr[@]}" -X POST "${BASE}/account-feeds/${feed_id}/sync")"
check "re-sync creates no duplicate operations" 'true' \
  "$(echo "${sync2}" | jq -r '.operationsCreated==0')"

echo "==> Feed audit trail is recorded"
check "feed has an audit trail" 'true' \
  "$(curl -sS "${hdr[@]}" "${BASE}/account-feeds/${feed_id}/audit" | jq -r '(.audit|length)>0')"

echo "==> Registering an API-provider feed does not fake a connection"
created="$(curl -sS "${hdr[@]}" -X POST "${BASE}/account-feeds" \
  -d '{"name":"gh","provider":"github","sourceType":"local_json_file","path":"inbox.json","accountLabel":"repo","enabled":true}')"
gh_id="$(echo "${created}" | jq -r '.id')"
check "github feed registers" 'true' "$([ -n "${gh_id}" ] && echo true)"
check "github feed is credentials_required (no fake connect)" 'credentials_required' \
  "$(curl -sS "${hdr[@]}" "${BASE}/account-feeds" | jq -r '[.feeds[]|select(.feed.id=="'"${gh_id}"'")][0].connectionStatus')"

echo ""
echo "==> Result: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
