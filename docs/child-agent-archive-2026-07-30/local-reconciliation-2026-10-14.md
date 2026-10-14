# July 30 transcript archive reconciliation (2026-10-14)

## Scope and method

This read-only reconciliation scanned files named `*2026-07-30*.jsonl` under the local Codex temporary archive tree. It recorded first-record session metadata, file sizes, and JSON `event_msg/patch_apply_end` records. JSONL records were streamed with a 10 MiB per-record cap; oversized records were counted and skipped. No transcript was copied, modified, moved, or deleted.

## Current inventory

The local tree contains 22 matching rollout files totaling 31,573,466,825 bytes (31.573 GB / 29.406 GiB):

| Local relative directory | Files | Bytes | Status |
| --- | ---: | ---: | --- |
| `hai-completed-agent-sessions` | 13 | 17,559,729,527 | The 13 retained files from the published 18-row manifest are present. |
| `completed-agent-sessions` | 3 | 4,651,926,405 | Additional files outside that manifest. |
| `session-log-relief` | 6 | 9,361,810,893 | Additional files outside that manifest. |

All 22 files' first `session_meta` records identify the pinned HAI parent thread `019e7acc-44f2-7c90-a04e-253f6d43df28`. The nine files in the latter two directories were not included in the published 18-row manifest and need their own provenance/disposition review.

## Manifest paths absent from the local temp tree

The published 18-row manifest lists five completed cleanup candidates totaling 3,179,439,595 bytes. Their exact filenames and child IDs were searched across the local temp archive tree; none was found:

- `019fb20c-2b95-7550-8304-1117de872148`
- `019fb220-c797-77d1-8060-0f220cd733e1`
- `019fb287-b2e4-7251-8f1b-bc8fa9febc07`
- `019fb287-d1ed-75f0-99a8-8cd09c1ef9f2`
- `019fb287-f3be-7860-ba1f-e6b05088db73`

This establishes absence only within the searched temp tree, not whether the files were deleted, moved elsewhere, or preserved in another backup. The published manifest's prior `deletion_performed=false` statement is historical and does not establish current local presence. Do not infer a safe deletion or a completed cleanup from these missing paths.

## Bounded patch-event scan

Across the 22 files, the scan counted 766,103 JSONL records and found:

- 66,469 parsed `patch_apply_end` events: 66,424 successful and 45 unsuccessful.
- Zero malformed JSON among the candidate event records.
- Seven oversized records above 10 MiB, skipped by design.
- 788 unique path keys among the parsed events; 674 contain the historical HAI checkout prefix and 114 do not.

The event scan is incomplete because seven records were skipped. The matching parent-thread metadata and repeated histories mean the event and path counts are not counts of unique child changes. They do not prove that a patch was accepted in the final tree, identify which transcript uniquely authored it, or establish integration into current `main`. The existing path-audit script's prior missing-path conclusion must not be generalized to these extra files or to the skipped records.

## Disposition

No files are cleanup candidates on this evidence. Keep all 13 present retained transcripts, all nine out-of-manifest files, and any other unresolved HAI evidence. The five absent manifest entries need external backup/path reconciliation; they cannot be reconstructed from the manifest's report hashes alone. A future cleanup review requires a complete, reproducible per-file scan, child-specific attribution, source-to-current-main crosswalk, and verified preservation of every needed report and patch.
