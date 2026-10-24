# Original Go inventory triage

## Bounded, read-only coverage

The next reconciliation pass structurally compared the 176 `.go` paths that
already had raw-content hashes in the original changed/untracked inventory.
Every original still matched its frozen hash during this pass. Both original
and current reads rejected symbolic links and checked size/modification-time
stability. Current working contents were checked against committed Git blobs
after CRLF normalization and strict UTF-8 decoding.

- 173 files produced declaration comparisons.
- Three files require manual handling because parsing/declaration identity did
  not yield an unambiguous comparison.
- No selected path lacked a committed current target.
- The compared original files contain 5,619 declarations; 5,102 are token-equal
  in the current files. Token comparison retains comments, literals and statement
  order. It is not semantic or runtime equivalence.
- Among the 173 compared current files, 166 current Git blobs match the inspected
  `main`. This does **not** mean 166 raw original copies match `main`.

The seven current files differing from that main reference concern the account
bridge, brain catalog, pursuit service, router smoke tests, Trello connector and
tests, and workflow service. Those are current branch changes, not proof of
lost original content or permission to discard the originals.

This scope excludes baseline-only files except those separately reviewed,
ignored content, missing original paths, files outside the earlier hashing
bound, non-Go files and transcript events. It overlaps the individual semantic
reviews; counts must not be added together as independent completed files.

## Leads requiring explicit disposition

Declaration keys found possible removals in six files. They are review leads,
not automatic evidence of missing features:

- `llm/policy_test.go`: the TLS/embedded-credential test is renamed and expanded
  to reject queries and fragments. Its complete text delta was read; live
  provider acceptance is not implied.
- `opscontrol/authorization_test.go`: a configured-owner-missing rejection test
  becomes a test binding recovery to the authenticated actor. Changed source,
  handler and admin route contexts have been inspected, but the change needs
  explicit safety-policy disposition rather than being called a mere rename.
- `router/routes.go`: the original `initializeRoutes` declaration is absent from
  that file; route coverage and replacement initialization need review.
- `source/handler.go`: the original `filterExtractionsForSources` helper is
  absent; its source/owner filtering responsibilities need comparison against
  current extraction reads.
- `verification/service.go`: original finalization, persistence, owner-run and
  freshness/evidence helper declarations are absent. Six removed keys must be
  reconciled against the replacement implementation before preservation can
  be affirmed.
- `background/worker_reliability_test.go`: the two failure-reader methods are
  renamed with their receiver to avoid colliding with a new configurable test
  reader. This rename is accounted for in the separate worker review.

Paths above are relative to `backend/internal`. The three manually unresolved
parser/declaration cases are `idp/internal/app/authentication/auth_service_test.go`,
`backend/internal/operations/ingest_atomicity_regression_test.go`, and
`idp/internal/app/authentication/auth_password_reset_integration_test.go`.
They were not silently treated as equal or completed.

## Next integration boundary

Prioritize the verification implementation and the owner/safety/route leads,
then resolve declaration ambiguities and remaining changed bodies. Preserve
useful behavior through the real current architecture, not blind restoration of
old file versions. Structural equality does not establish dependency behavior,
initialization ordering, side effects, external integrations, or operator
acceptance. No deletion eligibility or whole-HAI completion follows from this
triage, and no source or archive was removed.

## Subsequent bounded reviews

The counts and unresolved leads above describe the original triage checkpoint.
Later [verification reconciliation](verification-source-reconciliation-2026-10-23.md)
recovered the relevance guard and added finalization acknowledgement checks.
[Extraction and routing reconciliation](extraction-routing-reconciliation-2026-10-23.md)
traced the removed helper/wrapper to current owner-scoped paging and lifecycle
initialization. The [three test-file ambiguities](original-test-ambiguities-reconciliation-2026-10-23.md)
now have direct text-delta dispositions, without weakening the declaration
analyzer. These follow-ups do not turn the whole inventory into a semantic
review or authorize deletion.
