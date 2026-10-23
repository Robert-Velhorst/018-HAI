# Operations and control reconciliation

## Reviewed scope

Reviewed eleven selected source versions: the model-audit control controller,
handler, service and service tests; the artifact-audit handler, service and
service tests; and both worktrees' memory and PostgreSQL operations repositories.
Corresponding variants in the two worktrees match after line-ending
normalization. Original worktrees remain unchanged.

## Retained implementation

The older controller lacks current execution/mutation serialization and keeps
cached autonomy permissions after persistence failure. Retain main's fail-closed
mode state and compare-and-swap transitions, including restrictive changes.

Retain authenticated pause attribution, malformed-request rejection, durable
owner review and exact confirmation before resume, exact-effect control
approvals, and explicit recovery errors. Do not restore the older resume path
or silently return an empty successful recovery response.

Retain lease-aware recovery: preserve live workers, recover only expired claims,
and surface executing records without leases for manual reconciliation. The
older tests bypass worker claims and omit the live/unleased regression.

Retain atomic creation/mutation audit pairs, version/owner/workspace/source
guards, source-authority checks and execution-claim fencing in both repository
implementations. The older unconditional updates are not ported. Keep deep
copies in the memory repository, deterministic pagination, due-date filtering,
and bounded audit-history pagination.

## Additional verification

The service recovery regression now covers both running and verifying work.
Each case checks that a live lease is reported without recovery, the executing
record remains unchanged, and an unleased record stays visible without being
automatically resumed or marked interrupted. Counts must identify the correct
phase rather than merely sum to a plausible total.

Both phase cases and the full opscontrol, operations and safety Go test
packages pass locally. These memory-backed tests exercise the service
and report mapping; they do not establish PostgreSQL locking, external runtime
acceptance or whole-system recovery readiness.

## Preservation boundary

This is a source reconciliation decision, not permission to delete original
files. Exact source fingerprints and remaining-file counts stay in the private
integration ledger. No worktree, archive, database or original source is deleted.
