# Red-Team Loop 1 — Authentication, Authorization & Network Exposure

Adversarial review focused on "who can reach what." Findings feed
`docs/bug-hunt-log.md`.

## Surface reviewed

- Backend API-key middleware (`X-HAI-Backend-Key`, constant-time compare).
- CORS for local capture (extension/localhost origins).
- New rate limiting, security headers, idempotency.
- RBAC model (`internal/rbac`).

## Findings

| ID | Severity | Finding | Mitigation / status |
| --- | --- | --- | --- |
| RT1-1 | High (config) | When `BACKEND_API_SHARED_KEY` is empty the API was fully unauthenticated. | Remediated: production `doctor` and `/readyz` fail, and the protected backend API returns 503 until a key is configured. Explicit demo/test modes retain the local no-key workflow. |
| RT1-2 | Medium | Constant-time key compare is correct, but a wrong key returns 401 with no lockout. | Acceptable with the new per-IP rate limiter (`RATE_LIMIT_PER_MINUTE`) to blunt brute force; enable it on any non-loopback deployment. |
| RT1-3 | Medium | RBAC model exists but is not yet enforced in middleware. | Tracked: wire `rbac.Can` into route groups before multi-user use. Model unknown-role-grants-nothing default is safe. |
| RT1-4 | Low | CORS allows any `chrome-extension://`/`localhost:*` origin. | Appropriate for the local-capture extension; revisit if a hosted deployment is introduced. |

## Attempted attacks & result

- **Unauthenticated call with key set** → 401 (blocked).
- **Brute-force key guessing** → blunted by rate limiter when enabled.
- **Clickjacking** → blocked by `X-Frame-Options: DENY` + CSP `frame-ancestors 'none'`.
- **Duplicate mutation replay** → blocked when an `Idempotency-Key` is supplied.

## Verdict

No auth bypass found. The dominant risk is *operational*: running with an empty
API key on an exposed network. That is now visible via readiness tooling.
