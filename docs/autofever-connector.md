# Autofever integration status

HAI does not currently have a working Autofever integration. The current
`Robert-Velhorst/028-autofever` repository describes an incomplete
illness-notification application/prototype. Its checked-in API does not expose
`/api/integrations/hai/feed`, vehicle-review records, or the owner-scoped
read-only contract previously claimed by HAI.

HAI therefore keeps the legacy `autofever` connector visible only as a disabled
`not_implemented` catalog entry. New sources cannot be created, existing source
records are preserved but cannot be synced, and no Autofever credentials are
requested or forwarded.

## Prerequisite to implementation

Before enabling this integration, the sibling application needs a reviewed API
contract that defines authenticated owner scoping, vehicle-review fields and
provenance, pagination/cursor semantics, deletion or withdrawal behavior,
rate limits, and revocation. HAI then needs a matching adapter and tests against
that contract, followed by live acceptance against an authorized test account.
Until those prerequisites exist, do not configure a token or schedule.
