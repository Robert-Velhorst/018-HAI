# Archived TypeScript declaration reconciliation

## Scope and evidence

Reviewed baseline: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
This is a subset of the existing successful-addition archive cohort, not new
files to add to the global inventory. The 102 historical variants represent
100 distinct paths. The installed TypeScript 6.0.3 parser reads historical and
baseline source as data; no archived program, provider or browser was executed.
The frozen event index and successful source fingerprints select the input.
The private report retains declaration identities and comparison outcomes,
not transcript bodies or credentials.

| Disjoint classification | Variants |
| --- | ---: |
| Canonical full-source equality through the TypeScript printer | 17 |
| At least one declaration identity absent from the same file | 11 |
| Changed declarations, without absent same-file identities | 72 |
| Retained declaration/import comparisons, other file differences | 2 |
| Total | 102 |

Neither side has parser diagnostics in this cohort. Canonical equality is not
raw-byte equality, type checking, Angular template checking, runtime acceptance,
or proof that every requirement in a conversation was implemented. Class and
member identities overlap; declaration counts must not be summed as independent
features. The 72 changed variants and two other file differences still need
behavioral review. A failed shell pipeline produced no accepted report; the
successful direct Python-to-Node subprocess produced the report used here.

## Dispositions of all 17 absent same-file identities

All paths below are relative to `frontend/src/app/`. Historical references are
short patch fingerprints, not Git commit identifiers.

| Historical path and reference | Absent identities | Baseline source disposition |
| --- | --- | --- |
| `services/context-memory.service.interface.ts`, `8f14c10e06d4` | `IContextMemoryService.export` | Renamed to `exportMemories`; project scope remains an explicit argument. |
| `services/context-memory/context-memory.service.ts`, `fdcbc3e621e0` | `ContextMemoryService.export` | `exportMemories` calls the memory export API with project scope. |
| `pages/memory/memory.component.ts`, `5fbef1eb031f` | `MemoryComponent.export` | `exportMemories` is bound in the template, requires Advanced view, prevents duplicate execution, bounds request time, and creates a local JSON download. The backend export handler requires verified ownership and owner-scoped lookup. |
| `services/connected-source/connected-source.service.ts`, `a44e54a0cabf` | `ConnectedSourceService.updateExtraction` | Replaced by `submitExtractionCorrection`, with revision precondition and idempotency headers plus a correction receipt. Do not restore blind extraction mutation. |
| `services/connected-source.service.interface.ts`, `95d769a1b363` | `IConnectedSourceService.updateExtraction` | Interface exposes the corresponding correction submission and receipt flow. |
| `pages/hai-os/hai-os.component.ts`, `6f83c1ce07a3` | `HAIOSComponent.goHome` | Duplicate page navigation removed. No remaining page template binding invokes it; the shared shell and registry own Home and Control Center navigation. |
| `models/workflow.model.interface.ts`, `010b4d905dbc` | `IWorkflowTransitionRequest.approved` | Approval resolution has a separate request and endpoint. An arbitrary transition boolean must not become approval authority. This does not independently prove every global installation-admin policy. |
| `models/ambient.model.interface.ts`, `6f3b0e238df0` | `IAmbientScan.executed`, `IAmbientScan.rawBytesAvoided` | Current fields are `advanced` and `deduplicatedBytes`; see the model-field receipt for counter definitions and unverified legacy database backfill. Byte estimates are not measured disk or bandwidth savings. |
| `pages/control-center/control-center.component.ts`, `34aae6ad930d` | `scanSummary`, `snooze`, `archivingMemoryId`, `mobileNavigationOpen`, `navigation` | Summary moved to `agentCycleSummary`; archive-in-flight state is now a per-ID set; navigation state moved to the shared shell and registry. The old `snooze` performed a real transition to `waiting_external_input`, without a timer. The direct dashboard shortcut is absent; opening the workflow exposes that state transition in detail. This is a UX difference, not exact shortcut parity. |
| `pages/task-blueprint/task-blueprint.component.ts`, `1ba4bc108cb6` | `addAssistantMessage`, `messageFromPlan` | Controller notices use `addSystemMessage`; command results use `messageFromCommand` with explicit review, uncertainty and validation state. Do not restore a presentation that treats a prepared plan as verified execution. |
| `control-room/module-registry.ts`, `f6e8c8e1b0cc` | `moduleForUrl` | Moved to `control-room/module-url.ts`; Angular URL parsing and primary-outlet segments replace naive string-prefix routing. |

These dispositions cover 17 identities, not the complete behavior of the 11
containing variants. Current-main source, not an unmerged working tree, is the
preservation baseline. Several working-tree files have additional PR changes;
those are not counted as main-preserved merely because their paths match.

## Canonical-equal subset

The 17 canonical-equal variants comprise the model/service pairs for browser
verification, Gitleaks, Syft, Grype and Gosec; OpenLIT model/service; MiniSWE
model/service; MLflow model/service; and the AutoGen compatibility model.
Equality is source-comparison evidence only. Availability of external tools,
provider configuration and live end-to-end operation remain separate gates.

## Acceptance and cleanup boundary

No application build, full TypeScript type check, Angular template compilation,
browser flow or mutation test was run for this report. No new dependency or test
environment was installed. Existing tests referenced during inspection were
read, not rerun. No product-source change is justified solely by a renamed AST
identity; the remaining behavioral differences require separate reconciliation.

This receipt is not archive deletion authorization. Publication, exact-head CI,
main integration, complete requirement reconciliation and preservation checks
remain open. Neither whole-file integration nor source deletion eligibility is
claimed for any historical variant by this report.
