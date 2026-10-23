# Runtime lab and brain catalog reconciliation

## Reviewed scope

Reviewed six selected source versions: the runtime-lab service and service
tests from both audit worktrees, plus the model-audit brain catalog and
implementation-boundary registry. The two older runtime-lab variants match
after line-ending normalization.

Retain the current safe-worker policy check, targeted execution claim,
claim-release handling, redacted attempts and retained execution receipts.
Do not restore the older interpretation of a verified flag as sufficient
completion evidence or discard interrupted/uncertain outcomes.

Retain current OpenClaw discovery persistence-failure reporting and invocation
gating. In-memory protocol availability is not durable evidence of readiness.

Retain the implemented bounded conversation-history MCP entry and its explicit
implementation boundary. Do not remove it because the older catalog lacks it.
Keep the narrower loopback-only A2A endpoint boundary and existing newer
Promptfoo provenance rather than restoring stale descriptions. This source
comparison does not reverify upstream availability or install any upstream tool.

## Additional recovered-status defect

A completed operation marked verification-passed could still recover as a
succeeded runtime attempt when its world-model state was empty, whitespace,
an empty object or an unrelated object without `safeWorkerExecution` evidence.
The existing malformed-evidence checks did not cover these valid-but-missing
payloads.

Recovery now marks an otherwise successful attempt without a retained receipt
as inconclusive and reconciliation-required. The terminal operation record
remains recorded; recovery does not rewrite it or rerun work. Existing receipt
validation and redaction still apply. A complete, non-uncertain verified receipt
continues to recover successfully.

## Verification and preservation

All four missing-evidence cases failed before the repair. They pass afterward,
and the complete-receipt positive control continues to pass. These are local
status-projection tests, not a new execution or verification of artifact bytes
on disk, live runtime acceptance or restart testing against PostgreSQL.
The full runtime-lab, brain-catalog, background and execution-broker Go packages
pass locally.

The six original source versions retain private exact-content review receipts.
No original worktree, source file, archive, database or transcript is deleted.
