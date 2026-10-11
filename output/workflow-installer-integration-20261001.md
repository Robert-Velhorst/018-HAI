# Workflow and installer integration: 2026-10-01

This is a bounded continuation checkpoint, not a production release or a claim
that the entire HAI implementation is complete. Canonical stack: Go, Angular,
Postgres. Shared working tree: `codex/hai-runtime-release`; existing work and
diagnostic artifacts were preserved. Nothing was committed, pushed, deployed,
installed, signed, or removed from the personal installation in this phase.

## Parallel work integrated

Seven workers contributed across implementation and independent review: task UI,
installer signing guards, initial execution-path review, independent follow-up,
task fallback fixes, workflow-adapter fixes, and health-contract review. The
parent integrated backend fixes and CI wiring. The follow-up identified four
additional defects; two more
workers repaired them in separate task-engine and workflow-adapter files after
the frozen-source baseline completed. Independent final source review and the
post-repair backend suite are complete. All seven workers are closed. Workers
used distinct file ownership; resource-intensive checks are bounded and
serialized rather than competing for the host's remaining memory.

### Execution and workflow behavior

- Missing model selection blocks controlled runtime/model dispatch unless the
  existing narrowly scoped deterministic runtime path qualifies. Metadata is
  rechecked on initial execution and fallback/retry execution.
- Optional metadata inspectors fail closed, including typed-nil dependencies.
  Credential-bearing repository errors are not exposed to the user.
- A previous completed GET/HEAD receipt may be reused for the existing
  deterministic completion path without relaunching the runtime or inventing a
  new model request. The check does not authorize a new external action.
- A fallback readiness block retains prior runtime evidence for review rather
  than reporting that nothing happened or repeating a completed effect.
- Reservation denial also retains earlier tool evidence and actions. Fallback
  execution blockers override verification success and remaining retry budget:
  review is required, automatic retry is disabled, and no accepted lesson is
  recorded from that blocked branch.
- Only outstanding approval with no separate model/resource/participant/action
  blocker is translated into an approval-only workflow result. Existing runtime
  proof, required participants, governance, and approval checks are unchanged.
- Missing or malformed resource readiness and invalid domain authority cannot
  become approval-only routing. Mixed readiness retains named repair requirements
  alongside the other validation failures, with redacted and bounded messages.
- Workflow approval/review paths report `reviewRequired=true`, including their
  persistence/claim-loss branches; no completion attestation is fabricated.
- Framework health checks now accept the agent registry's verified `available`
  status as well as legacy `healthy`, while retaining registry provenance,
  timestamp, revocation, and contract-age checks. Inventory availability alone
  is not health proof and cannot confer execution authority.

### Task planning UI

- Basic view reports missing model selection and actual execution blockers.
- Framework authority, participants, and parameter prerequisites remain visible.
- Missing models are not labelled as an automatically selected routing tier.
- Existing execution/participant controls are not weakened to make the UI appear
  ready. These are targeted corrections, not a claim of full visual acceptance.

### Windows packaging and CI

- Explicit production mode requires a pinned, provisioned code-signing identity,
  an explicit signing tool, and an approved HTTPS timestamp endpoint.
- Production rejects dirty-worktree opt-in and skipped compilation. Workers and
  Setup require independent signature verification before output promotion.
- Unsigned developer previews remain distinct. CI names and artifact names now
  explicitly identify the unsigned preview and run guards in PowerShell 7 and
  Windows PowerShell 5.1 before building it.
- No real certificate was selected or used. No installer was regenerated.
  The generated uninstaller still needs its own signing hook and verification.

## Verification ledger

| Check | Outcome and scope |
| --- | --- |
| Pre-follow-up focused Go race suite | PASS: task, workflowtask, workflow; one repeat, 120-second per-package timeout. See `workflow-preflight-focused-final-race-20261001.log`. This predates the follow-up and health repairs. |
| Full backend, pre-follow-up baseline | PASS: terminal exit 0, 147 packages, 3863 top-level tests, zero failures, 208 optional test/subtest skip events, zero JSON parse failures. See `backend-preflight-final-full-20261001.jsonl`. This predates the four follow-up repairs and does not certify them. |
| Final corrected focused Go race suite | PASS: task 31.118s, workflowtask 1.335s, workflow 1.068s; terminal exit 0, one repeat, focused regression selection rather than full-backend race coverage. See `backend-health-final-focused-race-20261001.log`. |
| Final corrected full backend | PASS: terminal exit 0, 147 packages, 3871 top-level tests, zero failures, 208 optional test/subtest skip events, zero JSON parse failures. See `backend-health-repaired-final-full-20261001.jsonl`. Sources were frozen during this run; this is local synthetic/fixture acceptance, not live-provider or deployment acceptance. |
| Final corrected backend static analysis | PASS: `go vet -p 1 ./...`, terminal exit 0. See `backend-health-final-vet-20261001.log`. Cached Go 1.25.13 container, no network, read-only source; no local account or private deployment touched. |
| Task Blueprint component/accessibility suite | PASS: 37 tests in Chrome Headless. See `task-blueprint-preflight-20261001.log`. Component checks are not a full keyboard/browser accessibility audit. |
| Full current frontend suite | PASS: terminal exit 0, 1140 SUCCESS in Chrome Headless 151. See `frontend-preflight-final-full-20261001.log`. |
| Current frontend production build | PASS: terminal exit 0, 703.22 kB initial raw / 115.28 kB estimated transfer, six existing component SCSS budget warnings. See `frontend-preflight-production-build-20261001.log`. No live deployment or measured 90% resource-saving claim. |
| Frontend Node contracts | PASS: 10 tests. See `frontend-preflight-node-contracts-20261001.log`. |
| Progressive disclosure source contract | PASS: 29 templates, 146 static section IDs, two dynamic prefixes. This is source-contract coverage, not populated-view rendering proof. |
| Installer guard suite, parent rerun | PASS: 56 cases, including native read-only rejection of the retained unsigned Setup. Successful signing cases use doubles. See `installer-signing-parent-tests-20261001.log`. |
| Installer guard suite, worker checks | Reported PASS on both PowerShell engines; details in `installer-signing-gate-20261001.md`. |
| Local CI contracts | PASS: 77 tests. See `ci-preflight-signing-contract-20261001.log`. Hosted CI was not run. |
| Scoped tracked-file whitespace check | PASS. Git reported normal LF/CRLF conversion warnings, not whitespace errors. |

The fourth worker's source-derived findings are retained in
`workflow-preflight-followup-review-20261001.md`: resource-reservation denial
discarding an earlier receipt, fallback blockers retaining automatic retry,
incomplete resource/domain checks for approval-only routing, and named readiness
messages lost at the adapter. All four were repaired and independently re-read
in that report. The final corrected full and focused race suites above cover
their integrated source; the earlier passing baseline did not contradict them.

The first post-review full run failed on a resource-pointer test compilation
error and two fallback fixtures missing mandatory health prerequisites. The
first health-contract full run then failed on two new positive fixtures whose
five-minute timestamps exceeded the unchanged 60-second contract. Those test
fixtures were corrected; production health or authority gates were not relaxed.
Both failed logs (`backend-post-review-final-full-20261001.jsonl` and
`backend-health-final-full-20261001.jsonl`) remain as diagnostic history and are
not counted as passes. The independent health review is retained in
`framework-health-contract-review-20261001.md`.

Fallback tests use a synthetic health stamp for an existing verified embedded
card, fake execution/verification, and authorization/emergency-stop stubs. The
owned fixture's HTTP 200 does not prove coordinator, provider, participant, or
launcher readiness. Completed receipt reuse checks receipt shape, not durable
cryptographic attestation. These assumptions remain explicit acceptance limits.

The earlier broad race run with three repeats was interrupted after diagnostic
inspection of the owned test process. It revealed two regression fixtures/paths:
missing model readiness shadowing an invalid-governance fixture, and blocking
reuse of already completed deterministic evidence. Both were repaired and the
final focused race suite passed. That earlier failed/interrupted log is retained
as `workflow-model-preflight-race-20261001.log`; it is not counted as a pass.
No edit-during-test snapshot is offered as final-source acceptance.

## Remaining release gates

1. Boolean GET/HEAD metadata is not endpoint-semantic attestation or a digest-bound
   launch snapshot. The final launcher still requires its exact action-bound
   authority/proof; a metadata lookup does not grant permission. A configuration
   change between lookup and launch remains a binding gap to address explicitly.
2. Historical executor errors with unknown effects need a stronger explicit
   uncertainty contract. The new adapter does not turn them into ordinary
   approval or safe automatic retry, but does not close that broader gap.
3. The retained isolated operator journey is still failed, with missing real
   model/framework/participant prerequisites. No fresh end-to-end runtime image
   or successful terminal operator journey was produced in this phase.
4. Required specialist implementations and evidence, approved real providers,
   live connectors, OAuth/SMTP and account acceptance remain separate gates.
5. A signed current Windows release, signed uninstaller, clean installation,
   upgrade/uninstall, recoverability and actual database restore remain unproven.
6. Complete populated Advanced/keyboard/browser acceptance remains open, as do
   six component SCSS budget warnings. Current full frontend suite/build results
   above do not silently relabel earlier browser runs as current-source proof.

No paid provider ceiling, authority boundary, specialist requirement, source
retention policy, or live-account approval was changed to manufacture completion.
