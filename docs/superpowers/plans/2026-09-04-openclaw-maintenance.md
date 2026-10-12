# OpenClaw Maintenance Implementation Plan

**Goal:** Keep the installed OpenClaw harness observable and current through HAI, preserving HAI authority.

**Architecture:** A dedicated Windows worker pulls target-specific maintenance jobs from HAI. PostgreSQL stores target policy, due times, immutable job receipts and leases. Runtime Lab renders those records. The existing DSH worker and task authority remain separate.

**Tech stack:** Go, PostgreSQL/Gorm, Windows PowerShell, WSL, Angular/NG-Zorro.

**Spec:** ../specs/2026-09-04-openclaw-harness-governance-design.md

## Verified implementation refinements

- Initial inspection found Companion 2026.7.1-4 and Gateway 2026.6.10. The supervised update subsequently brought Gateway to 2026.9.1. They must not share a version comparator with prerelease semantics: the numeric fourth component is a patch revision.
- Companion uses signed Windows release assets. Gateway is an npm installation: Authenticode does not apply to its Linux package. Verify registry signatures/integrity separately; unsupported verification must be reported, never marked verified.
- A worker disconnect after starting installation must become needs_review; an apply lease must never be automatically replayed.
- Checks run every 24 hours by default and manual checks do not create duplicate pending jobs.
- Use existing owner authentication for dashboard routes and a separate worker bearer token for the pull endpoint.
- Do not change runtime flags or enable task tools during maintenance.
- Do not commit or deploy unrelated worktree changes.

## Implementation

- [x] Add maintenance tables and service with atomic queue/lease/receipt state, exact-version authorization, read-only defaults, and emergency-stop checks.
- [x] Wire authenticated owner and worker routes and durable scheduling.
- [x] Implement Windows worker: fixed Companion and Gateway profiles, bounded command output, official release checks, target-specific verification and exact-version apply.
- [x] Add Runtime Lab version/status/policy/check/update controls with real API calls and accessible error states.
- [x] Test duplicate/replay, expired/ambiguous apply, policy revocation, unknown evidence, and frontend compilation. Build the Windows executable and run read-only checks against the installation.
- [x] Record remaining live installation or deployment gates accurately.

## Remaining deployment and acceptance gates

- [x] Bundle the worker in Windows release builds and provide an explicit, validated current-user launcher.
- [x] Verify the pull client against real handlers/PostgreSQL and run an opt-in read-only check of the actual Windows/WSL Gateway through that chain (2026-09-05).
- [ ] Deploy the reviewed maintenance changes to the live HAI worktree and apply migration 0072.
- [ ] Configure separate worker credentials and register the Windows worker for startup.
- [ ] Verify the live HAI queue-to-worker-to-receipt flow, including a controlled installation and recovery.
- [ ] Complete rendered browser and accessibility acceptance for the maintenance controls.

Implementation checks are not deployment acceptance. The larger OpenClaw/Hermes
reuse programme remains separate and incomplete. See `docs/openclaw-maintenance.md`
for the exact local update, verification evidence and limitations.
