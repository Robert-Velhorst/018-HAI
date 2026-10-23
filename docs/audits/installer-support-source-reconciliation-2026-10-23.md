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

## Initial follow-up findings

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

## DACL and uninstall follow-up

The two implementation findings above are now repaired, with bounded evidence:

- Replace the sequential ACL reset/inheritance/grant commands with one native
  .NET file-security persistence call containing only the desired access section.
  Owner, group and audit sections are not imported. The full requested DACL is
  checked after persistence; failure stops initialization before new secret bytes
  are written. There is no reset or retry through a broader inherited policy.
- A preliminary `Set-Acl` approach failed on an already-protected synthetic file
  with a security-privilege error. It was not accepted. The final implementation
  uses the standard FileSystemAclExtensions API on current .NET and FileInfo's
  equivalent method on .NET Framework. No native interop or new package is added.
- Native Windows tests pass three cases on both PowerShell 7 and 5.1: inherited
  and already-protected files plus an injected pre-write failure. They check one
  persistence attempt, no owner/group/audit sections in the submitted descriptor,
  unchanged actual owner/group, exact resulting DACL and unchanged synthetic
  content. Only exact newly created fixture files are removed by bounded,
  non-recursive cleanup. No installed configuration or credentials are read.
- Uninstall no longer supplies a 30-second timeout overriding service-specific
  `stop_grace_period`. It retains the project/env/config scope, optional profiles,
  ownership checks and post-stop inspection. Six isolated function cases pass on
  both PowerShell engines: success, ownership/config/stop/query refusal and a
  still-running service. Docker and filesystem collaborators are mocked; no real
  stop, uninstall, volume deletion or forced process termination is attempted.

The ACL preflight originally rejected the old sequential implementation. The
uninstall regression originally failed its success and later-inspection cases
because of the timeout override. After repair, all 130 local CI contracts and
both engines' static installer contracts pass without skips. Hosted Windows CI
now runs both new suites on both engines; wiring is not a claim that the hosted
run has passed.

These cases do not establish concurrent path-substitution resistance, a configured
SACL's behavior under every Windows policy, a real long-running database drain,
whole installer readiness, or acceptance of existing installations. Those broader
security and release gates remain open. Full lifecycle-source reconciliation and
main preservation are still required before local source deletion.

API references: [specific security descriptor sections](https://learn.microsoft.com/en-us/dotnet/api/system.security.accesscontrol.objectsecurity.setsecuritydescriptorsddlform),
[native .NET file security persistence](https://learn.microsoft.com/en-us/dotnet/api/system.io.filesystemaclextensions.setaccesscontrol),
and [Compose stop grace](https://docs.docker.com/reference/compose-file/services/#stop_grace_period).
