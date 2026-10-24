# Owner, maintenance and remaining helper dispositions

## Scope

Main baseline: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
This static pass addresses the remaining ten absent production declaration identities in the refined changed-addition comparison. Together with the two previous helper receipts, all 23 absent production identities across twenty archived files have explicit dispositions. This is not completion of those files' other changed declarations.

The frozen indexed additions were reread and content fingerprints and stable archive metadata checked. All eight newly reviewed current production files match main through repository Git filters. Blank import-suppression declarations are discussed separately below. Historical code was not executed and no tests were rerun in this pass.

## Owner-bound deletion and preferences

| Archived source and reference | Historical identity | Disposition |
| --- | --- | --- |
| `backend/internal/source/repository.go`, `c10c7226e3a0` | `(*GormRepository).DeleteExtraction` | The old implementation deleted by extraction ID alone. Current repository entry points require the expected extraction/source and owner; the guarded transaction locks the source/extraction, compares identity and revisions, and participates in final authorization and workflow retraction. The service's legacy unguarded method returns `ErrDestructiveAuthorizationRequired`; its authorized path checks owner, emergency stop and transactional authorization. Do not restore ID-only deletion. |
| `backend/internal/ambient/repository.go`, `3dbcb5e46cba` | `(*GormRepository).UpdateNeed` | Historical code saved a shared `AmbientNeed`. Current repository stores separate `AmbientNeedOverride` records and looks them up by owner plus need key. Do not replace private overrides with shared-profile mutation. Repository save is not itself a substitute for service/HTTP authorization. |
| `backend/internal/ambient/service.go`, `4454e0b8c6a6` | `(*service).UpdateNeed` | Current `UpdateNeedForOwner` requires a nonempty owner, resolves defaults plus private overrides, preserves 0-100 clamping, trims notes and returns the resolved private preference. Omitted optional fields do not overwrite unrelated values. Keep owner-bound behavior. |

## Model update

- Source: `backend/internal/llm/maintenance.go`, reference `cb91455210c6`.
- Historical identity: standalone `pullOllamaModel`.
- Current successor: `(*Service).pullOllamaModel`, called from `refreshOllamaModel` after constructing final-effect authorization for the provider, model, endpoint, payload and configuration fingerprint.
- The successor propagates the parent context, bounds the timeout and response body, checks cancellation before network I/O, checks final-effect authorization immediately before the request and distinguishes an attempted update from rejection before the request. It requires explicit `status: success`; the old helper accepted a missing status.
- Retain this guarded service method. Copying the old standalone helper would bypass the current authorization hook and weaken provider-success evidence.
- Inspected existing fresh-authorization and missing-success-response test bodies as source. No actual model pull, authorization consumption or provider call was performed by this review.

## Telemetry and private utilities

| Archived source and reference | Historical identity | Disposition and limitation |
| --- | --- | --- |
| `backend/internal/workflow/autonomy_telemetry.go`, `6d63232bb376` | `closeRunningAutonomyTrace` | Historical generic closer updated the latest running trace and ignored save errors. Current `finishAutonomyAttempt` records completion time, latency, policy/approval/verification fields and evaluation data. Inspected its calls after owned claimed-item update and completed-item attestation. Keep the enriched path, not the old generic updater. This does not prove crash recovery or atomic telemetry persistence; current telemetry writes remain a separate reliability concern. |
| `backend/internal/planningoptimizer/service.go`, `17dc06709701` | `sortedIDs` | Old helper only sorted input job IDs. Current `validProposal` directly checks supplied IDs against the request map, rejects duplicates/unknown IDs, checks scheduled plus deferred cardinality, duration, priority and windows. The helper has no current caller; it appears only at its declaration in the 251-addition cohort. No separate user-facing scheduling capability is recovered by restoring it. The cohort is not every historical file. |
| `backend/internal/autonomy/service.go`, `15b8b2182135` | `ErrNotFound` | Historical sentinel is declaration-only in the changed-addition cohort. Current telemetry overview uses owner-scoped collection queries and propagates database errors; no current backend reference to `autonomy.ErrNotFound` was found. Do not add an unused sentinel as if it were missing record-retrieval functionality. Other historical files remain separately reviewable. |
| `backend/internal/gitleaks/service.go`, `e3634749c595` | `(*service).String` | The old method only formatted the enabled flag. Current `Service` exposes `Status`, `Probe` and `Scan`; the structured status remains the operational interface. The missing convenience formatter is not missing scan capability and need not be restored. This is not proof of live scanner readiness. |

## Blank declarations

- Ambient repository reference `3dbcb5e46cba`: `var _ = time.Time{}`.
- MCP bridge reference `671784fb6638`: `var _ = strconv.IntSize`.

Both merely kept imports referenced; neither calls an initializer with a side effect or implements a product capability. Their absence is not an integration gap. Do not restore unused imports for these markers.

## Remaining gates

This completes explicit dispositions for the absent production-identity subset only. Changed declarations, test fixtures, comments/requirements, other patch types, the three invalid historical Go snapshots, unique local source copies and non-code transcript requirements remain in scope. Publication, exact-head CI, main integration, private-data handling and deletion verification remain separate gates. No source or archive was deleted.
