# Model maintenance core reconciliation

## Selected source review

Compare the full meaningful differences in all twelve present changed or
untracked LLM paths in the selected model-maintenance worktree inventory against
main commit
`30781d4fb393386aecab0c449d100d158f4eabf5`. Verify raw original contents against
the private inventory separately from newline-normalized diff review.

| File relative to `backend/internal/llm/` | Decision |
| --- | --- |
| `maintenance.go` | Keep read-only cloud catalog probes, separately reported health-only checks, durable-history failure blocking, per-provider cache reuse, opaque credential/policy fingerprints, distributed maintenance leases, short retry for budget failures and completion-time check timestamps. Keep unsupported local version claims blocked. Independently fix endpoint-identity aliases found during review. |
| `maintenance_cooldown.go` | Keep the explicit positive-status allowlist; unknown or empty statuses must not become successful freshness evidence. |
| `maintenance_scheduler.go` | Keep explicit background permission, per-service duplicate-scheduler admission, lifecycle tracking, durable wake handling and bounded permission-monitor cancellation. Do not restore startup without a permission callback or detached untracked goroutines. |
| `calibration_test.go` | Keep explicit maintenance opt-out for calibration-specific generation tests; these tests must not imply daily maintenance ran. |
| `effect_authorization_test.go` | Keep the shared explicit test-only maintenance opt-out helper. Production maintenance admission is unchanged by this fixture. |
| `maintenance_scheduler_test.go` | Keep cancellation before inference/fallback, daily authenticated catalog checks, paid approval gates, distributed lease/recheck/release coverage, health-only distinctions, missing-callback denial, single-flight startup, wake consumption and provider-aware cache assertions. Remove no safeguards to restore old cloud-without-contact expectations. |
| `policy.go` | Keep durable paid reservations before dispatch, conservative uncertain usage, output withholding on audit failure, explicit paid enablement, bounded generation/probe timeouts, one total Odysseus probe timeout, protocol status/body validation, provider-key redaction and validated transport selection. |
| `policy_test.go` | Keep endpoint and transport restrictions, non-retried provider POSTs, read-only maintenance catalog behavior, bounded timeout cases, paid reservation admission and audit-write output withholding. A fake accounting repository is not proof of concurrent PostgreSQL behavior. |
| `repository.go` | Keep transaction-scoped daily budget locks, append-only reservation/settlement linkage, conservative unresolved reservations, non-double-counted aggregates, bounded collapsed history and deterministic checked/created/id ordering. Do not restore accounting based only on process-local state. |
| `model_maintenance_admission_test.go` | Keep zero-provider-call admission failure when durable maintenance history is missing. |
| `model_maintenance_admission_postgres_integration_test.go` | Keep the checked concrete repository fixture needed for disposable PostgreSQL cross-instance/recovery tests. Do not treat compilation or unit coverage as execution of this integration-tagged suite. |
| `repository_lease_test.go` | Keep caller-context coverage and deterministic checked/created/id ordering assertions. |

No missing retained behavior was found in these twelve differences. Original
integrity verification matched all twelve recorded raw file hashes. This is
not a completed review of every file in the model-maintenance worktree, other
worktrees or the transcript archives; those remain in scope for reconciliation.

## Endpoint identity repair

The maintenance key lowercased path segments, removed an uppercase version
suffix, collapsed internal empty path segments and omitted IPv6 brackets.
Consequently distinct endpoints could share the same maintenance identity.
That key is also used when checking whether an auxiliary planning endpoint
matches an enabled configured local provider.

Preserve path case, strip only the supported exact lowercase `/v1` suffix,
reject repeated internal slashes, and retain IPv6 authority brackets. Keep
existing host-case and default-port normalization and ordinary compatibility
suffix behavior. The change can invalidate old maintenance fingerprints for
previously aliased endpoints; do not reuse another endpoint's freshness evidence.

New tests cover five target-identity collisions, IPv6/default-port rendering,
preservation of mount case and the actual configured-local planning gate.
The planning-gate test disables maintenance I/O explicitly: it proves endpoint
admission, not provider freshness or a live model update. It also verifies that
the exact configured endpoint with the ordinary `/v1` suffix remains accepted.

## Validation and retention

The focused new tests failed before the production change, reproducing all
five identity collisions and incorrect planning endpoint admission. They passed
after the repair. The full LLM package also passed locally on Windows with Go
1.27.3 using `go test -p 1 -timeout 180s ./internal/llm -count=1`. Hosted
exact-head CI remains a separate merge gate. No real provider or model artifact
was updated in this review.

All twelve original versions remain intact. Semantic decisions do not make
byte-different originals, ignored files or the complete worktree deletable.
Private inventory evidence and public source review serve different purposes;
this document contains no private paths, source transcript identifiers or secrets.
