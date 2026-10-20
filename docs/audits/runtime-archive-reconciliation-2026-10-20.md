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

## Go source crosswalk follow-up

A read-only pass covered backend, IDP, and gateway-manager Go files across
all 11 registered HAI worktrees. It read 16,529 physical file instances and
deduplicated them into 4,239 path/content variants. The comparison tree was
main commit `f5438e210cce1f1378a06af0271d08086b7426d5`, whose 1,949 Go files
all parsed successfully. There were no filesystem coverage gaps within these
selected roots. The pass found 183 local variants that did not parse. A
hash-matched re-read confirmed merge-conflict markers in all 183; they remain
retained for further review.

Declaration-name candidates span 56 production paths: 290 production and 359
test declarations across variants were absent from the same package/test
scope on that main tree. These are review candidates rather than a count of
missing features. For example, current maintenance APIs use `WithContext`
names, DSpark uses private provider/configuration types,
and the old Autofever adapter conflicts with its intentionally disabled
upstream contract.

One reviewed gap is genuine: the retained source contains `EventOutboxProbe`,
while current main has durable outbox statistics but omits the delivery probe
from its readiness checks. The port restores a non-critical `events.outbox`
warning for dead-lettered events and pending deliveries older than two
minutes, while preserving current dependency and scheduler probes. It also
reports an unavailable pending age and propagates request cancellation.
The existing System Status checks render this probe under an Event delivery
group; no separate status endpoint or polling loop is introduced.

This pass covers declaration presence and selected source comparisons. It
does not establish equivalence of every differing function body, reconcile
all non-Go files, or approve deletion of any source checkout.
