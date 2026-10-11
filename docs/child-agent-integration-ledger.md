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

The secondary checkout at
`C:\Users\NO\Documents\Codex\2026-05-30\github-plugin-github-openai-curated-noodzakelijk`
is the same repository and its `e07b9dae` HEAD is an ancestor of the PR head.
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

PR #36 is currently open at `d6cb0903dd63e15ed9da685c8f42d831eda8c535`.
Its exact-head Actions run `38108110220` remains queued overall; some checks
have started or passed, while many matrix jobs remain queued or in progress.
This is not final CI acceptance. The PR has not been merged into `main`.

Archive byte/hash integrity and candidate-report presence do not prove the
unresolved transcript patch/event content is semantically integrated. The
793-path event crosswalk, retained/aborted child attribution, and remaining
source-difference dispositions are still incomplete. Therefore the eight
candidate files (about 7.94 GB) remain candidates only, the ten retained
files (about 12.80 GB) remain protected, and no local transcript or checkout
cleanup is authorized by the repository readiness gate.
