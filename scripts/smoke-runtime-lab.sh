#!/usr/bin/env bash
#
# Runtime Lab smoke test (HAI Phase 2C).
#
# Boots a throwaway PostgreSQL + the real backend and proves HAI is the truthful
# control plane above external runtimes: the local safe worker self-tests through
# the Operation Ledger (real execute + verify), while Hermes/OpenClaw/Odysseus/
# OpenHands are not_configured with exact setup requirements and NEVER report a
# successful self-test or execution. Browser/script runtimes are contracts only.
#
# Requires: postgres/initdb/pg_ctl/createdb, Go, curl, jq. No Docker.
# Usage: scripts/smoke-runtime-lab.sh
set -euo pipefail

PG_PORT="${PG_PORT:-55435}"
API_PORT="${API_PORT:-18083}"
API_KEY="${API_KEY:-smoke-key}"
JWT_SECRET="${JWT_SECRET:-smoke-jwt-secret}"
BASE="http://127.0.0.1:${API_PORT}/api/v1"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/scripts/smoke-auth.sh"

WORKDIR="$(mktemp -d)"
PGDATA="${WORKDIR}/pgdata"
IMAGES="${WORKDIR}/images"
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

echo "==> Starting throwaway PostgreSQL on :${PG_PORT}"
mkdir -p "${WORKSPACE}"
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
    HAI_PHASE2_WORKSPACE_DIR="${WORKSPACE}" \
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
  "$(curl -sS -o /dev/null -w '%{http_code}' "${key_hdr[@]}" "${BASE}/runtime-lab/overview")"

echo "==> Owner activates the durable local execution baseline"
hai_smoke_activate_baseline_constitution "${BASE}" "${API_KEY}" "${owner_jwt}"
kill "${BACKEND_PID}" 2>/dev/null; wait "${BACKEND_PID}" 2>/dev/null || true
start_backend; wait_live

echo "==> Truthful runtime overview"
ov="$(curl -sS "${hdr[@]}" "${BASE}/runtime-lab/overview")"
check "local safe worker is executable" 'true' \
  "$(echo "${ov}" | jq -r '[.runtimes[]|select(.info.id=="hai-local-safe-worker")][0].canExecute')"
check "hermes is not executable" 'true' \
  "$(echo "${ov}" | jq -r '[.runtimes[]|select(.info.id=="hermes")][0].canExecute==false')"
check "hermes exposes exact setup requirements" 'true' \
  "$(echo "${ov}" | jq -r '([.runtimes[]|select(.info.id=="hermes")][0].setupRequirements|length)>0')"
check "all configured external agent runtimes are present" 'true' \
  "$(echo "${ov}" | jq -r '([.runtimes[]|select(.info.kind=="agent_runtime")]|length)==4')"
check "browser runtime is a contract only" 'true' \
  "$(echo "${ov}" | jq -r '[.runtimes[]|select(.info.id=="browser-runtime")][0].canExecute==false')"
check "browser contract publishes forbidden boundary" 'forbidden:send_message' \
  "$(echo "${ov}" | jq -r '[.runtimes[]|select(.info.id=="browser-runtime")][0].capabilities[]')"

echo "==> Local safe worker self-test runs through the Operation Ledger"
st="$(curl -sS "${hdr[@]}" -X POST "${BASE}/runtime-lab/hai-local-safe-worker/self-test")"
check "self-test succeeds + verifies" 'true' \
  "$(echo "${st}" | jq -r '(.status=="succeeded") and (.verificationPassed==true)')"
check "self-test is tied to a ledger operation" 'true' \
  "$(echo "${st}" | jq -r '(.operationId|length)>0')"
op_id="$(echo "${st}" | jq -r '.operationId')"
check "the ledger operation is completed" 'completed' \
  "$(curl -sS "${hdr[@]}" "${BASE}/operations/${op_id}" | jq -r '.status')"

echo "==> External runtimes NEVER fake a self-test or execution"
for rt in hermes openclaw odysseus openhands; do
  res="$(curl -sS "${hdr[@]}" -X POST "${BASE}/runtime-lab/${rt}/self-test")"
  check "${rt} self-test is setup_required (never succeeded)" 'setup_required' "$(echo "${res}" | jq -r '.status')"
done

echo "==> Probes are truthful"
check "hermes probe reports not_configured" 'not_configured' \
  "$(curl -sS "${hdr[@]}" -X POST "${BASE}/runtime-lab/hermes/probe" | jq -r '.status')"
check "safe worker probe is ready" 'ready' \
  "$(curl -sS "${hdr[@]}" -X POST "${BASE}/runtime-lab/hai-local-safe-worker/probe" | jq -r '.status')"

echo "==> Attempts are recorded"
check "safe worker attempts recorded" 'true' \
  "$(curl -sS "${hdr[@]}" "${BASE}/runtime-lab/hai-local-safe-worker/attempts" | jq -r '(.attempts|length)>0')"

echo ""
echo "==> Result: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
