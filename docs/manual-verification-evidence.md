# Manual Verification Evidence

Executed commands and their results — the evidentiary basis for status claims.
Reflects the final post-implementation state.

## 2026-08-14 Windows signed ambient-monitor acceptance

The rebuilt Windows stack completed the signed Governance Control due-run,
pause/resume, and crash-recovery lifecycle against a disposable PostgreSQL
database. The clean isolated pass retained exactly one outcome revision, target,
observation, run, composition delivery, and successful composition attempt;
three target command receipts represented create, pause, and resume. The final
target was enabled with no remaining lease. Workflow items/events, automation
launches, execution-authorization receipts/consumptions, and final-effect
exercise tables remained empty.

See
[`docs/evidence/ambient-monitor-windows-acceptance-2026-08-14.md`](evidence/ambient-monitor-windows-acceptance-2026-08-14.md)
for the signed UI steps, exact database reconciliation, isolation method, and
remaining evidence boundaries.

## Commands run

| Command | Result |
| --- | --- |
| `go version` | `go1.25.6 darwin/arm64` |
| `go build ./...` (backend) | **PASS** |
| `go vet ./...` (backend) | **CLEAN** |
| `go test ./...` (backend) | **55 packages ok**, 0 failures |
| `find backend -name '*_test.go' \| wc -l` | **78** test files |
| `go run ./cmd doctor` | `12 ok, 2 warn, 0 fail` → READY WITH WARNINGS |
| `go run ./cmd reconcile` | runs (dry-run; graceful without DB) |
| `npm run build` (frontend) | **PASS** (Angular production build) |
| `ng test --watch=false --browsers=ChromeHeadless` | **20/20 SUCCESS** (headless Chrome) |
| `find frontend/src -name '*.spec.ts' \| wc -l` | **11** test files |
| `scripts/smoke-critical-path.sh` | **19 passed, 0 failed** — critical path + workflow lifecycle + grounded verification + per-user JWT RBAC |
| `govulncheck ./...` | after upgrade: **17** code-affecting vulns (was 20) — advisory; remediation in `docs/dependency-vulnerabilities.md` |

The two `doctor` warnings are the expected empty `BACKEND_API_SHARED_KEY` and
`HAI_MEMORY_ENCRYPTION_KEY` under default local config — correctly surfaced, not
failures.

## What this proves

- The backend compiles, vets clean, and passes its full unit suite (55 packages).
- The Angular app builds and its unit suite is green (20/20 in headless Chrome),
  including 10 new component specs and 10 repaired pre-existing specs.
- CLI subcommands (`doctor`, `reconcile`) run and behave as documented.
- The **critical path runs end-to-end**: `smoke-critical-path.sh` booted a real
  local PostgreSQL + backend and asserted **19/19** — `healthz`, `readyz`, memory
  create → persist → search, the **workflow lifecycle** (intake → approval gate →
  resolve → **audit trail recorded**), the **grounded-verification engine**
  (claims when evidence supports the draft; refusal to fabricate without evidence),
  and **per-user JWT RBAC** (viewer JWT → 403, owner JWT → 200 on the admin route).

## What it does NOT prove (honest boundary)

- The **full Docker Compose multi-service stack** (Postgres + Redis + Kafka +
  nginx together) was **not** booted — the Docker daemon was unavailable here.
  The smoke used a standalone local Postgres, with **Kafka degraded to a no-op**
  and Redis not exercised. Compose-topology behavior (event publishing, gateway
  routing) therefore has no executed test in this evidence set.
- **Live external providers** (Gmail/Drive/Calendar/paid LLMs) were not
  exercised — intentionally disabled in code.
- **Grounded LLM verification** (answer generation) is not exercised — no LLM
  provider is configured in the smoke; only the verification *runs surface* is
  asserted. (The intake → approval → resolve → audit lifecycle **is** asserted.)

New behaviors added during the run (e.g. `/memory/query`, `/system/info`,
`/readyz`, rate limiting, idempotency, RBAC middleware) are each covered by
package/httptest cases within the 54 passing backend packages.
