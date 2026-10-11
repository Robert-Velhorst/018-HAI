# Ambient Monitor Windows Acceptance - 2026-08-14

## Scope

This record retains the signed Windows gateway and Governance Control evidence
for the advisory outcome-monitor due-run and crash-recovery lifecycle at commit
`007ea2f` on branch `codex/ambient-composition-control`.

The exercise used the rebuilt Angular frontend, the normal nginx authentication
subrequest, the existing IDP-issued owner session, a temporary backend, and a
fresh disposable PostgreSQL database. The temporary gateway was bound only to
`127.0.0.1:18080`. The retained HAI database was not used for outcome or monitor
records.

All unrelated background schedulers were explicitly disabled in the temporary
backend. This prevented source, workflow, ambient, outcome-monitor scheduler,
and LLM-maintenance activity from contaminating the signed manual lifecycle.
The due pass itself was started only through the Governance Control button.

## Signed UI lifecycle

1. Opened `/governance-control` through the temporary loopback gateway with the
   existing signed owner session.
2. Created outcome `ambient-monitor-acceptance` in workspace
   `acceptance-workspace`. The UI reported immutable revision 1.
3. Registered one enabled `workflow_verified_completion_count` target with a
   60-second cadence and a due first-run time.
4. Selected **Run due now**. The UI reported `1 observation recorded; 0 failed`
   and stated that no work was executed or delivered.
5. Inspected the completed run, `0 tasks` source-backed observation, pinned
   composition snapshot, and successful immutable handoff attempt.
6. Selected **Pause monitor** and observed `Paused` plus **Enable monitor**.
7. Selected **Enable monitor** and observed `Enabled` plus **Pause monitor**.
8. Simulated a crashed worker only in the disposable database by installing one
   valid, expired collection lease while advancing the guarded row revision.
9. Selected **Recover expired work**. The UI reported `1 collection lease and 0
   advisory handoff leases returned to their durable queues`.

No browser console error, page alert, authentication redirect, or failed API
state was observed during the clean lifecycle.

## Database reconciliation

The final exact counts in the disposable database were:

| Table | Rows |
| --- | ---: |
| `outcome_evaluation_outcome_revisions` | 1 |
| `outcome_monitor_targets` | 1 |
| `outcome_monitor_commands` | 3 |
| `outcome_observation_records` | 1 |
| `outcome_monitor_runs` | 1 |
| `outcome_monitor_composition_deliveries` | 1 |
| `outcome_monitor_composition_attempts` | 1 |
| `workflow_items` | 0 |
| `workflow_events` | 0 |
| `automation_launch_events` | 0 |
| `execution_authorization_receipts` | 0 |
| `execution_authorization_consumptions` | 0 |
| `execution_authorization_final_effect_exercises` | 0 |

The final target was enabled, had `last_result=succeeded`, and had no lease ID,
owner, or expiry. The delivery and its attempt were `succeeded`; every
`can_execute`, `delivery_authorized`, and `execution_authorized` flag was false.
The three command receipts were the immutable `create_target`, pause
`set_enabled`, and resume `set_enabled` records.

Outcome creation also produced its documented owner-scoped life-ontology
projection. Migrations installed the five default ambient-need rows. These are
not monitor execution effects. The clean lifecycle produced no provider,
workflow, automation, execution-authorization, Calendar, message, mandate, or
learning effect.

## Boundary

This is retained acceptance evidence for this Windows host and commit. It does
not replace:

- the mandatory disposable-PostgreSQL integrated test for all three collectors,
  transient composition retry, replay, lease fencing, and two-owner isolation;
- a signed exercise with two distinct real accounts;
- live external-account correctness;
- acceptance on another Windows release target.

The temporary gateway, backend, and database were removed after reconciliation.
