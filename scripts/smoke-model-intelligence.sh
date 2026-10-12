#!/usr/bin/env bash
#
# Model Intelligence smoke test (HAI Phase 2B).
#
# Boots a throwaway PostgreSQL + the real backend and proves the architecture-
# aware model layer is truthful and behavioral: honest provider/model states
# (local test providers active, unconfigured remote providers not_configured and
# never auto-active), a real benchmark that promotes only when it actually runs,
# truthful hardware detection + serving-stack selection, a deterministic privacy
# scan that redacts secrets, and the fast-triage lane actually affecting an
# Operation (stamping its model provider) with telemetry + a lane winner.
#
# Requires: postgres/initdb/pg_ctl/createdb, Go, curl, jq. No Docker.
# Usage: scripts/smoke-model-intelligence.sh
set -euo pipefail

PG_PORT="${PG_PORT:-55434}"
API_PORT="${API_PORT:-18082}"
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
  {"externalId":"note-1","title":"Organize workspace notes","body":"Consolidate personal notes into a local file"}
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
key_hdr=(-H "X-HAI-Backend-Key: ${API_KEY}" -H "Content-Type: application/json")
hdr=("${key_hdr[@]}" -H "Authorization: Bearer ${owner_jwt}")

echo "==> Authentication boundary"
check "API key alone is rejected" '401' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${key_hdr[@]}" "${BASE}/model-intelligence/overview")"

echo "==> Truthful provider/model states"
overview="$(curl -sS "${hdr[@]}" "${BASE}/model-intelligence/overview")"
check "test-fast-triage provider is active" 'true' \
  "$(echo "${overview}" | jq -r '[.providers[]|select(.id=="test-fast-triage")][0].status=="active"')"
check "dspark is not_configured (never auto-active)" 'true' \
  "$(echo "${overview}" | jq -r '[.providers[]|select(.id=="dspark")][0].status=="not_configured"')"
check "ollama is not_configured" 'true' \
  "$(echo "${overview}" | jq -r '[.providers[]|select(.id=="ollama")][0].status=="not_configured"')"
check "nine routing lanes exposed" 'true' "$(echo "${overview}" | jq -r '(.lanes|length)==9')"
check "profiles listed" 'triage-rules-v1' "$(curl -sS "${hdr[@]}" "${BASE}/model-intelligence/profiles")"

echo "==> Benchmark promotes only when it truly runs"
bench="$(curl -sS "${hdr[@]}" -X POST "${BASE}/model-intelligence/profiles/test-fast-triage/triage-rules-v1/benchmark")"
check "local provider benchmark runs + promotes to benchmarked" 'true' \
  "$(echo "${bench}" | jq -r '(.ok==true) and (.claimLevel=="benchmarked")')"
bench_dspark="$(curl -sS "${hdr[@]}" -X POST "${BASE}/model-intelligence/profiles/dspark/dspark-default/benchmark")"
check "unconfigured provider benchmark is honest (not ok, not promoted)" 'true' \
  "$(echo "${bench_dspark}" | jq -r '(.ok==false) and (.claimLevel!="benchmarked")')"

echo "==> Truthful hardware profile + serving stack"
hw="$(curl -sS "${hdr[@]}" "${BASE}/hardware/profile")"
check "hardware OS reported" 'true' "$(echo "${hw}" | jq -r '(.profile.operatingSystem|length)>0')"
check "serving stack is a real detected path (cpu on CI)" 'onnx_runtime_cpu' \
  "$(echo "${hw}" | jq -r '.selectedServingStack')"
check "GPU/NPU unknown without detection" 'true' \
  "$(echo "${hw}" | jq -r '(.profile.gpuVendor=="unknown") and (.profile.npuVendor=="unknown")')"
check "hardware detect endpoint works" '200' \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${hdr[@]}" -X POST "${BASE}/hardware/detect")"

echo "==> Power policy + privacy scan"
check "power policy reachable" 'mode' "$(curl -sS "${hdr[@]}" "${BASE}/power/policy")"
scan="$(curl -sS "${hdr[@]}" -X POST "${BASE}/privacy/scan" \
  -d '{"content":"my api_key = sk-live-ABCDEF1234567890 and email a@b.com"}')"
check "privacy scan redacts the secret" 'REDACTED' "$(echo "${scan}" | jq -r '.result.redactedPreview')"
check "secret content is not safe for cloud model" 'true' \
  "$(echo "${scan}" | jq -r '.result.safeForCloudModel==false')"
check "raw secret never returned" 'true' \
  "$(echo "${scan}" | jq -r '(.result.redactedPreview|test("sk-live-ABCDEF"))|not')"
check "privacy scan is retrievable" 'scan-' "$(curl -sS "${hdr[@]}" "${BASE}/privacy/scans")"

echo "==> Fast-triage lane affects a real Operation + telemetry"
curl -sS "${hdr[@]}" -X POST "${BASE}/background/run" >/dev/null
op="$(curl -sS "${hdr[@]}" "${BASE}/operations?limit=1")"
check "operation stamped with fast-triage model provider" 'test-fast-triage' \
  "$(echo "${op}" | jq -r '.operations[0].modelProviderId')"
check "model telemetry recorded from the lane" 'true' \
  "$(curl -sS "${hdr[@]}" "${BASE}/model-intelligence/telemetry" | jq -r '(.telemetry|length)>0')"
check "fast-triage lane has a winner" 'fast_triage' \
  "$(curl -sS "${hdr[@]}" "${BASE}/model-intelligence/lane-winners")"

echo "==> Token budgets are conservative by default"
budgets="$(curl -sS "${hdr[@]}" "${BASE}/model-intelligence/token-budgets")"
check "default context strategy is evidence_only" 'evidence_only' "$(echo "${budgets}" | jq -r '.contextStrategy')"
check "default reasoning effort is low" 'low' "$(echo "${budgets}" | jq -r '.maximumReasoningEffort')"

echo "==> Model telemetry is durable across a real backend restart (§18)"
before="$(curl -sS "${hdr[@]}" "${BASE}/model-intelligence/telemetry" | jq -r '.telemetry | length')"
kill "${BACKEND_PID}" 2>/dev/null; wait "${BACKEND_PID}" 2>/dev/null || true
start_backend; wait_live
after="$(curl -sS "${hdr[@]}" "${BASE}/model-intelligence/telemetry" | jq -r '.telemetry | length')"
check "telemetry persisted before restart" 'true' "$([ "${before}" -ge 1 ] && echo true)"
check "telemetry survives restart (durable)" 'true' "$([ "${after}" -ge "${before}" ] && [ "${after}" -ge 1 ] && echo true)"

echo ""
echo "==> Result: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
