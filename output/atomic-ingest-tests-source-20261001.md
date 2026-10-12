# Atomic Ingestion Regression Tests: Source-Only Handoff

Date: 2026-10-01

## Owned Changes

- `backend/internal/operations/ingest_atomicity_regression_test.go`
- `output/atomic-ingest-tests-source-20261001.md`

No production source or existing test files were edited by this sidecar.

## Source Basis

Read the landed `Service.Ingest`, `Service.Save`, `AtomicCreationRepository`, creation validation, `MemoryRepository.CreateWithEvent`, mutation/claim guards, operation models, and existing `sampleInput` / `mutationSeed` fixtures. Applied the Go testing skill's table-driven and focused assertion patterns. Existing repository history was consulted only for workspace and verification boundaries; current behavior was taken from source.

## Added Assertions

- Memory creation stores exactly one operation/event pair, generates or retains the audit UUID, preserves caller inputs, and isolates scalar caller/return edits from stored values.
- Service creation supplies a nonzero operation UUID before the bound `created` / HAI / new-status audit and never calls bare Create, Update, or AppendEvent.
- Creation repository errors and real memory duplicate-event refusal leave the complete operation map and audit sequence unchanged.
- A repository without optional atomic creation refuses new ingestion with `ErrAtomicCreationUnsupported`; unchanged, empty, or `{}` duplicate evidence remains read-only and supported.
- Owner/workspace trimming and blank-workspace `local` default apply to both lookup and persistence. Opaque dedupe keys remain byte-for-byte unchanged.
- Identical keys remain isolated between owners and workspaces. Eight simultaneous ingests rendezvous after their initial lookup misses, then must produce exactly one creation and one event; the test has a five-second total wait bound.
- A creation collision returns the scoped winner unchanged, including its evidence, with no loser refresh or extra event.
- Invalid required fields and malformed evidence JSON are refused before any lookup or persistence, including when a matching operation already exists.
- Duplicate evidence refresh uses atomic Save and `source_evidence_refreshed`; a full-operation comparison allows changes only to EvidenceJSON, UpdatedAt, and Version, preserving stored lifecycle, safety, approval, world model, identity, source fields, and CreatedAt.
- Claimed, stale-CAS, repository-error, duplicate-audit, and unsupported-refresh cases preserve all operation/event state and reject split-write fallbacks.
- Direct malformed creation pairs cover missing identities, non-initial version/status, mismatched audit binding/type/actor/status, nonempty before-status, malformed or empty operation evidence and event payload JSON, and missing source/operation types.
- Direct operation UUID, audit UUID, and active dedupe collisions preserve both existing rows and immutable caller inputs.

All new helper names use the `ingestAtomic` prefix. The tests reuse existing fixtures without changing them. No synthetic SQL driver was added; the parent owns that separate coverage.

## Verification Boundary

Source inspection and manual review only. No tests, builds, formatters, Docker, services, provider calls, downloads, subagents, or Git commits were run. No compile, race-detector, formatting, SQL transaction, PostgreSQL locking/durability, or production acceptance result is claimed. The parent owns serial executable checks and any production changes.

## Parent Integration Amendment

The original collision assertion above reflected the requested snapshot behavior,
not the final behavior. The safety review identified silent loss of differing
evidence after a creation race. The parent changed that case to require a scoped,
audited refresh and added refusal when the winning operation has become claimed.
Both duplicate paths now use the same refresh function; unchanged duplicates
remain read-only. Parent formatting and the final native operations suite pass
43 top-level / 134 test-subtest checks in
`production-low-resource-atomic-intake-operations-reviewed-20261001.jsonl`.
This is parent-executed local proof, not agent execution, race detection or actual
PostgreSQL acceptance.
