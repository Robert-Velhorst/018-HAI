# Supplemental child-batch audit (2026-07-30)

## Scope and inventory

Two additional local folders contain HAI child transcripts whose first-line
metadata links them to root session `019e7acc-44f2-7c90-a04e-253f6d43df28`.
They are not covered by `child-agent-transcript-manifest.csv` or its current
automated cleanup gate.

| Local batch | Files | Bytes | Completed | Retained |
| --- | ---: | ---: | ---: | ---: |
| `D:\codex-temp\completed-agent-sessions` | 3 | 4,651,926,405 | 3 | 0 |
| `D:\codex-temp\session-log-relief` | 6 | 9,361,810,893 | 0 | 6 |
| **Supplemental total** | **9** | **14,013,737,298** | **3** | **6** |

Compared with the existing 18-file manifest, these nine files add nine
non-overlapping child IDs. The combined known inventory is 27 files, 25
unique child IDs, 12 completed transcripts, 8 aborted transcripts, 7
nonterminal transcripts, and 34,752,906,420 logical bytes. This is not yet a
complete inventory of every HAI-related local file.

The machine-readable nine-row inventory is
`supplemental-child-transcript-manifest.csv`; the three preserved completed
final reports are in `supplemental-child-final-reports.md`. The supplemental
manifest is deliberately not treated as part of the existing automated
deletion gate.

## Completed reports and code disposition

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
