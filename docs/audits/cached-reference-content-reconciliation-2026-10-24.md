# Cached reference content reconciliation

Reviewed against canonical main `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.

## Conduct reference

The final `CODE_OF_CONDUCT.md` in the cached conduct-contact reference is an
exact main blob match. No duplicate code or documentation port is needed.
This does not assert preservation of every intermediate commit or merge parent.

## Historical reconciliation reference

All ten final touched documents in the older local-checkout reconciliation
reference have successors on main. Literal comparisons differ because main
contains a deliberate public export with private identifiers and machine paths
replaced by aliases, plus explicit historical-scope and retention warnings.

The comparison verified:

- Each original narrative audit line remains in order after explicit machine-path,
  session-ID and transcript-basename alias normalization and trailing-space
  normalization. Remaining changes are inserted warnings, not removed findings.
- The supplemental manifest retains all 13 rows and all 18 non-identifier
  columns with identical values in row order. Four identifying columns have
  public alias names and values instead of private source identifiers.
- All 29 non-identifier summary keys retain their original values. Main adds
  public-export and non-cleanup-manifest flags and a root-session alias.
- The supplemental README retains its introduction and references SHA-256
  values that match the actual main-tree manifest, summary and final-report
  export. These are export hashes, not hashes of the private source artifacts.

Files checked:

- `docs/child-agent-archive-2026-07-30/cleanup-readiness.md`
- Supplemental-audit README, final-report index, transcript manifest and summary
  under `docs/child-agent-archive-2026-07-30/supplemental-audit-2026-10-17/`
- `docs/hai-integration-snapshot-audit-2026-10-17.md`
- `docs/hai-patch-archive-reconciliation-2026-10-17.md`
- `docs/local-checkout-reconciliation-2026-10-15.md`
- `docs/local-reconciliation-2026-10-17.md`
- `docs/local-worktree-audit-2026-10-17.md`

This establishes retained final report content, not transcript-source verification
or full intermediate-history preservation. Original identifiers and their private
mapping remain outside the public export. Do not republish the old branch history
or feed the public alias manifest to source-verification/deletion tooling.

## PostgreSQL upgrade reference

Four final artifacts exactly match main: the canonical migration guide, isolated
acceptance-stack script, isolated-recovery contract test and runtime-role fixture.
The final equality of the role fixture does not make it correct: a separate
review found its missing image digest, outdated data path and version labels,
and prepared a focused correction with three regression tests.

Main also retains the PostgreSQL-preflight copy and read-only bind mount used by
the isolated acceptance stack. The earlier migration preflight's executable
noncomment lines match main; its documentation is superseded by the canonical
guide rather than missing.

## Remaining gates

No whole checkout, private source mapping, transcript, database or backup has
become deletion-eligible through this report. Remaining CI/Compose differences,
parent-aware history checks, prepared-fix publication and main integration are
separate gates. No container or database was started during this comparison.
