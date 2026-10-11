# User Guide

018-HAI is your local-first Personal AI Operating System. This guide covers the
core tasks from the dashboard.

## First run

1. Start the stack (see `docs/operator-runbook.md`) and open the dashboard.
2. Sign in. If the backend key is configured, the client sends it automatically.
3. Check the HAI OS overview — it shows readiness and emergency-stop state.

## Memory

Your assistant remembers context as "memories" (preferences, decisions,
contacts, notes).

- **Add:** create a memory with content, a kind, optional tags and confidence.
  Duplicates are merged automatically.
- **Find:** use the memory search — filter by kind/tag, sort by recency,
  confidence, or relevance, and page through results (`/memory/query`).
- **Templates:** start from a preset (preference/decision/contact) that fills
  sensible defaults you can override.
- **Export / delete:** export your memories as a portable file, or request
  deletion; the system reports exactly which records were affected.

## Workflows & approvals

Work items move through defined states and pause at approval gates. You review
and approve or reject; nothing with real side effects runs without your approval.

## Trust & transparency

- Every answer is grounded — claims link to evidence.
- Quality/confidence is shown so you can judge reliability at a glance.
- The system never pretends an external step happened; it tells you what remains
  manual.

## Safety controls

- **Emergency stop** blocks new execution immediately while keeping planning visible. It cancels an in-flight run only when that runtime supports a retained cancellation handle; otherwise the run remains bounded by its configured timeout and is flagged for review.
- **Readiness** (`/readyz`) tells you if the system is correctly configured.

## Getting help

See `docs/troubleshooting.md` for error codes and first diagnostics.
