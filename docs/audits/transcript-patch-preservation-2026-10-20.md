# Retained transcript patch reconciliation - 2026-10-20

Baseline: main `e521f213eeece4500b569859598c702466876462`.

This extends the [oversized-record audit](transcript-oversized-records-2026-10-20.md).
It records preservation evidence, not authorization to delete the retained corpus.
No original archive, source worktree, credential, or database was changed or removed.
Private inventories, source names, transcript text, and local paths are not published.

## Structured event inventory

A bounded reader traversed all 13 remaining files, totaling 17,559,729,527 bytes
and 442,443 records. Every full-file SHA-256 matched the earlier baseline, and
file identity, size, and modification time remained stable during indexing.

The reader indexed actual top-level patch-completion events, not quoted marker
text. It found 58,960 file-change entries: 58,890 flagged successful and 70 not
flagged successful. Exact path/type/move/diff/content fingerprints reduce the
successful entries to 5,742 variants. This is not a count of missing features:
the same patch can recur against different states, and a success flag does not
prove that its result survived later changes or reached main.

There were 59 record/payload/change decoding failures and two oversized records.
The failures still require individual diagnosis; the counter includes both JSON
syntax and typed-payload decoding failures. The oversized records retain the
separate identity and limitations documented in the preceding audit.

Of the 5,742 variants, 5,503 map to the 11 previously inventoried HAI worktrees.
Another 239 reference paths outside those roots. They are not necessarily
unrelated: the two small child transcripts, for example, reference a separate
HAI port worktree. Their provenance must be established before interpreting them.

## Literal main comparison

Reading canonical Git blobs rather than a potentially dirty source tree produced
the following triage for the 5,503 mapped variants:

| Classification | Variants |
| --- | ---: |
| Every nonempty new-side hunk occurs on main | 1,107 |
| Added file matches main exactly after CRLF normalization | 104 |
| Deleted path remains absent | 4 |
| Deleted path has since been reintroduced | 17 |
| Added file has changed | 482 |
| Add/update target is absent | 24 |
| Other content review required | 3,765 |

Literal presence is useful evidence, not complete semantic reconciliation.
It does not establish execution order, deletion side effects, rename handling,
equivalence of changed implementations, or preservation of all non-patch work.
This comparison used the previously indexed offsets; it did not repeat the
full-file hashing pass. No classification automatically changes retention.

## Two small, aborted child transcripts

One child recorded changes to task evidence matching. All eight new-side patch
hunks are present on main, including the four regressions covering conflicting
protected values, negation, unrelated generic provenance, and valid evidence.
Those four tests passed in a fresh focused native Go run.

The other child recorded eight file changes for constitution history. The added
history test file matches main exactly. Twelve of fourteen update hunks match
literally. The remaining differences were inspected directly: `cloneTimePointer`
moved from the service into the repository file in the same package, and the
history handler replaced unchecked integer parsing with bounded request
validation. The original bounded ordering, owner isolation, lifecycle metadata,
and immutable version digest remain present. The focused constitution-history
tests passed in a fresh native Go run.

Current frontend source also contains the history client, response validation,
Advanced history section, separate unavailable and empty presentations, and
component assertions. This audit inspected that source; it did not perform a
new browser, Angular, or external-provider acceptance run. Both transcripts end
with an abort event, so their terminal markers alone cannot prove integration.
The reviewed package, router, and frontend files match the pinned main baseline.

## Damaged-copy comparison

Both NUL-padded archives were compared byte-for-byte with their corresponding
intact preservation copies. Full-file hashes matched the earlier baseline.
Every nonzero byte in each damaged copy occurs at the identical offset in its
intact counterpart; there are no differing pairs of nonzero bytes. The only
differences are respectively 1,013,503,439 and 903,999,051 NUL bytes where the
intact counterpart contains nonzero data.

This proves a narrow preservation relationship between local copies, not
integration into main. Neither damaged copy was removed under the main-first
cleanup rule. The intact copies, all remaining archives, decode failures,
unreconciled variants, and non-patch decisions remain retained for further work.
