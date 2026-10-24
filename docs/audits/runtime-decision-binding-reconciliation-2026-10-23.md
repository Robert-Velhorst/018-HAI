# Runtime decision reference binding

## Scope and publication boundary

This follow-up was prepared against commit
`9214792cfede8abaee34e1a7b364ee3bf7e391b5` while that published PR head's CI
was still running. It does not replace the completed retained-source reviews,
advance their file counts, or establish preservation on `main`. Publication and
exact-head CI of this follow-up remain separate gates.

## Defect and change

The runtime recovery selector previously accepted either the decision reference
or the evidence reference independently. If neither matched, it still selected
the only runtime attempt. A declined review skipped recovery-workflow creation
and could record a resolution without validating either reference. Supplying a
different decision type with a runtime decision ID also bypassed that binding.

The service now requires both references to identify the same nonzero, linked,
owner-visible runtime attempt. A runtime decision ID requires the runtime review
type. New resolutions, whether approved or declined, must correspond to a
pending runtime review. These checks run before workflow creation and resolution
recording. Retries of an already resolved decision preserve existing idempotent
behavior, but cannot bypass reference validation with different evidence.

Validated runtime IDs, review type and evidence URI are canonicalized before
idempotency lookup or recording. Equivalent casing, surrounding whitespace and
the accepted trailing URI slash cannot create a different audit identity or
leave the canonical pending review visible. Two replay cases and a canonical
audit-reference test failed before this follow-up normalization and pass after
it; accepting equivalent input is not permission to use mismatched evidence.

This does not execute or retry a runtime. Successful approval continues to
create the existing separately approval-gated recovery workflow.

## Verification

- A generated Go overlay substituted the exact published `service.go` while
  retaining the new tests. With the final normalization case included, the old
  implementation failed 21 subtests and four top-level tests. This comparison
  did not modify the published source.
- Before the normalization follow-up, the complete `internal/pursuit` package
  passed 247 top-level tests and 100 subtests, with no skips. This is historical
  verification of the earlier local revision, not the final follow-up count.
  Tests used existing fake repositories and
  workflow intake, not a live database or runtime.
- After normalization, the complete pursuit package passed 248 top-level tests
  and 100 subtests with no skips. The separate full source and workflow package
  commands also passed; they do not establish live database/provider acceptance.
- Cases cover absent, invalid, contradictory, foreign and zero references;
  identity rather than list order; approve and decline; running/non-pending
  attempts; wrong decision type; no workflow/activity/link effects on rejection;
  owner-scoped valid resolution; idempotent replay; and conflicting evidence on
  replay. The fixture asserts owner-visible runtime evidence to avoid mistaking
  an unrelated ownership rejection for reference validation.
- Go formatting and Git whitespace validation were checked. No dependencies
  were installed or changed.

The checks do not establish transactional behavior under concurrent requests,
database persistence, HTTP authorization, live runtime execution, or rendered
UI acceptance. Candidate-acceptance activity atomicity remains a separate open
finding. No original source, transcript archive, or local worktree is eligible
for deletion on the strength of this follow-up.
