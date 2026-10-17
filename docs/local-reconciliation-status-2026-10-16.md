# HAI Local Reconciliation Status — 2026-10-16

This dated addendum supersedes earlier inventory counts and PR-state notes where they differ. It is an evidence snapshot, not a claim that the complete local HAI corpus has been reviewed or is safe to remove.

## Canonical repository state

- `main` is at `df42ab2475b0814bacfc0f5989af85f855a64e14`.
- PR #41 merged as `71102fd40a960b59b70e56f902cb4b3dc91f1390`; PR #42 merged as `b1caa0b074793e794ea8bc340cba9ac562924ff7`. Both merge commits are ancestors of `main`.
- PR #156 remains open, with a four-file documentation-only diff. Its merge base with current `main` is `86fb1d9c53a69e1edb2fb098832aa6cb35d38eee`; it is 35 commits behind `main`. The previous PR head `e608467ed4f4bce4bd04505773bea7ae61d18dc2` passed all reported checks. The latest documentation commits require their own CI result; verify that result and refresh the branch onto current `main` before considering a merge.

## Local checkout inventory

The primary HAI checkout currently reports 692 tracked changes (448 deletions and 244 modifications) plus 1,582 untracked paths. 1,501 untracked paths are under `output/`. Among untracked files, 73 paths also exist on `main`: one has an identical Git blob and 72 differ. The exact-match file is `backend/internal/runtimelab/owner_gate_handler_test.go` (12,495 bytes). It remains local; deletion was blocked by the environment. The different-content files are not eligible for removal under an exact-match rule. No other local files or worktrees were deleted as part of this snapshot.

## Recovered transcript finding

The raw child-session source for a 152,530-byte workflow-service snapshot could not be recovered from accessible local locations. Its archived review summary identified provenance decoding, immutable-event conflict detection, normalization, and result-propagation concerns. Current `main` contains the corresponding decoder compatibility, normalized provenance, conflict checks, and failure-result propagation, with regression coverage including `TestFrameworkSelectionProvenanceRejectsConflictingIdempotentWrites` and `TestFrameworkSelectionDecisionReadsLegacyRuleAppliedPayload`. This confirms those summarized findings are represented in current code; it does not prove that the missing transcript or the wider transcript archive has been fully processed.

## Remaining status

No additional source files were copied from the local candidates in this pass. The remaining tracked changes, untracked files, linked worktrees, and transcript/archive contents still require item-level review before integration or deletion.
## Follow-up audit — 2026-10-16

The live GitHub ref was checked again: main remains df42ab2475b0814bacfc0f5989af85f855a64e14. PR #36 (runtime/release work) is merged; PRs #40, #41, and #42 are also already merged. These states do not by themselves prove that every file currently in this older checkout has the same content as the merged trees.

The local checkout is on codex/hai-runtime-release at e07b9daeb3ba2630ecbe12a64d948f791203c241. Its cached remote-tracking branch is e62b56843eb8bbbf9a566aff5e883f9adf375429 (440 commits ahead of this checkout); local HEAD is also 77 commits behind and 292 commits ahead of cached origin/main (left/right count from git rev-list --left-right --count origin/main...HEAD). A fetch was attempted but failed because Git could not open .git/FETCH_HEAD (permission denied). The cached origin/main hash does match the live GitHub main hash, so comparisons below use that verified tree; local branch refs were not refreshed.

Current local inventory still reports 692 tracked changes (448 deletions, 244 modifications) and 1,582 untracked paths. The hidden output/ tree contains 1,507 files totaling 126,491,186 bytes (about 120.6 MiB). These are test/recovery/run evidence and remain unreviewed as a complete set; they were not deleted. Other untracked paths include candidate tests, scripts, agent state, and logs.

A bounded same-path review covered the untracked source/test/document candidates. backend/internal/runtimelab/owner_gate_handler_test.go remains the only confirmed exact same-path blob match found in the previously inventoried set. The other reviewed copies are not exact matches. Current main versions generally contain additional cases or stronger assertions; examples include pursuit completion/receipt coverage, runtime acceptance retries, protected Docker network-range preservation in scripts/test-windows-runtime-lifecycle.ps1, and broader test fixture/error-path coverage. The local Promptfoo package snapshot is also older (0.123.1 versus 0.124.1 on main) and omits dependency/security configuration present on main. No source/test changes were ported in this bounded pass because no reviewed local delta was demonstrably an improvement over the canonical version.

This is not a complete file-by-file reconciliation. The 692 tracked changes, remaining untracked source candidates, all 1,507 output artifacts, external archive/transcript roots, and linked worktrees still need item-level classification. No local files were removed.
