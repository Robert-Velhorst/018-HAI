# Task state PostgreSQL reconciliation

## Retained-source decision

Read the complete retained PostgreSQL task-state repository and the complete
current difference. Preserve the current compact owner-scoped history view,
stable bounded ordering, immutable metadata validation and context-aware full
plan lookup. Do not restore full plan payloads into the compact history path.

Keep owner-filtered completion/review records, exact create replay bindings,
immutable review decisions, row-locked resolution/outcome transitions and
approval revision/request-digest checks. Retain the shared execution-pool
identity helper: it checks scoped pool identity without returning an unrestricted
database handle. Pool identity alone does not grant approval or prove that a
particular review remains usable.

## Guard correction

The approval-transaction guard checked database pointers and dialects but
dereferenced promoted GORM configuration fields before establishing that their
configuration existed. It also accepted dry-run root/transaction metadata.

Reject missing configuration and dry-run handles before checking the dialect,
transaction shape and shared pool identity. Do not alter decision contents,
owner scope, row locks, transition semantics, schema or stored data.

Four guard cases failed before repair: two missing configurations panicked and
two dry-run configurations were accepted. After repair, each invalid case is
also passed through the approval lookup entry point and rejected before SQL.
Nil handles and an already-transactional root remain rejected. A same-pool
structural positive control and foreign-pool negative control remain covered.

## Verification and limits

The complete uncached task package and task Go vet pass, including the earlier
mixed-polarity evidence correction in this PR. The storage guard fixture only
exposes pool identity; it opens no connection and issues no SQL. It does not
prove PostgreSQL locking, trigger enforcement, durability, concurrent approval
consumption or production readiness. SQL integration remains a separate CI gate.

This reconciles one retained repository source, not every task service, tagged
integration fixture, worktree or transcript. The original remains intact and
its frozen source hash is recorded privately. No deletion eligibility follows
from source review, test success or publication alone; main preservation and
the remaining full HAI scope are still separate requirements.
