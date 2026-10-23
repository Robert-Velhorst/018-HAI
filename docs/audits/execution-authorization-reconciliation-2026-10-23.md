# Execution authorization reconciliation

## Original-source decision

The complete original/current deltas of
`backend/internal/executionauth/repository_postgres.go` and
`repository_postgres_test.go` were reviewed. The current repository file has
28 unchanged declarations, seven changed declarations, one expanded import
group and one added lock-identity helper. No original function is removed.
The current test file has 20 unchanged declarations, two changed tests and two
added tests, with no removed declarations. These are structural comparison
counts, not successful test counts.

Both current file blobs already match the inspected `main`. Restoring the old
copies would discard these retained improvements:

- Receipt insert/select/scan/reference binding includes control decisions and
  OpsControl review item/decision provenance. The inspected migration binds
  owner, review decision and item through a composite foreign key and checks
  that evidence and reference columns agree.
- Approval-claim query/scan failures are returned before checking an empty
  approval source; the older ordering could silently treat an error as absence
  of approval. Existing database-driver regression assertions cover that case.
- Advisory-lock identities encode each component as hex before joining with
  colons. This avoids embedded NUL bytes in PostgreSQL text parameters and
  ambiguous component boundaries. The lock statement uses execution rather
  than interpreting a PostgreSQL `void` result as a Boolean authorization.
- The existing read-committed requirement, owner-scoped claim checks,
  transaction boundary and immutable receipt/consumption records remain.

This comparison includes the changed source contexts, the relevant provenance
migration and rollback guard, and existing unit/integration assertions. It is
not a full authorization security certification or production-database audit.

The related original `executionapproval/composite_resolver.go` was also
compared in full. Six declarations remain token-identical, three are extended,
and three are added; none is removed. Task/workflow/portfolio/owner-control
routing remains, with a dedicated OpsControl-review resolver and server-owned
additional namespaces that cannot overlap reserved or already registered
prefixes. Resolver rejection still returns directly rather than trying another
approval source. The current router registers the control-decision resolver and
attaches the OpsControl-review resolver before constructing the authorization
engine. This is inspected wiring, not an executed end-to-end approval flow.
Registering an arbitrary additional namespace alone does not establish database
receipt support; persistence still validates its supported evidence types.

## Close the database test selection gap

The existing backend `go test ./...` command can skip PostgreSQL tests when its
database environment is absent. The dedicated real-PostgreSQL migration job had
selected only the polymorphic approval and owner-control claim tests. It did not
select the added OpsControl provenance test or the two changed expiry tests.

The existing job now selects all five named tests and requires an explicit
top-level `PASS` line for each:

1. `TestPostgresReceiptPersistsOwnerSafePolymorphicApprovals`
2. `TestPostgresRepositoryClaimsOwnerControlApprovalAcrossReceipts`
3. `TestPostgresReceiptPersistsOpsControlReviewProvenance`
4. `TestPostgresRepositoryRejectsExpiredApprovalAtConsumption`
5. `TestPostgresFinalEffectExerciseRejectsStaleAuthority`

The existing job's database environment and migration setup are unchanged. A
missing or skipped test cannot satisfy its added `PASS` guard. The expiry tests
retain the expired-consumption and final-effect rejection intent while using
valid persisted approval references and letting an approval expire after valid
consumption. The two expiry subcases currently share a one-second lifetime;
they do not establish distinct microsecond-boundary coverage.

## Verification and limits

Locally verified: workflow YAML parsing, Bash syntax of the complete migration
step, exact selection of the five required names, rejection of suffix-matched
names, and all five mandatory `PASS` guards. The source comparison rejected
changed original hashes and checked current files against committed blobs.

The three newly selected database cases have not yet been executed by this
changed workflow. The earlier passing migration job used the previous two-test
selection and must not be presented as proof of the new cases. No local
database, container, account or provider was modified for this review.

The old source variants remain retained. Neither this comparison nor a future
passing CI run proves whole-checkout or transcript preservation on `main`; no
local source/archive deletion is released by this checkpoint.
