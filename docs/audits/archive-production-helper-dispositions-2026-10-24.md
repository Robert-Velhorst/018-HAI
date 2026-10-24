# Further archived production-helper dispositions

## Evidence boundary

Baseline main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`, freshly verified against origin on 2026-10-24. Published PR #319 head: `42c4bb6ce9a1f771eea593d60d91f909b7e7d267`.

This is a static, assertion-level review of eight additional declaration identities across seven archived additions. It is not a whole-file integration receipt. The archived successful add events were reread at frozen index offsets, checking content fingerprints and archive size/metadata stability. Archived code was not executed.

Six current source files match main through repository Git filters. The seventh, `backend/internal/safety/redaction.go`, differs by an already published six-line URL-redaction delta; its current normalized blob matches the published PR head. The remaining main-to-working difference was inspected, not ignored. No source deletion or raw-byte equivalence is asserted.

## Individual dispositions

| Archived path and content reference | Historical identity | Current source disposition |
| --- | --- | --- |
| `backend/internal/haios/handler.go`, `fba950aa3722` | `(*Handler).count` | Replaced by `countForOwner` and request-local count caching. Historical code ignored `Count` errors and had no owner filter. Current queries scope personal records by owner or owning workflow/source/run; only explicitly labelled shared automation registry records remain global. Missing owner/storage, unsupported models and storage errors are errors, not successful zero-health readings. Retain current implementation. |
| `backend/internal/memoryengine/service.go`, `4ff5b904605e` | `conversationPreview` | Historical code copied role-prefixed message content into a redacted, truncated plaintext preview. Current import stores the encrypted payload and nonce, with only message-count/platform metadata in `Preview`. Do not reintroduce plaintext message excerpts. This concerns the preview field, not a claim that every conversation metadata field or preexisting row is encrypted. |
| `backend/internal/safety/redaction.go`, `8eb831b71589` | `keyValueSecretPattern` | Replaced by structured JSON processing, sensitive-key matching and quoted/unquoted assignment redaction with size/depth limits. The same historical credential-key families remain covered by the successor definitions. Main already contains this replacement. The additional embedded-URL repair in PR #319 is a separate pending-main change; do not count that repair as main-preserved. |
| `backend/internal/source/scheduler.go`, `ed0934970876` | `fmtSscanf` | The historical variable was a direct `fmt.Sscanf` forwarding closure. Current `schedulerInterval` invokes `fmt.Sscanf` directly, checks errors and enforces the current interval bounds. There is no separate parser feature to port from this wrapper. This does not declare the entire scheduler behavior equivalent to the historical snapshot. |
| `backend/internal/braincatalog/capability_recommendation.go`, `6b0be667c4e2` | `capabilityTokens` | Replaced by `capabilityTerms` and `capabilityTermValues`. Lowercasing, letter/digit tokenization, short-term/stop-word exclusion and deduplication remain. Current code additionally normalizes plurals and records explicit versus expanded operational aliases for weighting. Retain transparent expanded-term behavior instead of replacing it with the historical flat token list. |
| `backend/internal/mcpbridge/service.go`, `671784fb6638` | `wrapWorkflowItems` | The historical function returned nil unconditionally. Current `Actionable` reads the configured owner's dashboard, projects approval/blocked/ready/high-risk/no-next-action groups, removes duplicate IDs, sorts by priority then ID and bounds the result. Restoring the old wrapper would restore a placeholder, not missing functionality. |
| `backend/internal/mcpbridge/service.go`, `671784fb6638` | `workflowItem` | The old private wrapper held an interface requiring `GetID`. Current code uses concrete model groups and a deliberately limited `ActionableWorkflow` projection. Full descriptions, raw intake, evidence and credential-bearing records are not fields of that projection. Keep the concrete limited projection. |
| `backend/internal/braincatalog/maintenance.go`, `430351c78627` | `redactedCatalogMaintenanceReason` | The old function forwarded a string to `safety.RedactSecrets`. Current maintenance failure constructors use fixed public messages instead of embedding repository/provider error details. Checked the upstream lookup, review and persistence failure paths and the collection failure constructors; do not reintroduce raw error interpolation through the old helper. This is not a review of every successful upstream payload. |

## Validation performed

- Fresh remote-ref check, archived event/content-identity checks and current source reads.
- Git-filtered main comparisons for all seven files, with the redaction difference isolated and compared to the exact published PR blob.
- Existing metric-scope, recommendation and MCP projection regression tests were located and inspected as source only. They were not rerun in this pass. No build cache was regenerated and no application, container, provider, database or tunnel was started.

## Remaining scope

The old planning `sortedIDs`, autonomy not-found sentinel, trace-closing helper, Gitleaks display helper and standalone Ollama pull helper are not resolved by this receipt. The source-deletion and ambient-owner repository migrations also require their own complete receipts. Compile assertions, changed declarations and historical test fixtures remain separate work. No complete archive can be removed on the strength of these eight decisions.
