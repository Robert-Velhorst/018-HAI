# Local script reconciliation and smoke diagnostic repair

## Inventory boundary

A fresh read-only pass examined the original checkout's 1,578 untracked files
against main `e08d4bc029d7631b740f857051b6363324b29b5a`. It selected 64 files
under source, test, documentation, script and service-configuration directories;
all 64 paths exist on main, but none are byte-identical or equal after CRLF-only
normalization. No read errors occurred for these selected comparisons.

The other 1,514 entries were excluded operational state, outputs, logs or test
evidence. They are not classified as disposable. Existence of a matching path
is not proof that useful local content is integrated. The private comparison
report retains file identities; this public report contains no private paths
or transcript identifiers. No original file is deleted.

## Directly reviewed script differences

- `test-native-go-discovery.ps1`: current main retains the synthetic discovery
  checks and updates the cached toolchain fixture to the declared Go 1.27.3.
  Do not restore the older fixture version.
- `test-native-compile-deadline.ps1`: current main increases deadlines for
  successful and failing synthetic child-process launches from 5 to 60 seconds.
  The explicit real-timeout and contention checks remain; do not restore flaky
  normal-startup deadlines as a supposed missing feature.
- `test-frontend-build-resource-limit.cjs`: current main tests Angular's actual
  custom-worker flag and invalid-value fallback and uses a pinned Nginx image.
  The older parser expectations and floating image are not missing behavior.
- `python_runner_container_smoke.py`: current main adds Grype's synthetic
  offline configuration, preserves failed containers until explicit cleanup,
  and captures exit diagnostics. Keep these improvements instead of reverting
  to the older generic failure message or automatic container removal.

These decisions cover the reviewed differences, not the remaining 60 selected
files or complete worktree/archival equivalence.

A subsequent ten-file authentication-test comparison is recorded in
[Authentication test reconciliation](auth-test-reconciliation-2026-10-20.md).
It reviews retained assertions separately; neither report treats equal paths
or older API signatures as raw-file identity or blanket cleanup permission.

The subsequent [backend test review](backend-test-reconciliation-2026-10-20.md)
covers the 25 selected backend and migration test files. It distinguishes
retained coverage from deliberately superseded safety expectations.

## Reproduced current defects

The newer diagnostic path calls `json.loads` without importing `json`, causing
a `NameError` when inspecting an exited container. Its log redaction also does
not cover state errors, failed startup, failed probes, cleanup errors or CLI
timeout messages that can include command arguments.

Regressions using existing synthetic runner configuration reproduced the
missing import and unredacted known token values before production edits.
This does not establish exposure of a real credential or a deployed incident.

## Repair

Import the standard-library JSON module and apply one bounded diagnostic helper
to the affected outputs. Redact known configured sensitive environment values
before whitespace normalization and truncation, including state-error text and
CLI exceptions. Preserve useful exit/OOM information and guaranteed cleanup.
No dependencies, runner permissions, network access or CI gates are broadened.

This is exact-value redaction for the smoke runner's known configuration, not a
general detector for arbitrary secrets. All 19 Python runner CI-policy and
container-smoke contract tests passed after the repair. They cover an exited
container through the actual smoke function with a stubbed process runner,
known-value redaction, bounded output and cleanup. `git diff --check` passed.
Hosted exact-head CI remains a merge gate. No real Docker stack or provider was
started locally.

## Retention

Original scripts, logs, database backups, runtime state and worktrees remain
intact. Do not treat this report as main-preservation proof for unreviewed
files, or as permission to remove the excluded operational material.
