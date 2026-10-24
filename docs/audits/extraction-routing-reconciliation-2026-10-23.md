# Extraction and routing reconciliation

This is a bounded follow-up to the original Go inventory, not a certificate that
the complete original checkout or its archives can be removed.

## Extraction ownership filter

The original `source/handler.go` contained `filterExtractionsForSources`.
`Extractions` loaded the owner's extraction history, filtered it against owned
source IDs, then applied its result limit in memory.

The current handler requires a bounded page service. Without an offset it calls
`ExtractionPageForOwner`; with an offset it calls `ExtractionsForOwnerPage`.
Both resolve source IDs through `extractionSourceIDsForOwner`, which rejects an
empty owner and includes only sources whose trimmed owner exactly matches the
requested owner. Foreign and ownerless legacy sources are excluded.

The production repositories apply those source IDs before counting and limiting
records. Both return an empty result without querying when the source-ID set is
empty. The offset path also applies its offset in the database. Restoring the
old full-history materialization would lose this bounded-query behavior.

The local ownership regression now exercises both service entry points, checks
that only owned IDs reach the repository, and rejects blank owners before any
extraction query. Existing HTTP tests cover offset dispatch and invalid offsets.
These fixtures establish service/handler contracts, not live PostgreSQL tenant
isolation or concurrent ownership-transfer acceptance. The full original handler
delta and other source variants still require review.

The focused native test run (`go test -p 1 -count=1 -run
'^TestExtractionHistory' ./internal/source`) passed locally. It covers three
top-level tests, five invalid-offset subtests and the two ownership-scope
subtests. This is not a full source-package test run.

## Routing compatibility wrapper

The original `initializeRoutes` was a wrapper that supplied
`context.Background()` to `initializeRoutesWithContext`. Current startup instead
calls the contextual initializer from `router.go`, passes the application
context, and registers worker shutdown with the HTTP server. The initializer
rejects a nil application context.

The removed wrapper name therefore does not, by itself, prove a lost route or
missing initialization. Reintroducing it would reintroduce an unowned lifecycle
context. This finding covers that wrapper and its current call site only; it
does not assert equivalence of every registered route, middleware, background
worker, or shutdown path. No router runtime test is claimed by this document.

## Preservation boundary

The current bounded-query and lifecycle paths are retained. No original files,
archives, source history, databases or worktrees are deleted by this review.
Publication to a draft PR remains separate from verified preservation on `main`.
