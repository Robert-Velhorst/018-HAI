# Child-agent integration ledger

This ledger preserves the operational result of the large HAI child-agent run
before any local transcript cleanup. It is evidence about integration state, not
permission to delete session data.

The separately audited 2026-07-30 archive and its cleanup-readiness decision are
recorded in `docs/child-agent-archive-2026-07-30/cleanup-readiness.md`; do not
mix its file counts or retention decisions with the August cohort below.

## Audited snapshot

Snapshot date: 2026-08-08

- Pinned HAI root session: `019e7acc-44f2-7c90-a04e-253f6d43df28`.
- August 4-5 contains 97 child transcripts. Applying the conservative rule that
  the latest terminal event wins produces 88 completed, three aborted, and six
  without a terminal marker. The completed set occupies exactly 238,891,183,316
  bytes: 222.485 GiB or 238.891 decimal GB.
- Across all audited August dates, 114 HAI child transcripts were found: 104
  completed, three aborted, and seven without a terminal marker. The completed
  set occupies exactly 263,110,870,955 allocated bytes: 245.041 GiB or 263.111
  decimal GB.
- Forty-seven completed August 4-5 children reported implementation work. The
  181 genuine repository paths declared by those reports were present in the
  shared worktree at audit time.
- Presence is not release proof. At audit time `main` and `origin/main` both
  pointed to `37edf880e9252bf34635ef9c35b173e32b89ced0`, while the shared
  worktree contained 273 modified and 362 untracked status entries with nothing
  staged.

## Results preserved in the worktree

The completed implementation sequence is represented by source, migrations,
tests, and the truthfulness matrices in this repository. Its major themes are:

1. Durable approval and single-use execution authorization.
2. Governed contact review and owner-scoped life context.
3. Selector-v5 risk, autonomy, and approval ceiling enforcement.
4. Typed framework evidence before authorization and after execution.
5. Temporal claim assessment with source and contradiction handling.
6. Pursuit portfolio planning, allocation, proposal, decision, authorization,
   workflow creation, dispatch, settlement, and controlled learning.
7. Advisory ambient outcome monitoring and immutable composition provenance.
8. Immutable plan graphs and exact workflow and pursuit coordination binding.
9. Governed internal reminder delivery with append-only receipts and no external
   messaging authority.
10. Runtime, provider, and external-effect boundaries that continue to fail
    closed when live acceptance evidence is absent.

The current implementation and remaining external acceptance boundaries are
tracked in `docs/completion-matrix.md`,
`docs/framework-operating-contract-matrix.md`, and
`docs/requirements-traceability.md`.

## Preserved transcript outputs

The provisional audit has been replaced by three generated, reviewable
artifacts:

- `docs/child-agent-transcript-manifest.csv` has one row per audited transcript,
  including terminal state, work kind, exact logical and allocated bytes,
  terminal-report SHA-256, and disposition.
- `docs/child-agent-final-reports.md` preserves the complete terminal report for
  every completed child. Potential credential-shaped values are redacted, while
  the manifest stores the SHA-256 of the original report text.
- `docs/child-agent-transcript-summary.json` records aggregate counts and exact
  byte totals.

The artifacts are reproducible with
`scripts/audit-child-agent-transcripts.ps1`. The script reads session metadata
and bounded transcript tails; it cannot delete, move, truncate, compress, or
archive session files.

## Transcripts that must be retained

Aborted children:

- `019fd061-d940-7c53-b9ce-a5f94cba4f37`
- `019fd062-611f-7fa1-a245-ceed50c175d2`
- `019fd062-e4b9-7e01-b26c-757eb8dfdbe3`

Children without a terminal marker:

- `019fd0dc-4212-7800-8448-62a9fc8ca673`
- `019fd0de-103a-7092-8771-9b1ae1c83fd6`
- `019fd0df-32c2-79b0-aae3-a9d3c566575b`
- `019fd0e0-43a3-76e0-abf3-4a1490e05cc9`
- `019fd0e0-64ff-7cf0-915c-1d101cbaeeb2`
- `019fd0e0-89cd-71c1-89ca-fa6ce900c8ac`
- `019fded0-b7ee-7490-9573-596d47cf4e36` (current August 8 child)

These ten retained transcripts occupy exactly 21,882,851,783 allocated bytes:
20.380 GiB or 21.883 decimal GB. This includes the current August 8 child.

The two completed children that stopped with partial work and the read-only
whole-system synthesis no longer require their multi-gigabyte transcripts for
result preservation. Their terminal reports are in
`docs/child-agent-final-reports.md`, and their applicable source work is covered
by the verified repository checkpoint. They therefore follow the same cleanup
disposition as other completed children.

## Cleanup gate

Completed transcripts become cleanup candidates only after all
of the following are true:

1. The shared worktree has a named Git checkpoint containing the intended HAI
   source, migrations, tests, and this ledger.
2. Backend, IDP, frontend, and Compose validation results are recorded against
   that checkpoint.
3. Audit-only findings that are not represented in the matrices are distilled
   before their transcripts are removed.
4. Aborted, non-terminal, and current children remain untouched. Completed
   partial-work reports must be preserved before their transcripts qualify.
5. Local databases, source attachments, worktrees, credentials, Playwright
   diagnostics, and external-provider evidence are excluded from transcript
   cleanup.

No transcript deletion, movement, truncation, compression, or archival was
performed while producing this ledger.

## October 10 integration checkpoint

The 2026-07-30 archive is separately inventoried in
`docs/child-agent-archive-2026-07-30/cleanup-readiness.md`. Its 18 JSONL files
total 20,739,169,122 bytes. The existing ledger crosswalk covers the eight
unique completed cleanup candidates and the patch-bearing retained transcripts;
it is not proof that every nested completion event in the retained histories
was a distinct task or was individually integrated.

An earlier note counted 542 nontrivial completion texts across six retained
histories without recording source paths or a reproducible extraction artifact.
The October 11 audit below has since attributed and reproduced the count. It
remains a count of distinct trimmed message texts, not distinct tasks or integrated work.
The verified July 30 inventory is the 18-file manifest above: eight unique
completed candidates have report/source crosswalks, and ten duplicate,
aborted, or nonterminal files remain retained. Do not treat the 542 messages as
proof that each underlying item is integrated, and do not claim that every
event embedded in retained histories has been individually reconciled.

For the current clean integration snapshot, the complete tracked HAI change
set was compared with the latest remote PR source. No additional product-code
patch was missing from that newer source after removing one byte-identical
duplicate 627-line frontend test block. The additional isolated-acceptance
guide and secret-scanner-safe test-fixture corrections are included in this
snapshot. The eight untracked local database/race-test outputs are diagnostic
evidence, not source changes, and are excluded; the original worktree and those
files remain untouched.

Local verification on 2026-10-10 used Node 24.19.0 and the repository-pinned Go
1.27.2. Results: frontend `npm test -- --watch=false --browsers=ChromeHeadless`
passed 175 Node checks and 1,328 Angular tests; frontend production build
passed with four existing SCSS component-size budget warnings; backend
`go test -p 2 ./...` passed; IDP `go test -p 2 ./...` passed. A redacted
Gitleaks directory scan of the candidate tree found no remaining findings
after converting synthetic credentials and a pinned public signing key to
scanner-safe runtime fixtures. These are local repository checks, not live
provider, account, deployment, or external acceptance.

At this checkpoint, remote `main` remains `91c8620c557229f1da4ed15fcbb7088c6a6947a7`.
The existing `codex/hai-runtime-release` branch remains at
`2691b54deeeb6aaff32021d8bf8d17f2b1453edd`; it has not been rewritten. No new
snapshot branch or pull request has yet been published. Preserve the original
20.7 GB archive until the repository and PR gates below pass. The ten retained
files are not cleanup candidates under this ledger; only the eight exact,
hashed candidate paths can be considered for later cleanup. The unattributed
542-text figure is a separate unresolved provenance question, not a verified
count of this archive's work.

## Cleanup disposition

All five cleanup gates are now represented in committed or generated evidence:

1. The integrated source checkpoint exists and is pushed.
2. Backend, IDP, frontend, build, and Compose validation evidence is recorded.
3. Every completed child's terminal report is preserved with its original-text
   SHA-256, including audit-only findings and partial-work reports.
4. Every aborted, nonterminal, and current child is explicitly marked `retain`
   in the manifest.
5. The manifest contains only HAI child transcript JSONL files under the audited
   August session tree. Databases, attachments, worktrees, credentials,
   diagnostics, and provider evidence are not cleanup candidates.

After this ledger and its generated artifacts are committed and pushed, the 104
manifest rows marked `candidate_after_ledger_commit` can become an exact cleanup
allowlist. Deleting only those paths would reclaim 245.041 GiB (263.111 decimal
GB) while preserving the ten protected transcripts and all source/worktree
data. Cleanup remains a separate explicit operation; this ledger does not
authorize or perform it.

The manifest, preserved terminal reports, reproducible audit script, and this
ledger were committed and pushed on `main` as
`7f76cd6671d498edfa8014996e4cab3682a9972c`. The 104 completed rows therefore
satisfy the `candidate_after_ledger_commit` condition.

## Cleanup execution

On 2026-08-09, the 104 exact manifest paths marked
`candidate_after_ledger_commit` were deleted in a separate, explicitly
authorized operation. Preflight required every file to remain under the audited
session root, match its recorded size, have terminal status `completed`, have a
preserved final report, and have no writer-lock reference.

Post-delete verification found zero candidate files, all ten retained files at
their recorded sizes, the pinned parent task still present, and exactly ten HAI
child transcripts remaining in the audited August tree. The operation reclaimed
263,111,102,464 bytes according to the filesystem. Full evidence is recorded in
`docs/child-agent-transcript-cleanup-receipt.md`.

## Verified checkpoint

The integrated source was checkpointed on `main` as commit
`4dc725628a717ece36ca22e246ba5a42c2fd2fcf` on 2026-08-08. The commit contains
785 source and documentation files and was pushed to the canonical
`Robert-Velhorst/018-HAI` repository.

Validation completed before the checkpoint:

- Backend: `go test ./...` passed with Go 1.25.12 in the documented container.
- IDP: `go test ./...` passed with Go 1.25.12 in the documented container.
- Backend and IDP production builds passed.
- Frontend: 372 ChromeHeadless unit tests passed.
- Frontend production build passed. Existing bundle-budget warnings remain and
  are not represented as failures.
- `docker compose --env-file .env.example -f docker-compose.local.yml config
  --quiet` passed.
- `git diff --cached --check` passed after three migration EOF whitespace fixes.
- A staged credential-pattern scan found only intentional fake values in
  redaction and security tests.

After the push, local `main` and `origin/main` both resolved to the checkpoint
commit and the worktree was clean. This satisfies cleanup gates 1 and 2. The
generated manifest and terminal-report archive satisfy gates 3 through 5 for
the audited allowlist; those gates remain mandatory for every future batch.

## Tracked documentation variant audit (2026-10-13)

Compared the remaining seven content-divergent tracked documentation files in
the stale local checkout against `origin/main`, ignoring line-ending-only
differences. None is suitable to copy over the canonical version:

- `docs/backup-restore.md`: the local variant removes the explicit inventory
  and fail-closed refusal for unaccounted `018-hai-*` Docker volumes, and
  describes the older v1 manifest contract instead of the canonical v2
  declaration. Keep the canonical volume-coverage checks.
- `docs/codex-goal/completion-matrix.md`: the local variant downgrades the
  verified-JWT authorization and Compose evidence, changes Go/test counts,
  and replaces the evidence-based roll-up with a different gap summary. These
  assertions conflict with the canonical implementation/evidence and must not
  be restored from the stale copy.
- `docs/completion-matrix.md`: the local commands pin Go 1.25.13 while the
  canonical instructions use Go 1.27.2. Do not downgrade the documented test
  toolchain.
- `docs/external-provider-reality-review.md`: the local copy omits the
  Autofever row and replaces the dated Trello evidence qualification with a
  blanket no-current-acceptance statement. Preserve the canonical distinction
  between historical evidence and current-account acceptance.
- `docs/optional-runtime-profiles.md`: the local copy omits the Compose-profile
  framing, one profile, network-overlap guidance, SearXNG and local-model
  activation procedures, and the retained one-model acceptance claim. It also
  adds an explicit warning that Compose syntax validation alone proves no
  runtime behavior. Do not replace the canonical guide wholesale; independently
  revalidate the historical model evidence before changing that claim.
- `docs/ossinsight-screening-ledger.md`: the local copy drops the governed,
  optional fixed-ngrok A2A transport boundary and describes only local use.
  Keep the canonical scope and explicit controls.
- `docs/provider-credential-checklist.md`: the local copy removes the
  read-only Trello live-acceptance procedure, leaving operators without its
  credential and verification checklist. Keep the canonical procedure.

Seven other modified tracked documents in this audit were line-ending-only
after normalization (`docs/acceptance-test-matrix.md`,
`docs/codex-goal/README.md`, `docs/migrations.md`,
`docs/operator-runbook.md`, `docs/release-process.md`, `docs/roadmap.md`, and
`docs/windows-installer.md`). No documentation variant was ported. This is a
source comparison only; it does not validate the truth of historical runtime,
provider, or release claims beyond the repository evidence cited by each
canonical document.

## Frontend shell and deployment boundary audit (2026-10-13)

The stale local checkout has 201 changed frontend paths relative to the
verified remote `main` head. This pass reviewed the shared route/shell and
deployment boundary, not all 201 page and service changes:

- The local route and module registries omit the `/operational-brain` route
  and navigation entry that exist on `main`; the local shell also removes
  module-specific document-title updates and the associated helper/tests.
- Local mobile-navigation focus handling focuses the first navigation entry
  whenever the drawer remains open. The canonical implementation checks the
  invoking/focused element before moving focus, avoiding focus theft while the
  user is already interacting inside the drawer.
- The local `nginx-config/nginx.conf` is absent from `main`. It contains a
  literal `change-this-local-backend-key` header value and a 752 MB upload
  limit. It was not copied into the PR; route, body-size, and secret handling
  require a separate review against the canonical gateway configuration.
- `.env-backend`, `.env-gateway`, and `.env-idp` are tracked in the stale local
  branch and absent from `main`. Their contents were not opened, copied,
  scanned into this ledger, or added to the PR. Treat them as sensitive local
  configuration, not cleanup candidates, until an owner-approved secret
  inventory and retention decision exists. The old PR #36 branch later removed
  these paths in commit `5815eeb5`, but that commit is not an ancestor of
  `main`; the current `main` tree already lacks the files and ignores all three
  names. The removal commit also weakens `.gitignore` protections for backups
  and personal fixtures and deletes CI assertions for redacted startup logs,
  frontend healthchecks, private subnet defaults, and Compose entrypoint
  contracts. Do not cherry-pick it wholesale. Its prior Git history may still
  contain the old blobs; this audit did not inspect their values or establish
  whether any were live credentials.
- The two owner/workspace deduplication migration pairs are identical in
  content but locally renamed from canonical sequence `0006` to `0004`.
  Preserve `main`'s migration numbering to avoid sequence collisions.

No frontend, gateway, environment, or migration variant from this batch was
ported. The remaining frontend page/service diffs still need focused review;
the 201-path count is not a full frontend audit or evidence that all relevant
local work has been integrated.

The tracked-tree diff also reports deletions for some paths whose exact
`main`-identical content exists as untracked local files. Blob-hash checks
confirmed this for the safe-return-URL helper, Skills page/service, and
Not-found page. These are duplicate copies, not missing physical files in the
working tree. By contrast, the Operational Brain component file is physically
absent locally, and its route/navigation entries are also absent. Continue to
use the tracked plus untracked hash inventory together when reconciling files.

### Frontend service contract batch

Compared the locally modified service implementations with their `main`
counterparts and the canonical frontend contract tests:

- `SystemStatusService` locally redirects detailed readiness from the
  authenticated `/api/v1/system/readiness` endpoint to public `/readyz`, and
  drops build/system information, event-delivery controls, and connector
  status. The canonical spec explicitly asserts the authenticated route and
  the related status methods; keep the `main` implementation.
- `TaskPlanService.logs()` locally removes the `limit=10` default and returns
  completion-plan values instead of task-history items. The canonical service
  spec asserts the bounded request; restore neither the unbounded read nor the
  mismatched response type.
- `AmbientService.overview()` locally removes its `full | summary` selector.
  The Control Center calls the summary mode and the canonical service spec
  covers it, so the local variant can force an unnecessarily large response.
- `GovernanceControlService` locally removes the domain-pack summary
  normalization that validates identity and sanitizes `methodCount`; the
  canonical service keeps that guard.
- The tracked local branch omits several main-only service files and tests
  (including view-refresh, Skills, OpenClaw artifact operations, and
  operational-graph paths). Some exact files are present as untracked copies;
  for example, the safe return-URL helper is imported by the local guards.
  Reconcile the complete tracked-plus-untracked import graph before treating a
  tracked deletion as a missing physical capability.

This was a bounded interface/contract audit, not a full test run of the stale
frontend. No service implementation or test was ported from this batch.

## Non-ignored untracked-file size inventory (2026-10-13)

Confirmed remote `main` remains `3b15fe4a08b10bb2e15afe15640bd6488cebff5d`.
The primary checkout reports 2,475 non-ignored untracked files, all confirmed
present with .NET filesystem metadata, totaling 137,007,708 bytes. The largest
area is generated `output/` evidence: 1,570 files, 126,970,727 bytes. Other
largest areas are backend (670 files, 5,404,455 bytes), services (33,
2,671,972 bytes), frontend (87, 727,294 bytes), scripts (39, 536,150 bytes),
IDP (58, 273,498 bytes), `.claude-flow` (3, 227,553 bytes), and docs (8,
151,013 bytes). This pass read file metadata only; raw output logs and local
agent state were not uploaded to the code PR.

The existing exact-blob comparison separately identifies 852 untracked files
already byte-identical to `main` and 351 modified tracked files with exact
`main` blobs, totaling 13,584,997 bytes. Those counts are not an authorization
to remove files: the local-cleanup contract remains read-only and reports
`cleanup_authorized=false`. No untracked file, environment file, output log, or
agent state was deleted.

## 2026-10-10 PR update

The existing PR #36 was updated by a fast-forward only. Immediately before the
push, `origin/codex/hai-runtime-release` resolved to
`2691b54deeeb6aaff32021d8bf8d17f2b1453edd`; the pushed head is
`60c7f6595dbb56cf08e86770307f2136675827e3`. The PR remains open against
`main`; no merge or force-push was performed. The update contains 19 explicit
source/documentation paths. Untracked CI logs, test evidence, scanner binaries,
archives, and local patch files were excluded and left untouched.

The update records scanner-fixture hardening, an isolated acceptance guide,
and the current transcript integration limits. Local backend, IDP, frontend,
production-build, and directory secret-scan checks passed before publication.
GitHub Actions has started a new run; its jobs were pending when checked. Do
not treat the PR as accepted until the required remote jobs complete and any
failures are resolved. The 20.7 GB transcript archive remains protected. The
source archive was reverified on 2026-10-10: 18 files and 20,739,169,122
logical bytes are present; the eight candidate files (7,939,888,699 bytes)
match their recorded hashes, and the eight candidate IDs match the crosswalk.
The ten retained files remain intact. Cleanup readiness is false because the
PR is open and required CI is failing; no deletion was performed. The
542-text count above remains unattributed and must not be confused with this
verified archive inventory.

### Acceptance-stack repair follow-up

The PR branch was advanced by a fast-forward from `4400b35d5cc67876c71d9dee0b43c20f8a7b6135`
to `7ecbc9aa03dc0aa20e22c19cd6f922526732216d`. The repair removes the
production-only `backend-state-permissions` dependency from the disposable
acceptance configuration and gives its private tmpfs state directory the
backend runtime UID/GID. The validator now rejects any remaining dependency
that references a service outside the isolated stack.

Local verification passed `pwsh -NoProfile -File
scripts/test-isolated-acceptance-stack.ps1` and `docker compose ... config
--quiet` against the generated disposable Compose file. The exact-commit
GitHub Actions run `38042681986` was still in progress at the time of this
entry; local validation is not remote CI acceptance. The PR remains open and
unmerged. No transcript or diagnostic archive was deleted.

### Archive verification and current publication state

On 2026-10-10, the read-only cleanup-readiness verifier was run with
`-TranscriptRoot D:\codex-temp\hai-completed-agent-sessions
-RequireSourceArchive`. It returned `source_archive_verified`, 18 manifest
rows, eight candidate files, ten retained files, eight candidate crosswalk
IDs, and `deletion_performed=false`. Its cleanup gate remained false because
the committed-ledger and merged-PR requirements are not satisfied by the
current publication state.

The remote branch is `1f12ef977bf43324f09ea27df16dc22c34713f9f`. PR #36
remains open against `main`, with no merge commit. The immediately preceding
exact-head Actions run `38043642051` failed its repository secret scan,
Windows environment-migration DACL regression, real-Postgres migration job,
two-account and authenticated smoke tests, browser acceptance, and Promptfoo
production dependency audit. The Windows migration and smoke-auth failures
have since received a focused local repair in `1f12ef9`: the migration now
verifies both the original DACL and complete file contents before removing
rollback data, and the shared smoke-token helper emits the claims required by
the backend verifier. Locally, the OpenClaw migration fixtures, seven smoke
auth contract checks, Bash syntax checks, Go identity tests, and staged diff
whitespace checks passed. These local checks do not establish remote CI
acceptance. Exact-head Actions run `38044450685` is queued. The remaining
history-wide secret findings, migration integration, two-account smoke,
browser acceptance, and Promptfoo dependency audit still require review of
that run and any follow-up. Local archive integrity does not override these
release gates; no transcript deletion is safe yet.

### Full patch-event crosswalk follow-up

A streaming pass over the large Carson transcript
`rollout-2026-07-30T11-17-55-019fb250-eabe-7c82-a488-19e4541375f0.jsonl`
found 3,574 `patch_apply_end` events (3,570 successful and four failed) and
695 distinct repository paths. This is an event/path inventory, not 3,574
independent requirements or proof that each patch was accepted. Comparing the
latest source-path dispositions with the current PR tree found no missing
tracked deletion. The previously untracked successful additions were:

- `backend/internal/browserverify/handler_test.go`: the workflow-link test was
  integrated with the browser-verification API change. CI caught an
  initialization-order compile defect in the first publication; commit
  `ff892b46` moves route setup until after the workflow service is constructed.
  Exact-head CI is pending; hosted Go tests have not yet verified this repair.
- `services/searxng/settings.yml`: integrated with the opt-in
  `research-discovery` Compose service, private backend network, separate
  SearXNG egress network, bounded resource settings, and activation docs in
  `ff892b46`. Local Compose config and 107 source-contract tests passed. This
  does not establish that the container successfully starts or returns live
  search results. The image tag was checked against the upstream GHCR package
  listing on 2026-10-10.
- `.env.local`: local credentials/configuration; deliberately not copied into
  source control.
- `mini-swe-workspaces/.gitkeep`, `mini-swe-workspaces/.gitignore`, and
  `security-snapshots/.gitignore`: ignored/generated workspace roots; these
  are not product behavior and remain local-only.

The archived transcript also changed `.git/info/exclude` and generated
`security-snapshots/README.md`; neither is part of the product patch. These
local artifacts remain untouched. Other paths in the 695-path event set were
compared by latest archived source disposition against the current tracked
tree; repository presence does not prove semantic equivalence or live
acceptance. The other 17 transcripts have not received the same exhaustive
patch-event-to-source crosswalk, and the six retained histories still have not
received a line-by-line semantic review. Therefore, the 18-file archive is not
fully integrated or eligible for removal.

At the time of this entry, PR #36 is open at
`ff892b46ea663c1bac7f05863a6c83537800bb09`. The preceding exact-head run
`38060350008` failed because the browser workflow linker was referenced before
`workflowService` existed; backend build, two-account, browser, and authenticated
smoke jobs consequently failed. The same run also reported 27 history-wide
secret-scan candidates and ten high-severity Promptfoo production dependency
advisories. These independent failures remain for review; do not suppress the
scans or claim acceptance. The new exact-head CI run is queued. No transcript,
diagnostic artifact, local credential, worktree, or database was removed.

### Exact-head CI failure evidence

The PR branch was advanced through `ff892b46` and `ab102d17`; the latter is the
head checked by GitHub Actions run `38061021277` (`ab102d178f5e6717fa7088fa06f4076680d13010`).
At this entry, the run's browser-acceptance job is still in progress, so the
overall run has no final conclusion. Backend, IDP, and frontend build/tests;
Postgres migration integration; two-account isolation; Compose validation;
cleanup-readiness contracts; provider fixtures; and the completed runner
contract jobs passed on this head.

The completed failures have distinct evidence and must not be collapsed into a
single environment issue:

- `Authenticated control-plane smoke`: background operations reported 17
  passed and five failed. After the exact safe-source approval, no verified
  artifact/runtime completion was observed. The Windows-runtime suite passed
  its pause/emergency-stop checks but its effect-bound resume request returned
  HTTP 403 with `control.execution.unavailable`; its final result was missing.
  The model-intelligence, runtime-lab, and account-bridge smoke suites passed.
  Root causes remain unconfirmed; the retained report is insufficient to
  distinguish execution rejection from a report-contract defect.
- `Windows installer preview and signing guards`: the Windows process,
  migration, installer, and 93 signing-contract cases passed, and the preview
  installer was produced. The install smoke then refused to run because it
  could not verify Docker engine state. This is an environment gate, not proof
  that the installed application passed.
- `Promptfoo safety runner image`: production dependency audit found ten high
  and zero critical advisories in the locked Promptfoo 0.124.1 tree. The audit
  identifies vulnerable transitive packages; its proposed Promptfoo 0.116.7
  change is a major downgrade. No downgrade, advisory suppression, or
  unverified override has been made. Upstream registry/advisory review and a
  compatible remediation remain necessary.
- `Repository secret scan`: the full-history scan reported 27 candidates and
  redacted values. Findings span test fixtures, workflow examples, and source
  files across prior commits. Their appearance in the history is not enough to
  call them false positives; no ignore entry or history rewrite has been made.

The browser-acceptance job's final state must be appended after run completion.
No transcript, local diagnostic, secret-scan candidate, credential, worktree,
database, or other local data was removed. The archive remains ineligible for
cleanup while the transcript crosswalk is incomplete and the PR is unmerged.

### Exact PR crosswalk refresh (2026-10-10)

PR #36 is open at `616c571e894a0c900e2385bdc96e7ab1d8f0f4fb`, based on
`main`. The local primary checkout is at `e07b9dae`, an ancestor of this PR
head; the PR contains 97 later commits. GitHub's remote ref was verified with
`git ls-remote`, so the stale local tracking ref is not used as publication
evidence.

A read-only content comparison against the exact PR tree classified the
primary checkout's 692 modified tracked files as 476 byte-identical to the PR,
153 differing only by CRLF/LF normalization, and 63 substantively different.
Of 897 untracked non-output product paths, 849 exactly match paths already in
the PR, 40 differ from the PR version, and eight are new local evidence files
under `backend/internal/outcomeevaluation/test-evidence/20260930-pg-snapshot/`.
The broader checkout has 2,475 untracked paths, including 1,570 under
`output/`; counts are an inventory, not a cleanup allowlist.

The differing local copies are not safe to bulk-port. Direct comparison found
examples that remove the exact-revision approval override for review reminders,
use the wrong operation-event payload column, remove source-owner binding from
approval receipts, restore a plaintext public-key literal, and remove
redaction from secret-scan output. The local Promptfoo package is also older
than the PR's pinned version. These examples are evidence for file-by-file
review, not a claim that all 63 tracked differences or all 40 untracked
differences have received semantic review. The eight new evidence records and
all other diagnostics remain local and unstaged.

Exact-head Actions run `38064692746` targets the PR head above. At this
checkpoint, 32 jobs passed, three failed, and the browser-acceptance job was
still running. Backend build/tests, real Postgres 17 migration integration,
Windows installer guards, frontend build/tests, and the cleanup-readiness
safety contracts passed. Failures were:

- Authenticated control-plane smoke: background operations returned HTTP 500;
  the suite reported 15 passed and seven failed. The Windows-runtime resume
  returned HTTP 403 with `control.execution.unavailable` even though its
  authorization receipt outcome was `authorized`. The safe diagnostic does
  not establish either root cause; no execution or approval gate was weakened.
- Promptfoo production dependency audit: 10 high and zero critical advisories
  in the locked 0.124.1 tree. The registry reports 0.124.1 as the current
  package version, and npm's suggested fix is a major downgrade to 0.116.7.
  No downgrade, override, or scanner suppression has been applied.
- Repository history secret scan: the log identifies a historical JWT-shaped
  test fixture at `backend/internal/identity/jwt_test.go:41` in commit
  `ecec8f55d449`. Its classification as synthetic versus sensitive has not
  been established from the complete findings; no finding was suppressed and
  no history was rewritten.

The browser-acceptance result must be added after run `38064692746` reaches a
terminal state. The source archive still contains 18 JSONL files totaling
20,739,169,122 bytes; the readiness inventory lists eight candidates and ten
retained files, but the six retained histories lack line-by-line semantic
review, the full archive crosswalk is incomplete, and the PR is not merged.
Therefore no transcript or source, worktree, diagnostic, credential, database,
or generated file is eligible for deletion at this checkpoint.

### Archive-to-Remote Crosscheck (2026-10-10)

The read-only transcript readiness verifier was rerun against
`D:\codex-temp\hai-completed-agent-sessions`. It verified all 18 manifest rows
against the source archive (20,739,169,122 logical bytes), including all eight
candidate hashes (7,939,888,699 bytes). Ten rows remain retained. The verifier
reported `source_archive_verified=true`, `deletion_performed=false`, and
`cleanup_authorized=false`; no archive file was changed.

The earlier statement that six retained histories contained no patch calls is
clarified: the shallow audit found no child-local patch calls, but a corrective
stream observed thousands of parent-history `patch_apply_end` events and
hundreds of paths in individual transcript files. In a sampled file,
`session_id`, `parent_thread_id`, and patch `turn_id` identify the pinned parent
session `019e7acc-44f2-7c90-a04e-253f6d43df28`. These repeated/shared records do
not prove that each child independently changed those paths. An initial path
comparison also failed to normalize a second HAI checkout root, so its
missing-path count is not valid evidence of unintegrated files. Child-local
attribution, event deduplication, and normalized source-to-PR reconciliation
remain incomplete. Do not use earlier “diagnostic only” wording or raw event
counts to authorize cleanup.

The sampled latest work reports repeat the guarded brain integrations and
disk/cache investigations already described above. Claims that code was
pushed were checked against the Git graph rather than accepted from transcript
text. The named RAGFlow, Presidio, Whisper.cpp, PydanticAI, FastMCP, Evidently,
Guardrails, and SearXNG commits are ancestors of both the live `main` ref
(`91c8620c`) and the earlier PR #36 head
(`da44f867d53df13030bed4c011cc31c25ddf1bbb`). The retained workflow/task and
Constitution-history commits `d3ad5606` and `29e97432` are ancestors of the PR
head but not of `main`; they are not merged product history yet.

This pass did not semantically review every assistant message, tool result, or
all shell activity in those six histories. The completed candidate reports and
source crosswalk remain the evidence for the other eight rows. Therefore this
crosscheck narrows the unresolved work but does not complete the full archive
integration audit or authorize deletion. Keep the ten retained histories and
all 18 source files until the full crosswalk, PR checks, and repository-history
cleanup gate pass.

At this checkpoint PR #36 remained open and unstable. Latest PR-head Actions
run `38065751414` had failures in authenticated control-plane smoke, repository
secret scanning, and the Promptfoo dependency audit; browser acceptance was
still in progress. The Windows-runtime authorization failure is not understood
well enough to change its safety gate. Treat this CI state as a timestamped
checkpoint, and refresh it before any merge or cleanup decision.

### Current PR and cleanup checkpoint (2026-10-10)

The current PR #36 head was re-verified as
`2ed55b26d2578ae80ac38a1a580d5ea844aa7791` on
`codex/hai-runtime-release`, targeting `main`. GitHub reports the PR open and
mergeable. The isolated worktree's tracked files are clean; 22 untracked
diagnostic/evidence artifacts remain and have not been staged or removed. The
separate primary checkout remains dirty and has not been modified in this
checkpoint.

Exact-head Actions run `38074032843` confirms backend build/tests, real
Postgres 17 migration integration, frontend build/tests, authenticated
control-plane smoke, Windows installer preview/signing guards, native runtime
regressions, cleanup-readiness contracts, and the other completed contract
jobs passed. The Promptfoo safety runner image failed its production dependency
advisory gate (10 high, zero critical in the locked tree); browser acceptance
was still running at the latest status check. No dependency downgrade, audit
suppression, or execution-policy relaxation was applied. Refresh this run and
resolve the upstream dependency finding before calling the PR green.

The source transcript archive remains at
`D:\codex-temp\hai-completed-agent-sessions`. Its 18-file, 20,739,169,122-byte
inventory was hash-verified, but hash verification is not semantic integration.
The earlier ledger records incomplete semantic review of retained histories
and crosswalk gaps; no new complete archive crosswalk was produced in this
checkpoint. Keep all source files and local diagnostics. The cleanup verifier
previously reported `cleanup_gate_ready=false` and `cleanup_authorized=false`;
no deletion is permitted until semantic integration, PR merge, and the
repository's explicit cleanup gates are all verified.

## Archive integrity and exact-head PR update (2026-10-10 18:58 UTC)

The eight completed-candidate path audit is still not a complete semantic
transcript integration. Two candidates have malformed JSONL records at exact
2-8 MiB byte boundaries: Aristotle has four and Descartes has fourteen
unterminated-string parse failures. Their recorded file hashes match the
archive manifest, but the malformed records cannot be decoded and have not
been reconciled. Keep the archive; do not infer integration for those records.
The separate recovery-tree and `hai-repo-link/` path normalization also remains
part of the crosswalk, with verified canonical Framework Registry equivalents
but at least one failed-and-removed test patch rather than an unintegrated
source file.

Read-only Memory Layer access to the synced parent Codex conversation
(`019fd0df-32c2-79b0-aae3-a9d3c566575b`, message IDs 21646, 21648, 21652)
recovered the final reports for Aristotle and Descartes. All 17 unique paths
listed in those reports are present in current PR `HEAD`: 14 backend/docs
Framework Registry paths, its frontend template, and the automation form HTML
and SCSS. This closes source-presence checks for those report outputs only;
the malformed raw records and remaining recovery/scratch path union remain
unresolved. Aristotle's report explicitly says Postgres integration-tag tests
compiled but runtime assertions were skipped because its `HAI_TEST_DATABASE_DSN`
was unset. Do not count that as a live database test.

PR #36 is open and mergeable at `c4d2aa752203df0ed17373cf1da496f29c95d989`.
Exact-head run `38076417111` is still running browser acceptance; Windows
installer preview/signing guards and the Promptfoo dependency audit have
failed, while backend, frontend, IDP, Postgres migration, authenticated smoke,
secret scan, and cleanup-readiness checks passed. Local `npm audit
--omit=dev --audit-level=high` independently reports ten high and zero
critical findings in Promptfoo `0.124.1`; npm's only automatic fix is a major
downgrade to `0.116.7`, which has not been adopted without compatibility
validation. The protected-recovery CI script set passed locally under
PowerShell 7.6.5; GitHub's failed Windows job still needs its own logs after
the workflow reaches a terminal state. This checkpoint does not authorize
merge or local deletion. The archive, dirty primary checkout, and untracked
diagnostics remain intact.

## Windows CI Docker-probe regression (2026-10-10 19:10 UTC)

Run `38076417111` reached a terminal failure. Its Windows recovery contract
logs show the failure came from the nested restore test invoking the real
`backup-windows.ps1 -ValidateOnly` callback, which attempted a Docker context
inspection. The Windows CI runner had the Docker CLI but no available local
engine. This was a test-isolation defect; the check failed before changing any
containers or volumes. Promptfoo's separate audit failure is confirmed as ten
high, zero critical production dependency advisories in the locked `0.124.1`
tree.

The recovery implementation now separates pure Docker-context validation
from the production bounded CLI probe. Contract mode supplies synthetic local
or remote context metadata and validates the recovered environment fields
without invoking Docker. The exact ten-script Windows recovery contract block
passed locally under PowerShell 7.6.5 with Docker removed from the child
process `PATH`, including failed, timed-out, malformed, and remote context
cases. This reproduces the CI constraint, but the post-fix GitHub run is still
required. No runtime policy was relaxed, and no Docker resources, archives,
checkouts, or diagnostic artifacts were removed.

## Full Primary-Worktree Reconciliation (2026-10-10)

The primary local checkout (absolute path omitted) is the same repository and
branch lineage as PR #36, not a separate product repository. Its `HEAD` is
`e07b9daeb3ba2630ecbe12a64d948f791203c241`, which is
the merge base of the PR worktree's current `HEAD` (`00b84408ecfcc3562b59234aa5c918e6b06fa695`).
The PR contains 120 commits after that base. A read-only inventory of the
primary checkout found 3,167 status entries: 682 modified tracked files and
2,475 untracked files. The tracked diff is 142,921 insertions and 17,804
deletions. Nothing in the primary checkout was staged, overwritten, or
deleted.

The tracked diff was three-way compared against the PR. The cleanly applicable
paths produced no additional tracked diff at PR `HEAD`, which confirms that
those changes are already represented in the PR. There were 47 overlapping
files. The primary-checkout variants remove or weaken later PR behavior,
including cleanup CI gates, Go toolchain alignment, paused-worker handling,
source-approval identity and evidence checks, workflow success criteria,
Windows recovery protections, and bounded secret-history scanning. The PR
versions were retained for those overlaps; the primary checkout remains intact
as the recovery source. The PR worktree has no remaining tracked diff from this
reconciliation.

For the 2,475 untracked primary-checkout paths, a path-and-content comparison
identified 1,383 source-like files: 816 matched an existing PR file exactly,
46 had an existing PR path with different content, and 521 were absent from
the PR. All 521 absent paths were under hidden `output/` directories or agent
state directories (`.claude-flow/`, `.swarm/`); no missing source path outside
those generated/evidence areas was found in this comparison. The 46
same-path differences were treated as competing versions, not assumed to be
newer. Targeted reviews found local alternatives that remove or weaken safety
behavior; the existing PR versions remain authoritative for those reviewed
cases. A complete semantic review of every differing output/evidence file is
still outstanding. The raw local files are preserved.

The hidden `output/` tree contains 1,576 files totaling 126,970,802 bytes;
the full untracked primary-checkout inventory totals 137,007,708 bytes. These
are mostly JSONL command transcripts, logs, structured receipts, and recovery
test artifacts. They are not being committed wholesale because that would
publish raw operational/test data and duplicate generated artifacts; their
relevant conclusions must be represented by reviewed, redacted evidence in
the repository before cleanup can be considered. The separate child-session
archive remains 18 JSONL files totaling 20,739,169,122 bytes. Its two
malformed candidates, incomplete semantic crosswalk, and report/runtime
verification gaps remain open as documented above.

At this checkpoint, PR #36 remains open and mergeable at
`00b84408ecfcc3562b59234aa5c918e6b06fa695`; exact-head run `38078840727` is
still in progress. The existing untracked CI logs, scanner downloads, evidence
fixtures, local patch, primary checkout, and transcript archive remain
untouched. This reconciliation does not satisfy semantic transcript
integration, PR merge, or cleanup-readiness gates and does not authorize local
deletion.

## Completed-session transcript structural pass (2026-10-10)

A bounded, read-only scan of the 18 files in
`D:\codex-temp\hai-completed-agent-sessions` covered 20,739,169,122 bytes
(about 20.7 GB decimal). It parsed 527,872 JSONL records and found 45,017
`patch_apply_end` records: 44,941 marked successful and 76 marked failed.
Those events contained 793 distinct absolute target paths and 5,130 distinct
unified-diff fingerprints. The path references were concentrated in the
primary HAI checkout (68,756 references); 1,214 references targeted the
separate `work\018-hai-port-engine-control` scratch checkout, and 325 targeted
temporary audit/smoke scripts. These are reference counts across patch events,
not counts of unique files or accepted changes.

Two 67 MB compacted-summary records exceeded the parser's 64 MiB per-record
limit. Their record type was `compacted`, and a chunk-wise scan found no
`patch_apply_end` marker in either. Every under-limit record containing a
`patch_apply_end` marker parsed as JSON; this is not a claim that every record
in the archives is valid. The previously identified malformed/truncated
candidate records remain unresolved. The transcripts were not changed.

This pass establishes a structural map of attempted edits, not a semantic
approval or complete integration proof. Failed patch attempts are not product
changes; repeated diffs are not independent contributions; temporary browser
scripts are not product source; and scratch-checkout changes still need to be
compared with the canonical PR. Only the 17 unique paths named in the
Aristotle/Descartes final reports have a completed source-presence check so far.
The 793-path semantic crosswalk, including patch intent, final surviving
version, tests, and disposition for every unique product path, remains open.
The two compacted summaries and the malformed/truncated candidate records also
remain retained. No archive, output, scratch checkout, or diagnostic data is
cleanup-ready on this evidence.

At the time of this transcript pass, PR #36 is at `cb5996a831abc9ac9aefdf1e9f43d73e97dfd598`.
Public GitHub Actions run `38080242458` for that exact commit is still in
progress; its Promptfoo safety-runner image job has failed at the production
dependency audit step. The GitHub app connector requires reauthentication, so
the run's final status and remaining job results are not yet verified. This
replaces earlier checkpoint references above to older PR heads and runs; those
entries remain historical snapshots, not current status.

## Current cleanup and CI gate (2026-10-10)

The integration ledger for the same July 30 archive identifies eight completed,
unique-ID candidate transcripts totaling 7,939,888,699 bytes, and ten files
that must remain retained totaling 12,799,280,423 bytes. The candidate set has
preserved final reports and a source crosswalk; the retained set includes
duplicate-ID, aborted, and nonterminal transcripts. This is a limited cleanup
scope, not authorization to delete the full 20.7 GB archive. No candidate or
retained file has been removed.

After the ledger update, the PR branch advanced to
`5d8c6abc54dfab7ed1ab028b05e203993e46a269`, confirmed equal to the remote
`codex/hai-runtime-release` ref. GitHub Actions run `38082211825` (`#783`) is
for that exact commit. A direct read of the public GitHub Actions API at
2026-10-10 20:07:32 UTC reports 36 jobs: 34 succeeded, one failed, and browser
acceptance is in progress. The failed job is `Promptfoo safety runner image`; its
`Audit production dependencies` step failed and the runner contract step was
skipped. The workflow has not reached a final state. A local reproduction
using the locked Promptfoo
`0.124.1` package and CI-pinned npm `10.9.8` reports ten high and zero critical
production dependency advisories. The only automatic npm remediation offered
is a major downgrade to Promptfoo `0.116.7`, while the current registry's
`latest` tag is `0.124.1`; no downgrade or audit suppression has been applied.
The candidate cleanup gate therefore remains false. Recheck all PR checks at
the latest head after resolving the upstream dependency security issue without
weakening the gate. The run's pending browser acceptance and the eight
candidate transcripts' malformed/truncated records also remain open gates.
Revalidate candidate hashes and cleanup boundaries before considering any
deletion; no archive or diagnostic files have been removed.

## Primary-checkout source comparison refresh (2026-10-10)

The primary checkout remains at `e07b9daeb3ba2630ecbe12a64d948f791203c241`;
the PR worktree is at `b6c0e0ecb365831bfc0595cb4944d9454f8ccb1c`. A fresh,
read-only SHA-256 comparison matched each path explicitly across 874 untracked
primary-checkout files with source/documentation extensions, excluding the
generated `output/`, `.claude-flow/`, `.swarm/`, and local outcome-evaluation
evidence trees. Of these, 825 are byte-identical to the PR, 49 have the same path
but different contents, none are missing from the PR, and none were unreadable.
This narrower source-like inventory is not directly comparable to the earlier
2,475-path/1,383-source-like inventory above because that audit used a broader
classification and excluded different generated areas.

The 49 same-path differences remain competing versions and are not automatically
integrated. A safety-focused sample found the PR versions include later source-
approval owner/revision binding and evidence-digest checks, approval-claim
handling aligned with the persisted event schema, and timestamp-preserving
compare-and-swap updates. These spot checks support retaining the current PR
versions; they do not complete the semantic review of all 49 files or the
793-path transcript patch crosswalk. No primary-checkout files were changed.
The transcript archive, diagnostic artifacts, and cleanup gates are unchanged;
no deletion is authorized by this comparison.

## Bounded transcript-tail reconciliation (2026-10-10)

A second read-only pass sampled at most the final 16 MiB of each of the 18
transcripts (288 MiB maximum read). It parsed 5,389 complete JSONL records,
with no parse errors in the sampled tails, and found 117 patch events (94
successful, 23 failed) across 11 transcript files. Those events referenced 71
distinct absolute target paths. Fifty-five paths mapped unambiguously to HAI
repository-relative source paths; all 55 target paths exist in the current PR
checkout. The remaining 16 paths point to recovery/audit scratch artifacts or
other non-canonical paths.

This tail sample is not a whole-transcript review. Path presence does not prove
that the corresponding diff, intent, or final state was integrated, and the
sample may include inherited parent-history events. The earlier 793-path
semantic crosswalk and transcript attribution work therefore remain open.
The 20.7 GB source archive and all local diagnostics remain untouched.

The current PR head is `9dc9cf27c4f3c7b034fc2726903df0868881a100` and remains
open against `main`. At the latest check, exact-head Actions run `38083647651`
has 34 successful jobs, the `Promptfoo safety runner image` job failed, and
`Browser acceptance (owned disposable stack)` remains in progress. The
Promptfoo dependency-audit step passed; the Docker build/isolated-contract-test
step failed. The public GitHub API returns job metadata but denies access to
the job log (HTTP 403), so the underlying Docker failure is not yet established.
No failure suppression or speculative workflow change was made. Transcript
cleanup remains gated on completing the semantic integration review and
passing the PR/cleanup readiness checks.

## Cleanup and snapshot verification refresh (2026-10-10)

A fresh read-only `-RequireSourceArchive` verification parsed all 18 manifest
rows, matched the 18 source files and 20,739,169,122 logical bytes, and
recomputed the hashes for all eight candidate transcripts (7,939,888,699
bytes). The ten retained files total 12,799,280,423 bytes. The verifier
reported `source_archive_verified=true` and `deletion_performed=false`; it
reported `cleanup_gate_ready=false` because the check was not on canonical
`main` and PR #36 is not merged. The combined local inventory also continues to
block PR diagnostics (15 files, 31,505,884 bytes) until merge, flags an
unrecognized/anonymous Docker mount, and requires preserving the active PR and
secondary checkout. No source, diagnostic, Docker, or transcript data was
removed.

The `hai-integration-snapshot` index has 2,917 tracked paths compared with
2,923 paths in PR `HEAD` `9200ae3f2f1b9a02925c883a11f400d3cb96bf7e`: 2,835
blob IDs match, 82 same-path blobs differ, and no snapshot-index path is absent
from the PR checkout. A separate read-only scan of its 19 unstaged paths found
four already byte-identical to the PR, twelve differing only in line endings,
two substantive code/script changes already present in the PR, and one newer
ledger checkpoint not yet integrated. This establishes path coverage, not a
semantic review of all 82 competing versions; the complete semantic comparison
is still open.

The detached `hai-merge-scratch-20261010` checkout remains unmerged with 189
conflict paths. All 189 paths exist in the PR checkout; 181 PR blobs equal the
scratch stage-2 blobs, while eight are distinct from all three conflict
stages. The scratch checkout remains untouched, and those eight versions have
not been declared redundant pending targeted review.

At the live check, PR #36 was open on `codex/hai-runtime-release` at
`9200ae3f2f1b9a02925c883a11f400d3cb96bf7e`. Exact-head Actions run
`38085176170` had 35 completed jobs, no failures, and browser acceptance still
in progress. This is a timestamped observation, not a passing-run result.
Cleanup remains prohibited until the full semantic crosswalk is completed,
checks pass on the final PR head, the PR is merged into `main`, and the cleanup
script's source/ledger/merge gates are revalidated.

This review also found three obsolete root `.env-*` files tracked in the public
repository. Current tracked runtime/Compose sources do not consume them, the
supported local setup uses ignored `.env.local` from `.env.example`, and the
Windows installer explicitly excludes those files. They are removed from the
PR's working tree, ignored going forward, and covered by a regression check.
This removes them from the proposed current tree only; it does not rewrite
public Git history or affect the separate primary checkout. If any of their
short default-like values were reused in an actual deployment, rotate those
credentials independently.

## Retained completion-event audit (2026-10-11)

The six retained transcript paths associated with the disputed completion-text
count were resolved from the July 30 manifest and rescanned with the
read-only `scripts/audit-hai-retained-completions.ps1`. The script verifies
each requested path is a retained manifest row under the expected archive
root, checks its byte length against the manifest, streams JSONL records, and
prints counts derived from message hashes only; it prints neither message
contents nor hash values and does not modify the archive.

Four large aborted histories each contain 580 `task_complete` records and 542
non-empty final messages. The two small aborted histories contain no
`task_complete` records. Across all six files, the audit counted 2,320
completion records, 2,168 non-empty final messages, 542 unique trimmed-message
hashes, and zero parse errors on matching records. The reproducible command
accepts the six `session_path` values from the manifest and
`-TranscriptRoot D:\codex-temp\hai-completed-agent-sessions`.

This establishes archive provenance and exact-text deduplication only. It does
not establish that 542 distinct tasks occurred, that any message describes a
successful change, or that every described change is integrated. The larger
793-path patch-event crosswalk, per-event child attribution, and semantic
comparison to final source remain open. All 18 archive files remain preserved;
this finding does not authorize removal.

## Primary-checkout source recheck (2026-10-11)

A fresh, read-only hash comparison used `git ls-files --others
--exclude-standard` in the primary checkout and the extensions `.go`, `.ts`,
`.html`, `.scss`, `.css`, `.json`, `.yaml`, `.yml`, `.md`, `.ps1`, `.sh`,
`.sql`, `.mod`, `.sum`, `.toml`, `.txt`, `.xml`, `.proto`, and `.conf`. It
excluded `output/`, `.claude-flow/`, `.swarm/`,
`backend/internal/outcomeevaluation/test-evidence/`, `node_modules/`, `dist/`,
`coverage/`, `playwright-report/`, `test-results/`, `.angular/`, and `.git/`.
It selected 833 files; 43 had the same relative path but different bytes from
this PR worktree, and none were missing from the PR. This is a narrower
population than the earlier 874-file/49-difference comparison and should not
be combined with those earlier totals.

A focused review compared `backend/internal/openclawmaintenance/pull_client.go`,
`backend/internal/openclawmaintenance/worker.go`,
`backend/internal/operations/source_approval.go`,
`backend/internal/operations/source_approval_test.go`,
`backend/internal/operations/execution_claim.go`,
`backend/internal/operations/execution_claim_memory.go`, and
`backend/migrations/protected_data_rollback_postgres_integration_test.go`.
The PR versions already include stricter lowercase-hex validation for pulled leases;
a byte-encoded pinned OpenClaw signing key to avoid secret-scanner false
positives; source-approval owner and raw-evidence digest binding; PostgreSQL
`payload_json` receipt lookup, exact-approval precedence over a future review
reminder, and timestamp-preserving compare-and-swap persistence; plus the
missing `post/` fixture directory for rollback integration tests. These are
already present on the PR and require no port from the primary checkout. The
remaining same-path differences were not semantically reviewed by this sample;
no wholesale copy is justified, and no primary-checkout file was modified.

## Primary-checkout diff triage (2026-10-11)

A fresh content diff of the 43 same-path files from the scoped primary-checkout
inventory was reviewed across approval/claim tests, migration rollback tests,
synthetic-stack guards, Windows installer smoke tests, and runner manifests.
This is a bounded semantic triage, not a claim that all 43 files have been
individually accepted or that the wider transcript/snapshot crosswalk is done.

The reviewed primary-checkout variants are not safe to copy wholesale. In
particular, `worker_reliability_test.go` removes the assertion that newly
ingested source work awaits approval; operation-ingest, repository-ordering,
and phase-2 claim test variants remove source-evidence digest, deterministic
claim-order, or approved-source-claim coverage. The migration-test variants
remove or relocate checks for later-phase rollback refusal and data-preserving
rollback behavior. Keep the stricter PR versions unless an isolated, stronger
replacement is implemented and verified.

The primary-checkout isolated-acceptance variants remove dependency validation,
the interrupted-preparation cleanup manifest and its hash binding, synthetic
credential scrubbing, and explicit ownership for private backend tmpfs state.
Its installer-smoke variant writes to the runner's normal LocalAppData profile
instead of keeping generated HAI state inside the uniquely owned smoke profile,
and relaxes the runner-temp boundary checks. These are safety regressions, not
acceptable ports. The primary Promptfoo runner manifest also downgrades the
pinned Promptfoo version and removes its `get-uri` override; do not substitute
it without a separate dependency/security review.

No files from this primary-checkout diff sample were copied into the PR by this
triage. It establishes concrete reject/retain decisions only; the remaining
same-path diffs, all 82 snapshot blob conflicts, the eight scratch-only
variants, and the full transcript semantic crosswalk remain open. Local source,
transcript, and diagnostic data remain preserved.

## Primary-checkout tracked working-tree comparison (2026-10-11)

The primary checkout is at `e07b9da` with a dirty working tree; the current PR
checkout is at `4343a89`. A read-only comparison found 682 tracked paths changed
relative to the primary checkout's `HEAD`. Every path exists in the PR. Exact
byte hashes differ for 678 paths, but after normalizing CRLF to LF, 608 paths
are byte-equivalent and only 74 still differ in content. The primary tree has
mixed LF/CRLF files while the PR worktree is CRLF, so raw hashes and ordinary
diff statistics substantially overstate the amount of unique work.

The 74 content-different paths are competing versions that still need
file-by-file semantic triage; none were copied in this comparison. A review of
`backend/internal/frameworkregistry/repository.go` found the PR version adds
owner-scoped, digest-chained preference history and transactional writes that
are absent from the primary checkout, so the older local variant must not
replace it. This spot review is not evidence that the other 73 differences are
resolved. The primary checkout was not modified, and these observations do not
authorize removal of its files or any source data.

## Primary-checkout tracked diff refresh (2026-10-11)

A fresh read-only comparison against PR worktree `3c62dcb2` found 682 tracked
paths changed in the secondary checkout. The paths exist in both trees. After
normalizing CRLF and LF, 607 are equivalent and 75 have substantive content
differences. This is a new checkpoint, not a cumulative count: it supersedes
the previous 74-difference snapshot because the outcome-evaluation integration
test has since changed to share its request across concurrent goroutines.

Targeted review reconfirmed that the PR version of
`backend/internal/background/worker.go` contains paused-mode deferral and
emergency-stop gates absent from the secondary checkout;
`backend/internal/pursuit/service.go` propagates success criteria through
intake; `frontend/src/app/pages/framework-registry/framework-registry.component.ts`
loads and displays durable preference history; and
`docs/optional-runtime-profiles.md` documents the opt-in SearXNG profile and
network-overlap recovery. The secondary outcome-evaluation integration test
uses per-goroutine copied requests, while the PR version intentionally shares
an immutable request to exercise the newly fixed production race. The PR's
browser acceptance test now expects a fail-closed runtime preflight and tests
the persisted review state; the secondary version still expects an approval
path unavailable in the isolated stack. These checked variants do not justify
copying secondary-checkout content into the PR.

The `.env.example` comparison was performed on variable names only; no values
were read into the report or printed. The secondary checkout has no unique
environment keys. The PR adds `HAI_SEARXNG_SECRET`,
`HAI_A2A_LOCAL_SUBNET`, and `HAI_HOST_RUNTIME_INTERNAL_SUBNET`; the matching
Compose, setup, and profile documentation were inspected in this pass. This
does not validate any machine's `.env.local` contents or authorize deleting
local configuration.

This targeted review does not semantically resolve the other differing files.
The 75-file inventory remains open for file-by-file triage; no secondary
checkout file was modified or deleted. The transcript, scratch, diagnostics,
and cleanup gates are unchanged, and no local deletion is authorized by this
checkpoint.

## Merge-scratch conflict comparison refresh (2026-10-11)

A read-only comparison of the 189 unmerged paths in
`D:\codex-temp\hai-merge-scratch-20261010` against the current PR tree shows
172 PR blobs still equal scratch stage 2 and 17 PR blobs distinct from all
three conflict stages. The earlier eight-path count was measured against PR
snapshot `9200ae3f`; it is historical and not a count of current unintegrated
scratch work. The current 17-path set includes `.env.example`,
`.github/workflows/ci.yml`, `backend/internal/phase2/handler.go` and its test,
`backend/migrations/migration_chain_contract_test.go`,
`docker-compose.local.yml`, `docs/optional-runtime-profiles.md`, the three
workflow-engine component files, two auth service contract files, and the
Windows backup/initializer/restore/recovery contract files plus
`scripts/test_ci_contract.py`.

Targeted comparison confirms the PR versions include the later profile/network
configuration, workflow success-criteria UI and migrations, explicit void
typing for the login API, test-only phase-2 diagnostics guards, and bounded
Windows Docker process/recovery checks. These are later PR changes, not
evidence that scratch content is missing. The remaining differing paths still
need disposition; neither checkout was altered by this comparison, and the
scratch state remains preserved.

## Exact-head CI regression review and fixes (2026-10-11)

The completed GitHub Actions run `38093710608` tested the PR merge commit and
finished red. The Windows installer smoke built and installed its unsigned
preview, verified that silent setup created no HAI environment, task,
containers, or process, and confirmed that uninstall safely cancelled while
preserving the installation when no protected environment existed. Despite
those PASS assertions, the step exited with code 1 after the expected
uninstaller cancellation. The smoke script now resets the hosted runner's
stale native `$LASTEXITCODE` only after its full success path and cleanup;
static contract validation passes. The installer itself was not run on this
workstation.

The browser job's retained artifact showed a real UI-state defect in the
operator acceptance flow: the server returned a valid blocked result with
`reviewRequired=true`, but the UI paused safely without reloading the persisted
workflow, leaving its displayed state at `ready`. The selected-workflow path
now pauses the worker and reloads the owner-scoped workflow record and summary
before presenting the result. The focused Angular suite passes all 44 tests,
including a regression asserting that a persisted `needs_approval` state is
shown after the blocked result; the Windows smoke static contract also passes.

The browser job remains unresolved. Its full matrix reports missing
`E2E_MATRIX_UNCERTAIN_WORKFLOW_ID` and `E2E_MATRIX_UNCERTAIN_TASK_ID` records,
route tests redirected to onboarding before theme controls were available,
and at least one operation that remained pending during health sampling. These
are not waived or reclassified as passes. The run's earlier failed-result
acceptance check also timed out after observing a stale `ready` state; the
workflow-record reconciliation fix addresses that observed defect, but requires
a new exact-head CI run. PR #36 remains open and unmerged. No branch cleanup or
local deletion is authorized by this CI diagnosis; archive, working-tree,
scratch, snapshot, and diagnostic gaps remain open.

### October 11, 2026: exact-head browser run 38096541735

Run `38096541735` tested PR head `ab9e377a790866fd71cfee6dcf08f3cbc68a5856`.
Backend build/tests and the other non-browser checks passed; the owned-stack
browser job failed after 28 minutes. Its retained report contains 124 failed
cases: 114 route cases attempted to focus a theme control while the rendered
page was the login form, eight uncertainty cases failed because the required
owner-scoped record IDs were never provisioned, and two failures occurred in
the operator acceptance flow (pursuit matching produced no response, and the
blocked workflow had no approval controls).

The IDP log confirms authenticated-session checks returned HTTP 401 during the
matrix. The matrix now verifies the owner session before route navigation and
renews it only against the validated disposable loopback target when it has
expired or been invalidated. Its E2E TypeScript check passes; this change has
not yet been exercised in a new browser run. The two persisted uncertainty
fixtures are still not provisioned; their strict checks remain enabled. The
acceptance-flow failures also remain open and must be reconciled against the
actual blocked/review safety state, not hidden by loosening assertions. The
run report is retained locally at
`D:\codex-temp\hai-browser-report-38096541735` and in GitHub Actions. PR #36
remains open; the transcript cleanup verifier still reports
`cleanup_gate_ready=false`, so no transcript or worktree deletion was done.

### October 11, 2026: local remediation and verification

The pursuit-match button now uses the workflow actions' loading and
availability predicate, and its handler continues to reject matching while a
different action is running. A component regression test covers the loading
case. The browser acceptance path waits for the button to become enabled and
ends at the expected fail-closed blocked preflight: the isolated stack does
not provision verified participants, a capable model, or required framework
evidence, so the prior test's attempted approval and follow-on execution were
invalid. Those prerequisites were not fabricated and no safety gate was
relaxed.

Local verification on the PR worktree passed: the five control-room compiler
checks, E2E TypeScript check, all 175 frontend Node logic tests, all 1,331
Angular/Karma tests, production build, and `git diff --check`. The build retains
existing stylesheet-budget warnings in task blueprint, app shell, workflow
engine, and pursuits. The updated browser acceptance path and renewed matrix
session have not yet been exercised against a fresh owned-stack browser run;
the required uncertainty-record fixtures also remain missing. Therefore the
exact-head browser acceptance and transcript semantic crosswalk are still
open, PR #36 remains unmerged, and the cleanup readiness gate remains false.

### October 11, 2026: exact-head browser run 38098898912

Run `38098898912` tested PR head
`f6bc6967068e31bb5c96a15f812478c3c1b98e3c`. The repository checks completed
with 35 successful jobs; `Browser acceptance (owned disposable stack)` failed
with 122 failed cases. The retained report at
`D:\codex-temp\hai-browser-report-38098898912` contains 112 module-route
preference cases whose expected theme control was absent after navigation,
eight cases blocked by the still-unprovisioned owner-scoped uncertain workflow
and task IDs, and two keyboard/control-room cases. At least one matrix session
renewal returned HTTP 429; the IDP log also shows repeated logins for the same
synthetic owner. This confirms the added renew-on-401 path is not yet a valid
session-lifecycle fix. The earlier acceptance-test rewrite was typechecked but
was not evidenced as passing by this run. Keep all required fixtures and
assertions; do not treat the failed browser run as acceptance evidence.

No new files were removed. The transcript archive and candidate files remain
protected because the ledger is not yet fully integrated/committed and PR #36
is open, unmerged, and has a failing browser job. A targeted removal of the
local generated `frontend/dist/app` output was rejected by the command policy;
it was not retried through another deletion mechanism. Browser reports, source
archives, scratch/snapshot trees, test evidence, and unrelated untracked
diagnostics remain preserved.

### October 11, 2026: exact-head browser run 38100670744 and local checkout reconciliation

Run `38100670744` tested PR head
`9a89ce70b3b3643dbc62104d0227d549111ebe08`. All 35 non-browser jobs passed;
`Browser acceptance (owned disposable stack)` failed after 24 minutes. Its
retained GitHub artifact `hai-browser-acceptance-38100670744-1` was inspected:
122 failed test result directories comprise 106 module-route cases where the
expected theme control was absent and the rendered page was still onboarding,
eight synthetic-owner session renewals rejected with HTTP 429, and eight strict
uncertainty cases without the required existing owner-scoped workflow/task
fixtures. The missing fixtures remain failures, not passes. This run is not
acceptance evidence and PR #36 remains open and unmerged.

The secondary local checkout (absolute path omitted) is the same repository
and its `e07b9dae` HEAD is an ancestor of the PR head.
Its status contains 692 tracked modifications; after normalizing Windows line
endings, 608 match the current PR files and 74 contain real content differences.
It also has 2,475 untracked files (about 137 MB): 1,570 under `output` (about
127 MB, mostly JSONL/log evidence), source/test artifacts, and agent state.
Across those untracked paths, 895 paths also exist in the PR worktree: 841 are
byte-identical (about 7.9 MB), and 54 differ (about 1.4 MB). The remaining
1,580 paths are absent from the PR worktree; this includes four missing
`backend/internal/outcomeevaluation/test-evidence/20260930-pg-snapshot` records
(about 470 KB). The other six records in that snapshot directory are already
byte-identical in the PR worktree.
These counts were computed read-only. A separate exploratory path inventory
used a flawed quoted-path parser and was stopped; its partial output is not
evidence. No source changes were copied, and no files were deleted.

The 74 tracked content differences, 54 divergent same-path untracked files,
four missing test-evidence records, 1,570 output records, and private
`.claude-flow`/`.swarm`
state still require classification or integration. Exact duplicate files are
not being removed before that reconciliation and PR merge. Docker inventory
still has unidentified HAI-mounted volumes and unverified recovery state, so
no Docker data, container, image, checkout, archive, or diagnostic cleanup is
currently authorized by the safety gates. Transcript semantic crosswalk and
cleanup gate remain open.

### October 11, 2026: browser-matrix session lifecycle repair

The exact-head browser report showed that the long serial route matrix was
falling back to repeated password logins when its stored access cookie stopped
authenticating. The test helper now checks the same `/api/v1/auth/session`
endpoint used by the frontend. That endpoint validates the owner session and
uses the existing refresh-token cookie flow to rotate expired access
credentials; the helper no longer retries password login or risks exhausting
the IDP login throttle. Global setup now validates the newly established
session before writing its run-scoped storage state.

Local verification passed: E2E TypeScript typecheck, all 108
`scripts/test_ci_contract.py` tests, and `git diff --check`. This change has
not yet been exercised by a fresh owned-stack browser run. The onboarding
route failures and missing persisted uncertainty fixtures remain open until
that run provides evidence; no browser acceptance or cleanup gate is claimed.

### October 11, 2026: outcome-evaluation shared-input race

The secondary checkout's four missing outcome-evaluation artifacts were
reviewed read-only. `pass-summary.json` records three runs of each of five
PostgreSQL tests (15 passes, no skips or failures) and hashes the retained
passing JSONL output. The separate first-run JSONL contains a real race report:
concurrent `CreateEvaluation` calls shared slices, and
`normalizeAndValidate` rewrote and sorted caller-owned observations and
corrections. The prior fixture deep-copy avoided the race in that test without
making the production normalizer safe for shared caller input.

The PR worktree now copies the observation and correction slices before
normalizing them, and copies outcome indicators before rewriting/sorting them;
existing nested source normalization already copies each source slice. The
PostgreSQL concurrency test now deliberately shares its immutable input across
goroutines. New unit coverage asserts that normalization leaves caller-owned
input unchanged and is race-free under concurrent shared reads. The previous
passing summary and CI provide historical evidence only; this patch has not
yet run through Go compilation or the PostgreSQL race gate. The available
local Go executables are zero-byte placeholders, so no local Go test or gofmt
run was possible; `git diff --check` passed before this checkpoint.

The raw missing migration log, cleanup log, first-run race trace, and passing
JSONL are local test artifacts, not source. They contain host-specific paths or
fixture identifiers and are not copied into the repository; relevant test
results and the race diagnosis are preserved here. Keep those local artifacts
until the new code passes the exact-head race gate and the broader source
crosswalk and cleanup conditions are complete.

### October 11, 2026: secondary-checkout untracked source comparison

A fresh read-only inventory of source-like untracked files in the secondary
checkout used the extensions `.go`, `.ts`, `.html`, `.scss`, `.css`, `.json`,
`.yml`, `.yaml`, `.md`, `.ps1`, `.sh`, `.py`, `.sql`, `.proto`, `.mod`, and
`.sum`, excluding `output`, `.claude-flow`, `.swarm`, `node_modules`, `dist`,
`coverage`, and `test-evidence`. It found 845 files: every path exists in the
PR worktree, 803 are byte-equivalent after newline normalization, and 42 have
content differences. The difference count is not evidence that all 42 have
been fully reviewed; the per-file semantic disposition remains open.

Targeted comparisons of execution claims and source approvals confirmed the
PR version has stricter durable receipt binding, binds source evidence raw
SHA-256, reports precise stale-receipt causes, and keeps the SQL and in-memory
claim rules aligned. The secondary checkout uses an older approval predicate,
including a mutable owner field as authority evidence, and its GORM update
rewrites lifecycle timestamps. OpenClaw maintenance variants in the secondary
checkout include a literal copy of the pinned npm registry public key and
weaker credential/worker checks; the PR keeps that key byte-encoded and adds
independent registry signature and Windows Authenticode verification. The
acceptance-stack variant also omits PR filesystem ownership and non-root tmpfs
constraints. These variants were not copied over the PR implementation.

Migration test comparisons found useful rollback assertions in the secondary
checkout, but its source-configuration test attempted rollback while later
phase migrations were still applied, and its source-head test dismantled live
post-phase tables to reach a data guard. The PR versions explicitly verify the
phase-order refusal and exercise data-loss guards in isolated databases,
preserving durable history. These improvements are present in the PR worktree;
they still require successful exact-head migration CI before acceptance.

The latest PR state was checked with GitHub CLI: PR #36 is open, targets
`main`, and points to `9a554bc5e070f84c5777ddf1b6975f958555fc02`; the remote
branch points to the same commit. Exact-head checks include passing frontend,
secret-scan, authenticated-control-plane, local-provider-fixture, Compose,
and cleanup-contract jobs, while backend, browser acceptance, guarded Windows,
real-Postgres migration, installer, and several Python-runner jobs remain
pending. The GitHub app connector required reauthentication, so no claim is
based on that connector. PR #36 is unmerged; the transcript cleanup readiness
gate remains closed. No files were deleted or moved.

Still open: semantic disposition of all 42 differing untracked files, the 75
secondary-checkout tracked content differences, transcript-to-source
crosswalk and malformed-record resolution, the remaining local-only evidence
and output classifications, and exact-head CI completion. Do not remove the
transcript archive, source checkout, merge scratch, diagnostics, Docker data,
or other local artifacts until those integration and readiness gates pass.

### October 11, 2026: tracked-diff and local cleanup preflight refresh

The secondary checkout reports 692 modified tracked entries. A read-only
comparison against the PR worktree enumerated 682 diff paths; 75 have content
differences after newline normalization, and all 682 paths exist in the PR
worktree. The ten additional status entries were also checked and are
newline-normalized identical to the PR versions. Targeted review of the
highest-risk variants found
the PR contains the newer workflow success-criteria propagation, whole-record
execution policy checks, advisory-lock identity encoding, browser-verification
route registration, and recovery preflights. Several secondary-checkout
variants were stale implementations or tests: its execution-auth lock used a
NUL-delimited identity and interpreted a void advisory-lock call as a boolean;
its recovery tests and Compose ownership helper used older sequencing and
broader capabilities. These reviewed examples were not copied over the PR.
The remaining 75-path semantic classification is not complete.

The read-only local cleanup inventory ran from the PR worktree at
`65788d88a6c31204278569a775b724d0cc8bcc4b`; it performed no deletion. It
reported zero eligible synthetic temporary fixtures, eight transcript
candidates totaling 7,939,888,699 bytes blocked until canonical-main and
merged-PR verification, and 15 PR diagnostic/tool artifacts totaling
31,505,884 bytes blocked until PR #36 is merged at the exact local HEAD. The
Docker inventory is incomplete: three HAI-mounted anonymous volumes are
unrecognized, including PostgreSQL data mounts and Redis `/data`; a recovery
bundle of 10,189,079 bytes has unverified ACL evidence. Two old HAI images
remain on retention review, and neither containers, volumes, recovery data,
nor images are eligible for removal. The active PR worktree has 22 untracked
items and remains in use. Joyce Work Schedule and ShareT were preserved.

The GitHub Actions run for that exact HEAD (`38104760246`) was still queued at
the last check. PR #36 remains open and unmerged. No local HAI files or Docker
resources were removed; the transcript and local diagnostic cleanup gates
remain closed.

The explicit read-only source integrity check has since completed: all 18
archive files (20,739,169,122 logical bytes) match the manifest inventory, and
all eight candidate files (7,939,888,699 bytes total) match their recorded
size and SHA-256. This verifies the transcript bytes, not completion of the
semantic crosswalk or permission to remove them. The gate still reports
`cleanup_gate_ready=false`, `deletion_performed=false`, and
`cleanup_authorized=false` because committed-ledger verification on canonical
`main` and the merged-PR verification have not passed.

### October 11, 2026: refreshed local cleanup inventory and PR checks

A fresh read-only cleanup inventory ran from this PR worktree at
`244b6669d8a74d0f24ac0077c528079c81f26248`. It reports zero eligible
synthetic temporary fixtures. The eight transcript candidates (7,939,888,699
bytes) remain blocked because PR #36 is open and the cleanup command must run
from canonical `main`; this inventory intentionally skipped the expensive
archive hash audit, so the separately recorded 18-file hash verification
above remains the latest source-integrity evidence. Fifteen PR diagnostics and
tool downloads (31,505,884 bytes) remain blocked until the PR is merged at the
exact local HEAD. The active PR worktree has 22 untracked files and is retained.

The Docker inventory remains incomplete: three anonymous volumes mounted by
HAI containers are unrecognized, the 10,189,079-byte recovery bundle has
unverified ACL evidence, and two HAI images remain under retention review. No
container, volume, recovery bundle, or image is eligible for cleanup. The
secondary checkout has 692 modified tracked entries and 2,475 untracked files;
it remains preserved while the source, transcript, and working-tree audit is
unfinished. Shared applications and the Go toolchain are not cleanup targets.
No local files, containers, volumes, or images were removed.

GitHub CLI confirms PR #36 is open against `main` at the same exact head as
the local PR checkout. Exact-head run `38105148528` has passed the cleanup
safety contracts, authenticated control-plane smoke, two-account isolation,
secret scan, frontend compiler/build stages, and multiple runner/container
contracts. At the latest refresh, frontend unit tests, backend/IDP tests,
real-Postgres migration integration, browser acceptance, guarded Windows
runtime, installer guards, and additional runner contracts were still pending.
The PR is not ready to merge, and local cleanup gates remain closed.

### October 11, 2026: secondary checkout commit ancestry and source-file delta

The secondary checkout remains at `e07b9daeb3ba2630ecbe12a64d948f791203c241`.
Git confirms this commit is an ancestor of the PR branch, with zero commits
unique to the secondary checkout and 148 commits between that checkout and
the PR branch at `e08427b5abe8c57d64393ba58d208eaed291e9eb`. No committed
branch history needs to be imported from the secondary checkout.

A current read-only enumeration found 839 source-like untracked files under
the secondary checkout after excluding generated output, dependency, coverage,
and test-evidence directories. Every path exists in the PR checkout; 797 are
content-identical after normalizing CRLF/LF, and 42 differ. This is path and
content inventory only, not a completed semantic review of the 42 variants.
Targeted checks of execution claims, source-approval receipts, OpenClaw
signing-key handling, the owned browser matrix, and Promptfoo dependency
versions confirm the PR contains the newer reviewed implementations. The
older secondary variants were not copied over. The 75 tracked content
differences remain unclassified as a whole.

Exact-head Actions run `38105368794` for commit `e08427b5abe8c57d64393ba58d208eaed291e9eb`
has passing cleanup-safety contracts, Compose validation, local provider
contract, secret scan, Windows path contract, two-account isolation, and
several runner/image jobs. Backend, frontend unit tests, IDP, migrations,
browser acceptance, guarded Windows runtime, installer guards, and other
runner jobs are still pending at this refresh. No cleanup condition has
changed: keep both checkouts, the transcript archive, diagnostics, Docker
volumes, and recovery bundle intact.

### October 11, 2026: follow-up semantic review of overlapping source files

The tracked/untracked comparison was independently rerun against the current
secondary checkout. Its 682 modified tracked paths include 75 substantive
content differences after line-ending normalization; all paths exist in the
PR tree. Among the additional reviewed deltas, the PR preserves workflow task
success criteria, returns partial reminder-delivery results when cancellation
or a delivery error interrupts a batch, and wires optional SearXNG discovery
through the backend API, isolated Compose networks, configuration, contract
tests, and operator/license documentation. Those facilities are present in
the PR and did not require copying from the secondary checkout. The frontend
login method's explicit `Observable<void>` response typing is also present in
the PR. These checks do not finish semantic review of the other tracked or
untracked differences.

The separate July 30 transcript archive remains 18 files (16 unique child
IDs), with nine terminal-completed records, eight unique cleanup candidates,
and ten retained duplicate, aborted, or nonterminal files. Its eight
completed-candidate reports have a source crosswalk. The retained records
contain repeated parent-session patch events; child-local attribution and a
deduplicated semantic crosswalk remain incomplete. This archive is distinct
from the August cohort summarized earlier in this ledger. No transcript or
other local data is eligible for deletion at the present PR state.

The latest PR head at this review is `238a621f28bcdd09b5c7b8381a0685e74637f619`,
open against `main`. Exact-head Actions run `38105484907` has passed the
cleanup-safety contract job; the other checks, including backend, frontend,
database, browser, Windows, installer, and runner acceptance, were pending at
the latest refresh. Do not merge or clean the local worktrees until those
checks and the remaining source/transcript crosswalk are complete.

### October 11, 2026: browser-matrix refreshed-session handoff

The retained browser report for run `38104459435` records route-matrix tests
falling back to onboarding when the expected theme control was absent, followed
by refusal to repeat password login after the synthetic owner session expired.
The session refresh rotated cookies, but the next serial test context loaded
the unchanged original storage-state file. The fixture now persists refreshed
state only after confirming the authenticated session and validates that the
state file resolves under the run-owned evidence directory. Global setup now
creates that private state in a temporary child directory of the already
validated evidence root; existing global teardown removes it, and the CI
workflow uploads only explicit report, screenshot/result, and guarded-log
subdirectories. This change does not make the acceptance test itself pass.

The e2e TypeScript typecheck and all 108 `scripts.test_ci_contract` tests pass
locally. The fix is at commit `6cbc4475e2551c42c5cd5d144e1dbf9642c456d9`,
confirmed at the remote PR branch head. Exact-head run `38105996226` is queued;
older browser acceptance run `38105148528` remains in progress, so real
browser verification is pending. PR #36 remains open and unstable. The source
delta review and cleanup gates are unchanged: no archive, checkout, Docker
volume, image, or diagnostic cleanup is authorized by current evidence.

### October 11, 2026: runtime configuration semantic triage

A read-only comparison of `.env.example` and its referenced implementation
against the older primary checkout confirms that the primary checkout has no
unique configuration keys to port. The current PR couples the configuration
to implementation for exact loopback source-address allowlists, owner-bound
Trello and GitHub credentials, explicit opt-in to volatile scheduler fallback,
NAT64-aware outbound target validation, generated local-preview gateway
secrets, and private-address-pinned OpenClaw host access. The Windows DSH host
bridge remains hard-disabled pending authenticated peer identity; its
configuration comments accurately describe reserved settings rather than an
operational connector. These controls are referenced by backend code and
focused tests in the PR tree.

The older checkout's broad `CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL` setting is
not an equivalent replacement for the PR's exact loopback-address list and
would weaken the source-fetch boundary if copied. No environment values or
machine-local `.env` files were read in this comparison. This batch has no
missing product-code port identified; the remaining source-diff and transcript
crosswalk inventories remain open, so this result does not authorize local
checkout, archive, or diagnostic cleanup.

### October 11, 2026: refreshed source comparison and pause-path test

The current primary checkout working tree (HEAD `e07b9da`, with 692 tracked
paths modified) was compared with the PR worktree at `0a116d6a`. Within the
1,878 tracked source/documentation files covered by the text-extension filter,
108 have content differences after CRLF/LF normalization and zero are missing
from the PR checkout. This snapshot includes uncommitted tracked primary-tree
content and supersedes prior difference counts only for this stated
population; untracked files, generated data, binaries, and the wider transcript
patch-event crosswalk are not covered.

The sampled PR variants preserve raw source-evidence SHA-256 on ingest and
refresh (`operations/domain.go`, `operations/service.go`), validate and persist
bounded workflow success criteria (`workflow/service.go`), use transactionally
bound execution authorization rather than the generic policy-only check
(`phase2/handler.go`), and encode advisory-lock claim identities without
ambiguous separator concatenation (`executionauth/repository_postgres.go`).
The paused worker path claims previously classified low-risk work only to
recheck the current policy and defer it; emergency-stop mode still processes
nothing. The existing paused-mode test covered only newly ingested work, so a
focused regression was added to assert that an already-classified safe
operation is deferred, remains unexecuted, and receives a next-review time.

The repository-pinned Go 1.27.2 toolchain was later located under
`D:\codex-temp\hai-go-toolchain-1.27.2`; `go test -p 2
./internal/background ./internal/operations ./internal/workflow
./internal/phase2 ./internal/executionauth` passed locally, including the new
regression. The GitHub run for PR head `e27522ba` was still evaluating the
full backend/browser matrix at the time of this update. No source from the
primary checkout was copied. The 108-file semantic review is incomplete, PR
#36 is still open, and local data cleanup remains gated on further
source/archive reconciliation and final PR verification.

### October 11, 2026: untracked source-candidate comparison

A fresh comparison of source-like untracked files in the primary checkout used
the same explicit text-extension filter and excluded generated output, test
evidence, dependency, build, coverage, Playwright, and VCS directories. Of 833
candidates, zero are missing from the PR worktree and 38 have different
content after line-ending normalization. The compared primary checkout remains
untouched. This comparison does not cover binary files, excluded generated
content, or prove the semantic integration of the 38 differences.

Targeted review of the competing operation/source-approval files confirms that
the PR preserves owner and revision binding, the source-evidence digest,
exact-receipt verification, and claimability for a fresh approval even when a
prior self-hold set a review reminder. The primary variants omit or weaken
parts of those guards. Other previously checked candidate variants remove
deterministic claim-order, approval-before-execution, interrupted-acceptance
cleanup ownership, synthetic-credential scrubbing, or isolated installer
profile boundaries; the PR versions are retained. No candidate file was
copied. The remaining 38-way semantic disposition and all archive child-local
attribution are still open, so no secondary checkout or archive cleanup is
eligible.

### October 11, 2026: Runtime Lab style deduplication

The tracked frontend comparison found a duplicated set of DeepSeek runtime
policy/status selectors in `frontend/src/app/pages/runtime-lab/runtime-lab.component.scss`.
The duplicate block was removed from the PR source; the single remaining block
still covers the template's configured state, policy details, and blocker
status. `npm.cmd test -- --watch=false --browsers=ChromeHeadless` passed 175
Node checks and 1,331 Angular tests locally. The production build was not
available under the installed Node 25.2.1 because the project explicitly
requires Node `^24.15.0`. GitHub's Node 24 frontend build passed on the prior
PR head, before this CSS cleanup; the current head requires its own CI result.

### October 11, 2026: refreshed tracked-source semantic comparison

The source checkouts were refreshed at primary working-tree HEAD `e07b9da`
and PR head `f8e927f88cd1e330123256946086690f06d3b2e3`. Both trees remain
untouched by the comparison. Direct diffs confirm the PR implementation is
the more complete version in several areas that could otherwise look like
primary-only work:

- `backend/internal/workflow/service.go` in the PR validates and persists
  bounded success criteria, includes them in source-revision identity, and
  forwards them into task execution. The primary working tree lacks this
  behavior.
- `backend/internal/frameworkregistry/service.go` in the PR records
  owner-attributed, redacted preference-change reasons and exposes bounded,
  owner-scoped preference history. The PR router wires that history endpoint.
- `backend/internal/phase2/handler.go` in the PR evaluates the full persisted
  operation for execution admission and effect policy rather than authorizing
  from title/description/type alone. Its optional diagnostic response is
  gated on test mode and an explicit test-only flag.
- `backend/internal/operations/service.go` in the PR compares and retains the
  raw source-evidence SHA-256, preserving evidence identity independently of
  normalized JSON.
- The PR router wires browser verification to the workflow linker; it does
  not register an unlinked default handler before workflow construction.
- `.github/workflows/ci.yml` in the PR adds dedicated cleanup-readiness safety
  contracts and newer pinned toolchain/test configuration. These changes are
  not candidates to copy from the primary tree.

No code was copied from these primary variants. This is a focused semantic
batch, not completion of the remaining tracked-path or 38 untracked-path
review: the primary checkout has 692 modified tracked paths, and source-like
untracked files, machine-local data, excluded artifacts, and transcript
patch-event attribution remain outside the comparison above.

The PR remains open at `f8e927f88cd1e330123256946086690f06d3b2e3`. Exact-head
CI run `38107670416` is active: the cleanup-readiness contract and several
backend/runner jobs have passed, while Windows installer, browser acceptance,
and other matrix jobs are still running or queued. Browser full-scope
acceptance still requires persisted synthetic uncertain-workflow and
uncertain-task fixtures; the suite intentionally fails when these are absent.
Do not treat the active run, a first-scope browser run, or this source batch as
final acceptance. The July archive and local checkout/diagnostic cleanup
remain ineligible until source and transcript crosswalks and PR acceptance are
complete.

### October 11, 2026: wider untracked-source and safety-path review

A second, broader untracked-text comparison used an expanded extension set and
excluded output, dependency, build, coverage, browser, VCS, and test-evidence
directories. It found 865 source-like candidates, 46 content differences
after line-ending normalization, and four paths absent from the PR tree. The
four are `.claude-flow/agents/store.json`, `.claude-flow/policy/state.json`,
`.claude-flow/terminals/store.json`, and `.swarm/model-router-state.json`.
They are generated agent/runtime state rather than HAI product source or
deployable assets; they are intentionally not copied into the repository. The
865/46 inventory is broader than the earlier 833/38 filter and must not be
read as a like-for-like count change.

Additional direct comparison confirms the PR carries the stronger behavior in
these areas:

- Account-feed worker regression: the PR requires newly ingested source work
  to reach `awaiting_approval` without verification or a host effect. The
  primary checkout's version instead expects verification. The PR package
  passed an uncached `go test -count=1 -p 2 ./internal/background` run.
- Source-operation ingestion and approval: the PR persists raw-evidence
  digests through refresh, includes them in approval revision binding, and
  permits a future review reminder to be overridden only after an exact
  source receipt is verifiable. Focused source-approval and claim tests are in
  the PR; no alternate implementation was copied.
- PostgreSQL migration tests: the PR exercises the current migration tail,
  refuses out-of-order rollback while later-phase migrations remain, and
  checks data-loss rollback protection in a separate isolated database rather
  than dismantling runtime tables.
- Browser acceptance: the PR keeps refreshed owner cookies in a run-owned
  temporary path, excludes credentials from the browser fixture, validates
  evidence-root ownership, and avoids collecting trace bodies.
- Windows/acceptance tooling: the PR preserves operator-selected network
  ranges, clears inherited secrets in synthetic acceptance configuration,
  isolates installer profile writes inside the uniquely owned smoke root,
  and only permits unavailable Docker inventory on an explicitly identified
  disposable GitHub-hosted Windows runner.
- Secret reporting in the PR validates the comparison revision in CI and
  redacts candidate values while retaining bounded rule/path/commit metadata.

No source was copied in this batch. This remains a sampled semantic review of
the 46 differing files, not a signed-off disposition of every one of them.
The primary checkout's modified tracked files, excluded/generated output,
private runtime state, July transcript child attribution, and exact archive
crosswalk still require further reconciliation before any local deletion.

The ledger-only follow-up is now on PR head `228a1ceec4669fb5eeb23320d7d39fb4e381f850`.
The exact-head Actions run is `38107937213`; at the latest refresh it was
queued overall, with browser, backend, frontend, and Windows installer jobs
in progress and cleanup-readiness contracts passed. Results from `f8e927f`
and earlier heads do not establish final acceptance for this ledger head.
PR #36 remains open and cleanup remains gated on full reconciliation and
current-head acceptance.

### October 11, 2026: current archive integrity and PR gate recheck

The source archive `D:\codex-temp\hai-completed-agent-sessions` was checked
read-only against the committed manifest with
`scripts/test-hai-transcript-cleanup-readiness.ps1 -TranscriptRoot`. All 18
manifest paths match the source inventory and logical byte total
(`20,739,169,122` bytes); all eight unique completed candidate files match
their recorded SHA-256 hashes. The check reports `source_archive_verified`,
but `committed_ledger_verified=false`, `merged_pull_request_verified=false`,
and `cleanup_gate_ready=false`. No transcript was changed or deleted.

At the archive-verification checkpoint, PR #36 was open at
`d6cb0903dd63e15ed9da685c8f42d831eda8c535`; exact-head run `38108110220` was
queued overall. The ledger update containing this checkpoint was subsequently
pushed as `b720ea1c1092b3b40ed0344fcf1264048a97d245`. At publication, PR #36
remains open and the new exact-head run `38108719169` is queued. Neither run
is final CI acceptance, and the PR has not been merged into `main`.

Archive byte/hash integrity and candidate-report presence do not prove the
unresolved transcript patch/event content is semantically integrated. The
793-path event crosswalk, retained/aborted child attribution, and remaining
source-difference dispositions are still incomplete. Therefore the eight
candidate files (about 7.94 GB) remain candidates only, the ten retained
files (about 12.80 GB) remain protected, and no local transcript or checkout
cleanup is authorized by the repository readiness gate.

### October 11, 2026: supplemental July 30 child batches discovered

Two additional folders, `D:\codex-temp\completed-agent-sessions` and
`D:\codex-temp\session-log-relief`, contain nine more transcripts linked by
metadata to the same HAI root session. Three are completed unique children
and six are aborted/nonterminal. All 21 source paths named by the three
completed reports are present and tracked in PR #36. The full frontend suite
passed (175 Node checks and 1,331 Angular tests), and the CI contract suite
passed all 108 tests. The three completed source transcripts also have
full-file SHA-256 hashes recorded in
`docs/child-agent-archive-2026-07-30/supplemental-child-batch-audit.md`.

This supplements, but does not modify, the existing 18-row manifest or its
single-root cleanup verifier. The combined known July 30 set is 27 files / 25
unique child IDs / 34,752,906,420 bytes. The six newly found unfinished
transcripts and their possible earlier patch events have not been semantically
reconciled. The existing automated gate therefore cannot authorize removal
from either supplemental folder; no local HAI files were deleted.

### October 11, 2026: current PR and local snapshot reconciliation

The current PR #36 head is `f9012189f4b4ce95ef4a6823ca8ad8a7702d9a5b` on
`codex/hai-runtime-release`; it remains open and unmerged. At the time of this
checkpoint, 35 of 36 GitHub checks had succeeded, no completed check had failed,
and the owned disposable-stack browser acceptance check was still running. Do
not treat this as a completed CI gate.

The local `D:\codex-temp\hai-integration-snapshot` checkout is based on
`91c8620c557229f1da4ed15fcbb7088c6a6947a7`, the current `main` base, and has
1,790 staged paths plus 19 unstaged paths. Git-filter-aware blob comparison
found 1,674 staged versions byte-identical to the PR working tree and 116
different; this is a source-state comparison, not evidence that every
historical file or transcript has been semantically integrated. Review of
representative authorization, approval, source, and Windows recovery changes
found snapshot variants that remove or weaken protections present in the PR,
including approval revision binding, source connector behavior, fail-closed
runtime checks, and installer/recovery isolation. Those variants were not
copied. The snapshot's two focused scanner-related changes are already present
in the current PR: a per-run CI-only approval signing fixture and a synthetic
privacy-scanner token assembled at runtime.

The snapshot's uncommitted October 10 archive analysis reports 542 deduplicated
nontrivial completion texts across six retained July 30 histories; this is not
542 verified distinct tasks. Their semantic crosswalk to reports, source
changes, and accepted tests remains incomplete. The original 18-file archive
(20,739,169,122 bytes), the supplemental six retained histories, and the local
snapshot/worktrees remain preserved. A generated `frontend/node_modules` cache
in the active PR worktree was removed in a separate cleanup pass after path
and link checks (280,611,707 bytes); it is reproducible with `npm ci`. No
transcript, patch, environment file, runtime database, or worktree was deleted
as part of this reconciliation.

### October 12, 2026: unstaged integration-snapshot layer crosswalk

Compared all 19 files in the snapshot worktree's unstaged diff with the current
PR #36 worktree, separately from the 1,790 staged snapshot paths above. Fourteen
files are content-identical after ignoring CRLF-only differences. The five
remaining paths are `.github/workflows/ci.yml`,
`docs/child-agent-integration-ledger.md`,
`frontend/src/app/pages/governance-control/governance-control.component.spec.ts`,
`idp/internal/app/authentication/auth_service_test.go`, and
`scripts/smoke-model-intelligence.sh`.

The scanner-safe CI signing fixture, governance test identifier, IDP test
secret, and assembled privacy-scan fixture are already represented in the PR.
The PR's corresponding CI and IDP tests also include newer isolated-startup,
redaction, and request-context coverage than the snapshot versions. The
snapshot ledger's October 10 note about 542 deduplicated completion texts is
superseded by the reproducible retained-completion audit above; the PR keeps
the distinction between exact-text deduplication and verified integrated work.
No unique product implementation from this unstaged layer was missing from the
PR, and none was copied. This was a text/source comparison, not a rerun of its
reported tests. The snapshot's staged and unstaged files remain untouched.

### October 11, 2026: captured branch-only feature review

Compared the three captured inventory branches with unique commits against
PR #36. These are historical refs in the integration snapshot, not verified
live remote branches; no branch was merged or deleted.

- The four unique `agent-ci-worker-control-hardening` commits were reviewed.
  The Worker Control source connector is already in the PR as
  `backend/internal/source/worker_control.go`, with connector registration,
  bounded owner-scoped read-only ingestion, revocable credential configuration,
  provenance checks, and focused sync/configuration tests. No duplicate port
  was made.
- The `windows-installer` branch's installer and recovery feature family is
  present in the PR (`installer/windows/`, `scripts/build-windows-installer.ps1`,
  `docs/windows-installer.md`, and Windows contract tests). The captured branch
  versions differ from the PR's current files and were not copied over them;
  those differences need a later line-by-line review before claiming semantic
  equivalence.
- The captured Trello incremental-action proposal was inspected. It replaces
  complete paginated board reconciliation with an action-cursor fast path. The
  PR's live connector instead uses bounded complete pagination, explicitly
  reconciles closed and missing-card tombstones, and verifies card content
  hashes. The proposed fast path does not establish equivalent tombstone and
  cursor guarantees, so it was not ported. Reduced API reads remain an
  optimization opportunity, not an accepted behavior change.
- The broader `ambient-composition-control` ref contains many additional
  commits touching runtime, installer, migration, frontend, and resource
  behavior. Commit-name and file overlap alone cannot prove their semantics
  are integrated. This audit records only the feature-level dispositions
  above; the remaining unique commits and the 1,794 modified snapshot paths
  remain unreconciled.

No local artifacts were changed or removed. This checkpoint is not full source
reconciliation and does not authorize transcript, snapshot, scratch, output,
or diagnostic cleanup.

### October 11, 2026: merge-scratch conflict provenance

The separate detached merge scratch is at
`e3843448a3e2af8a11556251af6a3861dd1a8577`, 80 commits behind and 0 commits
ahead of the PR branch. Its index has 189 unmerged paths. Comparing the
canonical PR tree to the available index stages found all 189 paths present in
the PR: 170 PR blobs equal stage 2, none equal stage 3, and 19 equal neither
stage. This is blob-level provenance only; it does not prove the semantic
contents of every conflict were integrated.

Sampled conflict diffs for phase-2 execution policy and Windows environment
migration confirm that the PR retains exact source-approval checks, policy
rechecks at the effect boundary, verified rollback, and preservation of
configured private subnets. The alternate conflict side removes or weakens
those guarantees. No conflict was resolved and no scratch file was changed.
The merge scratch has no `MERGE_HEAD`; keep it intact until its origin and all
remaining conflict semantics are reconciled.

Exact-head Actions run `38112556687` for PR head
`bb62666ee07cdf550242d2057412ed288bf00f47` was still running at that earlier
checkpoint. No current CI status is inferred from that historical run.

### October 11, 2026: follow-up branch and connector review

The Autofever adapter was previously ported into PR #36 as `e4abace3` and its
local contract tests passed. A later direct audit of the sibling
`Robert-Velhorst/028-autofever` source contradicted the claimed upstream
contract: its current application is an incomplete illness-notification
prototype and exposes neither `/api/integrations/hai/feed` nor vehicle-review
records. The adapter and its credential plumbing have therefore been removed
from the active implementation; the legacy catalog entry is disabled and
existing source records are preserved but cannot sync. The earlier test result
proved only the mocked HAI-side adapter behavior, not a real upstream
integration. A provider contract and live authorized acceptance remain
prerequisites.

The four-commit `agent/ci-worker-control-hardening` branch was compared against
the current PR tree. Its Worker Control adapter is not imported: PR #36 already
contains a later implementation, and the branch's older `service.go` differs
by thousands of lines and would discard unrelated source-service behavior.
The branch's Go 1.25.13 pin was not imported because it is stale; the official
Go release history lists Go 1.27.2 as released on 2026-10-08. The migration
rollback-test change was not imported because it scans and rolls back
migrations outside the deliberately bounded fixture used by the current test;
the current PR has newer rollback guards and a narrower migration fixture.

The independent workflow-model change removing GORM's generated unique-index
tag was cherry-picked as `73a30856`, leaving the versioned migration as the
sole owner of that constraint. Go was not on the shell's `PATH`; the staged
Go 1.27.2 toolchain was invoked directly and `go test ./...` in `backend/`
passed. This local suite does not run the separately tagged real-Postgres
migration integration, so that exact-head CI check remains necessary. No other
changes from the Worker Control branch were copied.

The Autofever audit correction and schema-model fix do not complete the
broader branch or archive reconciliation. Keep source branches, transcripts,
snapshots, scratch
trees, and diagnostic data until each remaining source difference is reviewed
and preservation is verified; this checkpoint authorizes no additional local
cleanup.
`2026-10-11 04:47 UTC`: 16 jobs succeeded, 10 were in progress, 10 were
queued, and none had failed. PR #36 remains open and unmerged; no local files
are eligible for cleanup from this checkpoint.

### October 11, 2026: untracked verification-runner source review

The primary checkout's untracked `output/` tree contains 12 PowerShell
verification/cleanup runners (the remainder of the 1,576-file, 126,970,802-byte
tree is primarily JSON/JSONL reports, logs, and evidence). Static extraction
found 31 distinct Go test identifiers referenced by the runners; every
identifier resolves to a test in the canonical PR tree. The hosted CI workflow
explicitly runs the 13 durable-session PostgreSQL/Redis regressions and selected
real-PostgreSQL suites referenced by the runners.

These scripts orchestrate tests against the checked-in Go packages; they are
not additional HAI runtime implementations. They also include local Docker
ownership, no-pull, resource-limit, source-integrity, and test-skip checks that
are not equivalent to simply having the named test functions. They remain
retained as verification evidence until the exact runner behavior and reports
are reconciled; no runner was executed, copied, or deleted in this review.

### October 11, 2026: installer branch safety comparison

Compared the `codex/windows-installer` branch tree with the current PR on the
Windows backup and installer-support paths. The branch version of
`scripts/backup-windows.ps1` is 10 lines versus the PR's 656-line implementation;
it removes protected environment backup/restore, DPAPI user binding, ACL and
reparse-point checks, and recovery validation. Its
`installer/windows/Hai-InstallerSupport.ps1` is 12 lines versus the PR's
259-line implementation and drops environment ACL handling and maintenance
worker lifecycle safeguards. These are behavioral/security regressions, not
safe deduplications; neither file was copied from the branch.

The `codex/ambient-composition-control` branch contains the same shortened
backup/build scripts in its branch-only delta. It is not an acceptable source
for replacing the current installer or recovery implementation. The local
`codex/windows-installer` checkout also has a modified IDP test and two
untracked executable files; they remain untouched and are not cleanup
candidates. The rest of both branches is still under review.

### October 11, 2026: supplemental transcript integrity recheck

Ran `scripts/test-hai-supplemental-transcript-cleanup-readiness.ps1` read-only
from the current integration worktree at `d88ffcf0`. It verified all nine
manifest entries across the two supplemental source roots and their exact
14,013,737,298 logical bytes. SHA-256 matched for all three completed
candidate transcripts (4,651,926,405 bytes total), and all 21 report-linked
source paths are present and tracked in this PR tree. The six aborted or
nonterminal transcripts remain retained. The verifier reports
`cleanup_authorized=false` and `deletion_performed=false`; path and hash
verification do not replace the still-incomplete transcript event/content
crosswalk or the merge gate.

### October 11, 2026: integration snapshot comparison and exact-head CI

The `codex/hai-integration-snapshot` checkout is at commit
`91c8620c557229f1da4ed15fcbb7088c6a6947a7`, an ancestor of PR #36's current
`codex/hai-runtime-release` head (`467` commits behind, `0` commits ahead).
Its worktree still contains 1,794 modified tracked paths. A read-only comparison
of 2,917 tracked, non-secret text/source files against the PR found 86 differing
paths: `.github` (1), backend (35), Compose (1), docs (4), frontend (15),
scripts (28), and services (2). This comparison does not cover untracked files,
binary assets, excluded credential-like paths, the primary checkout, or the
separate merge-scratch checkout; it is not a cleanup authorization.

Sampled high-risk comparisons of operation claims, source approvals, optional
runtime documentation, and Promptfoo dependencies confirm that the canonical
PR carries the schema-correct `payload_json` query, complete source-revision
and owner binding, audited network/profile documentation, and pinned runner
dependency safeguards. No source was copied in this batch. The remaining
snapshot differences still require disposition against the existing audit
ledger; the separate merge-scratch checkout still has 189 unmerged paths.

Exact-head Actions run `38112214166` for commit `0701e4201ce170467a3942f1c25ca816d55b228a`
was in progress at the latest check (`2026-10-11 04:44 UTC`): 33 jobs had
succeeded, 3 were still running, and none had failed. This is not final CI
acceptance. PR #36 remains open and unmerged; transcript deletion and local
checkout cleanup remain unauthorized until source reconciliation, merge, and
fresh retained-copy verification are complete.

### October 11, 2026: supplemental July 30 child batches discovered

Two additional folders, `D:\codex-temp\completed-agent-sessions` and
`D:\codex-temp\session-log-relief`, contain nine more transcripts linked by
metadata to the same HAI root session. Three are completed unique children
and six are aborted/nonterminal. All 21 source paths named by the three
completed reports are present and tracked in PR #36. The full frontend suite
passed (175 Node checks and 1,331 Angular tests), and the CI contract suite
passed all 108 tests. The three completed source transcripts also have
full-file SHA-256 hashes recorded in
`docs/child-agent-archive-2026-07-30/supplemental-child-batch-audit.md`.

This supplements, but does not modify, the existing 18-row manifest or its
single-root cleanup verifier. The combined known July 30 set is 27 files / 25
unique child IDs / 34,752,906,420 bytes. The six newly found unfinished
transcripts and their possible earlier patch events have not been semantically
reconciled. The existing automated gate therefore cannot authorize removal
from either supplemental folder; no local HAI files were deleted.

### October 11, 2026: old-checkout patch reconciliation and cache cleanup

Rechecked PR #36 at `ccfbda84114dfd099daa1f401ea6506ebd2d5229`; the PR remains
open against `main`. The exact-head Actions run `38117621117` has 35 successful
checks and one in progress (owned disposable-stack browser acceptance), with
no failed checks at this poll. This is not final CI acceptance.

Audited `D:\codex-temp\hai-pr-update-018-20261009\local-working-tree.patch`
without changing that checkout. The 682-path patch is based on an older tree.
A reverse-apply check finds 62 divergent paths; Git's three-way simulation
leaves 60 unmerged paths and one cleanly mergeable test change. Representative
differences show the current PR has newer Go and Promptfoo versions, stricter
source/approval ownership and revision checks, stronger Windows environment
ACL rollback/recovery, and fail-closed runtime behavior. These are not safe
reasons to replace current files with the older patch. The remaining divergent
paths still need file-by-file disposition; the old patch and supporting source
artifacts remain retained.

Ported the cleanly mergeable change to
`backend/internal/outcomeevaluation/repository_postgres_integration_test.go`:
each concurrent integration-test caller now gets its own cloned request,
avoiding a race while request normalization mutates slices. The local package
test passes. `TestPostgresRepositoryLifecycle` compiled but skipped because
`HAI_OUTCOME_EVALUATION_TEST_DATABASE_DSN` is not configured, so this local run
does not prove the live PostgreSQL concurrency scenario. The latest PR CI run
must cover the pushed change before it is treated as accepted.

Removed only the verified disposable `frontend/node_modules` cache from this
superseded clone: 37,316 files / 280,611,707 bytes. The cache was a regular
directory, not a reparse point, and no other process referenced the clone.
The clone, `dist`, test-evidence JSON, CI logs, downloaded Gitleaks binary,
patches, credentials, runtime data, merge scratch, and all transcript archives
remain untouched. In particular, the 27 known July 30 transcripts remain
ineligible for cleanup until the semantic crosswalk and merge/preservation
gates are complete.

### October 11, 2026: archived-session scope check and cleanup-gate refresh

Re-ran `scripts/test-hai-transcript-cleanup-readiness.ps1` against
`D:\codex-temp\hai-completed-agent-sessions`. It verified all 18 manifest
entries and `20,739,169,122` logical source bytes, including the eight
candidate hashes. The verifier reports `source_archive_verified=true`,
`committed_ledger_verified=false`, `merged_pull_request_verified=false`,
`cleanup_gate_ready=false`, and `deletion_performed=false`. No archive content
was changed.

Inventoried 26 files in the local Codex archived-session area (absolute path
omitted) by session metadata and searched for the canonical HAI repository
identifiers.
Eight session files contain HAI-related identifiers, but their recorded
working directories are other projects. The matching records are injected
developer/memory context or sidebar/project-list metadata; none is a
user-authored HAI request. No HAI code change or source artifact was identified
in this set. These are mixed conversation archives, not HAI-only build caches,
so they remain preserved and are not cleanup candidates.

The current PR #36 head remains `9509b7832e65a7a1fa486ef6d694df18bde1e49d`.
Exact-head Actions run `38118106367` has 35 successful jobs and only the owned
disposable-stack browser-acceptance job still in progress at this check. This
is not yet a completed CI gate. No transcript, archive, source checkout,
patch/log evidence, secret-bearing file, or runtime data was deleted in this
pass.

### October 11, 2026: August transcript retention-size drift

Performed a read-only path and length check for the 114 August entries in
`docs/child-agent-transcript-manifest.csv` under the local Codex session area
(absolute path omitted). All 104 previously deleted candidate
paths remain absent, and all ten `retain` paths still exist. However, only one
of the ten retained files currently matches its recorded `logical_bytes`;
nine differ. Their combined current file lengths total `3,207,933,505` bytes,
while the manifest records `21,882,851,783` bytes. The cause and content-level
integrity of the nine mismatches have not been established. Do not delete,
rewrite, or normalize these retained files; the 2026-08-09 receipt is
historical evidence of its then-current post-delete check, not proof of their
current lengths or contents.

The pinned HAI parent-session log (absolute path omitted) currently measures
`14,826,950,232` bytes and contains references to each of the ten retained
child IDs (234-336 occurrences per ID). These matches include
repeated session-state references and do not prove that the parent log contains
the complete child transcripts or recovers the nine size discrepancies. Keep
the parent log and all ten retained child files intact pending a content-level
reconciliation.

The current manifest records 97 children for August 4-5, of which 88 were
completed; across the audited August dates it records 114 children and 104
completed. It does not substantiate a count of 116 completed children for
August 4-5. Any external count or alternate archive source still needs to be
identified and reconciled before the inventory can be called exhaustive.

PR #36 is open at `e45ed503418b5e7534991e52c3edc8e2e884c65b`.
Exact-head Actions run `38118885565` was queued/in progress at this check; no
result from it is inferred here. The size audit made no local changes.

### October 11, 2026: provenance check for the reported 116-child count

Searched the indexed HAI Codex conversation from August 14 that surfaced the
reported “116 completed HAI child transcripts from August 4-5” wording. The
matching text is embedded in a large user-supplied Codex safety-assessment
prompt that quotes earlier HAI history. Its source metadata identifies the
August 14 session and its parent workspace, but the match is not a transcript
manifest, archive listing, content hash, or independent preservation receipt.
It therefore explains where the wording reappears, but does not establish
that 116 completed child transcripts existed for those dates or that their
contents are preserved elsewhere. The checked-in manifest remains the only
inventory currently reconciled here: 97 children / 88 completed on August
4-5, and 114 / 104 completed across its audited August dates. No additional
archive path or deletion candidate was identified from this provenance check.

PR #36 is open at `efc8e7cb8bfc057d4f7265a96aec1a9ca08c4487`.
GitHub reports 34 successful checks and two still in progress; no check
failure is reported. No local transcript or session data was changed.

### October 11, 2026: supporting report and source-material import

Imported 13 historical implementation/verification reports and their 64
linked supporting files from the superseded HAI checkout into `output/`.
Forty-four of the 77 output source files are byte-identical to their source;
29 have only line endings normalized for portable Git storage, and four
Markdown reports also have repository-relative links in place of
machine-specific absolute paths. The generated `output/README.md` explains the
historical scope and limits of these records. A separate user-provided
whole-life framework taxonomy is preserved at
`docs/source-material/whole-life-framework-taxonomy.txt`; its original
attachment SHA-256 is recorded separately from the LF-normalized Git blob hash.

File-level source and destination sizes, SHA-256 values, and transfer status
are recorded in `docs/local-hai-import-manifest-2026-10-11.csv`. Destination
hashes refer to the staged Git blob bytes; `.gitattributes` keeps these text
artifacts LF-normalized across platforms. The manifest contains 79 rows: 77
imported reports/supporting files, the generated output index, and the
framework taxonomy. No source file was deleted. These additions are in the PR
worktree and are not considered durable until committed, merged, and rechecked
against the manifest. Historical report claims remain dated evidence, not
current runtime or external acceptance proof.

The framework registry already contains the 55 versioned catalog entries
derived from the supplied taxonomy. Added
`TestBuiltinCatalogSectionsExistInImportedTaxonomySource` so the checked-in
source must retain the 55 sequential numbered sections expected by the
catalog's existing section-to-ID contract. The focused
`go test -count=1 ./internal/frameworkregistry` passes. This guards source and
catalog structure; it does not claim every candidate implementation or
external framework runtime is operational.

### October 11, 2026: additional snapshot comparison and PR update

The current PR #36 head is `fa8b69cd4d6a7a5afcabbd14853a1b6dad5739b9` on
`codex/hai-runtime-release`, targeting `main`. The branch was updated without
force-pushing. Exact-head Actions run `38150045368` is queued; its checks are
not treated as passing until GitHub reports terminal results.

Rechecked the two retained snapshots without changing their worktrees. The
integration snapshot at `D:\codex-temp\hai-integration-snapshot` remains at
`91c8620c557229f1da4ed15fcbb7088c6a6947a7`, with 1,790 staged paths, 19
unstaged paths, no untracked paths, and no conflicts. Compared staged Git blob
IDs against the current PR tree while excluding ten sensitive-name paths
without reading their contents: 1,531 staged blobs are identical, 245 differ,
and four paths are absent from the PR. The divergent and absent paths have not
been copied; they require content-level review before integration.

The merge scratch at `D:\codex-temp\hai-merge-scratch-20261010` remains at
`e3843448a3e2af8a11556251af6a3861dd1a8577`, with 17 resolved staged paths and
189 unmerged paths. Its conflicts remain untouched. Both snapshots still
contain `.env-backend`, `.env-gateway`, and `.env-idp`; their contents were
not opened, hashed, staged, or published. No local source, archive, or
diagnostic was removed. The PR is open and not merged, so no cleanup gate is
satisfied by this update.

### October 11, 2026: merged-main conflict reconciliation and readiness retry

PR #36 (`codex/hai-runtime-release`) now includes current `main` at
`37de3e04e766ac3f7df04aeaddaf774904a7ebca` through merge commit
`10741d92bed4f8ac2c68fe53d3ea1f336ed0f847`. The exact-head hosted CI run
`38155723976` completed with one failure: the owned browser-acceptance stack's
single immediate `/login` probe received HTTP 502 from nginx during upstream
startup. Other reported jobs in that run succeeded. Updated the isolated stack
launcher to retry the loopback gateway for a bounded 60-second window and
added a mocked regression test; no Docker stack was started by that test.

The fix is commit `4f9d1b5ecaae52ae880e3d729d667a3e2fbc29ff`, pushed by
fast-forward to the existing PR #36 branch. The PR remains open against
`main` at base `37de3e04e766ac3f7df04aeaddaf774904a7ebca`; GitHub had not yet
computed mergeability at the last check. Exact-head Actions run `38156381037`
was queued, so no hosted result is claimed. Local evidence for this patch:
the isolated-stack contract suite, startup-guard suite, `scripts/test_ci_contract.py`
(110 tests), and `git diff --check` passed. This does not substitute for a
successful hosted disposable-stack browser run.

PR #33 branch comparison confirms its `backend/internal/source/worker_control.go`
is byte-identical to the current PR #36 tree. Its historical migration names
`0060`-`0067` map to the already-present current migrations `0119`-`0126`;
they are not copied a second time. The branch also contains `.env-backend`,
`.env-gateway`, and `.env-idp`; these are excluded without opening or copying
their contents. Three branch-only tests were inspected: the ambient-monitor
release test depends on absent helpers and is not a direct copy candidate; the
model-maintenance test targets APIs absent from the current tree; the pursuit
projection test asserts an obsolete omission of source URIs. No claim is made
that these branches are fully reconciled or ready to close.

No source archive, transcript, scratch worktree, local runtime data, or Docker
resource was deleted in this checkpoint. PR #34 and #35, remaining branch-only
behavior, archive reconciliation, and exact local cleanup candidates remain
open work; local cleanup is still unsafe because preservation reconciliation
remains incomplete.

### October 11, 2026: branch-specific audit and PR #34 scheduler review

Refreshed remote refs and open PR metadata. PR #36 is open and mergeable at
base `37de3e04e766ac3f7df04aeaddaf774904a7ebca`; its current pushed head is
`9f2e1ba9304b60b9647f6550be3cf69ce2a2a4c3`. PR #35's head is an ancestor of
PR #36, so its commit history is included there. PR #33 and PR #34 heads are
not ancestors of #36 and are not marked reconciled or safe to close.

PR #33's only branch-added non-environment paths are three tests already
reviewed in the prior entry; the branch's worker-control implementation is
byte-identical to PR #36. PR #34 is substantially stale relative to #36: its
snapshot removes over 1,100 paths present in #36, so wholesale merge/cherry-pick
would regress current code. Its Windows no-fake-claims audit was ported as two
new scripts and wired only into the Windows CI job. Fixed the audit's root-commit
fallback: it now requires a resolvable merge base (PR base, explicit override,
push-before SHA, or `origin/main`) and fails closed when unavailable. Full Git
history is fetched only for the Windows audit job. Local verification passed:
13 audit checks, 110 Python CI contract tests, and `git diff --check`. Commit
`9f2e1ba9304b60b9647f6550be3cf69ce2a2a4c3` is pushed to PR #36. Its exact-head
Actions run `38156961155` is queued; hosted checks remain unverified.

PR #34 also contains branch-specific scheduler-readiness instrumentation,
compatibility tests, and a live-Trello acceptance launcher. These have not yet
been accepted or discarded: the older scheduler files cannot be copied without
reworking them against PR #36's current durable scheduler, explicit fallback,
and lifecycle behavior. The live-Trello launcher is a candidate for separate
review; no live credentials or Trello account were accessed. Existing worktrees,
archives, local runtime data, and source files remain untouched. Cleanup is still
blocked on full branch/archive reconciliation and a verified preservation gate.

### October 11, 2026: gateway/frontend port mismatch found in hosted acceptance

CI runs `38156961155` and `38157147190` both failed only in browser acceptance.
Downloaded, redacted evidence for run `38157147190` shows the frontend container
serving its health endpoint on port `8080`, while the dashboard gateway sent
`/login` to `frontend:80` and repeatedly received connection-refused responses.
This is a real deployment defect, not a readiness-delay issue; all containers
were reported healthy because the frontend health check probes `8080` directly
and the gateway health check does not exercise the SPA route.

Fixed `nginx-config/nginx.conf.template` to proxy to `frontend:8080` and added a
gateway contract test that cross-checks this upstream against the frontend's
actual listen port. Commit `b6ddf35f03f2153d22ab4b7cb6c53a02f8d1c946` is pushed
to PR #36. Local verification: 19 gateway tests, 110 CI contract tests, the
isolated-stack safety contract suite, and `git diff --check` passed. The fix is
not yet confirmed by hosted browser acceptance: at the time of this entry, no
Actions run for the new exact PR head was visible and PR mergeability was
`UNKNOWN`.

The redacted CI artifact remains available in GitHub Actions. Its temporary
local inspection copy and the synthetic evidence directory created by the
non-starting safety-contract test are disposable only after confirming neither
contains source/archive material or active containers. No original source,
archive, worktree, environment file, database, runtime data, image, or Docker
resource was deleted. Broader local cleanup remains gated on completing the
branch and archive reconciliation.

### October 11, 2026: PR #34 additions reconciled path by path

Compared files present only on `origin/codex/windows-installer` against the
current PR #36 tree. The three `.env-*` files were excluded by path and their
contents were not opened. The remaining additions were adjudicated as follows:

- Ported the Google OAuth route regression into
  `backend/internal/router/source_oauth_routes_test.go`, adapted to today's
  handler/service signatures. It verifies anonymous initiation is forbidden
  and a sessionless callback reaches signed-state validation (invalid state is
  rejected). `go test ./internal/router` passes.
- Did not port `backend/internal/accountfeed/bridge_test.go`: the old test
  reports provider-native API credentials as account-feed grants, while the
  current account-feed contract explicitly separates its normalized imports
  from live owner-scoped source connectors.
- Did not copy the three scheduler configuration tests: current durable
  scheduler tests already cover bounded poll intervals; the old tests also
  reference a legacy-fallback helper absent from this tree.
- Did not copy `backend/internal/ambientmonitor/release_acceptance_postgres_test.go`,
  `backend/internal/llm/maintenance_context_test.go`, or
  `backend/internal/pursuit/dashboard_projection_test.go` unchanged. The first
  uses obsolete fixture helpers, the second targets removed maintenance APIs,
  and the third asserts a projection contract that conflicts with current
  source-provenance requirements. They need fresh tests against current
  contracts if a specific uncovered behavior is later identified.
- Did not copy `backend/internal/router/lifecycle_compat.go` or its test: the
  helper has no current call site, so importing it would add unused lifecycle
  code rather than repair an active path.
- Did not copy the PR #34 durable-job queue/lease index migration: its
  `post/0005` filename conflicts with the current migration sequence, and no
  query-plan or workload evidence justifies adding another index beside the
  current queue and owned-lease indexes.
- Did not copy the old IDP password-policy, reset-token, default-service, or
  user-handler additions. Current password/reset behavior has newer code and
  service/repository integration tests; the old test helpers and user-service
  interface do not match the current implementation, and the old default
  logger/sender is already replaced by `event_bus.go`.
- Did not copy `installer/windows/Start-HAI-Cloud.ps1`: it calls
  `Assert-HaiLocalEnvironment`, which is absent from the current installer
  support API. Its underlying `scripts/start-ngrok.ps1` already exists with
  ownership and public-exposure guards; a current installer entrypoint would
  need deliberate product/UI integration, not this stale wrapper.

The new OAuth test and gateway correction are in the pushed PR #36 history;
the other additions above are documented as reviewed, not silently treated as
integrated. This closes only the PR #34 branch-additions sub-inventory. It does
not complete PR #34's full changed-file comparison, the other local branches,
or the archive/transcript inventories. No source or branch files were deleted.

### October 11, 2026: browser acceptance runner import failure

Exact-head PR #36 run `38158217614` passed backend, frontend, IDP, migration,
Windows, cleanup-safety, and provider-fixture jobs, but browser acceptance
failed before discovering tests. Its retained Playwright report records three
collection errors: importing `frontend/src/app/control-room/module-registry.ts`
loaded Angular Router's partially compiled `PlatformLocation`, but the
standalone Node Playwright runner does not load `@angular/compiler`. The report
therefore listed zero tests; this was an acceptance-harness defect, not a
browser route failure.

Moved the Angular-dependent URL parser into `module-url.ts`, leaving the shared
module registry free of Angular runtime imports, and updated the shell and
registry specs to use the new parser module. Local verification on Node 25.2.1
(the project specifies Node 24.15.0) passed E2E test discovery (142 tests), E2E
TypeScript checking, the focused Angular module-registry suite (22 tests), and
`git diff --check`. The exact-head hosted browser acceptance rerun is still
required; no Docker stack was started locally.

In the same checkpoint, `test-hai-supplemental-transcript-cleanup-readiness.ps1`
verified both supplemental source roots against all 9 manifest paths and
14,013,737,298 bytes, checked all 3 completed-candidate hashes and all 21
report-linked source paths, and identified 6 retained transcripts.
`deletion_performed` and `cleanup_authorized` were both false. This read-only
source check does not complete retained-transcript semantic reconciliation or
authorize local cleanup.

### October 11, 2026: PR #35 ancestry and follow-up frontend test correction

Refreshed remote branch refs. PR #35 head
`95155a58e8d0a295f2361e913cda7e870cb9cb38` is an ancestor of PR #36 head
`5acd361e21027d019c1d89a94f3b2cdf4886ff6f`; its task MCP changes are already
in #36 and must not be counted or cherry-picked again. PR #33's Worker Control
implementation is already byte-identical in #36. PR #33's remaining historical
test dispositions and PR #34's full divergent snapshot still require their
documented reviews.

Exact-head CI run `38159090927` exposed two stale frontend logic-test imports
after `moduleForUrl` moved from `module-registry.ts` to `module-url.ts`. The
control-room logic and deep-link focus runners now load the production resolver
through the TypeScript logic loader, with only the registry data bridged into
the isolated runner. Local rerun of the exact failing resource-regression
command passed all 96 tests on Node 25.2.1; the repository requests Node 24.15.0.
The hosted run was still in progress at this checkpoint. No source archive,
worktree, database, runtime data, or diagnostic artifact was deleted.

### October 11, 2026: PR #34 source/Trello safety comparison

Compared the current #36 source scheduler/service/Trello implementation with
the open #34 `codex/windows-installer` tree. The PR #34 Trello transport puts
the API key and token in the URL query, uses a simplified request path without
the current bounded aggregate request/page budgets and rate-limit handling,
and its board-action optimization skips closed cards without the current
closed/unavailable-card archival reconciliation. Current #36 uses the
Authorization header, rejects redirects, bounds provider reads/retries, and
retains the authoritative fallback/reconciliation path. These older changes
are intentionally not ported; the newer current implementation is safer and
has stronger state recovery.

PR #34 also proposes converting terminal scheduler failures into review
workflows. Its optional reporter returns no error, and its implementation
silently exits if source lookup fails; workflow/audit persistence failures are
recorded only best-effort. Do not treat that implementation as integrated or
reliable terminal delivery. A durable, idempotent failure-notification/outbox
contract remains a separate open candidate. The full #34 changed-file and
other-branch inventories remain unfinished; this safety finding does not
authorize branch or local artifact deletion.

### October 11, 2026: ambient-composition branch inventory refresh

Refreshed `origin/codex/ambient-composition-control` against the pushed PR #36
tree. The ref has 134 non-merge commits not reachable from #36; a stable
patch-ID comparison found only one exact patch match against #36's 1,048
non-merge commits. Patch-ID mismatch is not evidence that behavior is absent,
because equivalent code may have been rebased, rewritten, or superseded.

The branch-tip tree has 6 paths not present in #36: 3 environment files, which
were not opened, and 3 historical test files previously reviewed as
incompatible with current contracts. It has 1,139 #36-only paths and 820
shared paths with different contents. These counts disprove a safe wholesale
merge but do not finish the 134-commit semantic crosswalk. Several commit
subjects describe potentially valuable lifecycle, cancellation, resource,
database-query, and progressive-loading work; each still needs behavior-level
comparison against current code/tests. The ref remains preserved and no local
artifact or branch was deleted.

The ambient branch's `7fabfe75` opt-in local-model installer action was also
checked against current Compose and model policy. It invokes an `ollama-local`
service/profile that does not exist in current `docker-compose.local.yml` and
writes only `OLLAMA_BASE_URL`/`OLLAMA_MODEL_IDS`; current local Model
Intelligence stays blocked until the operator records a model-specific local
inference attestation and a recent live provider probe passes. Therefore this
installer action would fail or overstate readiness if copied unchanged. Keep
it as an open candidate for a deliberate opt-in service/configuration flow,
including resource limits, explicit confirmation, policy attestation, live
probe, rollback, and end-to-end Windows validation.

### October 11, 2026: queued sync superseded by operator pause

The PR #34 durable-source review exposed a retry-policy gap in the current
`syncHandler`: when an operator pauses or revokes a source after its scheduled
sync has been enqueued, `syncContext` correctly returns
`ErrSourceSyncUnavailable` before importing, but the scheduler treated that
expected policy outcome as a provider failure and retried it. The handler now
completes this obsolete scheduled intent without consuming retries; a later
scan can enqueue work if the source is re-enabled. Added a regression test
that pauses the source between enqueue and worker execution and verifies no
sync starts and the durable job succeeds with zero attempts.

Local Go execution is unavailable in this Windows worktree (neither `go.exe`
nor `gofmt.exe` is installed or on PATH), so this change is not locally
verified yet. The exact-head GitHub backend test/build and overall CI run must
pass before this finding is considered integrated. `git diff --check` passes.
No PR #34 branch content, local source, worktree, archive, database, or
runtime data was deleted. The full PR #34 divergent-file review remains open;
this entry records one reviewed behavior and its port, not a completion claim.

### October 11, 2026: connected-source extraction pagination

The connected-sources extraction endpoint accepted a result limit but loaded
all extractions for every owned source, filtered the full result in memory,
then sliced the response. The service and repository already provided an
owner-scoped page query, but the HTTP handler was not using it. The handler now
calls that bounded service path, preserves the JSON-array response and exact
total/result-limit headers, returns a clear unavailable response if a service
does not support bounded access, and returns an empty JSON array for no items.
Repository ordering now uses `updated_at DESC, id DESC` so page boundaries are
stable when timestamps tie. Handler and repository regression checks cover
the passed limit and total/page contents. Go and PostgreSQL-backed test
execution remain pending because Go is not installed in this Windows
worktree. Exact-head CI caught that the general migration-oriented visibility
helper intentionally includes ownerless legacy sources, unlike the historical
HTTP account view. `ExtractionPageForOwner` now filters to exact, non-empty
owner matches before requesting the page, preserving the previous tenant
boundary while retaining SQL pagination. A new exact-head CI run is required
before claiming verification. The existing real-Postgres CI job did not run
this DSN-gated repository test, so it now explicitly runs and requires that
test's pass marker before executing the other connected-source regressions.
The first explicit run exposed an incomplete temporary table fixture: an
existing full-record owner lookup needed the model's `raw_item_id` and other
columns not used by the paged query. The fixture now declares the complete
extraction model schema; rerun the exact Postgres job before treating the SQL
test as passed.

### October 11, 2026: idle model-maintenance scheduler

Compared PR #34's model-maintenance scheduler changes with current #36. The
current startup launches the daily model-maintenance worker even when policy
has no configured eligible enabled model, and it launches that worker while
`LLM_MODEL_MAINTENANCE_ENABLED=false`. Ported the eligibility check so the
model worker starts only when model maintenance is enabled and a provider/model
pair is currently eligible. Kept strict provider-probe scheduler startup
separate and before this guard: the older PR #34 guard would also suppress
required readiness probes. Focused and full backend CI verification are
pending on the latest pushed head; the branch remains preserved for continued
file-by-file comparison.

PR #34 also tightened provider URL validation. Current #36 already rejects
embedded URL credentials, but routing accepted query parameters and fragments.
Provider readiness now rejects all three components, with separate route-level
regression cases for credentials, query parameters, and fragments. This is
consistent with the existing maintenance endpoint parser, which already
rejects those URL components. Exact-head backend CI is pending.

The same PR's System Status page changed healthy readiness checks from a fixed
15-second interval to state-aware polling. Ported that resource behavior into
the current #36 component while preserving its module preferences and stale-data
presentation: healthy status polls every 120 seconds, degraded status or a
failed probe every 60 seconds, and `not_ready` recovery every 15 seconds. Hidden
tabs cancel the pending timer and refresh on return. Each request has a
10-second timeout, only one request may be in flight, and destruction clears
the timer/listener. That reduces healthy readiness requests by 87.5% compared
with the prior four-per-minute cadence; it is a per-endpoint reduction, not a
claim of 87.5% lower total application resource use.

Local verification on the #36 working tree used bundled Node 24.19.0:
`npm test -- --watch=false --browsers=ChromeHeadless` passed 176 Node-side
contract checks and all 1,369 Angular browser tests. `npm run build` passed;
the build emitted bundle and component-style budget warnings, which remain
visible and are not counted as a clean warning-free build. The isolated System
Status spec passed 9/9 before the full suite. Remote exact-head CI is still
required after this update.

### October 11, 2026: open-PR source crosswalk

Compared the current remote heads of open PRs #33 and #35 with `main` and the
PR #36 source branch at `61a370f020c4fa5db21f340847dd42d25ec3d100`.

- PR #35 (`feat/chatgpt-logs-mcp-tool-loop`, head
  `95155a58e8d0a295f2361e913cda7e870cb9cb38`) is an ancestor of PR #36. Its
  model-directed, bounded ChatGPT-history MCP adapter and task/UI integration
  are therefore already present in the consolidated #36 history via merge
  commit `0ddf1cb3`. It does not need a second source port. PR #35 remains open
  and GitHub currently reports it conflicting; its last observed CI run
  `38121339049` failed backend vet, frontend dependency audit, authenticated
  smoke, migration, IDP/nginx vulnerability, and two-account build checks.
  Those failures must not be described as resolved merely because the code is
  present in #36.
- PR #33 (`agent/ci-worker-control-hardening`, head
  `429f453e26045456ff437c0aa3561ab88dd1d5f9`) is not an ancestor of `main` or
  #36, but its `worker_control.go` adapter and test blobs are byte-identical to
  current `main` at `37de3e04e766ac3f7df04aeaddaf774904a7ebca`; the same files
  also exist in #36. Of its 25 paths changed from its merge base, 14 blobs are
  already identical to `main` and 11 differ. The divergent snapshot removes or
  regresses newer configuration and ingestion safeguards, including bounded
  GitHub pagination/completeness handling and stricter HTTP transport policy.
  Do not merge or port the PR #33 branch wholesale. Its connector implementation
  is already represented in the canonical tree; the remaining 11 paths need
  individual, behavior-based review before any selective port.

The comparison used remote Git object IDs and ancestry checks, not line-count
addition. PR #35's presence is proven by Git ancestry; PR #33's connector
presence is proven by exact file-blob identity and current-tree lookup. Neither
check proves live Worker Control account acceptance or ChatGPT-history daemon
acceptance. No branch was deleted, force-pushed, or merged by this crosswalk.

### October 11, 2026: System Status build provenance

Reviewed PR #34 commit `f0dc3fda` (`feat: show running build provenance`). The
current backend already exposes the owner-authenticated `/api/v1/system/info`
contract, so the useful frontend portion was ported rather than copying the
installer branch. System Status now reads version, commit, build time, Go
runtime version, and run mode once on page entry and displays a compact running
build label with keyboard-focusable full details. Metadata failure is
supplemental and does not alter readiness/error state; its request is bounded
and unsubscribed when the component is destroyed.

Local verification on bundled Node 24.19.0: focused System Status and service
tests passed 18/18; the complete frontend run passed 176 Node contract checks
and 1,374 Angular tests; production build passed. The build still reports the
existing initial-bundle budget warning (7.53 KB over 1 MB) and four component
SCSS budget warnings. These are recorded, not suppressed. The new exact-head
GitHub Actions run is required for remote acceptance. PR #34 remains open and
its broader divergent-file review remains incomplete; no source worktree or
branch was removed.

### October 11, 2026: background-operation refresh failure parity

Compared PR #34 commit `4d810722` (`fix: surface background dashboard load
failures`) with #36. The current #36 implementation already provides a stronger
equivalent: a persistent, accessible refresh-failure panel; a contextual
recovery step; retry; preservation of previously loaded results; and a guard
that disables a new background pass until the feed/operation state is current.
The older toast-plus-alert patch was therefore not copied. This is a verified
duplicate/superseded disposition, not an unreviewed candidate.

### October 11, 2026: ambient branch connected-source revocation crosswalk

Reviewed ambient branch commit `c558ddfc` (`Make connected-source revocation
terminal`) against the current #36 source implementation. The principal
security invariant is already implemented in #36: ordinary source updates use
an owner/connector/version-bound conditional update that rejects a revoked
row; explicit revoke uses a transactional mutation; and Google OAuth token
creation, refresh, and source revocation serialize against the source row so a
late consent or refresh response cannot restore credentials after revocation.
The current tree also contains regression coverage for revoked-source updates,
OAuth revocation races, and grant binding (`backend/internal/source/service_test.go`,
`oauth_test.go`, and `google_oauth_atomic_persistence_postgres_test.go`). The
older commit's broad `UpdateSource`/`SaveOAuthToken` rewrite is not copied: #36
has more specific conditional-update and Google-grant persistence paths, and
the legacy generic `SaveOAuthToken` method has no production call site.

Verification boundary: this was a source and test crosswalk, not a successful
local Go test run. The current environment has no `go` executable. Exact-head
GitHub CI run `38164082335` was still active when checked; backend/IDP and
Postgres integration results remain pending there. The ambient branch itself
remains unmerged and preserved; its other divergent paths have not thereby been
reviewed.

### October 11, 2026: canonical Compose entrypoint identity and environment

Reviewed ambient branch commit `f2f5babc` (`Consolidate Compose into one
canonical stack`) against the four compatibility Compose files on #36. The
canonical source-built service stack was already in place, but the wrappers
did not set a shared Compose project name or load the `.env.local` generated by
the Windows initializer. Added `name: 018-hai` and an overridable
`HAI_ENV_FILE` include environment path to the root, backend, IDP, and gateway
entrypoints. This prevents directory-dependent project identities and makes
the wrapper consume the installed local configuration while preserving an
explicit override for CI and alternate environments.

Verification: Docker Compose v5.3.1 `config --format json` resolved each of
the four entrypoints using `.env.example`; each produced project `018-hai`
and the same 10 services. This was config-only validation and did not start,
stop, or mutate any container or volume. The focused Compose contract test
passed, all 110 tests in `scripts/test_ci_contract.py` passed, and
`git diff --check` passed. This ports the wrapper behavior, not the ambient
branch's full Compose topology rewrite; the remaining 133 branch-only commits
and other divergent paths remain unreviewed.

### October 11, 2026: durable automation event delivery wired into production

The checked-in outbox table, dispatcher, and operator handler were not connected
to the production automation CRUD path. Added GORM repository methods that
commit create, update, or soft-delete mutations and their outbox event in one
PostgreSQL transaction. The service uses those methods when available and keeps
the existing direct-publisher fallback for other repository implementations.
The update uses an id-bound `UPDATE` rather than GORM `Save`, so a concurrent
delete cannot be undone by an upsert. Successful mutations wake the dispatcher;
it runs under the API lifecycle, closes its publisher on shutdown, and is only
started when Kafka brokers are configured. With no broker configured, events
remain durable and visible rather than being silently dropped.

Added owner-authenticated, admin-only delivery status and dead-letter retry
routes under `/api/v1/automation/events/delivery`; a successful retry wakes the
worker. Persisted error details pass through secret redaction and a 1,000-byte
bound. Updated readiness and doctor messages to describe queued delivery.

Verification: `go test ./internal/automation ./internal/events ./internal/router
./internal/doctor ./internal/health` passed using the cached Go 1.27.2 image.
The new PostgreSQL mutation/rollback integration test is present but was skipped
locally because `HAI_TEST_DATABASE_DSN` is not configured. The earlier exact-head
PR CI predates this change, so GitHub validation for the new commit is still
required. This is a selective completion of the durable-event path, not a
review of the remaining divergent branch commits or external Kafka acceptance.

### October 12, 2026: archive integrity and local-source reconciliation

Before this ledger update, PR #36 was open at
`df87e871e6639bb51829ad6d0e382af2b0479ad0`; exact-head CI run `38185989903`
completed successfully with 36 of 36 jobs and no failures. PRs #33, #34, and
#35 were also open with passing listed checks at that observation. A green
check run does not satisfy the archive semantic-reconciliation or merge gates.

Rechecked the original July 30 source root
`D:\codex-temp\hai-completed-agent-sessions` against the checked-in
18-row manifest: all 18 paths exist and all recorded logical byte lengths
match, totaling 20,739,169,122 bytes. The supplemental source roots contain
the expected 3 and 6 files, respectively, totaling 14,013,737,298 bytes.
Recomputed SHA-256 for the three completed supplemental transcripts; Hubble,
Popper, and Anscombe match their manifest hashes. The six supplemental
aborted/nonterminal transcripts remain protected. These checks establish
current file presence, lengths, and the three selected hashes; they do not
complete semantic review of every transcript or authorize deletion.

Refreshed the two D: scratch checkouts using a per-command Git safe-directory
exception only; no global Git trust setting was changed. The integration
snapshot remains at `91c8620c` with 1,790 staged paths, 19 unstaged paths, no
untracked paths, and no unmerged entries. The merge scratch remains at
`e3843448` with 206 staged paths, 189 unstaged paths, and 189 unmerged paths.
Both remain preserved; neither was copied or cleaned.

Reviewed local Autofever candidate commit `7eed157b`. Its new adapter assumes
`/api/integrations/hai/feed` and vehicle-review records, but its tests use only
a synthetic HTTP server and do not establish that the upstream application
implements this contract. The current PR correctly keeps Autofever disabled
pending a verified upstream contract; the candidate is not ported or described
as operational, and its local branch remains preserved for now.

Reviewed the two nginx additions in the conflicted `hai-pr36-integration`
worktree: `nginx-config/nginx.conf` and
`nginx-config/sites-enabled/generic-auto.conf`. They are not ported. The former
hard-codes `X-HAI-Backend-Key` as `change-this-local-backend-key` on API routes,
instead of using the current template's `${BACKEND_API_SHARED_KEY}` secret
substitution. The latter exposes `/generic-auto/` through the gateway even
though `generic-auto` is an opt-in compatibility service, not part of HAI's
operational path. The current PR retains the environment-substituted secret and
returns 404 for this public path. The conflicted source worktree remains
preserved for review; no files were copied from it.

Reviewed the safety-sensitive source deltas in
`hai-runtime-safety-audit` (worktree base `e07b9da`). Its edited DeepSeek path
removes the current PR's strict cancellation receipt identity checks, request
context failure handling, sensitive-environment filtering, bounded/redacted
child-process output, and Windows reparse-point rejection. Its host-runtime
variant also removes the persisted start-intent/acknowledgment protocol and
conservative uncertain-start recovery. These are regressions relative to PR
#36, so none of those edits were copied. The dirty worktree and its files remain
unchanged and preserved.

The separate `hai-openclaw-artifact-audit` worktree is based on the same older
`e07b9da` commit and has 405 uncommitted entries. A content-hash comparison of
its 255 untracked files found 97 that are not byte-identical to current PR #36;
this is a triage signal, not a code review or proof of unique capability. In
the OpenClaw maintenance subset, the worktree version lacks controls already
present in the PR, including the background-processing gate, bounded process
output handling, pinned Authenticode verification path, post-install evidence
binding, and fresh provider-metadata binding. That subset was not ported. The
remaining artifact, connector, frontend, and migration deltas require targeted
review; the worktree remains untouched.

Removed only the clean `hai-tree-compare` checkout after confirming it was at
the exact PR #36 head and contained no ignored or untracked files. Its 3,147
tracked repository files were already present in the open PR; the measured
checkout footprint was 37,283,017 bytes. This freed about 35.6 MiB of duplicate
local checkout data. No archive, transcript, diagnostic, environment file,
database, or unresolved worktree was removed. The wider
branch, transcript-semantic, and local-artifact audits remain incomplete; no
other cleanup gate is claimed.

### October 12, 2026: model-maintenance candidate safety review

Reviewed the model-maintenance and model-intelligence changes in the dirty
`hai-model-maintenance-audit` worktree against PR #36 head
`86e8d3d3fc51f3a8c0cbbd2feac9630a22f83444`. The candidate is based on the
older `e07b9da` tree; comparison was limited to the maintenance scheduler,
maintenance execution, provider registry, provider adapters, and inference
validation paths.

The candidate was not ported. In `backend/internal/llm/maintenance.go`, it
removes provider-specific restrictions on reusing past maintenance records,
including indefinite reuse of a prior `provider_managed` result, removes error
redaction, and no longer fails closed when durable maintenance history is
unavailable. In `backend/internal/modelintelligence/maintenance.go`, it drops
the selected profile's provider-ID consistency check. It also removes the
provider-specific restriction that prevents health-only checks from being
treated as verified artifact freshness. In
`backend/internal/llm/maintenance_scheduler.go`,
it removes lifecycle admission for maintenance runs and makes a missing
background-processing gate permissive instead of denying the run. In
`backend/internal/modelintelligence/provider_registry.go` and `service.go`, it
removes provider-probe age expiry and secret redaction, bypasses the active
profile check before inference, and removes validation of returned provider,
model, lane, success status, output text, and numeric metrics. These changes
would allow stale or inconsistent provider evidence and unverified inference
results to pass through, and weaken runtime and privacy controls.

This is a targeted source-level safety review, not proof that every file in the
770-entry dirty candidate worktree has been reviewed. No candidate files were
copied or modified; the source worktree remains preserved. Other changed areas
still require separate scoped review before integration or cleanup.

### October 12, 2026: connected-source candidate safety review

Continued the targeted review of `hai-model-maintenance-audit` against PR #36.
The source connector changes are not ported. The candidate removes the
JSON-feed HTTP dial path's blocked-address checks and DNS-pinned dialing,
including protections for private, link-local, multicast, carrier-grade NAT,
and cloud metadata addresses. Its Trello request path also removes the guard
that limits credentials to `api.trello.com` over HTTPS port 443 (with loopback
allowed only for tests). Those changes weaken SSRF and credential-exfiltration
boundaries.

The candidate additionally removes Google OAuth state binding to the source,
owner, and connector; atomic token persistence that rechecks the source binding
inside the database transaction; refresh-token compare-before-save; and
revocation/reconnect-state checks before token use. This would permit stale or
misbound OAuth state and grants to be accepted across source changes. The
current PR retains these checks. No files were copied or changed in the source
candidate worktree. This remains a focused review of source/OAuth paths, not a
complete review of the candidate's 770 dirty entries.

### October 12, 2026: identity and session candidate review

Reviewed the HAI backend JWT verifier, IDP authentication handlers/service, and
identity route changes in `hai-model-maintenance-audit` against PR #36. The
candidate was not ported. It removes bounded multi-key JWT verification and
strict token parsing checks for canonical base64url encoding, duplicate JSON
claims, critical headers, stable principal presence, and consistent `iat`,
`nbf`, and `exp` ordering. The candidate verifier also drops enforcement of
`nbf`, so a correctly signed token with a future not-before value can be
accepted early.

The IDP candidate removes rate limiting for public registration, login, and
password-reset confirmation, drops request-origin validation on logout, and
removes readiness checks for the durable session authority and revocation
store. These weaken token validation, brute-force resistance, logout
protection, and fail-closed authentication startup. The candidate worktree was
not changed; this remains a path-scoped review, not a complete audit of its
770 dirty entries.

### October 12, 2026: task approval and delivery candidate review

Reviewed the task execution, automation approval-proof, and agent-coordination
changes in `hai-model-maintenance-audit` against PR #36. None were ported. The
candidate removes the pre-review automation configuration snapshot used to
bind approval to the reviewed configuration. Its task executor also drops
request-context propagation and cancellation checks, the distinction between
failure before dispatch and uncertain launch outcome, receipt identity/status
validation, and sanitization of runtime output and audit fields. Approval proof
consumption loses its bounded timeout and explicit reconciliation result for
an uncertain persistence outcome.

In agent coordination, the candidate abandons the idempotency claim after any
transport error, rather than retaining it when delivery may already have been
accepted. A retry could therefore duplicate an externally accepted message.
The timeout evaluator also removes the due-time guard for subsequent reminders,
so repeated scheduler scans may issue reminders before the configured interval
elapses. These changes weaken approval binding, cancellation, outcome
reconciliation, privacy, idempotency, and follow-up cadence. The candidate
worktree remains untouched; this review covers these execution paths only, not
all 770 dirty entries.

### October 12, 2026: account-feed and operational authority review

Reviewed the account-feed fetcher and operations changes in
`hai-model-maintenance-audit` against PR #36. The candidate weakens feed
boundaries: local files use `SafeJoin` followed by ordinary `os.Open` instead
of secure-root open and file-identity verification; loopback feeds no longer
require an exact opt-in; URL credentials, fragments, and secret-bearing query
keys are no longer rejected; the HTTP transport no longer bypasses environment
proxies or caps response-header bytes; and special-use, metadata, and
carrier-grade NAT address protections and their security tests are removed.
With a configured proxy, destination validation may apply to the proxy rather
than the feed host. These changes increase local-file race, SSRF, and credential
disclosure risk.

The candidate also deletes the current PR's operations safe-effect boundary,
execution-claim authority, source-approval and source-observation components,
along with their atomicity, ownership, generation, and replay regression tests.
The deleted safe-effect scope binds the owner/workspace, operation version,
worker claim, source identity/revision, observation generation, and source
configuration epoch. No equivalent authority modules were found in the
candidate's operations package. The focused comparison accounted for more
than 22,000 removed lines across 102 account-feed/ambient/operations/source-
evidence paths; this is not a file-by-file review of the 770-entry candidate.
No candidate files were copied or modified.

### October 12, 2026: ambient scan authorization and durability review

Reviewed the ambient service and scheduler changes in
`hai-model-maintenance-audit` against PR #36. The candidate's scheduler treats a
missing background-processing callback as permission to run, and the legacy
scan path does not accept the request-scoped safety gate. Its scan reads
unscoped workflow dashboards/items, memory dashboards, pursuit data, and
repository records rather than requiring the owner-scoped/contextual interfaces
used by the current PR. The candidate also removes durable scan checkpoints,
context-aware persistence, the explicit unconfirmed-outcome state, and
acknowledgement handling/tests. On scan failure it best-effort updates the
record and ignores whether that update succeeded.

These changes weaken account isolation, emergency-stop/background policy
enforcement, and recovery after an uncertain scan result. No ambient candidate
files were copied or modified. This was a focused scheduler/scan review, not a
complete audit of ambient or ambient-monitor code.

### October 12, 2026: verification provenance and memory-promotion review

Reviewed the verification and memory changes in
`hai-model-maintenance-audit` against PR #36. The candidate removes
`VerifiedFactPromotionRepository` and the PostgreSQL transaction that
revalidates promotion evidence against persisted owner-bound verification
runs, recent explicit memory opt-in, source-supported low-risk claims, absence
of contradictions, exact used evidence, matching source provenance, and an
exact sentence in the cited source. A symbol search found no equivalent
promotion boundary in the candidate.

The candidate also deletes the source-extraction correction-lesson API and
transactional persistence path keyed to owner, extraction identity, and source
revision, plus its PostgreSQL and regression tests. These removals weaken
evidence-grounded memory promotion and correction provenance. No files were
copied or changed in the candidate worktree. This is a focused review of
verification/memory promotion, not a complete memory subsystem audit.

### October 12, 2026: runtime admission and artifact boundary review

Reviewed the agent-runtime and OpenClaw artifact changes in
`hai-model-maintenance-audit` against PR #36. The candidate deletes
`backend/internal/agentruntime/execution_admission.go` and its contract tests.
That current-PR fence bounds a final emergency-stop check and the durable
host-job/process-start admission in one short serialized operation; no
equivalent admission-fence symbol was found in the candidate runtime package.

The candidate also removes the OpenClaw signed-artifact URL validation and
redaction implementation and its URL/download contract tests, along with the
managed-archive validation/rollback implementation. In the upload handler it
drops the concurrent-upload semaphore and the ZIP compression-ratio check.
Together these remove protections around network artifact retrieval, secret
URLs, archive handling, resource exhaustion, and recovery. No runtime candidate
files were copied or modified; this review does not cover every deleted
OpenClaw integration or every runtime behavior.

### October 12, 2026: durable jobs, event delivery, and database roles review

Reviewed the durable-job, event, and database infrastructure deltas in
`hai-model-maintenance-audit` against PR #36. The candidate deletes the
review-recurring settlement implementation and its tests. The current PR uses
a two-phase settling barrier so a successor job is not released until the
first terminal transition is acknowledged, and requires reconciliation when a
commit result is uncertain. The candidate has no `CompleteReviewRecurring`
replacement.

The candidate also deletes the transactional event outbox and its management
handler/tests; no `EnqueueTx` or `OutboxStore` replacement was found. It removes
the PostgreSQL runtime-role provisioning and bounded execution-connection
manager, including their migration/runtime separation and pool lifecycle
contracts. These are material losses in reliable job recovery, durable event
delivery, and database least privilege. No candidate files were copied or
modified; this is a focused infrastructure review, not a complete audit of
durable jobs, event delivery, or database lifecycle behavior.

### October 12, 2026: DeepSeek Harness bridge execution gate

Reviewed the candidate Windows DSH bridge and adapter against PR #36. The
candidate intentionally fails closed: configuration, worker-loop, lease
acquisition, and process execution return
`errDSHIsolationUnavailable`. Its stated missing prerequisites are TLS peer
pinning, a verified OS-enforced least-privilege sandbox, and a server-to-Windows
process-start acknowledgment protocol that orders Resume against emergency
stop. This prevents the candidate bridge from starting, but it is not a working
DSH integration and does not prove those controls are available on Windows.

The separate `hai-runtime-safety-audit` candidate was not ported because its
DeepSeek/host-runtime changes remove cancellation receipt identity checks,
request-context failure handling, environment filtering, bounded/redacted
output, reparse-point rejection, and durable start-intent/acknowledgment
recovery. The reviewed material therefore supports a release gate, not enabling
host execution: DSH must remain unavailable until the isolation and start/stop
ordering controls are implemented and verified on Windows. No candidate code
was copied, and no live host-process acceptance test was run.

### October 12, 2026: migration-history integrity comparison

Compared `backend/internal/infra/migrate.go` and its migration tests in
`hai-model-maintenance-audit` with the exact PR #36 head. The candidate removes
the `schema_migrations` content-checksum column and the `MigrationChecksum`,
applied-record loading, checksum validation, source validation, and explicit
legacy-checksum adoption path. An already-applied migration whose checked-in
SQL changes would therefore no longer be detected at startup. It also removes
the out-of-order/gap check, allowing an applied later migration to pass even
when an earlier migration in that phase is absent. Related checksum and
ordering contract tests are removed as well.

The current PR retains these integrity checks. The candidate migration changes
were not ported; they weaken detection of schema/source drift on databases
whose data must be preserved. This is a focused migration-runner comparison,
not a live upgrade/rollback rehearsal against Robert's database.

### October 12, 2026: completed supplemental report-to-PR crosswalk

Mapped the three completed supplemental reports (Hubble, Popper, and Anscombe)
to the current PR #36 tree at `8f030a74d230ef102741859667c0862df5d24d73`.
This is a report-level behavior crosswalk; the source worktree that originally
held their diffs is unavailable, so it does not claim byte-for-byte patch
provenance or reconcile every intermediate patch event in the 4,651,926,405
bytes of source transcripts.

- Hubble's reported framework-registry behavior is represented in the current
  frontend implementation and focused specs: request-field focus after empty
  validation; owner plus `canAdminister` authorization; verified-owner-only
  mutations; loading, unavailable, and empty-state distinction; redacted
  credential-like failures; duplicate-submit prevention; and keyboard/focus
  handling. Relevant files include the registry component, inspector, and
  recommendation components and their specs under
  `frontend/src/app/pages/framework-registry/`. The current PR contains focused
  cases including `moves focus to the request field when empty selection
  validation fails`, `requires both the signed owner role and administration
  permission`, `fails closed when signed authorization capabilities are
  unavailable`, `does not expose credential-like client error text`, and
  `prevents duplicate preference submission while a save is running`.
- Popper's icon correction is represented by `BulbOutline` and `StarOutline`
  registrations in `frontend/src/app/hai-icons.ts`, the star/bulb selection in
  `frontend/src/app/services/theme.service.ts`, and a theme regression test
  asserting that the unsupported `moon` icon is absent.
- Anscombe's CI hardening is represented by the authenticated-smoke aggregator
  requiring a nonzero pass count and zero failures for each expected suite in
  `.github/workflows/ci.yml`, alongside the contract test
  `test_authenticated_smoke_requires_each_suite_result` in
  `scripts/test_ci_contract.py`. The workflow also uses uncached race tests,
  exact migration-test names, and explicit timeouts in the corresponding jobs.

Local verification: `python scripts/test_ci_contract.py` passed 110 tests.
Angular tests were not rerun from this clean audit worktree because its
`frontend/node_modules` is absent. The completed reports record their own
focused Angular results (Hubble 36/36; Popper 2 tests plus production build),
but those historical results are not current-head CI evidence. At this ledger
entry, PR #36 run `38187618372` is still queued. Treat these three reports as
behaviorally represented in the PR source, with current frontend CI pending;
do not use this crosswalk alone to authorize deletion of their source
transcripts or any of the six unfinished supplemental transcripts.

### October 12, 2026: supplemental source integrity revalidation

Reran `scripts/test-hai-supplemental-transcript-cleanup-readiness.ps1` against
the two inventoried source roots. The read-only verifier returned
`supplemental_source_archive_verified`: all 9 manifest files matched their
paths and recorded lengths, the three completed-candidate SHA-256 values
matched, and all 21 report source paths resolved. The two roots total
14,013,737,298 logical bytes; the three completed candidates total
4,651,926,405 bytes, and the six aborted/nonterminal files remain retained.
The verifier reported `deletion_performed: false` and
`cleanup_authorized: false`. This confirms source integrity, not semantic
integration; no source transcript or worktree was modified or deleted.

### October 12, 2026: refreshed integration-snapshot blob comparison

Compared the 1,790 staged paths in the preserved
`D:\codex-temp\hai-integration-snapshot` index with PR #36 tree
`89dd87276a6c545a5f994549bfd95697c1371d29` using Git blob IDs. A total of
1,501 staged files are byte-identical to the PR tree; 289 are different or
snapshot-only. Four of those 289 are new paths. This is a changed-file queue,
not a count of unintegrated features: the older October 11 comparison had a
different PR head and must not be reused as current evidence. The snapshot
remains unchanged with 19 unstaged paths and no untracked paths.

Reviewed the four snapshot-only paths. The two operations-dedupe migration
files are already represented by PR #36 migrations `0006` up/down: their Git
blob IDs match exactly despite the snapshot assigning the candidate migration
number `0004`. No data cleanup was added or inferred. The candidate static
`nginx-config/nginx.conf` and `sites-enabled/generic-auto.conf` are not copied:
the former contains a literal `change-this-local-backend-key` value and a
legacy static service-routing layout; the current PR uses its Gate nginx
template and the governed nginx configuration manager for dynamic routes.
The static candidate was not validated against the current container topology.
No snapshot files were changed. Current-head CI run `38188151400` for PR #36
remains pending; the path review is limited to these four additions.

### October 12, 2026: focused progressive-control frontend comparison

Compared the staged snapshot versions of the Control Center, Governance
Control, Runtime Control, Task Blueprint, and Workflow Engine with PR #36 at
`89dd87276a6c545a5f994549bfd95697c1371d29`. The snapshot changes were not
ported because the current PR has stronger data, approval, and recovery
boundaries:

- Control Center: the snapshot calls full ambient and pursuit endpoints rather
  than their summary projections, removes the requirement for complete
  needs-Robert/blocked/stale counts, and counts by list length. The backend
  already tests `TestDashboardSerializesEmptyQueuesAsArrays`, while the current
  frontend intentionally distinguishes incomplete counts from a verified
  empty queue. No evidence supports replacing these contracts with the older
  full-dashboard requests.
- Governance Control: the snapshot loads advanced agent, life-context,
  ledger, and proactivity surfaces on initialization instead of restoring only
  the advanced sections previously opened. The current specs assert
  `restores only advisory engines whose sections were saved open`,
  `loads full controlled-learning outcome history only when its section opens`,
  and `loads full authorization evidence only when its section opens`.
- Runtime Control: the snapshot removes the durable resume-review request,
  validates review evidence and the stop revision, and requires the exact
  typed confirmation `RESUME BACKGROUND PROCESSING`. Current tests assert the
  review is prepared without resuming, reject incomplete/mismatched review
  evidence, and prevent resume without explicit confirmation.
- Task Blueprint: the snapshot replaces the 900,000 ms execution timeout with
  the 20,000 ms ordinary-operation timeout and removes MCP call/result
  identity-pairing helpers. Current regression tests cover
  `pairs each successful MCP tool call with the result that call returned`
  and explain failed calls without misattributing a result.
- Workflow Engine: on `reviewRequired`, the snapshot calls `uncertain()` and
  returns, omitting the current PR's selected-record reload and queue refresh.
  Those steps reconcile a possibly completed operation before more work is
  authorized; the current spec asserts the worker enters review-required
  state.

The snapshot versions of the shell also remove the mobile-navigation focus
guard and document-title update; the current versions retain both. These
reviews establish that the examined snapshot deltas are not missing behavior;
they do not complete review of the remaining differences in the 289-path
queue. No snapshot content was copied or modified. Frontend tests were
inspected but not rerun locally in this dependency-free audit worktree; exact-
head GitHub CI remains the execution gate.

### October 12, 2026: connected-source backend comparison

Compared the staged Git index in the preserved
`D:\codex-temp\hai-integration-snapshot` with PR #36 at
`de5b86fed426d731405374177b76f1bebc9f1df1`. The snapshot differs across 17
paths under `backend/internal/source/` (2,917 staged paths and 3,147 paths in
the PR tree overall). The staged index was read directly; the snapshot's
unstaged edits were not treated as candidate content. None of these source
deltas were ported because they remove or weaken current behavior:

- OAuth removes persistence and atomic consumption of owner-bound,
  single-use Google OAuth state, weakening callback replay protection.
- Repository and handler changes remove owner-scoped extraction lookup and
  bounded history queries. The snapshot handler instead loads all owner
  extractions before filtering and truncating in memory.
- Search removes request-context cancellation and invokes semantic retrieval
  with `context.Background()`; the corresponding cancellation test is deleted.
- Scheduler changes remove runtime scheduler status reporting, including
  disabled, durable, and explicitly degraded volatile-fallback states and
  their regression tests.
- Sync changes remove the fail-closed Autofever feed-contract guard and
  rejected-LARO-cursor recovery, and remove the test cases that assert those
  safety and recovery behaviors.
- Durable sync changes remove the handling that completes queued work when a
  source is paused, risking provider retries for an obsolete operator intent.
- The snapshot also deletes `history_repository.go`,
  `repository_postgres_test.go`, and source-search cancellation coverage.
  Remaining small differences include extraction pagination assertions and
  Trello test context plumbing; they do not justify adopting the regressive
  snapshot versions.

This is a source-delta review, not a live Google/Trello provider acceptance
test. No source, snapshot, or worktree files were deleted or moved. PR #36
head remains `de5b86fed426d731405374177b76f1bebc9f1df1`; GitHub run
`38188496240` was still queued at the time of review, so the complete PR head
is not yet CI-validated. PR #35's separate CI run `38175351645` completed
successfully across all ten reported checks and is not evidence for PR #36.

### October 12, 2026: task and ChatGPT-log tool-loop comparison

Compared the staged snapshot index with PR #36 head
`9cd49d8ccd4eea9bcb7a98e330d8397840f8a7b4`. The overall tree comparison has
644 differing paths: 400 shared paths have different blobs, 237 PR paths are
absent from the snapshot index, and seven snapshot paths do not exist in the
PR tree. This task-focused pass covers 29 differing paths under
`backend/internal/task/`; it reads staged blobs and leaves the snapshot's
unstaged changes out of scope.

- The three PR-only ChatGPT-log files (`chatgpt_logs_tool_loop.go` and its
  context and tool-loop tests) are absent from the snapshot. They are already
  in the current PR and were not reconstructed from an older snapshot.
- The snapshot's resource planner computes automatic runtime admission from
  risk alone and drops the `AutomatedExecution` request gate. This would allow
  low-risk work to enter an automatic runtime path even when the request did
  not authorize automated execution; retain the PR implementation.
- The snapshot removes context-aware task-operation claim, heartbeat,
  completion, cancellation, and review transitions, reverting repository
  operations to non-cancellable calls. It also removes cancellation tests.
- The task history handler loses bounded limit validation and compact list
  projections, instead returning full owner-scoped completion records. The PR
  keeps list payloads bounded and offers an explicit full-detail view.
- The diff also removes numeric/state repository regression tests and reduces
  coverage for required participant admission, plan binding/projection,
  resource planning, and task-operation transitions. No snapshot test result
  establishes those deletions as safe.

No task code was ported: the PR contains the stronger and more complete
behavior. PR #36 run `38188686693` for the new head was pending at the time of
this audit; no CI result is inferred from source comparison. Snapshot files
and local artifacts remain untouched.

### October 12, 2026: execution-authorization comparison

Compared ten staged snapshot paths under `backend/internal/executionauth/`
with PR #36 at `5a117f8c53108d8e5e99585b7cc51a22fa3c9937`:

- The snapshot removes the inspection API's `view=summary` contract and its
  tests. The current PR can return a minimal receipt projection separately
  from full inspection detail; keep that bounded representation available.
- The snapshot removes control-plane and operations-control decision IDs from
  persisted authorization-receipt references and removes the corresponding
  `opscontrol-review:` resolution path and provenance tests. Those references
  bind authorization to the actual reviewed decisions and must remain.
- It deletes the explicit system workload policies for free task-engine
  inference, including the local read-only tool-loop policy, and their
  provider/runtime binding check. No broad or paid inference permission is
  implied by this audit; the existing scoped policies remain the intended
  boundary.
- The snapshot adds life-ontology projection calls to authorization receipt
  `Get` and `List`. The current test
  `TestAuthorizationReadsDoNotRunLifeGraphProjection` explicitly requires
  authorization reads to be side-effect-free, so this snapshot addition is
  not portable without changing that contract and its rationale.
- The snapshot also removes the isolated emergency-stop evaluator clone and
  deletes workload-policy, inspection, and projection regression coverage.
  Its PostgreSQL advisory-lock rewrite is unnecessary to recover any missing
  feature and is not adopted. The rewrite also embeds NUL bytes in a value
  sent to `hashtextextended(text, 0)`; PostgreSQL `text` rejects NUL. The PR's
  hex-encoded lock identity avoids that invalid parameter.

No authorization code was ported. This pass reviewed the staged snapshot
blobs and current source/tests only; no live database or external execution
provider was invoked. PR #36 run `38188757244` was pending during review.
Neither the snapshot nor any other local source artifact was removed.

### October 12, 2026: route registration and control-plane comparison

Reviewed 17 staged snapshot differences under `backend/internal/router/`
against PR #36 at `c70aae7151fc0a3c817672dda9d271bcd62109ea`, including route
registration, middleware setup, readiness responses, identity propagation,
and their associated tests. The snapshot was not ported:

- It removes ChatGPT logs route/service composition and operational-graph
  route composition, leaving these PR features unreachable through the
  registered API. Browser verification is registered without the current
  workflow-linker composition, dropping workflow linkage.
- It removes the operations-control review repository and tests, stops
  connecting the owner-bound review repository and resolver to the
  authorization path, and removes the review repository from ops-control
  service composition.
- It removes the automation event outbox route, durable outbox dispatcher,
  and notifier wiring, replacing the publisher dependency with a direct
  publisher value. The snapshot does not preserve the outbox-backed recovery
  path.
- It collapses the protected detailed readiness endpoint into the aggregate
  readiness handler and includes individual dependency checks in that
  response. The current PR keeps detailed topology behind owner authentication
  and a read permission, and disables caching for readiness output.
- It removes lifecycle, readiness, automation-permission, and legacy
  control-plane permission tests. The current route tree also maintains
  operator-scope identity propagation that the snapshot deletes.

The snapshot adds a context-free route-initialization wrapper, but that alone
does not justify importing its broader route and guard regressions. The
separate legacy `request_limits.go` helper has no production caller in the
current router; the runtime JSON-body limit is implemented in
`request_body_limit.go` and remains mounted, so its deletion was not counted
as a live request-size-control loss. No runtime route or identity code was
changed. PR #36 head CI remained pending; this review is static source
comparison, not a live route-security acceptance test.

### October 12, 2026: operations-control authorization comparison

Reviewed 13 differing staged snapshot paths under
`backend/internal/opscontrol/` against PR #36 at
`560f3effb41f2a1bc2c1fab434836ff2f7faa1bb`. Nine PR paths are absent from
the snapshot index, including durable approval/resume-review implementations
and their tests. The staged snapshot is not a safe replacement:

- Its direct control-authorization path substitutes the configured owner for
  the authenticated actor when binding the safety effect. The PR binds the
  action to the authenticated actor, preventing an owner configuration from
  standing in for the caller's identity.
- It removes the immutable-stop check that prevents an owner approval from
  clearing an emergency stop raised by a non-persisted safety source.
- Its `Resume` handler disengages the stop from a direct authorization
  request. The PR requires a durable owner review, a fresh exact-bound
  approval, and the explicit confirmation `RESUME BACKGROUND PROCESSING`;
  approval preparation alone does not resume background work.
- It removes the durable control-review repository and approval resolver
  wiring, including tests for persisted review evidence and lifecycle.
- It changes an already-disengaged stop from an explicit
  `ErrControlChangeNotRequired` result to success, changing the control API's
  idempotency/error contract without a demonstrated need.

No operations-control code was ported. This is static comparison of staged
source and tests; emergency-stop behavior was not exercised against a live
runtime. PR #36 CI status is tracked separately and remains pending until a
successful check for the exact pushed head is observed.

### October 12, 2026: ambient and proactivity comparison

Reviewed eight differing staged files under `backend/internal/ambient/`,
seven under `backend/internal/ambientmonitor/`, and three under
`backend/internal/proactivity/` against PR #36 at `efc0f714`. No ambient or
proactivity code was ported:

- The snapshot deletes `OverviewSummaryForOwner` and the handler's validated
  summary view. That reverts the PR's small owner-scoped dashboard projection
  to loading full opportunity history; tests that verify summary bounds and
  skip loading opportunity records are removed.
- It deletes tests for one owner-scoped pursuit read, fail-closed behavior
  when owner lookup fails, rejection of cross-owner/unscoped dashboard data,
  and non-pursuit visibility that does not read pursuit state. These are
  important tenant-isolation and minimal-read contracts.
- The snapshot removes the batched fingerprint lookup and the persistence
  optimization that avoids unchanged hourly writes while refreshing freshness
  daily. It also removes tests for bulk lookup, semantic-change writes, and
  daily freshness. These resource-saving behaviors already exist in the PR.
- It removes scheduler status reporting and its disabled-policy test.
- Ambient-monitor collectors in the snapshot read counts and rows in separate
  queries rather than a read-only repeatable-read transaction. That can make
  the displayed count and captured record set disagree under concurrent
  updates. A monotonic completion timestamp adjustment and composition
  eligibility calculation are also absent; the related completion-clock test
  is deleted.
- Proactivity drops its narrowly bounded two-microsecond logical-clock
  allowance for durable monitor transitions and deletes its test. This is not
  a reason to accept unbounded future timestamps; retain the current bounded
  rule.

These are source and test comparisons, not live ambient polling or
cross-account runtime acceptance. Snapshot files remain unchanged. The latest
PR #36 head is tracked above; queued CI from preceding heads does not prove
this or the current head's runtime behavior.

### October 12, 2026: pursuit-planning comparison

Reviewed ten differing staged paths under `backend/internal/pursuit/` against
PR #36 at `ae577926833c1ebcdf22f726c5700f1a2c9b6374`. Five substantial PR
files, including `dashboard_projection.go` and `dashboard_repository.go`, are
absent from the snapshot. No pursuit code was ported:

- The snapshot replaces the identity-aware legacy owner predicate with
  unconditional visibility of ownerless rows (`owner_identity = '' OR IS
  NULL`) for every authenticated owner. The PR's
  `TestOwnerVisibilitySQLAllowsLegacyRowsOnlyForConfiguredOwner` verifies
  legacy visibility is restricted to the configured migration owner.
- It removes the validated `view=summary` dashboard contract and the
  owner-scoped summary implementation. The PR-only summary projection bounds
  queue previews and omits private detail; its handler/service tests are
  absent from the snapshot.
- Intake in the PR returns the exact transient `IntakeWorkflowID` produced by
  the operation. The snapshot discards that result and re-reads the pursuit,
  which cannot reproduce a response-only workflow reference; its exact
  workflow-reference test is removed.
- The snapshot narrows `FindTaskAttemptsNeedingReview` in SQL. The PR applies
  the canonical `pursuitTaskAttemptNeedsReview` predicate after conservative
  candidate selection. The snapshot query omits some statuses handled by
  that predicate, including `error`, canceled, unsupported, and conflicting
  run outcomes, so it can hide items requiring review.
- The resource-ledger refactor mostly inlines the PR's calculation helper,
  but the snapshot removes associated coverage without showing a safer
  contract. It was not necessary to recover a missing feature.

This is a staged-source and test comparison, not a production multi-account
acceptance test. Snapshot data remains intact. PR #36 CI was still queued on
prior heads; no current runtime verification is inferred here.

### October 12, 2026: framework registry and agent-role comparison

Reviewed nine differing staged paths under `backend/internal/frameworkregistry/`
against PR #36 at `5ef9acdb49b118714fe8488fc17f66e84379701a`:

- The snapshot changes life-domain matching so a generic `health` signal
  always selects personal health, removing the distinction between personal
  care and operational health probes. It deletes tests for mixed clinical
  context and runtime-health classification.
- It removes the explicit allowlist mapping implemented deterministic HAI
  control-plane services to agent roles. The PR keeps unavailable external
  specialists explicitly unassigned rather than silently claiming the
  generic engine implements them; the snapshot deletes both role-contract
  tests.
- It removes the owner-scoped framework selection detail handler and its
  authorization test, along with catalog-source coverage.
- It removes stable empty-array normalization for conflict/adaptation fields,
  which can turn a valid empty collection into JSON `null` for strict clients.
- The snapshot changes the PostgreSQL advisory-lock key to concatenate owner
  and framework IDs with a NUL byte, then passes that value as PostgreSQL
  `text` to `hashtextextended`. PostgreSQL text cannot contain NUL; keep the
  current unambiguous hex encoding and its text-safety regression test.

No framework-registry code was ported. These checks are static source and
test comparison, not live PostgreSQL or provider validation. PR #36's current
head run remains a separate release gate; queued runs do not count as pass.

### October 12, 2026: brain catalog and upstream review comparison

Reviewed ten differing staged paths under `backend/internal/braincatalog/`
against PR #36 at `fa8adb974edd202234b180b5e2bcfa43a2ffeeba`:

- The snapshot removes the ChatGPT/Codex conversation-history MCP catalog
  entry even though the current PR contains its bounded, read-only integration
  and model-directed tool loop. The snapshot therefore does not add this
  requested capability; it drops catalog discoverability for code already
  present in the PR.
- Upstream entry, OSS Insight collection, and repository-discovery review
  paths lose caller-context propagation and use `context.Background()` for
  outbound review work. The PR-only tests
  `TestUpstreamReviewStopsWithCallerContext`,
  `TestCollectionReviewStopsWithCallerContext`, and
  `TestRepositoryDiscoveryStopsWithCallerContext` verify prompt cancellation.
- The catalog handler loses its cached JSON/ETag response and re-marshals the
  complete catalog on every request. This is a needless resource regression
  for a static payload, not a missing feature.
- A2A's snapshot catalog entry describes only a local bridge and omits the
  PR's separately guarded fixed-ngrok opt-in. The PR's exact-origin,
  production-mode, and local-login-bypass conditions remain the current
  reviewed boundary; no broader remote-agent authority is implied.
- Candidate repository discovery changes were compared as static source;
  no verified, safe missing capability justified copying the snapshot's
  alterations, and its tests do not establish live OSS Insight correctness.

No brain-catalog code was ported. This comparison does not establish current
upstream availability or live provider correctness. Snapshot files remain
preserved, and PR #36 CI must pass for the exact head before the PR can count
as release-validated.

### October 12, 2026: execution-approval resolver comparison

Reviewed four differing staged paths under `backend/internal/executionapproval/`
against PR #36 at `3e36e173005eb298448d528ba77409fd4910cca4`:

- The snapshot deletes `OpsControlReviewResolver` and its tests. The PR
  resolver accepts only a fresh, owner-approved review decision whose
  immutable task identity binds to the exact emergency-stop control-effect
  digest; it verifies owner, review item, decision, approver, freshness, and
  digest before producing resolved approval evidence.
- The snapshot removes `opscontrol-review:` dispatch from the composite
  resolver, so even a valid review cannot reach the execution authorization
  boundary. Routing tests that prove this namespace cannot fall through to
  generic task/workflow/portfolio resolvers are removed.
- The existing composite resolver's no-fallback behavior remains important:
  a selected resolver rejection must not be retried against a different
  approval namespace.

No approval code was ported. This source review complements the operations-
control comparison above; it is not a live database or emergency-stop
acceptance test. The snapshot remains intact and PR #36 CI remains the exact-
head release gate.

### October 12, 2026: verification service and persistence comparison

Reviewed the staged snapshot under `backend/internal/verification/` against
PR #36 at `83383e1eeb19da0a17f37d41a88948e320ee39a9`:

- The snapshot removes `AnswerContext`/`AnswerWithContext`, caller-context
  checks during source search and provenance resolution, and the cancellation
  path that marks an interrupted run for review. Provenance lookups now use
  `context.Background()`. It also deletes
  `TestAnswerContextStopsBlockedEvidenceSearchAndMarksRunForReview`, so a
  cancelled or timed-out verification can continue unnecessary external work
  without the PR's interrupted-run state transition.
- The snapshot removes the 200-run cap from both repository history queries.
  `TestVerificationRunHistoryIsBounded` is deleted; long-lived installations
  can load an unbounded verification history into routine reads.
- The PR's `verificationRunsForOwnerQuery` preserves ownerless legacy rows
  only for the explicitly authorized migration identity. The snapshot removes
  that policy helper and its owner-query coverage, making those legacy runs
  invisible to the migration operator. This is a compatibility/recovery loss;
  it does not justify broadening legacy access to other users.
- The snapshot's new evidence/claim persistence helper discards the objects
  returned by `CreateEvidence` and `CreateClaim` and does not reject nil or
  zero-ID results. The PR verifies durable identifiers before finalizing the
  run. Retain that verification even when finalization is wrapped in the
  snapshot's transaction helper.
- The staged snapshot removes `TestFindRunForOwnerUsesOwnerAndIDPredicates`.
  Its optional owner-scoped repository interface can fall back to a
  `FindRunsForOwner` scan; preserve an explicit test that both direct and
  fallback detail reads enforce exact owner and run ID predicates.

The atomic finalization transaction is a potentially useful addition, but it
does not compensate for the removed cancellation, bounded-history, legacy
migration, and durable-ID guarantees. No verification code was ported. This is
a staged-source/test audit only; it does not establish live source-provider or
database behavior. Snapshot data remains intact and PR #36 must pass checks on
its exact current head before release validation.

### October 12, 2026: ambient monitor and collector comparison

Reviewed seven staged paths under `backend/internal/ambientmonitor/` against
PR #36 at `174346d58cbb378d207d8ffbd9ca5fc0540ce852`:

- The collector snapshot reads the aggregate count and record rows as separate
  database statements rather than in the PR's read-only repeatable-read
  transaction. Concurrent source changes can therefore make one observation
  combine a count from one database state with records from another.
- The snapshot changes `postgresSnapshotCursorKey` to compact JSON. Migration
  `0051` compares this value byte-for-byte with PostgreSQL's
  `jsonb_build_array(... )::text`, which includes a space after the comma. The
  deleted `TestPostgresSnapshotCursorKeyMatchesJSONBCanonicalText` covers the
  exact canonical representation; preserve it or the database integrity
  trigger can reject otherwise valid composition snapshots.
- The manual lease-recovery handler stops calling
  `RecoverExpiredCompositionLeases`, so it recovers collection leases but
  leaves expired composition-delivery leases untouched. The snapshot deletes
  `TestRecoverReturnsCollectionAndCompositionLeaseCounts`.
- `ProcessDue` no longer advances the composition-processing `asOf` time after
  a collector advances the service clock. Newly queued composition delivery
  can remain pending until a later sweep; the snapshot deletes
  `TestProcessDueComposesNewDeliveryWhenCollectionAdvancesClock`, which
  requires collection and its resulting delivery to complete in one pass.
- The snapshot removes the guard that advances a very fast collection's
  completion timestamp beyond its claim timestamp. The remaining test
  `TestProcessDueRecordsAfterACollectorObservationCreatedMillisecondsLater`
  exercises a clock that advances by 10 milliseconds, not the same-tick case;
  retain a direct equality/coarse-clock regression test and the monotonicity
  guard.

No ambient-monitor source was ported. These are source, SQL-migration, and
focused test-contract findings; they are not a live scheduler or production
PostgreSQL run. Snapshot files remain preserved. PR #36 exact-head CI was
queued at the time of this comparison.

### October 12, 2026: automation, workflow, assistant, and agent-cycle comparison

Reviewed the differing staged files under `backend/internal/automation/`,
`backend/internal/workflow/`, `backend/internal/assistant/`, and
`backend/internal/agentcycle/` against PR #36 at
`8eb28053f59dbbe29684b80c0680b02ac5f6671e`:

- Automation snapshot drops `CreateWithEvent`, `UpdateWithEvent`, and
  `DeleteWithEvent`, which commit each mutation and its event-outbox row in
  one database transaction. The service now persists the change first and
  publishes separately; a publish failure leaves changed state without its
  durable delivery intent. `TestCRUDUsesDurableAutomationEventsWhenRepositorySupportsThem`
  is removed. Keep the transaction/outbox path for durable repositories.
- The workflow handler snapshot removes its cancelled-request check after the
  worker returns, and deletes
  `TestRunDueHandlerHonorsCancelledRequestContext`. This allows a cancelled
  request to proceed into response handling instead of stopping at the
  cancellation boundary. The scheduler also stops reporting enabled,
  disabled, durable, fallback, running, and shutdown-rejected states to
  `schedulerstatus`; its disabled-state test is removed. This makes system
  health less observable without adding a replacement status source.
- A second workflow test for malformed chunked JSON is removed, although the
  handler implementation itself is unchanged in this diff. Record this as a
  coverage loss only; the comparison does not prove the current handler accepts
  malformed bodies.
- Assistant snapshot removes `CommandContext`, the task-engine context
  adapters, and context-aware agent-cycle execution, calling the plain
  `Run`/`Plan` methods instead. Its two cancellation-propagation tests are
  deleted, so request cancellation is no longer forwarded through the
  assistant orchestration boundary.
- Authenticated agent-cycle snapshot replaces the owner-scoped operational
  pass with a context/pursuit refresh and marks stale-claim recovery, source
  sync, follow-ups, safe workflows, ambient scanning, and workflow dashboard
  refresh as skipped. Six owner-scoped phase/learning tests and the
  cancellation test are removed; one replacement test asserts only the
  narrowed refresh. This is a substantial capability reduction, not a safe
  integration of the existing owner-scoped contracts.

No code from these snapshots was ported. Preserve durable event publication,
context propagation, authenticated owner-scoped operating phases, and
scheduler-status reporting. This comparison is static source/test evidence,
not live Kafka, scheduler, or assistant acceptance. Snapshot files remain
intact; PR #36 requires checks on its exact latest head.

### October 12, 2026: LLM maintenance, memory, and OpenClaw worker comparison

Reviewed the differing staged files under `backend/internal/llm/`,
`backend/internal/memory/`, `backend/internal/memoryengine/`, and
`backend/internal/openclawmaintenance/` against PR #36 at
`7e505c2b5ae9375b16bca871c518f587adb639c9`:

- LLM readiness in the snapshot stops rejecting provider endpoint query
  parameters and fragments. The PR explicitly rejects those URL components;
  query data is sent as part of provider requests and can contain credentials
  or other sensitive values. Preserve the rejection unless a separately
  designed, redacted secret transport is implemented.
- Model maintenance startup no longer checks `modelMaintenanceEnabled()` or
  whether any eligible enabled model exists. This can keep a periodic
  scheduler alive when there is no authorized maintenance work, contrary to
  the configured-work gate and the resource-control requirement. The snapshot
  deletes `TestServiceReportsWhetherModelMaintenanceWorkIsEligible`.
- The endpoint-key change rejects mount-prefixed API roots such as `/api/v1`
  and collapses endpoint identity to scheme/host, removing the PR's explicit
  support and `TestMaintenanceEndpointKeyAcceptsAMountPrefixButNotARoute`.
  This is a provider-compatibility regression for valid API gateways.
- Provider catalogue response allowance is reduced from 4 MiB to 256 KiB.
  That may save memory, but no fixture proves real provider catalogues fit the
  smaller cap; retain the documented bound or add size-based provider contract
  coverage before changing it.
- Memory query normalization loses search/kind/tag length bounds, the
  16-token cap, and control-character cleanup. It also removes the checked
  page-offset multiplication and computes `(page - 1) * pageSize` directly;
  sufficiently large page values can overflow to a negative index and panic.
  `TestQueryBoundsTextAndTokenWork` and
  `TestQueryHandlesPlatformMaximumPageWithoutOverflow` are deleted. Restore
  bounded inputs and overflow-safe pagination.
- The memory-engine repository no longer applies the configured migration
  owner's allowlist when reading legacy ownerless archives, and
  `TestConfiguredLegacyOwnerCanReadButNotDeleteOwnerlessArchive` is deleted.
  This makes legacy material unavailable to its recovery operator; do not
  broaden reads to ordinary accounts.
- The OpenClaw lease token check is equivalent as written: `strings.Trim` with
  the lowercase hexadecimal character set rejects any remaining non-hex
  character. The public signing-key representation is only reformatted; no
  unique functional addition was identified in these two files.

No LLM, memory, or OpenClaw worker code was ported. The findings are source and
unit-test comparisons, not live provider or model-update acceptance. Snapshot
files remain intact; PR #36 must pass checks on its exact latest head.

### October 12, 2026: events, database infrastructure, and operations comparison

Reviewed staged changes under `backend/internal/events/`,
`backend/internal/identity/`, `backend/internal/infra/`, and
`backend/internal/operations/` against PR #36 at
`7e505c2b5ae9375b16bca871c518f587adb639c9`:

- The snapshot changes an unavailable Kafka publisher into a success-returning
  no-op and removes reconnection/backoff state. In combination with the
  automation change that publishes outside the database transaction, failed
  or unconfigured Kafka can silently lose automation events after the data
  mutation has committed. The snapshot deletes
  `TestUnconfiguredPublisherFailsClosed`; do not treat the no-op as durable
  event delivery.
- The snapshot removes the GORM logger configuration that enabled
  parameterized-query logging and a defined slow-query threshold. This loses
  the PR's protection against interpolating bound values into logged SQL and
  reduces database diagnostics; keep the explicit safe logger configuration.
- Migration startup no longer honors the existing `DB_RUN_MIGRATIONS=false`
  gate. With that flag set and `DB_MIGRATIONS_ENABLED` unset, the snapshot's
  startup predicate defaults to enabled and can attempt schema migrations
  under a production DML-only API role. Preserve backward-compatible
  fail-closed migration gating.
- Operations claim SQL changes `operation_events.payload_json` to `payload`,
  but the checked-in baseline migration defines `payload_json jsonb`; no
  corresponding column migration is present. Both affected approval queries
  will fail against the repository schema. Keep the mapped column name.
- The snapshot removes the approved-status exception for `next_review_at` in
  due selection. A fresh exact-revision approval receipt previously superseded
  a self-hold reminder; the new unconditional review-time filter can defer
  approved work despite its valid receipt. Preserve the shared in-memory and
  PostgreSQL eligibility contract.
- The snapshot replaces `UpdateColumns` with `Updates` in the operation CAS.
  GORM then assigns `UpdatedAt` automatically, despite the operation and
  immutable audit event having their timestamps bound together. This can
  invalidate the expected lifecycle/audit timestamp identity; retain the
  explicit timestamp write semantics.
- Raw source-evidence fingerprints are removed in favor of comparing the
  stored JSON string with incoming bytes. PostgreSQL `jsonb` may reserialize
  equivalent JSON, so the snapshot can repeatedly treat unchanged evidence as
  refreshed and increment/audit it unnecessarily. The two raw-byte stability
  tests are deleted; retain canonical/raw-digest semantics and their coverage.
- Identity snapshot removes the operator-scope claim/context members. The
  related authenticated runtime-lab scope behavior is already recorded in the
  router audit; this confirms the loss originates in the identity contract,
  not merely route wiring.

No events, infrastructure, identity, or operations code was ported. The
operation SQL finding is grounded in the actual migration schema; Kafka and
database runtime effects still need provider-backed acceptance. Snapshot
files remain intact. PR #36 requires exact-head CI before release validation.

### October 12, 2026: worker pause and phase-two safety comparison

Reviewed staged changes under `backend/internal/safety/`,
`backend/internal/background/`, and `backend/internal/phase2/` against PR #36 at
`7e505c2b5ae9375b16bca871c518f587adb639c9`:

- The background worker removes its early paused-mode return and calls the
  ordinary operation-processing loop. In the snapshot, paused mode can claim
  and classify newly ingested items (and invoke configured model triage)
  instead of leaving them new. The two removed tests,
  `TestPausedModeIngestsButProcessesNothing` and
  `TestPausedModeDefersPreviouslyClassifiedSafeOperation`, specify that pause
  leaves new work untouched and only defers previously approved safe work.
  Downstream policy checks still gate actual safe execution, so this audit does
  not claim pause bypasses every execution authorization; it does remove the
  established no-processing pause contract.
- The phase-two PostgreSQL integration test removes its end-to-end case that
  creates a source-derived operation, records an owner approval receipt, and
  verifies that the approved operation can be claimed. This reduces live-DB
  coverage for the approval-to-claim boundary; retain it alongside the
  authorization unit tests.
- Test-only phase-two diagnostic output and its explicit opt-in test are
  removed. This is diagnostic coverage removal, not evidence of a production
  behavior change.
- `EvaluateImmutableEmergencyStop` is removed as a helper, but its environment
  stop check is inlined into `EvaluateEmergencyStop`; no other call site exists
  in the PR. This is a mechanical refactor, not a demonstrated stop bypass.
- No changed `backend/internal/processcontrol/` files were present in the
  staged snapshot, so this batch contains no process-control code to port.

No safety or phase-two code was ported. The pause finding is backed by the
actual worker control flow and removed assertions; production stop behavior is
not inferred from the deleted helper name. Snapshot files remain intact, and
the PR's exact-head checks remain the release gate.

### October 12, 2026: IDP cancellation and owner-scope comparison

Compared the staged snapshot's IDP authentication service, interface, HTTP
handler, and password-reset SMTP sender with PR #36:

- The snapshot changes `Logout`, `RefreshToken`, `IsUserAuthenticated`, and
  `RequestPasswordReset` back to context-free APIs. The PR checks for an
  already-canceled caller context before doing work and passes request context
  through the handler, service, and SMTP sender. The snapshot therefore loses
  cancellation propagation for session-store/database operations and password
  reset delivery.
- The snapshot SMTP sender creates its dial context from
  `context.Background()` and does not close an active SMTP connection when
  the caller cancels. The PR sender derives the dial timeout from the request,
  observes cancellation during the SMTP session, and has a regression test
  proving a canceled request stops before network access. That test is absent
  from the snapshot.
- The snapshot removes the configured first-run-admin operator-scope claim
  builder and `TestOperatorScopeIsBoundToConfiguredFirstRunAdmin`. The claim is
  consumed by privileged runtime routes; the identity/router audit above
  already records the related scope propagation loss. Do not replace this
  narrowly configured authority with role-only authorization.

The snapshot retains the durable session, refresh-rotation, and reset-token
logic visible in the compared files, but contributes no unique safe behavior
here. No IDP code was ported. This is a source/interface/test comparison, not
live SMTP or external identity-provider acceptance; snapshot files remain
intact.

### October 12, 2026: gateway, legacy service, and migration crosswalk

Compared the snapshot's new `nginx-config/nginx.conf`,
`nginx-config/sites-enabled/generic-auto.conf`, `generic-auto/server.go`,
`generic-auto/dockerfile`, and post-migration against PR #36's gateway,
Compose, and migration files:

- The new direct Nginx config embeds the literal
  `change-this-local-backend-key` instead of rendering the configured secret.
  It omits the template's local-preview secret boundary, host-runtime route
  denial, explicit IDP readiness proxy, and frontend listener-port contract.
  The PR's mounted/rendered template is the authoritative configuration;
  this extra direct config is not safe to substitute for it.
- The snapshot's generic-auto server replaces the PR implementation with an
  unauthenticated `http.ListenAndServe(":80", nil)` handler that serves a
  static page for every path, has no method restrictions, response security
  headers, timeouts, health contract, or graceful shutdown. Its Dockerfile
  downgrades Go to 1.17 and runs as root rather than the PR's pinned Go 1.25
  multi-stage image and non-root scratch runtime. No service behavior from
  this snapshot is an improvement to port.
- The snapshot adds
  `post/0004_operations_owner_workspace_dedupe_active`, but PR #36 already
  applies the same partial unique index in
  `post/0006_operations_owner_workspace_dedupe_active`. The snapshot's number
  collides with PR #36's `post/0004_automation_event_outbox`; adding it would
  conflict with the established migration sequence and duplicate the index.
- The new `sites-enabled/generic-auto.conf` only routes the legacy service;
  it does not replace the PR's authenticated product API routes or make that
  legacy endpoint canonical.

No gateway, generic-auto, or migration files were ported. Snapshot `.env-*`
files were identified by name only and were not read or copied; environment
files may contain credentials and are not PR artifacts. The live gateway and
database migration acceptance remain governed by PR #36's CI and deployment
checks.

### October 12, 2026: model, evaluation, and browser-verification crosswalk

Compared the snapshot with PR #36 in model-intelligence endpoint validation,
calibration, the fixed-suite LM-evaluation endpoint, browser verification, and
OpenClaw maintenance:

- The snapshot accepts `host.docker.internal` for every local model provider.
  PR #36 centralizes provider-aware validation and only permits the
  `ollama-local` Docker alias for the Ollama provider; keep the narrower
  endpoint policy so a provider entry cannot be redirected to an unintended
  local service.
- The snapshot removes the explicit `successes == 0` calibration branch. PR
  #36 returns a stable zero Wilson lower bound in that case instead of
  persisting floating-point noise, including negative zero. This is a small
  numerical correctness improvement already present in the PR.
- The snapshot LM-evaluation handler rejects only positive `Content-Length`.
  Go represents an unknown/chunked body length as `-1`, so such a request can
  pass the snapshot's “accepts no caller-provided data” check and trigger the
  fixed evaluation. PR #36 rejects every non-zero length, including unknown
  length. Preserve the stricter bodyless contract.
- PR #36 adds read-only browser-verification linkage to a workflow as a
  review-quality signal, without authorizing execution or changing workflow
  completion. The snapshot omits that integration and its result fields; it
  supplies no replacement behavior to port.
- The snapshot's OpenClaw lease-token validation and the PR's equivalent
  `IndexFunc` implementation both accept only 64 lowercase hexadecimal
  characters. The pinned signing-key byte encoding is a secret-scanner
  representation change, not a runtime behavior change.

No model, evaluation, browser-verification, or OpenClaw-maintenance files were
ported in this batch. Findings are code/test-contract comparisons; no live
provider, local browser worker, or model-maintenance run was performed.

### October 12, 2026: health, rate limiting, MCP, and domain-pack APIs

Compared the snapshot with PR #36 in backend health probes, rate limiting,
MCP preflight, domain-pack catalog, and Runtime Lab ownership:

- The snapshot rate limiter retains every unique client key indefinitely.
  PR #36 caps the local fallback map at 4,096 entries, prunes expired windows,
  and evicts an oldest active key only when necessary. Without that bound,
  requests with rotating keys can grow process memory without limit; the
  snapshot also deletes the two bounded-memory regression tests.
- The snapshot MCP preflight drops the configured bearer-token handling and
  never sends the token expected by a protected MCP listener. It also removes
  `text/event-stream` negotiation and parsing used by the ChatGPT logs bridge,
  and removes the explicit opt-in policy that permits a remote endpoint only
  with TLS and a usable bearer token. The current PR keeps tokens out of
  reports and requires remote opt-in; preserve those contracts.
- The snapshot removes the `view=summary` domain-pack catalog projection.
  The PR projection returns bounded pack metadata and method counts without
  exposing the full rules/playbook payload, and its test asserts those fields
  are absent. Keep the summary route for scalable initial loads.
- The snapshot health handler removes the shared serving-pool probe and
  background scheduler status probes. It also reports that empty Kafka
  configuration drops events, while PR #36 queues automation events in a
  durable outbox and delivers them when a broker becomes available. The
  snapshot's warning would misstate the current delivery contract.
- Runtime Lab in the snapshot again requires exact owner identity and removes
  PR #36's configured first-run owner-scope allowance. This matches the IDP
  and identity scope regressions above; keep the narrow role-plus-configured
  scope gate rather than broadening to role-only access.
- The inspected `a2abridge/handler.go` is identical in the staged snapshot and
  PR #36, so this area has no delta to port.

No health, rate-limit, MCP, domain-pack, or Runtime Lab code was ported. These
are source and test-contract findings; no remote MCP server was contacted.

### October 12, 2026: life-ontology bounds and monotonic timestamps

Compared staged snapshot code with PR #36 in life-ontology persistence/query,
proactivity snapshots, outcome evaluation, and safety-stop helpers:

- PR #36 adds `BoundedQueryRepository` so PostgreSQL applies owner, local-only,
  temporal, external-key, and row-limit predicates before returning life-
  ontology entities and relations. The snapshot removes this repository
  contract and falls back to loading/filtering records in the service. That
  transfers unbounded, potentially private graph data into application memory
  and defeats database-side query limits; keep the bounded repository path.
- PR #36 accepts an `AsOf` timestamp at most two microseconds ahead of its
  service clock, then advances the logical clock to that timestamp. This
  bounded allowance covers the monitor's claim/completion timestamp steps.
  The snapshot rejects any future `AsOf`, which can reject an otherwise valid
  immediate handoff. Larger future timestamps remain invalid in the PR.
- Outcome request normalization in the snapshot removes defensive copies of
  observations, corrections, and indicators before normalizing/sorting them.
  The PR copies those caller-owned slices before mutation, preserving caller
  data and avoiding mutation of shared slices during concurrent evaluation.
- The snapshot inlines the deployment emergency-stop check into
  `EvaluateEmergencyStop`; the reviewed call sites and decision logic are
  equivalent to PR #36's helper form. This is a mechanical refactor, not a
  demonstrated safety bypass.

No life-ontology, proactivity, outcome-evaluation, or emergency-stop code was
ported. The PR includes explicit bounded-query and monotonic-clock tests; the
review did not run a live PostgreSQL concurrency test in this audit worktree.

### October 12, 2026: system-status and shared-shell frontend crosswalk

Compared the staged frontend snapshot with PR #36 in System Status, the
shared module registry/shell, and service contracts:

- The snapshot's System Status page polls the unauthenticated aggregate
  `/readyz` endpoint every 15 seconds. PR #36 uses the authenticated detailed
  `/api/v1/system/readiness` endpoint, bounded request timeouts, visibility-
  aware polling, and adaptive 120-second healthy / 60-second degraded /
  15-second recovery cadence. The snapshot would increase idle probe traffic
  and remove detailed operator diagnostics.
- The snapshot removes authenticated system build information and the event
  delivery overview/retry API contract. Those are present in PR #36's service
  and model types, including operator-visible outbox recovery; do not replace
  them with a public aggregate readiness response.
- The PR's module registry contains `/operational-brain`, which is absent from
  the snapshot. Its removal would hide a real route already implemented in
  PR #36, not simplify a duplicate page.
- The PR's shared shell updates the document title from the current module and
  keeps its mobile-navigation focus guard. The snapshot removes both. It also
  lacks the registry's URL-segment parser, which avoids treating an encoded
  slash inside a segment as a route boundary.
- The snapshot removes the ambient service's `view=summary` request option and
  weakens the typed response contract for monitor recovery. This would
  encourage full payload loads and lose explicit recovery response typing;
  retain the PR's projections and response model.

No frontend route or service code was ported. The review was static and did not
rerun Angular or browser tests; the PR's exact-head CI remains the frontend
execution evidence.

### October 12, 2026: model constraints and residual test-only deltas

Reviewed the remaining model and small test-file differences in the current
snapshot-to-PR reconciliation:

- The snapshot adds a generic GORM `uniqueIndex` tag to `Workflow.WorkflowID`.
  PR #36 deliberately leaves the versioned migration's explicitly named
  workflow-ID constraint as the sole owner, with a source comment explaining
  that AutoMigrate could otherwise try to rename or drop that named
  constraint. Keep the migration-owned constraint contract.
- The snapshot removes `TestHAIOSOverviewFailsClosedWhenMetricsAreUnavailable`.
  This is a loss of regression coverage for an unavailable metrics provider;
  the production handler itself is unchanged in this file comparison, so no
  runtime failure is claimed.
- Privacy-filter and Evidently tests in the PR construct synthetic secret-like
  fixtures from fragments instead of embedding scanner-triggering literal
  values. The snapshot restores literals; this is test-fixture representation,
  not a production scanner behavior change, and should not be copied in a way
  that causes secret scanners to reject the test source.
- The Promptfoo and proactive differences are fixture-only (pinned synthetic
  runner version and idempotency-key text). No runtime behavior or unique
  feature was found in those files.
- The connected-source OAuth-state entity and raw evidence digest fields are
  already covered by the source and operations reviews above; the snapshot
  omits those fields and their corresponding durable contracts.

No model or test code was ported from this batch. The conclusions distinguish
schema risk and deleted coverage from production behavior changes.

### October 12, 2026: Nginx config-manager event durability

Compared the snapshot's six changed Nginx config-manager files with PR #36's
`nginx-config-manager` implementation:

- The snapshot consumes only Kafka partition 0 with `OffsetNewest` and has no
  consumer group or durable inbox. Events already in Kafka when the service
  starts, and events missed during an outage, are not replayed by this
  consumer. PR #36 starts from the oldest offset, uses a consumer group, and
  records messages in a durable retry/retention inbox before marking them.
- Snapshot event processing logs malformed, unknown, and failed messages but
  does not return a durable retry decision. PR #36's inbox retries failures,
  tracks terminal outcomes, and makes at-least-once delivery idempotent.
- Snapshot config writes use `os.Create` directly, so a process interruption
  can leave a truncated/partial live `.conf`; updates delete the old route
  before writing the new one. PR #36 renders first, writes and syncs a
  same-directory temporary file, then atomically renames it; updates write the
  replacement before removing the old path.
- The snapshot consumer has no cancellation-aware group loop, restart backoff,
  or readiness state. Its main process blocks in an unbounded `Start()` and
  lacks the PR's bounded health server and graceful shutdown. Configuration
  also omits validation for broker addresses, group/inbox settings, retry
  retention, and health port.
- The snapshot event payload omits the event ID/timestamp fields used by the
  durable inbox contract. Its consumer therefore cannot adopt the PR's
  idempotent event identity without restoring that schema.

No Nginx config-manager code was ported. This package comparison is based on
source and tests; no live Kafka broker or gateway reload was exercised. The
existing policy that refuses unapproved Docker-socket reload remains intact.

### October 12, 2026: Compose and environment artifact boundary

Compared the snapshot index with PR #36's Compose, ignore, and frontend
container contracts:

- PR #36's `.gitignore` excludes `.env-backend`, `.env-gateway`, and `.env-idp`;
  these three files are present as new entries in the snapshot index. Their
  contents were not opened. They may contain deployment secrets and must not
  be copied, committed, or removed as part of integration without a separate
  explicit secrets-safe disposition.
- The snapshot's `frontend/nginx-custom.conf` listens on port 80, while the
  canonical gateway and Compose health contract target the frontend on port
  8080. The snapshot version would break the gateway-to-frontend path; retain
  the PR's listener port.
- The snapshot's local Compose file omits PR #36's opt-in internal SearXNG
  research-discovery profile and several backend configuration bindings for
  operational-graph bounds, authenticated MCP preflight, and the operator
  scope. Its omission is not a reason to copy the much larger file wholesale;
  preserve the canonical Compose topology and only port a reviewed matching
  configuration if a missing capability is demonstrated.
- The snapshot also drops `.gitattributes` rules for output and source
  material line endings. This is repository hygiene, not executable behavior.
- The checked compatibility Compose wrappers are otherwise equivalent in the
  reviewed blocks.

No environment files or Compose files were copied or deleted. This pass does
not establish whether any environment file contains an active credential; it
was deliberately not read.

### October 12, 2026: CI, smoke-path, and cleanup-gate crosswalk

Compared the snapshot's CI workflow and supporting smoke/cleanup scripts with
the current PR #36 versions:

- The snapshot CI workflow omits the PR's Windows no-fake-claims audit and
  live Trello acceptance-launcher contract steps. It also invokes the Windows
  installer smoke without the PR's explicit hosted-runner Docker-unavailable
  allowance, which can make the snapshot test fail for an environment
  prerequisite unrelated to the installer contract.
- The PR browser-acceptance workflow writes a separately redacted startup log
  for diagnostics; the snapshot does not retain that artifact-generation
  step. Both versions redact command output and collected owner-container
  logs, so this finding is limited to the startup-log artifact.
- The PR's Postgres CI lane includes source-extraction history bounds and
  authorization-receipt owner-control cases that are absent from the snapshot
  workflow. Its approval-proof test key is unique to each workflow run and
  attempt; the snapshot uses one static test-only value. These are test
  coverage/isolation differences, not evidence that production keys are
  exposed.
- PR #36's Promptfoo dependency audit omits optional packages; the snapshot
  does not. Keep the PR's defined audit scope rather than silently expanding
  or changing dependency installation behavior.
- The snapshot's `smoke-background-operations.sh` asserts that a low-risk
  source-derived operation is auto-executed before owner review. The PR smoke
  instead requires both items to enter the approval queue, binds approval to
  the reviewed revision digest, and only then executes the explicitly
  approved safe item. The snapshot test therefore contradicts the current
  approval-first contract and must not be used as evidence that unreviewed
  source work may run.
- Snapshot `smoke-auth.sh` mints JWTs containing only `sub`, `role`, and `exp`;
  PR #36 includes `iss`, `aud`, `token_type`, and `iat`, matching the IDP's
  token contract. Keep the richer fixture so smoke tests exercise real auth
  validation rather than relying on permissive test-only claims.
- The PR secret-scan runner requires a valid base revision in CI and scopes
  the Gitleaks diff to that revision; the snapshot instead scans the whole
  repository history without a required CI comparison ref and prints at most
  25 redacted finding summaries rather than 100. The broader history scan is
  not inherently weaker, but it does not prove the intended base-to-head CI
  comparison contract; retain the explicit PR base-ref validation.
- The PR transcript remover additionally verifies the supplemental transcript
  roots and requires a `supplemental_source_archive_verified` report before
  it can reach deletion preflight. The snapshot remover omits this second
  source-integrity gate. Related PR cleanup-readiness checks also account for
  the integration snapshot, merge scratch, conflicted state, and ignored
  environment-file names; do not treat snapshot cleanup readiness as
  equivalent or use it to authorize deletion.

No CI, smoke, or cleanup code was ported or run in this static comparison.
The cleanup scripts were not invoked, and all local transcript, snapshot,
merge-scratch, environment, and worktree data remain untouched. PR #35's
separate current-head CI result is not evidence for PR #36 or for live
production acceptance.

### October 12, 2026: documentation and browser-acceptance evidence

Compared the remaining documentation and Playwright acceptance differences;
the two child-report ID sets were also compared directly:

- `docs/child-agent-archive-2026-07-30/child-agent-final-reports.md` contains
  the same 9 report IDs in both trees, and `docs/child-agent-final-reports.md`
  contains the same 104 IDs. The snapshot's report text has old absolute
  Windows links and stale source line coordinates; PR #36 normalizes links to
  repository-relative paths and explicitly labels historical checkpoints.
  There is no additional child report to import based on these manifests.
- The snapshot cleanup-readiness document is substantially older than the PR
  version (720 versus 1,198 lines at this comparison). The PR adds later
  source-snapshot, transcript-integrity, runtime, and cleanup-gate evidence.
  Keep the PR's longer history; do not replace it with the snapshot's earlier
  checkpoint.
- The snapshot root `README.md` retains older Phase 2 feed-import instructions
  and omits the current Autofever non-connection status. The PR documents
  Autofever as unconnected and points to the actual integration prerequisite.
  The snapshot
  `ANALYSIS_REPORT.md` also presents missing `.env-*` files as a fixed startup
  issue; PR #36 marks that report historical and directs operators to the
  canonical Compose and current runbook. Do not restore those stale
  instructions, especially because the snapshot `.env-*` contents were not
  inspected.
- Snapshot completion and verification documents predate current Compose and
  runtime evidence: they describe multi-service boot as unrun/partial, while
  PR #36 records the later local Docker Desktop run and preserves fresh-clone
  release-target acceptance as outstanding. Snapshot dependency notes also
  cite an older Go patch version and frame dated scans as current. Keep the
  PR's date-scoped wording; neither document is universal production
  acceptance.
- PR #36's backend endpoint audit includes authenticated detailed system
  readiness routes that the snapshot table omits. The PR's optional-runtime
  matrix records per-profile inputs, limits, and capability boundaries, while
  the snapshot matrix is shorter and omits several current profiles. The PR
  performance baseline separates in-memory fallback measurements from the
  PostgreSQL path; the snapshot's older Apple-silicon benchmark is not
  comparable production-database evidence. These are documentation/evidence
  improvements, not unique executable features in the snapshot.
- The snapshot Playwright acceptance test proceeds from a selected runtime
  through an owner approval and then executes it, asserting a completed
  result. PR #36's current test intentionally stops at the real fail-closed
  boundary: missing participant assignment, capable model, approval, and
  framework evidence leave the operation blocked with zero attempts. It also
  verifies the persisted sync job and exact synthetic extraction before
  workflow planning. Preserve the PR test as the current safety contract; the
  snapshot's successful execution scenario is not proof that the present
  runtime is ready to execute.
- The snapshot completion matrix has older role-claim and Compose summaries.
  PR #36 records current owner/operator/viewer auth and the single source-built
  topology. Historical `final-verification-report.md` and phase reports remain
  dated records; their completion language must not be reused as a current
  release claim.

No documentation or Playwright files were ported in this pass. This was a
static comparison of committed file content and report-ID sets; no browser
acceptance test or clean-clone release run was repeated locally.

### October 12, 2026: Windows setup, installer-smoke, and recovery safety

Compared the snapshot's Windows initialization, installer-smoke, and backup
contracts against PR #36:

- Both initializers serialize existing-environment maintenance and check for
  existing Docker volumes/projects before creating replacement credentials.
  The PR version adds post-replacement verification of the original ACL and
  exact file bytes, attempts an explicit rollback on a failed verification,
  and preserves recovery files when rollback cannot be proven. The snapshot
  uses an atomic file replacement and temporary backup but lacks that same
  read-back/ACL-verification and verified-rollback sequence. Keep the PR's
  fail-closed recovery path; do not port the older initializer over it.
- Both Windows installer smoke tests refuse local/self-hosted execution and
  check the user's profile paths. The snapshot additionally requires
  `RUNNER_TEMP` to be nested under `RUNNER_WORKSPACE`, which is not a portable
  assumption for hosted Windows runner layouts; PR #36 instead requires a
  distinct existing temp directory and rejects a reparse-point temp root.
  Preserve the PR's runner-specific containment and cleanup ownership checks.
- Both backup implementations verify the local Docker engine and enumerate
  covered/uncovered recovery state. No new supported backup artifact or
  backupable HAI data volume was identified in the snapshot. The reviewed
  snapshot is not evidence that the local machine's actual volumes or backup
  contents are covered; that still requires the guarded host-side preflight.
- The snapshot's Windows recovery contract tests exercise many of the same
  protected-environment, manifest, volume-coverage, and failure cases. Their
  presence does not make the snapshot implementation interchangeable with
  the PR's more defensive read-back/rollback behavior. The CI change already
  recorded above also omits two current Windows audit/acceptance steps.

No installer, environment migration, backup, restore, or recovery drill was
run on this machine. These are source/test-contract findings only; no
environment file, Docker volume, scheduled task, or installed application was
changed.

### October 12, 2026: current-head reconciliation and isolated acceptance

This note supersedes older pass-specific head and changed-path counts above;
those counts remain historical evidence for their stated commits. The latest
comparison is against PR #36 head
`a09248628e6c166067f075638e364269fd6a2b96` and the preserved snapshot's staged
index:

- Snapshot staged paths: 2,917. PR tree paths: 3,147. Identical Git blobs:
  2,510. Differing blobs at shared paths: 400. Snapshot-only paths: 7. PR-only
  paths: 237. These figures reconcile arithmetically and describe file-level
  differences, not missing features or deletable source data.
- The seven snapshot-only paths are the three unopened `.env-*` files, the two
  dedupe-migration files already represented by PR migration `0006`, and two
  legacy nginx files already covered by the gateway template/configuration
  manager review. None is a newly identified feature to port. The env files
  remain secret-bearing/unreviewed and excluded.
- The isolated acceptance-stack script in the snapshot omits the PR's bounded
  gateway readiness retry, service-dependency validation, and runtime-UID
  ownership for private phase-two tmpfs. The PR implementation retains these
  startup and least-privilege safeguards. The compared Python runner smoke,
  isolated recovery rehearsal, and critical-path smoke files are byte-identical
  to the snapshot versions; no change is needed for those paths.
- PR #36 remains open and mergeable at this head. Its exact-head GitHub Actions
  run `38190771606` is queued, with jobs not yet started; this is not a CI pass.
  The GitHub app connector requested reauthentication, so status was verified
  through the authenticated local `gh` CLI instead. PR #35's separate green
  checks do not qualify PR #36.

This is a repository-tree and script-contract reconciliation, not a claim of
live-provider, real-account, local Windows installer, database restore, or
production acceptance. No source snapshot files, local runtime state, or user
data were changed or removed. Cleanup remains blocked on the exact PR merge and
the source-archive/readiness gates documented above.

### October 12, 2026: stale primary-checkout and local-review reconciliation

Rechecked the older primary checkout, the current PR checkout, and PR #36 after
the later ledger updates:

- The primary checkout remains on `codex/hai-runtime-release` at
  `e07b9daeb3ba2630ecbe12a64d948f791203c241`, 313 commits behind its remote
  tracking branch. It has 3,167 status entries: 692 tracked modifications and
  2,475 untracked paths. `output/` contains 1,576 files totaling 126,970,802
  bytes. These are current inventory counts, not permission to delete or proof
  that every item is disposable. Do not stage or remove this checkout wholesale.
- The audit checkout is at PR #36 head
  `7fad8bd45f0ca148c5e2db133a429de2a8475a81`; PR #36 is open and mergeable.
  Its exact-head workflow currently has 36 queued checks and no completed
  result. The independently green PR #35 is a different change and does not
  satisfy PR #36's gates.
- Previously identified weak local variants in execution approval, approval
  source ownership, execution context fallback, public-key byte handling,
  database logging configuration, and the older frontend shell remain excluded:
  the local copies are not a safe replacement for the PR implementation.
  Earlier blob inventory also found 108 untracked files whose names already
  exist in the PR but whose contents differ, plus many generated/output-only
  paths. Their semantic differences are not all reviewed; this remains an open
  source-reconciliation item.

#### Prior review handoffs checked against current PR source

The following outcomes are confirmed by current source/tests, not inferred
from the handoff report's older status:

| Handoff finding | Current PR evidence | Remaining boundary |
| --- | --- | --- |
| Acceptance CI could attach to installation volumes or delete them | CI uses `isolated-acceptance-stack.ps1` Prepare/Start/Stop with a unique temp evidence directory; it validates owned container labels and mounts before collecting logs. The old ordinary local Compose `down -v` cleanup path is not used in this job. | Hosted job has not completed at this head; 36 checks are queued. |
| Acceptance manifest could import external env/secrets/configs | Guard rejects service `env_file`, `secrets`, `configs`, and top-level `secrets`, `configs`, `include`, and `extends`; contract tests include external-source and empty-env-file negatives. | Source/contracts are not proof against every Docker Compose implementation/runtime. |
| Production accepted weak non-empty memory/API secrets | `doctor.go` requires at least 32 characters for these production secrets; focused secret tests exist. | Does not prove installed secrets are random or rotated safely; no current host secret was read. |
| JSON access/refresh credentials bypassed redaction | Structured redactor tests cover JSON, escaped keys/strings, NDJSON, malformed URLs, and sensitive query parameters. | No live credential-bearing provider response was exercised. |
| Atomic mutation trusted caller-supplied previous state or changed creation time | `validateMutationState` checks transition `BeforeStatus` and legal transition against the locked current row; both memory and PostgreSQL paths preserve `CreatedAt`; a forged-before-status regression is present. | Real PostgreSQL locking/concurrency behavior remains dependent on the queued integration checks. |
| Automation approval could be rebound to changed configuration after human review | Durable task review snapshots, configuration digest validation, and changed/missing/old snapshot regressions exist across task and automation packages. | Tests in current PR CI are queued; no live operator workflow was exercised. |
| Local safe worker could hide partial effects or retry after uncertain authorization | Worker progress fields are carried into background outcome classification; non-empty/contradictory progress prevents treating a refusal as safely pre-effect. | First-ledger-write crash durability and cross-process recovery of filesystem identity remain unproven/open. |
| Source-derived action might execute after its source changed | Current source-head generation/config epoch is checked at final effect admission; regression tests cover superseded source observations. | This is source/test evidence, not a live connector race or hosted execution result. |
| Installer release could claim signing without verifying retained uninstaller | Inno production/preview signing branches and exact retained-uninstaller verification are in source; doubles-based contract suites and Windows CI jobs are defined. | A real certificate, signing service, signed artifact, clean-machine install/upgrade/uninstall, and data-retention drill have not been verified here. |
| Durable session tests were not included in hosted CI | Current workflow contains the disposable PostgreSQL/Redis-backed durable-session test step and exact named-pass validation. | The current hosted workflow is still queued; no completed hosted logs are available. |

This pass did not run local tests or builds because the current PR's broad hosted
workflow is already queued and the stale primary checkout is not the candidate
tree. It also did not commit or push. No files were removed. Two scoped
worktree edits remain uncommitted: this inventory/crosswalk update and the
`owned-browser-matrix-handoff.md` correction. The latter fixes a stale note
about task-history receipt restoration but does not claim that the end-to-end
owned-browser matrix has passed.

Cleanup remains unsafe at this point: PR #36 has not merged, the primary
checkout's 108 same-name content variants and 202 previously identified
non-matching tracked variants have not all received semantic review, and the
20.7 GB transcript archive/source packages have not completed item-level
integration. Generated logs may eventually be removable after the evidence they
uniquely carry is summarized and checked against hosted/committed records; no
such deletion is authorized by this audit note alone.

### October 12, 2026: supplemental completed-report paths at current PR head

Re-extracted the repository-relative source links from the three completed
supplemental child reports and checked them against PR #36's exact current
tree, `69bbcbc9a621112bc82e9e1d476fda6c8840a3a7`. All 21 distinct report-named
paths are present and tracked; none is missing. This closes the source-presence
check for Hubble, Popper, and Anscombe's report-listed files only. It does not
reconstruct every historical patch, establish that unrelated archive events
were integrated, or turn the archived focused test results into current-head
CI evidence.

The three transcript files remain local cleanup candidates only: their
recorded source hashes were verified by the supplemental readiness check, but
the remover requires the verified ledger on merged canonical `main`. PR #36
is still open. Exact-head run `38192192044` is for this commit; at that earlier
check, Windows installer/signing, cleanup-readiness safety, and guarded native
Windows regression jobs were running while the run remained nonterminal. No
transcript or other local data was removed.

### October 12, 2026: focused stale-checkout source comparison

Compared the current dirty primary checkout's versions of six operational
files against PR #36 at the exact head above, normalizing line endings before
review. The current PR implementation is ahead of that checkout in each
reviewed area:

- `backend/internal/operations/service.go` fingerprints raw evidence with
  SHA-256 before refreshing source evidence.
- `backend/internal/workflow/service.go` bounds and validates success criteria,
  persists them, passes them into task execution, and includes them in the
  source revision identity.
- `backend/internal/source/trello_live_test.go` uses the context-aware live
  Trello request helper.
- Previously reviewed `backend/internal/task/service.go` contains the
  read-only ChatGPT logs MCP loop and trace retention, retry/review safeguards,
  and durable review snapshot checks.
- Previously reviewed `backend/internal/source/service.go` contains
  owner-scoped source lookup, an explicit disabled state for the unimplemented
  Autofever connector, opaque LARO cursor handling, cancellation-aware search,
  and owner-filtered extraction pages.
- Previously reviewed `backend/internal/automation/automation_service.go`
  persists mutation and outbox events transactionally.

These file-level findings do not establish that all local changes were ported.
The stale checkout has 692 modified tracked paths; its working diff reports
142,921 insertions and 17,804 deletions across those paths, and `output/`
contains a large generated artifact tree. Earlier normalized triage counted
195 differing shared text paths and two legacy nginx paths absent from the
current PR; those paths, plus all non-text and untracked material, still need
classification and semantic disposition. The two missing nginx files are
legacy static configurations; they were not copied because the PR uses the
governed nginx template/config-manager path and the legacy files include a
placeholder key.

Exact-head CI run `38192192044` remains nonterminal. Its Windows installer
preview/signing guards, guarded native Windows regressions, cleanup-readiness
contracts, and Windows CI/smoke-path contract have passed. Backend, frontend,
Postgres migration, browser acceptance, isolation, provider fixture, secret
scan, nginx manager, gateway/Compose, authenticated control-plane, and
framework-runner jobs remain queued. No full-suite or live-provider claim is
made from this partial run. No local files were removed.

### October 12, 2026: additional high-impact local source variants

Inspected additional differing files directly against the PR tree. These
examples confirm that the stale checkout's differences are not automatically
missing work to port:

- `backend/internal/braincatalog/catalog.go` in the PR includes the bounded,
  read-only ChatGPT/Codex history MCP integration and constrains optional A2A
  public transport to an explicit fixed-ngrok origin. The local version lacks
  the MCP entry and has the earlier, broader A2A endpoint wording.
- `backend/internal/lifeontology/repository_postgres.go` in the PR adds
  owner-scoped, bounded entity/relation queries and bounded merge-proposal
  history, with explicit domain/type/status validation and temporal filters.
- `backend/internal/ambient/service.go` in the PR adds a summary projection,
  rechecks scan dependencies, extracts candidate persistence behind a
  checkpoint-aware helper, and treats due-work failures as scan failures.
- `backend/internal/opscontrol/handler.go` and `service.go` in the PR require
  durable review plus exact typed confirmation for emergency-stop resume;
  the older local handler exposes a direct disengage request. The PR also
  distinguishes a no-op control transition from a successful state change.
- `backend/internal/phase2/module.go` in the PR separates the source-derived
  operation approval check from the final safe-effect policy, so a generic
  safe-action decision cannot authorize an unapproved source operation.
- `backend/internal/source/durable_scheduler.go` in the PR completes queued
  sync intent when an operator pause/revocation makes it obsolete instead of
  spending provider retries on that policy state.
- `backend/internal/task/state_repository_postgres.go` in the PR adds bounded,
  immutable completion-history projection and context-aware plan lookup.
- `backend/internal/identity/jwt.go` in the PR carries the operator-scope
  claim/context key; this comparison establishes the claim surface only, not
  end-to-end operator-scope enforcement.

These are targeted content comparisons, not an exhaustive review of every
remaining differing path. The PR includes these changes already; no code was
ported in this pass. No local runtime data or source archives were changed.

### October 12, 2026: untracked source-like file crosswalk

Compared the stale checkout's untracked, source-like text files with PR #36
after normalizing line endings. The comparison excluded `output/`, logs,
`.env*`, locks, exit markers, JSONL transcripts, dumps, archives, and binaries.
Of 871 eligible paths, 795 match the PR file content exactly, 66 share a path
but differ in content, and 10 are absent from the PR tree. The absent paths
are four `.claude-flow`/`.swarm` local-state files and six dated PostgreSQL
test-evidence JSON files; no product source path is absent in this scoped
comparison. These local-state and generated-evidence files remain preserved.

Reviewed divergent examples across operation approval/claiming, source sync,
task execution, database logging, OpenClaw maintenance, isolated acceptance,
Windows installer smoke, Postgres claim integration, IDP session tests, and
the control-room test harness. PR-side versions add or retain exact-revision
approval binding, context propagation, fail-closed policy and source-change
handling, redacted secret-scan output with a required CI base SHA, isolated
synthetic browser authentication, and owned temporary-path checks. The local
alternatives inspected were older or less restrictive; they were not copied.

One divergent local `scripts/audit-npm-production-dependencies.sh` adds an
`omit-optional` mode. It was not adopted: current PR behavior audits optional
production dependencies by default, while the alternate mode could suppress
their advisories. The default audit remains fail-closed on missing or
untrustworthy reports.

This is a bounded path/content crosswalk, not semantic review of all 66
divergent files. The remaining differences, all excluded transcript/log/test
artifacts, and the full historical patch-to-source mapping remain open. No
local file was removed or moved.

### October 12, 2026: expanded divergent-file review

Continued the same-path review using normalized line endings and inspected the
remaining high-signal Go, IDP, frontend, migration, and CI-support differences.
The observed PR-side changes are current API/context migrations, stronger
source-evidence revision binding and execution-claim checks, session-auth test
updates for context-aware IDP methods, isolated run-scoped browser auth state,
and migration rollback tests that use disposable databases instead of
unwinding later-phase schema/data. Other reviewed deltas are fixture fixes for
credential-like scanner fixtures, Windows runner compatibility/toolchain
version updates, environment preservation checks, and the installer migration
payload-selection correction already noted above. These are all present in
the PR tree; no stale-checkout code was copied.

This review still does not certify every assertion in each changed test or
every dependency transitive in the regenerated Promptfoo lockfile. The 66-file
set therefore remains an audit set, not a reason to delete the stale checkout.
The six generated PostgreSQL evidence files, four local agent-state files,
excluded outputs/logs/transcripts, and full historical patch-to-source
attribution remain preserved and unresolved. No local file was removed or
moved.

### October 12, 2026: transcript audit reader hardening

The transcript audit utility's tail reader previously made one `FileStream.Read`
call and assumed it returned all requested bytes. Since a stream read may return
fewer bytes before the requested count, the utility now loops until the full
tail is read and fails explicitly on premature EOF. Added a synthetic Windows
regression test to the cleanup-readiness CI job. The test verified one completed
transcript, manifest/report/summary generation, and `deletion_performed=false`,
and confirmed the source transcript still existed after the audit. The test
passed locally. It used a unique temporary directory, which was removed after
the containment check; no real transcript, report, or other archive artifact
was read or changed by this test.

### October 12, 2026: duplicate-ID transcript hash check

Independently hashed both files in each of the two duplicate-child-ID groups.
Although each pair has identical logical byte length, neither pair is
byte-identical:

| Child ID | Transcript suffix | Bytes | SHA-256 |
| --- | --- | ---: | --- |
| `019fb20b-b4b1-7250-9d7c-9d1e591ab852` | `.duplicate-5d868d4ecc874ff3a34f508517ace927.jsonl` | 1,603,851,727 | `a7b0bb14fbffa7571f436b023ed5316c9ab07fed6142874eb1fc6e4d53228ce9` |
| `019fb20b-b4b1-7250-9d7c-9d1e591ab852` | `.jsonl` | 1,603,851,727 | `84e07b24a3acaed044a1f52db04de1cbc69c635f4d111d6f86fbb243cdcbc89c` |
| `019fb277-0ddf-7500-9088-52cb4655be1a` | `.duplicate-5c6ea92b1f1c491ea0af46a1937e6b08.jsonl` | 1,603,399,243 | `961fb8ff7619f95ef70a7b32c1737605fc61d14d0b3ffe79f5e410a5b6e94500` |
| `019fb277-0ddf-7500-9088-52cb4655be1a` | `.jsonl` | 1,603,399,243 | `85da9f27d10a60112b327f9c4e478ecdf78dcd6db5ac8a0b8c96e24415bc3a65` |

The `.duplicate-*` records have terminal markers that differ from their
same-ID counterparts (completed vs nonterminal for Meitner; aborted vs
nonterminal for Volta). Their different hashes and terminal classifications
mean neither file can be discarded as a redundant copy. All four remain
preserved; no archive data was modified.

### October 12, 2026: full source-archive integrity gate

Ran `scripts/test-hai-transcript-cleanup-readiness.ps1` in read-only mode with
`D:\codex-temp\hai-completed-agent-sessions` as the source archive. It verified
all 18 manifest rows against all 18 source files, totaling 20,739,169,122
logical bytes. All eight candidate source hashes matched the manifest
(7,939,888,699 candidate bytes); the ten retained files were present with
matching sizes. The gate reported `source_archive_verified=true`,
`cleanup_gate_ready=false`, `deletion_performed=false`, and
`cleanup_authorized=false`. The gate is correct to remain closed: the
integration PR is still unmerged, the committed-ledger-on-main check was not
requested/passed, and semantic patch-event-to-source attribution remains
incomplete. No archive file was changed or deleted.

### October 12, 2026: supplemental source-archive integrity gate

Ran `scripts/test-hai-supplemental-transcript-cleanup-readiness.ps1` against
the two inventoried supplemental roots. It verified all nine manifest paths
and 14,013,737,298 logical bytes, hashed all three completed candidates
(4,651,926,405 bytes), confirmed all six retained transcripts remain present,
and verified all 21 report-linked repository paths. The result was
`supplemental_source_archive_verified`; `deletion_performed=false` and
`cleanup_authorized=false`. The supplemental completed reports have a
source-path crosswalk, but full historical patch-event attribution remains
incomplete and these files remain preserved.

### October 12, 2026: remaining candidate-delta disposition

Reviewed the three remaining local-only candidates that appeared potentially
additive. `backend/internal/runtimelab/owner_gate_handler_test.go` differs from
the PR copy only by a clarifying comment; the authorization assertions already
exist in the PR. `scripts/test-isolated-acceptance-stack.ps1` is superseded by
the PR copy, which retains the external-configuration denial checks and adds
gateway retry, credential-inheritance, and interrupted-cleanup provenance
coverage. The local `scripts/test-frontend-build-resource-limit.cjs` adds
observations about invalid values accepted by the installed Angular parser,
but the PR already enforces the supported worker values at the Docker build
boundary; those upstream-parser expectations would be brittle across Angular
updates and do not establish a missing product guarantee.

Compared `services/promptfoo-runner/package.json` in the stale checkout with
the PR tree. The PR pins Promptfoo `0.124.1`, includes the required
`@libsql/linux-x64-gnu` package, and applies a `basic-ftp` override; the stale
copy pins `0.123.1` and omits both. No stale dependency manifest or lockfile
was copied. The PR's Promptfoo runner image CI check passed on the audited
head. This resolves only these candidate deltas, not a complete semantic
review of the 66-file divergence set, the 1,576 output artifacts, or historical
patch-event attribution. No local files were moved or deleted.

### October 12, 2026: current bounded transcript inventory and PR checkpoint

This checkpoint supersedes older cleanup-candidate counts in the dated entries
above. A full-file bounded rescan now attributes only three main-archive
transcripts, totaling 1,644,177 bytes, as cleanup candidates after ledger
commit; the other 15 main-archive files (20,737,524,945 bytes) remain retained.
Thirteen of those retained files contain oversized records (1,630 skipped),
so any earlier observed completion is not sufficient to classify the complete
file. The 18-file source archive was reverified against its manifest and the
three candidate SHA-256 values.

The two supplemental roots contain nine files (14,013,737,298 bytes). Their
current bounded manifest has zero candidates and nine retained/unattributed
files; each contains oversized records (783 skipped total). The supplemental
readiness check verified the exact nine-file inventory and all 19 repository
source links in the preserved reports. Combined known transcript inventory is
27 files / 34,752,906,420 bytes: 3 candidates and 24 retained. This remains a
partial local-device inventory, not a claim that every HAI-related artifact
has been processed.

The audit updates were committed as `6f039bf3` and `3f1c00de` on the existing
`codex/hai-runtime-release` PR branch. Immediately before this ledger update,
PR #36 was open against `main` at `3f1c00de2a433b18637c7230fbe41d0a184cadd5`;
its exact-head GitHub Actions run `38201605903` completed successfully with
all 36 jobs passing. The branch is part of `Robert-Velhorst/018-HAI`, not a
separate repository. No transcript or local worktree was deleted, moved,
truncated, compressed, or archived.

Transcript cleanup is still not authorized: PR #36 is unmerged, the canonical
`main` ledger gate has not passed, and full semantic integration of oversized
transcript content remains incomplete. The three candidates are not removed
until the merged-main gate is independently satisfied. All 24 retained files
remain protected.

### October 12, 2026: 10 MiB rescan and integration update

The bounded record cap was raised from 8 MiB to 10 MiB after a byte scan of a
1.53 GB retained transcript found its largest record was 9,293,992 characters.
The synthetic regression now uses an 11 MiB record and continues to verify that
oversized content forces retention. `test-child-agent-transcript-audit.ps1`
passed after this change.

The full main archive scan covered all 18 files / 20,739,169,122 bytes. It
identified five uniquely attributed, completed, hashed candidates totaling
3,179,439,595 bytes; 13 files totaling 17,559,729,527 bytes remain retained.
Two records in two files are still over the cap. The supplemental scan covered
all 9 files / 14,013,737,298 bytes and identified two candidates totaling
3,113,294,358 bytes; seven files totaling 10,900,442,940 bytes remain
retained, including five over-limit records. Combined: 27 files /
34,752,906,420 bytes, seven candidates / 6,292,733,953 bytes, and 20 retained
files / 28,460,172,467 bytes. All candidate transcript hashes were computed;
both manifests state `deletion_performed: false`.

The two newly attributed main-archive reports name 16 source paths, all present
in the PR tree. The two supplemental reports name 19 source paths, also present
in the PR tree. The exact current PR head before these changes,
`5d2e24ddf4431f893de290fdcb368cd7a5fb0dd7`, passed all 36 Actions jobs in run
`38202234619`; the pending ledger/parser update requires its own new-head CI
run. PR #36 remains open and unmerged. No transcript, archive, or worktree was
deleted, moved, truncated, compressed, or archived. The 20 retained files and
all other unreviewed local HAI worktrees remain protected.

### October 12, 2026: scratch-worktree integration recheck

Rechecked the local OpenClaw-artifact and runtime-safety worktrees against the
current PR tree. All 255 untracked OpenClaw-worktree paths and all 25 untracked
runtime-safety-worktree paths already exist in the PR tree. The OpenClaw
worktree has 143 modified tracked paths, 107 of which differ from the current
PR copy; the runtime-safety worktree has 13 modified tracked paths, 12 of
which differ. These counts identify a remaining semantic-review set and are
not evidence that every divergent hunk is integrated. Targeted review of the
OpenClaw artifact URL/download implementation confirmed bounded content,
receipt and owner binding, approved HTTPS origins, DNS/IP validation,
redirect/proxy denial, and emergency-stop rechecks in the PR. The runtime
scratch contains older alternatives for these same surfaces; no code was
copied from either scratch tree.

Compared the model-maintenance scratch's `maintenance.go` with the PR copy.
The scratch removes provider catalog probes and the Odysseus health-only
status, narrows accepted API-root paths, and differs in the handling of
unavailable durable history. Those alternatives would reduce observability
or weaken current fail-closed/version-check behavior, so they were not
adopted. This is a targeted file review, not full semantic review of the
model-maintenance worktree.

The exact PR head `f890e3c8a0abd0d7299e86d218df526c267f6a6f` completed CI run
`38206115512`: 35 of 36 jobs passed. The sole failure was the `gosec-runner`
container build receiving HTTP 500 from `sum.golang.org` while verifying
`google.golang.org/api@v0.288.0`; its compile and isolated contract-test step
passed. Backend, IDP, frontend, browser acceptance, migration integration,
Windows, and cleanup-readiness jobs passed. The PR remains open and mergeable
but its checks are unstable until the transient dependency-service failure is
resolved. No rerun or merge was performed. The local environment has no `go`
executable, so the focused local agent-runtime/host-runtime test attempt could
not run; CI remains the available package-test evidence.

No source archive or scratch-worktree file was modified or deleted. The full
divergent-file review and historical patch-event attribution remain open, so
the cleanup gate remains closed.

### October 12, 2026: source-review report crosswalk

Compared the local-only account-bridge, account-feed, and safe-outcome review
reports with the current PR source rather than treating the reports' parent
amendments as proof. The account-bridge implementation now distinguishes
unknown feed totals from a known empty registry, blocks per-feed and bulk sync
during refresh, retains data after failed refresh, includes visible labels in
accessible names, and renders attempt/success timestamps separately in UTC.
Its component tests include failed initial load, refresh guards, label checks,
timestamp DOM bindings, failed-refresh retention, and recovery paths. These
source and tests are in PR #36.

The account-feed envelope parser rejects absent or null item arrays while
preserving a valid explicit empty array. Sync admission uses a durable
per-feed token; PostgreSQL row locking and the token-conditioned completion
write make competing sync claims fail closed across repository instances.
For operation evidence, observations receive durable monotonically increasing
generations. Intake locks source authority before writes, rejects an
observation older than the accepted source head, and publishes the head only
after the operation write. Deterministic source-head tests cover an older
blocked read completing after a newer one and an older duplicate-evidence
refresh attempting to overwrite newer evidence. These mechanisms and tests
are present in PR #36; the latest exact-head CI run also passed the real
Postgres migration-integration job.

The remaining account-feed lifecycle point is a product policy, not a verified
code defect: deduplication is intentionally limited to active operations, so a
later full sync can re-intake an item after its operation was archived or
dismissed. No durable feed-revision receipt currently changes that contract.
The report itself treats this as conditional on whether dismissal should
survive an automatic re-intake; do not broaden uniqueness or silently change
the behavior without an explicit product decision. Also, a successful local
ingestion timestamp is not proof of live-provider freshness or pagination
completeness.

The safe-outcome review's frontend safeguards are present in the PR: uncertain
execution receipts remain latched across stale or failed reads, duplicate
execution is withheld until reconciliation, completion labels require
consistent operation/verification evidence, and unresolved work is surfaced
in Basic view. Runtime Lab applies the corresponding self-test fence and
distinguishes read-only discovery from verified execution. Current PR CI
passed frontend tests/build and browser acceptance; this is CI evidence, not
live-provider, deployed-service, screen-reader, or cross-restart acceptance.

At the time of this crosswalk, PR #36 was at
`8c49f348496c664efbce201f37b57c526a953bff` and exact-head Actions run
`38206730870` passed all 36 jobs. The PR was open and mergeable, not merged.
Local Go is unavailable in this environment, so no independent native rerun is
claimed. This crosswalk closes these report-level source comparisons only; the
remaining divergent-file set, diagnostic/output artifact attribution, and
historical patch-event attribution remain open. No source archive, diagnostic,
or worktree file was modified or deleted.

### October 12, 2026: Trello and operation-audit crosswalk

Compared `output/trello-production-sync-20261001.md` with the current PR source.
All seven named regression cases are present in
`backend/internal/source/trello_production_sync_test.go`: long-running action
watermarks, configured short-link webhook refresh, webhook-first comment
deletion, canonical card and action-card identities, retry-cap inventory
checkpointing, and list-record aggregate caps. The corresponding durable sync,
webhook, and canonicalization paths are present in the PR source. PR CI passed
the backend suite, but that is synthetic/CI coverage, not live Trello account,
webhook registration, provider pagination, or permission acceptance. The local
handoff's PostgreSQL helper concern is addressed in current PR source:
`openTrelloRepositoryPostgresTestDB` calls
`pgtestguard.RequireDedicatedPostgresTestDSN` before opening its fixed
`hai_migration_runner_test` database, with dedicated rejection tests for
lookalike names, unrelated databases, sockets, and remote fallback hosts. No
operator DSN was supplied or used here, and this does not claim that the
Trello-specific PostgreSQL integration suite ran in CI.

Compared `output/operation-audit-atomicity-20261001.md` with the current PR
source. `UpdateWithEvent` is wired into unclaimed `Transition`/`Save`, while
`CreateWithEvent` and contextual atomic write methods cover ingestion and
source-observation flows. The PR contains the focused mutation transaction
regressions plus the later `ingest_atomicity_regression_test.go` scenarios for
atomic creation, evidence refresh, and audit-event failure. The implementation
retains claim-only authorization boundaries instead of falling back to
separate operation/event writes. Exact-head PR CI passed the backend tests.
Audit guarantees still depend on the selected repository implementation; the
source-only report's unexecuted SQL-driver tests were not independently run
locally because Go is unavailable in this environment.

These two report comparisons are now recorded; they do not close the broader
untracked-path inventory or prove every local transcript/patch event was
integrated. No local report, source tree, test output, archive, or worktree was
removed.

### October 12, 2026: installer and untracked-path inventory crosswalk

Compared the installer and Windows-lifecycle audit claims with the current PR
source. The PE parser enforces a complete PE32+ fixed header and bounds declared
data directories; its Windows PowerShell regression covers malformed and
positive payload shapes. Production installer builds now require an explicit
SignTool path, pinned certificate thumbprint, and HTTPS RFC 3161 timestamp
endpoint; Setup and the signed uninstaller are Authenticode-verified and the
signing path has focused synthetic tests. This closes the earlier source-level
finding that the build path had no enforced signing mode. It does not supply or
validate an actual signing identity, prove a signed release artifact exists, or
establish trust on a clean Windows machine. The retained release/signing and
rollback acceptance gaps remain.

The current Windows runtime source also contains strict Compose project-name
parsing, exact-project ownership enumeration, a second ownership check after
initialization, safe restoration of inherited settings in Stop, and a
directory/reparse-point refusal before worker replacement. Focused source and
mock contracts are present. These checks do not prove an actual Docker
installation, upgrade, stop, or uninstall lifecycle.

Recomputed the primary checkout's untracked-path inventory against the exact
PR tree with Git blob IDs. Of 2,475 untracked paths, 1,586 were excluded from
code/documentation comparison as `output/`, nested `test-evidence/`, browser
diagnostics, agent state, or generated/build/test artifacts. Of the remaining
889 candidate paths, 821 are byte-identical to a PR blob, 68 have the same path
but different contents, and zero candidate paths are absent from the PR. This
reconciles the earlier inconsistent mismatch count. The 68 mismatches are
listed in the audit output for the October 12 inventory; several inspected
examples are older in the primary checkout than the PR (including the
OpenClaw-maintenance signing-key representation and promptfoo runner lockfile),
but this is not a semantic review of all 68. Keep the set open until each
difference has an explicit disposition; recompute it with the same Git-blob
comparison if the primary checkout or PR head changes.

The 1,586 excluded diagnostics/evidence paths are not source candidates merely
because they are untracked; they remain preserved and are not cleanup-approved
by this comparison. No local archive, output, diagnostic, source file, or
worktree was removed.

### October 12, 2026: targeted review of divergent source candidates

Recomputed the inventory against PR #36 at `2ab9beede7650f71ed35acdbaa59fb36b76e186f`.
The source/documentation filter yields 889 candidate paths: all 889 exist in
the PR tree, 821 are byte-identical, and 68 differ at the same path. Four
additional untracked paths under `.claude-flow/` and `.swarm/` are agent state,
not source candidates. This corrects the broader filter's apparent four-path
absence without changing the earlier 889-candidate disposition.

Compared local and PR versions of the main divergent production files and
acceptance helpers. The PR versions retain additional database acquisition
timeouts and safe connection setup, complete operation-approval revision and
receipt binding, stronger claim-state fencing, and authenticated browser-matrix
session refresh through an isolated run-owned state file. The stale checkout's
variants do not provide a demonstrated behavior improvement over those PR
versions. The local OpenClaw public-key byte encoding is not needed to preserve
secret-scan behavior: the current PR's exact-head secret scan passed, and the
key is public verification material. The local resource-limit parser assertions
test the helper's numeric parsing rather than the production Docker build
boundary; the PR's boundary checks are the relevant contract. Local installer
smoke assertions were reviewed against the expanded PR installer safety tests;
no missing source behavior was established.

Across the divergent test-like paths, a name-level comparison found no local
test functions absent from the PR. This is only a test-inventory signal, not
proof that every assertion is semantically equivalent. The remaining 68 files
still require full per-file semantic disposition, particularly the large
Playwright acceptance-helper and dependency-lockfile differences; no local
content was copied on the strength of path presence or test-name overlap.

The PR remains open and mergeable. Exact-head Actions run `38207676729` tests
`2ab9beede7650f71ed35acdbaa59fb36b76e186f`; at the time of this check it had
34 successful jobs and two running, with no failures; final CI status is not
yet established. No local file or archive was deleted or modified by this
review.

### October 12, 2026: tracked-tree comparison and compatibility-route integration

A fresh comparison of the primary checkout's 682 modified tracked paths against
the current PR tree, ignoring end-of-line-only changes, found 202 content-
divergent paths. This supersedes the older 75-path estimate in prior ledger
entries. The comparison did not modify the primary checkout. The PR remains
ahead on net across the inspected files; review is continuing by behavior,
not by line count.

Targeted diffs in ambient scanning, operation approval/claims, source reads,
workflow execution, phase-two policy handling, IDP state handling, and
dashboard data loading showed that many stale-checkout variants remove PR-side
guards, bounded retrieval, explicit success criteria, or preflight checks. The
locally changed acceptance test that proceeds from approval to a completed
workflow is not evidence that those removed execution prerequisites are
unnecessary; it was not imported. A name-level test comparison is not treated
as assertion-level equivalence.

One local-only route was a concrete gap: the opt-in `compatibility` Compose
profile defines `generic-auto`, and the gateway mounts `nginx-config/sites-enabled`,
but the PR had no static `/generic-auto/` route. Added that route to the PR tree
with the existing internal auth subrequest, access/refresh cookie propagation,
and service-name upstream. Extended the gateway contract test to verify the
route's authentication and its opt-in Compose profile. `python
nginx-config/test_gateway_contract.py` passed all 19 tests.

Did not import the local standalone `nginx-config/nginx.conf`: the active
Compose path renders `nginx.conf.template` with the protected backend and
preview secrets under `umask 077`, while the standalone file embeds a literal
placeholder backend key and is not mounted as the gateway's main config. The
remaining tracked content differences and all 68 divergent untracked candidate
files remain open for semantic review; no primary-checkout file or archive was
removed.

### October 12, 2026: verification, pursuit, and worker-boundary review

Reviewed the local-vs-PR variants of `backend/internal/verification/service.go`,
`backend/internal/pursuit/service.go`, `backend/internal/ambient/service.go`,
`backend/internal/task/service.go`, `backend/internal/automation/automation_service.go`,
`backend/internal/identity/jwt.go`, and `backend/internal/safety/emergency_stop.go`.
The review was limited to behavioral deltas in those files; it does not close
the remaining 202-path source audit or 68 same-path untracked-content review.

Did not copy the local verification-service rewrite. It removes context-aware
cancellation from source retrieval and semantic projection, and lowers durable
memory promotion thresholds. Its sentence-extractive promotion and finite-value
checks are useful, but the PR already requires matching accepted evidence for
promotion and uses stricter confidence and evidence-quality thresholds. The
PR's cancellation and completion-audit behavior is retained.

Rejected local pursuit changes that widen access to ownerless records for any
authenticated user; the PR limits legacy ownerless access through the explicit
`CanReadLegacyOwnerlessData` policy. Also rejected the local workflow/runtime
completion changes: they weaken status and execution-result validation, while
the PR checks approval, recovery/error state, launch type, and exit outcome.
The local exact-runtime-evidence lookup is a candidate for a separate focused
review, but was not ported because its interaction with owner filtering and the
repository's batched lookup contract has not yet been established.

The local ambient variant removes candidate merging and refresh behavior and
has a simplified opportunity-visibility path; the PR's merge and owner/pursuit
visibility checks were retained. The local task variant removes the model-
directed ChatGPT-logs tool loop and verified-framework-agent gate, as well as
context propagation and several review/approval paths. These are not safe
replacements for the PR implementation. The local emergency-stop variant
removes immutable-stop evaluation; it was rejected. Automation API type changes
and JWT field removals were not independently demonstrated to improve runtime
behavior and were not copied.

This comparison produced no additional code changes. Existing PR content remains
the safer implementation for the reviewed behaviors. No primary-checkout file,
untracked artifact, archive, or worktree was modified or deleted.

### October 12, 2026: source pagination, event durability, and test-artifact review

Additional targeted diffs confirm that the local `backend/internal/source/handler.go`
replaces the PR's bounded extraction-page service with a full owner extraction
read followed by in-memory filtering and truncation. The PR's bounded service
contract remains; the local path can load substantially more data than the
requested API limit and returns a total derived only after loading the full set.

The local `backend/internal/automation/automation_service.go` removes the
repository interfaces that persist an automation mutation and its event
together (`CreateWithEvent`, `UpdateWithEvent`, and `DeleteWithEvent`). It
performs the mutation first and publishes afterward, so publication failure
can leave durable state without its corresponding event. The PR's durable
mutation/event path is retained.

The local verification checkout deletes the PR's focused contradiction-
relevance, evidence-freshness, and memory-promotion regression tests. Those
checks remain present in the PR; the local deletion is not an integration
candidate. A targeted read of `docs/child-agent-final-reports.md` also shows
local edits replacing repository-relative links with machine-specific absolute
paths and removing its historical-record caveat. The PR's portable references
are retained. Since the large report diff was not reviewed line by line, its
remaining content deltas remain open rather than being presumed equivalent.

No further source changes were justified by this batch. The main checkout and
all local source, transcript, diagnostic, and build artifacts remain untouched.

### October 12, 2026: Kafka entrypoint comparison

Reviewed the larger local-only delta in `kafka/docker-compose.yaml`. The local
file restores the retired three-broker ZooKeeper stack, uses mutable `latest`
images, publishes broker/control-center ports, and assumes an externally
created `service-hub` network. The PR deliberately makes this path a
compatibility include of the canonical local Compose stack, avoiding a second
Kafka deployment and unpinned images. The local multi-container definition was
not copied; this matches the user's prior request to avoid duplicate HAI
instances and reduces unnecessary local resource use.

### October 12, 2026: runtime configuration and CI contract review

Reviewed the local deltas in `.env.example`, `.github/workflows/ci.yml`,
`docker-compose.local.yml`, and the frontend package manifests. The local
configuration removes the explicit legacy-data owner setting, the private/
link-local source-network guard, the opt-in ChatGPT-logs MCP settings, isolated
A2A/runtime subnet controls, and bounded operational-graph settings that are
present in the PR. The local Compose diff also removes the SearXNG isolation
networks/cache and Nginx-manager inbox/health settings. These are not safe
resource-only simplifications; they remove isolation, policy, or integrations.

The local CI workflow drops the cleanup-readiness safety-contract job and its
related contract checks, and downgrades the configured Go toolchain. The local
frontend test command drops the framework-registry history-state test and
removes the declared `less` dependency. The PR-side guards and test coverage
remain; none of these local removals was copied. This is a targeted review of
the shown configuration hunks, not a complete deployment/runtime acceptance.

### October 12, 2026: transcript cleanup evidence boundary

Reviewed the local delta in `scripts/audit-child-agent-transcripts.ps1` against
the PR version because this script informs whether local transcript material
may be removed. The local version scans only a configurable tail window rather
than attributing lifecycle/patch records across the full transcript; it drops
turn attribution, duplicate-child detection, changed-during-scan detection,
malformed/oversized-record retention, optional source hashing, and cleanup
candidate byte totals. It can therefore classify a truncated or concurrently
changing file without the PR's evidence needed to justify a cleanup candidate.
The PR's stronger full-file, duplicate-aware, hash-capable audit remains in
place. No transcript was classified or deleted based on the local variant, and
no cleanup readiness is inferred from a report alone.

### October 12, 2026: isolated acceptance, installer, and runner contracts

Reviewed local-only diffs in `scripts/isolated-acceptance-stack.ps1` and
`scripts/isolated-recovery-rehearsal.ps1`. The acceptance variant removes
gateway readiness retries, service-dependency validation, cleanup-manifest
hash/size evidence, generic credential scrubbing for synthetic environment
values, and the explicit runtime UID/GID on private tmpfs state. The recovery
rehearsal variant drops `-LibraryOnly` when dot-sourcing the backup helper;
that helper contains executable backup/recovery logic, so sourcing it without
the library-only guard is not a safe simplification. These local versions were
not copied.

The local Windows installer smoke test removes the hosted-runner opt-in for
unavailable Docker, distinct/existing/non-reparse-point runner-temp checks,
isolated `LOCALAPPDATA`, and the installed-payload read/write probe (replacing
the latter with an ACL inspection). The local payload selector
excludes all SQL files, including versioned schema migrations that the PR
intentionally permits, and its source-selection fixtures omit migration and
customer-export cases. The local runtime lifecycle test also stops verifying
that an operator-selected network range survives environment replacement.
The PR-side ownership, migration, and preservation contracts remain in place.

The local secret-scan runner drops the required CI base revision and history
range, and suppresses all finding metadata instead of emitting the PR's
bounded, sanitized summary. It also switches away from the PR's pinned
`github.com/gitleaks/gitleaks/v8` module path. Local tests delete coverage for
invalid/missing base revisions, bounded finding summaries, and malformed
reports. The PR's fail-closed history scan and redaction behavior were kept.
Finally, the local Python runner smoke contract removes the Grype runner's
synthetic configuration and isolated read-only mounts, and deletes its smoke
test. None of these local changes was copied.

This batch reviewed the shown diffs only; it did not run Windows installer,
Docker acceptance, or third-party runner workloads. The source checkout and
all local artifacts remain untouched.

### October 12, 2026: browser matrix, task-history evidence, and runner locks

Compared the local `owned-browser-matrix.ts` helper with the PR. The local
variant places the synthetic operator password in the fixture target and logs
in through the browser UI, but removes the PR's run-scoped storage-state path
validation and authenticated-session refresh/rotated-cookie persistence. It
also counts canceled requests as failures and adds a global pending-request
poll, which can conflate expected route cancellation or long-lived polling with
an acceptance failure. The direct-login path may be useful for a separately
scoped login acceptance test, but copying this helper wholesale would weaken
the serial matrix's session lifecycle and failure classification. No change
was ported from it.

The local task-history handoff note asserts that a cold selected receipt is not
retained as a non-runnable restriction. The PR source calls
`rememberExecutionSafety` when loading a saved plan and reconstructs
restrictions from persisted API history; its `task-blueprint.accessibility.spec.ts`
also asserts the cold receipt remains visible in Basic after refresh, sensitive
values are redacted, and run/cycle controls remain blocked. That is focused
component-test coverage, not proof that the full browser scenario ran. The
handoff note's runtime acceptance caveat is retained; its contradictory source
gap assertion was not ported as a code change.

Other local browser test changes remove uncapped attention-count coverage and
the post-worker record-readback assertion. The local Python runner tests drop
Grype smoke coverage already noted above. The local DeepTeam lock removes the
declared `sentry-sdk` dependency and was generated from `requirements.txt`
rather than the checked-in `requirements.in`; local Promptfoo is older and
removes the PR's explicit Linux libsql package pin and `basic-ftp` override.
No current evidence justifies replacing these PR-side dependency/security
contracts with the local versions. This review is source/diff analysis only;
it does not establish live browser acceptance or third-party provider health.

### October 12, 2026: authentication and migration regression coverage

Compared the local IDP authentication tests with the PR. The local deltas in
password-reset, refresh rotation, login interleaving, Redis session loss, and
session-authority tests only remove `context.Context` from service and sender
calls. They target the older non-context-aware API and are not compatible
coverage improvements for the PR's context-aware authentication path. No local
test version was copied.

The local PostgreSQL migration tests also remove meaningful guard coverage:
the protected-data rollback fixture omits the later `post` phase; source
configuration/head tests no longer verify refusal to roll back a pre-phase
migration while a later phase remains applied, and weaken the isolated
populated-origin rollback guard; the source-observation fixture stops at an
older migration head; and the Trello reconciliation test applies every
migration rather than only the targeted migration and its prerequisites. The
PR's isolated real-Postgres assertions and migration-order checks remain. This
batch did not run the Postgres integration suite; no pass is inferred from
source inspection.

One safe local test improvement was ported: the transactional approval test
now uses the package's shared `fixedNow` clock and no longer overrides the
service clock redundantly. `git diff --check` passed. The local shell has no Go
installation (`go.exe` was not found in the standard Windows locations), so
the package test could not be run locally; exact-head GitHub Actions is the
remaining executable verification for this edit.

### October 12, 2026: workflow admission and Windows runtime contracts

The local ambient scheduler test deletes the PR assertion that a disabled
scheduler is reported as disabled and not running. Local automation projection
tests replace the accepting event publisher with an empty publisher, reducing
coverage of the publish path. The local Phase 2 Postgres claim test removes the
end-to-end path that ingests, classifies, owner-approves, claims, and releases
a source-derived operation. These coverage reductions were not copied. The
runtime-owner-gate delta is comment-only. The dossier test changes a generated
credential-shaped fixture to its equivalent literal, with no behavior gain.

The local task execution-context helper drops its private-context fallback and
the `hasTaskExecutionContext` distinction. Its transition test also replaces
the known pre-dispatch cancellation outcome (durably canceled, no review item)
with an uncertain-review expectation; the local required-participant test
removes the verified/healthy/provenance/freshness evidence and treats a ready
delegation alone as sufficient. The PR's cancellation and participant gates
are retained. The local reminder-boundary assertion also requires a non-nil
result when zero deliveries are eligible, contrary to the PR's explicit
no-work contract. None of these local changes was ported.

The local Phase 2 state-permissions proposal conditionally scans the full
volume before `chown`, but adds `DAC_READ_SEARCH` to a root one-shot container
and uses a shell pipeline whose final `grep` status can conceal a failed
`find`. It may avoid repeat ownership writes, but no benchmark or runtime test
establishes a net resource improvement, and its failure semantics need repair
before consideration. The PR's direct, networkless `chown` utility remains.
Local compiler-deadline tests cut fixture timeouts from 15 seconds to 5, and
local Windows native-runtime/process tests target Go 1.25.13 rather than the
PR's pinned Go 1.27.2. The local OpenClaw migration test compares raw SDDL
instead of the PR's normalized descriptor helper. These are not adopted.

The only code edit from the current audit batch is the shared fixed-clock test
cleanup recorded above. The Go package test could not run because Go is not
installed in the available shell; `git diff --check` passed before commit.

### October 12, 2026: provider transport and acceptance harness review

The local TLS test changes `httptest.Server.TLS` to `Server.Config.TLSConfig`
before `StartTLS`. Go's `httptest.StartTLS` consumes the former, so the local
change no longer installs the intended fixture certificate; the PR's test is
correct and was retained. The local production-dependency audit removes its
`include-optional`/`omit-optional` modes, so CI loses explicit coverage of both
audit scopes. The local isolated-acceptance contract deletes readiness retry,
synthetic-secret scrubbing, failed-preparation cleanup-manifest/hash, and
runtime-owned tmpfs assertions. These are not safe omissions.

The local Python lock check compares raw bytes and therefore rejects
line-ending-only differences, unlike the PR's CRLF-normalized lock comparison.
The local Promptfoo lock is older and removes the explicit Linux libsql pin and
the `basic-ftp` override. Neither dependency change was copied. This batch
found no additional code improvement to port; it was a targeted diff review,
not a live TLS, npm audit, Docker, or provider integration run.

### October 12, 2026: expanded local artifact inventory reconciliation

Rechecked the live PR head (`ef7ee540c2301b35e88f841b6568ff64850ffa85`)
against the separate primary checkout without modifying that checkout. The
local backend tree contains a hidden deploy marker and PostgreSQL test-evidence
files that are absent from the PR; these are local state/evidence, not product
source, and were not copied. Two additional local post-migration files named
`0004_operations_owner_workspace_dedupe_active.{up,down}.sql` have byte-identical
counterparts in the PR as `0006_operations_owner_workspace_dedupe_active`; PR
`0004` is already the automation event outbox. The candidate migration adds no
new behavior and must not be renumbered or applied a second time.

The local `output/` directory contains 62 Markdown review/handoff documents plus
large machine-generated run evidence. Their headings and selected current-state
amendments were indexed during this pass; they are not all independently
re-audited here. Examples checked against the PR include the Account Bridges
loading/error/accessibility/timestamp repairs and account-feed admission lock,
configuration version, source-head and effect-policy fences. Historical source
reviews explicitly distinguish fixes from residual limits; the PR source, not a
dated report's initial finding, is authoritative. Raw logs, receipts, database
dumps, browser output and sidecar reports remain local and were neither staged
nor deleted. This is an inventory reconciliation, not completion of the
remaining file-by-file audit or a claim that every report finding is closed.

The PR worktree was clean at the recorded head before this ledger update. No
local source artifacts were removed, and no private data or test output was
copied into the repository. The full untracked-output and differing-source
crosswalk remains in progress; all local material stays intact until each
relevant item has a verified PR disposition.

### October 12, 2026: refreshed source comparison and hosted CI state

Rechecked the preserved primary checkout against PR #36 head
`dec50d06b164cc69b9fa98e316d1e167c64cfd8e` without modifying the primary
checkout. It still reports 3,167 status entries (692 tracked changes and 2,475
untracked paths). Of those untracked paths, 1,509 have no same-named path in
the PR tree: 1,493 are under `output/`, ten are dated PostgreSQL test-evidence
files, four are local agent-state files, and two are diagnostic logs. These
counts classify paths only; they do not authorize cleanup.

A fresh byte comparison of the scoped untracked source-like text files
(`backend/`, `frontend/`, `idp/`, `services/`, `scripts/`, `installer/`, and
`docs/`, with the audited source/document extensions) found 841 paths that
already exist by name in the PR: 779 are byte-identical and 62 differ. This is
a deliberately scoped comparison, not a complete audit of all tracked changes,
binary/non-text files, generated output, or transcript archives. Prior ledger
entries review many of these differences by safety topic; they do not yet
establish line-by-line disposition of all 62. No candidate was copied in this
pass. Spot checks reconfirmed that the local database logger removes the PR's
validated `DB_LOG_LEVEL` handling and parameterized-query logger, the local
task context drops the PR's private-context fallback, and the local secret
scanner drops required CI base-revision validation and redacted finding
summaries. Those weaker variants remain excluded.

At this check, PR #36 remains open at the same head. Hosted run
`38210695028` is for that exact commit and is still in progress: all reported
checks passed except the `whispercpp-runner` contract, which was pending. PR
#35's separate CI result does not satisfy this gate. No source snapshots,
transcripts, logs, databases, or local application state were removed.

### October 12, 2026: local transcript archive identity verification

Ran the current read-only cleanup-readiness verifiers from this PR worktree
against the inventoried local roots. The main-root verifier returned
`source_archive_verified=true`: 18 manifest files / 20,739,169,122 logical
bytes, five SHA-256-verified candidates / 3,179,439,595 bytes, and 13 retained
files. The supplemental verifier returned
`supplemental_source_archive_verified`: nine files / 14,013,737,298 bytes,
two SHA-256-verified candidates / 3,113,294,358 bytes, seven retained files,
and 19 report-linked source paths present and tracked.

Combined inventory: 27 transcript files / 34,752,906,420 logical bytes; seven
candidate files / 6,292,733,953 bytes; 20 retained files / 28,460,172,467
bytes. The verifiers did not delete files and returned
`deletion_performed=false` and `cleanup_authorized=false`. Hash and path checks
establish source identity and crosswalk structure, not semantic understanding
of oversized, aborted, duplicate, or unattributed transcript content. PR #36
is still open, so the repository's merged-main cleanup gate is not met. All
27 local transcript files remain in place.

### October 12, 2026: refreshed integration-snapshot diff and guard review

Recomputed the preserved `D:\codex-temp\hai-integration-snapshot` comparison
against PR #36 head `297f5070b29bf123aa93aa2204b8791b53ccc978`. The snapshot
has 1,794 changed paths: 1,503 normalize to the same content as the PR, 275
differ at shared paths, three are snapshot-only, and 13 paths whose names
indicate environment/secret material were skipped without reading their
contents. Of the 275 differing paths, 224 are not mentioned by exact path in
this ledger; this is a path-reference heuristic, not a semantic disposition
count. The three snapshot-only files are the already-reviewed duplicate-name
post migrations and standalone legacy Nginx config. The snapshot and its
staged index remain untouched.

Fresh focused diffs rejected these additional snapshot variants:

- `backend/internal/modelintelligence/calibration.go` removes the exact-zero
  Wilson lower-bound case. The PR returns stable `0` when there are zero
  successes instead of persisting floating-point noise or negative zero.
- `backend/internal/llm/policy.go` replaces the named 4 MiB provider-catalog
  response bound with a 256 KiB literal, which can truncate larger catalogs,
  and removes rejection of endpoint query parameters/fragments. The explicit
  bound and endpoint validation in the PR are retained.
- `backend/internal/modelintelligence/http_openai.go` removes the explicitly
  recognized `ollama-local` provider host, reducing support for the isolated
  local Ollama topology.
- `backend/internal/assistant/service.go` removes context-aware task and agent
  cycle dispatch; it also no longer turns an agent-cycle error into a review-
  required result. The PR's cancellation propagation and surfaced failure
  state remain.
- `backend/internal/opscontrol/authorization.go` changes the safety-effect
  owner from the authenticated actor to configured workspace owner and removes
  the non-delegable immutable emergency-stop check. The PR's actor-bound
  receipt and emergency-stop fence remain.
- `backend/docker-compose.yml` removes the fixed Compose project name and
  explicit environment-file selection from the compatibility entrypoint.
  Those changes can make project identity depend on invocation directory and
  alter configuration loading; they do not establish a safe resource saving.
  The canonical explicit entrypoint remains.

No code from these six diffs was ported. This batch does not close the 275-file
semantic review, the primary-checkout tracked/untracked crosswalk, or the 13
skipped sensitive-name paths. PR #36 remains open at the recorded head; exact-
head Actions run `38212705796` completed successfully. PR #35 is separate: its
authorized Actions rerun (attempt 6) passed at
`57357dd1045ff3a6580d4055f2b7a5f634042acf`, but that is not evidence for PR
#36. No local source, transcript, archive, environment, database, or worktree
was modified or removed.

### October 12, 2026: receipt-read and safety-approval boundary review

Continued the snapshot comparison on three execution-boundary files. In
`backend/internal/executionauth/service.go`, the snapshot removes
`CloneWithEmergencyStopEvaluator`, which creates an isolated service for a
server-owned recovery boundary without mutating the shared authorizer. It also
adds life-graph projection calls to receipt `Get` and `List`. The PR performs
projection during receipt persistence or the transactional write projection;
the projector is an append-only operational-record operation, so putting it on
read endpoints would make inspection cause graph writes and potentially repeat
them. Neither snapshot change was adopted.

`backend/internal/executionapproval/composite_resolver.go` removes the dedicated
`opsControl` review resolver and its exact safety-control approval-prefix
routing. That would leave the privileged runtime-safety review reference
unsupported rather than resolving it under its own exact-effect policy. The
PR resolver remains.

The snapshot `backend/internal/phase2/handler.go` removes an explicitly
test-only diagnostics response, gated by both `RUN_MODE=test` and
`HAI_PHASE2_TEST_DIAGNOSTICS=1`. This removes test failure detail, not a
production feature; no runtime behavior improvement was established, and it
does not justify changing the current PR implementation.

This focused set is not a complete disposition of the 275 differing snapshot
files or the 224 differences lacking an exact-path ledger reference. The latest
PR #36 head remains `20e5ca69315588aa285c0f1278f09c7d53c01d6f`; its exact-head
run `38214257223` completed successfully. No local source or archive was
changed or removed.

### October 12, 2026: OAuth, evidence identity, and navigation contracts

Compared five more snapshot paths with PR #36:

- `backend/internal/source/oauth.go` removes creation and single-use consumption
  of the persisted, owner/source-bound Google OAuth state. This would remove
  the durable replay check for the callback. The PR's signed state plus
  digest-only persisted consume-once record remains.
- `backend/internal/source/repository.go` removes the OAuth-state repository
  methods and revocation cleanup, maps an optimistic source-version conflict
  to generic `gorm.ErrRecordNotFound`, and removes the ID tie-break from
  extraction-page ordering. These weaken replay protection, typed stale-write
  handling, and deterministic pagination; the PR behavior remains.
- `backend/internal/operations/domain.go` stops setting the initial raw source
  evidence SHA-256. The PR uses that fingerprint when deciding whether a
  duplicate observation represents changed evidence; the snapshot omission
  weakens the source-change check.
- `backend/internal/source/scheduler.go` removes scheduler-status publication
  for disabled, durable-running, failed-start, and volatile-fallback states.
  That hides operational health/degraded mode from the system status surface;
  the PR's redacted status reporting remains.
- `frontend/src/app/control-room/progressive-section.component.html` removes
  `data-hai-section`, which the PR shell uses to resolve module section
  deep-links and which the shell's route/deep-link tests query. The snapshot
  template would break that location contract; the PR attribute remains.

No code from these variants was imported. This comparison adds five exact-path
dispositions but does not reduce the audited 275-difference set until a full
path-by-path crosswalk is reconciled; no arithmetic subtraction from the
heuristic count is assumed. PR #36 is open at
`732fdeb61acdc90f1cf9a68820e1d263a8a170da`; exact-head run `38214966689`
passed. The snapshot and transcript archives remain unchanged by the review.

### October 12, 2026: agent coordination and framework crosswalk

Compared the snapshot's staged contents against the PR #36 checkout for all
14 paths under `backend/internal/agentcoordination/`, `agentcycle/`,
`agentframework/`, and `agentguidance/`. This included the coordinator,
timeouts, validation, dispatch and guidance implementation files and their
associated tests. SHA-256 digests matched for every path, so these are exact
content matches rather than unreviewed snapshot variants; no source change was
needed. The comparison read file contents but did not modify the snapshot or
PR worktree. It excluded environment/secret-named files and makes no claim
about paths outside this 14-file set. PR #36 head at comparison was
`8c9bc2139dd11ac8945bea42804180c177e5a331`; its exact-head CI run
`38215737492` was still in progress, so no CI result is claimed here.

### October 12, 2026: framework registry API and lock-key review

Compared all 23 staged snapshot paths under `backend/internal/frameworkregistry/`
with PR #36. Nineteen were byte-identical. Four differ:

- `handler.go` removes `Handler.Selection`, although the PR router still binds
  `GET /selections/:id` to that method. Applying the snapshot change would
  leave the route binding uncompilable.
- `repository.go` replaces the PR's hex-encoded owner/framework lock identity
  with raw owner + NUL + framework ID. The lock is passed as PostgreSQL text;
  a NUL byte is invalid in PostgreSQL text values. The PR's repository test
  also exercises distinct identities containing delimiter-like NUL data to
  protect against key collisions.
- `service.go` removes normalization of nil `conflictsWith` and `adaptations`
  slices to JSON arrays. That reintroduces `null` in the public framework
  catalog where clients consume collection fields.
- `service_test.go` deletes the regression test that rejects those `null`
  values.

No snapshot code was adopted. The 14-path agent coordination/framework
crosswalk above is a subset of the full snapshot comparison, not a complete
resolution of the remaining differing paths. The PR's latest head is
`9a21fe1b1b74ad52e45e75ea0604d4fc97d7c21d`; its exact-head CI run
`38216417197` was queued when this entry was written. The source snapshot and
transcript archives remain unchanged.

### October 12, 2026: health probes and integration-boundary review

Compared the staged snapshot versions of
`backend/internal/health/health.go`, `health_test.go`, and
`backend/internal/braincatalog/integration_boundary.go` with PR #36. The
snapshot removes the source, workflow, and ambient scheduler-status probes,
which would make those background-operation health states disappear from the
health report. It also replaces the PR's PostgreSQL probe of the acquired
serving pool with construction and ping of a separate database pool. That can
report a healthy new connection while the pool used by application requests is
unhealthy, and adds connection-pool work to a readiness check. The snapshot
deletes the tests that assert serving-pool identity, serving-pool failure
reporting, and inclusion of the noncritical scheduler probes.

The snapshot also removes the `chatgpt-codex-mcp-daemon` entry from
`integration_boundary.go`. That entry declares the concrete HAI route,
implementation, and safety boundary for its nine reviewed read-only tools;
without it, the integration is no longer represented in the boundary
inventory. No snapshot code was adopted. PR #36 was at
`f6c0960e98334afb2eb1ffcde9edba8926f95b27` at comparison time; the successful CI run recorded
above was for the earlier code head `9a21fe1b1b74ad52e45e75ea0604d4fc97d7c21d`,
so it is not claimed as an exact-head run for this documentation-only update.
The source snapshot, transcript archives, and primary checkout remain
unchanged.

### October 12, 2026: Windows lifecycle handoff revalidation

Rechecked `output/windows-lifecycle-production-20261001.md` against the exact
PR #36 source head `5c31d54aed5e35a13bbebf44c10fb68ecf519e9e`. Its source fixes
are present: Compose project assignments are counted before validation;
ownership enumeration includes the configured project and checks command
failures; `Start-HAI.ps1` repeats ownership validation after initialization;
`Stop-HAI.ps1` suspends optional caller overrides, snapshots the stop result,
and restores overrides in `finally`; worker promotion rejects directory and
reparse-point destinations before replacement.

Ran `scripts/test-windows-lifecycle-production-source.ps1` locally, then ran
its isolated `-MockedOwnership -MockedStop` modes. Both passed. The mocks
covered valid/foreign/missing ownership metadata, duplicate enumeration,
enumeration failure, Stop success/nonzero/throw paths, and exactly-once
environment restoration; they invoked no real Docker or process operation.
The exact-head GitHub Actions run `38291715078` passed all 36 jobs, including
`Windows installer preview and signing guards`. That job invokes
`test-windows-installer-contract.ps1`, which calls the runtime lifecycle and
installer update-promotion fixtures; this is hosted Windows regression
evidence, not acceptance on Robert's installed Windows machine.

The handoff's remaining production limits still apply: no user-machine
installation/update/uninstall or signing acceptance is established, and
path-based promotion checks are not a handle-bound defense against every
concurrent filesystem race. This crosswalk changes documentation only. PR #36
remains open and unmerged; local source, transcript, diagnostic, and runtime
data remain untouched. The broader semantic/provenance audit and cleanup gates
are still incomplete.

### October 12, 2026: safe-outcome consumer handoff revalidation

Crosswalked `output/safe-outcome-consumers-production-20261001.md` against the
current PR source. The broker preserves `SafeWorkerOutput` alongside an
execution error without invoking verification; phase-two responses retain
redacted receipt/operation correlation and uncertainty flags; Runtime Lab
attempts retain redacted receipt/status evidence and classify ambiguous
post-effect outcomes as inconclusive. The three assigned consumers retain
nonterminal claim fences when post-effect evidence is returned rather than
releasing authority for ordinary replay. Current focused regression sources
include `broker_partial_outcome_test.go`, `safe_outcome_response_test.go`, and
`safe_outcome_attempt_test.go`; the exact-head Actions run `38292809831` passed
all 36 jobs, including backend and IDP tests.

One handoff limitation is stale at the current source: it says the concrete
`LocalSafeWorker` returns zero output on its own error paths. After authority
consumption, `Run` now returns observed progress and a bounded reconciliation
summary on errors; the runtime adapter preserves that bounded output, and the
broker retains the structured partial result. `local_safe_worker_partial_outcome_test.go`
covers preflight/authorization/open/create failures, write/sync/read and
verification failures, bounded/redacted output, identity replacement, replay
refusal, and runtime-level failure output. This does not mean every partial
filesystem outcome has a complete artifact hash or verified completion: those
fields are withheld when the corresponding read/identity checks did not
complete. A receipt/HTTP response is not crash-durable; database persistence,
restart recovery, provider acceptance, and live production behavior remain
separate gates. No source code or local archive was changed in this
crosswalk; the broader content/provenance audit and cleanup gates remain open.

### October 12, 2026: pursuit completion handoff and timeline correction

Rechecked the five source findings in the pursuit appendix of
`output/background-outcome-production-20261001.md` against current source and
tests. The current PR recognizes completed runtime receipts by launch-specific
codes; unresolved required approvals block completion even when separate
verification evidence exists; task-run display status requires accepted
verification; non-UUID verification labels cannot substitute for an internal
verification record; and direct task-attempt activity/blockers use the shared
review predicate. Focused coverage is in
`backend/internal/pursuit/completion_outcome_integrity_test.go`.

That audit exposed one remaining inconsistency in the fifth finding: the direct
task-attempt timeline still derived `NeedsReview` only from status text and
`BlockedReason`, unlike activity/blocker projection. A `validated` attempt
with uncertain or conflicting verification could therefore appear ordinary
in timeline detail. Changed the timeline projection to use
`pursuitTaskAttemptNeedsReview` and added
`TestPursuitTaskAttemptTimelineUsesSharedReviewPredicate` for uncertain,
conflicting, blocked, review-required, and verified records. The first
exact-head CI run `38294366584` exposed a defect in the test fixture: its task
attempt had no timestamp, so the production timeline correctly omitted it.
The fixture now supplies `CompletedAt`; Go/gofmt remain unavailable locally,
so a follow-up exact-head backend CI pass is required before this entry is
considered tested. No other report finding required a source change in this
batch. The broader semantic/provenance audit and cleanup gates remain open.

### October 12, 2026: task retry economics and provenance review

The snapshot task service retries after validation failure whenever a retry is
available. PR #36 also checks whether another model can plausibly change the
result: if every claim is rejected solely for untrusted provenance, the
evidence boundary will produce the same review outcome, so the service skips
the redundant attempt and records why human review is needed. The PR's
`TestRetryRequiresPotentiallyActionableValidationFailure` covers all-untrusted,
mixed, empty, and nil claim results. This avoids unnecessary model work without
weakening evidence or approval requirements. No snapshot code was adopted; the
existing broader task-history, MCP-context, and cancellation findings remain
in the task comparison above.

### October 12, 2026: snapshot-wide byte inventory and remaining coverage

Re-ran a read-only SHA-256 comparison of all 1,790 paths staged in the source
snapshot index against the PR #36 worktree. Of these, 1,487 shared paths are
byte-identical, 299 shared paths differ, and three staged paths are absent
from the PR worktree. One staged path with a sensitive-name pattern was
excluded before reading either file's contents. This is a byte comparison of
staged snapshot paths only; unstaged snapshot changes and any path not staged
in that repository are outside this count.

The ledger does not quote 225 of the 299 differing filenames verbatim. That
number is a filename-indexing signal, not a count of semantically unreviewed
changes: existing package-level entries may already assess several files, and
byte differences can include formatting. Those paths still need explicit
cross-reference or review before claiming complete snapshot reconciliation.
Accordingly, this ledger is not yet a proof that every item from the source
snapshot or transcript archives has been integrated. No source or archive
data was modified, moved, or removed.

### October 12, 2026: completed-report source-link verification

Parsed repository-relative Markdown links in
`docs/child-agent-final-reports.md` and checked them against the current PR
worktree and Git index. The report bundle contains 756 link occurrences and
297 unique targets; 294 targets are files, all 294 exist and are tracked in
the PR tree, with no missing file targets. The remaining three unique targets
are non-file references. This establishes that linked source paths from the
preserved completed-agent reports remain available in the PR, not that each
report claim or nested `task_complete` event has been semantically validated.
The original transcript archives therefore remain protected pending the open
semantic crosswalk and PR/cleanup gates.

### October 12, 2026: corrected transcript patch-path reconciliation

Added the read-only PowerShell 7 utility
`scripts/audit-hai-transcript-patch-paths.ps1` and ran it over all 18 files in
`D:\codex-temp\hai-completed-agent-sessions` (20,739,169,122 bytes). The
utility emits only counts and relative target paths. It does not write to or
remove transcript data. The invocation compared absolute targets from the
original checkout with the current PR worktree, including the historical
`hai-repo-link` alias.

The pass found 45,017 JSONL records containing the `patch_apply_end` marker;
44,997 were actual `event_msg` patch records (44,941 successful, 56 failed)
and 20 were other record types containing that text. It parsed all marker
records without JSON errors. The patch events contain 793 unique target paths
and 70,295 path references: 68,756 to the primary checkout, 1,214 to the
separate `work\018-hai-port-engine-control` checkout, and 325 to temporary
audit/smoke paths. This reconciles the earlier 45,017 count: that was a marker
count, not an exact count of patch event records.

Of 704 unique primary-checkout paths, 14 use the historical
`hai-repo-link\` prefix and resolve to canonical, tracked PR paths after alias
normalization. At scan time 677 paths were tracked in the PR; four additional
paths existed in the worktree but were the safe workspace-policy files added
for this crosswalk and had not yet been committed. The remaining 23 paths were
not present in the PR tree:

- Local-only state: `.env.local` and `.git\info\exclude`. Neither was read or
  copied; the former is secret-bearing environment state and the latter is
  repository-local configuration.
- Preview/review tooling: `.codex-preview-server.mjs`,
  `.tmp-ai-code-review.json`, `frontend\mock-preview-server.js`, and
  `frontend\proxy.preview.json`. These are absent from both the current source
  checkout and staged source snapshot; path references alone do not justify
  restoring them as product code.
- `nginx-config\nginx.conf` is present in the old source checkout but not the
  staged snapshot or PR. It is a stale runtime config with a literal
  placeholder backend key and lacks the PR template's configured secret
  interpolation and host-runtime deny route. The current
  `nginx-config/nginx.conf.template` is the canonical implementation; the old
  file was deliberately not copied.
- Sixteen recovery/audit paths were not present in the current source checkout
  or staged snapshot: `pg-lifecycle-recovery.go.txt`,
  `recovery\catalog_test.go`, `recovery\pg-active-invariant.patch`,
  `recovery\pg-functions.patch`, `recovery\pg-trigger.patch`, the eight
  `recovery\registry-audit-stage\...` files, `recovery\test-small.patch`,
  `registry-postgres-audit-test.tmp`, and
  `repository_postgres_constitution_test.go`. Transcript patch records show
  these paths were subsequently deleted from the working tree; the lifecycle
  note was empty at deletion. The registry-audit staged copies map to canonical
  registry/migration/document paths already in the PR and are covered by the
  registry comparison entries above. The reviewed migration includes the
  active-constitution history trigger/functions and corresponding contract
  assertions. The remaining patch-file intent is not all semantically closed
  by this path inventory, so those historical records remain protected.

The four workspace-policy files added during this pass are
`security-snapshots/.gitignore`, `security-snapshots/README.md`,
`mini-swe-workspaces/.gitignore`, and `mini-swe-workspaces/.gitkeep`. Root
ignore rules now allow only these policy/placeholder files while keeping
snapshot and workspace payloads ignored. `git check-ignore` confirmed example
payloads remain ignored and the four policy files are visible to Git. They
were still untracked when the archive scan ran and must be included in the PR
commit before this entry is considered published.

This closes a structural path inventory and ports the missing repository
workspace guidance, not the full semantic review of 793 targets or every
transcript event. The other 1,214 scratch-checkout references, 325 temporary
references, source snapshot differences, child attribution, and external
acceptance remain open. No source checkout, snapshot, transcript, diagnostic,
credential, or runtime data was altered or removed.

### October 12, 2026: recovered PostgreSQL invariant patch disposition

Inspected the last successful transcript records for the PostgreSQL recovery
patch artifacts rather than relying on their filenames. The 722-character
`recovery\pg-active-invariant.patch` adds a PostgreSQL rejection assertion for
superseding an active Constitution without a replacement. The 1,205-character
`recovery\pg-functions.patch` adds catalog checks for the active-history
function and trigger. Both behaviors are present in the PR's canonical
`backend/internal/frameworkregistry/repository_postgres_test.go` (including the
rejection assertion) and `backend/migrations/pre/0003_framework_registry.up.sql`;
the migration contract tests assert the constraint trigger and rollback
function. These checks confirm those two patch artifacts do not contain an
additional unported invariant.

The transcript's `recovery\pg-trigger.patch` record is a failed add attempt,
not accepted implementation. `recovery\test-small.patch` is a synthetic
one-line `a` to `b` patch. The final successful delete records for
`pg-lifecycle-recovery.go.txt`, `registry-postgres-audit-test.tmp`, and
`repository_postgres_constitution_test.go` carry zero-byte content. The
non-empty registry-audit-stage copies remain covered by the framework-registry
source comparisons above; their path and deletion events alone do not make the
copies authoritative over the canonical PR files. No patch was replayed.

This closes the disposition of these specific recovered patch/test artifacts,
not every event that mentioned them or the full 793-target semantic audit.

### October 12, 2026: staged integration snapshot blob crosswalk

Compared the staged path set in the read-only integration snapshot at
`91c8620c557229f1da4ed15fcbb7088c6a6947a7` against this PR worktree at
`62a1f5eeb451ffeef0782ce3e5ba96990bd82dfc`, using Git index/tree blob IDs
only. Of 1,790 staged paths, 1,498 have byte-identical blobs in the PR, 288
shared paths differ, and three paths are absent from the PR tree:

- `backend/migrations/post/0004_operations_owner_workspace_dedupe_active.down.sql`
- `backend/migrations/post/0004_operations_owner_workspace_dedupe_active.up.sql`
- `nginx-config/nginx.conf`

One path matching a secret/credential filename pattern was excluded from this
comparison without reading its contents. `.env.example` was included. The two
snapshot-only migration filenames are not missing behavior: prior source and
blob checks recorded that their contents are identical to the PR's
`0006_operations_owner_workspace_dedupe_active` pair. The snapshot labels them
`0004`, which collides with the PR's automation-event outbox migration and
must not be copied or applied under that number. The third path,
`nginx-config/nginx.conf`, was previously found stale and unsafe to substitute
for the PR's current template; it is not an adoption candidate.

The snapshot path list itself does not establish that each changed blob is
valid, safe, or authoritative. The ledger contained literal path mentions for
76 of the 291 non-identical eligible paths; this is a navigation aid only, not
a semantic coverage measure. The differing paths are concentrated in backend
(139), frontend (67), IDP (24), scripts (31), docs (14), and smaller root,
gateway, services, and Nginx areas. Each candidate still requires provenance,
behavioral comparison, and appropriate tests before any port is considered.

No snapshot files were read into the worktree, changed, staged, or removed by
this comparison. No code was automatically adopted from the snapshot. This
crosswalk does not close the semantic audit or make any local archive eligible
for deletion.

### October 12, 2026: workflow scheduler and request-cancellation comparison

Reviewed the staged snapshot versions of
`backend/internal/workflow/scheduler.go` and
`backend/internal/workflow/handler.go` against the PR tree. The PR scheduler
records disabled, durable-attached, startup-canceled, lifecycle-rejected, and
in-process-fallback states through `schedulerstatus`; its fallback detail is
secret-redacted. The snapshot version omits these operational status records
and logs the durable-queue error without redaction. In the handler, the PR
checks request-context cancellation after contextual execution and avoids
writing a response to a canceled request; the staged snapshot lacks that
guard. Retain the PR versions; neither staged file is an improvement to port.

This is a source-path review of these two workflow files, not an acceptance
claim for the remaining workflow, frontend, IDP, or backend differences.

### October 12, 2026: IDP authentication context propagation comparison

Compared the staged snapshot's IDP authentication handler and service against
the PR tree. The handler in the PR passes the request context into logout,
session validation, refresh, and password-reset calls. The snapshot uses
context-free service calls. The corresponding PR service methods validate
context presence and cancellation and propagate it through token/session work;
the snapshot service exposes older context-free method signatures. The PR
therefore retains cancellation and request-lifetime boundaries that the
snapshot would remove. Keep the PR implementation; do not port the staged
handler/service pair as a replacement. This is a scoped authentication-path
review, not a complete review of the 24 differing IDP paths.

### October 12, 2026: ChatGPT Logs MCP feature cross-PR reconciliation

The staged snapshot's Task Blueprint does not contain the MCP tool-call result
rendering present in the separately submitted ChatGPT Logs MCP work. PR #35
(`feat: add model-directed ChatGPT logs MCP tools`) includes the Task Blueprint
template/style/spec/component files and corresponding backend task, MCP
preflight, execution authorization, verification, and route changes. Its
head `57357dd1045ff3a6580d4055f2b7a5f634042acf` passed all ten CI jobs on
October 12, 2026, and PR #35 is now merged. Do not duplicate its implementation
into PR #36; the merge is already present in `main`. The CI and merge evidence
establish repository integration, not production deployment or live-provider
acceptance.

### October 12, 2026: progressive disclosure and pursuit selection review

Compared the staged snapshot's Angular page variants with the PR for the
framework registry and Pursuits. The PR framework-registry component exposes
per-module Basic/Advanced mode and section toggles through
`ModuleViewPreferencesService`; those controls are absent from the staged
snapshot variant. Retain the PR's progressive-disclosure implementation.

The staged Pursuits variant automatically selects the first record when no
route selection exists. The PR intentionally keeps the list compact until the
user selects a pursuit, with a component test asserting that
`applyPursuits()` does not invoke `selectPursuit()` in that state. Auto-select
would also initiate detail loading and replace route query state. Treat this
as an intentional interaction contract, not missing functionality; do not
port the staged behavior. These are focused UI-path decisions, not a complete
review of the 67 differing frontend paths.

The System Status page also retains the PR's resource-aware polling behavior:
two-minute polling when healthy, one-minute polling when degraded, a 15-second
recovery interval, visibility pausing, and a 10-second readiness timeout. The
staged snapshot instead polls on a fixed 15-second interval, including while
hidden, and does not apply the readiness timeout. The PR tests exercise
backoff, hidden-page suspension/resume, and stalled-request recovery. Keep the
PR implementation; the staged version would add unnecessary background load
and allow a stalled request to remain unresolved.

### October 12, 2026: operation approval and worker-claim comparison

Compared the staged snapshot's operation approval, claim, and regression-test
paths with the PR. The PR binds a source-approval receipt to the exact
operation evidence revision, rejects duplicate approval receipts, and includes
raw-evidence digest expectations in ingest atomicity tests. The staged
`source_approval.go` variant lacks those exact-revision digest checks. The PR
claim query uses the actual `operation_events.payload_json` column and permits
a fresh receipt-backed exact approval to supersede an earlier self-hold
reminder; its query contract test asserts both rules. The staged claim variant
uses a different `payload` column and lacks the tested review-reminder
precedence. The PR ordering test also includes SQL-order assertions for
`ClaimNext` (`created_at`, `updated_at`, then `id`), which the staged version
does not. Retain the PR implementation and tests; do not port these older
variants. This comparison covers these paths, not all 139 differing backend
files.

### October 12, 2026: LLM maintenance and provider-probe comparison

Compared the staged LLM policy and scheduler with the PR after an initial
filename/line-delta check suggested an endpoint-credential validation gap.
There is no such gap: the PR readiness parser rejects embedded URL
credentials, query parameters, and fragments, while the staged version checks
embedded credentials but omits the latter two checks. Existing PR tests cover
credential and query rejection. The PR also starts the model-maintenance
loop only when maintenance is enabled and eligible work exists; the staged
version starts it without that eligibility guard. Keep the PR behavior.

The staged policy uses a 256 KiB cap for the model-catalog response, while
the PR uses the named 4 MiB bounded cap for provider catalogs and retains
smaller caps for health probes. This is a bounded compatibility/resource
tradeoff; do not lower it without verifying supported catalog sizes. The
`modelintelligence/maintenance.go` and
`modelintelligence/provider_registry.go` files are line-identical between the
staged index and PR in this comparison. No LLM code was copied from the
snapshot. This is a scoped review, not a full review of the 139 backend
differences or live-provider evidence.

### October 12, 2026: environment-template name crosswalk

Compared assignment names only in the staged snapshot's `.env.example` and
the PR template; no environment values were printed or copied. The staged
snapshot has 593 distinct assignment names, all present in the PR. The PR has
ten additional names: `CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL`,
`HAI_A2A_LOCAL_SUBNET`, `HAI_CHATGPT_LOGS_MCP_ENABLED`,
`HAI_CHATGPT_LOGS_MCP_TIMEOUT_SECONDS`, `HAI_CHATGPT_LOGS_MCP_URL`,
`HAI_HOST_RUNTIME_INTERNAL_SUBNET`, `HAI_LEGACY_DATA_OWNER_IDENTITY`,
`HAI_SEARXNG_SECRET`, `OPERATIONAL_GRAPH_CACHE_SECONDS`, and
`OPERATIONAL_GRAPH_MAX_NODES`. These cover controls/features present in the
PR that are absent from the snapshot template; the template comparison found
no environment configuration to port from the snapshot. It does not inspect
or authorize changes to machine-specific `.env` files.

### October 12, 2026: cleanup-gate audit and Trello regression crosswalk

Ran the transcript-removal and unified cleanup readiness contract tests from
the PR worktree; both reported PASS. The remover is constrained to the
inventoried transcript root and manifest-listed candidate files, revalidates
candidate size/hash, checks the supplemental source archives and the
source/ledger/merged-PR gates, and requires both an explicit apply switch and
a count-bound confirmation phrase. The source-integrity readiness script
reported 18 ledger rows (5 candidate files, 3,179,439,595 candidate bytes),
but `source_archive_verified`, committed-ledger verification, merged-PR
verification, and `cleanup_gate_ready` were all false; no deletion was
performed. PR #36 remains open, so local archive cleanup remains blocked.
These contract checks do not execute the remover or prove post-delete
recovery; no removal was attempted.

Compared staged Trello connector files with the PR. The core connector,
durable sync, network, sync repository, webhook intake, reconciliation, and
webhook repository implementations are byte-identical. The PR's
`trello_webhook_reconciliation_generations_integration_test.go` correctly
limits apply/reapply/rollback operations to migration 0109. The staged
snapshot instead passes the full migration set while asserting exactly one
migration, so keep the PR test. The staged `trello_live_test.go` also calls
the context-free `trelloGetJSON` with a context argument; the PR's
context-free call matches the helper signature, while cancellation-aware
callers use `trelloGetJSONContext`. Do not port either staged test variant.
The source connector has not been tested against Robert's live Trello
account in this audit; CI fixture and contract results are not provider
acceptance.

### October 12, 2026: account-feed and source-runtime crosswalk

Compared every staged production file under `backend/internal/accountfeed`;
all 12 files are identical to the PR, as are the staged account-feed tests.
No account-feed source or test code needs porting from this snapshot.

Reviewed the changed source-runtime production paths. The PR already has the
newer or safer behavior in the compared variants: durable source jobs retire
stale work after a source is disabled/revoked rather than spending provider
retries; the scheduler reports lifecycle/readiness and obeys the background
safety gate; legacy Autofever sync is explicitly disabled until its upstream
feed/review contract exists; and extraction listing uses an owner-scoped
database page query with a hard limit. The staged extraction handler instead
loads every matching extraction and slices the result in memory, so its
apparent pagination is not a bounded database query. The staged background
worker also omits the live paused-mode handling and safe deferral for already
approved local operations. Keep the PR implementations; do not port these
snapshot variants. Core authorization, calendar, contacts, Drive, extraction
correction, OAuth retry, manual sync, provider error, Trello, and worker-control
files compared in this pass are byte-identical. This is a focused source and
account-feed crosswalk, not a complete audit of every source test or provider
acceptance against real accounts.

### October 12, 2026: migration-tree reconciliation

Compared all 186 staged paths under `backend/migrations` by Git blob identity:
177 are byte-identical to the PR, seven existing paths differ, and two staged
post-phase migration files are absent from the PR. Reviewed differences
include older or less isolated snapshot tests: its chain contract omits the
PR's later migration tail, its source-head rollback test attempts to remove a
pre-phase prerequisite from a database with post-phase runtime migrations
already applied, its source-configuration rollback assertion does not isolate
the down-SQL data guard from phase-order refusal, and its Trello test applies
the full migration set where the PR limits the operation to migration 0109.
Keep the PR tests.
The two snapshot-only post migrations are the same active-operation dedupe
pair already present in the PR under the correct `0006` version, but the
snapshot labels them `0004`, colliding with the PR's existing
`post/0004_automation_event_outbox`; do not add or renumber them. This
crosswalk does not replace a full semantic review of every migration pair or
a real upgrade/restore test against a production database.

### October 12, 2026: current PR and merge-base status

Rechecked the repository after PR #35's CI authorization and rerun. PR #34
(`Add Windows 11 HAI installer`) and PR #35 are both merged; PR #34's merge
commit is `4f24b449c0ef88f87b9d9f8fc250e81021876266`, and PR #35's exact-head
CI run `38175351645` completed successfully with ten successful jobs and no
failures. PR #36 remains open at `531cb7ed5b0776dedaa480cb112801924fd02a4f`.
Against current `origin/main` (`7efe1183`), a read-only `git merge-tree`
simulation reports 125 conflicting paths; 120 overlap with PR #34's merged
change set and 13 overlap with PR #35's (some paths overlap both). No merge or
conflict resolution was attempted in this audit update. PR #36 cannot be
considered integrated until these conflicts are resolved, verified, pushed,
and its current-head checks pass. Local source archives and scratch worktrees
remain preserved; no cleanup was performed.

### October 12, 2026: runtime-lab lazy-load improvement and control-center payload candidate

The PR worktree now defers Runtime Lab feature-parity and capability-catalog
requests until the corresponding progressive section opens. Successful results
are reused while the section is collapsed; an explicit refresh reloads them
only when the section remains open. The component template is wired to the
progressive disclosure's `openChange` event, including its saved-open state.
Focused Angular component tests were added for deferred loading, cached reuse,
and explicit refresh behavior.

Reviewed ambient-composition commit `13bb9452` (`Reduce control center
payloads`). Its `view=counts` pursuit projection is not a safe drop-in: current
Control Center action selection reads the summarized decision queue and
representative pursuit records to produce a concrete next action. The PR
already has a bounded pursuit summary projection and an ambient summary
projection; preserve those richer action inputs rather than replacing them
with count-only responses. No backend code from that historical commit was
copied.

Verification in this worktree: `git diff --check` and the progressive-section
contract checker passed. TypeScript syntax transpilation for both changed
component files and Angular template parsing also passed using the existing
primary-checkout toolchain. The frontend Node suite reported 23 of 34 cases
passing; its 11 failures require packages missing from this worktree's partial
`frontend/node_modules` (including Angular compiler/forms/router, RxJS,
TypeScript, `semver`, and Jasmine). The targeted Angular component tests and
production build remain unverified. Existing dependencies in the primary
checkout were used read-only through a temporary Node loader; no dependencies
were installed and no scratch data was modified.

### October 12, 2026: refreshed PR #36 publication and CI state

Queried GitHub for PR #36 after the earlier merge-base checkpoint. It remains
open at head `ea35ea180b1755ce2ba807c949864d5a73c4adf7`, targeting `main` at
`bec266b74329b2b74f51234c1da14db1080d38e9`. The current `origin/main` is an
ancestor of the PR head, so the older 125-path merge-tree conflict result
against `7efe1183` is superseded and must not be used as the current blocker.
CI run `38254639526` reports all listed jobs successful except `Browser
acceptance (owned disposable stack)`, which fails when its `npm test` process
exits 1. The available failed-job log does not expose a specific Playwright
assertion or root cause, so this remains an unresolved release gate rather
than a verified product defect. PR merge state is `UNSTABLE`; no push, rerun,
merge, cleanup, or mutation of the existing scratch worktrees was performed.

### October 12, 2026: PR #36 browser failure artifact follow-up

Downloaded and inspected the retained browser-acceptance artifact for run
`38254639526`. The exact failing flow is runtime-selection approval in
`frontend/e2e/tests/acceptance.spec.ts`: the candidate button is visible and
the workflow record remains in `needs approval`, but the expected
`POST /api/v1/workflow/{id}/proposals/{id}/resolve` is never observed. The
isolated backend log contains no matching resolution request and no failed
HTTP status for that action. This narrows the fault to the client-side action
path or its pre-dispatch guards; it does not prove which guard or UI condition
caused the drop. The post-failure snapshot still shows the selection proposal
open and no framework selection recorded.

The artifact also records two console errors categorized as `other` on route
matrix cases, with zero page errors, failed requests, or HTTP errors. The
artifact intentionally excludes raw console messages. Updated the operator
acceptance test to assert the candidate button is enabled, cap the response
wait at 15 seconds, and include a bounded, redacted console summary if the
expected request is absent. TypeScript syntax transpilation and
`git diff --check` passed for this test-only diagnostic change. The instrumented
test has not been run, and no product-code fix has been made or claimed.

The diagnostic test change and ledger update are committed locally in
`codex/hai-ledger-review-20261012` (commit `879e37d3`); they have not been
pushed to PR #36. PR #36's browser acceptance is still a failing release gate,
and its workflow has not been rerun. PR #35's separate CI authorization does
not authorize a PR #36 run. All browser artifacts and worktrees remain
preserved.

### October 12, 2026: refreshed dirty-checkout content comparison

Recomputed the older May checkout's tracked working-tree paths against the
preserved PR #36 audit worktree. The older checkout is on
`codex/hai-runtime-release` at `e07b9daeb3ba2630ecbe12a64d948f791203c241`,
388 commits behind its remote-tracking branch, and has 682 modified tracked
paths. Comparing working-tree contents after normalizing CRLF and CR line
endings against the audit worktree at `70149373f50481e4f844c8757440d2710547402b`
found 447 identical paths, 234 differing paths, and one path present only in
the older checkout (`nginx-config/nginx.conf`). The current PR #36 remote head
remains `ea35ea180b1755ce2ba807c949864d5a73c4adf7`.

This live result supersedes the earlier 75-content-difference checkpoint for
the current checkout state; the reason for the count change is not established.
The comparison is path/content evidence only, not semantic acceptance of the
234 changes. The lone Nginx file is a legacy configuration candidate; current
gateway references and tests use `nginx.conf.template`, so it was not copied.
No source files were moved or changed in either checkout, and all existing
worktrees and artifacts remain preserved. Continue package-by-package review
of the 234 differing files before any port or cleanup decision.

The Nginx distinction was verified in the PR worktree: local Compose renders
`/etc/nginx/nginx.conf` from `nginx-config/nginx.conf.template`, and
`python -m unittest nginx-config.test_gateway_contract` passed all 20 tests.
This verifies the repository gateway template contract, not a live container
reload or deployed proxy behavior.

### October 12, 2026: source-connector delta spot review

Reviewed the current source-package differences in the older checkout against
the PR worktree, confirming the direction of the earlier comparison: these
examples are safeguards already present on the PR side, not missing local
features to copy. The PR source implementation persists owner-bound,
single-use Google OAuth state by digest; passes cancellation through semantic
source search; escalates terminal scheduled-sync failures into reviewable
workflows; disables Autofever sync until its contract exists; preserves an
opaque LARO cursor reset after a successful cursorless retry; and requires an
explicit setting before a volatile scheduler fallback may run. The PR's
Trello adapter also checks a bounded action page before skipping a quiet-board
reconciliation and falls back to the full bounded read on activity,
saturation, or malformed action data.

These are selected production-code spot checks, not a semantic review of all
15 differing source-package files or their tests. The PR's backend checks have
passed at its recorded head, but no live Trello, Google OAuth, LARO, or other
provider-account acceptance was performed. No source was copied from the
older checkout.

### October 12, 2026: backend ownership and frontend contract spot review

Reviewed several primary-addition-dominant files in the refreshed crosswalk.
The PR's verification repository bounds run history at 200 and supports exact
owner-scoped record lookup, with ownerless legacy reads restricted by
`CanReadLegacyOwnerlessData`; the HTTP handler rejects absent authenticated
owners and the service rejects blank owners before calling the owner-scoped
repository methods. A production call-site search found no callers of the
separate unscoped `Runs()`/`RunDetails()` methods outside the verification
package; the HTTP handler does not use them. They remain exported on the
internal service interface, so future in-process callers must not treat them
as authenticated reads.

The PR's framework-provenance client fetches the exact selection through
`/framework-registry/selections/{id}` instead of scanning a fixed 200-row
history; its backend route provides the owner-scoped lookup. The PR's memory
service interface aliases shared model query/page types rather than maintaining
a second frontend contract. These PR-side implementations were retained; no
older-checkout code was copied. This is a targeted contract/security review,
not semantic acceptance of every frontend or verification delta. Full
frontend component tests remain unverified in this partial worktree.

### October 12, 2026: bounded transcript audit readers

The patch-path and retained-completion audits previously used
`StreamReader.ReadLine()`, which can allocate an entire JSONL record before
inspecting it. The retained transcript inventory includes records larger than
1.5 GB, so running either old reader against those files risked excessive
memory use or process failure. Both scripts now use the shared
`scripts/lib/hai-transcript-records.ps1` reader: fixed-size character chunks, a
10,485,760 UTF-16-character-unit cap (lowerable for tests), skip-without-buffering
for oversized records, and continued scanning after each skipped record. Their
reports expose skipped-record/file counts and mark the scan incomplete if any
record was skipped or a relevant record failed to parse. The retained-completion
audit also accepts an explicit manifest and JSON output for reproducible tests.

Both `pwsh -NoLogo -NoProfile -File
scripts/test-hai-transcript-patch-path-audit.ps1` and
`pwsh -NoLogo -NoProfile -File
scripts/test-hai-retained-completions-bounds.ps1` passed with synthetic
oversized records followed by valid final records without trailing newlines.
The tests confirmed the oversized records are reported, later valid events are
counted, path resolution remains correct, and scans are marked incomplete. Test
fixtures were intentionally retained under the system temporary directory. The
shared reader has not yet been run across the source transcript archives; their
contents remain untouched, and this change alone does not alter transcript
cleanup eligibility.

### October 12, 2026: canonical PR and stale-checkout audit refresh

This is the latest state check and supersedes earlier same-day branch, PR-head,
and dirty-checkout counts above. The canonical repository is
`Robert-Velhorst/018-HAI`; PR #36 remains open against `main` from
`codex/hai-runtime-release` at `6ea5c83db7d18d0b96b5bdd979db3c799c06211e`.
The local audit worktree is at the same commit. A live GitHub query reported
`CLEAN` merge state and 36 successful checks out of 36, including browser
acceptance. The user authorized pushing to PR #36 and running its CI; no merge
was authorized or performed. The audit-ledger commit
`6ea5c83db7d18d0b96b5bdd979db3c799c06211e` was pushed to PR #36; its
exact-head CI run `38281476207` completed successfully.

The preserved older checkout is currently at
`e07b9daeb3ba2630ecbe12a64d948f791203c241`, 414 commits behind the PR branch.
Its working tree contains 3,167 status paths: 692 tracked modifications and
2,475 untracked paths. The latest crosswalk covers 860 eligible untracked
source/document/local agent-state candidates (one byte-identical, 782 equal
after line-ending normalization, 67 differing, ten absent from the PR
checkout) and 653 eligible tracked modifications (three byte-identical, 423
equal after line-ending normalization, 227 differing, none absent). The
crosswalk excludes secret/environment-named files and paths outside the
configured source/document extension allowlist. These counts show path/content
equality only, not semantic integration.

The ten PR-absent paths are `.claude-flow/agents/store.json`,
`.claude-flow/policy/state.json`, `.claude-flow/terminals/store.json`,
`.swarm/model-router-state.json`, and six generated PostgreSQL evidence files:
`backend/internal/outcomeevaluation/test-evidence/20260930-pg-snapshot/cleanup-proof.json`,
`format-ownership.json`, `pass-summary.json`, `pg-ownership.json`,
`pre-cleanup.json`, and `runner-ownership.json`. Their contents and provenance
have not yet been semantically verified; all ten remain in place. A metadata-
only JSON inspection found no sensitive-looking key names, but did not inspect
or publish secret-bearing values. The four agent-state records include
ephemeral agent, policy, terminal/history/output/environment, working-directory,
and model-router state; they are runtime state, not product source. The six
PostgreSQL snapshot records include machine-specific container identifiers,
mounts, commands, networks, and hashes. Their fields self-report 15 passes,
zero skips/failures, and a zero remaining-row count alongside zero deleted
volumes and `parentStackUsed=false`; these are not independently verified live
acceptance or cleanup receipts. Raw records were not copied into the PR.

Additional bounded source comparisons in this audit confirmed these PR-side
implementations are already more complete than the older checkout and should
not be overwritten by it:

- Source-approval revision digests include `SourceEvidenceRawSHA256` and the
  reviewed owner type; execution rejects mutated approvals with distinct stale
  diagnostics, and an explicit approval remains claimable despite a future
  review reminder. Regression tests cover these cases.
- The database logger validates `DB_LOG_LEVEL` and enables parameterized
  queries.
- OpenClaw pulled-lease tokens are rejected if any character is outside
  lowercase hexadecimal. The older `strings.Trim` predicate did not reject
  invalid interior characters.
- Reminder-boundary tests permit a nil summary for zero processed work.
- Password-reset tests use the context-aware request method. OpenClaw's pinned
  public key is byte-encoded to avoid a credential-scanner false positive.
- The execution-claim implementation uses the current `payload_json` schema
  column; the old checkout referred to `payload`.
- Runtime safety-control changes require durable owner reviews bound to the
  current emergency-stop revision and exact confirmation. Resolver prefixes
  are checked against reserved namespaces, resume has no caller-supplied
  authorization-proof bypass, and malformed pause bodies are rejected. The
  service, handler, and resolver regression tests are present.
- The ambient scheduler requires the durable path or both explicit controls
  for a legacy in-process fallback, and publishes its disabled/degraded state.
  Tests cover policy-disabled startup and the explicit fallback requirement.
- OpenClaw ecosystem uploads recheck request cancellation after inspecting a
  streamed upload and before persistence; the handler test verifies that
  cancellation does not change the stored ecosystem path.
- The progressive workflow UI converts bounded multiline success criteria
  into the backend's `successCriteria` request contract, with component tests
  asserting the normalized payload. The shell also keeps browser titles in
  sync with the active module and avoids stealing focus from an already focused
  mobile-navigation control.
- Windows installer selection retains versioned pre/post SQL migrations while
  excluding unversioned SQL exports, databases, secrets, and unreviewed
  untracked payloads; the selection contract tests cover those cases. The
  Promptfoo runner lock is at `0.124.1`, with a pinned native `libsql` package
  and `basic-ftp` override; the production dependency audit remains enabled.

No source from the old checkout was copied in this audit step because the
inspected deltas were already represented by the PR version, and exact-head CI
passed. Remaining work is
still substantial: semantically review the 227 tracked and 67 untracked
source/document content differences; identify and account for the ten absent
evidence paths; finish provenance and unique-finding review of outputs,
diagnostics, and transcript archives; and only then define an exact, safe local
cleanup allowlist. No local files were removed or moved. The earlier 2026-10-12
counts elsewhere in this ledger describe earlier snapshots and must not be
used as current state.

### October 12, 2026: exact-head CI completion

Superseding the PR-head and check status above: commit
`43e9e236f731e7d85b862ba13649a6b05d81568f` is the current PR #36 head. GitHub
Actions run `38282312428` completed with conclusion `success` for that exact
SHA. The user authorized pushing the two local commits to PR #36 and running
its workflow; no merge was authorized or performed. This CI result establishes
repository workflow success only; it does not complete semantic review of the
remaining local deltas or establish live external-provider acceptance.

### October 12, 2026: retained transcript path-reference scan

A read-only streaming scan processed all 18 JSONL files in
`D:\codex-temp\hai-completed-agent-sessions` (20,739,169,122 bytes). It found
45,017 marker-containing records: 44,997 patch events, 44,941 successful and
56 failed; 20 markers were not patch-event records. No matching-line JSON parse
errors occurred. Two oversized records were skipped at the configured
10,485,760-character limit, so `patch_event_scan_complete=false`; this is not
a complete patch-history scan.

The scan found 793 unique target paths, 70,295 total references, and 704 unique
paths under the preserved checkout prefix. Of those, 681 are tracked in the
current PR repository, zero were present-but-untracked there, and 23 were not
found in the PR checkout:

```text
.codex-preview-server.mjs
.env.local
.git/info/exclude
.tmp-ai-code-review.json
frontend/mock-preview-server.js
frontend/proxy.preview.json
nginx-config/nginx.conf
pg-lifecycle-recovery.go.txt
recovery/catalog_test.go
recovery/pg-active-invariant.patch
recovery/pg-functions.patch
recovery/pg-trigger.patch
recovery/registry-audit-stage/backend__internal__frameworkregistry__migration_contract_test.go
recovery/registry-audit-stage/backend__internal__frameworkregistry__repository_postgres_constitution_test.go
recovery/registry-audit-stage/backend__internal__frameworkregistry__repository_postgres_test.go
recovery/registry-audit-stage/backend__internal__frameworkregistry__repository.go
recovery/registry-audit-stage/backend__internal__frameworkregistry__service_test.go
recovery/registry-audit-stage/backend__migrations__pre__0003_framework_registry.down.sql
recovery/registry-audit-stage/backend__migrations__pre__0003_framework_registry.up.sql
recovery/registry-audit-stage/docs__framework-registry.md
recovery/test-small.patch
registry-postgres-audit-test.tmp
repository_postgres_constitution_test.go
```

The paths include local configuration, preview-only tooling, and recovery/test
artifacts, but their originating patch contents have not been semantically
reviewed. Do not infer that all are disposable or that all represent missing
product behavior. The source transcripts were not modified. The two skipped
records and all 23 missing-path references remain open items; no cleanup
allowlist can be derived from this scan alone.

### October 12, 2026: oversized-record identification and current PR checkpoint

A fixed-buffer byte scan identified the two records skipped by the focused
10 MiB path scan. Both are in files previously designated retained:

- `rollout-2026-07-30T10-02-19-019fb20b-b4b1-7250-9d7c-9d1e591ab852.jsonl`,
  line 29,807: 1,016,062,444 bytes.
- `rollout-2026-07-30T11-59-34-019fb277-0ddf-7500-9088-52cb4655be1a.jsonl`,
  line 31,014: 904,740,623 bytes.

The bounded prefix of each record identifies it as a `compacted` record,
consistent with the earlier structural classification. Only the record prefix,
line number, and byte count were read for this identification; the complete
records were not parsed or semantically reviewed. This byte measurement
supersedes the earlier approximate 67 MB note for these same two records. A
first metadata-only pass using PowerShell line strings grew to approximately
6.2 GB working set and was stopped; the fixed-buffer replacement remained
below 170 MB. No transcript data was modified.

Current PR checkpoint: PR #36 remains open against `main` at
`5c623fe98c04763589868fabaaa45f969a8eef90`, merge state `CLEAN`. GitHub Actions
run `38283787440` completed successfully on that exact SHA. These checks do not
resolve the historical records' contents or authorize local cleanup. No files
were deleted or moved.

### October 12, 2026: Windows contract and runner-delta dispositions

Rechecked additional old-checkout variants against the current PR tree. The
runtime DB role contract test in the PR already normalizes CRLF to LF before
checking shell-script guard order; the Windows worktree is confirmed `w/crlf`,
and the older local test does not normalize it. The PR's
`backend-state-permissions` contract matches the actual local Compose service:
it uses the already-built backend image with a bounded, networkless `chown`
helper. The older local test expects a separate Postgres utility image and the
prior `find/stat/chown` command, so it is stale and was not copied. The PR
emergency-stop permission test supplies the valid JSON request body; the local
variant sends no body and conflicts with the current malformed-body rejection.

The Python container smoke implementation in the PR includes the Grype
runner's synthetic token/workspace and isolated read-only input/database
tmpfs mounts, which are absent from the older local copy. The PR's Windows
installer smoke test performs an actual bounded create/read/delete probe in
the isolated installation directory rather than relying only on ACL
inspection. The local DeepTeam lockfile has 68 package pins; the PR has 69,
including `sentry-sdk==2.71.0`; it is not a newer replacement candidate. The
outbound TLS test differs only in choosing `httptest.Server.TLS` rather than
`Config.TLSConfig` before `StartTLS`; no missing product behavior was found.
No code or lockfile was copied from the older checkout in this batch.

Current PR checkpoint: PR #36 remains open against `main` at
`abe41d66e6863d4a9bb2acb11f26ca676b72139f`, merge state `CLEAN`. GitHub Actions
run `38286402325` completed successfully for that exact SHA. This closes only
the listed file-level comparisons; the wider semantic crosswalk, transcript
attribution, merged-main gate, and local cleanup remain incomplete.

### October 12, 2026: runner and test-harness crosswalk follow-up

Completed the remaining review of the small source-like differences surfaced
by the bounded untracked-file scan. The PR's Python runner CI contract
normalizes CRLF/LF before exact lock-versus-runtime requirements comparison;
the older variant does not. The PR's Grype container-smoke contract now checks
the synthetic workspace/token and empty read-only `/inputs` and `/grype-db`
mounts. The corresponding implementation adds that only for the Grype runner,
matching the runner's required configuration and read-only inputs. These
checks are present in the PR, not missing local work.

The local outbound-TLS test changes only the `httptest` TLS configuration
field used before `StartTLS`; no production transport behavior is changed. The
local phase-two permission contract expects an older Postgres-based helper,
while the PR's Compose contract uses the already-built backend image and
networkless `chown` service. The PR emergency-stop test uses the valid request
body required by the current handler. The corresponding source-approval
differences were already reviewed in the preceding audit entry: the PR binds
approvals to source-evidence hashes and owner type, while the local variant
does not. No new product code was found to port from this batch.

Exact-head CI for PR #36 commit `19f44c506c55f04e468992c6ad27a6c69854d646`
completed successfully in run `38287191833`. This follow-up closes these
named path differences only; it does not reduce the remaining 227 tracked and
67 untracked content-difference review set or the archive/cleanup gates.

### October 12, 2026: crosswalk correction and Trello cursor contract tests

Reconciled a conflicting exploratory scan: the earlier two-path result was not
a full crosswalk. In particular, `backend/internal/operations/source_approval.go`
is untracked in the preserved checkout, so a tracked-only scan cannot count or
review it. The PR version already has the stricter evidence-hash and owner-type
binding documented above. A separate expanded-extension scan is not
comparable with the established 653/860 candidate filters and is not used to
replace those historical counts. The exact content-review backlog remains
open; no local data was changed.

The old checkout also has a tracked `nginx-config/nginx.conf` that is absent
from the PR tree. It is superseded by `nginx-config/nginx.conf.template`, which
adds current gateway security headers, dedicated local-runtime routing, and
runtime-injected backend/local-preview secrets; the old static file embeds a
placeholder backend key and lacks those newer controls. `docker-compose.yml`
mounts the template read-only, and `nginx-config/test_gateway_contract.py`
asserts that contract. The old file was not copied.

The PR already contains newer implementations than the preserved checkout for
Trello's quiet-board cursor fast path, cancellable owner-scoped task-agent
context lookup, and avoiding an idle model-maintenance scheduler when no
eligible models exist. The Trello fast path had no direct tests for its
essential behavior, so `backend/internal/source/trello_test.go` now covers no
activity, board-metadata-only cursor advancement, card/unknown/malformed
activity fallback, and saturated-page fallback. The test change was pushed to
the existing PR #36 branch under the user's push/CI authorization. This
environment has no `go` or `gofmt` executable on PATH or in the checked
standard locations; local Go execution and formatting therefore remain
unverified. Git whitespace validation passed before this ledger addition;
exact-head GitHub CI is the required validation gate after push.

PR #36 was rechecked at `c7d191c8ab1f522617957781a68dccd41c559640`, open
against `main`, with `CLEAN` merge state and successful exact-head CI run
`38287984906`. No merge is authorized or performed. All source checkouts and
transcript archives remain untouched; the semantic crosswalk, archive
provenance review, merged-main gate, and local cleanup are still incomplete.

### October 12, 2026: account-feed report revalidation

Rechecked `output/feed-sync-production-review-20261001.md` against the current
account-feed implementation. Its missing/null item-envelope finding is closed:
`ParseGenericFeed` requires an explicit valid `items` array (while preserving
valid explicit empty arrays), and `sync_observation_test.go` asserts malformed
envelopes return no cursor, do not advance successful freshness, and are
recorded as failed. Its overlapping-sync regression is closed by the durable
per-feed sync token: claim is committed before fetch/intake, only the matching
token can finish, and token-conditioned completion plus the accepted source
observation generation prevents stale evidence and freshness from superseding
a newer observation. Deterministic blocked-intake coverage checks overlapping
manual/due runs, independent-feed progress, and post-release latest evidence.

The PostgreSQL integration test uses independent repository calls against an
owned real Postgres database to prove only one concurrent claim wins, stale
finish tokens are refused, finish-audit failure rolls back feed state, and
committed health/audit state survives fresh-pool reconstruction. Exact-head CI
run `38290881474` passed its `Migration integration (real Postgres 17)` job,
which invokes that account-feed integration test, as well as the backend suite.

Two report boundaries remain intentional rather than unresolved defects: a
crashed sync token does not expire or get stolen automatically and requires
operator review; archived/dismissed operations may be re-intaken by a later
whole-feed retry under the documented active-operation dedupe policy. The
successful timestamp means local ingestion completion, not proof of live
provider freshness or complete pagination. This was a source/test crosswalk,
not a live feed-provider acceptance. No product code or local archives were
changed; the broader semantic/provenance review and cleanup gates remain open.

### October 12, 2026: claimed-effect safety report revalidation

Rechecked the five findings in the preserved local-only
`output/claimed-effect-source-head-audit-20261001.md` against current PR source
and regression tests. The report's Runtime Lab confused-deputy finding is
closed in the PR: `Handler.requireConfiguredOwner` rejects missing, malformed,
or foreign subjects before service access; `owner_gate_handler_test.go` checks
every Runtime Lab endpoint, caller-supplied owner aliases, no data leakage,
and no ledger/runtime side effects on rejection. The report's context-free
Constitution lookup is closed: execution authorization passes its context
through `ConstitutionPolicyAdapter`, `EvaluateConstitutionExecutionPolicyContext`,
and `ListConstitutionsContext`; the GORM lookup uses contextual connection
admission. Tests cover context propagation, refusal of context-free repository
fallback, and cancellation while the DB pool is saturated.

The report's mutable mode/block/stop policy race is closed at the current
in-process boundary: `ExecuteSafeOperationClaimed` takes the final-effect fence
before policy recheck and dispatch; control and block-rule mutations take the
exclusive mutation fence. Regression tests cover mode changes during admission
and block-rule serialization. The detached-context scope issue is closed by
retaining and checking the original private authority context/deadline. The
artifact-collision issue is closed by using the full operation UUID in the
managed artifact basename. Source-head publication and final effect share the
origin/head/operation/claim lock order; current tests cover supersession,
observation order, A-B-A recurrence, and missing/unsequenced heads.

These are source and test crosswalk results, not a fresh local Go test run;
this machine still has no Go toolchain in PATH. Exact-head GitHub Actions for
`e88e6d1b56a408afcd5e1f84f106590babcc701b` passed all 36 jobs, including
backend and guarded Windows runtime checks. The guarantee remains bounded:
cancellation cannot undo a filesystem syscall after it begins, so uncertain
outcomes must remain latched and must not be replayed automatically. No
production code was changed for this revalidation. PR #36 remains open and
unmerged; all local source, transcript, and diagnostic material remains
preserved, and the broader content/provenance crosswalk and cleanup gates
remain incomplete.

An independent source recheck on October 12 confirmed these dispositions in
the current PR tree, including the handler owner gate, contextual Constitution
lookups and pool admission, the shared execution-commit/mutation fence, retained
server deadline, full-UUID artifact naming, and source-head generation checks.
This recheck did not run local Go tests; the exact-head Actions run
`38300896714` subsequently passed all 36 jobs on
`4bf3f2e848d9d5ee2a32a37d11ff9150ea1ee80a`. A new documentation-only commit
was then pushed, so that run is not evidence for the newest PR head. Separately,
the `output/` inventory contains 34 Markdown reports, 25 of whose filenames
were not literal matches in this ledger. That is only a navigation signal, not
proof of missing integration; the remaining report/source crosswalk and cleanup
gates stay open.

### October 12, 2026: exact pursuit runtime receipt integration

Revisited the local exact-runtime-receipt candidate against PR source. The
current PR fetched explicit runtime links through the display-capped query,
which can silently omit linked launches when the repository applies its
500-row cap; completion checks could also treat missing or foreign-owner
receipts as absent rather than unresolved. Restored an exact lookup that
deduplicates IDs, reads in batches of 50, filters to the requested owner, and
fails closed if any requested receipt is unavailable. Pursuit detail now merges
all exact linked receipts with its recent-activity result, and both completion
evidence and active-blocker checks use the exact lookup. A runtime launch is
accepted as completion evidence only when its status, exit result, approval
state, and review predicate all pass.

Added regular unit coverage for 51 linked receipts, oldest uncertain state,
foreign ownership, and a missing receipt. Wired the existing PostgreSQL
integration test for those same boundaries into the CI migration job; that
tagged test was previously not run by CI. Exact-head Actions run
`38296662563` passed all 36 jobs on commit
`7a3c344693b0e37686ad9ef985fa470b492dbdb3`; its backend and browser jobs passed,
and its Postgres migration job logged
`TestPostgresPursuitExactRuntimeQueryBatchesAllLinkedReceipts` as PASS. Go and
gofmt remain unavailable in this local checkout. No old-checkout content or
local archive was modified, and semantic source review and cleanup gates remain
open.

Also cross-checked the three findings in
`output/pursuit-outcome-review-second-20261001.md`. For direct task receipts,
the review query includes every `mode=run` row independently of verification
text, then applies the shared Go predicate; terminal validated/completed runs
with empty, NULL, unknown, or whitespace-wrapped uncertainty therefore remain
review blockers instead of being treated as verified. The PostgreSQL regression
`TestPostgresPursuitUnresolvedAttemptQueryIgnoresDisplayLimit` seeds old rows
outside the recent display window, including NULL and tab/newline/non-ASCII
whitespace cases, and asserts owner scope and parity with the Go predicate.
Exact linked runtime receipts use the batched lookup above, so the display cap
cannot authorize completion when an older explicit receipt is unresolved. This
was source/test reconciliation only; the prior hosted exact-head run is the
available execution evidence, not a fresh local PostgreSQL run.

The follow-up documentation-only commit triggered CI run `38297575184` on
`6bf6426e8d8356c629f4f709ce277cad8b5852d5`. Backend, real-Postgres migration,
browser, and two-account jobs passed, but the run concluded with two failures:
the progressive-section unit test observed a stale global
`hai-view-advanced` body class left by another spec, and the repository secret
scanner rejected its captured output as non-JSON and withheld the details. The
section test now removes that global class at setup and teardown so it owns its
fixture state; no runtime behavior was changed. This checkout has no installed
frontend dependencies, so the focused Angular test could not be run locally.
Exact-head Actions run `38298570056` passed all 36 jobs on
`ec56cd9cca4e875717fd2c916c57373067a69145`, including the frontend suite, the
repository secret scan, and real-Postgres migration integration. The earlier
secret-scan parser failure did not recur; its cause remains unknown and the
scanner's fail-closed redaction contract was not weakened. The semantic source
review and cleanup gates remain open.

### October 12, 2026: portfolio and pursuit report source crosswalk

Rechecked the five historical pursuit findings in
`output/background-outcome-production-20261001.md` against the current PR
source and regression tests. Launch completion now validates status by
`LaunchType` (including API 200/204/404, Docker 204/304, and script zero) and
rejects required-but-unapproved receipts. Rejected, revoked, expired, and
unknown required approvals remain review-required even when a separate
verification record exists. Completed workflow task projections derive from
accepted completion, rather than the state label alone. Completion evidence
ignores non-UUID verification labels as proof. Direct-task persistence,
activity, timeline, and review projections share the accepted-verification
boundary; uncertainty, missing execution evidence, blocked reasons, failed
validation, and ungranted approval cannot be projected as verified success.
Focused source tests cover these cases, including accepted historical
verification and pending intent compatibility.

Also rechecked `output/portfolio-postgres-integrity-review-20261001.md`. The
reported PostgreSQL `CHAR(64)` blank-padding defect now has a narrow
canonicalizer that removes only all-ASCII-space absence; present and malformed
nonempty digests remain exact. Both the digest and request-digest paths use the
canonical representation, and tests assert unchanged source values and reject
altered evidence. The report's unresolved-attempt display-cap query has a
separate guarded Postgres test covering stale unresolved attempts, null legacy
verification status, and owner isolation.

These specific regression sources had not been selected by the migration CI
job. Wired the existing guarded PostgreSQL save/reload test for absent and
present coordination digests, and the unresolved-attempt query test, alongside
the exact-runtime receipt test. `git diff --check` is clean; the local checkout
has no Go toolchain or frontend dependencies, so hosted exact-head CI remains
the execution gate. The historical reports' original unexecuted checkpoints
remain historical; this crosswalk does not claim user-database acceptance or
close the broader semantic/provenance review and cleanup gates.

### October 12, 2026: ambient, worker, configuration, and execution-gate crosswalk

Compared the preserved older checkout's ambient scheduler/service and adjacent
configuration, health, background-worker, durable-job, execution-authorization,
safe-worker, approval-resolver, and phase-two paths with the current PR tree.
No code from this batch was copied because the local variants remove or weaken
current safeguards:

- Ambient scheduling loses durable scheduler status reporting and the second
  explicit opt-in required for a legacy in-process fallback. The local
  opportunity path replaces the batched fingerprint lookup with per-item
  reads, removes the nil-service guard, and changes the needs lookup. The
  older five-minute worker-poll fallback is not adopted: the tracked
  environment template already sets the poll to 300 seconds, and no measured
  runtime benefit justifies changing the fallback independently.
- Configuration changes allow port zero and delete the regression asserting
  that ephemeral/out-of-range values are invalid. The local health test variant
  deletes serving-pool, noncritical scheduler-status, and Redis fallback
  diagnostics. Its database-password branch also drops the explicit production
  failure for the default `postgres` password; the PR keeps these checks.
- The local background worker removes paused-mode handling and safe deferral,
  writes raw error details into reports instead of redacting secrets, and
  deletes regressions for secret leakage and approval-gated handling. Its
  altered reliability expectation treats a fresh operation as verified rather
  than requiring it to remain awaiting approval.
- The local durable-job integration test uses an unscoped lease reaper and
  deletes the regression proving recovery in one queue leaves another queue's
  lease untouched. The ambient-monitor test variant also removes the JSONB
  canonical cursor-key quoting regression.
- The local execution-authority and phase-two variants drop exact safety-control
  approval resolution, reserved resolver-prefix checks, request validation,
  source-approval validation/consumption, and final-effect policy checks. The
  safe worker raises the artifact-size bound from 8 KiB to 64 KiB. These
  changes would weaken authority and execution constraints; the PR versions
  remain canonical.

This was a targeted semantic comparison of the named files, not completion of
the full stale-checkout or transcript audit. No local source, archive, output,
diagnostic, environment, database, or worktree was changed or removed. The
current PR head `6a2aa8fdae802d83aa854f59d880952de25c2287` passed exact-head
GitHub Actions run `38299612642` with all 36 jobs successful, including the
real-Postgres migration suite. PR #36 remains open and unmerged; the wider
content/provenance crosswalk and local cleanup gates remain incomplete.

### October 12, 2026: dirty-checkout CI workflow crosswalk

Compared the preserved checkout's 786-addition/103-deletion change to
`.github/workflows/ci.yml` with the current PR workflow. The PR already contains
the common runner matrix, redacted secret scan, guarded Windows installer and
native runtime checks, Redis/PostgreSQL IDP regressions, strict frontend checks,
owned browser stack, provider fixture, Promptfoo runner, authenticated smoke,
real-Postgres migration integration, and two-account isolation gates. The older
checkout's workflow omits four current PR safeguards: the detached-volume
cleanup-readiness contracts, A2A bridge race tests, Docling runner boundary
tests, and live-Trello acceptance launcher contract. Those omissions would
reduce coverage, so no workflow code was copied.

This is a source-level contract comparison; exact workflow execution evidence
for the current PR head `a1a54445bca5dfcd6c5a1e72660674ad17ab574a` is Actions run
`38301996815`, which passed all 36 jobs. It does not establish that the older
checkout's 3,167 dirty/untracked paths have all been semantically reviewed.
The primary dirty checkout, its untracked files, transcript archives, and local
runtime data remain unchanged. The full semantic/provenance audit and cleanup
gates remain open.

### October 12, 2026: giant compacted-transcript record classification

Re-read the two previously oversized transcript records with fixed-buffer
streaming. Their exact record lengths are 1,016,062,444 bytes and 904,740,623
bytes; SHA-256 values are `4417a8e6efac27578b30609fcd53fb5bfeed662c86cbe883c64ed886eeeb879d`
and `621b40c6ac6df8a5b248266e56bf913267b83ca1bb4be10dbd9272e6a7a27606`,
respectively. Their top-level type field is `compacted` (confirmed from the
record prefix). A streaming marker scan found no `patch_apply_end`,
`event_msg`, `changes`, or patch-array markers in either record, so neither is
an unscanned source-patch event.

This closes only their source-patch-event classification. The giant compacted
message contents have not been fully semantically read, and the other
transcript records still require provenance and unique-finding reconciliation.
Both archive files remain intact and protected; this result does not make them
eligible for deletion.

### October 12, 2026: progressive shell, dashboard, and safety-runner crosswalk

The same source comparison confirmed the current PR's frontend application
module wires the shared Control Room module and centralized icon registry,
while the preserved checkout still carries a separate inline icon list. The
PR's Pursuits view intentionally leaves the detail pane unselected until the
user chooses a record, rather than silently opening the first result. The
Control Center requests summary-sized ambient and pursuit payloads and rejects
malformed dashboard counts before rendering; the older component requests the
larger overview/dashboard payload and derives counts from arrays. These
changes align with the progressive disclosure and payload-reduction design;
no frontend code was copied.

For the safety runner, the PR installs the Promptfoo production lock with
optional packages omitted, while keeping install scripts disabled and the
lockfile audit enabled. The compared Dockerfile variant does not omit optional
packages. The existing CI job builds and exercises the runner image and audits
production dependencies. The older lockfile delta was not adopted because it
does not by itself establish a safer or compatible dependency graph; the PR's
lock and package manifest remain canonical. These source and workflow checks
do not replace fresh local browser or provider acceptance. The broader
semantic/provenance audit and cleanup gates remain open.

### October 12, 2026: agent runtime and coordination source crosswalk

Compared the legacy checkout's complete `backend/internal/agentruntime`
candidate set with the PR source after normalizing CRLF/LF. The candidate
`runtime.go`, `runtime_test.go`, DeepSeek harness and authorization files,
execution admission, OpenClaw artifact/Gateway/maintenance/archive/usage
modules, and their focused safety tests match the PR exactly. The PR's
`handler.go` adds request-cancellation checks after archive inspection and
before persistence; its tests add cancelled-upload and oversized-request
regressions. The legacy versions therefore must not replace the PR files.

The runtime directory comparison is source parity, not a new independent
security certification. In particular, host execution remains governed by the
PR's explicit admission and provider/runtime gates. Separately compared the
legacy dirty `agentcoordination`, `agentframework`, and `agentguidance` paths;
their existing files match the PR after newline normalization, and no port is
needed. No source code was copied. The old checkout and its untracked data
remain untouched; the full content/provenance audit and cleanup gates remain
open.

### October 12, 2026: task history, memory query, and Trello efficiency crosswalk

Compared the legacy variants of the task-history handler, memory handler, and
Trello source connector against the current PR implementation. The PR bounds
task-history requests to 1-50 entries (default 10), returns a compact list
projection, and exposes full records only by explicit request while retaining
owner-scoped durable reads. Its memory handler uses the PostgreSQL owner-query
capability when available, keeps the compatibility fallback owner-scoped, and
returns a service-unavailable response rather than panicking when no scoped
service exists. The repository query applies owner/project/archive predicates,
literal escaped AND-token search, stable ordering, and pagination in the
database; focused PostgreSQL tests cover isolation, literal wildcard search,
pagination, and cancellation.

The Trello connector checks a bounded actions page before fetching every card
and comment. It skips the expensive reconciliation only when the page proves
there was no card-level activity or only recognized board-metadata changes;
saturated, malformed, unknown, or card-affecting activity falls back to the
normal bounded reconciliation. Focused tests exercise those fallback cases.
These are PR-side implementations and test evidence, not fresh local test
execution. The compared legacy variants omit these controls; no source was
copied. The broader semantic/provenance audit, transcript review, and cleanup
gates remain open.

### October 12, 2026: post-merge repository and Autofever-branch disposition

Live GitHub CLI verification confirms PR #36 is `MERGED` into `main` in
`Robert-Velhorst/018-HAI` at squash commit
`c31cec1ba2a25fcfd280b1f2f499297e54d96aa4` (merged 2026-10-12 20:42 UTC).
This supersedes earlier checkpoints in this ledger that described PR #36 as
open. The canonical remote `main` currently resolves to that same commit.

The cleanup-readiness verifier still tests for GitHub PR state `CLOSED`, even
though the GitHub API reports merged pull requests as `MERGED`. That prevents
the verifier from recognizing the successful PR #36 merge. PR #37
(`codex/fix-hai-transcript-merged-state`) changes the gate to require `MERGED`
and adds a contract test rejecting the obsolete state check. PR #37 is open
and cleanly mergeable at `9ec3a326ecac663ed74732e60db5e0ed45e3f991`; its
exact-head CI passed all 36 checks. No merge of PR #37 or transcript cleanup
has been performed in this checkpoint.

Also reviewed the local-only `codex/autofever-connector` commit
`7eed157b5c740e6909eb560beaf313bd1d4cb619`. It adds a bearer-token adapter for
`/api/integrations/hai/feed`, but its tests exercise only a synthetic
`httptest` server. Current HAI `main` and `docs/autofever-connector.md` state
that the sibling Autofever app has no such endpoint, vehicle-review model, or
owner-scoped contract; the connector is intentionally disabled. The old
adapter is therefore not a verified integration and was not ported. Keep the
branch and its worktree until the broader local-source audit is complete; an
upstream API contract and authorized live acceptance are prerequisites to
re-enabling it.

These checks update repository and candidate status only. The full transcript
semantic/provenance reconciliation, exact-main cleanup gate, and local cleanup
remain incomplete. All archives, source checkouts, and branch worktrees remain
unchanged.

### October 13, 2026: current main and local-source disposition

The previous section records the October 12 checkpoint and its then-current
PR #37 status. Live GitHub CLI and `git ls-remote` checks on October 13
supersede that status: PR #37 merged at `daee223cd41f0a7847b80ab9533e7fc175023115`
at 2026-10-12 21:09:28 UTC; PR #38 merged at
`76477e5a313e13fd5bcfc4e4e341bdc1e3038fe8` at 21:20:49 UTC; and PR #39
merged at `3b15fe4a08b10bb2e15afe15640bd6488cebff5d` at 21:27:33 UTC.
Remote `main` and the clean comparison worktree both resolve to the PR #39
merge commit. No open pull requests were returned by GitHub CLI at the audit
time.

A read-only inventory enumerated 17 registered HAI worktrees. In the primary
dirty checkout, 815 ordinary untracked source/documentation files (7,719,027
logical bytes) matched files on `main` after normalizing Windows line endings.
The primary checkout also contained 74 untracked non-log/evidence paths that
differed from the corresponding `main` files; none was established as a safe
new production implementation. Among modified tracked files, many local
copies matched `main`, while the reviewed differences were older variants or
removed controls. The byte count is not a claim about physical disk blocks
reclaimed.

Independent backend, frontend/IDP, and documentation/runtime reviews found no
independently safe, unique source change cleared for porting. Examples of
rejected differences include removal of execution approval and source-receipt
bindings, loss of context cancellation and transactional event-outbox paths,
weaker nginx security headers, removal of cleanup preconditions, and a
Promptfoo version/security-override downgrade. These findings are targeted
examples, not a line-by-line approval of every local diff. No local source was
copied, staged, committed, or deleted during this audit, and no tests or builds
were run.

The remote `codex/hai-runtime-release` head was also compared with `main`.
Although its PR #36 is merged, its later tree delta still changes eight files
and removes 286 lines while adding 9. It weakens the merged-PR cleanup gate
and removes other cleanup safeguards, so it is not a safe branch to reopen as
a PR. PR #32 through #39 remain the historical merge records; their merged
code is represented by current `main`.

The source comparison is not a complete semantic/provenance review of every
archive, ignored file, runtime database, or local artifact. The transcript
archive review and cleanup remain incomplete. No local files were removed;
the attempted duplicate-file cleanup was blocked by the execution policy.
Keep source/worktree data intact until a permitted, verified cleanup can be
performed after preservation requirements are met.

### October 13, 2026: PR #40 and post-checkpoint preservation audit

Live `gh` verification confirms PR #40, `fix: validate verification source
snapshot linkage`, remains open and mergeable against `main` at
`3b15fe4a08b10bb2e15afe15640bd6488cebff5d`. Its head is
`3a1a22e49629cfb9307e7e4fc5ee5e4c6634d92e`; all 36 checks on that exact head
passed, including backend/frontend/IDP tests, Postgres migration integration,
browser acceptance, Windows installer guards, secret scan, and cleanup safety
contracts. The PR contains the source-snapshot verification hardening and this
ledger's reconciliation updates; it does not contain the primary checkout's
unreviewed working-tree changes.

A refreshed read-only inventory of the primary checkout found 692 modified
tracked paths and 2,475 untracked paths. Its branch `HEAD` is 292 commits ahead
and 9 behind `origin/main`; the committed tree differs from `origin/main` in
503 paths (28,323 insertions and 6,216 deletions). This divergence plus
unreviewed working-tree edits makes a wholesale push unsafe. Of the scoped,
ordinary untracked source/document files, 815 still match `main` exactly and
total 7,719,027 logical bytes. The prior cleanup attempt was rejected before
execution; a follow-up read-only check confirmed all 815 remain present and
unchanged. No fallback deletion mechanism was used.

The checkout also has 1,576 files under `output/`, occupying 126,970,802
logical bytes. These are local test/output artifacts and have not been
included in a PR or deleted. Their contents require separate classification
before any preservation or cleanup decision. The remaining implementation
audit is in progress; no claim is made that every branch commit or local
change has been reviewed or integrated.

Independent review of the frontend/IDP and operations slices found additional
reasons not to copy the local variants as-is. The frontend shell drops the
route-based document-title update present on `main`. Local IDP handlers use
unchecked request-context UUID assertions; several auth/reset paths no longer
propagate request cancellation; and password-reset digest validation accepts
non-hex 64-character strings. These need regression tests if a future port
reintroduces the local implementations; the current `main` versions retain
the stronger guards.

The local runner/CI additions are not a self-contained patch against this
checkout's `HEAD`: the workflow depends on lockfiles and scripts that are
untracked here but already exist on `main`. Runner payloads validate the
skill-guidance hash and consent ID format but need explicit backend-to-trusted
catalog/consent binding and tampering tests. Python runner dependency license
coverage also remains unverified. Review did confirm loopback gateway binds
and several `no-new-privileges` settings in the inspected Compose config; this
is not a full runtime acceptance result.

These reviews did not clear any of the dirty source variants for immediate
porting. They were bounded reviews, not line-by-line approval of the 503
committed-tree differences or 692 tracked working-tree edits. The backend
review did not complete within its bounded audit window; no backend-wide
clearance is claimed. PR #40 ledger commits `95b7fda89f4cae3ef5dc0a0c649373de053743f9`,
`a96664d6a7685b5060011044bdd97cf64e74151b`, and
`495dd0c1d6f1a4e0824689b24c9a8d616098bb60` are all pushed. Each exact-head CI
run passed all 36 checks; the current open PR head is `495dd0c1`.

### October 13, 2026: linked-worktree and runtime-safety audit

`git worktree list --porcelain` reports 17 linked HAI worktrees. The canonical
`main` worktree is clean at `3b15fe4a08b10bb2e15afe15640bd6488cebff5d`; PR
#40's worktree is clean at `a96664d6a7685b5060011044bdd97cf64e74151b`. Eleven
other worktrees have dirty files. The primary checkout has 692 tracked edits
and 2,475 untracked paths; the model-maintenance, OpenClaw-artifact, and
runtime-safety worktrees have respectively 425/345, 150/255, and 14/25 tracked
edits/untracked paths. Windows-installer has 1/2; PR34 conflict-fix 528/0;
PR36 integration 521/0; PR36 local-delta 59/0; PR36 resolution 264/0;
PR34 resolution 516/2; and PR35 resolution 216/0. Per-worktree counts overlap
and are not unique-file totals. No worktree was removed. Most dirty worktrees
are based on older commits; their contents must be reconciled individually.

Focused reviews of the model-maintenance and runtime-safety worktrees found no
local changes cleared for direct porting. They identified regressions
relative to `main`: account deletion no longer revokes sessions; user-context
UUID assertions can panic; reset digests accept non-hex strings; runtime stop
receipts lose task/owner/execution identity validation; the host start
intent/acknowledgement and approval-digest protocol is absent; artifact URL
validation weakens loopback/NAT64 protections; ZIP ratio multiplication can
overflow; workspace link checks and upload durability/recovery are reduced.
Production host execution remains fail-closed/disabled. The OpenClaw-artifact
review likewise found weaker migration rollback guards and stale-claim upsert
fencing; no local OpenClaw variant was safe to copy over current `main`.

Credential review found `.env-backend`, `.env-gateway`, and `.env-idp` in the
older local branch history. A live `git cat-file` check confirms none is
present in `origin/main` or PR #40; `.env.example` is the only one of these
paths on `main`. The credential-shaped local files were not opened or copied.
The remote `codex/ambient-composition-control` branch also contains these
`.env-*` files, and the repository is public. A redacted field check found
short DB password/JWT-secret values matching common development-placeholder
patterns, but did not establish that they are safe. GitHub secret scanning is
disabled on this repository, and no local `gitleaks`, `trufflehog`, or
`detect-secrets` executable is available. No values were printed. If any of
these values were ever live credentials, rotate them; do not copy these files
into a PR. The ignored local `.env.ollama.local` exists in the ambient
worktree at 38,215 bytes; its contents were not read or uploaded.

The runtime and OpenClaw audits were focused static reviews, not full
line-by-line reviews or runtime tests. The backend-wide primary-checkout audit
did not complete. Generated output, agent logs, browser diagnostic data, and
remaining worktree contents are not classified. Immediately before this note,
PR #40 was at `a96664d6a7685b5060011044bdd97cf64e74151b`; that exact head's
36-check CI run passed. This worktree/security note was subsequently pushed as
`495dd0c1d6f1a4e0824689b24c9a8d616098bb60`, whose exact-head CI also passed
all 36 checks.

### October 13, 2026: merged-PR worktrees and public-branch review

Live GitHub checks confirm PR #34 merged at `4f24b449`, PR #35 at `7efe1183`,
and PR #36 at `c31cec1b`. Their installer, model-directed logs, runtime, and
release changes are already represented in `main`; no re-port is needed.
Three PR35/36 integration snapshots remain conflict-heavy: `hai-pr36-integration`
has 256 unmerged paths, `hai-pr36-local-delta` has 59, and `hai-resolve-pr35`
has 199. `hai-pr36-resolution` has no index conflicts but its additions are
the unverified Autofever adapter; do not treat a simulated feed as a live
integration. Its outbox files match `main`.

The `codex/ambient-composition-control`, `codex/hai-post-pr36-audit`, and
`codex/fix-hai-transcript-merged-state` branch heads are still present remotely
at `7fabfe75`, `38b52d3c`, and `9ec3a326`. Commit `57e46c55` is reachable from
the remote Windows-installer branch. These snapshots are clean in Git but have
tree differences from `main`; clean status does not account for ignored local
configuration, dependencies, diagnostics, releases, or build output. Focused
reviews found no unique code safe to port as-is. Older Trello sync and dashboard
payload ideas remain candidates for isolated reimplementation with current
pagination, reconciliation, and safety contracts.

The Windows-installer worktree has two untracked backend executables totaling
166,467,072 bytes. They are unsigned and have August 22 timestamps; the local
Go CLI is unavailable, so their precise source/build provenance and
reproducibility were not verified. They were not pushed or removed. No worktree,
binary, ignored configuration, or other local data was deleted in this audit.
The local deletion operation remains blocked by execution policy; no alternate
deletion path was used.

### October 13, 2026: primary-checkout backend regression audit

Four bounded read-only audits compared the remaining dirty primary-checkout
backend areas with live `origin/main` (`3b15fe4a`) and PR #40 (`f65ad3ba`).
They found no divergent production change cleared for porting as-is. Many
untracked files are exact copies of files already on `main`; exact path/content
matches are not unique implementation work.

The reviewed local variants contain material regressions relative to `main`,
including weakened owner scoping for legacy pursuits; non-durable scheduler
fallback; automation writes without a durable outbox; missing cancellation
propagation; weakened approval/source-evidence revision binding and receipt
handling; dropped approval/review endpoints; public readiness detail exposure;
changed migration gates and migration-owned schema handling; lost framework
preference audit history; unbounded memory/verification reads; weaker provider
URL validation; omitted account-grant verification; and cancelled uploads that
may continue. Review also identified weakened stop-recovery invariants, dropped
source-search cancellation/error escalation, loss of several task-engine model
policies, and inconsistent approval queue filtering. These are audit findings,
not claims that each path was exercised at runtime; do not port the local
variants to replace `main`.

One focused execution review did not establish that the observed stop-recovery
change bypasses the separate global environment-stop checks. Likewise, the
phase-2 source-approval precheck removal was not shown to bypass the independent
receipt validation at the background execution boundary. These are bounded
negative findings, not a complete safety certification.

No tests were run for these local variants. The audits did not cover every
changed file, every backend package, ignored files, all linked worktrees, or
live provider/runtime behavior. In particular, the primary backend review
explicitly left numerous packages and adjacent integrations unaudited. The
exact-match cleanup candidate set was separately measured at 815 untracked
source/documentation files (7,719,027 logical bytes) in the primary checkout;
the deletion command was rejected before execution, so no local data was
removed. Larger output trees, ignored data, and unsigned executables remain
unclassified and must not be deleted on the basis of this audit.

At this note, PR #40 remains open and based on `main`. Exact-head CI for
`f65ad3ba` passed all observed jobs except the `whispercpp-runner` job, which
was still in progress; do not record the head as fully green until the check
finishes.

### October 13, 2026: refreshed archive integrity and August source-path mismatch

Read-only cleanup verification was rerun from a clean checkout of canonical
`main` at `3b15fe4a`, which matched live `origin/main`. The July 30 archive
verifier checked all 18 manifest paths and 20,739,169,122 logical bytes; it
verified five candidate hashes totaling 3,179,439,595 bytes and retained 13
files. It reported `cleanup_gate_ready=true`, but `cleanup_authorized=false` and
`deletion_performed=false`. The supplemental verifier checked all nine source
files across the two supplemental roots and 14,013,737,298 logical bytes; both
candidate hashes matched (3,113,294,358 bytes) and seven files remain retained.
It likewise performed no deletion and reported `cleanup_authorized=false`.
These are distinct July 30 archive sets; do not add their counts to the August
manifest.

The August manifest has 114 entries (10 for August 1, 5 for August 3, 48 for
August 4, 49 for August 5, and 2 for August 8). A fresh metadata comparison
against its recorded source root, `C:\Users\NO\.codex\sessions\2026`, found
104 entries missing at their manifest paths and all ten present entries with
file lengths different from the manifest. Specifically, the 48 August 4
entries are absent; 40 of 49 August 5 entries are absent and the nine present
files differ in size; one August 8 entry is absent and the other differs in
size; August 1 and August 3 entries are absent. This check did not hash the
present mismatched files and does not establish whether missing items were
moved, compacted, or otherwise preserved.

The current `C:\Users\NO\.codex\sessions\2026\08` directory contains 113
JSONL files totaling 19,395,151,209 logical bytes, but it is not a verified
match for the August manifest. Targeted filename searches for a sample missing
August ID found no match in `.codex`, `D:\codex-temp`, `D:\CodexTemp`,
`D:\CodexTempBackup`, `D:\Temp`, `D:\Codex-evidence`, Downloads, or Desktop.
The profile-wide search was stopped before completion to avoid an unbounded
resource scan. Therefore the previously reported 222.485 GiB completed August
4-5 set is a historical manifest value, not a verified current removable set.
Do not delete or regenerate any August source from these observations. Locate
and reconcile the original files, or document their authoritative preservation
elsewhere, before closing that archive's integration or cleanup gate.

The new PR #40 audit note is at `fcc0f71c5b605502b1e1cbd5fff5f1e78935d8bb`;
its exact-head GitHub Actions run `38324315532` completed successfully. The
environment rejected the requested local deletion operation before execution;
no alternate deletion mechanism was used. No archive, worktree, or local file
was removed.

## 2026-10-13 local checkout comparison

A fresh read-only comparison was run from the primary HAI checkout at
`codex/hai-runtime-release` (`e07b9dae`) against current `origin/main`
(`3b15fe4a`) and the checkout's remote feature branch. The local feature
checkout is 440 commits behind `origin/codex/hai-runtime-release`; it has 692
unstaged tracked modifications and 2,475 untracked paths. It must not be
treated as a current branch snapshot or copied wholesale into a PR.

For the 692 modified tracked files, comparing Git blob IDs against current
`origin/main` found 351 already identical, 340 different, and one path absent
from `main`. For the 2,475 untracked, non-ignored paths, 852 have a same-path,
byte-identical blob in `main` (2,230,351 filesystem bytes total), 114 share a
path but differ in content, and 1,509 have no same path in `main`. These are
identity comparisons only; the non-matching files were not individually
reviewed for correctness or relevance. The exact-match subset is only about
2.13 MiB using a filesystem character-count estimate. A fresh byte-accurate
Git blob scan on 2026-10-13 against `origin/main` `3b15fe4a08b10bb2e15afe15640bd6488cebff5d`
recounted the 351 exact tracked paths as 5,628,406 bytes and the 852 exact
untracked paths as 7,956,591 bytes: 13,584,997 bytes total (about 12.95 MiB).
The earlier 2,230,351-byte figure was not a byte-accurate measurement and is
superseded. This is the maximum content-identical candidate set from those
status groups, not proof that removing tracked files would leave this stale
checkout usable. A direct local deletion attempt was blocked by the environment;
no local files were removed and no alternate deletion method was used.

At the same recheck, PR #40 remained open at head
`4066f46ae6ff972a230bc92be715b799370f93ea`, with 19 checks successful, 8 in
progress, and 9 queued. GitHub reported the merge state as `UNSTABLE`; these
checks are not yet a completed integration gate.

Additional bounded source/test spot checks found no further local-only change
cleared for import. The local `source_approval.go` removes the reviewed owner
type from approval receipts and narrows revision matching; the local task
execution-context helper drops the private context fallback; and the local
database-pool file removes the configured parameterized-query logger. The
local Trello reopen test replaces action-history responses with an empty list,
and the local readiness test removes verified agent-card evidence. Those
variants reduce binding, cancellation, privacy, or regression-test coverage
relative to `main`; they were not ported. These checks are examples only, not
a complete semantic review of all 340 differing tracked paths or 114
same-path/different-content untracked paths.

A byte-accurate root-level grouping of the same current status snapshot found
the 340 tracked divergences in frontend (156 paths), backend (129), IDP (17),
docs (14), scripts (8), installer (5), and seven other root/config areas. The
114 different untracked paths are backend (33), scripts (20), IDP (10),
frontend (6), services (4), docs (1), and output evidence (40). Of the 1,509
untracked paths with no main-tree path, 1,493 are under `output/`; the ten
backend paths are outcome-evaluation test evidence logs/JSON, with the
remaining six in agent-state directories or root logs. This narrows the
unreviewed product-source inventory but does not replace semantic review of
the 340 tracked or 114 same-path/different-content paths.

### Deployment/configuration spot check

Comparison of local deployment files with the same `main` commit also found no
safe config to port as-is. The local Nginx template removes the Content
Security Policy and other response security headers, changes the frontend
upstream from the canonical container port, and drops the extended API proxy
timeout used by long-running task execution. The local backend and IDP Docker
build files and Windows installer script downgrade the pinned Go builder from
the current `main` version. The local `.env.example` also removes current
controls/documentation for legacy data ownership, private-source HTTP
destinations, Trello board connector configuration, and durable-scheduler
fallback. These are configuration-level comparisons; no live deployment was
changed or tested, and the local variants were not ported.

### IDP authentication spot check

The local `idp/internal/app/authentication/auth_service.go` also uses older
context-free signatures for logout, refresh, authentication checks, and
password-reset requests. Canonical `main` accepts request contexts and checks
cancellation before session work; local tests were changed to call the
context-free API. Replacing the canonical IDP implementation with this local
variant would reduce cancellation propagation. The local password-reset and
public-auth rate-limit source and tests are byte-identical to `main`, so those
untracked copies are duplicates rather than unintegrated functionality. No
local IDP code was ported from this sample.

### Verification repository spot check

The local `backend/internal/verification/repository.go` removes the canonical
200-run history limit and the narrow opt-in legacy-owner query in favor of
unbounded exact-owner reads. The canonical repository tests explicitly assert
the bounded history SQL and the designated legacy-owner boundary. Although
the local exact-owner query is stricter for ownerless records, importing it
would also remove the reviewed migration path and allow unbounded history
loads; preserve the canonical implementation unless those behaviors are
deliberately redesigned with pagination and migration coverage.

### CI and migration-contract spot check

The local `.github/workflows/ci.yml` removes cleanup-readiness and transcript
safety contract invocations and pins Windows installer CI to Go `1.25.13`
instead of canonical `1.27.2`. The local migration-chain contract removes the
expected tail migrations `0116` through `0127`. The local `scripts/test_ci_contract.py`
also removes assertions for startup-log secret redaction, optional production
dependency auditing, and the frontend's listening health endpoint. These
changes weaken release, cleanup, migration-order, and secret-handling gates;
none were ported.

At the time of this comparison, PR #40 was open against `main`, mergeable, and
headed at `e0670e870ccf381bbd5bb298adc67dc4dc1918f1`. That checkpoint contained
three files (the verification-service fix, its regression test, and this
ledger); all 36 checks for that exact head reported success. This inventory was
then pushed as `1fa6e72532b40511575046d46c4728d89ad2fc01`; its checks were still
running when observed (11 successful, 11 in progress, 14 queued). The GitHub
connector required reauthentication during this audit, so PR metadata was
independently checked through GitHub's public REST API. This does not establish
that the remaining local modifications are safe or ready to publish.

## 2026-10-13 local frontend validation

On the same primary checkout snapshot (`codex/hai-runtime-release`,
`e07b9dae`), `npm.cmd test -- --watch=false` completed successfully: 174 focused
Node tests and 1,325 Chrome/Karma tests passed. A production build initially
stopped at the repository's runtime guard because the shell default was Node
25.2.1 while the project requires Node `^24.15.0`. Rerunning with the bundled
Node 24.19.0 runtime passed and generated the production bundle (908.88 kB
initial raw total). It emitted four SCSS component-budget warnings: workflow
engine, app shell, task blueprint, and pursuits; pursuits was 24.15 kB over its
20 kB budget. These results validate local compilation and tests only; they do
not establish that the divergent local frontend should be copied to `main`.

## 2026-10-13 fresh archive verification and workspace footprint

Both committed readiness verifiers were rerun from the clean canonical `main`
checkout at `3b15fe4a`; its tracked and untracked worktree remained clean.
The primary July archive still contains 18 files and 20,739,169,122 logical
bytes. Five candidates (3,179,439,595 bytes) passed source-hash and ID-crosswalk
checks; 13 files remain retained. The committed-ledger and PR #36 merged gates
passed, and the script reported `cleanup_gate_ready=true`,
`cleanup_authorized=false`, and `deletion_performed=false`.

The distinct supplemental roots still contain nine files and 14,013,737,298
logical bytes. Two candidates (3,113,294,358 bytes) passed hash verification;
seven remain retained. Its verifier likewise reported
`cleanup_authorized=false` and `deletion_performed=false`. Together, the
verified candidate sets total 6,292,733,953 bytes (about 5.86 GiB), but neither
verifier performed cleanup.

The August directory `C:\Users\NO\.codex\sessions\2026\08` currently contains
113 files totaling 19,395,151,209 bytes recursively. It still does not match
the historical 114-row manifest: prior metadata checks found 104 listed paths
missing and ten present files with different sizes. No content hashes were
checked for those mismatches, so do not infer that the remaining 19.4 GB is
fully integrated or removable.

The primary checkout footprint was also measured: `frontend/dist` 387,064,530
bytes, `frontend/node_modules` 280,611,707 bytes, `output` 126,970,802 bytes
(1,576 files), and `.git` 127,144,128 bytes. `frontend/dist` was generated by
the successful production build recorded above; service dependencies on these
paths were not audited here. No local files or archives were removed.

## Frontend parity audit: regressions in the stale local snapshot

Comparison of the local `e07b9dae` frontend against canonical `main` found
changes that should not be imported as-is:

- The local route table and module registry remove `/operational-brain`, while
  canonical `main` still registers the route and its three progressive
  sections. The local registry test also removes that route's expected IDs.
- The local workflow-engine template and registry remove
  `intake-outcome-criteria`; canonical `main` still renders and registers it.
- The local framework-registry template and registry remove
  `framework-preference-history`; canonical `main` still renders and registers
  it.
- The local app shell removes `Title` injection and its per-module document
  title update, leaving the browser title stale during navigation.
- The local app shell now always focuses a navigation entry after opening the
  mobile navigation. It also deletes the existing test that verifies focus is
  not stolen after the user moves it before the scheduled callback.

The local suite's green result must be interpreted with these test edits in
view: the local registry expectations were reduced and the focus regression
test was deleted. These are concrete reasons not to copy the divergent local
frontend wholesale. The canonical `main` versions retain the routes, sections,
document-title behavior, and focus regression test; no fix was needed on `main`
for these findings.

## Local backend test boundary

`go test -p 1 ./...` was attempted from the primary local checkout but did not
start: no Go executable is installed in the checked standard locations or on
the shell `PATH`. No local backend or IDP Go tests are claimed for the divergent
snapshot. PR #40's remote CI does not validate those unported local edits.

## 2026-10-13 branch and cleanup boundary refresh

The primary checkout still points at `codex/hai-runtime-release` commit
`e07b9dae`, while the remote `main` ref is `3b15fe4a`. The local Git graph
reports nine commits on `main` and 292 on the stale local branch since their
merge base; this is not evidence that 292 changes are missing. PR #36 from the
same remote branch was already merged at `c31cec1` from head `e62b5684`, so
history may have been squash-integrated. A direct tree comparison between the
current local HEAD and `main` reports 2,071 path changes (4 additions, 1,189
deletions, 876 modifications, and 2 exact renames), with 393,575 removed lines.
Do not bulk-push or remove this checkout based on ancestry or path counts;
reconcile file content against current `main` first. The ordinary `git fetch`
refresh was denied because this checkout could not open `.git/FETCH_HEAD`; no
permission or filesystem workaround was attempted.

The exact-blob inventory of the dirty portion remains a separate result: 351
modified tracked files and 852 untracked files match current `main` exactly,
totalling 13,584,997 bytes. This is only about 13 MiB of recoverable checkout
content and is not the larger transcript-archive inventory. No local files were
deleted. The cleanup-readiness verifier previously reported
`cleanup_authorized=false` and `deletion_performed=false`; user approval does
not change that verifier result or establish that unmatched material is
published. Keep unmatched archives and working-tree data until the applicable
cleanup gate and content-level integration checks pass.

### Service dependency batch: `services/`

Compared every currently untracked, non-ignored service path against
`origin/main` by Git blob ID. Twenty-nine files are byte-identical and already
exist on `main`. The four differing paths are the DeepTeam
`requirements.in`/lock pair and Promptfoo `package.json`/lock pair. The local
DeepTeam input removes `sentry-sdk` from the canonical requirements; its
runtime/telemetry effect is not established, so retain the canonical dependency
until that behavior is tested. The local Promptfoo manifest and lock resolve
`promptfoo` `0.123.1`, while `main` has `0.124.1`, the `@libsql/linux-x64-gnu`
`0.5.29` dependency, and a `get-uri` override pinning `basic-ftp` to `6.2.1`.
The local pair is therefore not an update and must not replace `main`. No
service file from this batch needs porting.

### IDP untracked authentication tests

Compared the 38 currently untracked IDP paths against `origin/main` by Git
blob ID: 28 are exact matches and ten differ. The ten differences are
authentication tests, not new production implementations. Their changes remove
request contexts from reset, refresh, logout, and authentication calls, matching
the stale local context-free service API; the canonical `main` service and its
tests propagate `context.Context` through those operations. The test diffs do
not add coverage that can be ported independently without first restoring the
canonical signatures, and several are broad call-site rewrites only. Keep the
`main` tests and context-aware API; do not copy these stale variants.

### IDP tracked authentication/configuration deltas

Compared all 17 content-different tracked IDP paths from the local status
snapshot with `origin/main`. The context-free API changes also remove the
canonical canceled-context regression test and its SMTP cancellation test, and
delete owner-scope claim generation/tests. Other production differences
weaken reset-token persistence validation (`IsPasswordResetTokenDigest` is
replaced by a length-only check), restore unchecked Gin-context type
assertions and a possible nil-user dereference, and replace configurable
database pool settings with hard-coded values. The IDP Dockerfile downgrades
the Go builder from `1.27.2` to `1.25.13`. Kafka configuration also removes
the optional broker compatibility setting; this behavior was not separately
qualified. These changes either remove proven safety/error handling or change
runtime configuration without acceptance evidence. No IDP delta from this
local snapshot is eligible for import as-is.

### Backend untracked path batch

Compared all 670 untracked backend paths with `origin/main` using Git blob
IDs: 627 are byte-identical, 33 are same-path content variants, and ten
outcome-evaluation PostgreSQL/test-evidence logs have no main-tree path. The
different source/test variants include operation-claim and source-approval
logic, task context propagation, worker reliability, scheduler state,
verification freshness, pursuit receipt binding, emergency-stop request
validation, and migration rollback contracts. Review against canonical tests
shows the local variants drop or weaken exact revision/receipt binding,
approval-required routing, cancellation, error-detail redaction, or rollback
protection. Six migration-test variants also omit later raw-evidence migration
and phase-order/data-protection assertions present on `main`. The ten logs are
test evidence, not product source, and remain local pending the separate
archive cleanup gate. No backend path from this batch is eligible for import
as-is; the existing verification-service fix in PR #40 is the only backend
source change currently selected for integration.

### Untracked script/test batch: `scripts/`

Compared all 39 untracked script paths against `origin/main` by Git blob ID:
19 already match; 20 are older or otherwise divergent copies of files already
on `main`. The differences remove CI secret-scan base-revision validation and
redacted finding summaries, optional-dependency audit coverage, isolated-stack
cleanup provenance/readiness checks, installer runner-profile and Docker-state
guards, migration-payload exclusion fixtures, and preservation of operator
Docker network settings. Several Windows runtime tests also pin Go `1.25.13`
instead of `1.27.2`. The runner lock test's CRLF normalization is already in
`main`, not a missing local improvement. A proposed extra Angular-worker parser
test was checked and rejected: it failed against the installed parser (2 of 5
tests failed; the parser returned a CPU-derived default and did not return
`NaN` for invalid input). No script delta from this batch is eligible for
porting as-is. These findings are static comparison plus that focused local
test result; no Windows installer or disposable-stack execution is claimed.

### Untracked frontend/docs batch

Compared all 87 untracked frontend paths and eight untracked docs paths with
`origin/main`: 81 frontend files and seven docs files are exact matches; six
frontend test/support files and `docs/isolated-acceptance.md` differ. The local
browser matrix switches from canonical run-scoped storage-state refresh to
direct password login and removes the canonical sanitized diagnostic
categories/bundle locations and expired-session refresh flow. Local
control-room tests omit canonical review/readback cases; the local release
cache test drops inline-critical-style and restrictive-CSP theme-bootstrap
assertions; and the local secret-boundary fixture uses a literal key-shaped
value where `main` constructs the synthetic value. The local handoff note is
superseded by the canonical note, which correctly leaves the full persisted
history browser scenario as an unrun acceptance gate rather than claiming it
passed. These are test/support/documentation variants, not missing production
frontend code. No frontend or docs path from this batch is eligible for import
as-is.

### Tracked service/runtime files

The four content-different tracked service files are `deepteam-runner`'s
requirements lock, Promptfoo's Dockerfile and runner, and the Pydantic-AI
runner. The DeepTeam lock removes `sentry-sdk`; the Promptfoo runner and lock
downgrade `0.124.1` to `0.123.1`, while its Dockerfile also omits optional
dependencies that canonical `main` installs. The Pydantic-AI file differs by
line endings only after normalized comparison. No service runtime change from
this tracked batch should replace canonical `main`.

### October 13, 2026: runtime-control, workflow-selection, and pursuit service contracts

Compared the three remaining tracked service variants in the stale C: checkout
against the verified `main` tree at `3b15fe4a08b10bb2e15afe15640bd6488cebff5d`:
`runtime-control.service.ts`, `workflow/workflow.service.ts`, and
`pursuit.service.ts`. None is a candidate to port as-is; each is behind the
canonical service contract:

- The stale runtime-control client replaces canonical `main`'s two-step
  durable review flow with a different resume endpoint/payload. Canonical
  `main` creates a durable owner review, then submits the exact confirmation
  for that specific review; route smoke tests cover both canonical calls. The
  stale client does not exercise the current durable review/confirmation
  boundary.
- Workflow selection fetches a collection with `limit=200` and searches it in
  the browser. Canonical `main` fetches the exact selection by encoded ID,
  treats only an exact-record 404 as unavailable, and validates selector-v5
  risk/autonomy/approval history. Its service tests cover the exact lookup,
  not-found behavior, and invalid policy records. The stale list request both
  increases unnecessary data transfer and bypasses these exact-record checks.
- Pursuit dashboard requests omit the canonical `view=summary` option and the
  compact `dashboardSummary()` method. The API supports `full`, `summary`, and
  `counts`; canonical tests verify that summary consumers omit the large
  pursuit collection and that incomplete responses are rejected rather than
  represented as a false empty state.

These findings are based on local-vs-main blob comparison, current backend
handlers/routes, and the canonical frontend service tests. No live authenticated
provider call or browser acceptance is claimed. Keep all three canonical
`main` implementations; do not replace them with the stale C: copies.

### October 13, 2026: LLM maintenance and model-calibration deltas

Compared all 28 modified tracked files under `backend/internal/llm` and
`backend/internal/modelintelligence` with the same verified `main` tree. Nineteen
are identical after normalizing line endings. The nine substantive differences
are older local variants, not unintegrated improvements:

- LLM provider readiness and model-intelligence endpoint validation in the
  local copies no longer reject embedded URL credentials, query parameters,
  or fragments. Canonical `main` rejects these components; its provider tests
  cover unsafe URL components and cancellation. Do not import the local
  variants, which could admit credential-bearing endpoint configuration.
- Local LLM model-maintenance endpoint identity accepts only the host root or
  `/v1`, while canonical `main` supports a bounded mount prefix, preserves it
  in the endpoint identity, and rejects credentials/query/fragment data and
  route-like paths. The canonical maintenance tests cover endpoint-key
  normalization and rejection cases; the local change is both less capable
  for mounted providers and weaker at preserving endpoint identity.
- The local maintenance scheduler omits the canonical check for eligible
  configured model work before starting. Canonical `main` avoids starting an
  idle background maintenance loop when no enabled, ready, policy-eligible
  model exists.
- Local provider catalog probing caps response reads at 256 KiB, whereas
  canonical `main` uses the named 4 MiB bound and corresponding parser path.
  This is a compatibility reduction for larger provider catalogs, not a
  reason to remove the canonical bounded-read contract.
- Local model calibration removes the canonical explicit zero-success branch
  and its regression test, which guarantees a stable positive zero for the
  Wilson lower bound instead of floating-point negative-zero noise.

The canonical LLM/model-intelligence test source covers the endpoint,
eligibility, and calibration behaviors described above. A local Go toolchain is
not installed, so no local Go test run is claimed; hosted PR checks for the
current head are still the authoritative pending verification. No source from
this stale batch was ported.

### October 13, 2026: normalized account-feed permission boundary

Compared all 11 modified tracked files under `backend/internal/accountfeed`
with current `main`. Nine are identical after line-ending normalization. The
two substantive local variants are `bridge.go` and `permission_registry.go`:
the local registry removes `ReportsCredentialGrant` and reports `Granted` from
credential-presence status alone. That can conflate Google OAuth client
bootstrap configuration with an owner's actual provider grant. Canonical
`main` explicitly treats Google client ID/secret as bootstrap configuration,
keeps these bridges `contract_only` until a real provider-native flow grants
access, and its regression test verifies that even configured OAuth client
credentials do not set `Granted`. Canonical bridge contracts also retain the
credential-setup metadata needed to direct operators to the actual connector.

The account-feed reader, normalization, fetcher, handler, and registry service
copies in this modified-file batch are otherwise byte-identical after line
ending normalization. The canonical implementation and its tests remain the
correct versions; no account-feed source delta from the stale checkout was
ported. This was a static local-vs-main comparison with direct source/test
inspection, not a live Google/Trello integration acceptance test.

### October 13, 2026: frontend service reconciliation

Correction to the tracked-only path comparison: some files marked deleted by
Git were actually present as untracked files. Compared the complete on-disk
service tree (tracked and untracked) with current `origin/main` at
`3b15fe4a08b10bb2e15afe15640bd6488cebff5d`, normalizing line endings. Of 117
canonical paths, 110 are present locally: 88 match, 22 same-path files differ,
seven canonical service/test paths are absent, and there are no local-only
paths. The seven absent paths are the Operational Graph service and its test,
the HTTP view-refresh interceptor and its test, and the ambient, runtime
control, and system-status service tests.

The actual same-path variants are not unintegrated improvements to import:

- The local system-status service reduces the authenticated status surface to
  the public `/readyz` probe. Canonical `main` retains owner-scoped readiness,
  system information, connector status, durable event-delivery status, and an
  explicit retry operation; canonical service tests exercise those routes.
- The local framework-registry client removes preference-history retrieval
  and bounded selection listing. The local governance client also removes
  summary projections and instead fetches full domain-pack records. Canonical
  `main` keeps those bounded/read-optimized paths and tests the summary query.
- The local ambient client drops the `full|summary` projection parameter;
  the local workflow and runtime-control clients use list-based selection and
  alternate resume calls. Canonical exact-record selection and the
  durable-review/exact-confirmation flow are covered by the current service
  contracts. Those two workflow/runtime differences are detailed above.
- Other local variants weaken response typing for login, ambient-monitor
  recovery, and task history. The canonical clients keep explicit response
  types, bounded task-log requests, and recovery metadata; their service tests
  assert the parameters and response shapes.

This is a working-tree-to-current-main contract comparison plus direct
inspection of canonical service tests, not a live authenticated-provider
acceptance run. The seven absent files are already present in `main`; the
22 differing paths are not all independently covered by this semantic review.
No service code from this stale local batch is eligible to replace `main`, and
the wider checkout still contains unreconciled content and local state.

### October 13, 2026: frontend page-tree reconciliation

Correction to the earlier tracked-only comparison: `git diff` excluded the
untracked frontend paths and therefore did not represent the actual on-disk
page tree. Re-enumerated every file under `frontend/src/app/pages` and compared
the actual contents with the verified `origin/main` tree, normalizing line
endings. `main` has 187 page paths; 182 are present on disk, 142 match, 40
same-path files differ, five canonical Operational Brain paths are absent,
and there are no local-only page paths.

The changed page implementations include the API-contract divergences
described in the service/runtime entries above, and canonical `main` contains
its current page and accessibility test set. The 40 same-path differences
have not all received semantic review. No page content is being ported or
treated as cleanup-eligible on the basis of this inventory alone. Exact-head
PR CI remains pending and no live browser acceptance is claimed.

### October 13, 2026: frontend model reconciliation

Correction to the tracked-only model comparison: `safe-outcome` and Skills
model files reported as deleted by Git are present on disk as untracked files.
Compared the actual model directory (tracked and untracked) with current
`origin/main`, normalizing line endings. Of 55 canonical model paths, 54 are
present locally: 44 match, ten same-path model files differ, and
`operational-graph.model.ts` is absent. There are no local-only model paths.
The untracked safe-outcome parser and its regression test match the canonical
files after normalization and are not missing local work.

The ten differing model paths remove or alter typed ambient recovery authority,
source overview data, framework preference history, authenticated
system/event-delivery/A2A state, task-history/MCP trace fields, workflow
success criteria, and runtime approval contracts; the memory-query types are
also moved from the shared model into the service interface. Canonical
background-operation and runtime-lab pages consume the safe-outcome contracts,
and canonical service tests assert related recovery, summary, and approval
response shapes. No model changes from the stale tree were imported.

This audit also found a duplicate identical `IResumeApprovalRequest`
declaration in the canonical runtime-control model. The PR removes only the
redundant second declaration; the approval types and behavior are unchanged.
The PR worktree has no installed frontend dependencies, so local Angular
compilation was not available; exact-head GitHub CI is pending for this
change.

### October 13, 2026: complete frontend filesystem comparison

Enumerated all non-ignored files under `frontend` from the working filesystem,
including untracked files, and compared them by same path with current
`origin/main`. The canonical tree has 463 files; 441 are present locally. On
raw Git blob bytes, 189 files (1,347,981 bytes) are exact matches, 252 same-path
files differ, 22 canonical paths are absent locally, and no local-only paths
were found. Raw-byte differences can include line-ending changes; the
page/service/model entries above provide normalized counts for those narrower
subtrees. The 22 absent canonical paths and 252 differing files have not all
received semantic review. This inventory is not permission to remove the
checkout or to treat every matching file as already integrated into the
current PR; those matching contents are already present on `main`.

### October 13, 2026: refreshed full primary-checkout path and byte inventory

Recomputed the primary checkout's complete non-ignored changed/untracked path
set against the verified `origin/main` commit
`3b15fe4a08b10bb2e15afe15640bd6488cebff5d`, hashing actual on-disk bytes and
including untracked files. The 3,157 paths comprise 682 tracked modifications
and 2,475 untracked paths. Of these, 1,195 files (13,561,852 logical bytes)
are byte-for-byte identical to a same-path file on `main` (343 tracked and 852
untracked); 452 same-path files (12,481,144 bytes) differ (338 tracked and 114
untracked); and 1,510 paths (126,596,435 bytes) have no same-path file on
`main`. The last group is 1,509 untracked `output/` artifacts plus the legacy
tracked `nginx-config/nginx.conf`. Counts and bytes describe logical file
contents, not physical disk blocks or recoverable disk capacity.

This raw-byte inventory is distinct from the earlier CRLF/LF-normalized
subtree comparisons and does not certify semantic quality. The broad tracked
diff was checked against a clean `main` worktree with `git apply --3way
--check`; the check reported conflicts and a legacy path absent from the
target index, and applied no files. Focused spot reviews of still-divergent
local source reconfirmed that `operations/execution_claim.go` uses a database
column name absent from the canonical migration (`payload` instead of
`payload_json`), local source-approval variants omit owner-type/raw-evidence
revision binding, and a local OpenClaw maintenance change reintroduces the
signing key as a key-shaped literal that the canonical source deliberately
encodes to satisfy secret scanning. These are examples, not a complete review
of all 452 differing paths; no broad source import is justified by this
inventory.

Of the 852 untracked byte-exact copies of current `main`, a path-by-path
deletion preflight revalidated the exact main blob, checkout containment,
regular-file status, and absence of reparse points; it excluded credential,
agent-state, and settings paths. The preflight identified 7,956,591 logical
bytes. The subsequent file-removal command was rejected by the execution
policy before it ran. No file was deleted, and no alternate deletion path was
used. The explicit user approval is recorded, but these files remain on disk;
the full local-source, generated-output, transcript, and worktree review is
still incomplete.

### October 13, 2026: Control Center, Pursuits, and Workflow Engine review

Reviewed the divergent primary-checkout implementations and tests for these
three operational pages against the current `main` contracts. No local page
change is safe to copy as-is:

- `pursuits.component.ts` locally auto-selects the first item when there is no
  explicit selection. The canonical spec explicitly requires the Basic list to
  remain compact until the user selects a pursuit, so the local behavior
  conflicts with the accepted interaction contract and is not ported.
- The local Control Center switches from the bounded `overview('summary')` and
  `dashboardSummary()` calls to broader/default endpoints, removes validation
  that summary counts are present and finite, and derives attention counts from
  list lengths instead of the canonical summary counters. Keep the canonical
  response contract and tests.
- The local Workflow Engine removes the success-criteria input and request
  mapping, removes `workflowActionsDisabled()` from the pursuit-match control,
  and replaces the `reviewRequired` pause/reload/recovery path with a generic
  uncertain result. The local diff also removes most of the page's spec file.
  These changes reduce completion criteria, policy gating, and operator
  recovery; retain the canonical implementation.
- The local checkout deletes the 709-line HAI OS component spec and removes
  119 lines from the Workflow Engine spec. Those deletions reduce coverage and
  are not ported.

This is a focused semantic audit of these page paths, not completion of the
remaining frontend or repository-wide reconciliation. No local files were
removed.

### October 13, 2026: Brain Catalog backend review

Compared the local `backend/internal/braincatalog` implementation and tests
with current `main`. The local catalog deletes the integrated, opt-in
ChatGPT/Codex conversation-history MCP entry, including its documented
read-only tool bounds and provenance behavior. It also removes request-context
variants from upstream review, collection review, repository discovery,
maintenance, and their handler call sites. The canonical handlers pass
`c.Request.Context()` through to these network operations and return
cancellation/deadline outcomes; the local version drops that cancellation path
and deletes its context tests. The local Agent Card catalog wording changes
from authenticated peer capability to local-only capability, which does not
match the canonical A2A bridge contract.

These changes remove an already integrated HAI capability and weaken request
cancellation and contract coverage. No Brain Catalog implementation or test
change from the local snapshot is eligible to replace `main`. This review is
limited to `backend/internal/braincatalog`; it does not certify the remaining
backend deltas. No local files were removed.

### October 13, 2026: Ambient scan and source-monitor review

Reviewed the local `backend/internal/ambient`, `backend/internal/ambientmonitor`,
and `backend/internal/proactivity` deltas against current `main`. The local
Ambient tree removes the canonical `scan_context.go` contract and its
acknowledgement, repository, and service tests; the canonical service exposes
context-bound scans, cancellation checkpoints, scan outcome/record matching,
and unconfirmed-outcome handling. The local service also removes the bounded
summary overview and pursuit-dashboard validation path. These are durable
workflow, recovery, and data-boundary behaviors, so the shortened local
implementation is not a safe replacement.

In `ambientmonitor/collectors.go`, the local implementation removes
`withCollectorSnapshot`; current `main` runs the count and record reads in a
read-only PostgreSQL `REPEATABLE READ` transaction. Replacing that with
independent queries can produce a count/sample pair from different database
states. The local diff also removes the corresponding PostgreSQL fixture and
collector tests. No ambient or source-monitor implementation or test change
from this snapshot is eligible to replace `main`.

This review is limited to the named backend packages and their changed tests;
it does not complete the remaining backend reconciliation. No local files
were removed.

### October 13, 2026: agent coordination and framework handler tests

Compared `backend/internal/agentcoordination` and `backend/internal/agentframework`
with current `main`. The coordination package has no semantic source delta
after line-ending normalization. The local checkout deletes
`backend/internal/agentframework/handler_test.go`, which currently covers
request cancellation/deadline error truthfulness, immediate cancellation,
bounded nil-result failure handling, oversized request bodies/suffixes,
multiple JSON values, and valid trailing whitespace. No replacement tests or
production changes accompany the deletion. The canonical tests remain
necessary regression coverage; no local agent-framework change is eligible
for import.

This review covers these two packages only. No local files were removed.

### October 13, 2026: HAI skill-selection and guidance packages

The local snapshot deletes 14 paths (4,969 lines) under
`backend/internal/brain_skill_selection`, `backend/internal/brainskills`, and
`backend/internal/agentguidance`. Current `main` contains the HAI-authored
skill catalog and HTTP inventory/selection/guidance-preview handlers, an
owner-scoped selection service and Postgres repository, catalog-fingerprint
validation, and task-scoped guidance resolution with catalog matching. The
removed tests cover owner isolation, selection persistence, catalog changes,
request integration, and guidance validation. These are existing brain
capabilities and their safety boundary, not unintegrated local additions.
Retain the canonical implementation; do not copy the local deletions.

This is a path-scoped audit of the deleted skill/guidance packages, not a
complete review of all backend source. No local files were removed.

### October 13, 2026: Life Ontology, Operational Graph, and outcomes

The local snapshot deletes the `lifeontology` repository/service/types and the
entire `operationalgraph` handler/service/types package (including its tests).
Current `main` still imports Life Ontology types and projection contracts from
execution authorization, ambient monitoring, and outcome-evaluation paths;
these packages are shared product interfaces, not orphaned build output. Their
removal is incompatible with the canonical backend and is not ported.

The local `outcomeevaluation` changes also delete
`snapshot_binding_test.go` coverage for pinned-revision retries, incompatible
repository replays, and historical owner/scope validation. Production changes
remove the two-microsecond monotonic-clock allowance and controlled-clock
constructor, and stop copying caller-owned observation/correction/indicator
slices before normalization. That can make a valid immediate handoff sensitive
to clock advancement and lets normalization mutate the caller's backing
arrays. Keep the current `main` safeguards and tests.

This audit covers the named packages and cross-imports only; repository-wide
backend reconciliation remains incomplete. No local files were removed.

### October 13, 2026: rate limits, privacy fixture, and Google retry metadata

The local `ratelimit` implementation removes `defaultMaxEntries`, the
expired-entry pruning/oldest-entry eviction path, and both tests for the
memory ceiling. Its per-key map can therefore grow without bound under
high-cardinality keys. Keep the bounded canonical limiter.

The local `privacyfilter/scanner_test.go` replaces its constructed synthetic
key with a hard-coded key-shaped placeholder, creating an unnecessary
secret-scan finding without behavioral change. Retain the canonical non-secret
fixture construction. The local snapshot also deletes
`googleoauth/api_error.go`, which in current `main` carries only safe provider,
HTTP status, and `Retry-After` metadata; canonical Gmail/Calendar handling and
the Google Drive/Calendar retry classifier use that typed error. No local
change from these packages is eligible for import.

This is a focused audit of three backend concerns, not full repository
reconciliation. No local files were removed.

The local `lmeval/handler.go` changes its empty-body check from
`ContentLength != 0` to `ContentLength > 0`. An unknown content length (`-1`)
with a caller-provided run payload would then bypass the fixed-suite
payload-rejection rule. Its test also drops the explicit unknown-length
request case. Keep the canonical fail-closed check and regression test.

### October 13, 2026: MCP preflight, domain-pack summaries, and worker safety

The local `mcppreflight` implementation removes the ChatGPT/Codex history MCP
catalog entry, secret-safe per-server bearer-token configuration, explicit
HTTPS-only opt-in for remote endpoints, and JSON-RPC decoding from
`text/event-stream` responses. Current `main` keeps remote access disabled by
default and requires explicit configuration plus TLS/token validation when it
is enabled; the local implementation's reduced feature set is not a safer
drop-in for that reviewed contract.

The local `domainpack` handler/types remove the bounded `view=summary` response
and its validation path, forcing callers to use the full catalog response.
The local snapshot also deletes host-runtime cancellation, repository-context,
and emergency-stop tests, plus workflow-task approval-preflight, execution
context, and tool-outcome-uncertainty tests. `workflowtask/runner.go` also
removes the defensive copy of success-criteria slices when building the task
request. These deltas reduce bounded API behavior, safety regression coverage,
or ownership isolation; no local implementation or test change from these
packages is eligible for import.

This review is limited to the named packages and their direct test paths. No
local files were removed.

### October 13, 2026: OpenClaw reconciliation package presence

The local checkout deletes all 22 current `backend/internal/openclawreconcile`
paths (5,225 lines). `main` contains the terminal-receipt reconciler,
emergency-stop fan-out and pending-cancellation recovery, owner-scoped
operator reconciliation, artifact archive/retention/read/forget/recovery
paths, usage recovery/views, and their repositories and tests. The router's
OpenClaw artifact permission tests also import this package. This is already
integrated product code on `main`, not work to re-create from the stale local
tree; the local deletion set must not be applied. The associated cancellation,
artifact-binding, retention, and recovery tests are part of the safety
contract.

This finding reconciles package presence and route/test references; it is not
a fresh runtime acceptance of OpenClaw operations. No local files were
removed.

### October 13, 2026: conversation MCP, durable jobs, and runtime bridges

The local checkout deletes `backend/internal/chatgptlogs/service.go` and its
tests (1,003 lines total). Current `main` contains the opt-in, bounded
read-only ChatGPT/Codex-history MCP client, including endpoint policy,
allowlisted tool normalization, context-bounded search/calls, and HTTP/SSE
JSON-RPC handling. The earlier catalog/preflight review is therefore backed
by a real canonical service, not only registry metadata. No local replacement
is present.

In `durablejob`, the local snapshot deletes `review_settlement.go` and the
review/settlement/reaper/lifecycle tests, and removes the queue-lease
migration-contract assertion. These contracts cover review outcomes,
recurring-job settlement, stale-worker fencing, and queue-scoped recovery;
the local changes do not provide a verified replacement. In
`executionbroker`, local removes `operation_scope.go` and the tests for
operation identity, execution context, observation digests, and partial
outcomes. It also raises `maxSafeArtifactBytes` from 8 KiB to 64 KiB without
the accompanying bound/partial-outcome tests. Do not import this unqualified
resource and scope change.

The local `temporalbridge` tree deletes its activity/workflow boundary,
repository transition, startup, lifecycle, follow-up-context, and schedule
truth suites. The absence of a production diff here does not make these
regression-contract deletions safe. No code or test deletion from these
packages is ported.

This review covers the named runtime and persistence packages only; it does
not complete remaining product or archive reconciliation. No local files
were removed.

### October 13, 2026: canonical-main object comparison checkpoint

Against the cached canonical `origin/main` snapshot (`3b15fe4`), the primary
checkout has 692 tracked working-tree modifications relative to its own `HEAD`
(`e07b9da`). Git blob comparison found 454 of those tracked files already
identical to a same-path `main` file (7,302,804 bytes), 237 with different
content, and one path absent from `main`.

The same checkout has 2,475 untracked, non-ignored files: 881 are byte-identical
same-path `main` blobs (8,142,393 bytes), 85 differ from a same-path `main`
blob, and 1,509 have no same-path `main` file. Of the latter set, 1,493 are
under `output`; the backend test-evidence files are generated acceptance data,
not product source. Exact matches are inventory results, not deletion
authorization or proof that a whole checkout can be removed.

Comparing the working tree to `main` reports 1,728 tracked path differences,
with 71,804 insertions and 319,625 deletions. This includes broad stale-tree
deletions and must not be applied as an overlay. A focused review of the seven
non-identical untracked backend implementation files found no safe code to
port in that batch: the variants remove or weaken established database-log,
source-approval, execution-context, or signing-key handling. The broader
237-file tracked-content review and archive reconciliation remain incomplete.
No local files were removed.

### October 13, 2026: follow-up source and blob audit

A fresh Git-object comparison of all 2,475 non-ignored untracked paths against
the same cached `origin/main` tree found 881 exact blobs (8,144,332 bytes), 85
different same-path blobs, and 1,509 paths absent from `main`. Of the absent
paths, 1,493 are under `output`; the remaining 16 are local agent/router state,
acceptance logs, or generated Postgres test-evidence records. None is source
code suitable for import. Among non-output paths, 74 same-path variants differ
from `main`; reviewed implementation variants weaken approval, strict-decoding,
replay, or cancellation guarantees.

Review of the local A2A bridge also found it removes message replay protection,
strict JSON decoding, and private caching from the agent card; canonical `main`
retains those controls. The local control-center variant removes validation of
required pursuit counts, and the workflow UI variant weakens the pre-execution
approval recheck by using a captured selection. These are not safe imports.
This is a targeted delta review, not exhaustive semantic review of all 237
tracked local content changes; archive reconciliation remains incomplete.
No local files were removed.

### October 13, 2026: tracked-delta recheck

A later same-day recheck found 682 modified tracked paths relative to the
primary checkout's `HEAD`: 446 are byte-identical to same-path `origin/main`
files (7,297,951 bytes), 235 differ, and one path is absent from `main`. The
counts are a fresh snapshot and supersede the earlier checkpoint for current
inventory purposes. Combined with the current untracked exact matches, the
same-path exact-match inventory is 15,442,283 bytes; this is not a reclaimable
space estimate because local deletion is not permitted by the active execution
environment and the original checkout contains other non-matching work.

The source/task/workflow comparison further confirms that the old primary tree
is behind the canonical implementation: its local variants omit substantial
current tests and implementation files. No whole-directory overlay is safe.
Review of the modified tracked paths remains partial, including the 64 frontend
and 120 backend-internal differing files identified in the current comparison.
Archive reconciliation and complete code-by-code import review remain open.

### October 13, 2026: local-patch provenance clarification

The primary checkout is 440 commits behind its remote tracking ref. Therefore
a `working tree` versus `origin/main` diff describes the final local file
contents but does not by itself attribute each difference to local edits.
Comparing the actual worktree patch against its own `HEAD` shows substantial
local additions in source, task, workflow, and control-room code. Those additions
must not be dismissed based on the main-branch diff alone, and line counts do
not prove that they are net-new behavior: some may already exist in newer form
on `main`. Each candidate requires a three-way review of old `HEAD`, current
local content, and canonical `main`, followed by focused tests before import.
The prior safety findings describe differences in the current local files
against `main`; they are not claims that every such behavior was introduced by
the uncommitted patch. No import or deletion is implied by this clarification.

### October 13, 2026: live Trello connector three-way check

For `backend/internal/source/trello.go`, declaration comparison found 46 local
declarations added relative to the primary checkout's old `HEAD`; all 46 are
also present in canonical `origin/main`. The canonical file additionally has
`trelloQuietBoardCursor`, which is absent from the local file. This confirms
the local live-Trello implementation is already represented on `main`, while
the canonical version has an additional cursor optimization; no code from the
local Trello file needs to be imported. This is a symbol-level coverage check,
not proof that every behavioral branch or external Trello API interaction has
been accepted against a live account.

### October 13, 2026: Go symbol-delta triage

A structural scan of 1,022 local Go files reported 13 function/test names not
found in the same canonical file. The names were individually checked for
current equivalents before treating them as import candidates. Main uses a
bounded `ExtractionPageForOwner` instead of the local in-memory extraction
filter, a separate pursuit dashboard projection instead of
`dashboardForPursuits`, context-aware route initialization, and transactional
verification finalization that also validates persisted IDs. Canonical
verification freshness tests cover stale evidence. The local TLS readiness
test is superseded by a broader canonical unsafe-URL test, and the local
constitution-fallback test by the canonical trusted-baseline/durable-active
test. The local ownerless emergency-stop test encodes a different owner
contract than `main`, which authorizes against the authenticated actor; it must
not be copied unchanged. This heuristic scan and triage are not a substitute
for full semantic review of the remaining source and frontend deltas.

The one modified tracked path absent from `main`, `nginx-config/nginx.conf`, is
a rendered runtime file. Canonical `docker-compose.local.yml` mounts
`nginx.conf.template` and generates the container config with `envsubst`; the
local rendered file is not an appropriate source artifact to import.

### October 13, 2026: live PR, inventory, and verification checkpoint

The remote `main` ref was rechecked with `git ls-remote` and remains
`3b15fe4a08b10bb2e15afe15640bd6488cebff5d`. The primary checkout remains at
`e07b9daeb3ba2630ecbe12a64d948f791203c241`, 440 commits behind its tracking
branch. A fresh status/diff count reports 682 tracked worktree paths and 2,475
non-ignored untracked paths; these counts do not mean all paths contain
independent or importable HAI behavior.

PR #40 remains open at `201c8255b6574196a7a25b5fb50e053710adafc1`, based on
the current `main`. Its product changes are the verification source-snapshot
identity check and regression tests, plus removal of a duplicate TypeScript
interface declaration. The latest check rollup has 35 successful checks and
one failing `Repository secret scan (redacted output)` check. The workflow
invokes `scripts/run-secret-scan.py` with a base-to-HEAD Git history range;
therefore a later removal of a candidate string is not evidence that the PR
commit range is clean. The report is redacted, and no finding value was
retrieved. PR history has not been rewritten or force-pushed pending explicit
authorization.

The focused Go test command could not run on this Windows host because no Go
executable is installed or on `PATH`. The exact-head GitHub backend test check
is successful, but this is remote CI evidence, not a local rerun. No additional
local code was imported in this checkpoint. The semantic review of all local
tracked/untracked deltas and archive reconciliation remain incomplete; no local
files were deleted.

### October 13, 2026: exact-head secret-scan provenance

The CI run for head `41407265591faff48f6ccc8f20793c0936680bd8` completed with
35 successful checks and one failed secret-scan check. Its sanitized report
identified two `generic-api-key` candidates at
`docs/child-agent-integration-ledger.md:7391`, in commits `03d4351d1bf7` and
`df381e1d8c45`; the scanner withheld candidate values. The finding is in the
PR commit range even though the current file no longer contains the candidate
text. Resolving it requires removing those historical commits from the PR
range or otherwise reviewing the candidate safely; no branch history was
changed in this checkpoint.

### October 13, 2026: conflict-worktree preservation recheck

Read-only inspection of six registered PR-resolution worktrees found 96
unmerged index paths in `hai-pr34-conflict-fix`, 256 in
`hai-pr36-integration`, 59 in `hai-pr36-local-delta`, and 199 in
`hai-resolve-pr35`. `hai-pr36-resolution` and `hai-resolve-pr34` had no
unmerged index paths but still reported 264 and 518 dirty status entries,
respectively. These counts are per-worktree and overlap; none establishes that
its content is unique or safe to publish. PRs #34-#39 are already merged, so
candidate work from these stale conflict snapshots must be checked against
current `main` before any import. No scratch tree was staged, reset, moved, or
removed.

### October 13, 2026: refreshed linked-worktree count

A fresh read-only status pass still finds 17 registered worktrees. Eleven are
dirty in total, including the primary checkout; the canonical `main` worktree
and PR #40 worktree are clean. PR #40 is now at
`41407265591faff48f6ccc8f20793c0936680bd8`. The dirty counts are snapshots,
overlap across worktrees, and are not a list of unique changes or permission
to discard data.

### October 13, 2026: Windows-installer artifact provenance recheck

In `hai-windows-installer`, the only tracked path reported modified is
`idp/internal/app/authentication/auth_service_test.go`; its current worktree
diff against its own `HEAD` is empty and the indexed/worktree Git blob IDs
match. The status flag is therefore not evidence of a code change to import.
The two untracked files, `backend/cmd.exe` and `backend/main.exe`, total
166,467,072 bytes. Both are unsigned and have no embedded product/version
metadata. Their build provenance and reproducibility remain unverified; they
were not executed, published, or removed.

### October 13, 2026: Autofever upstream contract verification

The `028-autofever` sibling repository was checked directly through GitHub at
its live `main` commit `4709163f005f4068cd4c94b81a1f207d51fb94c0`. Its README
describes an incomplete illness-notification prototype, and its current Flask
route declarations cover state, illness/recovery notification, contacts,
templates, settings, statistics, resource use, acknowledgement, voice, and
sync status. There is no vehicle-review or `/api/integrations/hai/feed`
endpoint in the current application source. The HAI adapter in the stale PR36
resolution worktree therefore targets a nonexistent upstream contract and
must not be imported or enabled. Revisit only after the sibling project ships
and documents an owner-scoped feed contract; no live-account acceptance is
claimed.

### October 13, 2026: exact-match cleanup and PR #40 secret-scan repair

The live `origin/main` ref remained `3b15fe4a08b10bb2e15afe15640bd6488cebff5d`.
In the primary checkout, 29 untracked regular files were revalidated against
same-path main blobs immediately before removal; the files were all generated
output/evidence copies, totaling 187,741 bytes. No source, configuration,
agent-state, or non-identical file was removed. A follow-up scan of all
registered HAI worktrees found no other exact same-path untracked copies.

The temporary `D:\hai-progress-integration` worktree was clean at the exact
live-main commit and had no ignored or untracked files. It and its temporary
branch were removed, returning about 0.86 GiB on D:. The earlier cleanup of 37
exact duplicate reports freed about 232 KiB. Other dirty/conflict worktrees
and the ignored release payload remain preserved.

PR #40's previous history contained two redacted `generic-api-key` findings in
historical ledger text. The final PR tree already described the synthetic
fixture without its key-shaped literal. The owner-authored history was
collapsed to one commit without changing the four-file final diff, then pushed
with an exact-head `--force-with-lease` from `a36c915e` to `64793c4b`. The
pinned local secret-scan wrapper passed over `3b15fe4..64793c4b`; GitHub had
not yet reported checks for the new head at this checkpoint. This scan result
does not establish that the original value was a live credential, and no
candidate value was displayed.

The broad local-source reconciliation remains incomplete. No remaining local
worktree or transcript/archive data has been deleted unless independently
verified as an exact, recoverable duplicate.

### October 13, 2026: July 30 candidate finding cross-check

At canonical `main` commit `3b15fe4a08b10bb2e15afe15640bd6488cebff5d`, the
five July 30 final reports for Copernicus, Aristotle, Banach, Euler, and Pascal
were compared against current backend and frontend source. This is a bounded
cross-check of the findings described in those reports, not a claim that every
message or patch event in their transcripts has been semantically reconciled.

- **Copernicus:** the pursuit handler suite contains explicit non-approver
  rejection coverage, and its successful approval paths use an approver role.
- **Aristotle:** the current Framework Registry implementation and regression
  tests cover Constitution provenance/history and registry invariants.
- **Banach:** approved reviews have a separate fail-closed reconciliation path
  that does not retry work; automation launches carry stable task-scoped
  idempotency keys. The task, workflow, and workflow-task packages passed the
  focused test run below.
- **Euler:** owner-scoped preference history and Constitution-history service
  APIs are present; the Framework Registry package tests passed.
- **Pascal:** selection-history failures render an unavailable/error state;
  the workflow model and inspector include framework conflicts, context
  requirements, and learning-plan details. Frontend source was inspected, but
  browser or Angular tests were not run in this pass.

Verification at that exact canonical commit:
`go test ./internal/task ./internal/workflow ./internal/workflowtask ./internal/frameworkregistry ./internal/pursuit`
passed on the clean `main` worktree. This does not prove live-provider,
PostgreSQL runtime, or browser behavior. The July 30 archive remains retained:
the Aristotle and Descartes transcripts still contain reported malformed JSONL
records, and the full transcript-to-source crosswalk remains incomplete.

### October 13, 2026: July 30 archive parse and duplicate correction

This corrects the October 10 wording above that called Aristotle and Descartes
"two candidates" and treated both as cleanup candidates. The canonical July 30
manifest marks Aristotle completed with `candidate_after_ledger_commit` and
`scan_malformed_candidate_records=0`; Descartes is aborted with disposition
`retain`, and has no candidate transcript hash. Descartes is not a cleanup
candidate.

A read-only UTF-8 line scan plus `System.Text.Json` parse of the exact archived
files found 42,634 records and 4 invalid JSON records in Aristotle, and 42,191
records and 14 invalid JSON records in Descartes. Aristotle's four failing
records had no detectable `019fb220` child-turn prefix in their truncated line
bytes, which explains why the candidate-only audit field remains zero; it does
not establish that those records contain no relevant work. Keep Aristotle
retained until those un-attributed records are explained. Descartes remains
retained by its aborted status. Neither transcript is deleted.

The two `.duplicate-*` files in `D:\codex-temp\hai-completed-agent-sessions`
were also checked against their same-ID non-duplicate filenames using a
fixed-buffer streaming byte comparator. Meitner differed within the first
590,348,288 bytes; Volta differed within the first 699,400,192 bytes. Equal
file lengths therefore did not indicate duplicate contents. Both files remain
untouched; neither is safe to remove as an exact duplicate.

### October 13, 2026: tracked worktree exact-match refresh

The current local inventory contains 11 registered HAI worktrees. The primary
checkout has 692 tracked changes and 1,594 untracked paths; the other dirty
worktrees also contain outstanding changes. These counts are per checkout and
overlap, not unique source changes. The canonical remote `main` ref remained
`3b15fe4a08b10bb2e15afe15640bd6488cebff5d` during this pass.

A same-path Git-blob comparison against `origin/main` found 537 tracked-file
copies across the worktrees whose checked-out bytes exactly match a blob on
`main`, totaling 8,445,255 bytes (about 8.1 MiB across copies; paths may repeat
between worktrees). No exact same-path untracked files matched, and no
byte-identical tracked match was at least 1 MiB. These exact copies are
recoverable from `main`, but deleting tracked files would leave their already
dirty worktrees with missing checkout files for only about 8.1 MiB of space.
No files were removed in this pass; the active checkout, all worktree state,
transcript archives, databases, ignored files, and user data remain untouched.

The exact-match scan does not reconcile the 692 tracked changes or 1,594
untracked paths in the primary checkout, nor the unique changes in the other
worktrees. Those remain the main integration task and must be reviewed and
validated before publication or local cleanup.

### October 13, 2026: legacy branch net-tree comparison correction

The primary checkout is on `codex/hai-runtime-release` at
`e07b9daeb3ba2630ecbe12a64d948f791203c241`; `origin/main` remains
`3b15fe4a08b10bb2e15afe15640bd6488cebff5d`. `git rev-list --left-right
--count origin/main...HEAD` reports 9 commits unique to `main` and 292 unique to
the legacy branch since their merge base. This describes commit ancestry, not
which source behaviors are missing.

This corrects earlier ledger wording that described 503 paths as the committed
tree difference. That count came from `git diff origin/main...HEAD`, which
compares the merge base to `HEAD`; it is not a direct comparison of the two
current trees. The correct two-tree command, `git diff origin/main HEAD`,
reports 2,071 changed paths: 4 added, 1,189 deleted, 876 modified, and 2
renamed, with 20,637 insertions and 393,575 deletions. This committed-tree
comparison is separate from the primary checkout's 692 tracked modifications
and 1,594 untracked paths. It confirms that neither the old branch nor its
dirty checkout can be pushed wholesale without losing current `main` content.

The legacy branch also tracks `.env-backend`, `.env-gateway`, and `.env-idp`,
which are absent from canonical `main`. Their contents were not opened, copied,
or published; these paths remain explicitly excluded from any integration or
cleanup based solely on branch ancestry.

A bounded crosswalk of commit `2fd29807` (`feat: add governed agent team
operations`) found all 22 touched paths in both final trees: 9 have identical
Git blobs and 13 differ. The backend agent-team service is present in current
`main`; the differing validator, documentation, routing, progressive-section,
and agent-team UI/test files remain competing versions and were not copied.
This one-commit check is not a complete semantic audit of the remaining 291
legacy-only commits or the full 2,071-path tree difference.

## 2026-10-13 staged-source reconciliation

The `codex/hai-local-source-reconciliation` worktree is based on
`3b15fe4a08b10bb2e15afe15640bd6488cebff5d`, an ancestor of refreshed `main`,
and has 310 staged tracked-file changes: 7,138 insertions and 12,453 deletions.
The staged diff remains local and is not included in this PR. It is a candidate
source snapshot, not a validated integration checkpoint.

A bounded review of representative diffs found behavior changes that must be
resolved before publication:

- `backend/internal/ambient/scheduler.go` removes several scheduler status
  updates, removes the explicit legacy-fallback gate, and no longer records
  lifecycle launch rejection. This changes observability and startup policy.
- `backend/internal/operations/service.go` replaces raw-evidence SHA-256
  comparison and digest updates with direct JSON-string comparison, removing
  the digest helper and its linkage from this path.
- `idp/internal/app/authentication/auth_service.go` removes request contexts
  and their cancellation checks from logout, refresh, and authentication
  methods.

These examples are sufficient to reject a wholesale push, but are not a full
review of all 310 paths. The remaining staged changes must be classified and
validated individually or in coherent, testable groups. Do not delete this
worktree or related source until its full diff and any local-only data have
been accounted for and the accepted changes are preserved in GitHub.

At this audit point PR #40 remains open at `01d4d3285cfc16c802b151ea963b97c12d6d4b7d`.
The latest observed hosted run had 34 checks passed and two still pending
(backend tests and the whispercpp runner); this is not a final green result.

### October 13, 2026: PR #40 merge and source-connector reconciliation

The earlier status above is superseded. GitHub now reports PR #40 merged at
2026-10-13 07:16:39 UTC as merge commit
`59245962d2526351e3820237f3dbaf95d5e91740`; all 36 reported checks concluded
success. The merge includes verification source-snapshot linkage and its
regression test, the runtime-control model update, and this ledger. The local
`origin/main` ref was refreshed to that exact merge commit.

The primary checkout still points at legacy commit
`e07b9daeb3ba2630ecbe12a64d948f791203c241`. Relative to refreshed `main`, its
branch has 292 unique commits while `main` has 10 unique commits; this is
ancestry, not a count of missing features. The direct committed-tree diff is
2,074 paths (6 added, 1,192 deleted, 876 modified), with 20,646 insertions and
395,190 deletions. The six branch-only paths are three local environment files,
two older-numbered migration files, and an Nginx configuration. Environment
file contents were not opened. The active-dedupe index represented by the
older migration is already present on `main` as migration 0006; the Nginx file
needs comparison with the canonical template before any reuse. This confirms
that neither the legacy commit history nor its checkout is a safe wholesale
integration source.

A bounded review of `backend/internal/source` in the 310-file staged candidate
found changes that should not be ported as written:

- It removes the Trello quiet-board activity cursor and its regression cases;
  several fixtures now return an empty action list even for activity queries.
- It removes handling that retires queued work after a source is paused or
  unavailable, and removes terminal scheduled-sync failure reporting into a
  review workflow, along with tests for both behaviors.
- It removes the owner-scoped source overview and connection-health summary
  handlers.
- It logs the number of due sources rather than the number of newly enqueued
  jobs, which can overstate work when jobs already exist. It also changes the
  default poll interval from one minute to five minutes; this latency tradeoff
  needs a product decision rather than being accepted incidentally.

This is a static, package-scoped review; no tests were run against the staged
candidate, and no candidate source files were copied or deleted. The complete
310-file diff and the other dirty worktrees remain unclassified. In the primary
checkout, none of the 1,594 untracked files was an exact same-path blob match
for refreshed `main`; 85 paths share a name with a main file but have different
contents. The 692 tracked changes and all other dirty worktrees remain
preserved. No additional local cleanup is justified by this pass.

## 2026-10-13 resumed audit and PR gate

The current remote `main` remains `59245962d2526351e3820237f3dbaf95d5e91740`.
PR #41 (this ledger update) and PR #42 (pursuit completion HTTP-status test)
each passed all 36 CI checks and are reported cleanly mergeable, but both remain
open. Their branch contents are not yet on `main`; neither corresponding local
worktree qualifies for cleanup under the exact-main-match rule.

The refreshed worktree inventory contains 13 registered worktrees. The primary
checkout has 2,286 changed or untracked status entries. The source-reconciliation
worktree has 310 staged changes (7,138 insertions and 12,453 deletions); its
base commit is an ancestor of current `main`, not the current `main` commit.
Ten historical/source worktrees remain dirty. No files were deleted.

A further static review of the staged candidate found more reasons not to
publish it wholesale:

- `backend/internal/task/service.go` deletes 254 lines and
  `state_repository.go` deletes 164 lines; task operation tests lose 154
  lines, while the phase-two module deletes 57 lines and workflow handler tests
  lose 87 lines. These broad removals have not been mapped to replacement
  behavior or validated against current `main`.
- `backend/internal/verification/service.go` replaces the caller context with
  `context.Background()` for source-evidence resolution and semantic memory
  projection, and removes cancellation-error propagation at those boundaries.
  Current `main` contains a cancellation regression test for blocked evidence
  search; the staged candidate has not been run through tests.
- Authentication changes include removal of context preconditions from token
  and password-reset service paths. They need a focused contract-by-contract
  comparison before any port; no IDP candidate files were copied.

This expands, but does not complete, the staged-snapshot review. The 310-file
candidate, 692 tracked primary-checkout changes, and dirty historical worktrees
remain unclassified. Green checks on PRs #41 and #42 validate only those PR
heads; they do not validate or authorize publishing the staged candidate.

### October 13, 2026: post-merge local inventory refresh

PR #41 and PR #42 are now merged. GitHub reports `main` at
`71102fd40a960b59b70e56f902cb4b3dc91f1390`. Their two clean PR worktrees were
removed only after verifying their contents against `main`; other dirty
worktrees and transcript archives remain preserved.

The primary checkout remains on legacy commit
`e07b9daeb3ba2630ecbe12a64d948f791203c241`. Its current status has 682 tracked
changes and 1,594 untracked paths. Comparing its working files with the exact
`main` tree found 446 tracked-file contents that match a same-path `main` blob
and 236 tracked-file variants that do not. This is not permission to remove
those 446 files from the active checkout: they are mixed into a dirty working
tree that remains on an old, divergent base. A direct working-tree comparison
against current `main` spans 1,626 paths, so the checkout must not be published
wholesale.

Of the 1,594 untracked paths, 1,504 are under `output/`. The other 90 paths
include 74 whose path already exists on `main` but whose local content differs,
and 16 paths absent from the `main` tree. Those 16 are four local orchestration
state files, ten PostgreSQL/race-test evidence files, and two acceptance log
files; they are not product-source additions. No untracked source file was an
exact same-path blob match. Test evidence, orchestration state, and output logs
remain local and have not been published or deleted.

The existing `backend/internal/openclawmaintenance` implementation on `main`
already persists a 24-hour check deadline and schedules against that deadline;
the local untracked `worker.go` and `pull_client.go` are divergent copies of
files already present on `main`, not missing product modules. The local variants
were not copied over the canonical implementation.

This is a bounded inventory update, not a full semantic review of the 236
tracked variants, the 74 differing same-path untracked files, the 310-file
staged source-reconciliation candidate, or the ten remaining dirty historical
worktrees. No additional local files were deleted in this pass.

A conflict-marker scan of the current worktrees found unresolved merge content
in four historical resolution snapshots: `hai-pr34-conflict-fix` has 95 files
with 280 `<<<<<<<` markers; `hai-pr36-integration` has 254 files with 860;
`hai-pr36-local-delta` has 59 files with 183; and `hai-resolve-pr35` has 199
files with 1,208. The smaller PR #36 local-delta snapshot also contains an
unresolved marker in `backend/internal/background/worker.go` and a variant
that removes the canonical secret-redaction path. These snapshots cannot be
published or treated as resolved code. They remain preserved for file-by-file
recovery against current `main`.

## Live integration checkpoint (2026-10-13)

This addendum records a fresh remote-state check. It does not replace the
historical inventory above or claim that the full local/archive review is done.

- Repository remote: `Robert-Velhorst/018-HAI`.
- Remote `main` observed at `e7305ccadc84ef9647fe4d05245eb05813021e8d`.
- PR #36 is merged at `c31cec1ba2a25fcfd280b1f2f499297e54d96aa4`.
- PR #41 is merged at `71102fd40a960b59b70e56f902cb4b3dc91f1390`.
- PR #42 is merged at `b1caa0b074793e794ea8bc340cba9ac562924ff7`.
- PR #46 is merged and its merge commit is the observed `main` head above.
- PR #112 (`security: refresh agent runner dependencies`) remains open at
  `34b16209c2c7344e9f38bf2d19e1d6b58384d7f2`; its 40 reported checks were
  queued at observation time.
- PR #116 (`test: cover Angular worker parser fallback limits`) remains open
  at `472eb9e4b204454a9d31e5e96a222a549702f5fa`; its 40 reported checks were
  queued at observation time.
- The primary local checkout remains on `codex/hai-runtime-release` at
  `e07b9daeb3ba2630ecbe12a64d948f791203c241`, divergent from current `main`.
  Its last measured inventory was 692 tracked changes plus 1,594 untracked
  paths (2,286 total). This is not an approved or safe-to-publish aggregate.
- The semantic review of local variants, the 310-path staged reconciliation
  candidate, historical worktrees, and the 20.7 GB transcript archive remains
  incomplete. Transcript crosswalk and archive-integrity gaps remain open.
- No local HAI source, worktree, transcript, or archive data was removed in
  this checkpoint. The cleanup-readiness gate was not rerun or declared passed.

The queued PR checks are not evidence of successful CI. Merged PRs establish
that their recorded changes are on `main`; they do not establish that similarly
named local copies are safe to delete. Any later cleanup requires a fresh,
path-specific content comparison and independent safety review.

## Path-level review update (2026-10-13)

A fresh local inventory confirms 692 tracked paths reported modified and 1,594
untracked paths. The 1,504 paths under `output/` remain outside this source
review. Using Git's configured clean filters, among the 90 non-output
untracked paths, 74 have a same-path file on `main` but different content,
and 16 are absent from `main`; none of those 90 was an exact same-path blob
match. The 16 absent paths are four local
orchestration state files, ten test-evidence logs/JSON artifacts, and two
browser acceptance logs, not product-source additions.

Correction: the initial comparison with `core.autocrlf=false` did not apply
Git's configured clean filters consistently and overstated the differing-file
count. It is superseded by a normalized comparison using the repository's
configured filters. Of the 682 tracked content-diff paths against the old
local `HEAD`, 446 match current `origin/main`, 235 differ, and one path is
local-only: `nginx-config/nginx.conf`. Ten additional tracked status entries
did not appear in the normalized content diff; their index/status discrepancy
still needs reconciliation. The local-only Nginx file is withheld from
publication pending review of whether it is generated/runtime configuration
rather than source; its contents were not copied to a PR.

The 235 normalized differing paths are concentrated in `backend/internal`
(120), `frontend/src` (65), and `idp/internal` (16); two are in
`frontend/e2e`, two in `services/promptfoo-runner`, two in `backend/migrations`,
and the remaining 28 are distributed across other source, tests, scripts,
configuration, and documentation paths. This is a review-priority inventory,
not evidence that a local variant is newer or should be ported.

One candidate was reviewed semantically: the local untracked
`backend/internal/background/worker_reliability_test.go` differs from the
same-path test on `main`. The main version already includes the worker policy,
failure-reporting, and approval checks, and more strongly verifies that fresh
source-derived work remains approval-gated and feed errors are sanitized. The
local variant is not a safe improvement and was not ported.

These counts are inventory evidence, not a cleanup authorization. The 235
tracked variants, the ten status-only entries, all 74
untracked same-path variants, and the Nginx configuration remain subject to
file-level review. No source, worktree, transcript, or archive data was
removed.

Representative semantic comparisons of same-path local variants also found
that current `main` is stricter than the local copies:

- `backend/internal/operations/source_approval.go`: current `main` binds the
  reviewed owner type and source-evidence hash in its approval receipt and
  validates receipt history more explicitly; the local variant lacks those
  bindings. Do not replace the canonical implementation with the local copy.
- `backend/internal/operations/execution_claim.go`: current `main` uses the
  persisted `payload_json` field, preserves operation/event timestamp binding,
  and does not treat owner type alone as approval proof. The local variant has
  weaker receipt/eligibility checks.
- `backend/internal/infra/database_pool.go`: current `main` configures bounded,
  parameterized GORM logging and validates the log-level setting; the local
  variant omits that configuration.
- `backend/internal/source/trello.go`: current `main` includes a quiet-board
  cursor to skip full reconciliation when the action feed proves there is no
  card activity; the local copy lacks that optimization.
- `backend/internal/agentruntime/handler.go`: current `main` rejects cancelled
  ecosystem uploads; the local handler lacks that cancellation guard.
- `frontend/src/app/control-room/app-shell.component.ts`: current `main` adds
  page-title updates and restores keyboard focus around mobile navigation;
  those behaviors are absent from the local variant.

These are examples from the ongoing review, not a complete audit of the 235
different tracked paths. They provide additional reasons not to publish the
dirty checkout as a whole; any remaining local change must be compared to the
current implementation and its safety contracts before porting.

## Refreshed local-to-main path comparison (2026-10-13)

The primary checkout was rechecked against the current `main` commit
`e7305ccadc84ef9647fe4d05245eb05813021e8d`. Its inventory remains 692 tracked
status paths and 1,594 untracked paths. Restricting the comparison to those 692
tracked paths, Git's configured filters report 238 paths whose working-tree
content differs from current `main`. This fresh path-scoped result supersedes
the earlier 235-path count above; it is not a whole-tree equivalence result.

The 238 differing paths group as follows: `backend/internal` (122),
`frontend/src` (65), `idp/internal` (16), `frontend/e2e` (2),
`services/promptfoo-runner` (2), `backend/migrations` (2), and 29 paths across
scripts, documentation, configuration, and other service/build files. This
classification is for review prioritization only and does not imply that a
local version is newer or suitable to publish.

The latest review of the shared frontend shell and Control Center found local
changes that should not replace current `main`: the root app title is changed
to the generic literal `app`; the root module drops the `ControlRoomModule`
registration; the module registry removes route/Advanced-section entries; the
progressive-section template drops its `data-hai-section` marker; and the
Control Center switches away from summary endpoints while weakening required
count validation. These are regressions or behavior changes requiring a
separate intentional design decision, not safe integration candidates.

The existing PR #116 is a separate focused candidate: its Angular worker
parser contract test was run from the PR worktree on 2026-10-13 and passed all
5 tests. This is local test evidence only; the PR's hosted checks were pending
at the time of inspection. No source, test, worktree, transcript, diagnostic,
or archive files were deleted during this checkpoint.

Additional high-risk/backend and workflow UI comparisons against the same
`main` commit found more local variants that must not be imported as-is:

- `backend/internal/agentruntime/handler.go` removes request-cancellation
  checks around managed OpenClaw ecosystem upload inspection and persistence.
- `backend/internal/ambient/scheduler.go` removes scheduler status reporting
  and the explicit legacy-fallback opt-in gate.
- `backend/internal/executionapproval/composite_resolver.go` removes the
  dedicated ops-control review resolver and validated registration of
  additional approval namespaces.
- `backend/internal/operations/service.go` replaces the persisted raw-evidence
  digest comparison with direct JSON-string equality and stops refreshing the
  evidence digest.
- `backend/internal/router/identity.go` stops propagating the validated
  operator scope needed by owner-control authorization.
- `backend/internal/source/trello.go` removes the action-feed quiet-board
  cursor optimization already present on `main`.
- `frontend/src/app/pages/governance-control/governance-control.component.ts`
  removes summary/lazy loading for execution and learning records and loads
  several advanced panels on every refresh.
- `frontend/src/app/pages/runtime-lab/runtime-lab.component.ts` eagerly loads
  feature-parity and capability diagnostics instead of deferring them until
  requested.
- `frontend/src/app/pages/task-blueprint/task-blueprint.component.ts` reduces
  bounded long-running execution and approval timeouts to 20 seconds and
  removes the guarded mapping from completed MCP calls to their returned
  context records.
- `frontend/src/app/pages/workflow-engine/workflow-engine.component.ts`
  removes success-criteria intake, weakens the pre-dispatch selected-item
  check, and turns a review-required result into a generic uncertain outcome
  instead of pausing and refreshing the governed work item.
- `frontend/src/app/pages/framework-registry/framework-registry.component.ts`
  removes preference-history loading and its view-mode/section handlers.

These examples increase confidence that the divergent working tree cannot be
published wholesale. They do not complete the semantic review: the remaining
238 tracked differences, 74 same-path untracked variants, staged
reconciliation worktree, historical worktrees, and transcript archive still
require path-level disposition. No additional product-code candidate was
identified in this review batch.

## Untracked-overlay and staged-snapshot review (2026-10-13)

The primary checkout's non-output untracked set was re-enumerated against
current `main`: 74 paths overlap a `main` path with different content, and 16
paths are absent from `main`. A Git numstat comparison of the 74 variants
showed no distinct product-source addition that is ready to publish. Specific
variants reviewed include:

- `services/promptfoo-runner/package.json` selects Promptfoo `0.123.1` instead
  of `main`'s `0.124.1`, and removes the explicit `@libsql/linux-x64-gnu` pin
  and `get-uri`/`basic-ftp` override. Its larger lockfile is not evidence of a
  newer or safer dependency graph; neither file should be imported.
- `scripts/test-frontend-build-resource-limit.cjs` only proves that Angular's
  parser accepts invalid numeric values. PR #116 contains the stronger
  fallback assertions (including `hasCustomMaxWorkers=false`) and passes its
  five focused Node tests locally, so the untracked version is not an
  additional candidate.
- `scripts/test-hai-phase2-state-permissions-contract.ps1` expects a local
  ownership-migration variant that adds `DAC_READ_SEARCH`. The surrounding
  local Compose file also removes unrelated source-discovery and environment
  configuration. This paired local variant is not being split or published
  without an independent security and runtime validation.
- `backend/internal/runtimelab/owner_gate_handler_test.go` differs only by one
  explanatory comment; it has no functional delta to port.

The 16 paths absent from `main` are local orchestration state, test-evidence
artifacts, and browser-acceptance logs. The 1,504 `output/` paths were not
content-reviewed and remain explicitly outside this source batch. None of
these files was removed.

The `D:\hai-source-reconciliation` worktree was rechecked with a
per-command-only Git ownership exception. It has 310 staged paths and no
untracked paths; its `HEAD` is seven commits behind current `main`, with no
unique committed path ahead. Comparing the staged snapshot directly with
current `main` reports 316 path changes (148 `backend/internal`, 65
`frontend/src`, 26 `idp/internal`, plus migrations, services, scripts, and
configuration). This is not a merge-ready PR: it contains the previously
reviewed approval, evidence, scheduler, and authorization regressions, and no
wholesale cherry-pick or staged commit was made.

The `D:\hai-transcript-report-retention-gate` worktree is clean and tied to
merged PR #46. Its two changed script files were compared with current `main`
and are exact content matches; the broader worktree and transcript archive
were not deleted or declared safe to remove.

## Backend governance and replay review (2026-10-13)

The next primary-checkout backend batch also does not qualify for publication
as-is:

- `backend/internal/a2abridge/handler.go` removes the process-local bounded
  replay cache keyed by `messageId`, so retries can create fresh planning
  drafts instead of reusing the first response. It also stops using the
  strict decoder that rejects unknown request fields and removes the separate
  JSON-RPC ID validation. The reduced handler still caps the body and rejects
  trailing JSON, but that does not preserve the prior replay/validation
  contract.
- `backend/internal/a2abridge/service.go` reduces the bridge service
  implementation and tests; the removed coverage needs semantic review before
  any extraction is considered.
- `backend/internal/agentguidance/guidance.go` and its tests are deleted. This
  removes catalog-matched, consent-pinned guidance validation and safe
  provenance pins from the proposal-runner enrichment path.
- `backend/internal/agentcycle/service.go` removes `RunContext`, cooperative
  cancellation checkpoints, and owner-scoped source/workflow/ambient
  contracts. The replacement operator path explicitly limits itself to
  owner-scoped context and pursuit refreshes, while the prior path ran the
  authorized owner-scoped operating phases. This is a material capability
  and contract change, not a safe mechanical port.

No code from this batch was published. Focused Go package tests were attempted
for `a2abridge`, `agentcycle`, and `agentguidance`, but could not start because
the `go` executable is not available on this shell's `PATH`; this batch has
therefore not been locally compiled or tested.

## Account-feed and Trello connector review (2026-10-13)

The source-ingestion comparison confirms that the local checkout is missing
substantial behavior already present on `main`:

- The diff removes 25,522 lines across 102 `backend/internal/source` and
  `backend/internal/accountfeed` files. Among the deleted implementations are
  Trello durable sync, sync repositories, webhook ingestion/reconciliation,
  extraction correction/patch workflows, Google OAuth retry/lifecycle logic,
  and their persistence, security, cancellation, and integration tests. The
  checkout cannot be treated as a source of a complete live Trello connector.
- `backend/internal/accountfeed/permission_registry.go` now marks
  `Granted=true` whenever credentials are present. `main` additionally
  requires the bridge's `ReportsCredentialGrant()` signal, keeping credential
  availability distinct from the provider's declared grant. The local change
  weakens that consent distinction and must not be published.
- The local `backend/internal/source/trello.go` removes the quiet-board cursor
  optimization, but that isolated deletion does not restore the durable
  sync/webhook infrastructure removed elsewhere in the same package.

No source-ingestion code was ported in this batch. Live Trello behavior,
provider OAuth flows, and the deleted persistence/reconciliation contracts
remain unverified from this checkout; the Go toolchain limitation recorded
above still applies.

## Identity-provider and authentication review (2026-10-13)

The local `idp/internal/app` overlay is not safe to publish as a group. The
comparison removes 7,472 lines across 75 files, including critical controls
and the tests that enforce them:

- It deletes `public_auth_rate_limit.go`, `password_reset_rate_limit.go`,
  refresh-token rotation/family receipts, durable session repositories, and
  extensive Redis/Postgres/session recovery integration tests.
- `authentication/auth_service.go` removes request-context cancellation from
  logout, authentication checks, refresh, and password-reset delivery. It
  also removes configured owner-scope claims from issued access tokens, which
  breaks the owner-scoped authorization contract used by HAI.
- `services/smtp_password_reset_sender.go` detaches SMTP delivery from the
  request context, so cancellation no longer closes an in-flight connection.
- `config/postgres_config.go` removes required username/password checks and
  production placeholder/weak-password validation; its port condition also
  permits `0`, which the main implementation rejects.
- `users/user_handler.go` replaces checked context type assertions with
  unchecked assertions and removes a nil-result guard before dereferencing an
  updated user, creating panic paths where the main implementation returns a
  controlled error.

No IDP files were ported from this overlay. The relevant authentication,
database-configuration, password-reset, and account-isolation tests could not
be run locally because the Go executable is unavailable on this shell's
`PATH`; remote CI status for PR #117 is still queued and does not verify this
local overlay.

## Automation and execution-control review (2026-10-13)

The local overlay changes 145 files across `backend/internal/automation`,
`executionauth`, `opscontrol`, and `task`, with 16,900 deleted lines. The
removed files and tests include several active safety boundaries:

- `opscontrol/approval.go`, its repositories/resolver, and `resume_review.go`
  are absent locally, removing the durable approval and reviewed-resume
  implementations present on `main`.
- `executionauth/transactional.go` and its tests are removed, as are task
  cancellation, tool-outcome persistence, review acknowledgement,
  required-participant admission, and read-only runtime contracts.
- The automation overlay removes stop-request/replay/recovery bindings,
  emergency-stop admission tests, launch-admission/context bindings, and
  platform-specific process-launch implementations and identity tests.
- `opscontrol/handler.go`, `task/handler.go`, `task/service.go`, and state
  repositories are modified alongside those deletions, so this is not merely
  removal of obsolete tests; it changes the live execution and recovery path.

This batch was not ported or locally tested. Until the complete set of
approval, transactional, cancellation, process-launch, and recovery contracts
is restored or independently replaced and verified, no part of this overlay
should be used to replace the current `main` implementation.

## Database migration and rollback review (2026-10-13)

The local overlay changes 193 files under `backend/migrations` and removes
14,629 lines. It deletes migrations and their contract/integration tests for
Trello resumable sync and signed webhook intake, source cursor/idempotency and
evidence identity, approval provenance/review receipts, execution
confirmation/cancellation fences, OpenClaw artifact ownership/recovery, and
task/workflow history. The migration-chain contract test is also modified,
while rollback preflight, guarded rollback, and dedicated test-database safety
checks are removed.

Because these migrations define persistent schema and sequence compatibility,
restoring or cherry-picking individual SQL files without the matching code,
rollback path, and migration-chain proof could corrupt an existing database.
No migration was ported or executed from this overlay. The local schema tree
is not a safe replacement for `main`; migration tests were not run because the
Go executable is unavailable on this shell's `PATH`.

## Frontend route and workflow review (2026-10-13)

An exact route extraction from `frontend/src/app/app-routing.module.ts`
confirms that the local overlay has 31 route declarations versus 32 on
`main`; `/operational-brain` is the one removed route. This is a user-facing
module loss, not a cosmetic redesign. Existing review also found local page
changes that remove preference-history behavior, eagerly load advanced
governance/runtime data, shorten bounded task execution timeouts, discard
guarded MCP result-to-context mapping, and remove workflow success-criteria
intake and review-required state handling. These behavioral changes need
targeted UI/API acceptance checks before any frontend extraction; none was
ported in this batch.

## OpenClaw and runtime boundary review (2026-10-13)

The local overlay removes 17,895 lines across 82 files in
`agentruntime`, `openclawmaintenance`, and `openclawreconcile`:

- It deletes the OpenClaw maintenance service/worker, version-pull lifecycle,
  installer lock, trusted-command policy, and Windows process-containment
  implementation. The requested daily freshness/update behavior is therefore
  not present in this local overlay.
- It deletes managed-archive import/download validation, artifact URL and
  ownership checks, gateway-frame handling, and artifact retain/recover/forget
  reconciliation, including usage recovery and their tests.
- It deletes execution-admission code and DeepSeek harness stop, cancellation,
  and workspace-symlink security tests.
- The remaining `agentruntime/handler.go` removes request-cancellation checks
  after upload inspection and immediately before managed archive persistence;
  related tests asserting that canceled uploads do not consume authorization
  or mutate runtime state are deleted.

This is a major loss of updateability, artifact safety, and runtime control,
not an alternative harness implementation. No runtime code was ported from the
overlay. Local tests could not be run because Go is unavailable on this shell's
`PATH`; no claim of live OpenClaw installation or update verification is made.

## Current July archive state check (2026-10-13)

A fresh read-only inventory of `D:\codex-temp\hai-completed-agent-sessions`
now finds 13 files totaling 17,559,729,527 bytes, rather than the 18 files
and 20,739,169,122 bytes in the prior checkpoint. All five manifest rows
marked `candidate_after_ledger_commit` are absent. The other 13 paths match
the manifest's retained path and byte-length set exactly; their full contents
were not re-hashed in this check.

No deletion was performed by this audit. The observed absence does not prove
who removed the five candidates or when. The existing 18-file source verifier
and remover are now expected to fail closed because the candidate source paths
are missing; they were not run in apply mode. Preserve all 13 remaining
transcripts, including duplicate-ID and nonterminal rows, and do not revise
their dispositions from this inventory alone.

## Operations, memory, and framework-registry review (2026-10-13)

The local overlay changes 72 files across `operations`, `memory`,
`braincatalog`, and `frameworkregistry`, removing 16,430 lines compared with
`main`. This batch removes provenance and owner-safety contracts rather than
offering an equivalent implementation:

- `operations/service.go` removes context-bound source-observation intake,
  validation of evidence JSON and owner/workspace/source identity, source-head
  publication, legacy-dedupe migration detection, and raw-evidence digest
  comparison. Duplicate evidence is now compared as a trimmed JSON string and
  the source-evidence digest is not refreshed. Create/update and audit-event
  persistence are split into separate calls, and event append errors are
  discarded; the atomic mutation and execution-claim/fencing interfaces are
  removed.
- `memory/handler.go` removes request-body size bounds, review-managed memory
  write restrictions, explicit authenticated-owner validation, and response
  redaction of internal `SourceExtractionID`. A missing owner-scoped service
  now panics instead of returning a controlled unavailable response. Memory
  query also changes from the owner-aware query service to loading all records
  then filtering in memory, which loses database-side pagination and request
  cancellation.
- `frameworkregistry/operating_context.go` removes the health-vs-infrastructure
  disambiguation, so phrases such as “runtime health check” can be interpreted
  as a personal health signal. It also removes verification that required
  agent roles are backed by verified available agents or an explicitly
  registered embedded task-engine dependency.
- `braincatalog/catalog.go` removes the bounded, read-only ChatGPT/Codex history
  MCP integration entry and weakens the Ollama inference attestation text: a
  loopback URL and model-list response are again described as enough for local
  selection even though neither proves the inference path or billing behavior.
  The Promptfoo verification date/version note is also moved backward from
  `2026-09-27`/`v0.123.1` to `2026-07-20`/`v0.121.19`; this is stale evidence,
  not a new verification.

No implementation from this batch was ported. These regressions are concrete
reasons to preserve the canonical `main` versions and reject the local overlay
as a replacement. The Go test suite was not run because Go is unavailable on
this shell's `PATH`; this review is a source diff audit, not runtime proof.

## Untracked-file candidate screen (2026-10-13)

A read-only inventory excluded generated `output/`, agent/swarm state, log
files, and test-evidence payloads, leaving 74 untracked source, test, script,
package, and documentation paths in the primary checkout. All 74 paths also
exist in the freshly confirmed `origin/main` tree, but none is text-identical
after normalizing CRLF/LF line endings. Four representative operations files
(`ingest_atomicity_regression_test.go`, `source_approval.go`,
`source_approval_test.go`, and `repository_ordering_test.go`) are smaller local
variants than their corresponding `main` versions. This is not proof that all
74 variants lack unique useful lines; each still needs a semantic diff review
before either import or deletion. No candidate was ported or deleted.

The local-only `nginx-config/nginx.conf` is not mounted by
`docker-compose.local.yml`, which renders `nginx-config/nginx.conf.template`
with `envsubst` and the configured backend key. The static file contains the
literal `change-this-local-backend-key`, so it is not a safe deployment
artifact to add as-is. The local `kafka/docker-compose.yaml` already has a path
in `main`; its local variant uses mutable `latest` images, PLAINTEXT listeners, and
host-published broker/control-center ports, so it is not a suitable new HAI
deployment capability without a separate threat and configuration review.

The untracked screen therefore produced no exact-match cleanup candidates and
no immediately safe code import. No files, logs, evidence, worktrees, or
archives were removed.

## Small additive-diff review (2026-10-13)

The most additive tracked changes were checked against `main` and their
consumers before treating them as candidate improvements:

- `backend/internal/verification/repository.go` drops the 200-run history cap
  and changes owner lookup to include ownerless legacy rows for any non-empty
  owner. `main` permits those rows only for the explicitly authorized migration
  owner, so the local variant weakens account isolation and can load unbounded
  history.
- `frontend/src/app/control-room/module-registry.ts` removes progressive
  section ancestry/prefix validation, direct-section resolution, and module
  metadata used by the shared control-room shell. The local registry is a
  contraction of the shipped navigation/disclosure contract, not a compatible
  extension.
- `frontend/src/app/services/context-memory.service.interface.ts` removes the
  query API and page/query result types. `runtime-control.service.ts` changes
  resume to `/background/resume/approval`, while `main` registers
  `/background/resume-approval` and the corresponding approve-and-resume
  route; the local call does not match the current approval workflow.
- `scripts/two-account-isolation-test.sh` replaces the shared
  `smoke-auth.sh` token helper with an inline JWT implementation. `main` has a
  contract test requiring both this script and other smoke tests to use the
  shared helper, so the local change breaks the established test contract.

These candidate diffs were not ported. Their main-side contract evidence was
checked with `git grep` and direct source comparison; no live API calls or Go
test execution were performed. At this screen's check, GitHub had not returned
a workflow run for the audit PR; CI was not counted as passed.

## LLM provider and model-maintenance review (2026-10-13)

The local overlay changes 31 files under `backend/internal/llm`, deleting
9,919 lines. This removes runtime safety and the provider-maintenance controls
needed for the requested daily freshness check:

- `paid_budget.go` and its repository/tests are removed. `main` reserves
  estimated input/output cost in durable storage before dispatch and fails
  closed when paid calls, pricing, or the budget repository are unavailable.
  The local policy path does not retain that pre-dispatch reservation gate.
- `outbound_transport.go` and protocol tests are removed. `main` disables
  redirects and environment proxies for provider calls, pins resolved IPs for
  dialing, blocks remote providers resolving to private/local addresses, and
  restricts local provider DNS to an approved address scope. Without this
  transport, configured provider URLs no longer receive these SSRF and
  redirect protections.
- `model_maintenance_admission.go`, cooldown policy, provider-probe scheduler,
  and their database/integration tests are removed. The durable admission uses
  a lease and fencing token before local model network I/O. Local scheduling
  no longer preserves those admission, cooldown, or probe guarantees.
- Maintenance changes remove context propagation/cancellation, provider-managed
  version handling, verification of prior local-version evidence, and special
  handling for cloud-backed Ollama models. The local code can treat a recent
  stored record as reusable without the provider-specific evidence checks in
  `main`; a refresh result can therefore be trusted without proving the model
  is current or locally billed.
- The local LLM handler removes request-context error handling around provider
  probes, routing, and generation. This can leave provider work running after
  the initiating request is cancelled or times out.

No LLM implementation was ported. The main implementation is the stronger
baseline for cost control, outbound network boundaries, and daily model
maintenance. This was a source-diff review only: no provider was contacted,
no model was updated, and Go tests were unavailable on this shell's `PATH`.

## Agent skills and coordination review (2026-10-13)

The local overlay deletes the `brainskills` catalog/HTTP surface and the
`brain_skill_selection` service, owner-scoped persistence adapter, and their
tests. Those are HAI's catalog-backed, consent-scoped guidance and skill
selection paths; removing them is a capability loss, not an alternate
implementation to port.

The remaining agent changes also reduce reliability boundaries:

- `agentcoordination.Coordinator.Dispatch` drops the message-expiry deadline
  and the distinction between confirmed-not-delivered and outcome-unknown
  transport errors. The local code abandons the idempotency claim on any
  delivery error, including an ambiguous outcome, allowing a retry to
  duplicate a message that the remote agent may already have accepted.
- `agentcoordination.EvaluateMessageTimeout` removes acknowledgement
  validation and no longer waits for the next reminder interval before
  returning `TimeoutRemind`.
- `agentframework` and `crewai` remove owner-consented brain-skill guidance,
  request-scoped timeouts, bounded response decoding, and redirect rejection.
  The framework response path also drops the explicit high-risk approval
  requirement validation.
- `agentregistry` replaces `infra.PostgresExecutionDB` with plain
  `r.DB.WithContext(ctx)` for registry reads. The helper validates and binds
  the reserved execution/authorization connection, transaction scope, and
  deadline; the local query path bypasses those checks.

No agent/skill code was ported. The checks above are source-level findings
against `main`; runtime delivery behavior and Go tests were not exercised in
this environment.

## Background worker and durable-job review (2026-10-13)

The background worker diff removes the cross-process execution protocol:

- `background/lease.go` replaces the service/owner/workspace keyed run lease
  and ownership token with a mutex attached to one `Worker` instance. It cannot
  prevent two service instances or backend replicas from processing the same
  operation.
- `background/worker.go` removes durable claim acquisition, claim heartbeat,
  claim-fenced transitions, expired-claim recovery, and reporting for
  unleased running/verifying operations. The local worker no longer has the
  durable recovery/diagnostic path present on `main`.
- `background/execute.go` removes the claimed-execution, bound source
  approval, live final-effect-policy, and uncertain-outcome/reconciliation
  checks around safe worker effects. A failed response is no longer equivalent
  to evidence that no external effect occurred, so retrying from this simpler
  path can duplicate an effect.
- The `durablejob` diff removes durable review-settlement implementations and
  their migration, repository, and integration contracts. This reduces the
  persistence boundary for jobs awaiting human review and terminal settlement.

No worker or durable-job code was ported. These local implementations are not
safe substitutes for the canonical claim, approval, settlement, and recovery
contracts. Review was limited to source diffs; no multi-process execution or
database-backed job test ran here.

## Ambient, autonomy, assistant, and life-ontology review (2026-10-13)

Compared the local HAI checkout with the fetched `origin/main` ref at
`e7305ccadc84ef9647fe4d05245eb05813021e8d`. This is a source-level review;
the local checkout has no Go toolchain available for package validation.

- The local ambient overlay deletes `scan_context.go` and its context,
  repository, service, acknowledgement, and scheduler tests. The canonical
  path checks request cancellation and the current background-execution
  policy at operation boundaries, scopes retained scan history by owner, and
  verifies terminal scan persistence before reporting completion. The local
  simplified path does not preserve those guarantees.
- The local ambient scheduler removes the second explicit opt-in for legacy
  in-process scheduling and its scheduler-health records. If durable startup
  fails, the code now falls through to the in-process scheduler without the
  canonical `DURABLE_SCHEDULER_LEGACY_FALLBACK_ENABLED` gate. It also changes
  the default poll interval from one to five minutes. No scheduler was run.
- The local autonomy overlay deletes `public_projection.go` and its tests.
  The remaining `autonomy/handler.go` still calls `publicOverview`, while no
  definition remains in the local autonomy package. This is a statically
  evident compile blocker. The deleted projection also redacted secrets and
  removed raw action payloads, world-state snapshots, and stress results from
  public telemetry responses; those protections must remain when repairing
  the compile break.
- The local assistant service removes context-aware task and agent-cycle
  execution. It also stops propagating agent-cycle errors into the result's
  review-required state. This weakens cancellation and error visibility for
  long-running or failed operations.
- The local life-ontology PostgreSQL adapter deletes bounded query methods for
  entities, relations, and merge proposals, including owner normalization,
  query validation, local-only filtering, temporal filtering, and result
  limits. The adapter is therefore not a safe replacement for the canonical
  query surface.

No code from this overlay was imported or pushed. The autonomy compile blocker,
ambient fallback behavior, and removal of context and owner boundaries must be
resolved before considering any of these files for a release. `git diff
--check` passed for these paths; Go tests and live database/provider behavior
were not verified.

## Local release-branch reconciliation snapshot (2026-10-13)

The currently inspected checkout is on `codex/hai-runtime-release` at
`e07b9daeb3ba2630ecbe12a64d948f791203c241`; GitHub `main` was independently
confirmed at `e7305ccadc84ef9647fe4d05245eb05813021e8d`. The merge base is
`91c8620c557229f1da4ed15fcbb7088c6a6947a7`, with 292 commits unique to the
local branch and 16 commits unique to `main`. Its working tree has 692 tracked
changes and 1,594 untracked paths. Comparing the local worktree to current
`main` reports 1,628 paths: 1,192 deletions, 430 modifications, two exact
renames, and four additions. This is not a reviewable release diff and must
not be pushed as one unfiltered branch.

The four added paths include `.env-backend`, `.env-gateway`, and `.env-idp`
from the local branch tree, plus `nginx-config/nginx.conf`. The environment
files are absent from the materialized working tree, and their values were
not read or disclosed. They are not approved for publication: their secret
status and historical exposure have not been verified. The added Nginx config
also contains a hard-coded backend-key placeholder (`change-this-local-backend-key`);
it needs a secure configuration contract before any deployment use. None of
these paths was pushed by this audit.

The 1,594 untracked paths are not represented in the committed-diff counts.
They remain in place pending path/content-level classification; a matching
filename or a test result does not establish that a local file is redundant
or safe to delete. No local file was deleted during this reconciliation.

An exact blob comparison screened all 1,594 non-ignored untracked paths against
the current `main` tree. Eighty-five paths share a path with a tracked file on
`main`; zero had the same Git blob hash. This exact-byte check found no
untracked file eligible for removal under the direct-match cleanup rule. It
does not prove that any unmatched file is disposable; those remain preserved.

The added local `nginx-config/nginx.conf` was compared with the existing
canonical `nginx-config/nginx.conf.template`, `sites-enabled` configuration,
and `test_gateway_contract.py` on `main`. The local static file hard-codes the
placeholder backend key and omits the canonical envsubst-based key injection
and associated gateway contract. It is an older/insecure duplicate, not a
missing capability to port. The template/tests are on `main`; no local gateway
file or secret material was pushed.

## Untracked same-path source review (2026-10-13)

The 85 untracked paths that collide with paths in the current `main` tree are
not byte-identical copies. A targeted comparison of the highest-risk
operations files confirms that path overlap does not mean missing code on
`main`: `execution_claim.go`, `execution_claim_memory.go`, `source_approval.go`,
and their tests all already exist on `main` with the same exported/internal
claim APIs. No copy was imported.

The local `execution_claim.go` variant uses the older `payload` event field in
its source-approval query; current `main` uses `payload_json`. More
importantly, `main` validates the durable exact-revision approval receipt at
claim time and documents that `OwnerType` is not authorization proof. The
local variant adds an `OwnerType == OwnerRobert` gate instead. That is not a
safe substitute for the durable receipt contract and may not match the current
database schema. This representative mismatch supports keeping the local
candidate unported until each colliding path is individually reviewed.

This was a source comparison only. The Go toolchain is unavailable locally,
so neither variant was compiled or tested in this checkout. No files were
deleted; the remaining 84 same-path candidates and all non-colliding untracked
source/artifact paths still require classification.

## Branch-only deployment and runtime candidates (2026-10-13)

Two representative features from the local branch history were checked against
the current `main` tree and are already superseded by stricter canonical code:

- The branch commit `fe65fd9f` exempts an admitted automatic runtime from
  uncertain personal-capacity review. Current `main` already contains that
  behavior and its test, but additionally requires
  `request.AutomatedExecution` before applying the exemption. The local branch
  checks only `admittedAutomaticRuntime(request.Risk)`, which can grant the
  exemption outside an explicitly automated execution request. Do not port
  this older variant.
- The local `kafka/docker-compose.yaml` restores a three-broker
  ZooKeeper-backed stack using floating `latest` images and multiple published
  ports. Current `main` deliberately makes this path a compatibility wrapper
  around `docker-compose.local.yml`, documenting that the retired stack must
  not be started separately. The local version would reintroduce substantial
  container resource use and deployment drift; it is not a missing HAI
  capability.

These comparisons reinforce that branch commit history is not a safe import
list: the target tree already contains selected behavior with later authority,
resource, or compatibility constraints. No runtime code or Compose change was
ported. Compose was not started and the runtime planner was not executed.

## Database safety and provisioning candidate review (2026-10-13)

The local branch weakens destructive PostgreSQL test isolation in
`backend/internal/evaluation/repository_postgres_test.go` and
`backend/internal/resilience/repository_postgres_test.go`. Current `main`
uses `pgtestguard` to require an exact dedicated test database identity and
explicit destructive-test authorization. The local variants instead accept
database names merely containing `test` (or `ci` in one case); the evaluation
variant then drops and recreates the entire `public` schema. A database name
substring is not an adequate safeguard against destructive test execution.
These changes must not be ported.

The local `scripts/provision-runtime-db-role.ps1` also replaces the canonical
containerized `backend-runtime-role` provisioning path with inline generated
SQL piped directly to `psql`, and drops the prior requirement that owner and
runtime database passwords be present and distinct. Although identifiers and
password literals are escaped in the local script, this is a security- and
operations-boundary change that needs an isolated migration/provisioning test
before consideration. No database was started or modified.
### October 13, 2026: current-main source cross-check

Refreshed `origin/main` is `549e97012e8e8b52efebf7521ff83611a36bdeb1`.
PRs #41 and #42 are merged and their merge commits are ancestors of this main
revision. PR #56 is open and contains two focused regression-test additions:
guarded Ambient Monitor
PostgreSQL release-acceptance coverage and pursuit dashboard projection
coverage. When queried before this ledger-only update, GitHub reported all 40
checks pending for its then-current head, including browser acceptance,
backend, frontend, IDP, migration, runtime, and Python runner jobs. No hosted
test pass is claimed; the ledger update triggers a new check run and the PR is
not eligible for cleanup as main-matched source.

The primary legacy checkout currently reports 2,286 status entries: 692
tracked changes and 1,594 untracked paths. A bounded path comparison of its
non-output, non-orchestration untracked files found 86 product/test/document
paths, of which 12 paths are absent from `main`; all 12 are test-evidence
records or acceptance logs, not missing product source. The 1,504 `output/`
paths and the 692 tracked changes remain unclassified at file-content level in
this pass, and nothing was deleted.

Two untracked post-migration files in the old PR #34 resolution checkout use
the name `0005_durable_jobs_queue_lease_index`; current `main` has the same
queue/lease index as post migration 0007, and its durable-job contract tests
also read 0007. The old files are stale numbering, not missing schema work.
The runtime-safety scratch checkout likewise has no untracked source paths
absent from `main`. Its OpenClaw artifact URL implementation is an older
variant: current `main` includes additional NAT64-address policy and isolated
test-only loopback controls. It was not copied over the canonical code.

Test-only differences sampled from the primary checkout were also compared
against current `main`. The local worker-starvation regression is already
covered by the current `backend/internal/background/worker_reliability_test.go`;
the local source-approval tests are a shorter variant of the current test
suite. `TestUnknownOwnerCapacityCannotOverrideMissingRequiredParticipants` is
also already present in the legacy checkout and current `main`; both versions
assert fail-closed behavior when required review participants are missing.
An earlier candidate description of a unique test that allowed unknown owner
capacity to execute without approval was not reproduced in the live file and
is withdrawn. These stale or already-covered copies were not reintroduced. The
two tests in PR #56 are distinct gaps and remain the only new source files
published from this audit batch so far.

### Staged candidate: ambient and verification package review

A further bounded comparison of the staged `codex/hai-local-source-reconciliation`
snapshot with current `main` covered the five changed `backend/internal/ambient`
files and five changed `backend/internal/verification` files. No candidate files
were copied. The differences include:

- Ambient scan removes the explicit nil-service guard, so a nil receiver can
  panic instead of returning the established unavailable-context error.
- Ambient scheduler removes health-state reporting and the second explicit
  opt-in for legacy in-process fallback. Its changed behavior would permit
  fallback on the durable-scheduler opt-out alone, weakening the current
  fail-closed operator gate.
- Ambient candidate removes batched fingerprint lookup and resolves existing
  opportunities one at a time, increasing database round trips as candidate
  volume grows. It also removes the last-seen write interval and changes the
  default poll from one minute to five minutes; those are storage and latency
  tradeoffs, not safe incidental cleanup.
- Verification candidate removes the context-aware answer API and uses the
  non-contextual source-search path, losing caller cancellation propagation.
  It also removes the source-snapshot mutation regression test, removes the
  bounded run-history limit, and lowers memory-promotion confidence/evidence
  quality thresholds from 0.75 to 0.70/0.55. These changes reduce cancellation,
  resource, provenance, and evidence-quality safeguards.

These diffs do not establish that every staged file is wrong; they establish
that this candidate cannot be integrated as a package or wholesale. Any future
port must isolate a specific desired behavior, preserve the current safety and
resource contracts, and add focused tests against the current `main` baseline.

## Reconciliation continuation checkpoint (2026-10-13)

The current remote `main` head is `752c1424009f5eb519a0a578c2ee830c25199130`.
PR #41 is merged at `71102fd40a960b59b70e56f902cb4b3dc91f1390` and PR #42 is
merged at `b1caa0b074793e794ea8bc340cba9ac562924ff7`; each PR reports 36
successful checks. The exact corresponding paths in the primary local checkout
are not byte-identical to `main`: the ledger copy has a different Git blob, and
the pursuit integrity test is an untracked file with a different Git blob. They
were not deleted.

The integration-ledger PR #117 is still open at head
`672b576a2af6b2bc4f440c0a2a249c25dc58c235`. GitHub currently reports it
conflicting with `main`. A local merge of the current `origin/main` is in
progress in its dedicated worktree. The ledger conflict contained distinct
dated audit snapshots; the conflict markers were removed while retaining the
content from both sides. This merge resolution is not yet committed, pushed,
or validated by CI.

The primary checkout remains on `codex/hai-runtime-release` at
`e07b9daeb3ba2630ecbe12a64d948f791203c241`, with 692 tracked changes and
1,594 untracked paths at this checkpoint. That aggregate is not suitable for
publication or cleanup. No HAI-related local files, archives, or worktrees
were deleted. Continue file-level source review, only port behavior that is
not already present on `main`, and verify exact content and recoverability
before any local cleanup.

## Staged source-reconciliation refresh (2026-10-13)

A current staged-tree comparison in `D:/hai-source-reconciliation` against
`origin/main` covers 333 paths: 327 modifications, five deletions, and one
addition. The only added path is `nginx-config/nginx.conf`, a static config
that duplicates the canonical template/configuration surface and contains a
placeholder secret; it is not a missing HAI capability. The deletions include
existing regression/acceptance tests. The modified paths span backend,
frontend, IDP, migrations, scripts, deployment files, and dependency locks; no
whole-tree import is justified by the path count or build status alone.

One frontend difference proposed to select the first pursuit automatically
after list loading was tested in an isolated worktree against current `main`.
It failed the existing `keeps the basic pursuit list compact until the user
selects a pursuit` regression test (1 of 69 failed), which documents the
intentional on-demand detail behavior. That candidate was withdrawn without
changing the test contract. After reverting the scratch patch, the focused
Pursuits suite passed all 69 tests. The staged source-reconciliation tree and
primary checkout remain unchanged by this experiment.

## PR #117 merge-base and CI refresh (2026-10-13)

The local conflict resolution was committed as
`3436fde440bdee3d2f37af08d0f831b037e30ce8` and pushed to the existing PR #117
branch. Its diff against the refreshed `main` is confined to this audit ledger,
and GitHub reports the PR mergeable. At the time of this check, the new-head
run had one failed repository secret scan, 11 successful checks, two in
progress, and 27 queued. The failed job reported only that scanner output was
not valid JSON; no finding details were returned, so it is not yet known
whether the cause is a finding or a scanner/report failure. The PR remains open
and is not represented as CI-green.

## Post-merge local-copy reconciliation (2026-10-13)

GitHub confirms PR #56 (`test: add guarded runtime lifecycle and projection
regressions`) merged with merge commit
`3182a1e34bd24ae034f51cc74c1bdb2766b9ab59`; PR #112 (agent-runner dependency
security refresh) merged at current `main` `752c1424009f5eb519a0a578c2ee830c25199130`.
The corresponding local branch heads were exact PR heads. Their worktrees had
no untracked data, ignored state, or environment files in #56; the #112
worktree's only ignored files were eight Python `__pycache__` bytecode files.
Both merged worktrees were removed using Git's worktree mechanism after
checking repository origin and head. PR #56's checkout contained 37,873,552
bytes; PR #112's checkout contained 37,938,666 bytes including generated
bytecode. GitHub retains both merged histories and the current `main` source.

The clean `codex/frontend-worker-guard-test` branch has one local commit,
`472eb9e4b204454a9d31e5e96a222a549702f5fa`. Its only changed file is
`scripts/test-frontend-build-resource-limit.cjs`, whose Git blob exactly
matches current `main`; its 78 tracked `output/` files also match `main`.
No duplicate PR was opened. Its sole ignored path was a `frontend/node_modules`
junction to the primary checkout's dependency directory. The managed worktree
was archived; its checkout path is gone and the shared dependency target
remains present.

Current `main` remains `752c1424009f5eb519a0a578c2ee830c25199130`. PR #117 is
still open at `de967033238a954ae6033a7ba2ce658a30217f5a`; GitHub currently
reports it mergeable. The primary legacy checkout is still unclassified at
2,286 status entries, and the `output/` directory alone contains 1,510 files
(126,545,497 bytes). No files from that output directory or the primary
checkout were deleted in this checkpoint. Source reconciliation continues;
these cleanup steps do not imply the remaining checkout, historical worktrees,
or archives have been fully audited.

## Local source and patch audit (2026-10-13)

PRs #41 and #42 are confirmed merged. Their merge commits
`71102fd40a960b59b70e56f902cb4b3dc91f1390` and
`b1caa0b074793e794ea8bc340cba9ac562924ff7` are ancestors of the local
`origin/main` ref. No registered local worktree is checked out at either PR
head, so there is no dedicated #41/#42 checkout to remove.

The primary checkout reports 2,286 changed or untracked entries. A bounded
review found 43 untracked Go files (seven production files and 36 tests). Each
has a corresponding path in `main`, but the local bytes differ; this path and
blob screen does not prove semantic redundancy, so none was deleted.

The local `hai-local-progress.patch` is 4,052,586 bytes and describes 232
paths (+52,952/-9,154). A read-only `git apply --check` against the clean PR
#117 checkout at `fbd69da4938548c8edf4a3387ceab900b3fbbded` failed with 461
diagnostic lines. It is not safe to apply wholesale; it requires file-level
semantic reconciliation against current `main`.

A filename/path inventory found no HAI-specific hits in Downloads, Desktop,
or `.codex/attachments`; it found 719 candidate paths under Documents/Codex.
That set includes multiple HAI worktrees and integrations embedded in other
projects, so it is not a verified complete content inventory. The three
detached model-maintenance, OpenClaw-artifact, and runtime-safety audit
worktrees each have tracked changes (425, 150, and 14 status entries); they
remain intact pending confirmation that their work is integrated. No files or
worktrees were deleted in this audit pass.

The primary checkout's current untracked-file inventory contains 1,594 paths.
By pathname, 85 already exist in `main` and 1,509 do not. The latter break down
into 1,493 `output/` artifacts, ten outcome-evaluation test-evidence files,
four `.claude-flow`/`.swarm` state files, and two Compose logs. These are path
counts, not content identity or retention decisions. The 85 path matches
remain subject to content review; the untracked Go sample above has differing
bytes. No generated evidence or agent state was staged or deleted.

A filename-focused search found no HAI-named files in Downloads, Desktop, or
`.codex/attachments`; this does not establish that those locations contain no
HAI-relevant files under unrelated names. The Documents/Codex path scan is a
candidate inventory, not a full-content review of all 719 returned paths.

Seven untracked backend source copies were then reviewed against their current
`origin/main` implementations and removed from the primary checkout after
confirming their behavior was superseded there (115,383 local bytes):

- `backend/internal/infra/database_pool.go`: `main` adds parameterized,
  configurable GORM logging and validates the log-level setting.
- `backend/internal/operations/execution_claim.go` and
  `execution_claim_memory.go`: `main` binds approval queries to `payload_json`,
  permits a fresh exact-revision approval to supersede a prior review hold,
  and preserves lifecycle timestamps during fenced updates.
- `backend/internal/operations/source_approval.go`: `main` binds the reviewed
  owner type into the receipt and returns more precise stale/duplicate-receipt
  errors.
- `backend/internal/task/execution_context.go`: `main` also propagates the
  private execution context and uses it when selecting cancellation-aware
  storage.
- `backend/internal/openclawmaintenance/pull_client.go`: the local hex check
  is replaced by an equivalent but explicit lowercase-hex predicate in `main`.
- `backend/internal/openclawmaintenance/worker.go`: the only local delta was
  byte-encoding the same pinned public key; `main` retains the readable key.

Before removal, each path was untracked, a regular file inside the primary
checkout, and had a corresponding blob in `origin/main`. After removal, all
seven local paths were absent and all seven canonical blobs remained
readable. This is source-level comparison, not a Go test run.

Additional Go-test name comparisons reinforce that the untracked snapshot is
not additive: the local source-approval test file lacks the main-only
future-review-reminder regression; the local pursuit completion file lacks
main's batch/incomplete-receipt and beyond-cap receipt tests; and the local
control-room test snapshot lacks main's uncapped attention-count regression
and uses a stale framework-selection URL. The inspected ingest-atomicity and
required-participant test-name sets are present on `main` as well. These
checks do not substitute for line-by-line semantic review of the remaining
untracked test files.

The local `scripts/run-secret-scan.py` and
`scripts/test_secret_scan_output.py` snapshots were also removed after
comparison with `origin/main` (3,711 local bytes). The canonical scanner
requires and validates the CI comparison SHA, limits the scanned history,
parses reports defensively, and emits only bounded redacted metadata. Its
canonical test file has ten cases, including valid/malformed report handling;
the local copy had three tests and no unique test case. Both canonical blobs
remain available on `main`. No CI run or scanner execution was performed in
this cleanup step.

## Retained July archive full scan (2026-10-13)

The committed read-only transcript auditor was run from the PR #117 worktree
against the exact source root `D:\codex-temp\hai-completed-agent-sessions`,
with output kept outside the repository at
`D:\codex-temp\hai-retained-transcript-audit-20261013`. It completed a full
file scan of all 13 files (17,559,729,527 logical bytes). Results: 11 unique
child IDs, two duplicated IDs, zero completed transcripts, zero deletion
candidates, five unattributed transcripts, two transcripts with oversized
records, and zero malformed candidate records. All 13 remained classified
for retention; `deletion_performed` was false. No cleanup/removal tool was
run.

This scan did not hash retained transcripts (`candidate_hashing_requested`
was false), and it skipped two oversized records under the configured 10 MiB
record bound. The five older candidate paths were already absent. Their
absence does not establish who removed them or prove full semantic
integration. The prior committed manifest and final reports were not
regenerated or changed from this reduced source tree.

A path-level cross-check covered 51 candidate target paths surfaced while
reviewing the retained patch-bearing sessions; every checked path exists in
current `origin/main`. Target areas include IDP authentication/Kafka,
automation approval proofs and runtime handlers, task validation,
framework-registry history/routes, and automation forms. This is not proof
that every interrupted patch is semantically equivalent to `main`: raw
transcript search also finds echoed commands and prompt text, and interrupted
sessions have no complete final reports. Exact patch-to-commit attribution for
those retained sessions remains unresolved. Preserve the 13 transcripts and
their existing manifest dispositions until that attribution is complete.

## Untracked Go test snapshot comparison (2026-10-13)

The primary checkout's current untracked-Go inventory contains 36 files, all
test files; no untracked production Go file appears in this inventory. Every
path has a corresponding blob in current `origin/main`. Comparing declared
`Test`, `Example`, `Fuzz`, and `Benchmark` function names found no local-only
test function. `main` has five additional test functions across
`backend/internal/ambient/scheduler_test.go`,
`backend/internal/operations/source_approval_test.go`, and
`backend/internal/pursuit/completion_outcome_integrity_test.go`.

Focused line comparisons found current-main expansions in the source-approval
tests (raw-evidence digest and same-version mutation coverage, plus explicit
approval overriding a future review reminder) and pursuit completion tests
(additional receipt/cap and verification-parity cases). The local files are
not byte-identical to their main counterparts, and this test-name/targeted
diff review is not a complete semantic equivalence proof. No test files were
deleted or copied into the PR in this pass; keep them until any remaining
body-level differences are accounted for. This inventory covers only these
36 Go tests, not the full dirty checkout or other file types.

## Primary checkout exact-content comparison (2026-10-13)

The primary checkout was compared with the live GitHub `main` tree at
`345cf356c3f4ab497568713dc994820b4ba22b10`. The complete recursive tree was
returned without truncation. Among 692 tracked paths reported modified by Git,
448 working files have the exact Git blob hash on `main` (6,818,815 logical
bytes), 243 differ from `main`, and one path is absent from the current main
tree. These counts identify content equality, not whether a file is disposable:
the 448 matching files are source files in the active checkout, so deleting
them would leave that checkout incomplete and was not done.

The checkout also contains 1,585 non-ignored untracked files. None whose path
exists in the main tree has an identical Git blob hash. No untracked file was
deleted. This is a byte-equality inventory only; the 243 divergent tracked
files, the path absent from main, and all untracked files still require
classification and semantic review before any integration or cleanup.

The separate `D:` source-reconciliation worktree has staged changes affecting
310 paths relative to its branch `HEAD`; its resulting working tree differs
from current `main` in 346 paths (one addition, five deletions, and 340
modifications). `git diff --cached --check` passed, but the patch is broad and
includes substantial deletions. Go is not installed in this environment, so
the backend changes could not be locally tested. The patch was not committed,
published, or cleaned up; preserve it for file-by-file review.

Targeted review of the staged snapshot found additional release blockers:
relative to current `main`, it deletes
`backend/internal/verification/source_snapshot_validation_test.go` and other
health/release/pursuit tests, as well as `DEPLOYMENT_TARGET.md` and
`.github/dependabot.yml`. Its added `nginx-config/nginx.conf` contains a
hard-coded backend-key placeholder instead of a generated/configured secret.
Do not publish or merge this bulk snapshot; review and port individual changes
only after restoring required safety coverage and configuration boundaries.

The same staged ambient patch is statically incomplete: it removes
`OverviewSummaryForOwner` while the ambient handler interface and service tests
still reference it; removes `ScanForOwnerWithPursuitDashboard` while the
agent-cycle scanner interface, production caller, and fake scanner still
reference it; and removes `validatePursuitDashboardOwner` while service tests
still call it. A recursive symbol search in that worktree confirmed these
references remain. This is a compile blocker in addition to the safety and
configuration issues above; no Go test run was possible because `go` is not
installed.

Further comparison of key staged files against current `main` confirms these
are substantive regressions, not formatting-only changes. The candidate
`backend/internal/infra/database.go` drops the `DB_RUN_MIGRATIONS` gate and
adds migration-owned workflow/pursuit proof tables to GORM AutoMigrate;
`backend/internal/operations/service.go` replaces the raw-evidence digest
comparison and digest update with a direct JSON-string comparison;
`backend/internal/identity/jwt.go` removes the operator-scope claim and context
key; and `scripts/backup-windows.ps1` removes bounded Docker process handling
and recovery-asset coverage. For five representative core paths
(`database.go`, ambient service, LLM maintenance, operations service, and JWT),
the primary checkout and staged D: worktree have identical file hashes, while
all five differ from current `main`. Treat the corresponding local changes as
one shared candidate set, not independent implementations, and preserve them
until each is reconciled against the canonical safety behavior.

## Merged transcript-gate worktree cleanup (2026-10-13)

PR #46 (`fix: retain completed transcripts without final reports`) is merged
into `main`. Before removing its dedicated local worktree at
`D:\hai-transcript-report-retention-gate`, the checkout had no tracked,
untracked, or ignored changes. Its only PR-touched files,
`scripts/audit-child-agent-transcripts.ps1` and
`scripts/test-child-agent-transcript-audit.ps1`, had Git blob hashes identical
to current `main`. The clean worktree contained 3,179 files totaling
37,830,285 logical bytes. Git's non-forced `worktree remove` completed; the
path is absent, it is no longer registered, and its local branch ref remains.
This removes the duplicate checkout while preserving the repository history
and branch reference. It does not remove the separate 17.5 GB transcript
archive, which remains unintegrated and retained.

## Reconciliation continuation checkpoint (2026-10-13)

The live repository is `Robert-Velhorst/018-HAI`. A fresh read-only fetch
updated the local audit ref to `main` at `345cf356c3f4ab497568713dc994820b4ba22b10`.
GitHub confirms PRs #41 and #42 are merged, with merge commits
`71102fd40a960b59b70e56f902cb4b3dc91f1390` and
`b1caa0b074793e794ea8bc340cba9ac562924ff7`. Their PR-head GitHub Actions runs
completed successfully (runs 1154 and 1152, respectively). These are historical
PR-head checks, not a claim that the complete local reconciliation candidate
passes CI.

The primary checkout remains on its stale `codex/hai-runtime-release` branch,
440 commits behind `origin/main`, with 692 modified tracked paths and 1,585
untracked paths (2,277 status entries). It is not a publishable aggregate
patch. The separate model-maintenance audit worktree remains at the same old
base; its 95 untracked paths include 89 names also present on `main`, but a
worktree-scoped Git blob comparison found zero exact matches. No deletion is
authorized by that content screen, and the worktree remains intact.

PR #117 remains open and mergeable at head `00bab9f2c1c7fca5a0c5252782b0321b13f3c8e8`.
Its latest GitHub Actions run (1316) is queued, so its checks are not yet
passing. The ledger PR remains documentation-only; the staged source snapshot
has not been pushed because prior review found compile blockers, removed safety
tests and protections, and hard-coded configuration. No local files were
deleted in this checkpoint. Continue semantic review by file and only publish
isolated, verified improvements against current `main`.

## Go test-overlay reconciliation follow-up (2026-10-13)

After PRs #41 and #42 merged, a fresh scan of the primary checkout found 36
untracked Go test files under `backend` and `idp`. All 36 have same-path files
on `main`. Comparing declared `Test...` function names found no local-only
test function. The local Trello reopen test is an example of a non-additive
variant: `main` uses a richer `/actions?filter=all` fixture that emits card
activity records, while the local fixture returns an empty action list. Other
main-side test files also contain additional cases beyond these local
variants. This screen identifies likely superseded snapshots, but does not
prove body-level equivalence or safe deletion for all 36 files.

The PR merge status was checked directly on GitHub: #41 merged at
`71102fd40a960b59b70e56f902cb4b3dc91f1390`, and #42 merged at
`b1caa0b074793e794ea8bc340cba9ac562924ff7`. Local `audit/main` and the live
`main` ref both resolve to `345cf356c3f4ab497568713dc994820b4ba22b10` at
this checkpoint. No unique product-code change was identified in this bounded
test-only scan; no test file was copied, deleted, or declared safe to remove.
The remaining Go test bodies and all other local file categories still
require semantic comparison.

## Follow-up assertion review (2026-10-13)

Four local-only assertion strings in the untracked Go tests were compared with
their corresponding `main` tests. They do not identify a missing behavior to
port; the canonical tests enforce stronger or updated contracts:

- `backend/internal/background/worker_reliability_test.go`: the local feed
  failure check accepts an error containing the fixture's `test-feed` name;
  `main` explicitly rejects that value to enforce sanitized reporting. Its
  source-operation assertion also requires the new operation to remain
  approval-gated rather than merely counted as verified.
- `backend/internal/task/operation_transition_context_test.go`: the local
  post-claim cancellation case expects uncertain-outcome review. `main`
  verifies that a cancellation known to precede the effect is durably marked
  cancelled without creating uncertain-outcome review work, and separately
  checks replay/readback cancellation semantics.
- `backend/internal/operations/source_approval_test.go`: `main` additionally
  checks the status/version mismatch diagnostic on a stale lifecycle receipt.
- `backend/migrations/source_head_postgres_test.go`: `main` tests populated
  rollback protection in an isolated pre-phase database and verifies that
  refusal preserves the ledger and authority rows; it also exercises a
  separate later-phase rollback refusal.

This focused assertion review found no safe source/test delta to import from
these four local variants. It is not a full body-level review of the remaining
untracked files. No local files were removed.

## Retained July transcript path crosswalk (2026-10-13)

The read-only `scripts/audit-hai-transcript-patch-paths.ps1` utility was run
against the current 13-file retained archive at
`D:\codex-temp\hai-completed-agent-sessions`, comparing absolute patch targets
from the primary checkout with the current PR worktree based on `main`
`345cf356c3f4ab497568713dc994820b4ba22b10`. It scanned 17,559,729,527 logical
bytes and observed 37,824 patch events (37,783 marked successful and 41
failed), 58,960 path references, and 760 unique target paths. Of these, 674
unique paths reference the primary checkout; 667 are tracked in the current
repository tree and seven are absent from that tree:

- `.codex-preview-server.mjs`
- `.env.local`
- `.git\\info\\exclude`
- `.tmp-ai-code-review.json`
- `frontend\\mock-preview-server.js`
- `frontend\\proxy.preview.json`
- `nginx-config\\nginx.conf`

No content from these seven paths was read or restored by this scan. The
crosswalk does not compare patch contents to current source and therefore does
not prove that the 667 tracked targets contain the transcript changes. Two
oversized JSONL records were skipped at the 10 MiB record bound, so
`patch_event_scan_complete` is false. The transcript source remains
unmodified, and the result does not authorize cleanup.

## Retained child-task outcome cross-reference (2026-10-13)

The first user task messages in the two smaller retained transcripts identify
bounded work in the separate `work/018-hai-port-engine-control` checkout:

- Child `019fb296-75ae-7943-913c-32d41f5de2d6` targeted task validation for
  contradictory values, negation, and generic provenance. Current `main`
  contains the matching guards in `backend/internal/task/validation.go` and
  regression cases in `validation_test.go` for conflicting numeric/date/
  version values, a negated claim, and unrelated generic provenance.
- Child `019fb296-9d1a-7301-901c-c35d99ccf3da` targeted Constitution version
  history in the Framework Registry. Current `main` contains the owner-scoped
  bounded history service/handler/repository, `GET /constitution/history`,
  frontend service/model and history view, and corresponding tests.

The matching implementation and test additions are present in commit
`a97ff6e1930c16d9b04fc2f4bb12e4c470a3b2e6` (`feat: add governed framework
registry and durable approvals`, 2026-07-30), which is an ancestor of the
current `main`. The commit's presence establishes that these requested
capabilities are integrated; it does not prove that either aborted transcript
was the source of that commit or that its patch payloads are identical. These
two task outcomes need no code port based on current evidence. The remaining
nine broad child sessions and the two oversized records still need
task/content-level reconciliation; the archive remains retained.

## Direct-main cleanup screen (2026-10-13)

In the stale primary checkout on `codex/hai-runtime-release`, the read-only
screen found 692 modified tracked paths and 1,585 untracked, non-ignored paths
(2,277 status entries). A same-day fresh `git status --porcelain=v1
--untracked-files=all` count is the source of this measurement; it supersedes
the earlier 682/1,594 inventory in this ledger. The reason those two earlier
counts changed between snapshots is unknown. Seventy-six untracked paths share
a path with the current `origin/main` tree; none has an identical Git blob.
This screen identified zero untracked files eligible for the user's direct-
main cleanup rule. It does not assess ignored files, runtime data, or other
local archives.

The registered worktree inventory found that the three HAI audit worktrees
and the six October 11 PR-resolution worktrees have tracked changes, so they
were preserved. One clean sparse-review checkout is exactly at current `main`
and contains 264 working files totaling 4,188,581 logical bytes; it was also
preserved because it is a small, purpose-specific review checkout. The
registered `D:\hai-source-reconciliation` path was not present when checked.
No worktree or local file was removed.

GitHub issue #3, “Make 018-HAI operational with Claw-compatible agent runtime
and $0 LLM routing,” is closed with reason `completed` (2026-07-09). This is
issue metadata, not independent acceptance evidence for every operational
requirement listed in its body.

## Installer, runtime, and dependency-audit overlay review (2026-10-13)

Three bounded source reviews compared the primary checkout with current
`origin/main`; none identified a safe code delta to port from these areas:

- The local `backend/internal/agentruntime/handler.go` removes request
  cancellation checks around ecosystem-upload inspection and persistence, and
  its local test drops the cancellation regression case. `main` retains the
  fail-closed behavior. The local checkout has no `backend/internal/openclawmaintenance`
  source files; that package is already present on `main`.
- Local Windows installer smoke/payload-selection scripts are stale relative
  to `main`. The smoke-test copy drops hosted-runner argument compatibility,
  temporary-path/reparse-point checks, and the isolated local-app-data
  profile. The payload selector removes the versioned SQL migration exception,
  which can exclude installer-required migrations. Local runtime tests also
  pin an older Go toolchain or drop existing Docker-network-preservation
  assertions. Current `main` contains safer equivalents; none were run against
  an installer or runtime in this audit.
- The untracked primary-checkout copy of
  `scripts/audit-npm-production-dependencies.sh` is older than the tracked
  `main` implementation: it ignores the second `omit-optional` argument even
  though the Promptfoo CI job supplies it and `scripts/test_ci_contract.py`
  asserts the optional-dependency behavior. Keep the canonical `main` copy;
  do not port the local overlay.

These reviews are scoped to the named paths. They do not complete the
remaining local file-by-file audit, and no local files were removed.

## July archive source discrepancy (2026-10-13)

A fresh read-only comparison of the cleanup manifest with
`D:\codex-temp\hai-completed-agent-sessions` found 13 files totaling
17,559,729,527 bytes, rather than the 18 files and 20,739,169,122 bytes in the
manifest. All 13 present files match retained manifest rows by filename and
byte count; there are no extra files or retained-file size mismatches. The
five missing paths are every row marked `candidate_after_ledger_commit`,
totaling 3,179,439,595 bytes. The source verifier correctly failed closed at
the first missing candidate; it did not validate the full archive or authorize
cleanup. No file was deleted or changed during this audit.

Targeted filename searches found no copies of those five transcripts under
`D:\codex-temp`, `C:\Users\NO\.codex`, `C:\Users\NO\Documents\Codex`,
`C:\Users\NO\Downloads`, `C:\Users\NO\Desktop`, or
`C:\Users\NO\AppData\Local\Temp`. These searches do not prove whether the
files were deleted, moved elsewhere, or remain outside the searched roots; the
time and actor are unknown. The candidate SHA-256 values remain in the
manifest, but cannot be rechecked without the source bytes.

The recorded candidate implementation paths are present in current `main`.
PR #36 is merged into `main`; its head `e62b56843eb8bbbf9a566aff5e883f9adf375429`
has a successful GitHub Actions run (#1079), and its merge commit
`c31cec1ba2a25fcfd280b1f2f499297e54d96aa4` is an ancestor of current
`origin/main` (`345cf356c3f4ab497568713dc994820b4ba22b10`). This establishes
the repository integration and CI status, but does not recover or re-verify
the absent transcripts. Preserve the 13 retained files and do not represent
the source-archive cleanup gate as passed.

## Supplemental archive verification (2026-10-13)

The separate roots `D:\codex-temp\completed-agent-sessions` and
`D:\codex-temp\session-log-relief` were checked with the committed supplemental
readiness verifier. All 9 manifest entries match the source inventory:
14,013,737,298 bytes across both roots. Two completed candidates totaling
3,113,294,358 bytes passed exact SHA-256 verification; the other 7 transcripts
remain retained. The verifier reported
`result=supplemental_source_archive_verified`,
`deletion_performed=false`, and `cleanup_authorized=false`.

The candidate reports are Hubble (Framework Registry review/read-only/authority
guard work) and Popper (icon registration and Model Routing cleanup). Their
report-linked implementation paths are tracked in `main`; PR #36 is merged,
its exact-head CI run #1079 succeeded, and its merge commit is in `main`. The
cleanup-gate correction PR #37 is also merged; its exact-head CI run #1081
succeeded, and merge commit `daee223cd41f0a7847b80ab9533e7fc175023115` is in
`main`. These records and checks establish integration evidence, not permission
from the read-only verifier to delete data.

The purpose-built `remove-hai-supplemental-completed-session-transcripts.ps1`
exists for these two roots. Its preflight requires a clean canonical `main`
worktree at the current remote commit, the merged-PR-#37 primary cleanup gate,
and a passing primary source-archive verification before it resolves any
supplemental candidate. The primary archive has the five missing candidates
recorded above, so this required gate currently blocks supplemental cleanup.
The supplemental verifier itself is read-only; no supplemental files were
removed. Keep all 7 retained transcripts and do not bypass the primary gate or
use the primary-archive remover to target either supplemental root.

## Windows CI and retained patch-artifact review (2026-10-13)

The local Windows-native runtime/build scripts and isolated acceptance-stack
helpers already exist on `main`; local variants downgrade the Go version,
shorten test deadlines, or omit gateway-readiness behavior. The local CI
workflow removes existing release/safety gates, including cleanup-readiness,
A2A race, Docling-boundary, Windows no-fake-claims, and Trello-launcher checks.
No workflow or helper change was ported. The bounded static PowerShell parser
checks and `git diff --check` passed; no builds, installers, or acceptance
stacks were run.

Two retained patch files under `D:\codex-temp` were inventoried without
applying or modifying them:

- `hai-c-worktree-20261010.patch`: 682 paths, 199,137 additions, and 74,020
  deletions; file size 14,169,378 bytes.
- `hai-c-worktree-clean-20261010.patch`: 493 paths, 98,461 additions, and
  10,054 deletions; file size 6,442,729 bytes.

For the 493-path patch, an index-header-only crosswalk found 339 post-image
hash prefixes matching same-path Git blobs in current `main`; 154 paths did
not match those prefixes, including `nginx-config/nginx.conf`, which is absent
from `main`. These are abbreviated index hashes and path-level triage only,
not full-blob or semantic verification. The corresponding broader patch had
339 matching prefixes, 342 nonmatching existing paths, and one path absent
from `main`. The earlier semantic review of the staged snapshot found safety,
compile, and configuration regressions. Neither patch is safe to apply or
delete as a whole; both remain preserved for path-level reconciliation.

## October 14, 2026: direct-match cleanup check

The live `main` ref was read from GitHub as
`345cf356c3f4ab497568713dc994820b4ba22b10`. A recursive tree query returned
3,182 blob paths. The primary legacy checkout remains at
`e07b9daeb3ba2630ecbe12a64d948f791203c241` with 682 modified tracked files and
1,585 untracked files. Of the untracked files, 76 share a path with a blob on
`main`; each was hashed as a Git blob and none matched the `main` blob at that
path. Therefore, no untracked file in this comparison is a verified direct
duplicate eligible for cleanup. The tracked modifications remain untouched
because they are part of a dirty checkout and have not been reconciled
file-by-file.

A content-hash crosswalk of the 692 modified tracked working-tree files against
the same live `main` tree found 448 exact same-path blobs totaling 6,818,815
bytes, 243 different blobs at existing paths, and one modified path absent
from `main`. The largest exact match is 252,009 bytes; no individual exact
match is at least 1 MiB. Removing those tracked files would leave the already
dirty legacy checkout incomplete for less than 7 MiB of space, so they remain
in place. No environment values were read or published.

PRs #41 and #42 are merged, and both merge commits are ancestors of live
`main`; no local worktree for either PR branch is registered in the primary
checkout. The clean local worktrees for open PRs #117, #119, and #121 each
exactly match their pushed PR head, but they are not yet integrated into
`main`, so they remain in place under the main-match cleanup rule. No local
files were deleted in this check. A `git fetch origin main` attempt could not
write `.git/FETCH_HEAD` due to an access-denied error; live refs were checked
read-only through GitHub instead. The separate `D:\hai-source-reconciliation`
checkout was not modified.

The July 30 archive root was also re-inventoried by filename and file length:
`D:\codex-temp\hai-completed-agent-sessions` currently contains 13 files
totaling 17,559,729,527 bytes. All five manifest entries marked
`candidate_after_ledger_commit` are absent from that root; their recorded
logical-byte total is 3,179,439,595, exactly the difference from the manifest's
20,739,169,122-byte total. The expected date-based session directory under
`C:\Users\NO\.codex\sessions\2026\07\30` also does not contain those five
filenames. This supersedes the older archive-verification result as evidence
of current source availability: the remaining archive cannot pass the
source-archive gate from this root, and no retained transcript was removed.

## October 14, 2026: hidden transcript scan and output-report crosswalk

The transcript patch-path audit script previously enumerated only visible,
top-level `*.jsonl` files. On this Windows checkout, the 82 JSONL files under
`output/` are hidden, so the old default scan incorrectly returned zero files.
The scanner now includes hidden files and accepts `-Recurse`; its regression
fixture covers visible, hidden, and nested JSONL files plus an oversized record
followed by valid records. The focused test passed. A recursive scan of
`output/` then read all 82 files (120,845,323 bytes), reported no patch events,
parse errors, or oversized records, and marked the scan complete. Sampled
records are Go test JSON logs (`Time`, `Action`, `Package`), not Codex patch
transcripts. No output file was modified or removed.

A content-level scan of the 13 retained July archive files (17,559,729,527
bytes) was attempted with the corrected PowerShell scanner and two streaming
alternatives. The PowerShell pass and alternatives exceeded the bounded runtime
without a complete result; they were stopped, and no authoritative patch-event
count or source-path crosswalk is claimed for those archive bytes. Ripgrep
filename matches alone are not counted as valid `patch_apply_end` events. The
archive files remain untouched. A faster bounded scanner is needed before this
archive can be semantically reconciled.

PR #117's current head `de8c373c7c42b9bc32a67e866f919f9b32696a85` is open and
not CI-green. Workflow run 38480175434 fails two checks: the Python runner
contract reports that the Agent Framework and Garak lockfile contents do not
match their Docker-installed hash-lock inputs, and the IDP suite fails
`TestAutomaticGormPingFailureRequiresCallerCleanup`. PR #119 contains the
lockfile/contract changes and removes that obsolete automatic-ping assertion;
it remains open and unmerged. PR #120 independently changes the same IDP test
file and overlaps that portion of #119. No CI failure was concealed or treated
as a result of the transcript-scanner edits without proof of causality.

The hidden `output/` directory contains 40 Markdown review/handoff reports.
A path-only crosswalk extracted 203 unique repository path references: 195
exist in live `main`; eight do not. Of the 195 paths present in `main`, 126
also exist in the legacy primary checkout; 63 of those local files have the
same Git blob as `main` and 63 differ. These path/blob comparisons do not
establish that each report's proposed behavior was implemented, tested, or
accepted: the reports are historical evidence, and some contain unresolved
findings or tests that were authored but not run. Only five of the 40 report
filenames are currently named in this ledger. The remaining report-by-report
semantic disposition is still open; none of the reports or source files was
deleted or treated as fully integrated based solely on path overlap.

## October 14, 2026: live PR and artifact inventory recheck

PR #117 remains open and mergeable at head
`d91e8b76b8013c7c485d46bd8fbc5aaa54df4b1d`. GitHub Actions run
`38483307519` is still in progress for that exact head. At the time of this
check, `Windows CI and smoke path contract` had failed; other jobs were still
running or queued, so this head is not CI-green and no final conclusion is
claimed. The earlier failures recorded above belong to the prior head and run
`38480175434`; they must not be attributed to the current run without its
completed logs. PRs #119 and #120 remain open and mergeable; #120 overlaps the
IDP test change in #119. Neither PR was merged in this check.

A fresh inventory of the current hidden `output/` directory returned 67 files:
33 dated Markdown reports, one README, 19 JSON files, 13 log files, and one
PowerShell script (916,679 bytes total). This differs from the earlier
crosswalk's stated set of 40 Markdown reports. The earlier 203-path crosswalk
and report dispositions therefore remain historical evidence for that earlier
inventory, not a verified crosswalk for these 33 currently present reports.
The seven-report count discrepancy has not been reconciled against other
artifact roots or a manifest. All 67 files remain untouched; report-by-report
semantic disposition and any archive cleanup remain open.

## October 14, 2026: filename-set and historical-binding correction

The earlier "seven-report count discrepancy" wording is inaccurate and is
superseded by this exact relative-path comparison. The primary checkout's
`output/` contains 40 Markdown files; this PR worktree's checked-in `output/`
contains 34 Markdown files (33 dated reports and `README.md`). Only 11 relative
paths overlap: 29 files exist only in the primary output root and 23 only in
the PR worktree. Therefore the six-file difference in raw Markdown counts is
not a seven-report discrepancy, and neither root is a complete filename-level
proxy for the other. The 29 primary-only reports remain unclassified; the
crosswalk for the 34 PR-worktree Markdown files must be handled as a separate
set. No report or source file was deleted based on this comparison.

The historical automation launch-binding concern in
`automation-launch-binding-20261001.md` is implemented on current `main`, not
still open as the original worker handoff described. Live `main` includes
`ReviewConfigurationSnapshot` in the durable task review request, incorporates
it into `ReviewRequestDigest`, verifies the stored snapshot before approval
registration, and fails closed on missing or changed snapshots. The source
also contains dedicated regression tests in
`backend/internal/task/historical_automation_review_test.go` and
`backend/internal/automation/historical_review_binding_test.go`. This is a
source/test-presence confirmation only; it is not a fresh local test run or
live-provider acceptance.

Live GitHub recheck: `main` is `345cf356c3f4ab497568713dc994820b4ba22b10`.
GitHub commit comparison confirms both approved merges are ancestors of that
head: PR #41 at `71102fd40a960b59b70e56f902cb4b3dc91f1390` and PR #42 at
`b1caa0b074793e794ea8bc340cba9ac562924ff7`. PR #117 remains open and
mergeable at `01950020753fc4ebc7093c7a056f834b89a70f9a`; its exact-head CI has
41 completed checks: 38 successful and three failed (`Gateway and local
Compose validation`, `Windows CI and smoke path contract`, and `IDP build and
tests`). It is not CI-green. No PR was merged and no local data was deleted in
this checkpoint.

## October 14, 2026: bounded source disposition of seven primary-only reports

The following seven primary-only reports were semantically checked against the
current `origin/main` source at `345cf356c3f4ab497568713dc994820b4ba22b10`.
These are source/test-presence dispositions, not claims that every test passed
or that HAI is production-accepted.

| Report(s) | Current source disposition | Remaining verification or boundary |
| --- | --- | --- |
| `automation-launch-binding-20261001.md`; `task-automation-review-snapshot-20261001.md` | The review-time automation configuration snapshot is stored in the durable task request, bound by `ReviewRequestDigest`, verified before approval registration, and rejected when absent or changed. Dedicated task and automation regression files are present. | Go tests were not run in this audit. Process-wide policy mutation/ABA interleavings and atomicity between external effects and durable audit writes remain outside this binding. |
| `approved-embedded-runtime-review-20261001.md` | `main` no longer contains the reported whole-loop `usesAdmittedEmbeddedRuntime` exemption. Required participants remain explicit admission gates; tests cover automatic and approved paths, readiness probes, and ready participants. | Test source is present; no local Go test execution or live agent/runtime acceptance was performed. |
| `production-safety-followup-20260930.md` | Structured/nested JSON redaction is implemented before output truncation. Public automation reads and diagnostics use redacted projections; tests assert viewer responses and launch output/history do not expose synthetic credentials while stored execution configuration remains unchanged. | Go tests and live provider flows were not run. The durable manual source worker's PostgreSQL/browser/provider acceptance remains distinct and unverified. |
| `production-configuration-review-20260930.md` | The browser CI path prepares, starts, and stops an owner-scoped disposable stack; cleanup validates labels and refuses persistent volumes. The guard rejects service `env_file`, service/top-level secrets/configs, and related external configuration sources. Production secret checks enforce a 32-byte minimum. Current main includes focused contract tests for these controls. | `pwsh -NoProfile -File scripts/test-isolated-acceptance-stack.ps1` passed in the PR worktree. It used synthetic fixtures and reported that no containers were started; generated evidence was retained outside the repository. This does not prove hosted CI, real Docker, or deployment behavior. |
| `workflow-preflight-independent-review-20261001.md`; `workflow-preflight-followup-review-20261001.md` | The main branch carries explicit task/tool outcome-uncertainty state through the workflow adapter and routes it to `needs_review`/reconciliation rather than approval-only routing or retry. Tests exist for task-service errors, uncertain effects, lost leases, post-action review, and workflow recovery; the follow-up's first four findings are recorded as addressed in its own final review. | No Go suite or persisted PostgreSQL/browser workflow was run here. Existing limits around live effects, cross-system atomicity, and deployment acceptance remain. |

This closes the reviewed dispositions for seven of the 29 files found only in the
primary checkout's `output/`; the other 22 primary-only reports still need
semantic review. The 23 report files present only in the PR worktree are a
separate set and are not counted as reviewed by this checkpoint. No report,
archive, source file, or worktree was deleted.

## October 14, 2026: atomic mutation report source disposition

`atomic-mutation-source-review-20261001.md` is source-resolved on the same
`main` snapshot. `backend/internal/operations/repository.go` validates a
status-change event's `BeforeStatus` against the locked current row and runs
the transition check from that authoritative status before writing. The
memory mutation path also validates against current state. Both the memory and
Gorm update paths restore `CreatedAt` from the authoritative record rather
than accepting a caller rewrite. Regression coverage is present in
`mutation_atomicity_regression_test.go` for forged before-status and in
`mutation_authoritative_state_test.go` for claimed/unclaimed guards and
creation-time preservation. This audit did not run the Go suite. The source
check does not prove PostgreSQL transaction durability, lock behavior, crash
recovery, or concurrency acceptance.

This is the eighth disposition among the 29 primary-only reports; 21 remain.
The separate 23-report PR-worktree-only set remains unreviewed. No local data
was removed.

## October 14, 2026: automation executor outcome report disposition

`automation-executor-outcome-20261001.md` is represented in current `main`.
`AutomationToolExecutor.Execute` retains a non-nil launch receipt alongside an
error, marks post-invocation outcomes uncertain, and emits a non-authoritative
indeterminate result when the launcher provides no receipt. The task service
stores the returned tool result before branching on the error and routes
uncertain outcomes to reconciliation rather than completion. Focused executor
tests cover result-plus-error, missing receipts, status and malformed-receipt
uncertainty, HTTP/process exit semantics, and pre-dispatch failures. These
checks confirm source and test presence only; this audit did not run Go tests
or validate persisted replay against a live runtime/provider.

This is the ninth disposition among the 29 primary-only reports; 20 remain.
The PR-worktree-only report set remains a separate 23 files and is unreviewed.
No local data was removed.

## October 14, 2026: container build hygiene report disposition

`container-build-hygiene-20261001.md` is represented in current `main`.
`backend/.dockerignore`, `idp/.dockerignore`, and `frontend/.dockerignore`
contain the reported scoped credential/certificate, local-binary, cache, and
test-output exclusions; the frontend retains explicit `.crt`/`.cer` public
asset exceptions. The IDP Dockerfile also copies module manifests and downloads
dependencies before copying source. No unrelated production source, docs,
migrations, or frontend test source is excluded by the report's described
patterns. This audit checked committed source only; it did not execute Docker
builds, confirm every build-context match, inspect secret contents, or verify
runtime behavior.

This is the tenth disposition among the 29 primary-only reports; 19 remain.
The 23-report PR-worktree-only set remains unreviewed. No local data was
removed.

## October 14, 2026: durable-session CI integration disposition

`durable-session-ci-integration-20261001.md` is represented in current `main`.
The IDP CI job defines a disposable PostgreSQL 17 service bound to loopback,
uses the dedicated synthetic test database, and runs the named durable-session
PostgreSQL/Redis race suite with skip/failure rejection and an explicit PASS
requirement for all 13 real-store tests. The source contract in
`scripts/test_ci_contract.py` checks this wiring against the test names in the
IDP authentication sources. This is committed workflow/test-contract presence;
the current audit did not run the contract test, the Go suite, or the hosted
workflow, so no CI or real-store success is claimed.

This is the eleventh disposition among the 29 primary-only reports; 18 remain.
The 23 PR-worktree-only reports remain a separate unreviewed set. No local data
was removed.

## October 14, 2026: frontend build worker-limit report disposition

`frontend-build-resource-limit-20261001.md` is source-resolved in current
`main`. `frontend/Dockerfile` pins Node 24.15.0, defines
`HAI_FRONTEND_BUILD_WORKERS=2` after the dependency-install layer, rejects any
value outside `1|2|4|8`, and passes the accepted value only to
`npm run build` as `NG_BUILD_MAX_WORKERS`. The Nginx runtime stage has no worker
setting. `scripts/test-frontend-build-resource-limit.cjs` is present. This
audit did not run that Node test, perform a Docker build, measure cache reuse,
or quantify CPU/memory/build-time savings; the report's historical five-test
pass is not a fresh run here.

This is the twelfth disposition among the 29 primary-only reports; 17 remain.
The separate 23 PR-worktree-only set remains unreviewed. No local data was
removed.

## October 14, 2026: account-feed cleanup follow-up disposition

The primary-only report `hai-resource-cleanup-followup-20261001.md` contained
three related implementation threads. Current `main` contains the read-only,
bounded local-feed identity preview (`backend/internal/accountfeed/identity_preview.go`)
and handler tests that distinguish a busy sync from an unsupported HTTP feed
and a changed-feed conflict. The account-bridge UI maps those response codes
separately rather than treating every 409 as an unsupported feed. Context-aware
source intake is also present through `operations.ContextIntakeRepository` and
its repository implementations/tests.

The report's source-revision safety concern is addressed in the current source:
source-derived operations retain observation IDs/generations, and
`WithClaimedSafeEffect` locks the feed origin and source head, validates the
operation's observation reference and executable head, then checks the current
execution claim before invoking the effect. This is the final-effect admission
path, not merely an earlier timestamp lookup. Tests for changed observations,
stale revisions, lock order, and rejected claimed effects are present. These
are source and test-presence findings only; this audit did not execute the Go
or frontend suites, run PostgreSQL/Redis integration tests, or establish live
runtime acceptance. The report is therefore source-resolved on `main`, with
execution verification still outstanding. No source files, archives, or
worktrees were changed or deleted.

This is the thirteenth disposition among the 29 primary-only reports; 16 remain.
The separate 23 PR-worktree-only reports remain unreviewed. No local data was
removed.

## October 14, 2026: source disposition of the framework-health fixture report

The primary-only report `framework-health-contract-review-20261001.md`
described a deterministic test-fixture mismatch: a positive health-evidence
case used five-minute-old evidence against a 60-second contract, making its
expected result impossible and weakening several negative assertions. Current
`main` at `345cf356c3f4ab497568713dc994820b4ba22b10` already contains the
report's test-only repair in
`backend/internal/task/framework_health_contract_test.go`: the baseline is
30 seconds old, while the explicit `expired contract` case remains five minutes
old (lines 73-100). The production evidence gate was not relaxed. This report's
source finding is therefore represented on `main`; this audit did not run the
Go test, so executable verification remains unclaimed. This is the fourteenth
disposition among the 29 primary-only reports; 15 remain. The 23 PR-worktree-
only reports remain a separate, unreviewed set. No files were deleted.

Live GitHub recheck: PR #41 and PR #42 are already closed and merged to `main`
at `71102fd40a960b59b70e56f902cb4b3dc91f1390` and
`b1caa0b074793e794ea8bc340cba9ac562924ff7`. PR #117 remains open and
mergeable at head `12572f1a8c9697e88ce0b0bc3075724fd431fd01`, based on
`main` `345cf356c3f4ab497568713dc994820b4ba22b10`. The combined commit-status
endpoint returned no status entries; the workflow-runs endpoint showed one
queued CI run (`38485702783`) and is not a complete check inventory. No
CI-green claim is made. No merge or local cleanup occurred in this check.

## October 14, 2026: runtime metadata and outcome-consumer dispositions

Two additional primary-only reports were checked against `main` at
`345cf356c3f4ab497568713dc994820b4ba22b10`:

| Report | Current source disposition | Remaining verification or boundary |
| --- | --- | --- |
| `read-only-runtime-metadata-20261001.md` | The optional `ReadOnlyRuntimeInspector` and `AutomationToolExecutor.IsReadOnlyRuntime` are present in `backend/internal/task/read_only_runtime.go`; `model_execution_preflight.go` consumes the interface before permitting the relevant fallback path. Focused tests cover stored-target classification, invalid IDs, identity mismatch, redacted lookup failure, unavailable readers, and fresh configuration reads. | The current audit did not execute Go tests. This classifies configured HTTP metadata only; it is not execution permission, proof of side-effect-free endpoints, or live provider/runtime acceptance. |
| `runtime-outcome-independent-review-20261001.md` | The reported uncertain-outcome promotions are source-resolved: life-graph projection gives uncertainty precedence and binds it into the digest (`life_ontology_projection.go`, with `TestLifeProjectionCannotPromoteUncertainExecution`); assistant `planRequiresReview` calls the shared uncertainty helper (`assistant/service.go`, with `TestAssistantRequiresReviewForUncertainExecution`); workflow-owned operation retry is rejected before decision mutation and has a no-execution regression; missing prior plans become `ErrTaskOutcomeReconciliationRequired`, with the handler returning 409. | No Go tests ran in this audit. Existing tests cover uncertainty projections, assistant signaling, and workflow-owned retries. A narrow service regression isolating the missing-prior-plan lookup branch was not found in the searched test sources; treat that branch's targeted coverage as outstanding. Source presence is not runtime acceptance. |

This raises the number of primary-only reports with a semantic disposition from
fourteen to sixteen, leaving 13 of the 29 primary-only reports to review. The 23
PR-worktree-only reports remain a separate unreviewed set. The live metadata
inspection reports PR #117 at head `12572f1a8c9697e88ce0b0bc3075724fd431fd01`;
the ledger update following that observation is now committed and pushed as
`1f1a1840`, so that PR head value is historical for this section. No report,
archive, source file, or worktree was deleted.
