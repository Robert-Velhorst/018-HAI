# Router source reconciliation

## Retained variants

Two retained `backend/internal/router/routes.go` files, 102,696 and 100,507 bytes,
were compared against published commit
`13933b0a744b37f671d39ac2651767b10845e3d1`. They are not identical variants:
the older artifact copy grants write permission for forgetting retained OpenClaw
artifacts; the model-maintenance copy and current router require admin permission.

The Go AST/token comparison accounts for every declaration, preserving comments,
literals and statement order. Both older files have 90 declarations; the current
file has 92. The model variant has 78 token-identical and ten changed declarations;
the artifact variant has 77 token-identical and eleven changed declarations.
The remaining changes replace the import group and remove `initializeRoutes`.
Three route initializer functions are added; all previous imports remain.

The complete model-to-current diff (178 added and 25 removed lines) and the
complete artifact-to-model permission difference were read, along with selected
current initialization and authorization contexts. Token-identical bodies were
not all manually reread. Token identity does not prove dependency behavior.

## Preserve current composition

- Startup requires an application context instead of silently detaching workers
  through `context.Background()`. `router.go` supplies that context; removal of
  the compatibility wrapper is deliberate.
- Source scheduling propagates startup errors. Lifecycle-owned outbox and
  emergency-stop workers, scheduler observation and maintenance background gates
  remain wired to application startup.
- Account feeds use the durable registry, stable configured IDs and propagated
  seed errors rather than silently dropping registration failures. Runtime Lab
  receives the configured safe-execution policy.
- Browser verification can link workflow outcomes. Operational graph routes,
  approval/control-review resolvers and bounded chat-history context remain
  connected to their existing services.
- Source/background/pursuit overviews, workflow project dossier, account-feed
  identity preview, framework history and archive rollback routes remain exposed
  through their current permission checks.
- Trello webhook routes remain outside browser-owner middleware. The handler
  requires configured callback/signing material, bounds POST bodies and checks
  the signature before accepting events. Configured HEAD registration probes
  return without ingesting an event; that is not authenticated message intake.
- Global pause remains restricted to the configured installation owner, not
  every operator. OpenClaw artifact forgetting remains admin-only.
- Automation delivery inspection/retry requires the current operator/owner/admin
  middleware composition. Hire.AI routes retain their read-only search contract.

No older router was copied over the current code. This is a reconciliation of
the retained router files, not a claim that every underlying service, external
connector, startup database operation or background worker is production-ready.

## Verification boundaries

`go test -p 1 ./internal/router -count=1` passed at the compared published
commit. No dependency installation or production startup was performed.

The associated route tests exercise current middleware, fixture-backed handlers,
bootstrap behavior and server lifecycle separately from production startup.
Their route registration and status assertions do not prove real provider
execution, database durability or account acceptance. Review, CI, publication,
preservation on `main` and local deletion remain independent gates. Both original
router files remain intact.
