# Supplemental child-batch audit (2026-07-30)

## Current bounded-scan disposition (2026-10-15)

This section reflects the committed 10 MiB bounded-scan manifests and summary
files. Records larger than the configured bound are skipped by the parser and
their transcripts remain retained; an earlier completion observation does not
override the current bounded scan.

| Scope | Files | Logical bytes | Cleanup candidates | Retained | Oversized records skipped |
| --- | ---: | ---: | ---: | ---: | ---: |
| Supplemental roots | 9 | 14,013,737,298 | 2 | 7 | 5 |
| Main archive root | 18 | 20,739,169,122 | 5 | 13 | 2 |
| **Combined known inventory** | **27** | **34,752,906,420** | **7** | **20** | **7** |

The counts above match the current machine-readable manifests and summary
files. Hubble and Popper are the two supplemental candidates; their reports
map to source paths recorded in the audit below. The other seven supplemental
transcripts remain retained, including five marked
`retain_unparsed_candidate_record`. The main archive manifest has five
candidate rows, but their source paths were not present during the 2026-10-15
read-only reconciliation: 13 retained/duplicate-ID paths matched their
recorded lengths, while the five candidate paths (3,179,439,595 bytes in
total) had no file or same-name copy under the checked audit temp root. This is
a source-integrity mismatch, not evidence that those files were safely
integrated or intentionally removed. Preserve the remaining source files and
resolve the missing-path discrepancy before changing dispositions.

The supplemental source paths and lengths matched all nine manifest entries
during the same check. The five skipped oversized records and incomplete
supplemental-root cleanup coverage still prevent treating this scan as a
deletion receipt. No transcript was removed. This is not a complete inventory
of every HAI-related local file, and candidates are not blanket deletion
authorization.

Machine-readable results are in
`supplemental-child-transcript-summary.json` and
`supplemental-child-transcript-manifest.csv`. Main-archive results are in
`child-agent-transcript-summary.json` and `child-agent-transcript-manifest.csv`.

## Scope and inventory

Two additional local folders contain HAI child transcripts whose first-line
metadata links them to root session `019e7acc-44f2-7c90-a04e-253f6d43df28`.
They are not covered by `child-agent-transcript-manifest.csv` or its current
automated cleanup gate.

| Historical full-parse observation | Files | Bytes | Observed completed | Retained by bounded rescan |
| --- | ---: | ---: | ---: | ---: |
| `D:\codex-temp\completed-agent-sessions` | 3 | 4,651,926,405 | 2 | 3 |
| `D:\codex-temp\session-log-relief` | 6 | 9,361,810,893 | 0 | 6 |
| **Supplemental total** | **9** | **14,013,737,298** | **2** | **9** |

Compared with the existing 18-file manifest, these nine files add nine
non-overlapping child IDs. The combined known inventory is 27 files, 25
unique child IDs, and 34,752,906,420 logical bytes. Statuses from this older
full parse are historical observations, not the current bounded-scan
disposition; use the current summary above for cleanup decisions. This is not
yet a complete inventory of every HAI-related local file.

The machine-readable nine-row inventory is
`supplemental-child-transcript-manifest.csv`; the three preserved completed
final reports are in `supplemental-child-final-reports.md`. The supplemental
manifest is deliberately not treated as part of the existing automated
deletion gate.

## Historical completion reports and code disposition

The rows below document prior report/source cross-checks. They do not override
the current scan's unattributed status or make any transcript eligible for
cleanup.

| Child | Work | Source disposition | Final-message SHA-256 | Transcript SHA-256 |
| --- | --- | --- | --- | --- |
| `019fb087-5a29-7983-bc56-3f8956867155` (Hubble) | Framework Registry validation focus, owner authority, read-only viewer behavior, credential/error redaction, accessible actions, duplicate-submit guard, and clearing approval drafts after authority loss. | All 14 report-named files are tracked in the PR: `frontend/src/app/pages/framework-registry/` component, inspector, recommendation, and module files; `frontend/src/app/services/framework-registry.service.ts` and its spec. | `3e92dc21b2f993737e85467b3f762c8f78b0d43cde358e1eaab2813e5aaecc10` | `a9a2dc0a6fd91c539b5819b91dbf75c54463e91902b0942ccd219cf857345df7` |
| `019fb090-112e-7741-8c8b-1f78cafa14d6` (Popper) | Replaced an unsupported icon, registered shipped icons globally, and removed duplicate icon logic from Model Routing. | All five report-named files are tracked: `frontend/src/app/app.module.ts`, `frontend/src/app/services/theme.service.ts` and its spec, and `frontend/src/app/pages/llm-policy/llm-policy.component.ts` and its spec. | `80cb0c9bb4b12ef684fe6da81a4a6ace8328415639100b9bc1b215a14819ffbe` | `cc6f2e5766d97c5a3a0f3cad3b0803e938c4389b39825ad4ba9950d527076506` |
| `019fb09c-e5bb-7c31-b6c4-59479eaca1e6` (Anscombe) | Strengthened CI evidence aggregation, liveness checks, exact migration-test matching, uncached race tests, timeouts, and regression contracts. | Both report-named files are tracked: `.github/workflows/ci.yml` and `scripts/test_ci_contract.py`. | `52478a75db344592832394bcd6197ac6cf7d6c5f34c65eb2c1a2572061fba352` | `1aa5acf74e7f07d870b52ec01b50f56c71b910638582a6a99b877f6652518c39` |

Path presence was checked against PR worktree `codex/hai-runtime-release` at
`5f2e29ea44f87929b12e376e2ef4126fb9608d89`; all 21 distinct report-named
paths were present and tracked. Verification on that source tree passed the
frontend suite (175 Node checks and 1,331 Angular tests) and
`python scripts/test_ci_contract.py` (108 tests). The local Node version was
25.2.1 while `.nvmrc` requires 24.15.0; tests passed, but no production build
was run in this batch. These checks establish source presence and test
results, not a full historical patch-by-patch semantic reconstruction.

## Retained transcripts

| Child | State | Path | Bytes |
| --- | --- | --- | ---: |
| `019fb0ab-52f6-74b0-bf05-b3b8539e1ade` (Maxwell) | Aborted (`interrupted`) | `session-log-relief/rollout-2026-07-30T03-37-25-019fb0ab-52f6-74b0-bf05-b3b8539e1ade.jsonl` | 1,602,767,769 |
| `019fb1e6-d4d1-7e72-a99b-a69efb24bb1e` (Confucius) | Nonterminal | `session-log-relief/rollout-2026-07-30T09-22-02-019fb1e6-d4d1-7e72-a99b-a69efb24bb1e.jsonl` | 1,548,412,854 |
| `019fb200-3f6f-73b3-9a65-6bb698282c5d` (Turing) | Nonterminal | `session-log-relief/rollout-2026-07-30T09-49-48-019fb200-3f6f-73b3-9a65-6bb698282c5d.jsonl` | 1,548,199,820 |
| `019fb203-62c9-7311-b945-b92b0a9739a2` (Bacon) | Nonterminal | `session-log-relief/rollout-2026-07-30T09-53-14-019fb203-62c9-7311-b945-b92b0a9739a2.jsonl` | 1,547,750,866 |
| `019fb204-9a2b-7e90-9c56-752cda3997d4` (Chandrasekhar) | Nonterminal | `session-log-relief/rollout-2026-07-30T09-54-33-019fb204-9a2b-7e90-9c56-752cda3997d4.jsonl` | 1,549,071,735 |
| `019fb218-3df4-7f60-a937-0cba84e0c0ce` (Avicenna) | Nonterminal | `session-log-relief/rollout-2026-07-30T10-16-01-019fb218-3df4-7f60-a937-0cba84e0c0ce.jsonl` | 1,565,607,849 |

The audit status and patch-call counts for these six are tail-derived from the
final 16 MiB of each transcript. A zero tail count does not prove that a
transcript contains no earlier edits or useful decisions. The full retained
event/content crosswalk remains outstanding. All six stay protected.

## Cleanup boundary

The original readiness verifier validates only the 18 paths rooted at
`hai-completed-agent-sessions`. It does not validate either supplemental
folder. The three completed files above are byte-hash verified, but are not
eligible for deletion until their report/code crosswalk is incorporated into
the canonical archive manifest and the cleanup tooling verifies all source
roots. The six retained files are not deletion candidates. No file was
changed, moved, or deleted during this audit.

## 10 MiB full-file rescan update (2026-10-12)

The scanner now accepts records up to 10 MiB; all nine files across both
supplemental roots were rescanned. Two completed transcripts in
`completed-agent-sessions` are now uniquely attributed, report-preserved, and
SHA-256 hashed: Hubble (`019fb087-5a29-7983-bc56-3f8956867155`) and Popper
(`019fb090-112e-7741-8c8b-1f78cafa14d6`). Their reports map to 14 and 5 source
paths respectively; all 19 paths exist in the current PR tree. The 1.54 GB
Anscombe transcript remains unattributed: its full scan has no unique child
turn prefix or terminal event and it stays retained.

The full supplemental inventory is 9 files / 14,013,737,298 bytes: 2
candidate transcripts / 3,113,294,358 bytes and 7 retained / 10,900,442,940
bytes. Five over-limit records across five files remain unparsed and retained.
The current manifest and summary record the 10 MiB bound, candidate hashes,
and `deletion_performed: false`. No transcript was removed. These are
source-integration facts only; cleanup still requires the canonical-main gate
and tooling that verifies both supplemental roots.
