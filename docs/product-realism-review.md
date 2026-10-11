# Product Realism Review

Honest assessment of whether 018-HAI is a real, working product versus a
scaffold — and where the seams are.

## Real and working

- Substantial backend (Go): ~50 packages, ~50 test packages passing, real
  services for memory, workflow, sources, verification, LLM routing, safety.
- Real Angular dashboard with 13 feature pages wired to real endpoints.
- Genuine safety machinery: approval gates, emergency stop, redaction, retry/
  dead-letter.
- Verified from a clean clone: build + vet + test + `doctor` all pass.

## Honest seams

| Area | Reality |
| --- | --- |
| Shared safety utilities | Real and tested. The task engine enforces `autonomygate` and `actionresolver`; RBAC and upload/path safety are enforced on live routes. Shared error-envelope and filesystem-helper adoption remains incremental where recorded in `docs/technical-debt.md`. |
| Full-stack boot | The critical-path smoke and the canonical Docker Compose topology have both run on this Windows host. A separate clean checkout generated fresh secrets, built empty volumes, reached healthy state, signed in, and completed a bounded governed workflow. Each distinct release target still needs the same retained acceptance run. |
| Real external providers | Intentionally disabled pending OAuth/scope review — assisted, not automated. |
| Frontend polish | Accessibility, responsive matrix, onboarding wizard not done. |

## Is it real?

Yes — the critical path is genuinely implemented and exercised, not mocked. It is
a real product with a mature backend and a functional UI, plus a clearly-scoped
set of not-yet-wired capabilities and frontend polish remaining. Crucially, the
gaps are **documented as gaps**, not disguised as done.

## Biggest lever

Keep the current full-stack and permission gates green, then close the remaining
provider-specific and mutable-runtime acceptance boundaries with scoped consent,
idempotency, postcondition evidence, and retained audit records.
