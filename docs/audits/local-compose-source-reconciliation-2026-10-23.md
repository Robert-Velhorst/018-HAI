# Local Compose source reconciliation

## Source comparison

Read the complete retained local Compose source and its complete difference
against the integration tree. A separate structured YAML comparison rejects
duplicate mapping keys instead of silently keeping the last definition. It
does not interpolate environment variables, read installed environment files
or invoke Docker.

The retained configuration defines 43 services; the current configuration has
44. Of the 42 shared service definitions, none loses a top-level field or an
environment key. All 19 historical networks and six named volumes remain
declared. These structural checks do not imply that changed values, volume
layouts or images are operationally interchangeable.

Keep the current control-plane readiness checks and healthy dependencies,
resource limits, loopback-only published ports, separate migration/runtime-role
jobs, unprivileged backend image and bounded shutdown windows. Keep the current
PostgreSQL migration preflight and volume layout; do not revert to old database
images or remount a live volume as part of source reconciliation. Existing
data, backup/restore and migration acceptance remain separate gates.

The two added services are the backend state-ownership one-shot job and the
optional SearXNG discovery service. The latter remains profiled, publishes no
host port, and separates the internal backend link from search-provider egress.
This review neither starts these services nor independently certifies every
runner, upstream image, external provider or host filesystem permission.

## Explicitly unresolved historical feature

The removed service is `ngrok`, formerly behind the `cloud-tunnel` profile.
Its launcher, entrypoint, configuration, documentation and historical contract
test remain retained as described in the
[ngrok reconciliation record](legacy-ngrok-source-reconciliation-2026-10-23.md).
That work is not integrated or implicitly retired. Do not restore public
exposure, downgrade current safeguards or treat this Compose review as
permission to discard the source.

## A2A shutdown correction

The A2A gateway command uses `exec nginx`, but the service had a Go-specific
comment and an explicit `SIGINT` override. Nginx documents INT as fast shutdown
and QUIT as graceful shutdown. Replace that override with `SIGQUIT` while
retaining the existing 25-second container stop grace period. This allows
draining rather than explicitly requesting immediate termination; the deadline
still bounds shutdown and does not promise every long request completes.
See [Nginx signal documentation](https://nginx.org/en/docs/control.html).

The new source-level regression fails against the previous configuration and
passes after the repair. All 125 CI contract tests pass locally without skips.
This establishes configuration intent, not an observed container restart or
in-flight request acceptance test.

## Preservation boundary

Private receipts identify the original bytes, reviewed integration and remote
main revision without publishing local paths or secret values. All retained
sources remain intact. Publication in a draft PR is not preservation on main,
and a completed source review is not whole-worktree deletion eligibility.
