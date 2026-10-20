# Non-Go source reconciliation

## Scope

This accounts for the 29 selected non-Go files in the original checkout's
64-file source/test/configuration subset. The other 35 files are covered by
the [authentication](auth-test-reconciliation-2026-10-20.md) and
[backend](backend-test-reconciliation-2026-10-20.md) reviews. The comparison
base is main `847ce573a65659abca85bb8897a42e386ab8fe2c`.

Script, test and document diffs were read directly. JSON lockfiles were parsed
as structured data; dependency-graph metadata review is not provider/runtime
equivalence proof. The 1,514 excluded operational entries and all tracked local
production differences remain outside this batch.

## Decisions

| Path | Review decision |
| --- | --- |
| `docs/isolated-acceptance.md` | Main constructs the sensitive environment-variable name in two pieces; the same save/restore behavior remains. Keep the current example. |
| `frontend/e2e/tests/support/owned-browser-matrix-handoff.md` | Main records synthetic session rotation and supersedes the old persisted-task-history source-gap note, while explicitly leaving browser acceptance open. Do not restore a stale gap as current fact. |
| `frontend/e2e/tests/support/owned-browser-matrix.ts` | Keep validated run-scoped synthetic storage state, serial session refresh, credential-free diagnostics and UI-specific readiness. Main deliberately replaces per-test password login and global network-idle assumptions. This review is not a live browser or every-network-boundary acceptance claim. |
| `frontend/scripts/control-room-logic.test.mjs` | Keep the module-URL loader, valid same-ID refresh handling, changed-selection rejection, authoritative review readback, exact selection detail route and uncapped attention counts. Older object-identity and capped-state expectations are superseded. |
| `frontend/scripts/deep-link-focus.test.mjs` | Main loads the split module-URL logic and supplies its bridge to the real shell loader. Existing deep-link focus assertions remain. |
| `frontend/scripts/framework-secret-boundary.test.mjs` | Main adds actual-newline RSA/OpenSSH fixtures but omits the old generic-header/literal-escape combinations. Recover those combinations additively, preserving the actual separator rather than expecting escaped input to become a real newline. |
| `frontend/scripts/release-cache.test.mjs` | Existing immutable-bundle/no-store checks remain; main additionally tests no critical-style inlining and same-origin theme bootstrap under restrictive CSP. |
| `scripts/audit-npm-production-dependencies.sh` | Main validates an explicit include/omit-optional mode while retaining inclusive auditing by default and the existing report validation/retry policy. Do not drop optional packages silently. |
| `scripts/isolated-acceptance-stack.ps1` | Keep dependency validation, scrubbed synthetic configuration, interrupted-preparation provenance, pinned PostgreSQL prerequisites, runtime-owned private tmpfs and bounded gateway readiness retries. Do not restore persistent-volume helpers into the disposable fixture. |
| `scripts/isolated-recovery-rehearsal.ps1` | Main loads backup code with `-LibraryOnly`, avoiding accidental backup execution while importing helpers. Preserve that boundary. |
| `scripts/python_runner_container_smoke.py` | See the earlier script review and merged diagnostic repair: keep Grype's synthetic configuration, bounded offline containers, explicit cleanup and redacted failure diagnostics. |
| `scripts/test-frontend-build-resource-limit.cjs` | See the earlier script review: preserve current Angular parser expectations and pinned runtime image instead of restoring old parser behavior. |
| `scripts/test-hai-phase2-state-permissions-contract.ps1` | Main tests reuse of the backend image and direct no-dereference ownership repair with CHOWN only, retaining offline/read-only/resource limits. The older utility image and extra capability are not needed. |
| `scripts/test-isolated-acceptance-stack.ps1` | Main preserves isolation checks and adds transient gateway, scrubbed-credential, runtime-UID tmpfs and interrupted-preparation provenance assertions. Retired tunnel-profile assumptions are not restored. |
| `scripts/test-native-compile-deadline.ps1` | See the earlier script review: normal child-start deadlines increased; explicit timeout and contention cases remain. |
| `scripts/test-native-go-discovery.ps1` | See the earlier script review: the cached discovery fixture follows the declared toolchain, without adding download/install behavior. |
| `scripts/test-openclaw-maintenance-migration.ps1` | Main compares the relevant access descriptor and tests removal of retired public-tunnel settings even during maintenance opt-out. Existing token/text preservation checks remain. |
| `scripts/test-windows-installer-install.ps1` | Keep explicit hosted-runner-only Docker-unavailability opt-in, isolated profile paths and actual read/write access probes. Do not conflate skipped Docker inventory or functional file access with Docker or full ACL-policy acceptance. |
| `scripts/test-windows-installer-source-selection.ps1` | Main explicitly includes versioned schema migrations and excludes unversioned SQL/customer exports. Existing sensitive-artifact exclusions remain. |
| `scripts/test-windows-native-runtime.ps1` | Main updates declared/native/cross-compile toolchain references consistently; no-pull and source-hash/native-proof boundaries remain. |
| `scripts/test-windows-process-containment.ps1` | Main updates the already-cached builder image reference; containment and no-pull requirements remain. |
| `scripts/test-windows-runtime-lifecycle.ps1` | Keep local-only A2A fixtures, retirement of public settings, preservation of chosen network ranges and existing token/environment restoration checks. Do not restore arbitrary advertised endpoints. |
| `scripts/test_python_runner_ci.py` | Main compares effective install directives, versions and hashes while excluding resolver-provenance comments. Do not turn harmless comment differences into lock mismatches. |
| `scripts/test_python_runner_container_smoke.py` | Main retains cleanup/offline policies and adds Grype and redacted failure-path coverage. The old version has no unique missing assertion to restore. |
| `scripts/windows-installer-payload-selection.ps1` | Main permits only versioned schema SQL under the migration directories while retaining other sensitive-artifact exclusions. Restoring blanket SQL exclusion would omit required installer migrations. |
| `services/deepteam-runner/requirements.in` | Main pins the newer declared DeepTeam input and includes its SDK dependency. Do not downgrade this input independently of its lock. |
| `services/deepteam-runner/requirements.lock` | Full diff shows the matching DeepTeam pin/hash update, SDK addition and resolver-provenance changes. Keep the current coordinated lock; no live model/SDK operation was tested here. |
| `services/promptfoo-runner/package.json` | Keep the coordinated Node/npm/Promptfoo pins, explicit Linux native dependency and scoped override. This is the bounded synthetic local runner, not an arbitrary caller-selected provider platform. |
| `services/promptfoo-runner/package-lock.json` | Structured comparison covers 941 old and 704 current package paths: 60 added, 297 removed and 116 changed entries. The root follows the current manifest; the Dockerfile omits optional packages and explicitly installs its native dependency. These metadata changes are not a count of lost features or proof that every transitive provider is equivalent. Keep the old copy while broader dependency/runtime acceptance remains separate. |

## Recovered coverage and actual checks

The added private-key regression exercises generic, RSA and OpenSSH headers with
both real and literal-escaped newline separators, complete and incomplete keys,
public prefix/suffix preservation, idempotence and unchanged input objects.
No production sanitizer behavior is changed. The current service already passes
the recovered cases; no new production defect is claimed.

All 84 tests across the four reviewed Node suites passed with concurrency one
and a 512 MiB heap limit. The focused sanitizer file passed all four tests.
Both versions of the 13 selected PowerShell scripts parsed without syntax errors;
none was executed. Local checks use the existing installed dependencies, not a
fresh lockfile install. No installer, Docker stack, database or external model
was started. Exact-head hosted CI remains the merge gate.

## Retention and remaining scope

The 64 selected files now have explicit review decisions, but this does not mean
the whole checkout or archive is integrated. Some expectations are intentionally
superseded; dependency metadata review is narrower than runtime acceptance.
None of the originals is asserted byte-identical to main. Keep all originals,
operational logs, database dumps, session state and archives. No deletion is
authorized or performed by this report.
