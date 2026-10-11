# Performance Baseline & Indexing

This baseline keeps performance evidence explicit and scoped. Production
authenticated memory browsing runs as a bounded, owner-scoped PostgreSQL query.
The benchmark below exercises the pure Go fallback used by legacy/custom
repository implementations; it is not database latency evidence.

## Fallback query benchmark (10,000 memories)

Measured on 2026-08-14 in the pinned Go 1.25.12 Linux container on Windows
Docker Desktop (`Benchmark-16`, one run):

| Benchmark | Operation | Time/op | Bytes/op | Allocs/op |
| --- | --- | ---: | ---: | ---: |
| `BenchmarkQueryFilterSortPaginate` | kind filter + sort + paginate | 2.10 ms | 2,333,017 | 7 |
| `BenchmarkQuerySearch` | free-text relevance search + sort | 11.37 ms | 6,053,974 | 77,553 |

Reproduce:

```bash
go test ./internal/memory -bench BenchmarkQuery -benchmem -run '^$'
```

**Reading it:** the fallback still scans an entire supplied slice. Free-text
search is allocation-heavy because it tokenizes every candidate. Production
requests avoid this path when the configured repository implements
`OwnerQueryRepository`.

## Production PostgreSQL query path

`GET /api/v1/memory/query` applies the authenticated owner, optional project,
archive state, kind, exact normalized tag, escaped literal search tokens,
count, deterministic order, limit, and offset in PostgreSQL. Search input is
bounded to 512 runes, filters are length-bounded, distinct search tokens are
capped at 16, and page size is capped at 100.

Migration `pre/0061_context_memory_owner_query_indexes` adds the indexes used by
the hot paths:

| Index | Purpose |
| --- | --- |
| `idx_context_memories_owner_active_updated` | owner/archive listing ordered by freshness |
| `idx_context_memories_owner_project_active_updated` | project-scoped owner listing |
| `idx_context_memories_owner_kind_active_updated` | case-insensitive kind filtering |
| `idx_context_memories_search_trgm` | trigram acceleration for the normalized content/title/source expression |

The PostgreSQL integration test proves owner/project/archive isolation, literal
`%` and `_` handling, exact tag filtering, pagination, and request cancellation.
The retained local stack applied migration 0061, exposed all four indexes to the
restricted runtime role, and denied that role temporary-table creation.

## HAI OS aggregate snapshot

`GET /api/v1/os/overview` loads dashboard aggregates in one request-cancellable
PostgreSQL statement. The previous handler issued repeated sequential count
queries, including duplicate reads for values and statuses, and suppressed
database errors as zero. The consolidated query reuses owner-scoped source,
workflow, and verification-run CTEs, returns one consistent snapshot, and fails
closed when PostgreSQL cannot supply it.

A disposable PostgreSQL integration fixture proves owner isolation, archived
and revoked record exclusion, due-date handling, literal `pursuit_` prefix
matching, and deterministic latest-scan selection. This is query-count and
correctness evidence, not a production latency claim; representative
release-target percentiles remain required.

## Guardrails already in place

- Pagination and query inputs are bounded before repository execution.
- Database reads use request context so obsolete browser requests can cancel.
- Large-dataset fallback correctness is covered by `largedataset_test.go` (50k
  rows); it is not a production SQL load benchmark.
- Production-scale `EXPLAIN (ANALYZE, BUFFERS)` and retained latency percentiles
  still require a representative owner-scoped dataset on the release target.

## Local event-broker baseline (Windows Docker Desktop)

HAI clients continue to use the Kafka protocol at `kafka:9092`. The local
Compose implementation is Redpanda Community Edition so the single-user stack
does not pay for an idle JVM and KRaft controller.

| Broker | Ready-state memory | Processes | Scope |
| --- | ---: | ---: | --- |
| Previous Kafka 7.6.1 KRaft broker | about 364 MiB | 64 | Active local broker immediately before cutover |
| Pinned Redpanda v26.2.1 durable broker | about 58.7 MiB | 3 | Same host/network; fsync bypass disabled, cluster info and topic creation passed |
| Pinned Redpanda with connected HAI clients | 153 MiB median | 4 | Three live samples after backend, IDP, config-manager, and application round-trip |
| Pinned Redpanda with fractional-CPU scheduling | 98 MiB median | 4 | Six live samples after the same authenticated create/delete round-trip; fsync bypass remains disabled |

The isolated durable-broker reduction is about 305 MiB (83.9%) and 61
processes (95.3%). The connected live broker still reduced measured broker
memory by about 211 MiB (58.0%) and 60 processes (93.8%). These are point-in-time
measurements, not throughput claims.

On 2026-08-14 an A/B run on the same Docker Desktop host compared only the
Seastar scheduler mode. With `--overprovisioned=false`, six post-round-trip
samples settled at 151 MiB and about 1.3% CPU. With
`--overprovisioned=true`, the corresponding samples settled at about 98 MiB
and 0.38% CPU. The local broker has a fractional `0.5` CPU ceiling and shares
Docker Desktop with the rest of HAI, so the overprovisioned scheduler avoids
busy polling. This does not enable Redpanda development mode: unsafe fsync
bypass remains false, write caching remains disabled, and the named volume is
retained.
CI starts the actual Compose service and requires a Kafka metadata request plus
topic creation/description, disabled write caching, and no unsafe fsync bypass.
Release acceptance additionally requires the HAI producer, config-manager
consumer, and readiness path against the rebuilt local stack.
