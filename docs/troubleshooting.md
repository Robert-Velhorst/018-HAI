# Troubleshooting Guide & Error Catalog

This guide pairs the machine-readable API error codes (`internal/apierror`) with
likely causes and operator actions. Every API error is returned in a stable
envelope:

```json
{ "error": { "code": "validation_failed", "message": "content is required", "details": { "content": "must not be empty" } } }
```

## Error catalog

| Code | HTTP | Likely cause | Action |
| --- | --- | --- | --- |
| `bad_request` | 400 | Malformed JSON or missing/invalid query params | Fix the request body/params; check the endpoint's schema in Swagger |
| `unauthorized` | 401 | Missing/incorrect `X-HAI-Backend-Key` | Set `BACKEND_API_SHARED_KEY` and send it in the header |
| `forbidden` | 403 | Origin not allowed by the local-capture CORS policy | Call from an allowed extension/localhost origin |
| `not_found` | 404 | Resource ID does not exist or was deleted | Verify the ID; it may have been archived/removed |
| `conflict` | 409 | Duplicate `Idempotency-Key`, or a state conflict | Use a new key for a genuinely new operation |
| `validation_failed` | 422 | A data invariant was broken (see `internal/invariants`) | Read `details` for the offending field(s) and correct them |
| `rate_limited` | 429 | Per-IP rate limit exceeded (`RATE_LIMIT_PER_MINUTE`) | Back off and retry after `Retry-After`; raise the limit if legitimate |
| `unavailable` | 503 | Not ready — a required configuration check failed | Run `backend doctor` or inspect authenticated `GET /api/v1/system/readiness` and fix failing checks |
| `internal_error` | 500 | Unhandled server error | Check server logs; capture a support bundle |

## First diagnostics

1. **Liveness:** `GET /healthz` → `{"status":"ok"}` means the process is up.
2. **Readiness:** `GET /readyz` returns aggregate 200 ready/degraded or 503 not-ready state; signed-in operators use `GET /api/v1/system/readiness` for individual checks.
3. **Config self-check:** run `backend doctor` for a full readiness report
   (`ok`/`warn`/`fail` per setting) with a non-zero exit code on any failure.

## Common situations

- **`/api/v1/system/readiness` reports `database.*` failures** — the DB env
  (`DB_HOST`/`DB_PORT`/`DB_NAME`/`DB_USER`) is incomplete. Fix and restart.
- **API returns 401 everywhere** — `BACKEND_API_SHARED_KEY` is set on the server
  but the client is not sending `X-HAI-Backend-Key`.
- **Repeated 409 on a create** — an `Idempotency-Key` is being reused; generate a
  fresh key per distinct operation.
- **Sporadic 429** — rate limiting is enabled; inspect `RATE_LIMIT_PER_MINUTE`.
