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
