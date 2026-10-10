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
