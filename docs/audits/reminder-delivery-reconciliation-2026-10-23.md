# Reminder activation and delivery reconciliation

## Retained-source review

Read the complete retained reminder activation implementation, delivery
implementation and delivery specification, then their complete differences
against current code. Keep current safeguards rather than restoring the older
variants. This covers three retained files, not the full workflow repository,
all database fixtures or the transcript archives.

Preparation, approval and internal delivery authorization remain distinct and
owner-bound, with immutable evidence digests and explicit confirmations. None
grants general execution authority. Preserve exact idempotency and decision-chain
binding, supported scheduling horizon, current-source checks for new approval,
and revocation after preparation expiry or source changes. Cancelling an existing
grant must not require the original source to remain actionable.

Preserve atomic internal signal plus receipt storage, row-lock revalidation,
savepoint isolation for a failing transactional sink, explicit expiry, bounded
retry/dead-letter behavior and owner-scoped history. The nontransactional fallback
is restricted to explicitly marked in-memory fixtures, not unknown repositories.
Keep context-bound repository views, cancellation checkpoints and partial results
for already-confirmed deliveries. Do not restore background-context sink calls
or treat cancellation as a new retry receipt.

## Missing-decision correction

The immutable decision validator read `value.Decision` before checking whether
`value` was nil. A missing saved decision could panic instead of returning a
validation error. Reject nil before accessing its fields, using the existing
invalid-evidence error. Non-nil digest, owner, confirmation, expiry and decision
chain rules are unchanged.

Two direct missing-decision cases and a service-level missing saved-decision
case all reproduced the panic before repair. The service test confirms that
the save boundary was reached, but no success result is returned without its
decision evidence. Positive controls retain a valid owner-bound approval and
reject a decision without activation evidence.

## Verification and preservation boundary

After repair the complete uncached workflow package and workflow Go vet pass.
This includes existing reminder expiry, horizon, revocation, bounded retries,
context checkpoints and transactional-boundary fixture checks. No local database
DSN was supplied: these results do not establish real PostgreSQL lock/rollback
acceptance, deployed delivery, provider access or external notification behavior.
The change does not run an installer, enable a scheduler or send a reminder.

Original copies remain intact and are tied to frozen content hashes in the
private review ledger. Publication, semantic replacement and passing tests do
not establish exact main preservation or whole-worktree/archive deletion
eligibility. Those remain separate gates across the full retained scope.
