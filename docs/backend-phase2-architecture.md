# HAI Phase 2 — Backend Architecture (§11/§15/§16/§18)

This document preserves the mandatory Phase 2 backend flowcharts and maps each to
the packages that implement it. Everything here is real, tested code on branch
`codex/hai-next`; nothing is faked.

## Package architecture

```mermaid
flowchart TD
    Router[HTTP Router] --> Handlers[Handlers / DTO Validation]
    Handlers --> Services[Services / Command Handlers]
    Services --> Policy[Autonomy Policy]
    Services --> Repos[Repositories]
    Services --> Broker[Execution Broker]
    Services --> ModelIntel[Model Intelligence]
    Services --> Privacy[Privacy Filter]
    Services --> Verify[Verification]
    Repos --> Postgres[(Postgres)]
    Services --> Outbox[Event Outbox = OperationEvents]
    Services --> Redis[(Redis leases/rate limits optional)]
```

| Concern | Package |
| --- | --- |
| Operation Ledger | `internal/operations` |
| Background autonomy loop | `internal/background` |
| Autonomy policy engine | `internal/autonomypolicy` |
| Privacy / PII filter | `internal/privacyfilter` |
| Execution broker + local safe worker | `internal/executionbroker` |
| Runtime Lab (Hermes/Odysseus/contracts; read-only canonical OpenClaw projection) | `internal/runtimelab` |
| Governed agent-runtime registry and approved execution | `internal/agentruntime` |
| Architecture-aware model intelligence | `internal/modelintelligence` |
| Windows/local hardware profile | `internal/hardwareprofile` |
| Account feed bridges + registry | `internal/accountfeed` |
| Always-on runtime control (emergency stop/recovery/readiness) | `internal/opscontrol` |
| Idempotency + canonical JSON hashing | `internal/idempotency` |
| Composition root + HTTP handlers | `internal/phase2` |

## Ingestion → operation pipeline

```mermaid
flowchart TD
    FeedItem[Feed Item] --> Privacy[Privacy Scan]
    Privacy --> Source[Store Source Evidence]
    Source --> Dedupe[Compute Dedupe Key]
    Dedupe --> Operation[Create/Update Operation]
    Operation --> Decision[Policy Decision]
    Decision -->|Low Risk| SafeWorker[Local Safe Worker]
    Decision -->|Medium/High Risk| Approval[Approval Record]
    SafeWorker --> Verify[Verification Record]
    Verify --> Event[Operation Event + Audit]
    Approval --> Event
```

Implemented by `accountfeed.Registry.Sync` (fetch → privacy scan → dedupe-ingest)
and `background.Worker` (decision → safe worker + verify / approval).

## Repository / service / handler separation

```mermaid
flowchart LR
    HTTP[HTTP Request] --> Handler[Handler]
    Handler --> DTO[Validate DTO]
    DTO --> Service[Service]
    Service --> Domain[Domain Logic]
    Service --> Repo[Repository]
    Repo --> DB[(DB)]
    Service --> Response[Domain Result]
    Response --> Handler
    Handler --> JSON[API Response]
```

## Runtime broker (§15)

```mermaid
flowchart TD
    Operation[Operation ready to execute] --> Select[Runtime Broker selects adapter]
    Select --> Health[Health check runtime]
    Health -->|not installed/configured| Block[Block: setup required]
    Health -->|ready| DryRun[Dry run / plan exact action]
    DryRun --> Policy[Policy checks exact action payload]
    Policy -->|approval required| Approval[Server-side approval]
    Approval -->|approved| Execute[Execute runtime task]
    Policy -->|low-risk allowed| Execute
    Approval -->|rejected| Rejected[Mark rejected]
    Execute --> Capture[Capture bounded output + logs]
    Capture --> Verify[Verify postcondition]
    Verify -->|passed| Complete[Operation completed]
    Verify -->|failed/inconclusive| Review[Needs review]
    Complete --> Audit[Audit event]
    Review --> Audit
```

In Phase 2 the only adapter that actually executes is the local safe worker
(`executionbroker`); Hermes/OpenClaw/Odysseus are honest contracts that refuse
execution until real, operator-verified (`runtimelab`).

## Browser automation boundary (§15)

```mermaid
flowchart TD
    Op[Browser-needed operation] --> Domain[Check domain allowlist]
    Domain -->|not allowed| Block[Block]
    Domain -->|allowed| DryRun[Plan browser steps]
    DryRun --> Screenshot[Capture pre-action screenshot]
    Screenshot --> Approval[Approval if action can change state]
    Approval --> Execute[Execute allowed steps]
    Execute --> PostScreenshot[Capture post-action screenshot]
    PostScreenshot --> Verify[Verify expected page/result]
    Verify --> Audit[Store screenshots/evidence refs]
```

The browser runtime is a **contract only** (`runtimelab` `newBrowserContract`):
allowed read/draft actions and forbidden-by-default state changes are published;
no executor is wired or faked.

## Architecture-aware routing (§16)

```mermaid
flowchart TD
    A[Operation] --> B[Task Lane Classifier]
    B --> C[Architecture-Aware Model Registry]
    C --> D[Provider Health + Telemetry]
    D --> E[Autonomy Policy]
    E --> F[Route Selection]
    F --> G1[Fast Triage Model]
    F --> G2[Long-Context Model]
    F --> G3[Omni Model]
    F --> G4[Diffusion / Parallel Draft Model]
    F --> G5[Byte-Robust Model]
    F --> G6[Looped / Deep Reasoning Model]
    F --> G7[Verifier Model]
    G1 --> H[Verification]
    G2 --> H
    G3 --> H
    G4 --> H
    G5 --> H
    G6 --> H
    G7 --> H
    H --> I[Approval or Execution]
    I --> J[Audit + Learning]
```

Implemented by `modelintelligence` (`ClassifyLanes`, `Router.Route`, registry,
telemetry). The privacy lane restricts cloud routing; the fast-triage lane runs a
real bounded local call per background operation.

## Windows serving-stack selection (§18)

```mermaid
flowchart TD
    Task[Model Work Needed] --> Hardware[Detect Hardware Profile]
    Hardware --> WinNative[Native Windows Path?]
    Hardware --> Nvidia[NVIDIA GPU?]
    Hardware --> NPU[Qualcomm / Intel / AMD NPU?]
    Hardware --> LowVRAM[CPU-only or Low VRAM?]
    Hardware --> Power[Power Mode / Battery]

    WinNative --> WinML[Windows ML / ONNX Runtime]
    Nvidia --> WSL[WSL2 + CUDA for Linux-first LLM Serving]
    Nvidia --> TRT[Native ONNX TensorRT RTX EP if supported]
    NPU --> EP[QNN / OpenVINO / VitisAI EP]
    LowVRAM --> Llama[llama.cpp / GGUF Quantized Model]
    WinNative --> Ollama[Ollama / LM Studio / LocalAI / vLLM]

    WinML --> Router[HAI Model Router]
    WSL --> Router
    TRT --> Router
    EP --> Router
    Llama --> Router
    Ollama --> Router
    Power --> Scheduler[Power-Aware Scheduler]
    Scheduler --> Router
```

Implemented by `hardwareprofile` (`Detect`, `SelectServingStack`, power policy).
Detection is truthful: Windows ML / GPU / NPU are never claimed off-Windows or
without detection; unknown hardware selects `onnx_runtime_cpu`.

## Efficiency optimization pipeline (§18)

```mermaid
flowchart TD
    Model[Model / Provider] --> Format[Model Format: ONNX / GGUF / API / OpenAI-Compatible]
    Format --> Quant[Quantization: int8 / int4 / GGUF / KV Quant]
    Quant --> Cache[Prefix Cache / Semantic Cache / Source Summary Cache]
    Cache --> KV[KV Strategy: Paged KV / Chunked Prefill / Continuous Batching]
    KV --> Context[Context Budget Manager]
    Context --> Effort[Reasoning Effort Controller]
    Effort --> Route[Lane-Based Routing]
    Route --> Verify[Verifier]
    Verify --> Telemetry[Quality per Token/Watt/Euro Telemetry]
```

Implemented by `modelintelligence` (`OperationBudget`, `Cache` with §19 reuse
boundaries, `RecommendReasoning`, `Router`, `ComputeQualityScore`, durable
`TelemetryStore`). Model format/quantization/KV labels are metadata HAI records;
it does not implement foundation-model internals.

## Model telemetry transaction (§10.9)

Model calls happen outside the DB transaction. After a call, HAI creates a
durable `ModelRunTelemetry` row (`models.ModelRunTelemetry`, persisted via
`modelintelligence.GormTelemetryRepository`), updates observed metrics + claim
level, writes a cache record if safe, and appends an OperationEvent if linked.
Telemetry survives restart (proven in `smoke-model-intelligence.sh`).

## Outbox

`OperationEvent` rows are the durable event log written alongside every state
change. The Kafka publisher is optional and disabled in this deployment, so no
events are lost (they persist in Postgres). No fake publisher is implemented.
