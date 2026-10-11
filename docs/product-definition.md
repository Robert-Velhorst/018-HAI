# Product Definition & User Outcome Contract

## What 018-HAI is

A local-first Personal AI Operating System: it ingests the user's own sources,
remembers context, plans and executes bounded automations under approval, and
grounds every answer in evidence — without pretending an external step happened.

## Who it's for

A single operator/professional who wants an assistant they can trust with their
data locally, not a cloud service that phones home.

## Outcome contract (what the user is promised)

| Promise | Guaranteed by |
| --- | --- |
| "Remember my context and surface what's relevant." | memory create/dedup/retrieve + search |
| "Never act without my approval." | approval gates, autonomy gate, pre-action safety |
| "Never fake that something happened." | no-fake-success rules, honest completion matrix, demo-mode labelling |
| "Keep my data local and private." | local-first analytics, redaction, encryption, retention/deletion |
| "Tell me if you're healthy and ready." | `doctor`, `/healthz`, `/readyz`, `/system/info` |
| "Show me how reliable an answer is." | grounded answers + quality/confidence bands |

## Non-goals

- Not a cloud SaaS; not a general web scraper that evades access controls.
- Not an unattended autonomous desktop agent — actions are bounded, allowlisted,
  approval-gated, and verified.

## Success definition

The critical path (dashboard → source → task → routing → approval → execution →
verification → audit) works end to end with real, non-faked behavior, and the
user can always see status, activate an emergency stop that blocks new execution,
request supported cancellation for an in-flight runtime, and export/delete their data.
