# HAI archive reconciliation follow-up: 24 October 2026

This is an earlier review snapshot. The current totals, later repairs and
remaining gates are maintained in `archive-coverage-reconciliation-2026-10-24.md`
and its linked follow-ups; do not read the open counts below as the latest state.

## Scope and preservation boundary

Read-only review against freshly observed remote main
`3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
No source files or archives were deleted, changed, or executed.
This receipt is outside the D: checkout because D: has zero free bytes.
Publication is tracked in PR 319. This receipt does not authorize archive deletion
and is not proof that its contents have been merged into main.

## Failed-patch intent already preserved on main

Four distinct failed-patch fingerprints account for 24 entries in the retained
failed-patch coverage report. Their actual source changes and main implementation
were read, not inferred from failed/success event flags alone.

| Main path | Archive change fingerprint prefix | Entries | Result |
| --- | --- | ---: | --- |
| `idp/internal/infra/kafka.go` | `9d58327525e9` | 9 | Intent preserved: trim client ID, reject empty ID, use trimmed value. Main additionally validates allowed characters before factory invocation. Full hunk differs because that additional check interrupts the historical context. |
| `idp/internal/infra/kafka_test.go` | `636fc9a5a9d2` | 9 | New hunk present verbatim, including missing-client-ID regression. |
| `idp/internal/app/authentication/google_oauth_test.go` | `6217c3de911d` | 1 | New test hunk present verbatim. Production client also rejects redirect following with `http.ErrUseLastResponse`. |
| `backend/internal/workflow/service.go` | `8debad9e430f` | 5 | New hunk present verbatim: scoped visibility compares trimmed record owner exactly. Existing blank caller behavior was not changed or newly qualified by this review. |

Source JSONL records were read at indexed byte offsets. Their SHA-256 prefixes:

- Kafka pair: `bd175c7e49d4`.
- OAuth test: `5deee265d4e5`.
- Workflow ownership: `065c2d5f0c4f`.

The SHA-256 of each decoded unified diff matches its full private index
fingerprint. Public prefixes are short references, not standalone hash proofs;
the full exact hashes remain in the private evidence/index. The generated
public receipt does not contain credentials or raw conversation content.
No fresh test suite was executed: this establishes source preservation, not live
provider acceptance or full runtime correctness. The additional review below
reduces the outstanding failed-patch subset to three unique variants. Remaining
archive contents still require reconciliation.

## Additional source preservation review

Main was freshly rechecked and remains the same commit. All 18 distinct archived
change hashes were independently recomputed from the decoded diff or added-file
content and matched their coverage-report fingerprints. Windows line endings were
normalized only for comparison; original files were not altered.

Nine more distinct variants, covering 38 failed event entries, have their source
intent preserved on main:

| Fingerprint prefix | Entries | Preservation evidence |
| --- | ---: | --- |
| `25a89a948161` | 9 | Task blueprint still defaults success criteria, framework evidence/completion requirements and validation-result criteria to arrays. Additional assurance defaults explain changed historical context. |
| `e841ec76c366` | 9 | Task-plan interfaces retain the proposed validation fields and criterion result fields. Main also supports framework assurance and not-applicable status. |
| `89aba5e8c834` | 6 | All four added cache exclusions remain in `.gitignore`: Go cache directories, Python bytecode directories/files and pytest cache. Additional Angular cache exclusion explains context differences. |
| `be87872e513b` | 1 | Image-removal button reset and visible keyboard-focus outline hunk is present after line-ending normalization. |
| `29592f138670` | 5 | Workflow service-test new hunk is present verbatim. |
| `f55f82ae072a` | 5 | Workflow handler new hunk is present verbatim. |
| `4bf6e7f0c275` | 1 | Approval-boundary test import addition is present after line-ending normalization. This import-only variant does not establish preservation of separate test-body variants. |
| `c30921a343b1` | 1 | Canonical allowed-symlink target regression is preserved in `openclaw_ecosystem_canonical_path_test.go` as `TestOpenClawSetEcosystemPathStoresCanonicalAllowedSymlinkTarget`. It reads synchronized ecosystem state and limits symlink skips to the Windows missing-privilege error. |
| `8c48a74e85cc` | 1 | Runtime tests still reject caller-selected outside paths and symlink escapes, and still check emergency stop prevents Odysseus network I/O. Their approved task helper now additionally supplies the required final-effect proof. |

Two further variants have now received a semantic disposition:

- `fd841c0a35f7` (one entry): all four proposed security scenarios survive in
  `approval_boundary_security_test.go`. Stale/future decisions retain the original
  test name, now using the required reviewed configuration. The renamed tests
  `TestApprovalProofCannotSurviveExecutionPolicyMutation`,
  `TestLaunchFailsClosedWhenImmutableIntentCannotBePersisted` and
  `TestLaunchDoesNotClaimCompletionWhenOutcomeAuditFails` retain the concrete
  zero-network, nil-result/error, indeterminate-status, single-effect and durable
  intent/audit assertions. This was checked by reading both old and current test
  bodies, not by names alone.
- `29b03a632046` (one entry): duplicate-active-ID rejection and truncated-stream
  tests survive. Cancellation now truthfully reports an indeterminate downstream
  outcome rather than claiming a verified stop. The former direct Odysseus
  execution test is superseded by explicit tests blocking unmediated tools before
  network I/O. This is a documented policy supersession, NOT a claim that the old
  direct execution behavior still exists. Do not restore the old unsafe path.

Together with the first four variants this accounts for 15 distinct variants and
64 failed event entries as source-preserved or explicitly superseded. No fresh
tests were run. Supersession is not byte equality or full external acceptance.

Three variants, representing six failed entries, remain OPEN:

- `37d7d98e1590`: historical added runtime test file; current runtime policy and
  several test names changed substantially. Function-name matches alone are not
  sufficient preservation proof for the entire old file.
- `c970061fa5e3`: proof lifetime bounded by recorded decision. Production issuance
  caps proof TTL to remaining decision lifetime, but the proposed regression is
  absent by name and an actual timing-boundary gap remains; see below.
- `a5a7e12aae53` (four entries): framework-selection provenance changes. Main
  contains newer normalization, persistence and audit code; the full nine-hunk
  semantic comparison is not complete. Do not overwrite it with the older diff.

This is a coverage receipt, not permission to delete any complete archive.

## Approval expiry boundary requiring repair

At the checked main revision, `automation_service.go:875` captures `decisionNow`.
Lines 900-915 compute `ApprovedAt + maximumApprovalDecisionAge - decisionNow`, cap
the requested duration to it, and pass only that relative TTL to the signer.
`approval_proof.go:371-372` then takes a later signer clock reading and adds the
relative TTL. Microsecond truncation does not generally cancel elapsed time
between those reads. There is no absolute decision deadline in
`ApprovalProofIssueRequest`, and the service returns the signed proof without a
post-issuance deadline check.

Therefore the intended hard upper bound is not guaranteed by the current source.
This is a source-level timing analysis, not a reproduced live exploit or a freshly
executed regression. A deterministic regression should advance the signer's
injected clock between the service's decision check and issuance. The repair
should carry an absolute not-after bound into the signer, enforce it before
signing and reserving issuance, reject already-expired windows, and retain the
existing owner/action/scope, durable issuance and consumption protections.
Mutating an already-signed proof is not a valid fix.

Implementation and full package tests are still outstanding. D: remains full;
no product code was modified and no test/build environment was started here.

## Prior published-head CI evidence

PR 319 head `52fc5a97be4dec94447897f39ca1dbaece689658` CI run
`40042113181` is terminal with one failed job: Promptfoo safety runner image.
Its production dependency audit reports one high and zero critical advisories.
The pending targeted dependency repair still requires the requested decision.
No CI check was waived and no main merge was performed. These results apply only
to that preceding head; later documentation commits require their own CI results.

## Storage observations

- C: approximately 6.45 GiB free; D: zero free bytes at this follow-up.
- Additional source worktree `codex-018-go-stable-20261023` is clean with no
  ignored files, but HEAD `30d895fdd572fa4e46628639f36ff9a05f265d69` is not
  an ancestor of current main. Its audit explicitly says the Go draft remains
  unmerged. Retain pending full content and publication verification.
- Docker reports 19.41 GB reclaimable build cache. HAI ownership cannot be
  established from its 983 cache descriptions; shared-cache approval is pending.
- Docker's unreferenced volumes are not proof of disposable data. Retain them.

The earlier whole-drive metadata inventory covered 8,486,552 files and 1,197,850
directories, with 121 denied locations, two concurrently missing directories and
97,951 skipped reparse points. It did not read every file's content. A further
bounded content pass read 1,167 candidate text files (11,837,053 bytes), found
64 explicit HAI marker matches, skipped 350 sensitive/binary/large/reparse
candidates and found 29 candidates no longer present. No raw contents were
printed by that pass. These bounded checks are not an exhaustive content audit.
