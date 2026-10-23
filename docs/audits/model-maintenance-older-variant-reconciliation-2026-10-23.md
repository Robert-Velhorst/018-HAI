# Older model maintenance variant reconciliation

## Review method and decision

Compared the retained artifact-audit maintenance implementation and scheduler
tests against the previously reviewed model-maintenance variant. Read both
complete inter-variant deltas, then the complete model-variant-to-current deltas
for these two paths, ignoring line-ending-only noise. This is a semantic delta
reconciliation using the existing baseline review, not a claim that every HAI
file or every retained archive has been reviewed.

Do not restore the older implementation over the current code. It lacks newer
context propagation, owner/model-scoped persistence-failure cooldowns, durable
Ollama refresh admission, post-failure artifact inspection and blocking for
unverifiable operator-managed versions. Keep those newer behaviors and their
tests rather than reviving the older non-blocking expectations.

Current code also distinguishes a daily read-only provider catalog check from
an update of a hosted model artifact. Catalog omission does not by itself prove
that a configured model is unavailable. Odysseus health-only evidence is not
model-catalog or model-version evidence. Keep the separate approval and budget
gates, distributed lease, cached-result recheck after acquisition, completion-time
freshness, unsupported-local-version rejection and fail-closed scheduler safety
callback. A fixture is not real-account acceptance.

## Correction found during reconciliation

The endpoint-identity parser allowed standalone dot segments in short mount
prefixes. The existing negative `/api/../secret` case rejected a three-segment
path, but did not cover shorter `/.`, `/..`, `/api/.`, `/api/..`, `/./v1` and
`/../v1` inputs. These are path-navigation forms, not ordinary API mounts.

Reject a segment equal to `.` or `..` before accepting a mount identity. Preserve
legitimate dotted names, exact mount-prefix identity, IPv6/default-port handling
and trailing compatibility-version normalization. Do not normalize a rejected
path into a different authorized endpoint.

## Preservation boundary

All six new negative subcases failed before the correction. After it, the
uncached `go test -p 1 -timeout 180s -count=1 ./internal/llm` package run passed,
including positive controls for ordinary dotted mount names. These are local
tests and HTTP fixtures, not real-provider or PostgreSQL acceptance.

Keep original historical sources. A newer semantic replacement is not a
byte-for-byte preservation record and does not establish whole-worktree deletion
eligibility. No provider was contacted, model downloaded or runtime installed as
part of this reconciliation.
