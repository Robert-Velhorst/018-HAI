# Backend Migration Notes

The repository-wide migration runner, ordering rules, and rollback procedure are documented in [`docs/migrations.md`](../../docs/migrations.md). This file records backend-specific behavior that can affect source and memory evidence.

## Destructive PostgreSQL test targets

The destructive integration suites for resilience, agent registry, brain-skill
selection, evaluation, and durable jobs are skipped unless `HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true`
and their package-specific DSN is configured. Each DSN must use a literal
loopback IP and the exact dedicated database name shown below; hostnames,
remote targets, multi-host failover, and similarly named databases are refused.
These suites can drop schemas, replay migrations, or probe append-only guards,
so point them only at separately provisioned disposable databases.

| Suite | DSN variable | Required database |
| --- | --- | --- |
| Resilience | `HAI_RESILIENCE_TEST_DATABASE_DSN` | `hai_resilience_test` |
| Agent registry | `HAI_AGENTREGISTRY_TEST_DATABASE_DSN` | `hai_agentregistry_test` |
| Brain-skill selection | `HAI_BRAIN_SKILL_SELECTION_TEST_DATABASE_DSN` | `hai_brain_skill_selection_test` |
| Evaluation | `HAI_EVALUATION_TEST_DATABASE_DSN` | `hai_evaluation_test` |
| Durable jobs | `HAI_DURABLEJOB_TEST_DATABASE_DSN` | `hai_durablejob_test` |

## `pre/0101_llm_model_maintenance_admission_claims`

Migration 0101 adds an owner-free Ollama admission table keyed by provider ID,
model ID, and the SHA-256 fingerprint of the active configuration. The
backend commits an `in_progress` claim before calling local Ollama tags or
pull endpoints. A fenced finalization records verified success (next refresh
no sooner than the daily interval) or `retry_wait` (the configured bounded
failure retry interval). Claim-storage errors block provider calls; they do
not fall back to process-local cooldowns.

If a process stops while its claim is `in_progress`, new attempts are blocked
until the lease expires. The lease covers three bounded provider phases, the
bounded post-failure digest inspection, and a 30-second margin: 45 minutes
35 seconds with default timeouts, up to 3 hours 35 seconds at the configured
maximum. A maintenance-history write failure is finalized as `retry_wait`; if
claim finalization also fails, the unfinalized lease remains the durable
fallback. Cancellation after claim acquisition best-effort finalizes a
retry; if that write fails, lease expiry is the recovery path. After the retry
timestamp expires, a new atomic claim may proceed. A changed configuration
fingerprint is a separate key and may proceed without waiting on the old one.

The rollback migration refuses to drop active claims or any unexpired claim
window, including the daily success-reuse window. Stop new local model
refreshes and let all claim windows expire before considering rollback; do not
delete claim rows to force it.

The opt-in cross-instance test is:

```text
go test -tags integration -count=1 -run '^TestOllamaAdmissionPostgresCrossInstanceAndRecovery$' ./internal/llm
```

It requires `HAI_TEST_DATABASE_DSN` to target loopback PostgreSQL and
`HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true`. It creates and drops its own
uniquely named database and does not migrate the configured database.

## `pre/0090_context_memory_source_extraction_identity`

Migration 0090 adds nullable `context_memories.source_extraction_id` and a partial unique index over owner, extraction, and memory kind. It preserves the original `source_uri` value; the new UUID is separate identity, not a replacement for the evidence link.

The migration keeps the exact legacy `source-extraction://<uuid>` backfill. It also links an older correction lesson only when all of these conditions hold:

- the memory kind is exactly `lesson`;
- its comma-separated tags contain the exact `source-correction` token (case-insensitive, with surrounding whitespace ignored);
- the memory has a non-empty owner identity that exactly matches a connected source owner;
- its stored `source_uri` is byte-for-byte equal to a source extraction URI belonging to that owner's connected source; and
- exactly one distinct extraction matches.

Ambiguous extraction matches, owner mismatches, other memory kinds, and near-match tags remain unlinked. Before backfilling, the migration also checks the combined canonical-URI and exact-URI candidates by `(owner_identity, extraction_id, kind)`. If multiple memory rows would collide with the unique index, it leaves every row in that collision group unlinked; it does not pick a winner, rewrite or delete a memory, or fail solely because of that collision. The migration never normalizes, redacts, or otherwise rewrites `source_uri`.

### Rollback

The down migration checks for any non-null `source_extraction_id` before dropping the index or column. If provenance exists, it raises an error and refuses the rollback; PostgreSQL leaves the identity column, index, and evidence links intact. Do not clear identities or delete memories merely to force rollback. Prefer a forward migration or a separately reviewed, data-preserving migration plan.

The focused disposable-PostgreSQL integration test is:

```text
go test -tags integration -count=1 -run '^TestContextMemorySourceExtractionIdentity0090ApplyAndRefusesPopulatedRollback$' ./migrations
```

It requires `HAI_TEST_DATABASE_DSN` to point to a disposable loopback PostgreSQL server and `HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true`; the test creates and drops its own uniquely named database.

## `pre/0107_workflow_reminder_delivery_expired_status`

Migration 0107 adds `expired` as a terminal outcome for an approved internal reminder whose delivery window elapsed before the worker processed it. The worker records the outcome in the append-only reminder-attempt ledger and does not call the reminder sink. The rollback refuses to remove the status while any expired receipt exists.
