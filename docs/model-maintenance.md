# Model Maintenance

This is the operator guide to HAI's scheduled model-maintenance path. It describes what the current backend does; it is not evidence that a live provider or local runtime was accepted. The effective runtime policy is authoritative and may replace the default provider list through `LLM_PROVIDERS_JSON` or `LLM_POLICY_JSON` ([policy construction](../backend/internal/llm/policy.go#L336)).

## Operator summary

HAI has one model-download path: a local Ollama-compatible runtime for provider IDs `ollama` and `miniswe-ollama`. It refreshes the exact configured tag, then verifies the digest reported by that runtime. Other local runtimes are probe-only; non-local providers are catalog- or health-probe-only. HAI does not install hosted models, update other local runtimes, or update the Ollama container image.

## Eligibility and scope

The scheduled sweep walks the providers and models in the effective policy. A model is considered only when it is enabled, its provider is enabled and has a configured/accepted endpoint, maintenance is enabled, and durable maintenance history is available. Provider policy must also permit that category. The sweep does not enumerate installed models or the whole model catalog ([maintenance.go](../backend/internal/llm/maintenance.go#L275), [eligibility checks](../backend/internal/llm/maintenance.go#L287), [provider gates](../backend/internal/llm/maintenance.go#L347)).

| Effective provider/model | Default-policy scope | Scheduled behavior |
| --- | --- | --- |
| Local `ollama` | Only IDs in `OLLAMA_MODEL_IDS`; absent that setting, the active list defaults to `phi3:mini`. The wider built-in catalog is not swept automatically. | Pulls the configured local tag when due, then compares installed digests. Ollama cloud-tag IDs are reclassified as non-local/paid and cannot use this path. |
| Local `miniswe-ollama` | A dedicated isolated Ollama endpoint. It is not listed in the default policy provider array; `EnsureMiniSWEOllamaModel` invokes its gate when that workflow needs the model. It joins the scheduled sweep only if included in the effective policy. | Uses the same pull-and-digest path when invoked and authorized; cloud-tag IDs are skipped by the sweep. |
| Other local providers | Default entries include `lm-studio`, `llama-cpp`, `localai`, `vllm`, `sglang`, `mistral-rs`, conditional `dspark` and `litellm`, and `odysseus`. Their candidates are the model IDs in the effective policy, not every model installed in those runtimes. | Read-only runtime check. HAI does not install or update these models; even a healthy endpoint is blocked from model use when HAI cannot verify the exact configured model version. |
| Non-local/cloud providers | Only enabled models in the effective policy. Ollama cloud tags are treated as non-local/paid. | Read-only catalog check; no model download or hosted-version update. Paid providers require paid usage to be enabled and a positive configured budget to enter the sweep; the probe also blocks if approval is still required or budget/accounting is unavailable. |

In the default policy, paid calls are disabled, the daily paid budget is zero, and approval before paid use is required; the default free-cloud entry is disabled with zero quota. Those defaults do not authorize paid catalog probes. Changing policy can make a provider eligible for a read-only check; it does not add an updater for that provider ([default policy](../backend/internal/llm/policy.go#L2597), [default providers](../backend/internal/llm/policy.go#L2610)).

**Odysseus configuration caveat:** the default policy marks Odysseus `Local: true`, so the maintenance dispatcher sends it through the non-Ollama local verification path, which requires the exact configured model ID in the probe response. The Odysseus probe itself is health-oriented and does not establish model version. `TestOdysseusMaintenanceReportsHealthOnly` builds a zero-value provider (`Local: false`), so it exercises the non-local health-only branch, not the default policy shape. Do not treat that test as proof that a default-configured Odysseus model passes maintenance ([default entry](../backend/internal/llm/policy.go#L2718), [dispatcher](../backend/internal/llm/maintenance.go#L525), [health probe](../backend/internal/llm/policy.go#L1428), [test fixture](../backend/internal/llm/maintenance_scheduler_test.go#L984)).

## Cadence

Both maintenance and the scheduler are enabled by default. When the background-operations gate allows work, the scheduler runs an initial sweep and then schedules from each model's persisted due time. A successful/reusable result is fresh for 24 hours from its check time; the interval environment setting is clamped to exactly 24 hours, so it cannot currently select a different interval. This is a rolling per-model interval, not a once-daily wall-clock job. Failed checks remain blocked and use a shorter retry window (five minutes by default) ([scheduler](../backend/internal/llm/maintenance_scheduler.go#L29), [startup run](../backend/internal/llm/maintenance_scheduler.go#L109), [fixed interval](../backend/internal/llm/maintenance.go#L870), [retry interval](../backend/internal/llm/maintenance.go#L890)).

`LLM_MODEL_MAINTENANCE_ENABLED=false` disables the maintenance gate. `LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED=false` disables the background scheduler; disabling the scheduler does not itself establish freshness for generation-time checks. A missing durable maintenance-history repository prevents a scheduled sweep from running, and Ollama refresh also fails closed when durable admission storage is unavailable.

## What digest verification proves

For Ollama, HAI reads `/api/tags` before refresh, finds the exact configured model name, and requires a non-empty digest. It posts that configured name to `/api/pull`, requires an explicit success response, then reads `/api/tags` again. HAI records `updated` when the before/after digests differ, `installed` when the model was absent before, and `current` when the digest did not change ([pre-check and pull](../backend/internal/llm/maintenance.go#L916), [digest read](../backend/internal/llm/maintenance.go#L1182), [pull confirmation](../backend/internal/llm/maintenance.go#L1239)).

This verifies the digest that the configured runtime reports as installed. It does **not** independently establish that the digest is newer, identify an upstream release, verify publisher signatures/provenance, test model quality, or prove compatibility with HAI's prompts, memory, tools, or workload. A changed digest means “different,” not “known-good latest.”

## Failure and rollback behavior

Ollama refresh requires a durable per-provider/model admission claim before provider I/O and a fresh final-effect authorization for each pull. Authorization is limited to local Ollama provider IDs and rechecks the emergency stop immediately before the effect ([admission path](../backend/internal/llm/maintenance.go#L578), [allowed pull providers](../backend/internal/llm/effect_authorization.go#L215), [final authorization](../backend/internal/llm/effect_authorization.go#L250)).

If the pull fails, is cancelled after it was attempted, reports no explicit success, or the post-pull digest cannot be verified, HAI records a failure and blocks model use until a later successful check. After an attempted pull failure it makes a bounded best-effort read to record the digest it observes. **HAI does not roll back or restore the previous model artifact.** A partial/failed pull can therefore leave changed files or a changed tag installed while execution remains blocked ([failure handling](../backend/internal/llm/maintenance.go#L981), [post-failure inspection](../backend/internal/llm/maintenance.go#L988)).

## Tests and real-world acceptance gaps

The unit tests use `httptest` endpoints to simulate Ollama tags/pulls and cloud catalogs. They cover digest-change reporting, missing-model installation, failure on unverifiable digests or missing success status, cancellation, daily result reuse, and read-only cloud checks ([Ollama update test](../backend/internal/llm/policy_test.go#L795), [scheduled configured-model test](../backend/internal/llm/policy_test.go#L1427), [cloud probe test](../backend/internal/llm/maintenance_scheduler_test.go#L806)). A PostgreSQL integration test exercises durable admission across service instances, but its Ollama endpoint is still simulated ([integration test](../backend/internal/llm/model_maintenance_admission_postgres_integration_test.go#L29), [test server](../backend/internal/llm/model_maintenance_admission_postgres_integration_test.go#L153)).

Still unproven against real systems: registry/tag resolution and download behavior; real installed-runtime digest semantics; interruption, disk exhaustion, and recovery during a large pull; rollback or staged/canary promotion; and post-update inference/quality/compatibility. Cloud checks prove only what the provider's read-only endpoint returns. Live acceptance must be performed separately on a disposable local runtime and explicitly authorized provider accounts; these tests are not that acceptance.
