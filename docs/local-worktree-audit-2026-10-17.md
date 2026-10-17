# HAI worktree reconciliation audit - 2026-10-17

## Scope and reference

This check compared the registered HAI Git worktrees that were readable from this Windows session against the GitHub `main` tree at `80fdeabd16efda6d9328fa0c89a4ab813f087908`. It compared tracked tree entries and existing non-ignored changes; this is not a whole-device inventory.

Ten registered worktree roots were readable. None had a tracked/non-ignored file tree exactly matching current `main`. The primary checkout is substantially dirty (244 modified tracked paths, 448 deleted tracked paths, and 1,581 non-ignored untracked files). The other readable roots also contain material differences, not just redundant checkouts.

The registered `D:\hai-source-reconciliation` worktree could not be inspected with Git because Git reported dubious ownership for that filesystem. No trust exception or ownership change was made; its files remain untouched and its content is not assessed here.

## Integration status

PRs #41 and #42 are merged. Their changed files remain present in `main`, but the local copies checked in the registered worktrees did not match the current `main` blob hashes, so those copies are not direct cleanup candidates.

The three parallel audit worktrees (`hai-model-maintenance-audit`, `hai-openclaw-artifact-audit`, and `hai-runtime-safety-audit`) have overlapping local deltas: 206 changed paths overlap between model-maintenance and OpenClaw artifact audit, 17 between model-maintenance and runtime-safety audit, 15 between OpenClaw artifact audit and runtime-safety audit, with 15 paths changed in all three. These deltas need file-by-file reconciliation before they can safely become one or more PRs.

Four PR-resolution worktrees have unresolved index conflicts: PR34 conflict-fix has 82 `UU`, 13 `AA`, and 1 `UD` entries; PR36 integration has 227 `UU`, 27 `AA`, and 2 `UD`; PR36 local-delta has 59 `UU`; PR35 resolution has 199 `UU`. These checkouts are not publishable as-is. The PR34 resolution worktree also has staged-plus-unstaged edits and requires separate review.

A focused Go test attempt for the runtime-safety audit could not run because `go` is not available in this shell's PATH. No local test pass is claimed for that delta.

## Cleanup decision

No worktree was removed and no source file was deleted. The available evidence does not establish any registered worktree as a complete, clean duplicate of current `main`; several contain unresolved merges, and the three parallel audit outputs overlap. Preserve these worktrees until their unique deltas are reconciled and any retained work has been pushed and verified.

This report does not assess ignored build caches, containers, databases, backups, Codex session history, source archives, or other HAI-related files elsewhere on the device.