# Archived HAI Verification Artifacts

This directory contains dated implementation reports and their directly
referenced receipts, test logs, source-review notes, and runner scripts from
the September 30–October 1, 2026 HAI work. The Markdown reports are preserved
because existing project documentation cites them; the linked supporting
files are included so those citations remain inspectable in this repository.

These artifacts record the environment, code state, and test scope at the time
they were produced. They are historical evidence, not proof of current
deployment, provider behavior, production readiness, or acceptance on another
machine. Re-run the current release gates before relying on them as present-day
status. Source links were normalized to repository-relative paths; any trailing
`:line` text preserves the original report's line coordinate and may not match
the current file.

## Integration and durability

- [Production durability and integration ledger](production-durability-integration-20261001.md)
- [Production integration report](production-integration-20261001.md)
- [Runtime outcome integration report](runtime-outcome-integration-20261001.md)
- [Workflow installer integration report](workflow-installer-integration-20261001.md)
- [Integrated safety checkpoint](integrated-safety-checkpoint-20261001.md)

## Focused verification

- [Automation public projection](automation-public-projection-20260930.md)
- [CI isolation](ci-isolation-production-20260930.md)
- [Windows installer audit](installer-production-audit-20260930.md)
- [Manual worker with PostgreSQL](manual-worker-postgres-20260930.md)
- [Manual source worker](manual-source-worker-20260930.md)
- [Ambient monitor](ambient-monitor-production-20260930.md)
- [Production secrets doctor](doctor-production-secrets-20260930.md)
- [Structured secret redaction](structured-redaction-20260930.md)
