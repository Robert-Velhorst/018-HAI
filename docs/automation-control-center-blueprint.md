# Automation Control Center Blueprint

## Purpose

The Automation Control Center is the operational layer for the Automation Hub. Its purpose is to make every automation visible, measurable, diagnosable, and maintainable before failures become invisible operational debt.

The system should not only show where an automation is located. It should record what the automation is, how it is reached, what it depends on, how its health is proven, when it last worked, when it failed, why it failed, and what the recommended next action is.

## Current foundation

The repository now contains the first monitoring foundation:

- automation metadata for launch target, route, runtime, health check, status, latency, and failure history
- service-level health result structures
- service-level health summary structures
- service-level diagnostics structures
- persistent operational data models for health events, dependencies, route checks, alerts, incidents, and service-level objectives

## Target status model

Supported statuses:

- healthy
- warning
- degraded
- broken
- unknown
- disabled
- maintenance

Recommended interpretation:

- healthy means the automation recently passed its configured checks
- warning means a first failure or minor problem was detected
- degraded means the automation may still be reachable but is unreliable
- broken means repeated failure or a critical dependency failure
- unknown means monitoring is not yet configured or disabled
- disabled means the automation is intentionally not monitored
- maintenance means the automation is intentionally paused for work

## Automation categories

The dashboard should support multiple automation categories as metadata:

- browser dashboard
- Docker service
- hosted web service
- local dashboard
- webhook-based workflow
- cloud service
- file pipeline
- email pipeline
- Trello pipeline
- document pipeline
- manual external tool

The category should guide which checks are relevant.

## Health check types

Recommended health check types:

- HTTP status check
- TCP port check
- route check
- dependency check
- file path check
- folder freshness check
- external API reachability check
- queue or backlog check
- synthetic dry-run check
- manual verification check

A synthetic dry-run should be preferred for mission-critical automations because a tool can be online while still failing its actual business purpose.

## Dependency model

Each automation can have many dependencies. Examples:

- Docker service
- Nginx route
- local folder
- Google API credential
- Trello board, list, or card
- Gmail label or search query
- database connection
- scheduled trigger
- webhook endpoint
- browser session
- cloud project

Dependencies should have their own status, last check time, notes, and required or optional flag.

## Route diagnostics

Route diagnostics should answer:

- Is the expected route configured?
- Is the route reachable?
- Does the route point to the expected host?
- Does the route point to the expected port?
- Does the endpoint return the expected response?
- Is there a mismatch between the stored automation config and the gateway config?

The current repository already contains an Nginx gateway and a dynamic sites-enabled include path. The Control Center should eventually validate against those generated route configs.

## Alert model

Alerts should be created when:

- an automation enters broken status
- an automation remains degraded beyond its threshold
- a required dependency fails
- a route check fails
- a health check is stale
- latency exceeds the SLO target
- no successful check has happened inside the expected window

Alerts should have severity, title, message, status, first seen time, last seen time, acknowledgement time, and resolution time.

## Incident model

Incidents are for larger operational failures that require human review. They should record the title, severity, current status, start time, resolution time, root cause, and resolution note.

An incident can group repeated alerts into one operational story.

## SLO model

Each automation should optionally define an SLO with availability target, maximum acceptable latency, maximum consecutive failures, monitoring window, and notes.

The dashboard should compare actual health history against these SLOs.

## Dashboard layout

Recommended dashboard sections:

1. Summary bar with healthy, warning, degraded, broken, unknown, and open alert counts.
2. Automation table with name, category, status, last checked, last successful check, last failure, failure reason, latency, and open alerts.
3. Detail drawer with launch target, health check target, route diagnostics, dependencies, recent health events, alerts, incidents, SLO, and notes.
4. Operations history with check results, status changes, alerts, acknowledgements, and incident notes.

## Implementation order

Recommended order:

1. Wire persistence for the operational models.
2. Wire health and diagnostics routes.
3. Add frontend service methods for health summary, health checks, and diagnostics.
4. Add the Control Center Angular page.
5. Add persistent health event writes in the health-check service.
6. Add dependency and route-check management.
7. Add alert creation logic.
8. Add incident grouping logic.
9. Add SLO calculations.
10. Add an approval-gated local runner for local operational actions.

## Safety boundary

Browser-safe opening of URLs is acceptable inside the web dashboard. Starting, stopping, or changing local programs and system services must be handled by a separate local runner with explicit approval boundaries, audit logs, and allow-listed actions.
