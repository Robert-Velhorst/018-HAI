# Windows installer support source reconciliation

Read the complete retained installer support source and its complete current
delta. PowerShell AST comparison retains all 49 original function names:
37 function definitions are identical after newline normalization and twelve
are adapted. The integration also has eleven added helpers. This comparison
does not certify every helper's live behavior or authorize an installer run.

Keep the current missing-environment/data preservation checks, strict local-port
parsing, explicit Compose project/configuration ownership checks, staged-worker
payload validation and shared promotion deadline. Preserve the DSH production
execution block, process identity checks and no-automatic-startup-cleanup policy.
Do not restore the old broader A2A URL acceptance, weak port fallback or old
environment-file recreation advice. Current local-only A2A does not revive the
separately retained historical public-tunnel feature.

## Reproduced A2A wiring defect

The installer read the resolved A2A gateway configuration but selected target
port 80. The actual Compose mapping and Nginx listener use 8080. The existing
runtime lifecycle fixture repeated target 80, concealing the disagreement.

An independent no-side-effect contract extracts and invokes the actual
`Get-HaiA2AComposeSettings` and URL-validation functions without running script
entry points. Its expected target comes from checked-in Compose and is compared
with the Nginx listener. Docker is replaced with an argument-checked synthetic
configuration response; no Docker executable, network request, installed env file
or process mutation is involved.

Before repair, canonical IPv4 and IPv6 mappings were rejected and the stale port
was accepted. Select target 8080 and align the older lifecycle fixture. Twelve
cases now pass independently on PowerShell 7 and Windows PowerShell 5.1: the two
valid mappings plus wrong target, wildcard/remote binding, UDP, zero/malformed
published port, duplicate mapping, disabled bridge, remote bridge URL and absent
gateway. The existing CI contract suite invokes this script; all 128 local
contract tests pass without skips. This is runtime-function/configuration proof,
not a real Compose startup, Agent Card fetch or installed A2A acceptance run.

Only the port fixture line was changed in the larger lifecycle test. That entire
retained test source still needs its own complete reconciliation; this pass does
not count it as reviewed or claim its full suite passed.

## Remaining review and acceptance boundaries

- The existing-file ACL helper uses successive `icacls` calls, including a
  reset before restriction. Atomicity and intermediate access to an existing
  secret-bearing file require a dedicated security review and controlled Windows
  regression; this source comparison does not prove that path safe.
- Uninstall explicitly requests a 30-second Compose stop timeout, shorter than
  some services' declared grace periods. Aligning and exercising shutdown bounds
  remains follow-up work; no uninstall or process stop was attempted here.
- Readiness probes cannot establish an authenticated operator session or replace
  target-machine, backup/restore, signed release and provider acceptance.

Original installer sources, lifecycle sources and archives remain intact.
Publication in a draft PR is not preservation on main or deletion eligibility.
