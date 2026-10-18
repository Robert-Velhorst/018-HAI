# HAI worktree reconciliation audit - 2026-10-17

> Historical audit record. Counts and repository references describe the
> dated observations below, not current device or release status. Local
> paths and transcript identifiers use public aliases; private source
> evidence remains retained. This export does not authorize deletion.

## Scope and reference

This check compared all 11 registered HAI Git worktrees with the GitHub `main` tree at `80fdeabd16efda6d9328fa0c89a4ab813f087908`. It compared tracked tree entries and existing non-ignored changes. It is not a whole-device inventory and does not assess ignored build caches, containers, databases, backups, Codex session history, source archives, or files outside the registered worktrees.

None of the 11 worktrees is an exact match for current `main`.

The primary checkout is substantially dirty: 244 modified tracked paths, 448 deleted tracked paths, and 1,581 non-ignored untracked files. The other registered worktrees also contain material differences, not just redundant checkouts.

`<local-path-021>` initially rejected ordinary Git inspection because of filesystem ownership metadata. It was subsequently inspected read-only using a one-command `safe.directory` setting; no global trust or ownership settings were changed. That worktree is on `codex/hai-local-source-reconciliation` at `3b15fe4a`, 75 commits behind `origin/main`, with 309 staged modifications, 34 staged deletions, and one staged addition. Its index has 3,178 paths: 2,777 exact blob matches to `main`, 400 different blobs, one path absent from `main`, and 23 `main` paths missing locally. It is not a duplicate. `git diff --cached --check` returned no findings; this is not a test or correctness result.

The staged D: snapshot is not ready to publish as a bulk change: its version of `docs/child-agent-integration-ledger.md` removes 5,263 lines, and the staged state also deletes tests and audit documents. Review and extract only individually verified changes; do not use the staged tree as a replacement for `main`.

## Integration status

PRs #41 and #42 are merged. Their changed files remain present in `main`, but the local copies checked in the registered worktrees did not match the current `main` blob hashes, so those copies are not direct cleanup candidates.

The three parallel audit worktrees (`hai-model-maintenance-audit`, `hai-openclaw-artifact-audit`, and `hai-runtime-safety-audit`) have overlapping local deltas: 206 changed paths overlap between model-maintenance and OpenClaw artifact audit, 17 between model-maintenance and runtime-safety audit, 15 between OpenClaw artifact audit and runtime-safety audit, with 15 paths changed in all three. These deltas need file-by-file reconciliation before they can safely become one or more PRs.

Four PR-resolution worktrees have unresolved index conflicts: PR34 conflict-fix has 82 `UU`, 13 `AA`, and 1 `UD` entries; PR36 integration has 227 `UU`, 27 `AA`, and 2 `UD`; PR36 local-delta has 59 `UU`; PR35 resolution has 199 `UU`. These checkouts are not publishable as-is. The PR34 resolution worktree also has staged-plus-unstaged edits and requires separate review.

A focused Go test attempt for the runtime-safety audit could not run because `go` is not available in this shell's PATH. No local test pass is claimed for that delta.

## Cleanup decision

No worktree was removed and no source file was deleted. The available evidence does not establish any registered worktree as a complete, clean duplicate of current `main`; several contain unresolved merges, and the parallel audit outputs overlap. Preserve these worktrees until their unique deltas are reconciled and any retained work has been pushed and verified.
