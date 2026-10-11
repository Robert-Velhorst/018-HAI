# Documentation Truthfulness Audit

Checks that documentation describes what the code actually does — no aspirational
claims presented as shipped behavior.

## Method

Cross-check each significant claim in the docs against code/tests/executed
commands. Flag anything that overstates.

## Results

| Claim source | Claim | Verdict |
| --- | --- | --- |
| completion-matrix | "N Implemented" counts | **Corrected** — hand-maintained roll-up had drifted from row states; now recomputed from the actual rows. |
| matrix rows | Each Implemented row cites a package/file/endpoint | Verified — evidence is concrete and checkable. |
| performance-baseline | Benchmark numbers | Verified — numbers came from an executed `go test -bench` run. |
| fresh-clone-dryrun | "clean checkout initializes and boots the canonical stack" | Verified on the current Windows host on 2026-08-09, including generated secrets, empty volumes, healthy services, first-run sign-in, and a bounded governed workflow. Repeat per release target. |
| readiness/doctor docs | `/readyz`, `backend doctor` behavior | Verified against tests and a live `go run ./cmd doctor`. |
| status labels | "Implemented" vs "Partial" | Mostly accurate; several "Implemented" are **tested packages/docs not yet wired end-to-end** — the rows say so and `stabilization-gates.md` defines the distinction. |

## Corrections applied

1. Completion-matrix roll-up recomputed to match actual rows.
2. "Implemented" evidence wording distinguishes tested helpers from live-path
   adoption. RBAC and upload safety are now live-path controls; shared error and
   filesystem helper adoption remains incremental where explicitly recorded.

## Standing rule

No doc may claim a capability as shipped unless code + a test (or an executed
command) backs it. Aspirational items live in the roadmap, not in feature docs.
