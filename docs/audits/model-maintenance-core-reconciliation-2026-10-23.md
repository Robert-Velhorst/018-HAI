# Model maintenance core reconciliation

## Selected source review

Compare the full meaningful differences in three retained model-maintenance
worktree files against main commit
`30781d4fb393386aecab0c449d100d158f4eabf5`. Verify raw original contents against
the private inventory separately from newline-normalized diff review.

| File relative to `backend/internal/llm/` | Decision |
| --- | --- |
| `maintenance.go` | Keep read-only cloud catalog probes, separately reported health-only checks, durable-history failure blocking, per-provider cache reuse, opaque credential/policy fingerprints, distributed maintenance leases, short retry for budget failures and completion-time check timestamps. Keep unsupported local version claims blocked. Independently fix endpoint-identity aliases found during review. |
| `maintenance_cooldown.go` | Keep the explicit positive-status allowlist; unknown or empty statuses must not become successful freshness evidence. |
| `maintenance_scheduler.go` | Keep explicit background permission, per-service duplicate-scheduler admission, lifecycle tracking, durable wake handling and bounded permission-monitor cancellation. Do not restore startup without a permission callback or detached untracked goroutines. |

No missing retained behavior was found in these three differences. This is not
a completed review of every file in the model-maintenance worktree. Remaining
test, policy, persistence and other module versions still require reconciliation.

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

All three original versions remain intact. Semantic decisions do not make
byte-different originals, ignored files or the complete worktree deletable.
Private inventory evidence and public source review serve different purposes;
this document contains no private paths, source transcript identifiers or secrets.
