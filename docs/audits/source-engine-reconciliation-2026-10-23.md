# Connected-source engine reconciliation

## Retained variants and method

Retained `backend/internal/source/service.go` variants of 225,462 and 219,156
bytes were compared with published commit
`b23388ddd53e9d3e9ffd7c030ef3996a2a98168a`. They are different snapshots, not
interchangeable byte duplicates.

Both retained variants have 222 declarations. The current file has 246. Go
AST/token accounting retains comments, literal content and statement order:
the model variant has 192 token-identical and 28 changed declarations; the
artifact variant has 189 token-identical and 31 changed declarations. The other
two retained entries are replaced import and error-variable groups. Every
retained function, import and error identifier remains present.

The complete model-to-current diff (567 added and 71 removed lines) and complete
artifact-to-model delta were read through their ends, with selected current
lifetime, extraction-lock, history, health-handler and transport context.
Token-identical bodies were not all manually reread; identity is not proof of
dependency behavior.

## Preserve current behavior

- Runtime composition attaches lifecycle ownership to source sync. Contextual
  scheduling rejects blank owners and passes caller cancellation through sync.
  Contextual semantic search checks cancellation before and after retrieval.
- Google health keeps paused/reconnect states distinct and requires the stored
  source, provider, credential material and exact read-only scope to match.
- Trello polling evidence remains separate from signed callback receipts and
  current registration/provider health. Webhook worker identities stay private,
  with receipt-bound validation, comment reconciliation and deletion restoration.
  Event-only updates do not pretend to complete a whole-board scan.
- Extraction update/archive paths reread owner/source/revision after acquiring
  source and extraction locks. Correction audit and memory evidence remain linked
  to the current saved extraction.
- LARO retains deliberately cleared opaque cursors rather than substituting a
  generic timestamp. Durable Trello commits keep their inventory binding and
  content-hash checks instead of restoring older timestamp/reindex behavior.
- Extraction history keeps owner scope explicit. Offset history and the legacy
  array endpoint have distinct response shapes; shared struct fields alone do
  not establish a paginated API contract.
- GitHub JSON decoding retains numbers without floating-point conversion and
  rejects trailing JSON. Counts must be nonnegative integers representable by
  the current platform; identifiers remain exact strings.
- HTTP transport keeps exact loopback opt-in, private/metadata/reserved-prefix
  checks, all-result DNS validation, pinned dial addresses, redirect policy and
  bounded connection resources. The old broad network bypass is not restored.
- Unsupported Autofever intake stays explicitly disabled rather than accepting
  invented upstream records as a working connector.

No production code was changed in this review; the newer behavior is already
present in the published engine.

## Verification and remaining scope

The full uncached source package passed at this published code, including the
separately documented Trello metadata repair. Local fixtures do not prove live
accounts, OAuth acceptance, provider completeness or PostgreSQL concurrency.
Some legacy compatibility paths still use `context.Background()`, and fallback
interfaces or repository calls need not observe cancellation. Universal
cancellation or authorization of every direct internal caller is not claimed.

Review, publication, preservation on `main` and deletion remain separate gates.
The selected ledger covers 606 of 618 files; other checkouts and transcript
archives remain open. Both original source files remain intact.
