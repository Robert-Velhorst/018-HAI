# Historical preview reconciliation

## Scope and provenance

Six successful indexed patch records across three historical paths were read
from a retained transcript archive using bounded positional reads. Every
selected content/diff payload was verified against its recorded byte length and
SHA-256 fingerprint. Source size and modification time remained stable during
each read. This did not rehash the whole archive or review its remaining events.

The selected index records pair each addition with a later deletion containing
the same file contents:

- `frontend/proxy.preview.json` redirected `/api` to a local mock on port 4400.
- `frontend/mock-preview-server.js` returned hardcoded dashboard counts, example
  records and a simulated authentication response.
- `.codex-preview-server.mjs` returned hardcoded authentication, dashboard and
  runtime health/readiness responses, proxying other requests to a local UI.

These selected records do not establish the complete transcript chronology:
other events or repeated applications could exist. They do establish that these
particular payloads were preview fixtures, not live connector or runtime proof.
No private archive, credentials, transcript identifiers, or raw messages are
published with this report.

## Product decision and integration

Do not restore these scripts as operational HAI servers. Their static counts and
fabricated readiness would violate HAI's distinction between fixture behavior
and real account/runtime observations. All three historical paths are absent
from the inspected current product tree.

Angular's current serve entry point uses `frontend/proxy.conf.cjs`, whose API
target must be a loopback HTTP(S) origin without user credentials, a path, query
or fragment. The existing production frontend image serves the compiled app
through its Nginx configuration rather than these historical mock servers.

The existing proxy regression suite now checks Angular's base serve settings,
each configuration override, build configuration and package command references
for accidental reconnection of these three historical preview filenames. Its
negative controls mutate the configuration in memory and verify rejection of
mock default proxies, production overrides, injected scripts and mock start
commands. Child Node processes in this suite disable compilation-cache writes;
no global environment or user setting changes.

All five proxy tests passed locally with zero skipped tests. This is a static
configuration and subprocess-loading contract, not proof that a local server,
authentication provider or external account is currently available. It does not
detect every possible simulation under arbitrary new filenames.

## Preservation boundary

The useful integration is the explicit fixture exclusion and regression guard,
not copying simulated behavior into the product. No archive deletion is
authorized by this six-record review. Remaining source files, other events,
oversized/malformed records, execution outcomes and archive-wide preservation
still require reconciliation with the actual current product and `main`.

The existing `scripts/audit-hai-transcript-patch-paths.ps1` path inventory now
always emits `content_preservation_verified=false` and `deletion_eligible=false`,
with an explicit coverage limitation. Its record-scan completion flag remains
independent: parsing every record does not prove integration of those records.
The existing fixture test was extended to check this for both an oversized-record
scan and a complete record scan. It failed before those fields were added and
passed afterward; source fixtures remain intact. The Windows CI test sequence
now includes this regression. No real archive was deleted or declared safe by
these fixture results.
