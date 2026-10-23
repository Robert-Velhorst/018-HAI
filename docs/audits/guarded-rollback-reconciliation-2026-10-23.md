# Guarded rollback reconciliation

## Scope and decisions

Reviewed six retained source versions: the guarded rollback regression suite,
earlier down migrations 0070, 0093 and 0095, and the artifact recovery and
retention integration test suites. This is not a whole-repository
or archive completion claim.

- Keep current exclusive table locks and terminal review metadata protection
  in 0070; the older script only checked unresolved review status.
- Keep current retained-artifact refusal before removing owner-binding
  constraints in 0093; the older script removed them unconditionally.
- Keep current Trello state, receipt and progress preflight in 0095, including
  preservation of the shared source-item uniqueness index.
- Keep current account-feed coverage and comment-aware source lint rather than
  replacing the test suite with its older version.
- Keep current claim-token fencing, explicit failed-claim release/backoff and
  lease-expiry scenarios in recovery tests. The older fixture omitted migration
  0098 and did not test these stronger guarantees.
- Keep current retention source-event classification and the additional
  independent-archive quota race and independent-owner concurrency scenarios.
  The older retention suite adds no missing behavior.

## Integrated correction

The inherited isolated rollback fixtures declared only the `pre` directory.
The production executor correctly refuses pre-phase rollback when it cannot
read the later `post` phase. Consequently the fixtures could not exercise the
intended SQL guards or successful empty rollback path.

Extract the fixture filesystem builder and explicitly declare an empty `post`
directory. A database-free regression covers all three fixture versions.
No production phase guard or historical SQL checksum is changed.

CI now selects all three guarded rollback tests against its existing disposable
PostgreSQL service and requires an exact top-level PASS marker for each. A skip
cannot satisfy the marker gate. Existing recovery tests remain selected.

The review also found seven existing retention tests absent from the explicit
PostgreSQL selection: legacy download/provenance upgrade, retained-data rollback,
concurrent forget, cross-archive quota race, independent-owner concurrency,
key-check persistence after final forget and competing first-write keys.
All are now selected and require their own PASS marker. A source-enumerating
contract prevents either reviewed integration test file from silently acquiring
new SQL tests that CI only compiles and skips.

## Verification and preservation boundary

- The new filesystem regression failed for all three fixtures before the fix.
- The new CI contract failed before workflow wiring was added.
- The source-enumerating CI regression first identified all seven omitted tests.
- Local migration package tests and 112 CI contract tests pass after the fixes.
- No local PostgreSQL DSN was provided; SQL-backed tests skip locally. Their
  actual database execution remains a CI acceptance gate, not a local claim.
- Original source copies remain intact. Publication alone does not authorize
  deleting them; main integration and preservation checks remain required.
