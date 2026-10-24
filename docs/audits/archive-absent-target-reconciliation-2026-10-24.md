# Absent archive targets reconciliation: 24 October 2026

## Scope and integrity

The literal main comparison labels 24 successful patch variants `target_absent`.
They concern six paths, not 24 missing product modules. All 24 indexed events
were read within their recorded bounds; operation, success flag and content/diff
fingerprints were reverified. No archived instruction, server or configuration
was executed, and no private configuration values were printed or published.

| Path | Variants | Disposition |
| --- | ---: | --- |
| `nginx-config/nginx.conf` | 19 | Generated secret-bearing configuration; preserve intent in the tracked template and renderer, never publish the generated file. |
| `frontend/proxy.preview.json` | 1 | Historical preview-only proxy, with an exact-content paired deletion in the frozen index. |
| `frontend/mock-preview-server.js` | 1 | Historical simulated API server, with an exact-content paired deletion. Do not restore as a real backend. |
| `.codex-preview-server.mjs` | 1 | Historical static/proxy preview with simulated API responses, with an exact-content paired deletion. Do not restore simulated authentication/runtime status. |
| `.git/info/exclude` | 1 | Local Git exclusion metadata, not a missing product file. Do not place Git internals in the source tree. |
| `.env.local` | 1 | Private operator configuration. Do not publish, overwrite or delete from this review. |

All three preview paths remain absent in the integration checkout. Their earlier
additions and deletions carry the same archived content hashes. Their absence is
not evidence of lost backend code. The JavaScript server texts were inspected
structurally with the TypeScript parser without execution. This does not establish
that their old UI examples or mock data represent live product capabilities.

## Generated gateway intent

The nineteen configuration variants cover module routes, authenticated forwarding,
backend-key injection, the OpenClaw upload envelope, upstream resolution and
health/readiness routes. Some later changes revise route matching. The current
tracked template has an authenticated backend catch-all instead of requiring
one generated location for every new module, explicit IDP handling, bounded
readiness forwarding, and the protected upload route. Dynamic Docker-service
upstreams replace fragile container-address assumptions.

`docker-compose.local.yml` mounts the template read-only and renders only the
declared backend/preview secret variables with restrictive permissions. The
generated `nginx.conf` is intentionally ignored and must not be restored from
old runtime patches. Operator-specific values and old generated upstream choices
were not copied into the tracked template.

Verification: all 24 existing native `test_gateway_contract.py` tests passed on
Python 3.11. They inspect source and Git tracking/ignore contracts. No new Docker
container, Nginx daemon, provider request or live gateway acceptance was performed.
The source contracts do not prove deployed routing or every backend endpoint.

## Private Trello configuration boundary

Only environment key names were inspected from the private file:
`TRELLO_API_KEY` and `TRELLO_API_SECRET`. Their blank example slots and consumers
already exist in tracked source. The secret is used for webhook signing; it is
not a substitute for the read token and account-owner/member binding required
by the current read-only polling adapter.

This does not establish that the archived credentials are valid, current, safely
scoped or sufficient for live Trello access. No value was printed, installed,
validated with a provider, rotated or published. The private file/archive remains
protected pending an appropriate private-data preservation decision.

## Cleanup boundary

This resolves why these six paths are absent from current tracked product source;
it does not mark the full archive corpus integrated. No original archive, source
checkout, operator secret or database was deleted. Publication, exact-head CI,
main preservation and deletion remain separate gates.
