# Connected-source test corpus reconciliation

## Scope and method

Retained `backend/internal/source/service_test.go` variants of 162,512 and
156,319 bytes were compared with published commit
`13933b0a744b37f671d39ac2651767b10845e3d1`. The variants differ; they are not
treated as duplicate bytes or interchangeable snapshots.

The Go AST/token comparison accounts for every declaration, including comments,
literals and statement order. The model-maintenance variant has 194 declarations:
174 token-identical, 19 changed and one replaced import group. The artifact
variant has 192 declarations: 172 token-identical, 19 changed and one replaced
import group. Every retained function and import remains in the current file,
which has 216 declarations.

The full model-to-current diff (625 added and 13 removed lines) and the complete
artifact-to-model delta were read through their ends. Token-identical bodies
were not all manually reread. Helper behavior outside this file is not proved by
declaration identity.

## Preserve the newer coverage

- Blank owner identities are rejected before scoped or unscoped repository
  queries. Unsupported Autofever intake remains explicitly disabled rather than
  accepting synthetic records as a working integration.
- Local HTTP fixtures opt into exact loopback endpoints, replacing the older
  broad private-network override. Additional cases reject private and metadata
  destinations, mixed public/private DNS results, redirection of the dial target
  through a second lookup, and loopback hostname alias reuse.
- Returned, persisted job and audit errors must redact decoded query keys and
  duplicate query values, not only a plainly spelled token parameter.
- Trello polling success is not webhook verification. Callback configuration,
  missing receipts, recent persisted delivery, stale delivery and invalid future
  or incomplete receipt records have distinct assertions. A stored receipt does
  not establish current provider health or Trello-side registration.
- Canonical Trello board ID case does not retarget a source; a different board
  still fails. Existing untrusted-evidence review assertions remain present.
- Extraction update/archive tests simulate a newer revision arriving while the
  source lock is acquired and reject the stale snapshot.
- OAuth fake state/token operations model owner/source binding, expiry, single
  use, revocation and token-specific reconnect updates. Semantic and repository
  fakes expose request context, query scope and bounded stable-page behavior.

No old file was copied over the current tests. No production or current test
code was edited in this review.

## Verification and limits

`go test -p 1 ./internal/source -count=1` passed at the compared published commit.
These checks use local fixtures and test doubles. Database-dependent tests may
skip without their configured environment; the passing package command is not
evidence of running a real PostgreSQL server, live Trello/GitHub/Google accounts,
an external DNS adversary or production OAuth.

The retained production service and Trello test variants remain separate review
work. Original files remain intact; review, CI, publication, preservation on
`main` and local deletion are separate gates.
