# Codex Goal Run — Artifact Index

This directory contains the deliverables of the Giant Codex Goal Prompt run for 018-HAI.

**Core rule honored throughout:** *no mockups, no fake integrations, no false completion.* Statuses record what is actually present and verifiable; unverified work is marked as such.

## Current continuation

The run is continuing beyond the historical phase roll-up below. The current
implementation adds a durable Plan Graph and authenticated Plan and Coordination
surface at `/plans`: immutable PostgreSQL revisions, digest-chain and optimistic
concurrency checks, owner-scoped actors, acceptance and repair history, and
progressive Basic/Advanced inspection. Tasks, workflows, pursuits, portfolio
allocations, proposals, dispatch, effect authorization, and first receipt
consumption now bind and revalidate an exact latest accepted plan revision.
Plan acceptance is coordination only and never grants approval or execution
authority. Historical recovery preserves exact provenance without authorizing
new work. A durable task `Plan` operation with no accepted reference now
projects one immutable advisory draft graph bound to the exact task operation;
side-effect-free task previews and task runs never create that state, and an
accepted reference suppresses a competing draft. Governed pursuit-candidate
acceptance and ordinary workflow intake now also project one immutable
revision-1 advisory graph from the persisted workflow checklist. The workflow
stores the exact draft plan, revision, digest, and root-node binding; projection
failure blocks the workflow and an unchanged source replay can recover it
without creating a second workflow. Acceptance and execution authority remain
separate. Automatic projection from pursuit planning, connected-source records,
and other intake paths and completion of every 55-family capability remain
active work. The workflow reminder path now also
has a separate owner-approved, one-time internal delivery authorization, an
append-only authorization and attempt ledger, bounded three-attempt processing,
terminal suppression/dead-letter receipts, and an idempotent handoff into the
local proactivity inbox. One approved decision can authorize only one delivery,
and Workflow Engine exposes the exact authorization and receipt state. It can
create only an internal HAI signal: email,
Calendar writes, webhooks, desktop push, provider calls, and external follow-up
effects remain unauthorized and unimplemented. The older roll-up must not be
read as current whole-product completion. Agent-team coordination now also has
an append-only acknowledgment stream and a side-effect-free attention
projection. Required replies can be waiting, deferred, acknowledged, rejected,
overdue, or expired; exact retries are idempotent, terminal responses cannot be
rewritten, and owner isolation is enforced in PostgreSQL. These records are
coordination evidence only and never approval or execution authority. The next
active implementation phase is a source-verified OpenClaw, Hermes, and Odysseus
feature-parity inventory followed by bounded HAI capability contracts; no
external runtime is treated as integrated merely because a health URL exists.

| File | Purpose | Phases |
| --- | --- | --- |
| [`01-repo-audit.md`](01-repo-audit.md) | Repository integrity + file/dependency audit with executed build evidence | 000–001 |
| [`completion-matrix.md`](completion-matrix.md) | All 112 phases mapped to honest status + evidence | 095 |
| [`worklog.md`](worklog.md) | Auditable, resumable checkpoints + resume instructions | 087–088 |
| [`final-verification-report.md`](final-verification-report.md) | Evidence-based verification & sign-off | 093–097 |
| [`trello-hai-card-source.md`](trello-hai-card-source.md) | Provenance and implementation mapping for Robert's primary `018 - HAI` Trello card and its two attached specifications | Cross-cutting |

**Run mode:** began as a broad audit pass, then implemented across all phases — real, tested code committed phase by phase, without fabricating completion.
**Historical July roll-up (not current production acceptance):** **110 Implemented · 1 Partial (032, full Docker Compose boot — needs a Docker host) · 0 Missing · 0 Blocked · 1 N/A (090, a process rule).** These recorded classifications include structural-only evidence and do not establish current live-provider, autonomous operator or signed Windows readiness.
**Base:** `main` @ `0f7f12c`; delivered via PR #13 (merged).

Start with the [repository README](../../README.md) and dated integration ledgers
for the current state. Use this completion matrix and final verification report
as historical scope and evidence records, not as release sign-off.
