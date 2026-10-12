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

## Verification aids

- `backend doctor` / `/readyz` warn when security-sensitive keys are unset.
- `internal/providerfallback` guarantees free/local is preferred and paid is
  never selected unless explicitly allowed.

## Trello read-only acceptance

HAI's Trello connector imports board cards, descriptions, comments, checklists,
attachment metadata, due dates, and labels. It has no Trello write path. Before
calling it live-proven, add the following non-placeholder values to untracked
`.env.local`:

```text
TRELLO_API_KEY=...
TRELLO_READ_TOKEN=...
TRELLO_LIVE_BOARD=<board ID or https://trello.com/b/... URL>
```

Then run from PowerShell:

```powershell
.\scripts\test-live-trello.ps1 -EnvFile .env.local
```

The runner mounts the backend source read-only in a disposable Go container and
runs only the connector's three GET-only live tests. It fails if credentials
are absent, placeholders, API access fails, any test is skipped, the cursor is
not incremental, or Trello reports write permission. It does not print the
credential values or change a Trello board.

## Sign-off

A provider is "verified" only when every box is checked and a probe has succeeded
in the target environment. Record the sign-off (who, when, scopes) alongside the
enablement change.
