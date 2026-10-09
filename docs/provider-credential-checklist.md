# Provider Credential Verification Checklist

Before enabling any real external provider, verify each item. Until all pass, the
provider stays disabled (the default).

## Per-provider checklist

- [ ] **Credential present** — required API key / OAuth token is configured via
  env/secret store (never committed to VCS).
- [ ] **Scopes minimal** — OAuth scopes are the least needed; scope diff reviewed
  and approved.
- [ ] **Live probe passes** — provider probe reaches the endpoint (Ollama
  `/api/tags`, OpenAI-compatible `/v1/models`) without following redirects.
- [ ] **Cost posture** — paid usage stays impossible until explicit approval;
  `daily_paid_budget_eur` remains 0 for free-only operation.
- [ ] **Rate/quota respected** — provider quota + budget ledger configured.
- [ ] **Failure handling** — retries/backoff + dead-letter cover transient
  failures (`internal/backoff`, `internal/worker`); simulate with
  `internal/fakeprovider` first.
- [ ] **Redaction** — provider error bodies and outputs are redacted before
  logging/returning.
- [ ] **Rotation** — a rotation schedule is set for the credential
  (`internal/secretrotation`).
- [ ] **Audit** — actions against the provider emit audit events
  (`internal/auditevent`).

For Gmail's bounded read-only client acceptance, use a dedicated sandbox OAuth
client and mailbox, set `GMAIL_LIVE_CLIENT_ID`, `GMAIL_LIVE_CLIENT_SECRET`,
`GMAIL_LIVE_REFRESH_TOKEN`, and a non-sensitive test message ID in
`GMAIL_LIVE_EXPECT_MESSAGE_ID`, then run:

```powershell
cd backend
go test -tags live -run LiveGmail -v ./internal/googleoauth ./internal/source
```

The client test refreshes the credential and reads one selected message. The
source test projects that same bounded message into HAI's source-import shape,
checking its stable external identity, project provenance, source link, and
content envelope. Neither test prints message content, headers, addresses,
subjects, or token values. Treat a skipped test as no live acceptance evidence.

## Verification aids

- A deployment-level `GITHUB_SOURCE_TOKEN` is attached only when the exact
  connected-source owner matches `GITHUB_SOURCE_TOKEN_OWNER_IDENTITY`. Without
  that match HAI makes the GitHub request without the token, so public
  repositories remain available but private repositories fail closed.

- `backend doctor` / `/readyz` fail when required security-sensitive keys are
  unset in production; non-production modes show an explicit warning.
- `internal/providerfallback` guarantees free/local is preferred and paid is
  never selected unless explicitly allowed.
- Trello sync requires `TRELLO_API_KEY`, `TRELLO_READ_TOKEN`,
  `TRELLO_ACCOUNT_OWNER_IDENTITY`, and `TRELLO_ACCOUNT_MEMBER_ID`. The HAI
  identity must exactly match the authenticated IDP subject authorized to use
  the deployment-level token, and the token's provider-reported `idMember` must
  match the configured Trello member ID. Other HAI owners cannot connect or
  sync with that shared credential. Board requests send credentials in
  Trello's OAuth `Authorization` header, not query parameters. The permission
  verification endpoint requires the token as its path resource identifier;
  logs and errors must continue to redact that path.
- Signed Trello webhook intake additionally requires the Trello application
  secret in `TRELLO_API_SECRET`, an exact public HTTPS callback in
  `TRELLO_WEBHOOK_CALLBACK_URL`, applied database migrations, and an operator-
  registered webhook. The application secret is not the API key or read token.
- Trello's 1,000-result API limit is a per-page maximum, not a board-size limit.
  HAI paginates bounded card and action requests, validates strict descending
  action-ID boundaries, and refuses to advance the cursor when a page is
  incomplete. Initial card and comment-action backfill is resumable; later syncs
  read comment actions incrementally from a durable action cursor with a bounded
  overlap, while still scanning the complete paginated card inventory for state
  reconciliation. A card changed during a scan is picked up by a later scan.
  Trello exposes comment edits/deletions as `updateComment` and `deleteComment`
  webhook actions. When an operator-registered webhook delivers one, HAI refreshes
  current comment state for the affected card; missing or delayed webhook delivery
  can leave that state stale until the next successful source sync. Attachment
  bodies are not downloaded. Fixed safety-budget failures require
  review rather than repeating the same incomplete run. Card targets are stored
  as canonical board IDs and cannot be redirected after connection; create a
  separate source to preserve board provenance and cursor history. HAI never
  writes to Trello.

## Sign-off

A provider is "verified" only when every box is checked and a probe has succeeded
in the target environment. Record the sign-off (who, when, scopes) alongside the
enablement change.
