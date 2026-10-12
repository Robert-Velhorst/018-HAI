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

Inventoried the 26 files under `C:\Users\NO\.codex\archived_sessions` by
session metadata and searched for the canonical HAI repository identifiers.
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
`docs/child-agent-transcript-manifest.csv` under
`C:\Users\NO\.codex\sessions\2026`. All 104 previously deleted candidate
paths remain absent, and all ten `retain` paths still exist. However, only one
of the ten retained files currently matches its recorded `logical_bytes`;
nine differ. Their combined current file lengths total `3,207,933,505` bytes,
while the manifest records `21,882,851,783` bytes. The cause and content-level
integrity of the nine mismatches have not been established. Do not delete,
rewrite, or normalize these retained files; the 2026-08-09 receipt is
historical evidence of its then-current post-delete check, not proof of their
current lengths or contents.

The pinned HAI parent-session log
`C:\Users\NO\.codex\sessions\2026\05\30\rollout-2026-05-30T23-31-07-019e7acc-44f2-7c90-a04e-253f6d43df28.jsonl`
currently measures `14,826,950,232` bytes and contains references to each of
the ten retained child IDs (234-336 occurrences per ID). These matches include
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
