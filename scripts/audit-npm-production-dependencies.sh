#!/usr/bin/env bash
set -euo pipefail

scope="${1:-production package}"
report="$(mktemp)"
stderr_file="$(mktemp)"
trap 'rm -f "$report" "$stderr_file"' EXIT

validate_report() {
  node - "$report" <<'NODE'
const fs = require("fs");

let report;
try {
  report = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
} catch {
  process.exit(2);
}

const counts = report && report.metadata && report.metadata.vulnerabilities;
if (!counts || typeof counts !== "object") {
  process.exit(2);
}

const high = Number(counts.high);
const critical = Number(counts.critical);
if (!Number.isInteger(high) || high < 0 || !Number.isInteger(critical) || critical < 0) {
  process.exit(2);
}

if (high > 0 || critical > 0) {
  console.error(`confirmed high or critical production dependency advisory: high=${high}, critical=${critical}`);
  process.exit(1);
}
NODE
}

for attempt in 1 2 3; do
  : >"$report"
  : >"$stderr_file"
  set +e
  timeout 90s npm audit --omit=dev --audit-level=high --json >"$report" 2>"$stderr_file"
  audit_exit=$?
  set -e

  set +e
  validate_report
  report_exit=$?
  set -e

  case "$report_exit" in
    0)
      exit 0
      ;;
    1)
      exit 1
      ;;
  esac

  if [ "$attempt" -lt 3 ]; then
    echo "npm audit did not return a trustworthy ${scope} report (exit ${audit_exit}); retrying (${attempt}/3)." >&2
    sleep $((attempt * 10))
  fi
done

echo "::warning::npm audit infrastructure did not return a trustworthy report for ${scope} after 3 bounded attempts; production dependency advisory status was not verified." >&2
exit 2
