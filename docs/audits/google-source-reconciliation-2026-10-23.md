# Google source reconciliation and history response validation

## Scope and source decisions

Reviewed eight retained source versions: `gmail.go`, `gmail_test.go`, `oauth.go`,
and `oauth_test.go` in each of the model-maintenance and artifact worktrees.
The two retained Gmail test versions have identical text after newline
normalization; other version differences were compared explicitly.

| Area | Decision |
| --- | --- |
| Consent | Keep supported single-product read-only grants, PKCE S256, trusted Google endpoint validation, and secure redirects. |
| Credentials | Keep rejection before sending credentials to untrusted hosts; ordinary and debug OAuth error formatting must not expose provider-controlled descriptions. |
| Account state | Keep typed reauthorization errors distinct from temporary provider failures. |
| Mail history | Keep additions, deletions, label mutations, stable event IDs, and deduplication rather than reverting to additions only. |
| Partial content | Keep explicit unavailable, omitted, truncated, unsupported, and malformed-encoding states; do not silently discard unavailable messages or attachment failures. |
| Extraction | Keep bounded response/attachment sizes, total attachment budgets, declared charset decoding, and UTF-8-safe truncation. |
| Errors | Keep typed provider status and retry metadata without logging raw response bodies. |

The current implementation includes the retained capabilities with stronger
boundaries; copying the older files over it would regress those controls.

## Finding and repair

The client validated the caller's history cursor and profile history ID but
accepted malformed IDs from a history-list response. Such an ID could become
an unusable next cursor or an invalid event identity. Apply the existing decimal
ID validation to the response cursor and every nonempty history entry ID before
returning a page. Reject the entire page on failure, preserving no partial
message/event result. Preserve existing missing-event-ID handling and decimal
IDs as strings, including values larger than signed 64-bit integers.

Google documents the response history ID as the value clients can store for a
future request after the last page. This makes response validation relevant to
checkpoint reliability. [Google Gmail history API](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.history/list)

## Evidence and limits

Five malformed-response regressions failed before the production repair.
The full `internal/googleoauth` package passed after the repair with Go 1.27.3
on Windows, including the large-decimal-ID preservation test.

These tests use local HTTP fixtures, not a real mailbox or Google consent flow.
Retained original files remain intact; semantic reconciliation is not proof of
byte-for-byte preservation on main or whole-worktree deletion eligibility.
