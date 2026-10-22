# Model provider reconciliation

## Reviewed retained versions

Compared fourteen source versions with main
`4684546907900d4958f85e7b71ba8848376b0b93`:

| Files | Versions | Integration decision |
| --- | ---: | --- |
| `calibration.go` and `calibration_test.go` | 2, model-maintenance worktree | Retain exact nonnegative zero for the Wilson bound with zero accepted outputs. |
| `http_openai.go` | 1, model-maintenance worktree | Retain endpoint credential/query rejection, direct no-proxy/no-redirect clients and provider-scoped internal-host validation. Wire that validation into the constructor. |
| `maintenance.go` and `maintenance_test.go` | 4, both worktrees | Retain canonical context-aware local maintenance, exact selected-model identity, cancellation and paid/budget gates. Hosted models must not invoke the local artifact updater. |
| `provider.go` | 1, model-maintenance worktree | Retain typed-nil dependency handling and redacted errors that preserve error identity. |
| `provider_registry.go` | 2, both worktrees | Retain explicit local inference attestation, unknown billing state, model-specific readiness evidence, timestamp expiry and private raw adapters. |
| `service.go` | 2, both worktrees | Retain durable telemetry gating, bounded single-flight benchmarks, token accounting, context cancellation, health-response ordering and strict inference identity/metrics validation. |
| `service_test.go` | 1, model-maintenance worktree | Retain external inference rejection and endpoint secret rejection coverage. |
| `fake_provider.go` | 1, artifact worktree | Retain explicit deterministic classification, estimated token labeling and measured rule timing instead of fixed fabricated model throughput. |

All filenames above are under `backend/internal/modelintelligence/`. Older
exports that expose raw adapters, structural-only readiness or implicit free
billing are superseded, not restored. This review covers these selected versions,
not all model integrations or all remaining worktree files.

## Wiring repair

The provider-specific endpoint validator already permitted `ollama-local` for
the Ollama provider only. Its helper test passed, but the actual local provider
constructor called the generic validator, which rejected the same identity.
The constructor now calls the provider-specific validator with its provider ID.

This is a configuration wiring repair, not automatic trust: local inference
attestation, model probe, canonical maintenance, budget and telemetry controls
remain unchanged. No runtime, model download or paid provider is enabled.

## Evidence and limits

- A constructor-to-inference regression failed before repair at provider
  configuration. After repair, the actual registry constructor, model probe,
  lane selection, maintenance call and HTTP inference complete against a
  controlled local server. It checks the exact endpoint/model maintenance
  identity, two model probes and one generation.
- The fixture preserves the Compose hostname in requests but redirects only
  its test dial to loopback. It does not validate real Docker DNS or Ollama.
- Negative constructor checks keep the Ollama hostname unavailable to LM
  Studio, llama.cpp, LocalAI and vLLM, including maintenance identity export.
- Full `modelintelligence` and `llm` package tests passed locally on Windows Go
  1.27.3. The maintenance gate in the new HTTP-chain test is a recording fixture,
  not a real model pull or provider update.
- Original copies and private integrity receipts remain intact. Semantic review
  and an open PR do not prove exact main preservation or permit local deletion.
