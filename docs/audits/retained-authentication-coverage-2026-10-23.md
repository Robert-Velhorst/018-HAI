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
