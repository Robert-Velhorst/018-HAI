# Retained transcript record audit - 2026-10-20

This read-only audit resolves the two oversized-record observations left
open in the October 17 transcript review. It does not establish complete
semantic integration of the retained archive into HAI.

## Scope and integrity

- Read all 13 files in the retained completed-agent archive: 17,559,729,527
  logical bytes.
- Used bounded 1 MiB reads and SHA-256 hashes. File lengths and modification
  times were unchanged across each full-file scan.
- Found exactly two records larger than the previous 32 MiB audit limit.
- Found no identical full-file hashes among these 13 files. Duplicate-like
  filenames and equal lengths therefore do not establish duplicate content.
- Re-read both oversized byte ranges and verified their hashes against the
  full-file scan's range hashes.

Raw transcript names, local paths, contents, and private hash manifests are
not included in this public report. Source files remain retained locally.

## Oversized records

| Record alias | Total bytes | Nonzero prefix bytes | Trailing NUL bytes |
| --- | ---: | ---: | ---: |
| A | 1,016,062,444 | 2,559,005 | 1,013,503,439 |
| B | 904,740,623 | 741,572 | 903,999,051 |

Neither range contains a newline. A separate full-range byte count, followed
by a bounded prefix read, proved that all nonzero bytes occur in the prefix
and all remaining bytes are NUL padding.

Strict JSON decoding still fails after excluding the padding from the
in-memory inspection: both prefixes end inside an unfinished JSON string.
Reading only the complete leading fields with the standard JSON decoder
identifies both top-level record types as `compacted`; the incomplete payload
field is `replacement_history`. Neither byte range contains the raw
`patch_apply_end` marker.

These are truncated compaction records, rather than unexamined top-level
patch-completion events. This resolves the identity of the two records
previously skipped for size. It does not establish that every summarized
decision or source change in the damaged compaction history is represented
in main, or explain how the padding and truncation occurred.

## Remaining work and retention

The valid transcript records, candidate source variants, and differing
worktrees still need content-level reconciliation. This audit supplies no
basis for treating the entire archive as integrated or deleting files based
on names, lengths, or the absence of a patch marker alone.

No original transcript was truncated, repaired, moved, or removed. Inspection
results are separate artifacts; the damaged source records remain intact for
future recovery or comparison with another preservation copy.
