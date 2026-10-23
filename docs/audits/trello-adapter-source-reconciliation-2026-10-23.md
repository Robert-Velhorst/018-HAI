# Trello adapter source reconciliation

## Preserved implementation

Read the complete retained Trello snapshot adapter and its complete difference
against the current adapter. Preserve read-only GET requests, owner/member and
read-only permission checks, bounded pagination and retries, inclusive cursor
boundary handling, stable card upserts, tombstones, comments, checklists and
attachment metadata without downloading attachment content.

Retain the newer canonical board/card binding, real short-link provenance,
member/start/due-completion metadata, canceled-context checks, remaining-byte
read limits and aggregate list/record accounting. Keep the restricted credential
destination, public-address transport checks, OAuth header authentication,
redirect refusal and distinct permission versus credential failures. Do not
restore the older weaker allowlist-only credential destination or unverified
board identity.

Read the complete retained durable cursor repository unit tests and their
current difference. Preserve their ordering, cursor/evidence, phase/completion,
source revocation and source-enabled checks. Retain the newer owner-bound manual
resume selection and ambiguous takeover rejection. This does not constitute
a full new review of every repository, connector service or large test variant.

## Fixed observation boundaries

- The quiet-board activity probe decoded JSON directly into a slice. JSON `null`
  therefore became a successful empty observation and skipped full scanning.
  Use the existing array-response helper: missing/non-array observations fail
  without a quiet result or advanced cursor. A genuine empty array still means
  no activity; unknown, saturated or malformed action entries still fall back
  to bounded full reconciliation as before.
- The durable page counter check accepted integer wraparound from the maximum
  page count to a negative next count. Require strict forward progress before
  adding one in the equality check. Zero and the maximum valid increment remain
  accepted. Record and page evidence checks remain intact.

The old no-activity fixture now emits a real empty array rather than a nil slice
serialized as `null`. Separate HTTP tests explicitly reject `null` and an object,
and retain an empty-array positive control. No weaker assertion is substituted
for the previously missing observation boundary.

## Verification and limits

The page-counter regression failed before repair. The HTTP null-response
regression also failed before repair; the object and empty-array controls passed.
The complete source Go package passes uncached after both fixes. Tests use local
HTTP fixtures and synthetic credentials, not a live Trello account. No production
database, source configuration or token was changed. Publication, exact-main
preservation and local source deletion remain separate gates. Retained worktrees
and transcript archives are not deleted by this reconciliation.

Integration-tagged source tests also compile and the selected Trello cursor,
quiet-probe, source-state and resume unit cases pass with that build tag. No
PostgreSQL case was selected or executed by that targeted command; database
acceptance remains a separate CI/environment gate.

## Additional retained model-maintenance variant

The complete 56,724-byte retained model-maintenance adapter was read, including
its credential, snapshot, pagination, rendering, retry and cursor helpers. Its
complete normalized diff against the published adapter contains 241 added and
33 removed lines. No older connector implementation was copied over the newer
adapter. Keep the newer identity normalization, member/start/due-completion
metadata, public short links, request-budget sentinel, restricted credential
destination and transport integration. The retained standalone full-scan logic
is already represented in the current bounded reconciliation path.

The newer quiet-board probe is an intentional behavioral difference, not proof
that every sync is a complete snapshot. The stale source comments claiming a
full scan on every sync and unconditional cursor losslessness were corrected.
Quiet observations skip stored-card hash comparison, tombstones and propagation
of board metadata into stored card content. A board metadata-only action can
advance the cursor without rebuilding card content. Periodic full-snapshot
freshness and completeness under concurrent changes remain unproven; the
existing no-activity tests do not establish either property. Do not remove the
optimization or claim periodic reconciliation without a separately reviewed
design and persistence contract.

After the comment correction, the Trello-named local Go run reports 163 passed
top-level tests and 178 passed subtests. Nine PostgreSQL-dependent tests skip;
their database behavior is not verified by this run. This command does not
enable the `live` build tag or exercise a real account. Other retained
service/test variants remain separate reviews. This additional source review
does not make its original file, worktree or transcript history deletable.
