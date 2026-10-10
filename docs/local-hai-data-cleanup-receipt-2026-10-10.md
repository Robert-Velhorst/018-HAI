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

The separate 20.7 GB completed-session archive remains untouched. Its manifest
and source hashes verify 18 files: eight completed unique transcripts totaling
7,939,888,699 bytes are cleanup candidates, while ten duplicate-ID, aborted, or
nonterminal transcripts remain protected. Transcript deletion still requires
the cleanup ledger on merged `main`.

Six Temp fixture directories remain. At this inventory, none met the 24-hour
retention threshold and two did not match the generated fixture layout; no Temp
files were removed.
