# External Provider Reality Review

External integrations are operating boundaries, not placeholders presented as
working. This document distinguishes implemented code, retained bounded live
evidence, and the configuration or acceptance work still required for a newly
connected account.

## Providers In Scope

| Provider | Adapter status | Verified behavior | Evidence |
| --- | --- | --- | --- |
| Local LLM (Ollama) | Probeable | Local/free model probe and fallback preference. | Unit and probe coverage; no configured live model acceptance run. |
| OpenAI-compatible endpoint | Probeable, paid-gated | Model probe; paid use remains approval-gated. | Unit coverage; no configured live model acceptance run. |
| Gmail (Google OAuth) | `operational`, unconfigured by default | Read-only headers, bounded bodies and textual attachments; encrypted grants; refresh; Gmail history-ID incremental sync; safe backfill when a history cursor expires; revoke; retained source links. Dedicated encryption and state-signing keys are mandatory. | Unit-tested and previously live-tested against a sandbox mailbox. The newer full-content/history path still needs a fresh live acceptance run. |
| Trello (read-only REST) | `operational`, unconfigured by default | Read-only board card, list, due-date, and label sync with incremental cursor and card provenance. Every request is a GET; there is no write path. Reports `not_implemented` until a board id and least-privilege `TRELLO_API_KEY` and `TRELLO_READ_TOKEN` are set. | Unit-tested and live-tested against a real board on 2026-07-23. |
| Trello JSON export | `local_only` | Reads Trello JSON exports from an allowlisted local folder. This is distinct from the Trello REST adapter. | Unit-tested. |
| Google Drive | `operational`, unconfigured by default | Separate Drive read-only grant; bounded initial file inventory; native change-page cursor; Google Docs text and Sheets CSV export; bounded text-file reads; metadata-only binary records; non-destructive removal tombstones. | Unit and contract tested. A real sandbox-folder OAuth/backfill/change/revoke acceptance run is still required. |
| Google Contacts | `operational`, unconfigured by default | Separate Contacts read-only grant; bounded People API backfill; native sync token with bounded backfill recovery after token expiry; source-linked candidate records; non-destructive removal tombstones. There is no merge, delete, or provider write-back path. | Unit and contract tested. A real sandbox address-book OAuth/backfill/change/revoke acceptance run is still required. |
| Google Calendar | `operational`, unconfigured by default | Separate `calendar.readonly` grant; GET-only primary-calendar event listing; bounded one-year initial backfill; native sync token with bounded recovery after token expiry; source-linked event records; 14-day preparation proposals; 30-day overlap detection; cancellation and resolved-conflict retraction. There is no create, update, delete, invitation-response, or provider write-back path. The local ICS path remains available separately. | Unit and contract tested. A real sandbox-calendar OAuth/backfill/change/revoke acceptance run is still required. |
| GitHub (read-only) | `operational` | Bounded REST sync of repositories, issues, pull requests, commits, and workflow runs. Private or rate-limited access needs a least-privilege token. | Unit-tested. |
| Kafka-compatible event bus | Optional, disabled by default | Redpanda broker/topic integration and nginx configuration consumer behind `HAI_EVENT_BUS_ENABLED=true` plus the `event-bus` Compose profile. | Compose contract coverage; no current live event-delivery acceptance run. |

## Evidence Levels

- **Implemented**: code, persistence, API contract, and focused automated
  coverage exist in this repository.
- **Live-tested**: a real credential/account completed a bounded approved
  end-to-end run with audit and verification evidence. This proves that exact
  scenario, not any future account or configuration.
- **Live-proven for current use**: the currently configured account, model, or
  runtime has its own bounded retained acceptance evidence. No LLM provider or
  agent runtime currently meets this bar.

The detailed Gmail and Trello evidence is retained in
`docs/completion-matrix.md`.

## Safety Controls

- Gmail requests only `gmail.readonly`; Drive requests only `drive.readonly`;
  Contacts requests only `contacts.readonly`; Calendar requests only
  `calendar.readonly`. Gmail reads bounded bodies/text
  attachments, Drive exports bounded text, and Contacts emits review-required
  candidates without provider write-back. Trello
  issues only HTTP GET requests and expects a read-scoped token. Neither has a
  remote mutation path.
- Credentials remain environment-managed. Source rows do not persist API keys,
  tokens, or board ids. Google OAuth tokens are encrypted at rest.
- Remote fetches use the shared allowlist, blocked-address guard, timeout, and
  response-size cap. Trello's API host is explicitly allowlisted.
- The local-first provider policy never selects a paid model without explicit
  policy approval. A failed probe or sync keeps its failure/audit record rather
  than reporting simulated success.

## Deferred And Remaining Work

Google Calendar's primary-calendar read adapter is implemented but remains
unconfigured by default and lacks a retained live sandbox acceptance run. Local
ICS import remains available as a separate path. All source adapters are
read-only: provider write-back is not implemented.

Gmail and Trello are unconfigured by default despite their retained bounded
acceptance evidence. Any newly connected account must complete its own
consent/token, source-sync, audit, and revoke test before it is relied upon.
GitHub needs a chosen repository and, where necessary, a least-privilege token.
LLM provider and agent-runtime acceptance runs remain separate external gates.

## Verdict

HAI exposes Gmail, Drive, Contacts, Calendar, and Trello as implemented read-only connectors, not
as permanently connected accounts. Gmail and Trello have bounded prior live
evidence; Drive, Contacts, and Calendar do not yet. The defaults fail safe: unconfigured connectors
and providers do not claim a live connection, and no source, model, or runtime
is treated as operational for a new account until that account has its own
verified run.
