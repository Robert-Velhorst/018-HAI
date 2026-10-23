# Manual source sync reconciliation

## Reviewed scope and retained behavior

Reviewed the manual sync implementation and test file from each retained audit
worktree, both provider-error test variants and the retained artifact
provider-error implementation: seven source versions. The manual implementation variants have the same text;
the older artifact test uses less specific credential-recovery wording. The
current test already retains both credential and reconnect guidance.

Keep these existing behaviors rather than replacing current files wholesale:

- Owner-bound idempotency using the caller's normalized request, independent
  of later changes to the connected source's default project.
- Readiness fencing through durable acceptance, and the newer independent
  manual-only worker readiness option.
- Returning an existing accepted job during an idempotent retry even if the
  worker becomes unavailable or the source is subsequently paused; changed
  request content still conflicts and new work still requires readiness.
- Captured project assignment for queued local imports and explicit Trello
  overrides without overwriting a newer source default.
- Queue/source/job identity checks before replay shortcuts or execution.
- The newer busy-source/board deferral and paused-Trello deferral instead of
  turning temporary contention into a terminal provider failure.
- Durable retry-aware public status, bounded counts and redacted messages,
  owner-scoped HTTP status, no-store responses and private payload omission.
- Cursor preservation and non-retryable credential/reconnect guidance after
  a permanent provider failure, without exposing the provider response body.

Retain the existing typed provider error classification, original error causes,
separate internal upstream status, provider-specific rate-limit classification,
bounded seconds/HTTP-date retry metadata, and manual review when a valid provider
delay exceeds the automatic retry limit. Do not clamp a longer delay into an
earlier automatic retry. The old provider tests match current behavior; current
HTTP fixtures additionally pin the exact local server address rather than
weakening the network policy. No old behavior needs restoring.

## Additional regression coverage

The terminal replay guard is already implemented but was not directly covered
by the retained tests. A four-case test covers completed and cancelled history
with the correct and a mismatched durable job identity. Correct replays return
without reading the source or writing history; mismatched identities remain
rejected even for terminal history. Preserved terminal counters, cursor values,
completion time and message remain unchanged. No production behavior is changed
by this additional test.

The full source and durable-job Go packages passed locally with uncached tests
on Windows and Go 1.27.3. Tests use synthetic repositories and local HTTP
fixtures, not Robert's accounts.
They do not prove live Trello acceptance, database concurrency or a running
background worker. Original sources and archive data remain intact; a private
fingerprint review receipt is not itself main-preservation or deletion proof.
