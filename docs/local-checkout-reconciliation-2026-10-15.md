# HAI local checkout reconciliation checkpoint (2026-10-15)

## Scope and source of truth

This is a bounded checkpoint for one HAI repository checkout, not a complete audit of every HAI-related file on the device. The canonical repository is `Robert-Velhorst/018-HAI`; the verified `main` commit was `86fb1d9c53a69e1edb2fb098832aa6cb35d38eee`.

The primary local checkout under review was on `codex/hai-runtime-release` at `e07b9dae`. Its Git refs were refreshed before comparison. No local source, logs, transcripts, databases, or worktrees were changed by this audit.

## Why the local branch must not be merged wholesale

The local branch and current `main` have divergent history: `git rev-list --left-right --count HEAD...origin/main` returned 292 local-only commits and 42 main-only commits. This is commit ancestry, not a count of missing features.

The committed tree comparison (`origin/main` to local `HEAD`) reported 2,092 changed paths, 20,789 insertions, and 401,467 deletions. It included 1,198 deleted paths, 888 modified paths, four added paths, and two renames. This stale tree is not a safe PR candidate: applying it as a merge or snapshot would remove substantial content from current `main`. Any local work worth preserving must be reviewed and ported as narrowly scoped changes based on current `main`. A direct comparison of the primary working tree against current `origin/main` also confirmed that it is not a safe code PR: it removes the entire 531-line `backend/internal/operations/source_approval.go`, removes OpenClaw upload-cancellation checks and the Trello quiet-board cursor safeguards, and drops the router's 1 MiB `MaxHeaderBytes` limit. The separate reconciliation index exhibits the same classes of regression. These are observed candidate diffs, not proposed changes.

## Working-tree inventory

At the checkpoint, the primary checkout had 2,274 non-ignored status entries:
- 448 tracked deletions
- 244 tracked modifications
- 1,582 untracked files

The 1,582 physical untracked files totaled 128,691,399 bytes. The largest group is dated test/diagnostic output under `output/`; this raw output was not copied to GitHub. It has not yet been classified record-by-record for unique findings, data sensitivity, or retention requirements. A read-only inventory found 1,507 files under that directory (126,491,186 bytes), including 82 Go test JSONL event logs (120,845,323 bytes). Aggregated parse results: 16 logs contain package-level failures, 15 contain test-level failures, and one log contains one malformed/partial JSON record. Across all logs, the latest recorded outcome for 8,545 distinct test identifiers and 166 package identifiers had zero failures (300 test identifiers and ten packages ended in skip). This is a cross-run aggregation, not one complete green run or current-main acceptance. These are historical runs from September 30-October 1, not current-main validation; no raw log content was published. One exact-duplicate group consists of two empty files. Failure-to-fix attribution remains open, so the logs are retained. A separate line-by-line comparison of the local `output/production-durability-integration-20261001.md` against its 539,810-byte current-main version found 48 differing lines: 24 expanded user-profile temp-path references versus 24 `%TEMP%` placeholders, with no other line differences. The 540,410-byte local variant adds no new report content and should not be published because it reintroduces machine-specific paths.

A content-hash comparison against the refreshed `origin/main` tree found 1,091 physical files in this checkout with exact main blob matches, totaling 6,788,083 bytes. Nearly all are tracked checkout files required by this stale working tree and must not be removed individually. Among untracked files, exactly one was an exact main match: `backend/internal/runtimelab/owner_gate_handler_test.go`, 12,495 bytes. A deletion attempt was rejected before execution; the file remains in place. No deletion is recorded as completed.

The untracked Go test variants examined here use paths already present on `main`, and their named test functions are also present in the corresponding main files. That is not proof their assertions are equivalent. Their bodies still need a semantic comparison before any port or cleanup. The Go toolchain was not available in the PowerShell environment used for this checkpoint, so no local Go test result is claimed. A separate inventory found 81 non-output untracked paths: 64 have same-named current-main files with different bytes, 16 paths do not exist on `main`, and one is already an exact match. The 16 include agent-state files, diagnostic logs, and test-evidence fixtures; they were not published as a batch. The ten-file PostgreSQL/race evidence set is 476,719 bytes and contains container/volume metadata, which was not uploaded. Its summary records an initial fixture-race package failure followed by 15 passing executions across five test cases; all five test names exist on current `main`, but the recorded source SHA-256 matches no current file in that package, so this is historical supporting evidence only. Its cleanup proof reports zero remaining owned resources and no volume deletion, but no live Docker state was checked in this audit.

## Other registered worktrees

### Review-report file comparison

A read-only inventory found 37 local Markdown review reports under `output/`, totaling 966,731 bytes. Comparing each relative path and Git blob with current main found 29 paths absent from main and eight same-path files with different bytes; none is byte-identical. Three of the eight become text-identical after normalizing only the expanded local temp-root prefix to `%TEMP%\` (the installer-production, integrated-safety, and production-durability reports). Five still differ after that normalization. This is a file-level comparison, not a finding-by-finding integration audit: absent or differing reports remain retained, and no report is marked safe to delete on this evidence alone.

A fresh `git worktree list` inspection found eleven registered paths in addition to the primary checkout. Three dirty HAI audit worktrees based at `e07b9dae` contain 425, 150, and 13 tracked paths changed, plus 95, 97, and four untracked files respectively. A separate clean managed worktree for PR #147 is also registered. The dirty audit worktree deltas have not yet been semantically compared or matched to PRs.

Five historical PR-resolution worktrees under the October 11 Codex directory also contain distinct working-tree changes (527, 519, 59, 264, and 216 paths respectively, with two untracked files in one). Their contents must be compared with current `main` and the corresponding PR histories before archiving. They were not removed.

The secondary source-reconciliation worktree is based on `3b15fe4a`, 33 commits behind current `origin/main`, and has 310 staged paths (7,138 insertions and 12,453 deletions) plus 34 unstaged deletions. Those 34 deletions are tracked reports under `output/` (885,533 bytes total); each file's committed blob exactly matches the corresponding file on current `origin/main`. They are already absent from this worktree, while the remote copies remain intact. A bounded blob comparison of its staged index against current `main` found 308 changed existing files, one exact match, and one new path. Representative review found security-sensitive regressions in the candidate: it removes upload cancellation checks, omits source-evidence hash material from a source-approval digest, and removes the HTTP max-header limit. Therefore this staged batch is not safe to publish wholesale. The remaining files need semantic review and selective porting; no part of this worktree was deleted.

Physical checkout sizes are not equivalent to reclaimable disk space: Git object storage may be shared, dependencies and generated outputs may be present, and uncommitted changes remain in several worktrees. No storage recovery is claimed.

## Related published work

PRs #41 and #42 are already merged. Their merge commits are `71102fd40a960b59b70e56f902cb4b3dc91f1390` and `b1caa0b074793e794ea8bc340cba9ac562924ff7`. They required no action in this checkpoint.

PR #147 is open and contains the published redaction change. Its managed worktree was clean at head `2cfcffaf901a93577c41367127f4181287f3f5ab`, occupied 38,217,382 bytes, and contained 21,454 bytes of ignored files. A recoverable archive request was queued, but the worktree still existed at the last verification. No archive completion or local space recovery is claimed.

PR #151 is this draft checkpoint. Other open HAI PRs were observed, including #143-#150 and earlier PRs. Their scope and current checks must be reviewed before treating any matching local work as integrated.

## Remaining reconciliation work

1. Review every differing tracked source file against current `main`, grouping genuine behavior changes separately from stale regressions and line-ending churn.
2. Compare the differing local Go test bodies with their current-main equivalents; port only assertions that add meaningful coverage and pass current-main tests.
3. Classify untracked scripts, reports, fixtures, and output artifacts by source, sensitivity, uniqueness, and whether their findings are already represented in `main`.
4. Inspect the remaining registered worktrees and any other HAI-related roots separately; this checkpoint does not hash every file in every worktree and is not a whole-device search.
5. Publish each validated missing code/documentation change as a focused PR against current `main`.
6. Remove a local copy only after its exact content or complete information value is preserved remotely, its cleanup scope is explicitly attributable, and post-removal verification succeeds.

This checkpoint records evidence and remaining work; it does not claim the overall HAI reconciliation or cleanup is complete.

## Historical report cross-check against current main (2026-10-15)

A bounded source check compared several retained October 1 review reports with the refreshed current-main tree. This records source presence and control-flow evidence only; no local Go/PowerShell tests, browser run, live Trello call, deployment, or database acceptance was performed in this pass.

- The production-configuration report's three findings have corresponding controls on current main: browser acceptance uses the guarded isolated-stack Prepare/Start/Stop flow (the reviewed workflow no longer contains the reported ordinary Compose `down -v` cleanup); the acceptance guard rejects service `env_file`/`secrets`/`configs` and top-level `secrets`/`configs`/`include`/`extends`; and the doctor rejects production secrets shorter than 32 bytes. Relevant current-main paths: `.github/workflows/ci.yml`, `scripts/isolated-acceptance-stack.ps1`, and `backend/internal/doctor/doctor.go`.
- The three Trello P1 regressions and the reviewed P2 checkpoint cases are represented in current main by `backend/internal/source/trello_production_sync_test.go`; matching tests cover long-cycle watermarks, verified short-link refresh, callback-first comment archival, inventory checkpointing, and canonical checkpoints. Current source also retains the cycle-start watermark, board-target canonicalization, and callback action identity logic. Presence of these tests/source does not substitute for executing them or testing against a real Trello account.
- The embedded-runtime review's blanket whole-loop exemption is not found in current `backend/internal/task/service.go`; required-agent evaluation remains present. This bounded check does not certify every specialist-coverage or durable-approval edge case from the historical report.
- The source-revision handoff's stale-evidence concern has corresponding current-main binding: `backend/internal/operations/source_approval.go` includes `SourceRevisionHash` in approval material, and `backend/internal/background/execute.go` checks the stored/candidate revision match. This is source inspection, not a concurrency or live-execution acceptance test.

- The workflow-preflight reports' approval/readiness and fallback-outcome cases are represented in current main: `workflowtask/runner.go` uses a narrow outstanding-approval predicate and preserves readiness blockers; `approval_preflight_test.go` covers approval-only versus missing parameters/participants; `fallback_review_evidence_test.go` covers second-reservation denial after a completed receipt and empty-model fallback without automatic retry; and `tool_outcome_uncertainty_test.go` covers uncertain execution classification. These are code/test-presence checks only; none of those Go tests was run in this pass.
- The frontend safe-outcome report's main state and test seams are present on current main: bounded run-receipt projection, per-operation and runtime uncertainty latches, completion predicates, and Basic-view review prioritization appear in the listed background-operations/Runtime Lab models, components, and specs. The report's source-only boundary is retained; this pass did not execute Angular tests or a browser.
- Structured-redaction regression coverage from the retained redaction report is also present in `backend/internal/safety/redaction.go` and `redaction_test.go`, including duplicate JSON values, quoted HTTP-error URLs, and encoded/repeated secret query names. The retained report records failed frameworkregistry and pursuit compatibility suites at its October 1 snapshot; those historical failures were not re-run against current main and must not be represented as either current failures or a clean current acceptance result.

## Runtime-outcome report cross-check against current main (2026-10-15)

A source-only comparison of `output/runtime-outcome-independent-review-20261001.md` against current main found the historical P1 paths represented by current guards and regression tests:

- Both safe-failure markers now walk the full unwrap chain and reject joined errors before accepting a marker. Current regression tests include `TestToolFailureBeforeDispatchDoesNotHideJoinedUnknownFailure` and `TestSafeNoSideEffectMarkerRejectsAllJoinedErrors`.
- Owner approval of an ordinary task review loads the persisted prior plan and returns `ErrTaskOutcomeReconciliationRequired` if that evidence is missing or uncertain. Workflow-owned operations are refused on the task-review retry path and directed to workflow reconciliation. Current source includes `TestWorkflowOwnedOperationCannotRetryThroughTaskReview` and `TestApprovedReviewReconciliationCannotCompleteUncertainExecution`.
- Uncertainty dominates optimistic completion in life-graph projections: the record is waiting/needs-review, uncertainty is included in the provenance digest, and unverified output is labelled. The assistant review/next-action projection also treats uncertain execution as requiring reconciliation. Current source includes `TestLifeProjectionCannotPromoteUncertainExecution` and `TestAssistantRequiresReviewForUncertainExecution`.
- If launch outcome persistence is unconfirmed after runtime execution, current `automation_service.go` returns an indeterminate result retaining redacted output and runtime route trace while explicitly withholding completion. This is source inspection; a focused current-main test for that exact persistence-failure receipt was not established in this pass.
- This pass did not execute Go tests, so the listed tests are present, not newly passing. The report's historical test failures remain historical and are not evidence of current-main failure or acceptance.

These reviewed historical findings do not presently justify porting the corresponding old candidate patches as new PRs: current main already contains the identified controls and regression source. Other retained reports, differing test bodies, and worktree changes remain unreviewed, so the complete reconciliation is still open.


## Transcript oversized-record count cross-check (2026-10-15)

The current-main transcript summaries account for seven skipped records across the 22-file inventory: the main manifest reports two and the supplemental manifest reports five. PR #149's documented byte-stream scan also reports seven records over 10 MiB. The counts agree numerically, but the summary scanner's configured bound is in characters while PR #149 describes a byte threshold; this is cross-artifact consistency, not proof that the scanners classified identical records.

Both published summary files correctly report `full_file_scan: false` because oversized records were skipped. PR #149 reports that its scan found no literal `patch_apply_end` marker in the seven oversized records, but that does not establish lifecycle attribution or that those records contain no other useful work. A separate local full-volume rescan was attempted and stopped without producing results, so it adds no independent confirmation. Keep all transcript files retained until the oversized records' content and attribution are fully reconciled; no cleanup is justified by this count comparison.


## Runtime-safety worktree review (2026-10-15)

A fresh status and content comparison of `C:\\Users\\NO\\.codex\\worktrees\\hai-runtime-safety-audit\\github-plugin-github-openai-curated-noodzakelijk` against current `main` (`86fb1d9c53a69e1edb2fb098832aa6cb35d38eee`) found 18 changed paths: 13 modified tracked files, one tracked deletion, and four untracked files. All 18 paths exist on current `main`; none of the local file contents is an exact main-blob match. The worktree remains unchanged and retained.

Targeted diffs show the local snapshot removes or weakens controls already present on current main: DeepSeek cancellation fails open with a background context, drops cancellation-receipt owner/runtime/task/job identity checks, removes environment-secret filtering and output truncation/redaction, and replaces no-link/reparse-point validation with `EvalSymlinks`; ecosystem authorization drops the rollback action binding; host-runtime changes remove durable start-intent/acknowledgment/unknown-outcome routes and related lease/approval-digest checks. The local OpenClaw artifact URL helper also lacks current main's NAT64-aware address policy and its regression cases.

Test-name comparison of four local test files found no local-only test names in three files. In the fourth, the local-only `TestOpenClawAuthorizedUploadReplacesAndDeletesManagedArchive` explicitly expects the previous managed archive to be deleted. Current main instead preserves previous archives and has additional tests for upload cancellation, upload-size limits, validated DNS pinning, and NAT64 address policy. These findings reject the candidate variants as written; they do not establish that every assertion across the remaining large test diffs has been semantically reviewed. No code was ported, no Go tests were run, and no local files were deleted.
