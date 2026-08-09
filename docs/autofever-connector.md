# Autofever connected source

HAI can ingest Autofever's reviewed vehicle records through the read-only
`autofever` connected-source adapter. The adapter calls only
`GET /api/integrations/hai/feed`; it does not search, shortlist, edit, or delete
Autofever records.

## Configuration

1. In the intended Autofever account, create a HAI connector token. Autofever
   displays the token once; keep it secret and revoke it in Autofever if it is
   exposed.
2. In HAI's private environment, set `HAI_AUTOFEVER_CONNECTOR_TOKEN` to that
   token and `HAI_AUTOFEVER_CONNECTOR_OWNER_IDENTITY` to the exact authenticated
   HAI owner identity that may use it.
3. Add only the Autofever host to `CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS`.
   Remote hosts must use HTTPS. Plain HTTP is accepted only for loopback and
   `host.docker.internal` development targets.
4. Restart the HAI backend. In **Connected Sources**, select **Autofever reviewed
   vehicles**, use category `vehicle_intelligence`, and set the source target to
   the absolute Autofever feed URL ending in `/api/integrations/hai/feed`.
5. Confirm the connector is configured for the intended owner and run a manual
   sync before enabling a schedule.

The token is passed in the Authorization header, never in the URL. HAI refuses
redirects, query strings, URL credentials, non-canonical feed paths, unapproved
hosts, non-HTTPS remote URLs, oversized responses, and oversized or stalled
cursors. Requests inherit the sync cancellation context and configured timeout.
The shared server token is usable only by the one HAI identity named in
`HAI_AUTOFEVER_CONNECTOR_OWNER_IDENTITY`; other HAI accounts cannot create,
update, or sync an Autofever source with it.

Autofever returns only that token owner's reviewed `shortlisted` and
`needs_correction` listings. HAI stores them as source-linked records under the
configured project and preserves the provider cursor. The source is remote,
read-only, and has no write-back capability.

## Verification boundary

HAI's local tests cover owner binding, allowlist and transport checks, bearer
authentication, cursor progression, request cancellation, bounded response
handling, and connector status. They do not prove that a particular Autofever
instance is reachable, that the configured token is valid, or that the live
owner's records sync correctly. Verify those separately with the intended
Autofever deployment and account before enabling scheduled sync.
