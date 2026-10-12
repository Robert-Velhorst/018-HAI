# Privacy Impact Assessment (PIA)

018-HAI is a local-first Personal AI Operating System. This assessment records
what personal data it handles, where it lives, and the controls in place.

## Data categories

| Category | Examples | Where stored |
| --- | --- | --- |
| Context memories | User preferences, decisions, notes, contacts | Postgres (`context_memories`) |
| Connected-source content | Ingested local files / JSON feeds | Postgres + local paths (allowlisted) |
| Conversations | Imported chat/memory-engine content | Postgres (encrypted at rest via `HAI_MEMORY_ENCRYPTION_KEY`) |
| Operational logs | Redacted runtime/LLM output, audit events | Postgres + logs |

## Principles & controls

- **Local-first:** data stays in the operator's Postgres; no third-party
  analytics service. Usage analytics are aggregated in-process
  (`internal/analytics`).
- **Minimization:** provider probes are blocked for paid providers until
  explicit approval; real account connectors stay disabled until OAuth scopes
  are minimal and reviewed.
- **Secret hygiene:** shared redaction helpers strip passwords, tokens, API
  keys, authorization headers, and private-key patterns from logs, runtime
  output, and error bodies before storage or return.
- **Encryption:** memory-engine content is encrypted with a dedicated key; if
  unset, `backend doctor` and `/readyz` fail in production; non-production
  modes retain an explicit warning for local development.
- **Right to deletion/export:** memories support archive, delete, and export;
  retention thresholds are defined in `internal/retention`.
- **Provenance:** file provenance display avoids exposing unnecessary local
  absolute paths.

## Retention

Governed by `internal/retention.Policy` (default: archive inactive memories after
180 days, delete archived after a further 365). See `docs/troubleshooting.md`
and the data-retention policy for operator actions.

## Residual risks

- Empty `HAI_MEMORY_ENCRYPTION_KEY` falls back to the backend key (weaker
  isolation) — surfaced as a warning by `doctor`/`/readyz`.
- Ingested local files may contain third-party personal data; operators are
  responsible for the lawful basis of ingestion.
