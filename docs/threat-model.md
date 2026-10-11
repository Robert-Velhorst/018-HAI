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
| Spoofing | Unauthenticated caller | `X-HAI-Backend-Key` (constant-time); signed JWT identity; rate limiting; production `doctor`/`readyz` fail and protected routes return 503 when either credential is unset or an example value |
| Tampering | Path traversal, bad input | `pathsafety`, `upload` validation, invariants, adversarial tests |
| Repudiation | No trail of actions | `auditevent` (redaction-aware) + immutable approval/audit records |
| Information disclosure | Secrets in logs | redaction helpers; support bundle excludes secret values |
| DoS | Request flooding, runaway jobs | per-IP rate limiter; retry budget + dead-letter; emergency stop |
| Elevation | Acting without authorization | approval gates, pre-action safety, RBAC model (enforcement follow-up) |

## Highest residual risks

1. **Unauthenticated API when the key is unset** — operational; surfaced by
   readiness tooling. Mitigate by never exposing the host without the key.
2. **RBAC not yet enforced in middleware** — safe single-operator default; wire
   `rbac.Can` before multi-user use.
3. **Path-safety not yet adopted at every call site** — utility exists and is
   tested; adoption tracked in the tech-debt register.

## Design principles upheld

Least privilege (paid/real connectors disabled by default), fail-safe defaults
(unknown mode → production strictness, unknown role → no grants), and defense in
depth (headers + auth + rate limit + validation).
