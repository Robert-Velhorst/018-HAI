# Constitution history archive integration

Reviewed main: `403b7a6866c98f7a278fb351646a48e5dfcf68fe`.
This continues transcript reconciliation after PR #319. Neither this scoped
repair nor its tests establish that the whole transcript corpus is integrated.

## Two small source archives rechecked

Both retained files were reread in full and matched their frozen index hashes.
The validation archive has 58 JSON records, seven shell calls, one successful
two-file patch and an aborted terminal state. The Constitution history archive
has 94 JSON records, 24 shell calls, two planning-tool calls, one successful
eight-file patch and an aborted terminal state. No historical test command was
run before either interruption. Historical commands were inspected as data,
not executed by this review; no raw transcript is published.

The first archive's protected-value/provenance matching and later mixed-polarity
repair are already represented on main. Some original literal hunks now differ
because the stronger polarity implementation and additional tests supersede
them. This is not an instruction to restore the earlier matcher.

The second archive's bounded owner-scoped history repository, history DTO,
immutable version fingerprint, read route and permission tests are present.
Its complete 141-line history test file is byte-identical after ordinary line
ending normalization. The pointer-copy helper moved to the repository file;
the handler now rejects invalid/repeated limits instead of silently parsing
them. Current frontend model, transport and Advanced version-ledger rendering
exist; this does not establish real browser or database acceptance here.

## Additional client contract gap and repair

The transport validated each history record but not the collection contract.
It accepted more rows than the declared limit, mismatched response limits,
empty/short truncated pages, duplicate record IDs, and reversed ordering.
An invalid ledger could therefore be presented as successfully loaded.

The scoped repair checks the returned limit against the normalized request,
enforces page bounds and the truncated-page invariant before mapping entries,
rejects duplicate IDs, and verifies version-descending, creation-descending,
ID-ascending order. It rejects invalid data instead of silently sorting or
discarding records. Existing authentication, ownership, read endpoint, request
limit fallback and individual-record validation remain unchanged.

Go's RFC3339 timestamps retain up to nine fractional digits. Ordering uses
standard Date parsing for the instant plus the remaining fractional precision,
so valid nanosecond ordering is not mistaken for an ID tie. Different timezone
representations of the same instant still use the ID tie-breaker.

## Executed verification

- Before repair, nine of sixteen HTTP-double cases failed the intended contract.
- After repair, all sixteen cases pass using real installed RxJS. Valid empty,
  full/truncated, version ordering, timestamp ties, nanoseconds, timezone ties
  and default-limit behavior remain accepted.
- A negative control removes only the page-size comparison; the oversized-page
  regression fails again while the other fifteen cases pass.
- Sixteen permanent Angular service regressions cover the same boundary cases.
- A strict no-emit TypeScript check of the service and complete service spec
  reports zero diagnostics. Its one imported product model matches main.

These are native transport-double and compiler checks using existing installed
dependencies, not Angular TestBed execution, a full application build, a browser,
real HTTP, PostgreSQL or external-provider acceptance. Third-party declaration
checking is skipped. No new environment, agent, dependency or Docker stack was
installed or started. Exact-head CI remains a publication/merge gate.

## Retention boundary

Both source archives remain. Their remaining full-record/source-read baseline
and canonical cleanup decisions must be closed explicitly; a patch hunk match
alone is not whole-archive clearance. The large archives, malformed compacted
records, unrelated local copies, databases and shared Git metadata are untouched.
No original source or transcript is deletion-eligible solely from this report.
