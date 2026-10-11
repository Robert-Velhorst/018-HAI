# Repository Integrity & File/Dependency Audit

> Historical inventory. Compose entrypoints have since been consolidated into
> one source-built topology; see the root README and operator runbook for the
> current deployment contract.

**Phases covered:** 000 (Repository integrity and true starting point), 001 (Complete file and dependency audit)
**Audit date:** 2026-07-06
**Method:** Structural inspection + real build/compile execution. This is a broad audit pass; it establishes ground truth and a starting point, not a line-by-line functional verification of every subsystem.

## Core rule for this repo

> No mockups, no fake integrations, no false completion.

This audit and every artifact under `docs/codex-goal/` follows that rule literally: statuses record what is **actually present and verifiable**, and anything not verified is marked as such rather than claimed complete.

## Phase 000 — Repository integrity and true starting point

| Signal | Observed value |
| --- | --- |
| Default branch | `main` |
| Work branch for this run | `codex/hai-goal-run` (never merged to `main` without owner authorization) |
| Working tree at start | clean (`git status` → nothing to commit) |
| Starting commit | `0f7f12c Add local browser QA artifacts` |
| Recent history | Real feature commits — e.g. `ee82d34 Build HAI pursuit operating layer`, `3512c1f Integrate controlled Hermes and Odysseus runtimes`, `2108328 Build encrypted AI memory command dashboard` |
| Untracked/generated | `hai-engine-control.zip` (2.2 MB binary artifact committed at root — flagged, see Findings) |

**Conclusion:** This is an existing, actively-developed product, not an empty scaffold. Per the non-negotiable rules, existing work is **preserved and hardened**, not rewritten.

## Phase 001 — Complete file and dependency audit

### Top-level layout

```
backend/                 Go 1.21 module (canonical backend) — 23 internal packages
frontend/                Angular 16 dashboard — 13 feature pages, 11 services
idp/                     Identity provider service
gate/ nginx-config/      Gateway + routing
nginx-config-manager/    Gateway config management service
generic-auto/            Generic automation runtime
browser-extension/       Companion browser extension
automation-scripts/      Allowlisted scripts (read-only mount)
connected-sources/       Allowlisted local ingestion sources
kafka/                   Kafka assets
docs/                    12 blueprint/ADR docs + swagger + engineering register
docker-compose*.yml (x3) Local / dev / default compose stacks
makefile, init.sql       Local one-command dev + DB bootstrap
```

### Backend packages (`backend/internal/`)

`agentcycle`, `agentruntime`, `ambient`, `assistant`, `automation`, `autonomy`, `config`, `events`, `haios`, `infra`, `llm`, `memory`, `memoryengine`, `models`, `pursuit`, `router`, `safety`, `source`, `task`, `util`, `verification`, `workflow`, `workflowtask`.

These map directly onto the critical path required by the prompt:
`dashboard -> source ingestion/memory -> task planning -> LLM/tool routing -> approval gates -> controlled execution -> verification -> workflow state -> audit`.

### Frontend feature pages (`frontend/src/app/pages/`)

`home`, `login`, `command-dashboard`, `hai-os`, `connected-sources`, `memory`, `workflow-engine`, `task-blueprint`, `llm-policy`, `grounded-answers`, `pursuits`, `ambient-brain`, `control-center`.

### Dependency & build reality (executed, not assumed)

| Check | Command | Result |
| --- | --- | --- |
| Go toolchain | `go version` | `go1.25.6 darwin/arm64` (module targets Go 1.21) |
| Backend builds | `go build ./...` | **PASS** (exit 0); all modules resolved & compiled |
| Test packages compile | `go test -run=NONE ./internal/safety/... ./internal/verification/...` | **PASS** (`ok`, no-tests-to-run) |
| Backend test files | `find backend -name '*_test.go'` | **29** files |
| Frontend spec files | `find frontend -name '*.spec.ts'` | **8** files |
| CI | `.github/workflows/ci.yml` | present |

Full runtime smoke (Postgres/Redis/Kafka/nginx via Docker Compose) was **not** exercised in this pass — it requires the container stack. That is recorded as `Partial / needs-runtime-verification` in the completion matrix, not as done.

## Findings (carried into the technical-debt register)

1. **`hai-engine-control.zip` (2.2 MB) committed at repo root.** Binary artifacts in git bloat the repo and are opaque to review. Recommend extracting to source or moving to release assets / `.gitignore`.
2. **Toolchain drift.** `go.mod` declares Go 1.21; local toolchain is 1.25.6. Builds pass, but CI should pin the Go version to avoid silent drift (feeds phase 068 CI/CD quality gates).
3. **Runtime smoke.** *(Resolved for the critical path.)* `scripts/smoke-critical-path.sh` now boots a real local Postgres + the backend and asserts the critical path end-to-end (ran 7/7). The full **Docker Compose** multi-service boot remains pending a Docker environment (feeds phases 031/035).

## Verdict

Repository integrity: **sound and safe to build on.** Real product, clean tree, passing build. Starting point established.
