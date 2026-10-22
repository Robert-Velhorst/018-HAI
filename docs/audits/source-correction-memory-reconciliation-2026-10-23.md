# Source correction and memory lesson reconciliation

## Reviewed source versions

Compared the retained artifact-worktree versions of:

- `backend/internal/memory/source_extraction_lessons.go`
- `backend/internal/memory/source_extraction_lessons_test.go`
- `backend/internal/memory/source_extraction_lessons_postgres_test.go`
- `backend/internal/source/extraction_correction_repository.go`
- `backend/internal/source/extraction_correction_worker_fence_postgres_test.go`
- `backend/internal/source/extraction_patch_postgres_test.go`

The current implementation retains these capabilities with stricter evidence
and concurrency controls. Replacing it with the retained versions would weaken
the boundaries below, so those older versions are not copied over current code.

## Decisions

| Boundary | Retained behavior |
| --- | --- |
| Evidence ownership | Active, enabled, non-revoked owner-bound source; current extraction and durable raw item must agree. |
| Revision and content | Exact extraction revision and lesson-content digest in the correction audit; a durable correction intent alone cannot authorize arbitrary lesson text. |
| Worker persistence | Validate worker, durable job, lease generation, correction phase, applied revision, and the generated request before recording the audit and lesson in one transaction. |
| Sensitive evidence | Only the fixed non-content review lesson and canonical extraction reference may persist. |
| Lesson kinds and sizes | Accept only lesson/correction-lesson kinds with evidence URI/label and ordinary memory storage bounds. |
| Conflicting content | Identical retries remain idempotent; a different lesson cannot silently overwrite an existing one. |
| Lock ordering | Source/extraction/raw evidence locks precede a separately namespaced memory advisory lock, avoiding collision with the worker session fence. |
| Deletion | Exact owner and extraction provenance scope memory/vector cleanup; caller transaction rollback remains effective. |
| Test fixtures | Keep durable raw evidence and varied lesson kinds; source sync leases remain authoritative during worker memory persistence. |

## CI gap and repair

The existing workflow did not explicitly run the selected source-lesson and
correction-fence PostgreSQL suites. Add ten memory tests and thirteen source
tests to the existing migrated, disposable PostgreSQL job. Anchored test-name
selection and a required PASS marker for every selected test prevent renamed,
missing, or skipped tests from being mistaken for executed database coverage.
No extra local database, container, or CI job is introduced.

## Verification boundary

The full memory and source packages passed locally without integration tags.
Workflow syntax and integration-tag compilation are checked separately.
The workflow YAML parsed, its added shell block passed Bash syntax checking,
and all 23 selected test names resolved to exactly one test declaration each.
Both integration-tagged packages compiled with an empty test selection; this
was compilation only, not a PostgreSQL test run.
Actual PostgreSQL execution of the newly selected tests must still pass CI;
local unit results do not prove database lock or transaction behavior.

Original source copies remain retained. This semantic review does not prove
byte-for-byte preservation on main or make the entire worktree disposable.
