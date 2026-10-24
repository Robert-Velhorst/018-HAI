# Source API handler reconciliation

## Retained sources

Read the complete retained source handler and handler tests from the artifact
worktree, the complete differences in the model-maintenance variants, and their
complete differences against current code. This covers four physical source
variants, not every source-service, repository, connector or archive file.

Retain the current owner-scoped source controls, concealed foreign-resource
responses, manual sync queue/status contract, retry metadata, safe public errors,
and separate approval-bound destructive operations. Local audio/document runners
keep the configured source folder as their only scope and persist provenance
through the normal controlled import path. Browser input does not choose runner
files, models or parser settings.

Do not restore the older OAuth callback: the newer variant validates signed,
unexpired browser-bound state before clearing a pending cookie or handling a
denial. Keep current compact overview/health endpoints, repository-bounded
extraction pagination, exact total metadata, owner-filtered history and
request-context propagation to scheduled syncs. The original whole-inventory
extraction fetch must not replace the bounded repository path.

Correction submission remains distinct from patch completion: accepted work is
not described as already applied. Retain revision checks, idempotent intent,
owner-scoped status, unknown-persistence recovery and no automatic retry after
an uncertain write.

## Repeated-header correction

The handlers previously read only the first Idempotency-Key or If-Match value.
Repeated header fields, even with conflicting values, could therefore enqueue a
sync or save a correction under an arbitrarily selected binding.

Require one Idempotency-Key field for manual sync and correction submission and
reject repeated If-Match fields before correction persistence. Preserve the
existing missing-revision precondition response and single-field key/revision
validation. This does not redefine opaque key syntax or add alternate approval
authority. Rejecting repeated fields is not a claim to detect every form of
proxy-coalesced input or to implement HTTP's general multi-ETag semantics.

## Preservation and verification boundary

All six new repeated-header cases failed before repair by accepting the request
and creating persisted test work. After repair, the full uncached source package
passes, including the existing single-header manual-sync and correction paths.
Negative cases require HTTP 400, zero new queued jobs or correction intents and
an unchanged extraction. These assertions exercise handler-to-service behavior,
not only an isolated header parser.

The clean revision of this PR replaces a scanner-flagged synthetic idempotency
fixture with a deterministic repeated-character value of the same valid key
class. The identical/conflicting repeated-header cases and production behavior
remain unchanged. The original commit and complete patch are retained privately;
no scanner rule, allowlist or scan scope is relaxed.

Original worktrees and archives remain intact. Reviewed content and a public PR
are not a whole-worktree deletion gate. Local fake repositories and queue tests
are not real PostgreSQL, Trello, Google OAuth or runner acceptance.
