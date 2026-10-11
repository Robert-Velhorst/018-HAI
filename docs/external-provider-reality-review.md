# External Provider Reality Review

External integrations are operating boundaries, not placeholders presented as
working. This document distinguishes implemented code, retained bounded live
evidence, and the configuration or acceptance work still required for a newly
connected account.

## Providers In Scope

| Provider | Adapter status | Verified behavior | Evidence |
| --- | --- | --- | --- |
| Local LLM (Ollama) | Probeable; inference blocked by default | A loopback endpoint and `/api/tags`/`/v1/models` probe establish endpoint reachability and model presence only. Model Intelligence generation requires an exact operator attestation for local inference and unmetered billing; cloud model tags and gateways are not inferred to be local or free. | Unit and probe coverage; no configured live model acceptance run or independent proof of inference locality/billing. |
| OpenAI-compatible endpoint | Probeable, paid-gated | Model probe; paid use remains approval-gated. | Unit coverage; no configured live model acceptance run. |
| Gmail (Google OAuth) | `operational`, unconfigured by default | Read-only headers, a 512 KiB body, at most 20 attachment records and 1 MiB of extracted text attachments per message; encrypted grants; refresh; Gmail history-ID incremental sync; safe backfill when a history cursor expires; revoke; retained source links. Dedicated encryption and state-signing keys are mandatory. | Unit-tested and previously live-tested against a sandbox mailbox. `go test -tags live -run LiveGmail ./internal/googleoauth ./internal/source` provides credential-gated one-message client and source-projection checks; a fresh full-content/history acceptance run still remains a release gate. |
| Trello (read-only REST + signed webhook intake) | Implemented, unconfigured by default | Read-only board, card, comment, checklist, due-date, label, and attachment-metadata reconciliation. Initial card/action backfill is resumable. Later syncs read comment actions incrementally using a durable action cursor and bounded overlap, but still scan the complete paginated card inventory. Signed callbacks queue durable read-only reconciliation; comment creation/copy/edit/deletion is retained as source-linked evidence/tombstones without deleting prior evidence. Attachment bodies are not downloaded. The token's provider-reported member ID and read-only permissions are checked before board access. HAI has no Trello write path. Requires `TRELLO_API_KEY`, `TRELLO_READ_TOKEN`, `TRELLO_ACCOUNT_OWNER_IDENTITY`, and `TRELLO_ACCOUNT_MEMBER_ID`; webhook intake additionally requires `TRELLO_API_SECRET`, a public HTTPS `TRELLO_WEBHOOK_CALLBACK_URL`, the migration, and operator-side Trello webhook registration. | Unit-tested; no current live account acceptance evidence or registered webhook in this checkout. |
| Trello JSON export | `local_only` | Reads Trello JSON exports from an allowlisted local folder. This is distinct from the Trello REST adapter. | Unit-tested. |
| Google Drive | `operational`, unconfigured by default | Separate Drive read-only grant; bounded initial file inventory; native change-page cursor; Google Docs text and Sheets CSV export; bounded text-file reads; metadata-only binary records; non-destructive removal tombstones. | Unit and contract tested. A real sandbox-folder OAuth/backfill/change/revoke acceptance run is still required. |
| Google Contacts | `operational`, unconfigured by default | Separate Contacts read-only grant; bounded People API backfill; native sync token with bounded backfill recovery after token expiry; source-linked candidate records; non-destructive removal tombstones. There is no merge, delete, or provider write-back path. | Unit and contract tested. A real sandbox address-book OAuth/backfill/change/revoke acceptance run is still required. |
| Google Calendar | `operational`, unconfigured by default | Separate `calendar.readonly` grant; GET-only primary-calendar event listing; bounded one-year initial backfill; native sync token with bounded recovery after token expiry; source-linked event records; 14-day preparation proposals; 30-day overlap detection; cancellation and resolved-conflict retraction. There is no create, update, delete, invitation-response, or provider write-back path. The local ICS path remains available separately. | Unit and contract tested. A real sandbox-calendar OAuth/backfill/change/revoke acceptance run is still required. |
| GitHub (read-only) | `operational` | Bounded REST sync of repositories, issues, pull requests, commits, and workflow runs. Private or rate-limited access needs a least-privilege token. | Unit-tested. |
| Kafka-compatible event bus | Optional, disabled by default | Redpanda broker/topic integration and nginx configuration consumer behind `HAI_EVENT_BUS_ENABLED=true` plus the `event-bus` Compose profile. | Compose contract coverage; no current live event-delivery acceptance run. |

## Agent Runtime: DeepSeek Harness

HAI has a DeepSeek Harness architecture/runtime adapter, capability metadata,
and a Windows host-runtime bridge design. Its production task execution is
**hard-disabled**, not merely unconfigured: the adapter and host-runtime
service refuse dispatch, and the native bridge fails closed before gateway
polling or process launch. The configured bridge transport is plain HTTP on
loopback; its bearer token does not authenticate the server to the worker, so
local listener impersonation remains unresolved. Windows Job Objects provide
process-lifecycle containment, not OS-enforced least-privilege isolation for
files, network, registry, or credentials; plugin-launched WSL work is outside
that boundary.

Current evidence is source/contract-level only; it is not live DSH acceptance.
Even a process exit code of zero is recorded as
`process_succeeded_unverified` and does not establish verified completion or
update the automation's last-success state. Production execution must remain
closed until verified TLS peer identity/pinning, a real OS-enforced sandbox
with native Windows negative tests, and independent task-outcome verification
are available. No environment flag, approval, version pin, or bridge token is
an authorized workaround for these gates.

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

Gmail is unconfigured by default despite its retained bounded acceptance
evidence. Trello is unconfigured by default and requires account-specific
acceptance evidence. Any newly connected account must complete its own
consent/token, source-sync, audit, and revoke test before it is relied upon.
GitHub needs a chosen repository and, where necessary, a least-privilege token.
LLM provider and agent-runtime acceptance runs remain separate external gates.

## Verdict

HAI exposes Gmail, Drive, Contacts, Calendar, and Trello as implemented read-only connectors, not
as permanently connected accounts. Gmail has bounded prior live evidence;
Trello has no current account-specific live evidence in this checkout. Drive,
Contacts, and Calendar do not yet. The defaults fail safe: unconfigured connectors
and providers do not claim a live connection, and no source, model, or runtime
is treated as operational for a new account until that account has its own
verified run.
