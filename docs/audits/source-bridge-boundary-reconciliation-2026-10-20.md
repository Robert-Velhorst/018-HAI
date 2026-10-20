# Source and bridge boundary reconciliation

## Scope and comparison base

This bounded review compares retained source-handler and bridge variants with
main `e08d4bc029d7631b740f857051b6363324b29b5a`. It follows the Go declaration
crosswalk, but reads function bodies and callers rather than treating absent
names as absent features. No private archive identifiers or machine paths are
needed to describe the decisions.

## Decisions

| Retained behavior | Current code | Decision |
| --- | --- | --- |
| `filterExtractionsForSources` removes unauthorized rows after loading extractions. | `Handler.Extractions` calls bounded owner-scoped page services; `extractionSourceIDsForOwner` rejects an empty owner and excludes other owners and unowned legacy sources. Repository queries apply `source_id IN` before count, limit and offset. | Keep the current database-scoped implementation. Do not restore unbounded loading and post-query filtering. |
| `taskText` validates user text parts and rejects attached data, metadata and unsupported task context. | `taskInput` retains these checks and returns the message ID with text; strict decoding and replay handling additionally reject ambiguous input and avoid duplicate planning. | Keep `taskInput` and replay handling; restoring `taskText` would discard newer protections. |
| One older A2A `validateLocalURL` accepts private-network IPs and arbitrary paths. Another retained variant supports public ngrok configuration. | `validateBridgeURL` requires the exact unescaped `/api/v1/a2a` path, rejects credentials/query/fragment, and restricts addresses to loopback and named local gateway hosts. | Do not copy these wider address policies. External publication is not authorized by archive reconciliation. |
| MCP `validateLocalURL` restricts endpoints to loopback and named local hosts. | `validateEndpointURL` preserves local defaults; an explicit remote opt-in additionally requires HTTPS and a valid configured bearer token. | Preserve the current local default and explicit remote gate. A helper rename is not missing local validation. |

These are function-level decisions, not whole-file equivalence. In particular,
retained public-bridge behavior is deliberately not adopted, rather than claimed
to be present on main. Review any future public-bridge requirement separately.

## Evidence and verification

- Source code: `backend/internal/source/handler.go`, `service.go`, `history.go`
  and `repository_paged.go`.
- Existing source regressions exercise owner-scoped extraction queries, invalid
  offsets, and exclusion of unowned legacy sources.
- A2A code: `backend/internal/a2abridge/handler.go` and `service.go`.
  Existing tests cover unsupported shapes, ambiguous JSON and repeated delivery.
- MCP code: `backend/internal/mcppreflight/service.go`; existing tests reject
  credentials and nonlocal endpoints by default and enforce remote TLS/token
  prerequisites.
- `TestRetainedPrivateNetworkURLsDoNotBroadenBridgeBoundary` adds a regression
  for private IPv4/IPv6 addresses, wrong paths and escaped path aliases. It also
  checks that rejected configurations cannot reach the planning service.

The complete native `a2abridge`, `mcppreflight` and `source` package suites
passed. The new A2A regression was then run separately with verbose output;
all six cases passed. `git diff --check` passed. Hosted exact-head CI remains
required before merge. This review does not establish live-provider, public
deployment, IDP, database or external account acceptance.

## Retention

No local source, archive or worktree is removed by this report. There are other
unreviewed functions and files in the retained variants. These decisions do not
authorize deleting those variants or marking the complete archive reconciled.
