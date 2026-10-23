# Trello test corpus reconciliation

## Retained source review

Retained `backend/internal/source/trello_test.go` variants of 118,991 and 98,521
bytes were compared with the current test corpus. Every retained Go declaration
was parsed and token-compared, retaining comments, literal text and statement
order. Token-identical bodies were not all manually reread.

The model-maintenance variant has 75 declarations: 42 token-identical and 33
changed. The artifact variant has 59: 22 token-identical, 36 changed and one
replaced import group. Every retained function and import remains present. The
repaired current file has 86 declarations. No older file was copied over it.

The complete model-to-published diff (542 added and 37 removed lines) and
artifact-to-model diff (520 added and 42 removed lines) were read through their
ends, followed by the metadata repair and its regression test.

Preserve canonical board/card identity fixtures, pagination boundary
deduplication, header-only read credentials, restricted credential destinations,
exact loopback endpoints, timeout/cancellation checks, retry-budget limits,
later-page failure cursor retention and aggregate record/byte limits. Keep the
newer member/start/due-completion context and deterministic assignee rendering.

Some expectations deliberately changed: unchanged cards are hash-deduplicated
rather than all reindexed at a cursor boundary, and the workflow-intake failure
case expects `failed` rather than `partial_failure`. Unchanged assertion strength
or identical old behavior is not claimed. Canonical IDs and activity records are
synthesized by fixtures, not obtained from a live provider.

## Metadata reconciliation repair

The audit reproduced an actual inconsistency through `service.Sync`: after a
board rename, the activity probe advanced its cursor but skipped the card
snapshot, leaving the old board name in retained card context.

Newer metadata-only activity now requests full bounded reconciliation. Its
cursor is only proposed by the probe, then combined with card activity after
the snapshot succeeds. Existing item filtering still uses the original cursor
and content hashes. Failures do not commit the proposed cursor, and unchanged
metadata at the already-reconciled boundary keeps the cheap quiet path.

The new service-level test failed before repair. After repair it checks initial
context, failed-snapshot cursor retention, renamed context, successful cursor
advancement and an unchanged subsequent sync without another card snapshot.
The existing quiet-probe table now distinguishes a proposed metadata cursor from
a successful quiet observation; it includes an already-reconciled boundary case.

## Verification and limits

The targeted metadata/probe cases and the complete uncached source package pass.
The same targeted cases also pass with the `integration` build tag, compiling
that test surface without selecting or executing PostgreSQL cases.
The earlier Trello-named run also passed before the repair. These are local HTTP
fixtures with synthetic credentials, not real Trello account verification.
Database-dependent tests need their own configured environment; a passing local
package command does not establish PostgreSQL durability or live-provider safety.

This repair does not implement periodic full snapshots or prove completeness
under concurrent changes, invisible provider actions or permissions changes.
The quiet path still skips stored-card reconciliation when it observes no new
relevant activity. The remaining production service variants are separate work.

The selected ledger covers 604 of 618 files. Other checkouts and transcript
archives remain open. Original files and archives remain intact: review, PR
publication, preservation on `main` and deletion are separate gates.
