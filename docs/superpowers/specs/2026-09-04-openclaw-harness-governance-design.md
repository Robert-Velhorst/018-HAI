# OpenClaw Harness Governance Design

## Purpose

Implementation status and deployment limits are tracked in
[the maintenance runbook](../../openclaw-maintenance.md). This design is not
evidence of an installed service or a completed unattended-update acceptance run.

Use OpenClaw as a vendor-maintained, local agent harness for narrowly
delegated HAI work. HAI remains the personal operations system: it owns
identity, authority, project and workflow state, memory, source evidence,
planning, scheduling, approval, verification, and audit. OpenClaw never
becomes a second source of truth for any of those concerns.

This design adds a governed update and compatibility boundary around the
existing OpenClaw Gateway integration. It does not create competing sources of
truth for HAI workflows, memory or authority.

## Reuse-first architecture

Robert clarified that OpenClaw and Hermes are engineering foundations and
reference implementations, not merely extra buttons or isolated connectors.
HAI should borrow proven infrastructure wherever it fits its purpose instead of
building another agent harness from scratch. HAI is a distinct personal
operations product; technical reuse does not imply replacing that product with
OpenClaw's UI or adopting its decisions as authoritative.

For each capability, use this order:

1. Reuse a supported upstream API, protocol or library already providing it.
2. Adapt an existing HAI integration with the smallest necessary bridge.
3. Study and selectively port a proven upstream implementation only when direct
   reuse is unsuitable, preserving its license and provenance.
4. Build new HAI infrastructure only for a documented unmet requirement.

Candidates include session lifecycle, tool invocation, worker execution,
cancellation, context handling, model access and recovery mechanisms. Verify
the actual available upstream implementation and compatibility before assigning
it to either runtime; this list is a selection policy, not a claim of completed
feature parity. Do not run duplicate OpenClaw and Hermes services for the same
job merely to include both names.

HAI retains the user experience, personal goals, cross-project context, policy,
budgets, approvals, evidence and final completion decision. Reused mechanisms
must report into those records. Track upstream version, license, HAI owner,
capability boundary and contract tests for each adopted component. Maintenance
is one supporting part of this architecture, not the whole integration.

## Scope

The system manages two independently versioned local dependencies:

1. **OpenClaw Companion**: the signed Windows desktop/hub application.
2. **OpenClaw Gateway/core**: the loopback-only WSL runtime that exposes the
   Gateway health endpoint and, only when explicitly configured later, bounded
   delegated sessions.

HAI records their observed current version, available version, check time,
update outcome, signature-validation outcome, restart outcome, and post-update
Gateway health. It runs an inexpensive check no more often than every 24 hours
per component, with an explicit manual refresh for the owner.

## Boundaries of this maintenance increment

- HAI does not package, fork, mirror, or reimplement OpenClaw.
- HAI does not download an executable from a generic URL or run unsigned code.
- Installing an update does not automatically grant OpenClaw channels, browser
  control, shell access, filesystem access, credentials, memory, cron, skills,
  plugins, or models any HAI authority. Integration of these capabilities remains
  part of the larger programme, with explicit ownership and verification; it is
  not excluded from HAI by this maintenance boundary.
- HAI does not auto-enable Gateway delegation, authenticated discovery, or any
  task capability after an update.
- An update result is not proof of a delegated task effect.

## Authority Boundary

```text
Robert's standing update mandate
  -> HAI update policy and durable maintenance record
  -> local Windows OpenClaw maintenance worker
  -> official OpenClaw Companion/core updater
  -> signed package validation and restart
  -> version + loopback health evidence
  -> HAI maintenance receipt

HAI workflow approval
  -> bounded delegation envelope
  -> OpenClaw session
  -> terminal Gateway receipt
  -> HAI reconciliation and independent verification
```

The first path changes only the locally installed harness. The second path is
separate and remains disabled until its current distinct delegation policy,
tokens, and receipt store are configured. A successful update may not expand
the permissions of a later task.

## Components

### HAI Backend: OpenClaw Maintenance Service

Create a focused `openclawmaintenance` package beside
`openclawreconcile`. Its only responsibilities are scheduling, durable update
state, policy checks, and exposing a redacted owner-visible status. It does not
launch installers or parse unbounded upstream output.

The service models each target as `companion` or `gateway_core`. A target state
contains:

- target ID and installation scope;
- installed and available version strings;
- last checked, attempted, installed, and verified timestamps;
- state (`unknown`, `current`, `update_available`, `installing`, `verified`,
  `blocked`, `failed`, `needs_review`);
- a bounded, redacted reason;
- a vendor metadata digest and installer signature digest; and
- a unique immutable maintenance receipt reference.

The durable scheduler follows the existing recurring-job pattern. The default
interval is fixed at 24 hours, executes a small bounded batch,
and honours HAI's emergency stop for update application. It may still record a
read-only stale status when the stop is active.

### Windows OpenClaw Maintenance Worker

The installer runs on Windows while HAI runs in Docker. The repository already
has a proven, loopback-only **pull worker** pattern in `hai-dsh-bridge`: a
Windows process polls authenticated HAI lease routes, reconfirms the lease while
working, and posts one bounded completion. It has no listener. The OpenClaw
worker must use that pattern rather than adding a generic Windows HTTP bridge.

The existing host-runtime handler is intentionally hard-bound to
`deepseek-harness`, so it is not reused as a generic executor. Extend the
lease contract with a distinct `openclaw-maintenance` runtime type and a
distinct worker identity/token. The worker accepts only these target-bound job
kinds:

- `status`: reads vendor-maintained local update state and installed versions;
- `check`: invokes only the official updater's check mechanism and emits
  normalized version metadata; and
- `apply`: applies an already-authorized newer version, then emits a bounded
  receipt containing version, publisher validation, installer result category,
  and Gateway health result.

The worker must never accept a URL, command line, arbitrary file path, shell
script, package hash supplied by the dashboard, or arbitrary installer command.
It invokes only the official Companion/core updater selected by its fixed
target profile. Companion requires a valid Authenticode signature and a configured
certificate pin. Gateway/core is an npm package, not a Windows executable; it
requires a valid npm registry signature over the exact version/integrity.
An unsupported publisher/signature is `needs_review`, not an automatic install.

### Update Policy

The owner-managed policy stores a standing mandate independently for Companion
and Gateway/core. The default is observe-only. Robert can set either target to
`auto_install_verified` after seeing the exact scope in the
dashboard. Emergency stop blocks admission, while ambiguous failures revoke
automatic updates. Neither setting grants task,
channel, model-provider, browser, shell, filesystem, or credential authority.
Maintenance never depends on, modifies, or enables
`OPENCLAW_AGENT_CLI_ENABLED`; direct CLI task execution remains blocked until HAI
can verify the effective OpenClaw sandbox and tool policy for the exact run. A
HAI mirror flag or standalone sandbox report is not a run-bound policy attestation.
The separate Gateway-delegation route retains its own credentials, receipts, and
authorization checks.

Before applying an update, HAI requires all of the following:

1. A live policy mandate for that exact target.
2. A strict newer-version comparison; equal or lower versions are rejected.
3. Vendor metadata tied to the target; only bounded official GitHub asset
   redirects are allowed, never arbitrary endpoints.
4. A valid pinned Authenticode signature for Companion or npm registry signature
   for the exact Gateway/core version.
5. A fresh, single-use maintenance authorization and idempotency key.
6. No active delegated OpenClaw session for the target, or an explicit
   maintenance window that leaves it intact.

After installation, HAI requires the reported version to equal the approved
version and probes the fixed loopback `/health` contract. The response must be
`{"ok":true,"status":"live"}` through the existing allowlisted health
adapter. Failure becomes a durable `failed` or `needs_review` receipt; HAI does
not claim success, retry an unknown installer effect, or automatically
downgrade.

## Data Flow and Reconciliation

1. The 24-hour scheduler creates a target-bound maintenance lease; the Windows
   worker polls and accepts only its own `openclaw-maintenance` leases.
2. HAI persists only normalized versions, timestamps, target IDs, result code,
   and evidence digests.
3. If an update is available, HAI shows a decision or applies it only under the
   target's standing mandate.
4. The worker posts one terminal receipt before HAI updates its own state.
5. HAI validates the worker's receipt submission. Ambiguous, stale, or
   mismatched receipts become review items and are never retried as installs.
6. On terminal success HAI independently probes the loopback Gateway, then
   stores a verified maintenance receipt.

No OpenClaw transcript, prompt, tool call, account, file name, channel
identifier, or token is ingested during update handling.

## Error Handling

- Network, metadata, signature, or bridge errors are bounded and redacted.
- A target is checked at most once per configured interval unless the owner
  explicitly requests a refresh.
- Installer timeouts produce `needs_review`; a retry needs a new authorization.
- A version mismatch, untrusted redirect, unsigned installer, unknown publisher,
  or certificate mismatch blocks application. A failure after an installer
  starts cannot be assumed to preserve the prior runtime.
- Gateway health failure after an otherwise successful install keeps HAI task
  delegation disabled and surfaces the recovery evidence.
- Existing OpenClaw sessions are reconciled by `openclawreconcile`; maintenance
  does not cancel, restart, or reinterpret them.

## Dashboard Contract

Runtime Lab and System Status show one compact “OpenClaw harness” card with
separate Companion and Gateway/core rows. Basic view shows installed version,
availability, verified health, and the next required action. Advanced view
shows the policy, last check, receipt ID, evidence digest, failure category,
and a manual refresh/apply control. It never displays installer locations,
tokens, raw vendor responses, raw logs, or Windows profile paths.

## Verification

Automated coverage must prove:

- interval bounds and durable idempotency;
- Companion/core state is independent;
- version comparisons reject equal and lower versions;
- only a fresh target-bound authorization reaches `apply`;
- untrusted publishers, signature failures, redirects, raw URLs, and malformed
  receipts fail closed;
- pending/running/ambiguous installs are not retried;
- an install cannot enable OpenClaw task, channel, host-tool, skill, plugin,
  browser, or delegation flags;
- post-install Gateway health must satisfy the existing strict health parser;
- dashboard responses omit secrets and raw bridge data; and
- a full integration fixture covers check, authorized apply, receipt, health,
  and maintenance audit persistence.

## Rollout

1. Ship observe-only status and daily checks for both targets.
2. Validate the bridge against the installed Companion and WSL Gateway using a
   signed test release check, without applying an update.
3. Enable the standing mandate per target only after the owner reviews the
   dashboard record and the signed-install path passes a controlled test.
4. Keep Gateway delegation, authenticated discovery, channels, host tools,
   skills, plugins, and model operations disabled throughout rollout.

## Acceptance Criteria

HAI has one canonical control plane and OpenClaw remains an optional harness.
Every installed OpenClaw component is independently observed at least daily;
only a newer official signed package can be installed; every action is
idempotent and auditable; an update cannot increase runtime authority; and HAI
reports verified, failed, blocked, and unknown states without overstating
readiness.

## Remaining acceptance boundaries

The official npm updater also synchronizes OpenClaw plugins and performs
Gateway restart/doctor operations. HAI retains its own authority gates but does
not sandbox the vendor updater or prove that upstream configuration and plugin
permissions are unchanged. Health/version verification alone is not full
release-compatibility proof. Independently started Companion sessions are not
covered by HAI's task-admission lock. Startup registration, controlled live
installation, and compatibility validation remain deployment gates; the source
implementation must not be advertised as universally safe unattended updating.
