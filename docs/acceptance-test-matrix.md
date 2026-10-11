# Acceptance Test Matrix

Maps user-visible capabilities to the tests that prove them. "Automated" means a
Go test asserts it; "manual" means a documented step.

| Capability | Acceptance criterion | Coverage |
| --- | --- | --- |
| Remember context | Duplicate memories merge; retrieval ranks relevant ones | Automated — `memory/service_test.go` |
| Search memories | Filter by kind/tag, sort, paginate; bounded | Automated — `memory/query_test.go`, `handler_test.go` |
| Large datasets | Pagination correct at 50k rows | Automated — `memory/largedataset_test.go` |
| Hostile input | No panic; bounded output | Automated — `memory/adversarial_test.go` |
| Project isolation | No cross-project leakage | Automated — `memory/isolation_test.go` |
| Readiness | `/readyz` = 200 ready / 503 not-ready | Automated — `router/readiness_test.go` |
| IDP dependency readiness | Container-local `/readyz` is 200 only when the identity database and Redis session-revocation store respond; failures are generic and bounded | Automated — `authentication/auth_service_test.go`, `router/router_test.go`, `services/block_list_service_test.go`; Compose YAML validation |
| Config self-check | `doctor` exits non-zero on failure | Automated — `doctor/doctor_test.go` |
| Security headers | Present on responses; Swagger CSP-exempt | Automated — `router/security_headers_test.go` |
| Rate limiting | 429 over limit; off by default | Automated — `router/rate_limit_test.go` |
| Ordinary mutation idempotency | Duplicate key -> 409 within the configured process-local replay window; opt-in | Automated - `router/idempotency_test.go` |
| Task operation identity | `/task/plan`, `/task/run`, and `/task/success` use an owner-scoped PostgreSQL operation ledger: one active claimant, exact completed replay, changed-input conflict, lease fencing, and fail-closed review after an expired/uncertain attempt | Automated - `task/task_operation_test.go`, `migrations/task_operation_identity_postgres_test.go`; live gateway replay documented in the operator verification record |
| Governed portfolio dispatch | A signed-in owner explicitly selects approved proposal items; each item is revalidated, authorized, and creates at most one review-gated local workflow. Exact retries replay or resume without creating approvals, running workflows, settling reservations, or invoking external providers. | Automated - `pursuit/portfolio_dispatch_test.go`, `migrations/pursuit_portfolio_dispatch_coordination_contract_test.go`, router permission coverage, and Pursuits service/component tests; live PostgreSQL append-only and invalid-provenance trigger probes |
| Advisory monitor collector accuracy | For each fixed source kind, seed a disposable owner-scoped PostgreSQL snapshot, run one target, and compare the numeric value and deterministic source digest with the expected workflow/open-loop, verified-completion, or overdue-commitment records. No arbitrary collector input is accepted. | **Required acceptance** - focused collector and security tests exist; parent must record the disposable-PostgreSQL result before marking verified |
| Monitor collection replay | Replay the same completed run/idempotency input before and after composition attempts. The existing immutable observation/run and its single composition delivery are reused without recollection or duplicate observation evidence; changed collection input conflicts. Collection replay must not report composition success while the delivery is pending or dead-lettered. | **Required acceptance** - focused service/repository and migration coverage exists; parent must verify the composed PostgreSQL and signed-HTTP chain |
| Monitor composition retry and receipts | Force a transient composition failure, verify an immutable failed attempt receipt and a future `pending` retry, then verify a later fenced claim can succeed without recollection. Exhaust the bounded policy separately and verify `dead_lettered`, immutable receipts, stale-worker refusal, and no duplicate downstream advisory record. | **Required acceptance** - migration `0050` contract and disposable-PostgreSQL lifecycle tests cover the ledger; scheduler/composer crash-boundary and end-to-end acceptance remain required |
| Monitor composition snapshot fidelity | Delay a delivery while changing the outcome definition, proactivity policy/feedback, or composer version, then demonstrate which snapshot the retry used. | **Partially verified, full chain remains open** - historical outcome evaluation selectors now retain and revalidate their exact revision/digest; five real PostgreSQL groups passed three race-enabled repetitions on 2026-09-30. This does not prove the whole `0050` delivery chain pins proactivity policy/feedback or composer versions. See the dated verification record; full advisory snapshot replay must not yet be claimed. |
| Monitor owner isolation | Create equivalent target IDs for two owners/workspaces. Target, observation, run, delivery, attempt-history, due-claim, and recovery reads reveal only the authenticated owner; foreign resource probes return no record. | **Required acceptance** - handler/repository and migration tests exist; a two-owner signed HTTP exercise is required |
| Monitor disable and lease fencing | A disabled target is not claimed for collection. An active unexpired collection or composition lease cannot be replaced or completed by another worker; only an expired lease can be recovered, after which a new generation may claim it. | **Required acceptance** - service/migration tests exist; parent must record the collection lifecycle against `0049` and the composition lifecycle against `0050` |
| Monitor no-effect boundary | Configure, collect, replay, compose, fail, retry, dead-letter, recover, pause, and resume a monitor while asserting zero task/runtime execution, notification/message delivery, Calendar write, workflow mutation, mandate authorization, external provider call, or controlled-learning mutation. Every target, evidence, delivery, and attempt authority flag stays false. | **Required acceptance** - security/handler/UI coverage exists; signed-in integration proof remains required |
| Governance monitor UI | In Governance Control, load an existing outcome indicator, create a fixed target, inspect status, run a bounded due pass, inspect immutable observations/runs, pause it, and resume it. Errors name a recovery path and duplicate clicks do not start duplicate work. | **Required acceptance** - Angular component/service coverage exists; parent must complete authenticated browser verification |
| Provider fallback | Free before paid; never paid unless allowed | Automated — `providerfallback` |
| Autonomy gate | Risky/irreversible never auto-runs | Automated — `autonomygate` |
| Upload safety | Traversal/extension/size rejected | Automated — `upload`, `pathsafety` |
| Full-stack boot | Compose stack healthy end-to-end, including an authenticated browser flow | **Required CI gate** — `browser-acceptance` provisions an isolated production-mode Compose stack, waits for `/readyz`, and runs `frontend/e2e`. The first successful hosted run and a fresh Windows 11 target-machine run remain release evidence to record. |
| Accessibility / responsive | WCAG + breakpoints | **Pending** — frontend work |

## Gate

A change is acceptable when: `go vet` clean, `go test ./...` green, `/readyz`
ready against the target config, and any new user-visible capability has a row
here with automated coverage (or a documented manual step if automation isn't
yet feasible).
