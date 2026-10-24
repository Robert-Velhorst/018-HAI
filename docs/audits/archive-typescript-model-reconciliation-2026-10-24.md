# Archived TypeScript model-field reconciliation

Baseline: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
The installed TypeScript 6.0.3 parser inspected 29 historical model variants
covering 28 paths within the already indexed 102-variant TypeScript cohort.
Two versions of the task-plan model are counted separately. Counts below are
variant-level observations, not unique application fields or additional files.

## Comparison results

Of 1,088 historical named interface properties, 1,081 retain their printed type,
optional flag and readonly flag; four change and three are absent by old name.
There are 2,392 newly named properties across the current counterparts of these
variants. Additions are not independently verified features.

Neither historical nor baseline model files contain top-level statements outside
interfaces, type aliases and import declarations. Existing type aliases,
interface headers and other compared members are retained. Three existing import
declarations expand their symbol lists without removing historical symbols:
task-plan adds `ILLMGenerationResult`; memory-engine adds `IContextMemory`;
pursuit adds workflow decision, event, checklist, quality-gate, source-link and
transition types. This is AST/source evidence, not emitted-JavaScript checking.

## Four property changes

| Variant and property | Change | Disposition and limitation |
| --- | --- | --- |
| Connected-source, `72a356fe4890`: `ISourceSyncJob.startedAt` | Required string becomes optional string | Loosens the client contract. The baseline Go model still serializes `startedAt` without `omitempty`. This does not prove every consumer handles missing timestamps. Do not describe it as verified server omission compatibility. |
| Task-plan, `ab065f7b31a1`: `IToolExecutionResult.auditEvents` | Required string array becomes optional | Loosens the client contract. The baseline Go task result still has a non-omitempty JSON field. Consumer guards, nullability and live serialization remain unverified here. |
| Same task-plan variant: `IValidationCriterionResult.kind` | Adds `framework_assurance` to the documented string union | The baseline backend explicitly appends and handles assurance criteria. Both old and current unions already include `string`; this is a documented value addition, not a newly closed enum. |
| Same task-plan variant: `IValidationCriterionResult.status` | Adds `not_applicable` to the documented string union | The baseline validation implementation defines and returns this state. Both unions already include `string`; rendering and approval semantics still require their own behavioral checks. |

## Three absent historical properties

`IWorkflowTransitionRequest.approved` is replaced by the separate approval
resolution contract. `IAmbientScan.executed` and `rawBytesAvoided` have the
`advanced` and `deduplicatedBytes` successors documented in the declaration and
Go model-field receipts. These source-level dispositions do not prove legacy
database migration, historical counter backfill or all approval-policy gates.

## Two additive model variants

The context-memory model (`e435af27b09d`) and RAGFlow model (`8a1fa0d14466`)
retain all compared historical declarations and imports. Their source additions
explain the previous whole-file inequality without absent historical identities.
There is no reason to overwrite their current source with the older snapshots.
Added definitions and integrations are not thereby runtime-accepted.

## Remaining gates

The helper parsed source as data, without importing or executing archived code.
No compiler installation, app build, full type check, browser, provider call or
database operation occurred. This analysis does not resolve all changed service
or component method bodies. It also does not prove runtime nullability,
structural assignability, API compatibility or complete semantic preservation.

All archives and unique source remain protected. This receipt alone establishes
neither whole-file integration nor source deletion eligibility. Publication,
exact-head CI and current-main preservation remain separate requirements.
