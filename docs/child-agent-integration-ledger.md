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
