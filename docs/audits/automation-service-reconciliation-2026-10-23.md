# Automation service reconciliation

## Scope and decisions

Compared the automation service and execution-authorization test variants in
both audit worktrees, plus the artifact-worktree idempotency and OpenClaw CLI
identity test variants: six source versions. The two service variants match
after line-ending normalization. Their older behavior is not ported over the
current safeguards:

- Keep transactional CRUD/outbox writes and credential-preserving updates.
- Keep literal destination resolution, pinned HTTP dialing, IPv6 formatting
  and secret-redacted diagnostics.
- Keep immutable review configuration, action binding, bounded approval
  registration/issuance/consumption and uncertain-storage errors.
- Keep contextual launch/stop admission, exact owner/task/reference identity,
  immutable intents and narrow monotonic launch-summary writes.
- Keep the emergency-stop dispatch fence, staged pinned script bytes, bounded
  preprocessing/output capture and process cancellation safeguards.
- Keep complete bounded capture before redaction rather than truncating raw
  output first; incomplete captures must not leak raw prefixes.
- Keep context-aware policy test fixtures and distinguish in-memory proof-chain
  fixtures from the production PostgreSQL approval transaction.

The older idempotency test version omits the existing outcome-identity/conflict
regression. Retain that regression. Automation-service test-file variants are
not included in this completed review batch.

## Verification improvement

The existing PostgreSQL tests for concurrent launch-intent admission and
outcome retry identity/conflict preservation were not explicitly required by
the migration CI job. That existing job now runs both against its disposable
database and requires each named PASS marker. A skipped or missing test cannot
satisfy the gate. No additional database service or CI job is introduced.

Their shared database helper now uses the existing dedicated PostgreSQL test
guard before opening a connection or applying migrations: explicit destructive
test opt-in, literal loopback address and exact disposable database identity.

The new CI contract failed before the workflow wiring and passes afterward.
All 111 workflow contracts pass. Live SQL execution remains a separate CI gate;
local Go tests without a configured dedicated database skip those SQL cases.

## Preservation

A fresh comparison of all 618 selected source versions against the current
main snapshot found no exact raw Git-blob matches. Original selected files
still matched their frozen inventory. No original worktree or transcript was
deleted. Review decisions are not byte-preservation or whole-archive completion
claims.
