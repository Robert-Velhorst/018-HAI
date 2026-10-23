# Frontend dependency reconciliation

## Scope and preservation

The retained lockfile and current lockfile were parsed completely as npm lock
version 3 data. This is structured dependency-metadata review, not review of all
upstream library source or downloaded package contents. No dependency was
installed, upgraded, removed or downgraded by this pass; the current lockfile
bytes remain unchanged.

| Comparison | Records, including the application root |
| --- | ---: |
| Retained packages map | 683 |
| Current packages map | 593 |
| Identical metadata records | 357 |
| Changed metadata records | 177 |
| Removed historical locations | 149 |
| Added current locations | 59 |

All historical direct dependency names remain represented. Current declarations
include the aligned Angular 22.2.3 family, NG-Zorro 22.1.2, zone.js 0.16.3,
updated type declarations and the added Less build dependency. The historical
versions were not restored over the current graph. The current manifest also
declares Node 26.12.0 compatibility and npm 12.2.1 as its package manager.

Both parsed graphs record only HTTPS registry.npmjs.org downloads and SHA-512
integrity strings. These metadata properties are not proof of publisher trust,
tarball safety, signature verification or correct runtime behavior. The same
five locations declare install hooks in both graphs: Parcel watcher, esbuild,
fsevents, lmdb and msgpackr-extract. Their script bodies were not audited here.

## Local validation

Four new native tests check manifest/lock root parity, presence of direct
dependency records, registry URL and integrity metadata, and the existing
explicit install-hook allowlist. They are wired into the ordinary frontend test
command. No second dependency manager or custom dependency resolver was added.

Running the complete native portion initially passed 188 of 189 cases. Its one
failure was an older source assertion requiring selection-history errors to hide
retained rows, contrary to the already-rendered stale-history fix. Update that
assertion to require the error, retry, retained-row and guarded-empty branches;
add a separate retained-row assertion. All 190 native cases then pass.

The installed npm 11.6.2 CLI, run with Node 26.12.0, accepts the full current
logical tree using `npm ls --package-lock-only --all --json`. No problems are
reported. This is not an installation/reproducibility check with the manifest's
prescribed npm 12.2.1, and no installed dependencies were changed.

## Advisory snapshot and open work

The public npm registry audit was queried on 2026-10-23 using lockfile-only,
ignore-scripts requests. Production-only auditing reports zero known findings;
the full graph reports two high and one low package-level findings, involving
follow-redirects, Vite and their Angular build-tool parent. These counts are a
dated advisory snapshot, not a guarantee that the frontend or repository is
secure. Advisory conditions and actual application exposure need separate review.

The reviewed [follow-redirects advisory](https://github.com/advisories/GHSA-q3jh-wrc6-jgq8)
identifies affected 1.16.0 releases and patch 1.16.1. The reviewed
[Vite advisory](https://github.com/advisories/GHSA-9jrq-w75r-8gcw) includes the
8.3.0 through 8.3.2 line and identifies patch 8.3.3; it describes conditional
custom-middleware exposure. The full npm report includes additional advisories
for these package records. This pass does not claim all reported issues have
the same conditions or that an exploit was reproduced.

Do not blindly apply npm's suggested Angular build downgrade from 22.2.3 to
22.1.9. A targeted dependency repair needs approval, compatible parent/override
selection, refreshed lock integrity, native/Angular/build validation, and a
repeat full audit. Patch permission has been requested; no update or clean
dependency install was performed here. Keep that remediation open rather than
using the clean production-only report as full-toolchain acceptance.

The retained lockfile and source worktrees remain intact. Publication of this
review is neither a main-preservation receipt nor local deletion authority.
