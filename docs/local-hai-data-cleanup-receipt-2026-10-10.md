# Local HAI data cleanup receipt

Date: 2026-10-10 (Europe/Amsterdam)

## Detached Docker volumes removed

The guarded `scripts/remove-hai-detached-volume.ps1` operation removed exactly
these four detached HAI volumes from Docker context `desktop-linux`:

| Volume | Recovery bundle | Archive bytes | Archive SHA-256 |
| --- | --- | ---: | --- |
| `018-hai-kafka-kraft-data` | `hai-volume-recovery-a1489c6f199e4b8c93bdfe864d8275d5` | 10,201,979 | `acffd54c11a877a80eb948bdf47a52141aa2fc2745a88b61c68ad9a11ebab32c` |
| `018-hai-ollama-local-data` | `hai-volume-recovery-8520099a46d940f39c72c5fcc520a2c5` | 384,800,505 | `4bce3222e106f8d082035f4a30f232d508bfd9e859653a6f28dff910f8f7dab2` |
| `018-hai-redis-data` | `hai-volume-recovery-32738cc8c1cd46228bb7e9533a02925a` | 663 | `4ae6bea1884d69a2c34e1d434ccdf86dd7c5a32b67883e3248d4916404f7a9fd` |
| `018-hai-redpanda-data` | `hai-volume-recovery-a428f75bb9e6442a9a44d4c3ce87fb82` | 274,228 | `44fa5c75b3db1e2faf980e9ce6a81623c963539bcdae49fb236805068c73e704` |

Immediately before each removal, the script verified the exact source was
detached and matched its archive, validated the archive manifest and hash, and
checked the recorded restore drill. Postflight confirmed all four selected
volume names were absent. Their recovery bundles remain under
`%LOCALAPPDATA%\HAI\volume-recovery`.

The byte count above is archive size, not measured host-space reclaimed. Docker
Desktop's backing disk compaction and physical free-space change were not
measured. No global prune or other-project cleanup was run.

## Retained local state

- Both Postgres data volumes remain attached to their healthy containers.
- `018-hai-phase2-control-state` remains attached to HAI containers.
- HAI containers, images, worktree, toolchain, diagnostics, and test evidence
  were not removed by this operation.
- Joyce, ShareT, and LARO containers were not touched.
- Recovery bundles that do not pass the exact verification/ACL contract remain
  retained for review.

## Follow-up verification

The readiness verifier distinguishes an archive whose source still exists and
matches from an integrity-verified archive whose source was removed. The latter
checks the manifest, archive size and SHA-256, recorded successful restore
drill, and absence of the source volume and container references. It reports
`safe_to_remove=false`; it does not authorize deleting the recovery archive.

At the follow-up inventory, three named HAI volumes remained and all were held:
the two attached Postgres volumes and attached phase-two control state. Four
removed-volume recovery archives passed archive-only integrity verification.

A separate guarded cleanup removed four additional recovery directories only
after proving their archive files were exact byte-for-byte and SHA-256 copies
of the canonical Kafka recovery archive, their paths and ACLs were private to
the current user and SYSTEM, and the canonical archive remained intact. The
removed duplicate bundle IDs were:

- `hai-volume-recovery-39d10f85f0f84e03b4fae095a42c9e0d`
- `hai-volume-recovery-9415d2d9532f4e918d1caf5b817719d2`
- `hai-volume-recovery-ad2f86d3d7fe48eb9af37dc39fa607fa`
- `hai-volume-recovery-ffdcf3ec070549eaad7ac5fc615af55c`

The four directories contained 40,808,612 bytes in total. This is deleted file
size, not a measurement of physical disk space reclaimed. The differing,
unverified recovery bundle remains retained.

The volume retirement scripts now also support the two Postgres data volumes
and phase-two control-state volume, but only through the same exact-name,
detached-source archive and restore-drill flow. Removing one of those
persistent volumes additionally requires `-AllowPersistentDataRemoval` and an
exact confirmation phrase containing the selected volume name(s). The scripts
do not stop services or alter attached volumes. The current live readiness
check still finds these three volumes attached, so none is eligible now.
Recovery archives remain local after source-volume removal; this workflow does
not by itself reclaim the archive's disk space or authorize deleting backups.

The separate 20.7 GB completed-session archive remains untouched. Its manifest
and source hashes verify 18 files: eight completed unique transcripts totaling
7,939,888,699 bytes are cleanup candidates, while ten duplicate-ID, aborted, or
nonterminal transcripts remain protected. Transcript deletion still requires
the cleanup ledger on merged `main`.

Six Temp fixture directories remain. At this inventory, none met the 24-hour
retention threshold and two did not match the generated fixture layout; no Temp
files were removed.

## Unified read-only readiness report

Run `pwsh -NoProfile -File scripts/get-hai-local-cleanup-readiness.ps1` from
the inventoried PR worktree to collect the current Temp-fixture, transcript,
Docker-volume/recovery, PR-diagnostic, and image-removal gates in one JSON
report. The command invokes only read-only readiness/dry-run paths; it never
passes `-Apply`, deletes files, removes Docker resources, or authorizes cleanup.
Missing scripts, failed checks, and malformed reports are reported as blocked.
The transcript source archive is independently enumerated and candidate hashes
are verified even when the remover's canonical-main/merged-PR gate is still
closed. Its integrity result is separate from deletion readiness; a verified
source archive does not authorize removing its contents.

New isolated-acceptance runs write a cleanup provenance marker before writing
the synthetic environment. A crash after that marker but before the environment
is complete can be identified as an interrupted synthetic fixture; the marker
must have the exact owner/project identity and empty environment hash metadata.
The two pre-existing markerless partial fixtures remain unverified and are not
made cleanup candidates by this change.

Use each existing narrowly scoped remover separately only after reviewing that
target's current report and satisfying its independent exact-path, identity,
backup/retention, PR, and confirmation requirements. The active PR worktree and
toolchain, retained transcripts, attached data volumes, and unverified recovery
bundles are preservation targets, not candidates for broad cleanup.

## Latest read-only recheck (2026-10-10, 07:17 Europe/Amsterdam)

The unified image cleanup allowlist now covers all five removable first-party
tags in the archived six-image inventory: backend latest, backend-migrate,
IDP, frontend, and nginxconfigmanager. The `018-hai-backend:local` backup and
restore image remains excluded. The remover validates each image against its
Compose service/build source, ownership labels, unique tag, and container
references; it removes only the exact requested image tag and does not prune.

Live read-only Docker inventory in `desktop-linux` found six HAI tags. Four
remain referenced by containers (`backend-migrate`, `backend:local`, IDP, and
frontend; each has one reference), while
`018-hai-backend:latest` and `018-hai-nginxconfigmanager:latest` have no
container references. Their reported sizes are 249 MB and 29.2 MB; these are
image-size estimates, not guaranteed disk savings. No image was removed. The
three named HAI volumes remain attached (phase2 control state to two
containers; each Postgres volume to one container), and are not safe
to remove. One recovery bundle remains unverified (10,189,079 bytes) and is
retained.

The six Temp fixture folders were rechecked: zero are cleanup candidates; four
verified fixtures are about 14.4 hours old, and two markerless partial folders
remain unverified. They were not removed. The 20.7 GB transcript archive and
active PR worktree/toolchain/diagnostics were not modified.

The Temp-fixture remover was hardened to repeat its read-only readiness audit
for each directory after `ShouldProcess` confirmation, immediately before
recursive removal. It compares the owner, exact path, byte count, file hashes,
retention age, Docker references, reparse-point state, and running-process
references. Its live dry run still reports zero candidates and performs no
deletion.

PR #36 remains open at `ba9c67e140545cdb564d615ba6451ef91a1cc5b8` and is
unstable. CI run `38027085175` is still running: the dedicated HAI
cleanup-readiness safety-contract job has passed, while repository secret
scan, Promptfoo safety image, and two-account isolation have failed so far;
other jobs were still pending or running at the last check. The merge/removal
gate remains closed. The expanded image-removal contract, unified inventory
contract, Temp fixture cleanup and removal contracts, transcript cleanup
gate, volume/image inventory contract, PowerShell parser, and local Compose
validation passed in this session. No data or containers were deleted or
stopped during the recheck.

## Latest read-only integrity and cleanup recheck (2026-10-10, 08:25 Europe/Amsterdam)

The unified readiness command was rerun with `-VerifyTranscriptArchive`. This
performed a full read-only enumeration of the July 30 source archive and SHA-256
verification of every candidate transcript. It reported:

- 18 source files totaling exactly 20,739,169,122 bytes; all source paths and
  sizes match the committed manifest, and all eight candidate transcript hashes
  match. The ten aborted, nonterminal, or duplicate-ID transcripts remain
  retained.
- Eight completed unique-ID transcript candidates totaling 7,939,888,699
  bytes. They remain blocked because PR #36 is open and the remover requires
  execution from canonical `main` after the PR is merged.
- Nine Temp fixture directories totaling 659,988 bytes. Seven are younger than
  the 24-hour retention threshold and two lack verified cleanup manifests, so
  zero are currently eligible.
- Three HAI volumes remain attached: phase-two control state is referenced by
  two containers, and the automation and IDP PostgreSQL volumes are each
  referenced by a healthy running database container. No volume is eligible.
  The 10,189,079-byte recovery bundle with an unverified ACL remains retained.
- Docker's verbose disk report measured the attached phase-two volume at 164 B,
  automation PostgreSQL at 858.8 MB, and IDP PostgreSQL at 65.13 MB (about
  924 MB combined as reported by Docker). The old `018-hai-ollama-local-data`
  source volume is absent. Its separate 384,801,204-byte recovery bundle remains
  under the local HAI recovery root; the earlier removal receipt records its
  archive hash and restore drill. That bundle is retained backup data, not an
  unreferenced Docker volume, and this workflow has no automatic backup-purge
  path. Keep it until Robert makes an explicit retention decision.
- Two image tags have no container references (backend latest and
  nginxconfigmanager latest), but remain held by the image-retention gate. No
  image was removed.
- The exact 15-file PR diagnostic allowlist totals 31,505,884 bytes and remains
  blocked until PR #36 is merged at the verified local HEAD.
- PR #36 is open at `f0b3d40110672aa86c835fe4f4c650bcac5a4318`. At this
  snapshot, 20 checks passed, seven failed, and nine were queued or in progress.
  Therefore the PR is not eligible for merge or cleanup-gate completion.
- The active PR checkout has no tracked changes and 23 untracked files. The
  separate C: HAI checkout has 692 modified tracked files and 2,475 untracked
  files; it remains preserved because its work has not been reconciled with the
  PR checkout. The shared Go toolchain also remains preserved.
- A read-only same-relative-path comparison of all 3,167 C: checkout status
  entries found 872 same-size, SHA-256-identical files, 715 files with different
  content or size, and 1,580 paths absent from the PR worktree. The C: status
  entries occupy 152,672,874 bytes. This is overlap evidence only: matching
  paths may still be untracked in the PR checkout, and no source path is thereby
  approved for deletion. The 2,295 non-identical or destination-missing entries
  make whole-checkout cleanup unsafe.

This pass changed no source archive, Temp fixture, Docker container, volume,
image, recovery bundle, diagnostic, or worktree file. The report's
`cleanup_authorized` and `deletion_performed` fields remained `false`. The prior
1.6 GB estimate for detached HAI volumes is not current evidence: this live
inventory found three attached named volumes, and older detached volumes
already have separate recovery records in this receipt. Re-inventory all
targets immediately before any future apply operation.

## Structured Docker volume-size check (2026-10-10, 08:33 Europe/Amsterdam)

The read-only volume readiness script now reads Docker's structured
`system df --verbose --format json` output and includes each named volume's
reported size. If the CLI does not return that optional data, the status is
`unavailable`; that metric cannot change volume eligibility or authorize
cleanup. The live report returned `reported` and confirmed:

| Volume | Reported size | Container references | Disposition |
| --- | ---: | ---: | --- |
| `018-hai-phase2-control-state` | 164 B | 2 | Retain; attached |
| `018-hai-postgres-automation-data` | 858.8 MB | 1 healthy container | Retain; attached |
| `018-hai-postgres-idp-data` | 65.13 MB | 1 healthy container | Retain; attached |

The local Ollama volume remains absent; the separate 384,800,505-byte archive
was verified by the readiness script, with its recorded restore drill passed.
That backup stays protected pending a retention decision. No Docker resource or
recovery file was removed. The volume and unified readiness contract tests and
PowerShell parser passed after this code change; the live readiness command
returned `safe_to_remove=false` for every attached volume.

## Latest full read-only inventory (2026-10-10, 08:42 Europe/Amsterdam)

The unified readiness command was rerun from the PR worktree with
`-VerifyTranscriptArchive`. It completed successfully in read-only mode. The
report is not an authorization to delete anything:

- The completed-session source archive still has 18 files totaling
  20,739,169,122 bytes. Source paths and sizes match the manifest; all eight
  completed-transcript candidate hashes verify. The eight candidates total
  7,939,888,699 bytes. Their remover still requires canonical `main` after PR
  #36 is merged. The ten protected transcripts remain retained.
- Temp has nine HAI acceptance directories (659,988 bytes total), with zero
  eligible candidates: seven are younger than 24 hours and two interrupted
  fixtures have no verifiable cleanup manifest.
- The three named HAI Docker volumes are still attached. Docker reports 164 B
  for phase-two control state, 858.8 MB for automation Postgres, and 65.13 MB
  for IDP Postgres. The Postgres containers are healthy/running. The old Ollama
  source volume remains absent; its separate verified recovery archive is
  retained. A separate 10,189,079-byte recovery bundle still has unverified
  ACL provenance and remains retained.
- Two image tags without container references (backend `latest`, 249 MB; and
  nginxconfigmanager `latest`, 29.2 MB) remain held by retention review; no
  image is eligible under the current gate.
- The 15-file, 31,505,884-byte PR diagnostic allowlist remains blocked until
  PR #36 is merged at the exact verified HEAD. The active worktree has no
  tracked changes and 23 untracked files; the separate C: checkout still has
  692 modified tracked files and 2,475 untracked files, with 2,295 entries
  that are not byte-identical matches in the PR checkout. Both checkouts and
  the shared Go toolchain remain preserved.
- PR #36 is open at `23ac8dd7acec381701f4c8a907bd66b9114ad343` and GitHub
  reports it as unstable. CI run `38031485799` completed with seven failures:
  authenticated control-plane smoke, browser acceptance, Postgres migration
  integration, Promptfoo safety runner image, repository secret scan (redacted
  output), two-account isolation, and Windows installer/signing guards. The
  dedicated HAI cleanup-readiness safety-contract job passed. These failures
  leave the merge/removal gate closed; no secret-scan findings or values are
  copied into this receipt.

No source archive, transcript, Temp fixture, Docker resource, image, recovery
bundle, diagnostic, checkout, or toolchain was changed or removed by this
inventory. Both `cleanup_authorized` and `deletion_performed` remain `false`.

## Follow-up read-only inventory (2026-10-10, 09:00 Europe/Amsterdam)

The unified readiness script was rerun with `-VerifyTranscriptArchive` from the
PR worktree. It completed successfully and verified all eight transcript
candidate hashes. The archive still contains 18 files totaling 20,739,169,122
bytes; the eight completed candidates total 7,939,888,699 bytes and remain
blocked until PR #36 is merged. The ten non-candidates remain protected.

The current Temp inventory contains nine fixture directories (659,988 bytes).
Seven are younger than the 24-hour minimum and two have no verifiable cleanup
manifest, leaving zero candidates. The three named HAI Docker volumes remain
attached and non-removable; the 10,189,079-byte recovery bundle still has
unverified ACL provenance. The separate Ollama recovery archive remains
retained. Two unreferenced image tags (249 MB backend and 29.2 MB
nginxconfigmanager) remain held by the image-retention gate. The exact
15-artifact PR diagnostic allowlist (31,505,884 bytes), active PR worktree
(23 untracked files), secondary checkout (692 modified tracked and 2,475
untracked files), and shared Go toolchain remain preserved.

PR #36 is still open at `00c0b69bf30f41adfddb0c0020f5ee2d0795022a`. The
latest GitHub check snapshot has 29 successes and seven failures, so the
merge-dependent cleanup gates remain closed. The dedicated cleanup-readiness
safety contract passed in the recorded run; the failed checks are not treated
as evidence that any artifact is safe to remove. This recheck changed no
archive, fixture, volume, image, recovery file, diagnostic, checkout, or
toolchain. `cleanup_authorized` and `deletion_performed` remain `false`.

## Bounded cleanup operations (2026-10-10, 09:15 Europe/Amsterdam)

Cleanup readiness and the destructive volume/image runners now invoke Docker
through a bounded process helper. A timeout terminates the Docker CLI process
tree and fails closed. If a volume or image removal request times out, the
runner attempts a fresh read-only postflight; it reports confirmed absence,
confirmed presence, or an explicitly unknown outcome. An unknown outcome is
not reported as `deletion_performed=false` and must not be retried until fresh
inventory succeeds.

The bounded-process behavior and the Temp, transcript, volume/archive, image,
diagnostic, and unified-readiness safety contracts passed locally. PowerShell
parser checks and `git diff --check` passed. These checks validate the local
implementation only; they do not prove Docker Desktop behavior under every
failure mode.

The change was committed as `ba562961645b3735e6913ffb7f6e959dadd454ee` and
pushed to PR #36. GitHub confirms the PR remains open and unstable; its newly
triggered checks were still pending at the time of this receipt. The current
read-only inventory found zero eligible Temp fixtures, eight transcript
candidates totaling 7,939,888,699 bytes still gated on PR merge, three attached
HAI volumes, and 15 PR diagnostic/tool files totaling 31,505,884 bytes still
gated on PR merge. No source archive, transcript, Temp fixture, Docker
resource, image, recovery bundle, diagnostic, checkout, or toolchain was
changed or removed.

## Provenance-gated Temp recheck (2026-10-10, approximately 09:24 Europe/Amsterdam)

The unified read-only inventory was rerun after tightening synthetic fixture
provenance checks. It found nine Temp fixture directories (659,988 bytes) and
zero eligible candidates. Six validated fixtures are still younger than the
24-hour minimum. Two manifest-less, one-file fixtures now pass the exact
synthetic-environment validation but remain under that same age hold. Three
other fixtures are retained as unverified because at least one environment
value differs from the tracked acceptance-generator defaults; their values
were not copied into this receipt. No fixture was removed.

The update adds explicit provenance and source hashes to any future removal
record. Eleven cleanup safety contracts passed, including Temp readiness and
removal, transcript gates, volume/archive gates, image removal, diagnostic
allowlisting, duplicate recovery-bundle protection, and unified readiness.
All four changed PowerShell files passed parser checks and `git diff --check`.
These are local code and contract checks, not proof of external CI or Docker
behavior.

The same inventory still reports eight completed transcript candidates
(7,939,888,699 bytes) blocked until PR #36 is merged; three attached HAI
volumes; a 10,189,079-byte recovery bundle with unverified ACL provenance; two
image tags held for retention review; and 15 diagnostic/tool artifacts
(31,505,884 bytes) gated on the exact PR head being merged. The PR worktree,
secondary checkout, and shared toolchain remain protected. `cleanup_authorized`
and `deletion_performed` remain `false`; this pass changed no local cleanup
targets.

## Archive hash and complete Docker mount audit (2026-10-10, approximately 09:34 Europe/Amsterdam)

The July 30 transcript readiness verifier was run read-only against
`D:\codex-temp\hai-completed-agent-sessions` with source-archive verification
enabled. It verified an exact 18-file, 20,739,169,122-byte archive against its
manifest and ledger, including SHA-256 matches for all eight completed
candidate transcripts (7,939,888,699 bytes). Ten retained transcripts remain
in the manifest. `cleanup_gate_ready` remains false because PR #36 has not
merged; no transcript was changed.

The Docker readiness audit exposed a reporting gap: three anonymous volumes
mounted by HAI-named containers were listed separately but omitted from the
main volume inventory. The report now unions named HAI volumes, Compose-labeled
HAI volumes, and mounts discovered from HAI-named containers. The live report
therefore marks inventory incomplete and exits 2 until those unrecognized
anonymous volumes receive explicit ownership and recovery coverage. It records
their mount destinations and references, assigns a hold disposition, and never
marks them removable. Docker reports 0B for the two anonymous Postgres-init
mounts and 1.139kB for the Redis mount; the three persistent named volumes
remain attached, including the 858.8MB and 65.13MB Postgres data stores.

The live Docker query found no `018-hai-ollama-local-data` volume and no
Ollama-named volume. The older 1.6GB volume estimate is not reproduced by the
current Docker inventory and must not be used as a deletion target. Shared
Docker build cache is also excluded because it is global and not attributable
to HAI alone. No container, volume, image, build cache, transcript, or local
file was removed; `cleanup_authorized` and `deletion_performed` remain `false`.

## PR validation gate recheck (2026-10-10, run `38035011669`)

After commit `eaf5708808355523c9b7dbf50171f6832692401e` was pushed, GitHub
reported PR #36 still open and unstable. The completed workflow run had 29
successful jobs and seven failures. The HAI cleanup readiness safety-contract
job passed. Other failures were: a redacted repository secret-scan failure; a
Postgres integration migration attempting to add a duplicate primary key to
`ai_conversation_archives`; ten high production-dependency advisories in the
Promptfoo runner; browser-stack startup failure; authenticated smoke requests
returning HTTP 401; two-account isolation exit code 2; and the Windows installer
payload/safety contract. Secret-scan findings and values are intentionally not
copied into this receipt.

These failures are merge blockers, not cleanup approval. PR #36 is not merged,
so transcript and PR-diagnostic cleanup remain gated. The local archive,
volumes, images, Temp fixtures, worktrees, toolchain, and untracked diagnostics
remain unchanged; no deletion was performed.
