# Source and memory boundary reconciliation

## Source scope

Compared six retained model-maintenance worktree versions with main
`4684546907900d4958f85e7b71ba8848376b0b93`:

| Files | Versions | Decision |
| --- | ---: | --- |
| `backend/internal/googleoauth/calendar.go` | 1 | Retain safe provider error metadata and Calendar sync-token expiry classification before reading error bodies. |
| `backend/internal/googleoauth/drive.go` | 1 | Retain safe provider errors, resource-unavailable and content-too-large classifications instead of echoing response bodies. |
| `backend/internal/memory/handler.go` and `handler_test.go` | 2 | Retain owner-scoped service checks, paged query dispatch and cloned API responses that omit internal extraction lineage without changing stored evidence. |
| `backend/internal/memory/service.go` and `service_test.go` | 2 | Retain durable verified-fact promotion, generic-write restrictions, unambiguous creation lock keys and source-aware deduplication. |

The memory-query cancellation follow-up is already proposed in PR #253 and is
not duplicated here. Retained older structural-only promotion must not replace
the current durable provenance transaction. Memory unit fixtures that simulate
promotion are not proof of real PostgreSQL provenance enforcement.

## Redirect repair

The initial Google API URL was validated, but Calendar, Drive, Gmail and People
clients could follow a response redirect without revalidating its destination.
Local two-server tests demonstrated requests carrying the fixture bearer token
to the second endpoint for both 302 and 307 responses.

All four API clients now reject redirects using a shared client-copy helper.
It retains configured transport, timeout and cookie jar without mutating the
caller's client. Existing connector default timeouts are unchanged. The original
redirect response reaches normal provider-error handling; redirected content is
not accepted as a successful sync page. OAuth token exchange already has a
separate no-redirect policy and is unchanged.

## Verification and limits

- Sixteen redirect cases failed before the repair: four API clients, two
  redirect statuses, and default/injected HTTP clients. They pass after it.
- A configuration test checks client-copy isolation and preserved transport,
  timeout and cookie-jar configuration.
- Full Google OAuth, memory and source package tests passed locally on Windows
  with Go 1.27.3. These use local fixtures, not real account data.
- No production credential disclosure is asserted. If a real provider needs a
  legitimate redirect, it will now fail closed pending explicit reviewed
  endpoint support; live sync/download acceptance remains a release gate.
- Private source fingerprints and original copies remain intact. No local
  source, transcript, settings or database data was removed.
