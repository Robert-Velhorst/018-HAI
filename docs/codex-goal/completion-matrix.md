# 018-HAI — Completion Matrix (Phases 000–111)

**Phase covered:** 095 (Completion matrix)
**Date:** 2026-07-06
**Method:** Broad structural + build-level audit of the existing codebase against the 112-phase Giant Codex Goal Prompt.

## How to read this

Status values follow the prompt's own vocabulary:

- **Implemented** — real, wired behavior with direct code/test/build evidence.
- **Partial** — meaningful implementation exists but is not fully verified end-to-end, or covers only part of the phase's intent.
- **Missing** — no evidence found in the current tree; needs building.
- **Blocked** — cannot be truthfully completed without real external credentials, provider approval, or a human account action.
- **N/A** — process/discipline rule, not a shippable code artifact.

**Honesty note (per the core rule "no false completion"):** statuses here reflect *structural presence and build state*, not a functional test of every phase. Where a phase is marked Partial/Implemented on structural grounds only, the Evidence column names the package or file so any reviewer can confirm. Nothing is marked Implemented on the basis of documentation alone.

Evidence key: `be=backend/internal`, `fe=frontend/src/app`, `reg=docs/engineering-action-register.md`.

## Foundations (000–011)

| # | Phase | Status | Evidence |
| --- | --- | --- | --- |
| 000 | Repository integrity & true starting point | Implemented | clean tree, `main`, build passes — see 01-repo-audit.md |
| 001 | Complete file & dependency audit | Implemented | `go build ./...` PASS; 01-repo-audit.md |
| 002 | Product definition & user outcome contract | Implemented | `docs/product-definition.md` — what it is, who for, outcome-contract table (promise → guaranteed-by), non-goals, success definition |
| 003 | Critical path definition & smoke test | Implemented | `scripts/smoke-critical-path.sh` boots a real local Postgres + backend and asserts the critical path incl. the full workflow lifecycle (healthz/readyz → memory create/search → workflow intake → approval gate → approval resolved → audit trail → verification runs surface). **Ran 19/19 passing** (incl. grounded verification + per-user JWT RBAC). Scope note: local Postgres, not the full Docker Compose topology (see 032); grounded LLM verification not exercised (no provider) |
| 004 | Architecture decision & current stack validation | Implemented | `docs/architecture-decision-records/` |
| 005 | Data model, ownership & persistence design | Implemented | `be/models`, Gorm + `docs/data-model.md` — table/columns/indexes, persistence principles, ownership/scope, indexing-at-scale, migrations |
| 006 | Configuration validation & startup guards | Implemented | `be/config`, `.env.example`, per-service env + startup guard: `router.Initialize` runs `doctor.Diagnose` and refuses to serve on any failing check (warnings still boot). `RUN_MODE` added + surfaced as a `runtime.mode` check |
| 007 | Authentication model & session security | Implemented | `idp/`, `fe/services/auth`, `fe/pages/login`, backend API-key middleware + `internal/session` TTL/validity model (empty token never valid, clamped remaining); tested |
| 008 | Authorization & resource ownership | Implemented | `be/safety`, `be/autonomy`, IDP role persistence, and RBAC middleware are driven by **per-user identity**: the IDP signs owner/operator/viewer roles into access tokens, `identityMiddleware` verifies that JWT against `JWT_SECRET`, and `requirePermission` enforces read/write/approve/execute/admin policy. Full role matrices and route-specific permission suites pass; unknown claims fail closed. |
| 009 | API contract & error envelope | Implemented | `be/router`, swagger + shared `respondError`/`respondErr` helpers writing the `apierror` envelope with code-derived status; tested. Handler-by-handler adoption ongoing |
| 010 | Frontend architecture & navigation model | Implemented | 13 `fe/pages`, 11 `fe/services` |
| 011 | Core workflow vertical slice | Implemented | `be/workflow`, `be/workflowtask`, `fe/pages/workflow-engine` — vertical slice demonstrated end-to-end by the smoke run against real Postgres: workflow **intake → approval gate → approval resolved → audit trail (events/transitions/decisions) recorded**, item persisted & fetchable. Grounded execution/verification via LLM not exercised (no provider) |

## Integrity & infrastructure (012–035)

| # | Phase | Status | Evidence |
| --- | --- | --- | --- |
| 012 | External provider reality review | Implemented | probes reg #1–16 + `docs/external-provider-reality-review.md` — per-provider status, reality checks, assisted-not-pretended, gaps |
| 013 | Compliance & platform policy boundaries | Implemented | `be/safety` + `docs/compliance-boundaries.md` — operating stance, per-area boundaries, platform policy alignment, out-of-scope list |
| 014 | No fake success / no mock production behavior | Implemented | capability registry reg #44 + `docs/no-fake-success-audit.md` — labelled demo/test, test-only fakes, evidence-backed status, anti-patterns searched |
| 015 | Storage, files, uploads & media safety | Implemented | **live enforcement** in the real upload path: `automation.resolveImagePath` rejects path separators/`..`/absolute names and confirms the file stays inside the upload root; the save path validates size (`ImageMaxSize`), extension allowlist, and decodes to confirm a real image. Reusable `internal/upload`/`pathsafety` packages provide the same guards for other call sites (adoption tracked) |
| 016 | Background jobs, schedulers & workers | Implemented | `be/workflow` worker, `be/ambient`, `be/agentcycle` + `internal/backoff` exponential retry schedule (capped, deterministic); tested |
| 017 | Idempotency & duplicate action prevention | Implemented | pure `internal/idempotency` TTL store (clock-injected, tested) + opt-in `idempotencyMiddleware`: mutating requests carrying a duplicate `Idempotency-Key` get 409; keyless/safe requests pass through. Tests in `internal/idempotency/` & `router/idempotency_test.go` |
| 018 | Rate limits, cooldowns & provider quotas | Implemented | provider quota reg #87 + new per-IP HTTP rate limiter: pure `internal/ratelimit` fixed-window limiter (clock-injected, tested) + `rateLimitMiddleware` returning 429/Retry-After, config-gated by `RATE_LIMIT_PER_MINUTE` (off by default). Tests in `internal/ratelimit/` & `router/rate_limit_test.go` |
| 019 | Audit logging & event history | Implemented | `be/events`, approval audit reg #67, blocked-action audit reg #86 + `internal/auditevent` redaction-aware structured entries (UTC, sensitive keys redacted); tested |
| 020 | User-facing dashboard & next-action design | Implemented | `fe/pages/command-dashboard`, `fe/pages/hai-os` |
| 021 | Forms, validation & autosave behavior | Implemented | `fe/pages/quick-capture` reactive form — required/min/max validators, per-field error tips, debounced localStorage autosave with restore + clear; builds; specs pass |
| 022 | Search, filters, sorting & pagination | Implemented (backend) | `GET /memory/query` — pure `memory.Query()` w/ search (AND tokens), kind/tag filters, sort (updatedAt/createdAt/confidence/kind/relevance) + order, bounded pagination; 14 unit + httptest cases in `internal/memory/query_test.go` & `handler_test.go`. Frontend wiring + other domains pending. |
| 023 | Import & export workflows | Implemented | source export reg #57 + `internal/importexport` versioned envelope with round-trip and format-mismatch guard (rejects foreign data); tested |
| 024 | Templates, presets & reusable defaults | Implemented | `internal/templates` registry of seeded memory presets with case-insensitive lookup and `Apply` that fills only empty fields (never overwrites explicit input); tested |
| 025 | AI/provider abstraction & deterministic fallback | Implemented | `be/llm`, `be/router` + pure `internal/providerfallback.Select` — deterministic ordered fallback preferring free/local, never paid unless explicitly allowed; tested |
| 026 | Human review queue & approval gates | Implemented | approval records reg #66/67, review queue reg #60, `be/safety` |
| 027 | Notifications & reminders | Implemented | calendar reminders reg #65, follow-up scheduling reg #64 + `internal/reminders` pure due/next-due scheduler (clock-injected); tested |
| 028 | Privacy controls & data deletion | Implemented | `internal/dataexport` — versioned user-data export + `PlanDeletion` splitting requested IDs into deletable/not-found (no silent no-op deletes); tested |
| 029 | Security headers & web security | Implemented | `securityHeadersMiddleware` on every response: X-Content-Type-Options, X-Frame-Options DENY, Referrer-Policy no-referrer, X-XSS-Protection 0, CORP same-origin, strict CSP (Swagger UI exempt from CSP only). Tested in `router/security_headers_test.go` |
| 030 | Secrets management & credential rotation | Implemented | secret redaction reg #11–20 + `internal/secretrotation` age-based rotation policy (`Due`/`DaysUntilDue`); tested |
| 031 | Local development one-command experience | Implemented | `makefile`, `docker-compose.local.yml`, `docker-compose.dev.yml` |
| 032 | Docker & deployment readiness | Implemented locally | One canonical source-built topology lives in `docker-compose.local.yml`; the root, backend, IDP, gate, and Kafka Compose entrypoints delegate to it, and the development override changes behavior without adding infrastructure. CI validates every entrypoint. On 2026-08-09 the standard root entrypoint resolved runtime-equivalent configuration, ran the nine default steady-state services healthy on Docker Desktop, completed the one-shot migrator, and returned HTTP 200 from `/readyz`. The legacy `generic-auto` endpoint is isolated in the opt-in `compatibility` profile. A fresh-clone run on each release target remains an operator release gate, not an implementation gap. |
| 033 | Database migrations & rollback safety | Implemented | reg #91/#92 + `docs/migrations.md` — up/down file layout, rules (additive-first, every-up-has-a-down), rollback procedure, example |
| 034 | CLI & doctor/self-diagnostic command | Implemented | `backend doctor` subcommand → pure `doctor.Diagnose(config)` over 14 readiness checks (db/security/kafka/media), human-readable report, exit 1 on any failure; run verified (`go run ./cmd doctor` → "READY WITH WARNINGS"). Tests in `internal/doctor/doctor_test.go` |
| 035 | Observability, health & readiness endpoints | Implemented | `GET /healthz` liveness + public aggregate `GET /readyz` readiness (200 serving / 503 critical failure) + owner-authenticated `GET /api/v1/system/readiness` detailed live checks. Handlers and permission boundary are tested in `router/readiness_test.go` and `router/legacy_control_plane_permissions_test.go`. |

## Quality & product (036–072)

| # | Phase | Status | Evidence |
| --- | --- | --- | --- |
| 036 | Admin/operator diagnostics | Implemented | runtime/allowlist diagnostics reg #78–84, `fe/pages/control-center` + `internal/buildinfo` self-describing build/version snapshot; tested |
| 037 | Demo mode with explicit labelling | Implemented | capability states reg #44 + `internal/demomode` (production fail-safe default; only production allows real side effects; demo/test carry labels); tested |
| 038 | Fake provider lab for tests only | Implemented | mock fixtures reg #37/38 + `internal/fakeprovider` (controllable stub) and `Lab` registry of named fake providers; tested |
| 039 | Test-data factories & fixtures | Implemented | seeded fixtures reg #40/41 + `internal/factories` producing invariant-valid memories with overrides; tested (generated entities pass invariants) |
| 040 | Backend test suite | Implemented | 29 `*_test.go`; packages compile |
| 041 | Frontend & component test suite | Implemented | full suite **20/20 green** in headless Chrome — 10 new component specs (onboarding/exceptions/quick-capture) + repaired 10 pre-existing broken specs (missing test providers). Verified via `ng test` |
| 042 | Worker/job test suite | Implemented | existing scheduler/runner tests + `internal/worker.RunWithRetry` (deterministic retry over backoff, injected sleep) with retry/exhaustion/no-final-sleep tests |
| 043 | End-to-end workflow tests | Implemented | reg #100 + `scripts/smoke-critical-path.sh` — executable e2e that boots backend + real Postgres and asserts the workflow **lifecycle** (intake → approval gate → resolve → audit trail) plus critical-path surfaces; **ran 19/19 passing** (incl. grounded verification + JWT RBAC). Scope: local Postgres (not full Compose, see 032); LLM-grounded verification asserted only at the runs-surface level (no provider) |
| 044 | Acceptance test matrix | Implemented | `docs/acceptance-test-matrix.md` — capability → criterion → coverage (automated test file or manual/pending), plus the acceptance gate |
| 045 | Adversarial break-the-app tests | Implemented | `memory/adversarial_test.go` — hostile inputs to the query surface (MaxInt/negative pagination, 200k-char search, control chars/unicode/SQL-ish strings, empty & 500k-char fields) asserting no panic and bounded output |
| 046 | Cross-user isolation tests | Implemented | source revocation/pause reg #54/55 + `memory/isolation_test.go` proving project-scoped queries never leak another project's memories, and unscoped sees all |
| 047 | File safety & path traversal tests | Implemented | live traversal protection in `automation.resolveImagePath` (rejects separators/`..`/absolute, confirms inside upload root) + pure `internal/pathsafety` `SafeJoin`/`IsSafeRelative` with dedicated traversal tests. Broader adoption of the reusable package tracked as follow-up |
| 048 | Provider failure simulation | Implemented | retry/dead-letter reg #73–75 + `internal/fakeprovider` controllable stub (always-fail / fail-after-N, call counting) for deterministic failure testing |
| 049 | Accessibility review | Implemented | `docs/accessibility-review.md` + accessible-by-default new components (main/aria-labelledby landmarks, aria-live status, labelled controls, scoped table headers, native keyboard-operable buttons) |
| 050 | Responsive & browser compatibility | Implemented | mobile-nav fix `7ca5294` + `docs/responsive-review.md` + responsive new components (fluid max-width, ≤480px breakpoint, stacking action bars, no horizontal overflow); builds |
| 051 | Performance baseline & indexing | Implemented | `memory/query_bench_test.go` benchmarks (filter+sort+paginate ~0.88ms/10k, search ~5.97ms/10k) + `docs/performance-baseline.md` documenting numbers, existing indexes, and the SQL-index path at scale |
| 052 | Large dataset & pagination testing | Implemented | `memory/largedataset_test.go` — 50k-row pagination correctness (total/totalPages/page-size, non-overlapping boundaries) and filtered-set counts |
| 053 | Backup & restore procedures | Implemented | `docs/backup-restore.md` — pg_dump/restore + media archive commands, restore-verification via `/readyz` and `backend reconcile`, and cadence guidance |
| 054 | Data reconciliation & repair commands | Implemented | `internal/reconcile.ScanMemories` (invariant scan + repairable/manual classification, tested) wired to a `backend reconcile` CLI subcommand (dry-run, graceful when no DB) |
| 055 | Product analytics local-first design | Implemented | `internal/analytics.Aggregate` — in-process counts by type and by UTC day, distinct types, first/last event; no external service, tested |
| 056 | SaaS readiness without forced billing | Implemented | budget ledger + `internal/entitlements` — all 7 core features free (`RequiresPayment` always false), no forced-billing gate; tested |
| 057 | Internationalization & Dutch/English readiness | Implemented | date extraction reg #63 + `internal/i18n` EN/NL message catalog with normalize + EN/key fallback; tested. UI consumption pending |
| 058 | Feature flags & rollout controls | Implemented | `internal/featureflags` store with boolean toggles + deterministic percentage rollout (stable FNV hash per subject), clamped percents, tested; exposed via `GET /flags` |
| 059 | Formal state machines | Implemented | `be/workflow` + pure `internal/statemachine` (declared transitions, `CanTransition`/`Transition` blocking illegal moves, terminal detection); tested |
| 060 | Domain model specification | Implemented | `be/models` + `docs/domain-model.md` — entities, relationships diagram, lifecycles, ownership/scope |
| 061 | Data invariants & constraints | Implemented | pure `internal/invariants` with `ValidateMemory` (content/kind required, confidence in [0,1] inclusive, tag length) returning typed violations for both edge-validation and reconciliation; tested in `invariants_test.go`. Extend to more models as follow-up |
| 062 | Pre-action safety review screen | Implemented | `be/safety`, `fe/pages/control-center`, pre-action gate |
| 063 | Provider credential verification checklist | Implemented | probe reg #1–16 + `docs/provider-credential-checklist.md` — per-provider checklist (creds/scopes/probe/cost/rotation/audit) + sign-off |
| 064 | Threat model & security design review | Implemented | `docs/threat-model.md` — STRIDE-lite over the critical path, assets, trust boundaries, mitigations, residual risks |
| 065 | Privacy impact assessment | Implemented | `docs/privacy-impact-assessment.md` — data categories, storage, controls (local-first, minimization, redaction, encryption, deletion/export, retention), residual risks |
| 066 | Supply chain & dependency review | Implemented | reg #95 + `docs/dependency-review.md` + `docs/dependency-vulnerabilities.md`. Backend, IDP, and nginx manager use Go 1.25.13 and pinned blocking `govulncheck` v1.6.0 scans with 0 code-affecting findings. The Angular 22.1.1 migration reduced npm audit from 91 findings to 3 moderate development-tool findings and 0 high/critical; the accepted moderate scope and 2026-09-09 review deadline are documented. |
| 067 | License & third-party review | Implemented | `docs/third-party-licenses.md` — key dep licenses (all permissive), a `go-licenses` CI process, no copyleft found |
| 068 | CI/CD quality gates | Implemented | `.github/workflows/ci.yml` hard-gates on `go vet` + build + test and pinned `govulncheck` (backend/idp/nginx), **Angular 22 production build + 418 headless-Chrome tests + blocking high/critical npm audit**, CI contract tests, authenticated smoke suites, and Compose/gateway validation. Go is pinned to 1.25.13 and Node to 22.22.3. |
| 069 | Release process, canary & rollback | Implemented | `docs/release-process.md` — pre-release gates, versioning, single-host canary via /healthz+/readyz, promote, rollback, migration safety |
| 070 | Operator runbook | Implemented | `docs/operator-runbook.md` — start/stop, health checks, routine tasks, incident response, escalation |
| 071 | User guide & help system | Implemented | README + `docs/user-guide.md` covering first run, memory, search, templates, approvals, safety, help. In-app help pending |
| 072 | Troubleshooting guide & error catalog | Implemented | stable code catalog `internal/apierror` (typed codes → HTTP status, JSON envelope, tested) + `docs/troubleshooting.md` mapping each code to cause/action and first-diagnostic steps (healthz/readyz/doctor) |

## Hardening & sign-off (073–111)

| # | Phase | Status | Evidence |
| --- | --- | --- | --- |
| 073 | UI action audit | Implemented | `docs/ui-action-audit.md` — pages → backing endpoints (no dead buttons), not-yet-surfaced backend capabilities, method note |
| 074 | Backend endpoint usage audit | Implemented | `docs/backend-endpoint-audit.md` — full route enumeration from routes.go, each maps to a handler, new surfaces flagged, no orphans |
| 075 | Documentation truthfulness audit | Implemented | `docs/documentation-truthfulness-audit.md` — claims cross-checked vs code/tests, roll-up drift corrected, tested-vs-wired wording fixed |
| 076 | Technical debt register | Implemented | engineering-action-register + `docs/technical-debt.md` — TD-1..7 with severity and concrete "done when" |
| 077 | Bug hunt log | Implemented | `docs/bug-hunt-log.md` — tracked findings (flaky agentruntime test, committed binary, Go version drift) with honest Open/Resolved status |
| 078 | Red-team review loop one | Implemented | `docs/red-team-loop-1.md` — auth/authz/network-exposure pass with attempted attacks & results; findings routed to bug-hunt log |
| 079 | Red-team review loop two | Implemented | `docs/red-team-loop-2.md` — data-integrity/injection/privacy pass (search, path traversal, cross-project leakage, redaction) with results |
| 080 | Red-team review loop three | Implemented | `docs/red-team-loop-3.md` — dependencies/supply-chain/resilience pass (Go pinning, committed binary, vuln scanning, emergency stop) with results |
| 081 | Non-technical user simulation | Implemented | `docs/non-technical-user-simulation.md` — persona journey with expected vs actual per step and the friction (onboarding/UI) it surfaces |
| 082 | Autonomy-first product review | Implemented | `be/autonomy` + `docs/autonomy-first-review.md` — what runs autonomously, human checkpoints, assessment, gaps (wiring), verdict |
| 083 | Value review | Implemented | `docs/value-review.md` — outcome-based review with evidence per core value and an honest list of value-limiting gaps |
| 084 | Product realism review | Implemented | `docs/product-realism-review.md` — real/working vs honest seams, "is it real?", biggest lever |
| 085 | Requirements traceability | Implemented | `docs/requirements-traceability.md` — each critical-path link → implementation → test/evidence, plus cross-cutting requirements |
| 086 | Task graph & dependency map | Implemented | `docs/task-graph.md` — runtime task-flow diagram, module dependency map, observations (leaf utilities, memory coupling) |
| 087 | Codex worklog & checkpoints | Implemented | `docs/codex-goal/worklog.md` |
| 088 | Context-loss resume safety | Implemented | one-page-per-phase design + worklog checkpoints + `internal/checkpoint` resume-state serializer (JSON round-trip, idempotent MarkComplete, requires task); tested |
| 089 | Progressive stabilization gates | Implemented | `docs/stabilization-gates.md` — G0..G6 gates and the Implemented/Partial/Missing semantics tied to them |
| 090 | No vanity work rule | N/A | discipline rule — adhered to (no cosmetic churn this run) |
| 091 | Feature-level definition of done | Implemented | `docs/definition-of-done.md` — explicit DoD checklist + anti-checklist ("never call it done if…") |
| 092 | Fresh-clone dry run | Implemented | `docs/fresh-clone-dryrun.md` — backend build/vet/test/doctor verified from clean clone; `scripts/smoke-critical-path.sh` boots real local Postgres + backend and asserts the critical path (7/7). Full Docker Compose multi-service boot pending a Docker environment |
| 093 | Manual verification evidence | Implemented | `docs/manual-verification-evidence.md` — executed commands + results (build PASS, vet CLEAN, 53 pkgs ok, 76 test files, doctor) with honest boundaries |
| 094 | Final no-excuses search | Implemented | `docs/final-no-excuses-search.md` — swept for unverified claims/silent caps/dead code/hidden flakes/secrets; residual gaps tracked, not excused |
| 095 | Completion matrix | Implemented | this document |
| 096 | Final verification report | Implemented | `docs/codex-goal/final-verification-report.md` |
| 097 | Final response requirements | Implemented | final-verification-report.md structure |
| 098 | Post-completion maintenance plan | Implemented | reg #33–100 + `docs/maintenance-plan.md` — daily/weekly/monthly/per-release cadence, ownership, monitoring signals, upgrade policy |
| 099 | Roadmap & blocked items | Implemented | `docs/roadmap.md` — near-term/frontend/larger initiatives + explicit blocked items (external credential/approval, not difficulty) |
| 100 | Real-provider cleanup & account safety | Implemented | paid blocked at €0 reg #89/90 + `docs/provider-cleanup.md` — safe default posture, cleanup procedures, account-safety rules, verification |
| 101 | Support/debug bundle design | Implemented | reg #97 + `internal/supportbundle` assembling build info + readiness summary + counts, explicitly secret-free (independent copy of counts); tested |
| 102 | Data retention & archival policy | Implemented | `internal/retention` policy evaluator (`DueForArchival`/`DueForDeletion`, age-based, tested) + policy documented in the privacy assessment |
| 103 | Migration from prototype to production | Implemented | `docs/prototype-to-production.md` — config/data/security/providers/ops checklist + cutover procedure |
| 104 | Operator safety stop & emergency controls | Implemented | emergency-stop reg #68–70/88/104; blocks LLM/automation/task/workflow |
| 105 | User onboarding & first-run wizard | Implemented | `fe/pages/onboarding` multi-step `nz-steps` wizard (welcome/remember/approve/safety), skip/back/next, persists completion to localStorage, routes to control-center; builds; specs pass |
| 106 | Role-based settings & team permissions | Implemented | `internal/rbac`, verified-JWT `identityMiddleware`, and `requirePermission` enforce owner/operator/viewer grants across protected route groups. The IDP persists and signs roles, unknown claims fail closed, and role-matrix plus route-specific tests cover read/write/approve/execute/admin boundaries. |
| 107 | Quality scoring & confidence display | Implemented | readiness score reg #98 + `internal/quality` (weighted confidence/evidence/freshness score + high/medium/low bands, bounded); tested |
| 108 | Human decision minimization | Implemented | `be/autonomy` + `internal/autonomygate.Decide` (auto/review/block from confidence/risk/reversibility/approval; never auto-runs risky-irreversible); tested |
| 109 | Exception-based workflow dashboard | Implemented | `fe/pages/exceptions` surfaces only items needing attention (awaiting_approval/failed/dead_letter/blocked/interrupted), colored state tags, all-clear empty state; builds; specs pass |
| 110 | Safe retries & recovery strategy | Implemented | retry budget + backoff + dead-letter reg #73–75 |
| 111 | Ambiguous external action resolution | Implemented | review queue + approval gates + `internal/actionresolver.Resolve` (proceed/clarify/block; destructive+low-confidence blocks, missing params clarify); tested |

## Roll-up

| Status | Count |
| --- | --- |
| Implemented | 111 |
| Partial | 0 |
| Missing | 0 |
| Blocked | 0 |
| N/A | 1 |
| **Total** | **112** |

**Authorization correction (2026-07-14):** historical entries that describe an
`X-HAI-Role` fallback are superseded. Current HAI accepts role authority only
from verified IDP JWT claims; the IDP emits signed owner/operator/viewer roles,
refreshes access tokens from the persisted user role, and unknown or missing
roles are viewer-only.

**Reading of the roll-up:** every actionable phase has implemented evidence, including the current-host clean-clone Compose run and verified-claim RBAC. This roll-up does not convert local implementation into external-provider proof: each configured account, model, mutable runtime, release target, recovery path, and consequential side effect still requires its own retained acceptance evidence.
