# July 30 transcript archive reconciliation (2026-10-14)

## Scope and method

This read-only reconciliation scanned files named `*2026-07-30*.jsonl` under the local Codex temporary archive tree. It recorded first-record session metadata, file sizes, and JSON `event_msg/patch_apply_end` records. JSONL records were streamed with a 10 MiB per-record cap; oversized records were counted and skipped. No transcript was copied, modified, moved, or deleted.

## Current inventory

The local tree contains 22 matching rollout files totaling 31,573,466,825 bytes (31.573 GB / 29.406 GiB):

| Local relative directory | Files | Bytes | Status |
| --- | ---: | ---: | --- |
| `hai-completed-agent-sessions` | 13 | 17,559,729,527 | The 13 retained files from the published 18-row manifest are present. |
| `completed-agent-sessions` | 3 | 4,651,926,405 | Additional files outside that manifest. |
| `session-log-relief` | 6 | 9,361,810,893 | Additional files outside that manifest. |

All 22 files' first `session_meta` records identify the pinned HAI parent thread `019e7acc-44f2-7c90-a04e-253f6d43df28`. The nine files in the latter two directories were not included in the published 18-row manifest and need their own provenance/disposition review.

## Manifest paths absent from the local temp tree

The published 18-row manifest lists five completed cleanup candidates totaling 3,179,439,595 bytes. Their exact filenames and child IDs were searched across the local temp archive tree; none was found:

- `019fb20c-2b95-7550-8304-1117de872148`
- `019fb220-c797-77d1-8060-0f220cd733e1`
- `019fb287-b2e4-7251-8f1b-bc8fa9febc07`
- `019fb287-d1ed-75f0-99a8-8cd09c1ef9f2`
- `019fb287-f3be-7860-ba1f-e6b05088db73`

This establishes absence only within the searched temp tree, not whether the files were deleted, moved elsewhere, or preserved in another backup. The published manifest's prior `deletion_performed=false` statement is historical and does not establish current local presence. Do not infer a safe deletion or a completed cleanup from these missing paths.

## Bounded patch-event scan

Across the 22 files, the scan counted 766,103 JSONL records and found:

- 66,469 parsed `patch_apply_end` events: 66,424 successful and 45 unsuccessful.
- Zero malformed JSON among the candidate event records.
- Seven oversized records above 10 MiB, skipped by design.
- 788 unique path keys among the parsed events; 674 contain the historical HAI checkout prefix and 114 do not.

The event scan is incomplete because seven records were skipped. The matching parent-thread metadata and repeated histories mean the event and path counts are not counts of unique child changes. They do not prove that a patch was accepted in the final tree, identify which transcript uniquely authored it, or establish integration into current `main`. The existing path-audit script's prior missing-path conclusion must not be generalized to these extra files or to the skipped records.

## Tracked local source delta review

A bounded comparison of 223 modified tracked text files against current `main` found nine with a greater local line count. Line count alone is not evidence that a local version is newer or better. Semantic spot review found no whole-file replacement safe to port:

| Local path | Review |
| --- | --- |
| `frontend/package-lock.json` and `frontend/e2e/package-lock.json` | Dependency graphs differ from canonical locks. Do not transplant lockfiles without matching manifests and a clean dependency-resolution verification. |
| `frontend/src/app/services/context-memory.service.interface.ts` | Local code duplicates query contracts; `main` aliases the shared `IMemoryQueryParams` and `IMemoryPageResult` models. No local consumer references the duplicate declarations. Keep the canonical shared types. |
| `frontend/src/app/services/workflow/workflow.service.spec.ts` | Local tests expect the older list-based selection lookup; the canonical service contract tests direct selection lookup. Do not restore stale API expectations. |
| `frontend/src/app/app.module.ts` | Local inline icon registration removes the canonical `ControlRoomModule` import/export arrangement. Do not replace the module wiring. |
| `backend/internal/verification/repository.go` | Local repository contract omits canonical owner-scoped run lookup and has a different transaction abstraction. Review transaction behavior separately; do not copy the file over the current ownership boundary. |
| `frontend/src/app/pages/pursuits/pursuits.component.ts` | Local code auto-selects the first pursuit after refresh. This changes the explicit-selection/inspector behavior and lacks a focused acceptance test; deferred, not silently ported. |
| `frontend/src/app/control-room/module-registry.ts` | Local registry lacks the current `operational-brain` module and `intake-outcome-criteria` disclosure metadata. Keep the newer canonical registry. |
| `frontend/src/app/pages/control-center/control-center.component.ts` | Local version calls older ambient/pursuit APIs and removes validation of non-negative, finite count fields. Keep the current API and validation path. |

This is a line-count triage plus the listed semantic checks, not a proof that every local changed file has been fully reviewed. No local source variant was copied in that earlier bounded pass; the later regression addition is documented below.

## Broader checkout reconciliation (2026-10-14)

The source checkout at `C:\Users\NO\Documents\Codex\2026-05-30\github-plugin-github-openai-curated-noodzakelijk` is on `codex/hai-runtime-release` at `e07b9da`. Its current worktree reports 244 modified tracked files, 448 tracked deletions, and 1,582 untracked files. The untracked set is 1,501 files under `output/` (126,491,111 bytes) plus 81 elsewhere (2,198,860 bytes). These are not all product source: the latter include regression tests, historical test-evidence logs, environment state, and browser logs.

The checkout's committed `e07b9da` is an ancestor of the remote `codex/hai-runtime-release` branch (`e62b568`), which is PR #36's merged branch; the local branch has zero commits not reachable from that remote branch. This proves the committed history is remotely reachable, not that the current uncommitted worktree has been published.

Against current `main` `12246bd`:

- 241 of the 244 modified tracked files were comparable by path; none matched the `main` blob after Git path filters.
- The 448 files absent from the worktree all have paths on `main`; eight historical `HEAD` blobs are exact matches to `main`, while 440 differ. Those 448 files are already absent from disk, so deleting them cannot reclaim additional space.
- Of the 81 non-`output/` untracked paths, 65 are already tracked at the same paths in PR #117, but none has identical content. The remaining 16 are four local agent-state files, ten PostgreSQL/test-evidence files, and two browser/Compose logs. None was copied to the PR.
- The local and PR versions of the 63 comparable text files contained 272 local-only line occurrences versus 720 PR-only occurrences. This line-set comparison is triage, not semantic proof; targeted review found the PR versions generally preserve stronger ownership, sanitization, timeout, CI, and error-handling contracts.
- Three local environment files (`.env-backend`, `.env-gateway`, `.env-idp`) are absent from the PR. Their contents were not opened and must not be published without a separate secret review.

Targeted review found one useful regression not directly asserted in the PR: the durable execution-constitution gate must reject the non-persisted built-in fallback while accepting a valid active ID and rejecting a draft. Added `TestDurableConstitutionAvailabilityRejectsBuiltinFallback` alongside the existing broader constitution test in `backend/internal/phase2/module_test.go`. `git diff --check` passed; local `go` is unavailable, so this test is not locally verified and exact-head CI remains required.

The generated local checkout also contains approximately 369 MiB in `frontend/dist`, 73 MiB in `frontend/node_modules`, 6 MiB in `.angular`, 0.8 MiB in `.npm-cache`, 35 MiB in `installer/release`, and 0.3 MiB in `scripts/__pycache__`. No cache, release artifact, worktree, environment file, output log, or transcript was removed. The 13 additional worktrees in the shared Git repository have not yet received a per-file content reconciliation; several are dirty and remain out of cleanup scope until that review is complete.

## Disposition

No files are cleanup candidates on this evidence. Keep all 13 present retained transcripts, all nine out-of-manifest files, and any other unresolved HAI evidence. The five absent manifest entries need external backup/path reconciliation; they cannot be reconstructed from the manifest's report hashes alone. A future cleanup review requires a complete, reproducible per-file scan, child-specific attribution, source-to-current-main crosswalk, and verified preservation of every needed report and patch.

## Reconciliation checkpoint after PR #41/#42 (2026-10-14)

The canonical repository remains `Robert-Velhorst/018-HAI`; current `main` is
`12246bd28820f892a48e04b528ff8e28b102b7d4`. PR #40 is merged (merge commit
`59245962`); PR #41 is merged at `71102fd4`; PR #42 is merged at `b1caa0b0`.
The current integration PR is [#117](https://github.com/Robert-Velhorst/018-HAI/pull/117),
based on that `main`. Its head now includes commit `5698b7e2`, which corrects
the transcript scanner's `full_file_scan` result and the two generated
summaries. At the time of this checkpoint, all 40 exact-head checks were
queued; merge and cleanup are not authorized by this report.

The scanner previously defined `full_file_scan` using only skipped metadata
records, even though candidate transcript records could also be skipped. The
supplemental summary consequently said `full_file_scan: true` while reporting
five oversized records skipped. The result now becomes false if any metadata
record or candidate transcript record is skipped/malformed, or any file changes
during scanning. Both generated summaries now truthfully report false. The
synthetic scanner contract passed, and the supplemental readiness contract
passed after the commit: nine source files and their manifest sizes matched,
two candidate hashes were verified (3,113,294,358 bytes total), seven files
remained retained, and the check performed no deletion.

### Local worktree inventory

The current shared Git repository has 14 registered HAI worktrees. Status
counts below include staged, unstaged, deleted, conflicted, and non-ignored
untracked entries; they are not counts of unique source files. These snapshots
are an inventory, not approval to discard or bulk-copy any worktree.

| Worktree | HEAD | Status entries | Review state |
| --- | --- | ---: | --- |
| `2026-05-30/.../github-plugin-github-openai-curated-noodzakelijk` | `e07b9dae` | 2,274 | 244 modified, 448 deleted, 1,582 untracked; preserve; source review incomplete |
| `.codex/worktrees/hai-model-maintenance-audit/...` | `e07b9dae` | 520 | 301 modified, 124 deleted, 95 untracked; candidate tests reviewed, other deltas incomplete |
| `.codex/worktrees/hai-openclaw-artifact-audit/...` | `e07b9dae` | 247 | 108 modified, 42 deleted, 97 untracked; candidate tests reviewed, other deltas incomplete |
| `.codex/worktrees/hai-runtime-safety-audit/...` | `e07b9dae` | 18 | 13 modified, one deleted, four untracked; do not port weakened URL/environment/symlink/host-runtime safeguards |
| `.codex/worktrees/pursuit-default-selection/...` | `b76881c1` | 0 | Clean ancestor of PR #117; no unique commits |
| `2026-10-11/hai-pr34-conflict-fix` | `f0dc3fda` | 528 | Conflicted; preserve, do not copy conflicted files |
| `2026-10-11/hai-pr36-integration` | `ed9b379c` | 521 | Conflicted; Autofever lacks a verified upstream feed contract |
| `2026-10-11/hai-pr36-local-delta` | `ed9b379c` | 59 | Unresolved `UU` conflicts; preserve |
| `2026-10-11/hai-pr36-resolution` | `6b971af7` | 264 | Alternate resolution; per-file review incomplete |
| `2026-10-11/hai-resolve-pr34` | `f0dc3fda` | 518 | Conflicted; preserve |
| `2026-10-11/hai-resolve-pr35` | `ed9b379c` | 216 | Uncommitted source delta; per-file semantic review incomplete |
| `2026-10-13/hai-main-sparse-review` | `9a5fe676` | 0 | Clean; associated with open PR #121 |
| `2026-10-14/hai-pr117-review` | `5698b7e2` | 0 | Clean; current PR #117 head |
| `D:/hai-source-reconciliation` | `3b15fe4a` | 310 | Staged delta; no untracked files; read-only review found no unique high-value port |

The D: review found that its only path absent from both `main` and PR #117,
`nginx-config/nginx.conf`, duplicates the current template with a placeholder
key and missing routes/protections; it is not to be ported. Its alternate
approval, OAuth, task-state and scheduler variants remove controls present in
the reference trees. The unresolved PR #36 worktree also contains alternate
migration filenames already represented in the current migration chain; do
not revive older migration numbers.

The six local test candidates identified during the model-maintenance and
OpenClaw-artifact review were checked against current behavior. Process-start
revalidation and artifact-forget authorization already have stronger canonical
tests; durable-job cancellation already has a canonical test for the current
single-claim behavior; the canonical OpenClaw policy intentionally preserves
previous archive files; and the local SMTP test assumes a STARTTLS-only
implementation while canonical SMTP also supports implicit TLS. The local
background-operation 503 test would incorrectly claim that work was unchanged
when a timeout/503 can leave execution outcome uncertain; canonical tests
instead require reconciliation before retry. None of these six tests was
ported.

This checkpoint advances the earlier worktree audit but does not complete it:
hundreds of same-path alternate source files remain to be adjudicated, the
three environment files remain unopened and unpublished, and seven oversized
July records remain retained and unparsed. No local source checkout, worktree,
environment file, log, or transcript was deleted. No local file is certified
for cleanup by this checkpoint.

### October 14, 2026: duplicate frontend engine declaration candidate

The clean `2026-10-13/hai-main-sparse-review` worktree contains one candidate
change: remove the first of two `engines` keys from `frontend/package.json`.
Current `main` still contains both keys; the later key sets Node to
`^24.15.0`, which JSON parsing uses in practice. The exact change is already
published in open PR #121 (`codex/deduplicate-frontend-engines`, head
`9a5fe676ebba14fede67b5567e32a2adf478206b`), so it was not duplicated into
PR #117. GitHub currently reports PR #121 mergeable, but its base is the older
`345cf356c3f4ab497568713dc994820b4ba22b10`; its current checks against that
base do not prove integration with current `main` (`12246bd28820f892a48e04b528ff8e28b102b7d4`).
Treat the source as published for review, not yet preserved on current `main`.
The sparse-review worktree remains intact; no cleanup is authorized by this
finding.

### October 14, 2026: focused review of the dirty runtime-release checkout

The dirty checkout at `2026-05-30/.../github-plugin-github-openai-curated-noodzakelijk`
is `codex/hai-runtime-release` at `e07b9dae`, compared with current `main`
`12246bd28820f892a48e04b528ff8e28b102b7d4`. It has 2,274 status entries,
including about 1,500 generated `output/` entries; its committed history is
diverged from current `main` (the scoped reviews report 292 commits ahead and
36 behind). No bulk port or cleanup is justified by this checkout.

Three disjoint, read-only reviews found no complete product change ready to
port:

- Backend: 404 dirty backend entries (243 deletions, 125 modifications, 36
  untracked); targeted review found weakened provider URL validation, removed
  source-approval/execution-policy checks, and removed emergency-stop approval
  wiring. It was not a full review of all 404 entries.
- Frontend and IDP: 259 dirty tracked paths and 16 untracked paths; reviewed
  examples remove request contexts, authentication rate limits, session
  rotation/authority, shell templates, lazy route modules, and browser-state
  safeguards. Those are regressions or already-covered behavior, not port
  candidates. The slice was not line-by-line reviewed in full.
- CI, installer, Compose, gateway, scripts, services, and docs: targeted review
  found missing files still required by CI/installer/Compose, removed gateway
  security headers, an unsafe placeholder-key nginx config, a transcript audit
  that can classify a partial tail as complete, and several runtime/dependency
  downgrades without supporting evidence. These are blockers, not improvements
  to import. The 259-path slice was not line-by-line reviewed in full.

Generated-output inventory, read without dumping log contents: 1,507 physical
files occupy 126,491,186 bytes; 82 JSONL files occupy 120,845,323 bytes and 884
log files occupy 3,308,520 bytes. Of the 78 `output/` paths tracked by current
`main`, 70 are already absent from this checkout. The eight local copies that
exist are not byte-identical; after normalizing checkout/temp paths and
Markdown link syntax, each has zero semantic line differences from `main`.
They do not contain new report findings, and none qualifies for exact-match
local cleanup. The remaining untracked output and all dirty source files stay
preserved pending broader review. No environment file or raw log was opened.
The reviews did not edit files in the dirty runtime-release checkout, change
worktrees, or move Git refs. Their findings were recorded separately in PR
#117's reconciliation ledger.

### October 14, 2026: effective Docker build-context regression contract

Added `scripts/test_docker_build_context.py` to exercise each backend,
frontend, and IDP `.dockerignore` using Docker Buildx's local output exporter
and synthetic files only. It asserts that service source, manifests, SQL
migrations, and docs stay present; credential-like files, test fixtures,
coverage/build outputs, and dependency caches stay out; and the frontend's
public CA certificate exception remains included. The contract is wired into
the existing hosted Compose-validation job and guarded by a CI workflow
contract assertion. No local Docker build was run in this audit. The Python CI
contract suite passed locally (111 tests), and `git diff --check` passed.

This verifies source-level wiring locally, not the Buildx execution itself;
that check still requires the hosted CI job. It is a preventive regression
test, not a finding that a current service build context leaked credentials.
No source checkout, worktree, environment file, log, or transcript was deleted.

### October 14, 2026: D: source-reconciliation snapshot review

A read-only comparison of the D: worktree at `3b15fe4` found 310 changed
tracked paths and no untracked files. That commit is an ancestor of current
`main`; the path-difference counts against `main` and PR #117 are not counts of
unique functionality. The targeted review found no high-value change absent
from the canonical source that should be ported.

Do not port the worktree's added `nginx-config/nginx.conf`: it duplicates the
canonical template with a placeholder backend-key value and lacks current
routes/protections. Reviewed changes to approval and OAuth state handling,
task-history bounds, scheduler failure behavior, task-state JSON bounds, and
tests/docs remove or weaken behavior present in the canonical tree. This was a
targeted security-sensitive review, not a line-by-line audit of all 310 paths;
no tests were run. The staged state and the D: worktree remain unchanged and
preserved. No local cleanup is certified by this review.

### October 14, 2026: clean pursuit worktree removal

The managed worktree `C:\Users\NO\.codex\worktrees\pursuit-default-selection\github-plugin-github-openai-curated-noodzakelijk`
was rechecked immediately before removal. It was at `b76881c1`, had zero
tracked, untracked, or ignored status entries, and occupied 38,096,373 bytes.
The PR #117 checkout confirmed that commit is an ancestor of pushed head
`0742ddc61c5c4718ef05642c545264d65720b960`; the checkout therefore contained no
unique worktree changes. The non-force `git worktree remove` command succeeded.
Afterward the path was absent and `git worktree list` no longer contained it.
This removes one reproducible checkout; it does not claim that all remaining
local HAI data is reconciled or that the same number of bytes was returned to
the volume's free-space counter.

### October 14, 2026: archive source recheck and scratch runtime audit

The transcript cleanup-readiness verifier was rerun against
`D:\codex-temp\hai-completed-agent-sessions` with source-archive verification
required. The source directory exists but is empty. All five manifest-listed
cleanup candidates were checked individually and are absent at their expected
paths, including the 1.58 GB Copernicus and 1.60 GB Aristotle files. Targeted
filename checks for Copernicus in the expected transcript root, the general
completed-session root, retained/archive-audit roots, quarantined roots, the
integration snapshot, and bounded Codex evidence/temp backup roots found no
copy. The exhaustive home-session search was stopped after reporting system
resource exhaustion; therefore this is not proof that no copies exist anywhere
on the device. No deletion was performed in this pass. Missing source files do
not pass the manifest byte-count/SHA-256 gate and cannot be certified as
integrated-and-safe-to-remove.

Read-only MemoryLayer records from the parent Codex session expose bounded
reports for the five candidates (`message_id`s `21642`, `21644`, `21646`,
`21658`, `21659`, and `21660`); these messages are secondary evidence, not the
original transcripts or their bytes. Their reported findings were checked
against current `main`:

- Copernicus: workflow selection provenance normalization, `Reason` decoding
  with legacy `RuleApplied` fallback, conflicting immutable-event rejection,
  failure-result propagation, and owner-role pursuit approval tests are
  present. Focused tests cover the persistence round-trip, conflicting
  idempotent writes, legacy decoding, and normalization.
- Aristotle: Constitution base-version provenance, stale-activation rejection,
  first-owner draft selection, deferred active-Constitution invariant, and
  deterministic catalog digest are represented in the framework registry,
  migrations, and tests. Prior live PostgreSQL assertions were environment
  blocked.
- Banach: approved-task recovery is fail-closed and never retries uncertain
  external work; criterion matching rejects conflicting values and negation;
  workflow success criteria are carried through the runner. Current tests
  cover review reconciliation, amount/date/version conflicts, negation, and
  runner delegation.
- Euler: immutable preference-change history, owner-scoped Constitution
  history, and frontend `baseVersion` contracts are present in services,
  routes, migrations, and frontend interfaces.
- Pascal: history-load failures render as unavailable rather than empty, and
  workflow framework audit detail includes conflicts, context requirements,
  and learning plan fields.

Focused Go tests passed locally with Go 1.25.13 for `internal/frameworkregistry`,
`internal/task`, `internal/workflow`, and `internal/workflowtask`. The frontend
`framework-registry-history-state` contract test also passed (1/1). The indexed
Copernicus report records a prior backend test/vet pass for 70 packages; that
historical result was not rerun here. These cross-checks establish selected
findings in current source, not full transcript integration or archive
integrity. No candidate can be removed under the exact-hash cleanup gate while
its source file is absent.

The uncommitted `hai-runtime-safety-audit` worktree was inspected without
modifying it. Its agent-runtime and host-runtime changes do not compile under
the available Go 1.25.13 toolchain: both focused packages fail because the
changed host-runtime code references missing safety APIs
`AcquireExecutionCommitFenceContext`, `EvaluateEmergencyStopForExecution`, and
`CurrentEmergencyStopRevision`. The diff also deletes
`final_effect_authorization.go`. No changes from this worktree were ported.

The uncommitted `hai-openclaw-artifact-audit` worktree has 247 status entries,
including 42 tracked deletions, and `hai-model-maintenance-audit` has 520,
including 124 tracked deletions. Their broad mixed diffs were not reviewed
line-by-line or tested in this pass. They remain untouched and are not
certified for porting or cleanup.

The pushed branch ref, GitHub PR API, and `refs/pull/117/head` were resynchronized
at `b5b09be01aeb0bb472a358eea7abd76c1efa9146`. Exact-head workflow run
`38587326456` is still active: 4 of 36 jobs completed, none failed, none was in
progress, and 32 were queued at the last status check. No merge or additional
local cleanup was performed.

### October 14, 2026: primary branch and cleanup-gate refresh

The primary checkout remains at `e07b9dae` on `codex/hai-runtime-release`.
Current status is 2,274 entries: 244 modified tracked files, 448 tracked
deletions, and 1,582 expanded untracked files. Its local branch is 292 commits
ahead of `origin/main` at `12246bd` and 36 behind it. The local commit is an
ancestor of remote `codex/hai-runtime-release` at `e62b568`, so the local branch
has no commits absent from that remote ref; this does not publish or preserve
the uncommitted worktree contents by itself.

PR #36 is merged. Its recorded merge commit is `c31cec1`, and current `main` is
30 commits ahead of that merge. A three-dot comparison of the older local
`e07b9dae` tree with current `main` lists 503 paths: 111 have the same blob in
both trees, 389 differ at the same path, and three exist only in the old
branch tree. One branch-only migration path, `post/0004_operations_owner_workspace_dedupe_active`,
has equivalent index behavior in the canonical `post/0006` migration on `main`.
This is a tree-level triage, not semantic review of all 503 paths; it does not
justify transplanting or deleting the remaining branch/worktree content.

A fresh exact-byte check examined 2,272 modified or untracked file paths in the
primary checkout. Of those, 314 paths also exist on current `main`; none had
identical bytes to its same-path `main` blob. No file met the direct-match
cleanup gate. The separate `D:\hai-source-reconciliation` checkout remains
staged/dirty with 310 entries and was inspected read-only using a one-command
`safe.directory` override; it was not altered.

The `hai-model-maintenance-audit` scratch worktree still has 520 status entries.
Its LLM maintenance diff against current `main` removes about 2,187 lines across
eight paths, including cooldown and lease/admission tests; it is not safe to
port as a whole. No changes were copied from that worktree. Other dirty scratch
worktrees remain preserved as listed above.

PR #117 remains open at `f5c37f5c060ff359731132489e38034d7d082627`. Exact-head
Actions run `38588676552` is queued with 4 of 36 jobs completed successfully,
32 queued, and no failures at this check. The local PR checkout is clean. No
additional local files were removed in this refresh.

### Local pursuit completion test cross-check

GitHub reports PR #42 merged at `b1caa0b074793e794ea8bc340cba9ac562924ff7`.
The corresponding local untracked file,
`backend/internal/pursuit/completion_outcome_integrity_test.go`, is not an
exact copy of the current-main file (local Git blob `2bde5557`; current-main
blob `e3d606b4`). In the shared `TestPursuitCompletedReceiptUsesLaunchSpecificCodes`
case matrix, the local copy still lacks the HTTP 302, 500, and 599 rejection
cases added by PR #42. The canonical main version is therefore stronger; the
local file contributes no additional justification to port as written and is
not eligible for deletion under the exact-main-match rule. This is a focused
test-file comparison, not a broader semantic audit of the pursuit package.

The local untracked
`backend/internal/pursuit/completion_outcome_postgres_test.go` is also not an
exact copy of current `main` (local Git blob `2d01cdbe`; main blob
`973b316b`). Its `TestPostgresPursuitExactRuntimeQueryBatchesAllLinkedReceipts`
passes `len(ids)` (51) to `FindLinkedAutomationLaunches` while asserting that
the result is capped at 20. The canonical repository implementation honors
positive limits through 500; only zero, negative, or over-500 limits become
the default 20. Therefore the local test does not exercise the display cap it
claims to cover and can return all 51 rows. Current `main` passes zero, which
exercises the default cap. The other two local test bodies matched the
canonical versions in this bounded function-level comparison. Do not port or
delete the differing local file; it is neither a correct improvement nor an
exact-main match. The local test was not run because this environment lacks
the required Go toolchain/database acceptance setup.

### Live remote checkpoint (2026-10-14)

`git ls-remote origin refs/heads/main` returned `c7657c9e2ab198a3a2a1db076619344d07ef7d60`.
The primary checkout's `origin/main` still points at `b7e3ad2` because its
`FETCH_HEAD` could not be written, so that local ref is stale and is not used
for cleanup decisions. PR #117 remains open at
`3446479107cd81074ea5e1b655e84090f0f38c96`; exact-head CI run `38634361913`
was queued when checked. No local candidate has been removed in this pass.

The local untracked
`backend/internal/router/opscontrol_emergency_stop_permissions_test.go` also
differs from current `main` (local blob `39370be9`; main blob
`0458af7e`). In `TestGlobalEmergencyStopUsesVerifiedAdminRoleNotWorkerOwnerScope`,
the local request omits the JSON `reason` body and `Content-Type` header that
the canonical test supplies. The remaining test function matched in the
bounded comparison. The local variant therefore is not a stronger replacement
and is not an exact-main cleanup candidate; no source was copied or removed.

The separately reviewed
`backend/internal/runtimelab/owner_gate_handler_test.go` differs only by the
security-clarifying comment `Even an administrative role does not grant
another operator's scope.` The comment is now included in PR #117. The updated
PR-worktree blob is `7333d2da`, matching the local candidate's Git blob; current
`main` remains at `2324a9c0` until merge. The local copy remains in place and
will not be removed before a post-merge main-blob equality check.

### Additional task-test cross-checks

The local untracked
`backend/internal/task/operation_transition_context_test.go` differs from
current `main` (local blob `b47eab63`; main blob
`3a86b47d`). In
`TestTaskOperationTransitionCancellationAndEmptyReplay`, the local test treats
a cancellation before any executor call as `ErrTaskOperationNeedsReview` and
expects the claimed operation to be surfaced without its canceled execution
context. Current `main` instead requires the cancellation to be durably
recorded as `TaskOperationCanceled` and explicitly rejects an uncertain-outcome
review for pre-execution cancellation. The local variant encodes the weaker
and contradictory outcome; do not port or delete it.

The local untracked
`backend/internal/task/required_participant_admission_test.go` differs from
current `main` (local blob `8bb4b593`; main blob
`37078ded`). In
`TestReadyRequiredParticipantsRemainEligible`, the canonical fixture includes a
verified, healthy, provenance-bearing `evidence_reviewer` AgentCard with a
verification timestamp; the local fixture omits that participant evidence.
The local test therefore does not establish admission under the current
required-participant contract. Do not port or delete the non-identical local
file. These were bounded test-function comparisons; neither local Go test was
run.

The local untracked
`backend/internal/workflow/project_dossier_test.go` differs from current `main`
(local blob `867365ef`; main blob `62a22d40`). In
`TestProjectDossierBoundsNestedContextAndRedactsCredentials`, the local test
embeds the credential-shaped literal `AKIAABCDEFGHIJKLMNOP`; current `main`
constructs its AWS-shaped fixture dynamically with `"AKIA" + strings.Repeat(...)`
and checks redaction against that value. The canonical test preserves the
security assertion without adding a scanner-triggering literal. The local
variant is not a better port and is not an exact-main cleanup candidate.

The local untracked
`backend/internal/workflow/reminder_context_boundaries_test.go` differs from
current `main` (local blob `325d8282`; main blob `e46ae2c8`). In
`TestReminderContextStopsBetweenStorageStepsAndRetainsConfirmedResults`, the
local test requires a non-nil zero-count result when `count == 0`; the canonical
test requires a nil result for that empty case and checks the result fields
only for positive counts. This is a stale contract mismatch, not a missing
enhancement to port. The local file is not an exact-main match and remains
preserved.

The local untracked
`backend/internal/verification/freshness_test.go` differs from current `main`
(local blob `3f9f5f41`; main blob `0796a733`). In
`TestStaleEvidenceCannotMarkClaimSourceSupported`, the local assertion calls
`runStatus(claims)` while `main` supplies the grounded answer mode and request
context: `runStatus(claims, ModeGrounded, AnswerRequest{})`. The local test
therefore uses an obsolete contract and is not a stronger case to port. It is
preserved because its content does not exactly match `main`.

### 2026-10-14 exact-content inventory

The current local primary checkout is `codex/hai-runtime-release` at
`e07b9daeb3ba2630ecbe12a64d948f791203c241`; it has 2,274 status entries and
is 440 commits behind its remote branch. GitHub's live `main` ref was
`c7657c9e2ab198a3a2a1db076619344d07ef7d60`. A content-hash comparison of all
3,126 existing, non-ignored local tracked/untracked paths against the complete
3,182-blob recursive `main` tree found 780 exact path-and-blob matches. The
remaining local paths are not exact matches and require individual integration
review; identical files are source files inside the active checkout, not
redundant scratch copies. No files were deleted. This count establishes
eligibility for later cleanup only if an independently verified disposable
copy is identified; it does not justify removing source files from this
working tree or deleting the checkout.

The same snapshot compared each status path to the live `main` tree by path:
all 448 locally deleted tracked paths still exist on `main`; 243 of 244
modified paths exist there; 73 of 1,582 untracked paths collide with a path on
`main`; and 1,509 untracked paths are absent from that tree. These are path
existence counts, not content-equivalence or quality judgments. In particular,
the 448 deletions are not candidates for porting as deletions. The 73
untracked path collisions require blob comparison before any integration.

The checkout's untracked `output/` contains 1,507 files (126,491,186 bytes;
about 0.118 GiB): 884 `.log`, 420 `.json`, 82 `.jsonl`, 60 `.exit`, 37 `.md`,
and smaller script, archive, dump, and binary groups. This is generated output,
not a GitHub source snapshot; its contents have not been fully classified or
integrated, so it remains untouched. The local `.claude-flow/` and `.swarm/`
state (four files, 235,446 bytes combined) is likewise retained pending
provenance and usefulness review. No local files were deleted in this pass.

### Additional operations-test comparisons

Three more untracked test files collide with paths on `main`, but are not
equivalent or stronger ports:

- `backend/internal/operations/repository_ordering_test.go` (local blob
  `e109b7f4`; main `a68026ca`): both define three tests, but `main` has 13
  additional distinct lines covering deterministic SQL ordering. The lone
  local-only line makes a fake transaction return `driver.ErrSkip`; do not
  replace the canonical test with it.
- `backend/internal/operations/source_approval_test.go` (local blob
  `ab5102ff`; main `b99253b8`): `main` has 13 tests versus 12 locally, including
  explicit approval despite a future-review reminder. Its existing cases also
  assert status/version binding diagnostics and reject same-version mutation;
  local variants omit those checks. Do not port the weaker snapshot.
- `backend/internal/operations/ingest_atomicity_regression_test.go` (local blob
  `b0836d80`; main `0b535964`): both have 14 test names, but the local expected
  records omit `SourceEvidenceRawSHA256` updates/assertions present in `main`.
  Keep the canonical digest-persistence assertions; do not port the snapshot.

These were source comparisons only; no local Go test was run. They add no
unique behavior warranting a code change or cleanup eligibility.

### IDP and migration test snapshot review

The ten untracked `idp/internal/app/authentication/*_test.go` candidates were
compared with their same-path `main` versions. All ten have the same named test
functions as `main`, and the sampled differing service calls in the local
versions omit the `context.Context` parameter now required by the auth API.
The canonical tests use `context.Background()` and must remain authoritative;
the local files are stale snapshots, not additional test cases. Local/main
blob pairs:

| File | Local blob | Main blob |
| --- | --- | --- |
| `auth_password_reset_integration_test.go` | `3835926e` | `c54c07bd` |
| `durable_session_authority_test.go` | `9fd400b7` | `eb7b5848` |
| `durable_session_postgres_redis_test.go` | `eefb3d43` | `3562765f` |
| `http_session_redis_test.go` | `7d9c8cd7` | `771d9ce4` |
| `login_interleaving_integration_test.go` | `424cc1d6` | `fbb9ab21` |
| `password_reset_log_privacy_test.go` | `683b181c` | `ecb8d763` |
| `refresh_rotation_redis_test.go` | `4030ea74` | `97f04702` |
| `refresh_rotation_test.go` | `d9201dab` | `671bb5dc` |
| `session_authority_test.go` | `42f668f8` | `ab009ecb` |
| `session_store_loss_redis_test.go` | `37fd37a1` | `43f0464f` |

Seven further untracked migration/phase-2 test snapshots are likewise not
additive:

- `backend/migrations/protected_data_rollback_postgres_integration_test.go`
  has the same three test names; `main` adds a rollback assertion (local blob
  `16fd03ad`, main `b692001f`).
- `backend/migrations/runtime_db_role_provisioning_contract_test.go` has the
  same single contract test but differs in one line each way (local
  `28bb8bce`, main `c1ec097e`); no distinct local test case was found.
- `source_configuration_postgres_test.go`, `source_head_postgres_test.go`,
  and `source_observation_postgres_test.go` locally stop at migration
  `pre/0114_operation_source_configuration`; `main` advances the fixtures to
  `pre/0118_operation_source_evidence_raw_digest` and includes the later
  rollback-boundary checks (local blobs `469e5c7b`, `e981a869`, `af517e63`;
  main `2f458563`, `df4ec9e9`, `2119768a`).
- `trello_webhook_reconciliation_generations_integration_test.go` locally
  applies the full pre-phase migration set rather than isolating migration
  `pre/0109_trello_webhook_reconciliation_generations` as `main` does (local
  `9b1267ff`, main `69b608d9`).
- `backend/internal/phase2/execution_claim_postgres_integration_test.go` has
  the same test name, but its 55-line difference is not an additive second
  scenario; `main` includes explicit owner and source-derived-operation
  assertions (local `8cd6120f`, main `623b3131`).

These are content and named-test comparisons, not executions. The local
versions were not run; no local candidate in this group should replace or be
ported over its canonical `main` test.

### Frontend test and acceptance harness cross-check

Six more local frontend candidates were compared to their live `main` paths:

- `frontend/scripts/control-room-logic.test.mjs` (local `cbebf688`, main
  `ec5fd9a8`): main adds the `refreshed` selection scenario. The local-only
  `id_changed` mode is covered by main's `selection` mode, which changes both
  the selected record and its ID; the production confirmation handler also
  rechecks the ID. Do not copy the older test file.
- `frontend/scripts/deep-link-focus.test.mjs` (local `1acbbc1f`, main
  `325c1ad0`): no local-only source lines; main routes module URL loading
  through its shared test helper. No missing behavior identified.
- `frontend/scripts/framework-secret-boundary.test.mjs` (local `1c9c5378`,
  main `9da9aa5a`): main constructs the synthetic private-key marker from
  fragments so secret scanners do not mistake the fixture for a credential.
  Keep the canonical fixture.
- `frontend/scripts/release-cache.test.mjs` (local `291822e2`, main
  `a1833f2e`): main additionally checks disabled critical-style inlining and
  same-origin theme bootstrap behavior under the restrictive script policy.
- `frontend/e2e/tests/support/owned-browser-matrix.ts` (local `d5196ef4`, main
  `a7faa6f6`): main uses a validated isolated storage-state path and requires
  `E2E_ALLOW_MUTATION=true` before synthetic setup. The local version instead
  accepts email/password fields; do not port it over the safer canonical
  harness.
- `owned-browser-matrix-handoff.md` (local `a5942980`, main `5b8cf20b`): main
  includes an October 11 session-refresh follow-up; the local handoff snapshot
  is older. It is not a source-code change to port.

These candidates did not yield an unimplemented frontend code change. The
temporary clean worktree used to inspect current `main` was removed after
verification; no primary checkout or source file was changed or deleted.

### Build, acceptance, and Windows-runtime script snapshot review

Eighteen untracked HAI scripts were compared against their same-path `main`
versions. The local files are not exact duplicates and were not executed from
the legacy checkout. Their local-only/main-only differing-line counts are
inventory data, not a quality score:

| Path | Local-only lines | Main-only lines |
| --- | ---: | ---: |
| `audit-npm-production-dependencies.sh` | 1 | 9 |
| `isolated-acceptance-stack.ps1` | 3 | 47 |
| `isolated-recovery-rehearsal.ps1` | 1 | 1 |
| `python_runner_container_smoke.py` | 0 | 10 |
| `test-frontend-build-resource-limit.cjs` | 9 | 13 |
| `test-hai-phase2-state-permissions-contract.ps1` | 7 | 7 |
| `test-isolated-acceptance-stack.ps1` | 1 | 56 |
| `test-native-compile-deadline.ps1` | 5 | 5 |
| `test-native-go-discovery.ps1` | 1 | 1 |
| `test-openclaw-maintenance-migration.ps1` | 2 | 3 |
| `test-windows-installer-install.ps1` | 26 | 69 |
| `test-windows-installer-source-selection.ps1` | 1 | 9 |
| `test-windows-native-runtime.ps1` | 8 | 8 |
| `test-windows-process-containment.ps1` | 2 | 2 |
| `test-windows-runtime-lifecycle.ps1` | 5 | 8 |
| `test_python_runner_ci.py` | 3 | 13 |
| `test_python_runner_container_smoke.py` | 0 | 10 |
| `windows-installer-payload-selection.ps1` | 1 | 3 |

Spot review of the high-risk differences confirmed that `main` has the newer
or stricter behavior: the npm audit script has an optional-dependency mode;
acceptance startup retries gateway readiness; the phase-2 permission contract
uses the utility-image migration fixture; OpenClaw maintenance compares the
existing file DACL; installer tests support an explicit hosted-runner Docker
availability mode; and the installer source-selection and lifecycle checks
have additional assertions. The local line-ending-sensitive installer checks
also lack the canonical CRLF-tolerant expressions. No local script behavior
was selected to port in this batch.

Two dependency-manifest candidates were also compared to `main`:

- `services/deepteam-runner/requirements.in` and `.lock` locally omit
  `sentry-sdk`; the current main input and lock include it. Do not replace the
  canonical dependency set with the local snapshot.
- `services/promptfoo-runner/package.json` and `package-lock.json` locally pin
  Promptfoo `0.123.1` and omit `@libsql/linux-x64-gnu`; current `main` pins
  `0.124.1` and declares `@libsql/linux-x64-gnu` `0.5.29`. Preserve the main
  manifest/lock pair.

No script, dependency install, or Windows runtime was executed in this source
comparison. The local candidates remain unmodified and are not cleanup-safe
until the remaining untracked evidence and primary checkout changes have been
reviewed.

### Remaining backend worker and source-test comparisons

Five remaining untracked backend test snapshots were compared with their
same-path `main` versions:

- `backend/internal/ambient/scheduler_test.go` (local
  `6a26fd4f`; main `acc30517`): main adds
  `TestStartSchedulerReportsPolicyDisabledState`; local lacks this explicit
  policy-disabled behavior.
- `backend/internal/automation/public_projection_test.go` (local
  `4a93dd5e`; main `dea54351`): the ten test names match, while main injects
  an accepting event publisher in the service fixture instead of relying on
  the zero-value publisher. No extra local case was identified.
- `backend/internal/background/worker_reliability_test.go` (local
  `c757d928`; main `9b6f5b10`): both have six test names, but the local
  reliability case expects fresh ingested work to be verified immediately.
  Main instead asserts it is routed to `awaiting_approval` and not executed
  without approval. Preserve the safer canonical contract.
- `backend/internal/opscontrol/emergency_stop_fanout_test.go` (local
  `d90a6bbd`; main `353b07a7`): main sends a JSON reason and sets
  `Content-Type: application/json`; the local request omits the body and
  content type, so it does not exercise the actual request contract.
- `backend/internal/source/trello_reopen_test.go` (local
  `6bc283d6`; main `1902f7cb`): main validates the all-actions filter and
  decodes an action fixture before testing reopening; local returns an empty
  array and omits that source-response validation.

These local test snapshots are not candidates to port over current `main`.
The comparisons were not test executions, and all five files remain
unmodified pending the wider audit.
