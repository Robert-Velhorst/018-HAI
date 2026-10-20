# Runtime archive reconciliation - 2026-10-20

This is a bounded review against main commit
`e6269d3093ecceaef18b1cd0ebd2643f0ccd75ae`. It does not establish that all
local worktrees or transcript archives have been reconciled.

## Archive selection behavior

A retained runtime-safety worktree has 18 changed or untracked paths under
`backend/internal/agentruntime` and `backend/internal/hostruntime`. A
same-path comparison found no identical source files among the 17 physically
present candidates; the remaining entry deletes
`agentruntime/final_effect_authorization.go`, which main retains.

Comparing Go test function names identified two names absent from the
corresponding main files. Their assertions were read directly:

- `TestOpenClawAuthorizedUploadReplacesAndDeletesManagedArchive` expects a
  previously selected archive to be removed after an authorized upload.
- `TestOpenClawSetEcosystemPathReleasesPreviousUploadArtifact` expects an old
  upload to be removed when the selected archive changes.

Neither expectation should be restored. Main deliberately preserves previous
archives; `TestOpenClawSetEcosystemPathPreservesPreviousArchives` exercises
that policy. Name differences alone are not missing work.

## Recovered coverage

The earlier transcript audit identified a positive-path assertion that was
not represented in the current test suite: an allowed symlink must be stored
as its resolved archive target, rather than retaining the caller's alias.
The production implementation already resolves the target in
`prepareEcosystemPath` before applying the selection.

`TestOpenClawSetEcosystemPathStoresCanonicalAllowedSymlinkTarget` adds this
regression coverage using synthetic archives. It complements the existing
outside-root and symlink-escape rejection tests. Unix fixture creation must
succeed; Windows reports an explicit skip if symlink privileges are absent.

The focused Windows test run passed archive preservation and outside-root
rejection. Both symlink cases reported explicit skips because this process
lacks Windows symlink privileges. The new positive-path assertion therefore
requires a successful Linux CI run before its behavior is considered verified.

## Retention

This review does not classify every differing function or assertion in the
18-path worktree. No source checkout or transcript is approved for removal
by this report. The original worktree remains the source for further review.
