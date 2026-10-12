# DeepSeek Harness Windows Host Bridge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the containerized HAI backend dispatch one approved DeepSeek Harness task to a locally installed Windows `dsh` executable without granting direct host execution or exposing a public bridge.

**Architecture:** The backend persists an owner-bound job after its existing final-effect proof succeeds. A separately installed Windows bridge polls the backend through the loopback gateway using a dedicated bridge credential, leases exactly one job, runs only `dsh --profile headless <prompt>` in a configured workspace, and posts a bounded redacted result. The public nginx/ngrok path denies bridge endpoints, while normal HAI execution remains disabled by default.

**Tech Stack:** Go, GORM/PostgreSQL migrations, Gin routes, Windows PowerShell launch script, DeepSeek Harness CLI.

**Spec:** Approved in the task conversation on 2026-08-24: private, approval-bound Windows DSH bridge with fixed command shape, workspace allowlist, output limits, replay protection, cancellation, and audit results.

## Global Constraints

- Do not expose the host bridge through ngrok or bind a host listener.
- Keep `DEEPSEEK_HARNESS_ENABLED` and `DEEPSEEK_HARNESS_EXECUTION_ENABLED` false by default.
- Require the existing HAI final-effect proof before creating a job.
- Never send secrets, arbitrary environment variables, or unbounded output to the host bridge.
- Do not alter unrelated, uncommitted pursuit changes.
- Do not start, stop, or rebuild Docker services during implementation.

---

### Task 1: Persist and lease an approved host-runtime job

**Files:**
- Create: `backend/internal/hostruntime/job.go`
- Create: `backend/internal/hostruntime/service.go`
- Create: `backend/internal/hostruntime/service_test.go`
- Create: `backend/migrations/pre/0065_host_runtime_jobs.up.sql` and `.down.sql`

**Interfaces:**
- Produces `Service.Enqueue(ApprovedTask)`, `Service.Lease(owner, runtimeID)`, `Service.Complete(lease, Result)` and `Service.Cancel(owner, taskID)`.
- A job contains a random ID, SHA-256 bridge token digest, owner, runtime ID, task ID, bounded prompt, workspace key, lease expiry, state, and redacted result.

- [ ] **Step 1: Write failing service tests** for rejecting unapproved jobs, allowing a single active lease, rejecting an expired or mismatched lease, and redacting a completed result.
- [ ] **Step 2: Run the hostruntime package tests** and confirm the missing package causes the expected failure.
- [ ] **Step 3: Add the migration, model, and service** with transactional compare-and-set state transitions.
- [ ] **Step 4: Re-run hostruntime tests** and confirm they pass.

### Task 2: Add bridge-only backend routes and authorization

**Files:**
- Create: `backend/internal/hostruntime/handler.go`
- Modify: `backend/internal/router/routes.go`
- Modify: `docker-compose.local.yml`
- Modify: `.env.example`
- Test: `backend/internal/hostruntime/handler_test.go`

**Interfaces:**
- `POST /api/v1/host-runtime/leases` receives a bridge token and returns one leased job or `204`.
- `POST /api/v1/host-runtime/leases/:id/complete` accepts only the corresponding lease token and bounded result.
- Route access requires `HAI_HOST_RUNTIME_BRIDGE_TOKEN`, a 32+ character secret; disabled/missing configuration fails closed.

- [ ] **Step 1: Write failing handler tests** for missing credentials, a public-role rejection, no-job `204`, successful lease, and result replay rejection.
- [ ] **Step 2: Run hostruntime handler tests** and confirm they fail before the routes exist.
- [ ] **Step 3: Implement token validation and routes**, wiring them only into the loopback gateway profile.
- [ ] **Step 4: Re-run handler tests** and confirm they pass.

### Task 3: Replace direct container DSH execution with queued host dispatch

**Files:**
- Modify: `backend/internal/agentruntime/deepseek_harness.go`
- Modify: `backend/internal/agentruntime/runtime.go`
- Modify: `backend/cmd/main.go`
- Test: `backend/internal/agentruntime/runtime_test.go`

**Interfaces:**
- The DeepSeek adapter receives a `hostruntime.Dispatcher`.
- After final-effect authorization, `ExecuteTask` enqueues an immutable task and returns `queued`, never runs `exec.Command` inside the container.
- Cancellation propagates to a pending/leased host job and is recorded in HAI audit output.

- [ ] **Step 1: Write failing adapter tests** proving a verified task queues one job and never executes a local process, while an unverified task queues nothing.
- [ ] **Step 2: Run the focused runtime tests** and confirm the new behavior fails.
- [ ] **Step 3: Add the dispatch dependency and bounded queued result** without weakening proof verification or emergency-stop checks.
- [ ] **Step 4: Re-run the focused runtime tests** and confirm they pass.

### Task 4: Add a Windows bridge executable and guarded launcher

**Files:**
- Create: `backend/cmd/hai-dsh-bridge/main.go`
- Create: `backend/cmd/hai-dsh-bridge/main_test.go`
- Create: `scripts/start-deepseek-harness-bridge.ps1`
- Modify: `README.md`
- Modify: `.env.example`

**Interfaces:**
- The bridge reads the gateway URL and token from process environment, long-polls for jobs, and runs exactly `dsh --profile headless <prompt>`.
- It validates the configured local workspace before every execution and returns redacted, size-limited stdout/stderr.
- The PowerShell launcher rejects non-loopback HAI URLs and refuses to start when DSH is absent or configuration is incomplete.

- [ ] **Step 1: Write failing bridge tests** for rejecting non-loopback URLs, option-like prompts, workspace escapes, and oversized results.
- [ ] **Step 2: Run bridge tests** and confirm they fail before implementation.
- [ ] **Step 3: Implement the polling bridge and launcher** with timeout/cancellation handling.
- [ ] **Step 4: Re-run bridge tests** and confirm they pass.

### Task 5: Prove private exposure and document operational limits

**Files:**
- Modify: `nginx/nginx.conf` or the active gateway configuration
- Modify: `scripts/test_ci_contract.py`
- Modify: `README.md`
- Test: `backend/internal/router/routes_smoke_test.go`

**Interfaces:**
- Host-runtime endpoints are denied from non-loopback/ngrok ingress.
- Documentation distinguishes installed CLI, configured bridge, and live approved execution.

- [ ] **Step 1: Write a failing contract test** that requires an explicit public-path denial and default-disabled bridge configuration.
- [ ] **Step 2: Run the contract test** and confirm it fails.
- [ ] **Step 3: Apply the nginx/config/documentation changes.**
- [ ] **Step 4: Run focused Go tests, CI contract tests, and Compose validation.**

## Verification

- `go test ./internal/hostruntime ./internal/agentruntime ./internal/router`
- `go test ./cmd/hai-dsh-bridge`
- `python scripts/test_ci_contract.py`
- `docker compose --env-file .env.example -f docker-compose.local.yml config -q`
- `git diff --check`

## Out Of Scope

- Enabling a real approved DSH task against Robert's workspace.
- Making the host bridge public, using it through ngrok, or granting it arbitrary shell execution.
- External provider, Gmail, Drive, Calendar, or Trello writes.
