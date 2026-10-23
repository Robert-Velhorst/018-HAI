# OpenClaw maintenance interface reconciliation

## Review

Read the complete retained maintenance component and specification from both
audit worktrees, confirmed each pair is byte-identical, and reviewed the full
current differences. Preserve owner-only controls, stale-check installation
denial, recovery acknowledgement without re-enabling automatic updates, and
the distinction between request acceptance, worker contact, job start and
verified completion.

Keep the newer serialized, bounded status polling for jobs submitted by this
component, including read-only-check-to-apply progression, error reporting,
retry exhaustion and destruction cleanup. Do not restore the older interface
that stops tracking after its first post-acceptance status refresh.

## Contract fix

The maintenance overview placed a positive containment-readiness message in
`installBlockedReason` while reporting `installStatus: capability_ready`.
The existing interface correctly treats a nonempty blocking reason as a
disabled installation action, so this positive message created a false blocker.

Capability-ready Companion projections now leave the blocking reason empty.
Unavailable containment retains its actual blocker, Gateway/core remains
blocked without verified Linux-side containment, and missing publisher proof
explicitly sets the structured status to `blocked`. Installation admission,
owner policy, verified receipts, emergency stop and runtime safety gates are
unchanged; capability readiness alone does not authorize an installation.

## Verification and remaining acceptance

- The new ready-Companion reason test failed before the fix and passed after it.
- The local maintenance package passed uncached; PostgreSQL-dependent tests
  were skipped because no dedicated database was configured locally.
- All 27 maintenance component tests passed in ChromeHeadless, including a
  rendered installation-button click, exact request payload and acceptance-only
  messaging for a ready Companion response fixture.
- All 112 CI workflow contracts passed and the workflow YAML parsed.
- The existing PostgreSQL CI job now must execute both new overview tests and
  produce their individual PASS markers. Skipped tests cannot satisfy that gate.

The real PostgreSQL overview results remain pending CI. These results do not
prove a live Windows installer or OpenClaw Gateway update. No runtime/provider
was enabled or updated, and no original worktree or archive was deleted.
