#!/usr/bin/env bash
#
# Run all HAI Phase 2 smoke suites (2A-2F) and aggregate the results (Phase 2G
# final smoke pass). Each suite boots its own throwaway PostgreSQL + backend on a
# distinct port and tears everything down on exit.
#
# Requires: postgres/initdb/pg_ctl/createdb, Go, curl, jq. No Docker.
# Usage: scripts/smoke-all.sh
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

SUITES=(
  smoke-background-operations
  smoke-model-intelligence
  smoke-runtime-lab
  smoke-account-bridges
  smoke-windows-runtime
)

overall=0
declare -a summary

for s in "${SUITES[@]}"; do
  echo "############################################################"
  echo "## ${s}"
  echo "############################################################"
  out="$("${ROOT}/scripts/${s}.sh" 2>&1)"
  code=$?
  reported_line="$(printf '%s\n' "${out}" | grep -E '^==> Result:' | tail -1)"
  valid_line="$(printf '%s\n' "${out}" | grep -E '^==> Result: [1-9][0-9]* passed, 0 failed$' | tail -1)"
  printf '%s\n' "${reported_line:-==> Result: missing}"
  if [ "${code}" -eq 0 ] && [ -n "${valid_line}" ]; then
    line="${valid_line}"
    summary+=("PASS  ${s}  (${line#*==> })")
  else
    # Keep the aggregate concise on success, but preserve the child output on
    # failure. Otherwise CI reports only "Result: missing" and hides the
    # startup, migration, or authentication failure that needs recovery.
    echo "==> ${s} failure detail"
    printf '%s\n' "${out}"
    line="${reported_line:-==> Result: missing or invalid}"
    summary+=("FAIL  ${s}  (${line#*==> })")
    overall=1
  fi
  echo
done

echo "############################################################"
echo "## Aggregate"
echo "############################################################"
for row in "${summary[@]}"; do echo "  ${row}"; done
echo
if [ "${overall}" -eq 0 ]; then
  echo "==> ALL PHASE 2 SMOKE SUITES PASSED"
else
  echo "==> ONE OR MORE SUITES FAILED"
fi
exit "${overall}"
