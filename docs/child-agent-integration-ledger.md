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
histories, but did not record the six source paths, their hashes, or a
reproducible extraction artifact. That count therefore cannot currently be
attributed to this July 30 archive or treated as 542 distinct work items. The
verified July 30 inventory is the 18-file manifest above: eight unique
completed candidates have report/source crosswalks, and ten duplicate,
aborted, or nonterminal files remain retained. Do not use the unattributed 542
count as evidence that this archive has additional deliverables, and do not
claim that every event embedded in retained histories has been individually
reconciled. If those six histories are identified later, audit them as a
separate source set with file-level provenance.

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

The primary checkout at
`C:\Users\NO\Documents\Codex\2026-05-30\github-plugin-github-openai-curated-noodzakelijk`
is the same repository and branch lineage as PR #36, not a separate product
repository. Its `HEAD` is `e07b9daeb3ba2630ecbe12a64d948f791203c241`, which is
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
