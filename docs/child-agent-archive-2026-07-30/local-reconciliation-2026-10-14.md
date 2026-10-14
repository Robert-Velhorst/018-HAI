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
required. It failed closed because the manifest-listed Copernicus source file
`rollout-2026-07-30T10-02-49-019fb20c-2b95-7550-8304-1117de872148.jsonl` was
missing. Targeted filename checks in the expected transcript root, the general
completed-session root, the retained/archive-audit roots, the quarantined
roots, the integration snapshot, and bounded Codex evidence/temp backup roots
found no copy. The exhaustive home-session search was stopped after reporting
system resource exhaustion; therefore this is not proof that no copy exists
anywhere on the device. No transcript deletion was attempted. The five main
archive candidates remain ineligible for cleanup until the source manifest and
hash gates pass again.

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

The pushed branch ref `codex/integration-ledger-live-checkpoint` is at
`768b6b173c613c73481ebb9a54f808aa9871bc88`, while the GitHub PR API and
`refs/pull/117/head` still report `0742ddc61c5c4718ef05642c545264d65720b960`.
The latest listed workflow run is queued on `0742ddc6`; no exact-head CI run
for `768b6b17` is visible. The PR description was corrected to disclose this
state; no close/reopen, force-push, duplicate PR, or additional local cleanup
was performed.
