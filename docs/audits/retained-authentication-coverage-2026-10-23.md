# Retained authentication coverage reconciliation

## Scope and decisions

The retained authentication service, interface, two route guards, and service
tests were read and compared with the current implementation. The current
implementation is retained: empty successful login responses are valid, session
checks use the shared bounded read deadline, logout uses POST, and an unavailable
identity service remains distinguishable from an unauthenticated session.

The older suite contained useful coverage that was missing from the current
service suite. This change restores explicit non-persistent login, capabilities
timeout/cancellation, and reset-bearer body placement checks. The old hanging
session check is adapted to the current contract: a timeout propagates an error
for the guard to present as service unavailability, rather than silently returning
false. The existing test accepting a valid response after 2.5 seconds remains.

Additional controls cover a 401 returning false, a 503 propagating an error, and
POST-only logout. No runtime authentication behavior or permission is changed.

Three further retained test files were reviewed in full against their current
diffs. Their useful behavior is already represented:

- `backend/internal/llm/effect_authorization_test.go` retains trusted effect
  provenance and distinct server-derived task/effect identity per maintenance
  attempt. A newer test-only maintenance-disable helper remains.
- `backend/internal/verification/trello_review_test.go` retains inspectable but
  review-only Trello evidence, refusal to promote it into memory, and independent
  promotion of unrelated valid evidence. Current nonzero fixture IDs remain.
- `backend/internal/agentguidance/guidance_test.go` retains catalog provenance,
  consent, forged/stale/duplicate rejection, bounded content, and restricted
  runner payloads. The current oversized-selection rejection test replaces an
  inappropriate restriction on total catalog size; per-request limits remain.

## Module wiring follow-up

Eight retained Angular module definitions were reviewed in full: the root app,
Control Center, background operations, runtime control, command dashboard,
connected sources, memory, and home. The retained app and Control Center variants
from a second worktree were also read. Their routes, providers, forms, artifact
components, and shared control-room imports are preserved by the current code.
Current additions for alerts, confirmations, runtime inputs, and memory pagination
remain. Root icons now use the explicit central registry, including every icon in
the retained app module, rather than restoring the shorter old registry.

Home no longer owns a drawer: operational navigation belongs to the shared shell.
The old unused drawer import is not restored. One duplicate `ControlRoomModule`
entry in the current command-dashboard imports is removed; its remaining import
and memory-service provider stay intact. This is metadata deduplication, not an
assertion that Angular instantiated that module twice at runtime.

Both retained root-routing variants were read and compared. Every old lazy route
and authentication guard remains. Keep the newer operational-brain and Hire.AI
routes, authenticated theme guard, exported route definitions, and guarded
not-found page instead of restoring the old wildcard redirect.

## Verification boundary

The three relevant Go packages passed locally:

```text
go test -C backend -p 1 -timeout 180s ./internal/agentguidance ./internal/llm ./internal/verification
```

Frontend verification passed all 26 Angular HTTP-fixture tests in Chrome Headless
for the auth service, both route guards, and safe return URLs. Seven service
checks were added; the current session-delay positive control remains. These fixtures do not establish
live login, email delivery, Google OAuth configuration, or deployed product
acceptance. Remote CI and preservation on `main` are separate gates. Retained
source copies are not deleted by this reconciliation.

The production build and progressive-section, split-theme, and icon-registry
contracts passed after module-import deduplication. The initial bundle is
781.29 kB. Four existing component-SCSS budget warnings remain for the shell,
workflow engine, task blueprint, and pursuits; this change does not resolve them.
The 12 focused app-routing and Home component tests also passed in Chrome
Headless; they are not authenticated live-route or external-service acceptance.
