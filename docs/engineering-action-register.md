# 018-HAI Engineering Action Register

This register converts the real-world readiness audit into concrete engineering actions. The canonical product is the Go backend, Angular dashboard, Postgres persistence, and Docker Compose local Windows setup. The Manus React/tRPC/MySQL stack is reference-only unless a future ADR changes that.

## Immediate Actions Completed In Code

1. Add a live LLM provider probe API for configured providers.
2. Probe Ollama through `/api/tags`.
3. Probe OpenAI-compatible providers through `/v1/models`.
4. Prevent LLM probes from following redirects.
5. Block paid-provider probing until server-side paid approval exists.
6. Bound provider probe timeout with `LLM_PROVIDER_PROBE_TIMEOUT_SECONDS`.
7. Surface provider probe results in the Angular LLM policy dashboard.
8. Add frontend service/types for LLM provider probes.
9. Add backend tests proving Ollama probe behavior.
10. Add backend tests proving provider probes do not follow redirects.
11. Add shared backend secret redaction helpers.
12. Redact common password, token, API key, authorization, and private-key patterns.
13. Redact user info and sensitive query parameters from logged URLs.
14. Redact controlled runtime launch output before it is returned.
15. Redact controlled runtime launch output before it is persisted as a launch event.
16. Redact launch failure reasons before storing them on automation records.
17. Redact API launch response bodies.
18. Redact API launch target URLs in operational messages.
19. Redact LLM provider error bodies before returning generation failure reasons.
20. Redact LLM generation output for common secret patterns before returning it.
21. Stop inheriting the host process `PATH` for script execution.
22. Provide a minimal deterministic script execution `PATH`.
23. Keep extra script environment variables behind `AUTOMATION_SCRIPT_ENV_ALLOWLIST`.
24. Remove the remaining image upload filename log during automation updates.
25. Document live provider probes in the README.
26. Document runtime/LLM redaction in the README.
27. Update the canonical-stack ADR with provider probe readiness policy.
28. Update the runtime safety ADR with redaction requirements.
29. Extend router smoke coverage for the provider probe endpoint.
30. Add unit coverage for redacting script output secrets.
31. Add unit coverage for redacting API target and response secrets.
32. Add unit coverage for redaction helpers.

Additional completed autonomous-control actions:

- Add shared backend emergency-stop policy helpers.
- Block LLM generation when `HAI_EMERGENCY_STOP=true`.
- Block automation runtime launches when `HAI_EMERGENCY_STOP=true`.
- Block task success-engine execution while leaving planning/review visible.
- Block workflow worker execution without consuming due workflow items.
- Block follow-up/open-loop worker actions without triggering proposals.
- Surface emergency-stop state and redacted reason in the HAI OS overview.
- Add backend tests for emergency-stop enforcement across LLM, automation, task, workflow, and safety helpers.

## Next Engine Actions

33. [x] Add a durable provider probe history table.
34. [x] Record last successful provider probe per provider.
35. [x] Prevent routing to configured-but-never-probed providers when strict mode is enabled.
36. [x] Add dashboard indicators for last probe time and last probe failure.
37. [x] Add an integration fixture for a local Ollama-compatible mock service in Docker Compose.
38. [x] Add an integration fixture for an OpenAI-compatible mock service in Docker Compose.
39. [x] Add CI that runs the live provider probe fixtures.
40. Add seeded local-folder source fixtures to prove source ingestion end-to-end.
41. Add a seeded workflow fixture that starts from connected-source sync.
42. Add an end-to-end test from source extraction to workflow candidate creation.
43. Add a test proving unsupported source connectors cannot be scheduled as if live.
44. Add a connector capability registry with `implemented`, `stub`, `disabled`, and `blocked` states.
45. Add a public connector readiness endpoint.
46. Add per-source readiness history and last sync evidence.
47. Add a sandbox Gmail adapter against a test mailbox before production Gmail.
48. Add a sandbox Google Calendar adapter against a test calendar.
49. Add a sandbox Google Drive adapter against a test folder.
50. Add a sandbox Trello adapter against a test board.
51. Add a GitHub adapter that reads repository issues through a configured token.
52. Keep all real account adapters disabled until OAuth scopes are minimal and reviewed.
53. Add connector permission diffing before enabling a source.
54. Add source revocation tests that prove sync stops after revoke.
55. Add source pause tests that prove scheduled sync skips paused sources.
56. Add file provenance display that avoids exposing unnecessary local absolute paths.
57. Add local-folder source export with provenance and redaction controls.
58. Add near-duplicate document detection.
59. Add final-vs-draft version hints for ingested documents.
60. Add a review queue for uncertain source extractions.
61. Add structured claim-source precision checks against evidence snippets.
62. Add contradiction detection tests using seeded source records.
63. Add deterministic date extraction tests for Dutch and English date phrasing.
64. Add deadline-to-follow-up scheduling tests.
65. Add calendar reminder creation behind approval or local-only mode.
66. Add workflow approval records that include approver identity.
67. Add immutable audit-event records for approval decisions.
68. Add emergency-stop state persisted in the backend.
69. Make emergency stop block launches, workflow execution, and provider generation.
70. Add dashboard emergency stop controls.
71. Add a task execution mode that refuses to run without success criteria.
72. Add a validation gate that blocks completion when required evidence is missing.
73. Add retry budget persistence per workflow item.
74. Add exponential retry backoff for transient worker failures.
75. Add dead-letter state for exhausted workflow retries.
76. Add a worker heartbeat table.
77. Add stale worker detection in the dashboard.
78. Add per-runtime allowlist diagnostics.
79. Add script allowlist inventory endpoint.
80. Add script hash pinning before execution.
81. Add script output size and duration dashboard columns.
82. Add Docker socket mount detection to the HAI OS overview.
83. Add Docker container allowlist diagnostics.
84. Add API launch method and host diagnostics.
85. Add stricter controls for private-network API targets unless explicitly allowlisted.
86. Add a blocked-action audit dashboard.
87. Add provider quota refresh hooks for real free/freemium providers.
88. Add budget ledger records for every provider call, including zero-cost local calls.
89. Add manual approval workflow for paid provider calls before enabling paid routing.
90. Add a policy test proving paid usage remains impossible at `daily_paid_budget_eur: 0`.
91. Add migration files instead of relying only on Gorm `AutoMigrate`.
92. Add database schema drift checks in CI.
93. Add a canonical-stack guard in docs and dashboard linking Manus only as reference.
94. Add a Manus behavior porting checklist with explicit acceptance criteria.
95. Add dependency freshness checks for Go and Angular packages.
96. Add Windows 11 smoke instructions for real Ollama and local-folder ingestion.
97. Add a support bundle export with redacted logs and health evidence.
98. Add an operational readiness score that separates internal logic from live integrations.
99. Add trace IDs across source sync, task planning, workflow execution, and verification.
100. Add an end-to-end acceptance test for the complete loop: source input, workflow item, approval gate, safe execution, verification, audit log.
