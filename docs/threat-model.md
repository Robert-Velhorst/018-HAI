# Threat Model & Security Design Review

Scope: the 018-HAI backend, gateway, and local data. Method: STRIDE-lite over the
critical path (dashboard → source → task → LLM routing → approval → execution →
verification → audit).

## Assets

- User memories, connected-source content, conversations (some encrypted).
- Provider credentials / API keys.
- The automation execution capability (can act on the user's behalf).

## Trust boundaries

1. Browser/extension → gateway (CORS-restricted, API-key auth).
2. Gateway → backend (shared key).
3. Backend → local providers / connectors (allowlisted, approval-gated).

## Threats & mitigations

| STRIDE | Threat | Mitigation |
| --- | --- | --- |
| Spoofing | Unauthenticated caller | `X-HAI-Backend-Key` (constant-time); rate limiting; `doctor`/`readyz` warn when unset |
| Tampering | Path traversal, bad input | `pathsafety`, `upload` validation, invariants, adversarial tests |
| Repudiation | No trail of actions | `auditevent` (redaction-aware) + immutable approval/audit records |
| Information disclosure | Secrets in logs | redaction helpers; support bundle excludes secret values |
| DoS | Request flooding, runaway jobs | per-IP rate limiter; retry budget + dead-letter; emergency stop |
| Elevation | Acting without authorization | approval gates, pre-action safety, signed IDP roles, verified-claim identity middleware, and route-specific RBAC guards |

## Highest residual risks

1. **Unauthenticated API when the key is unset** — operational; surfaced by
   readiness tooling. Mitigate by never exposing the host without the key.
2. **Permission-regression drift** — new routes must declare their read, write,
   approve, execute, or admin requirement and add a role-boundary test.
3. **Path-safety not yet adopted at every call site** — utility exists and is
   tested; adoption tracked in the tech-debt register.

## Design principles upheld

Least privilege (paid/real connectors disabled by default), fail-safe defaults
(unknown mode → production strictness, unknown role → no grants), and defense in
depth (headers + auth + rate limit + validation).
