# Historical model-field reconciliation

## Scope and method

Baseline main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
Reviewed seven changed model additions from the 251-variant Go cohort. The frozen archive helper rechecked event/content identity and stable archive metadata; a standard Go AST parser compared named/embedded fields, type expressions, unquoted tags and relative old-field order.

The current seven working files match main after Git filtering. The machine comparison is `archive-model-field-dispositions-2026-10-24.json`. It covers historical structs in these files, not every newly introduced model or whole-module behavior.

## Field results

- 22 historical structs contain 300 archived fields.
- 298 field names remain with the same formatted Go type. Their relative order and archived imports remain represented.
- The two absent names are ambient counters with explicit current successors, described below.
- Main adds 65 fields inside these historical structs. Field presence is not proof that their handlers, constraints or workflows are operational.
- Six retained fields have changed database tags; their JSON names and Go types are unchanged.

| Source path | Archived reference |
| --- | --- |
| `backend/internal/models/context_memory.go` | `acd59756c9cb` |
| `backend/internal/models/connected_source.go` | `54a511eada62` |
| `backend/internal/models/verification.go` | `cade0dc0ad16` |
| `backend/internal/models/workflow.go` | `3022e50cebf2` |
| `backend/internal/models/memory_engine.go` | `f65c0ec7a716` |
| `backend/internal/models/ambient.go` | `b7bc57b3e13b` |
| `backend/internal/models/pursuit.go` | `3529f26bccba` |

## Ambient counter successors

Historical `AmbientScan.Executed` summed follow-up triggers/resolutions and completed workflow runs. Current `Advanced` receives those contributions without calling all advancement verified execution. Current `DeduplicatedBytes` replaces `RawBytesAvoided`; its duplicate-candidate calculation includes rationale, next action and evidence-manifest text. This is a logical text-byte estimate, not measured filesystem reclamation or network bandwidth savings.

Inspected the archived service assignments, current ambient service/repository writers, baseline schema columns and frontend interface/scan normalization names. The current product uses `advanced` and `deduplicatedBytes` consistently in those paths.

No operator database was read or migrated. Values in a hypothetical legacy store with `executed` or `raw_bytes_avoided` columns have not been proven copied into the new columns. No backward-compatible old JSON alias or legacy-row backfill is claimed. Such data remains a preservation gate, not permission to delete a database.

## Database-tag changes

- Connected-source cursor and sync-job cursor-before/cursor-after now use text columns. Migration `0104_source_sync_cursor_storage.up.sql` explicitly widens all three. Its down migration checks for oversized values and refuses narrowing that would discard them. These SQL files were read, not executed.
- `SourceConnector` catalog entries default false rather than silently enabling an unconfigured adapter. The current model and baseline schema agree. Existing connected-account settings and legacy column defaults were not inspected or changed.
- Conversation platform/external-ID fields gain participation in the owner/platform/external-ID unique key; the archived tags had ordinary indexes, not that unique key. The current model and baseline index agree. Existing database constraints, duplicate rows and backfill were not inspected or migrated.

## Execution and remaining gates

Built and ran a small trusted AST analysis utility with the existing Go 1.27.3 toolchain, offline module resolution and bounded compilation. Only archived source text was parsed; it was not executed. The utility's isolated build cache was cleaned with native `go clean -cache`, removing 48,344,456 logical bytes and retaining the small reusable parser executable. No application build, product tests, provider calls, database migration, Docker stack or browser acceptance ran.

Alias identity, type checking, other declarations/comments, new models, real migration/data compatibility, publication and exact-head CI remain separate. These seven snapshots are not raw-source-equal to main. No source, archive or database is deletion-eligible because its field names were accounted for.
