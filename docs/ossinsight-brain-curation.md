# OSS Insight Brain And Operations Curation

## Purpose

This document records the 2026-07-19 review of
[OSS Insight Collections](https://ossinsight.io/collections). OSS Insight is a
repository-discovery and ranking source. HAI uses it to discover candidate
capabilities, never to automatically download, vendor, enable, or execute
third-party code.

## Review rule

An upstream project becomes a HAI catalog entry only when it has a concrete
role in one of these planes:

| Plane | Needed capability | HAI decision rule |
| --- | --- | --- |
| Thinking | Model routing, retrieval, evidence context | Must preserve local-first routing, source provenance, and verified completion. |
| Memory | Semantic search or graph enrichment | Must not create competing truth stores or bypass retention controls. |
| Operations | Durable jobs, retries, follow-ups | Must not self-authorize work or conceal workflow state. |
| Execution | Runtime, code tools, MCP | Must have a reviewed adapter, scoped workspace, tool allowlist, audit trail, and approval gate. |
| Observability | Health, queue, model, and cost telemetry | Must report real source-backed metrics, not placeholders. |

## Curated outcomes

| Collection | Candidate | Disposition | First safe increment |
| --- | --- | --- | --- |
| ai-gateways | LiteLLM | Integrated profile | Enable a keyed loopback gateway profile; HAI requires manual approval because proxy billing cannot be inferred. |
| Vector Database & Vector Store | pgvector | Integrated profile | Opt-in `vector` extension plus local embeddings; keyword retrieval remains the fallback. |
| Workflow Scheduler | Temporal | Integrated, opt-in | Run one local durable, proposal-only follow-up workflow through a narrow Go worker. |
| Monitoring Tool | Prometheus | Integrated profile | Enable a token-protected local metrics endpoint; configure a separate local collector when needed. |
| Model Context Protocol Client | MCP Inspector | Integrated profile | HAI-owned, local-only Streamable HTTP preflight lists tools before a separately reviewed adapter activation. |
| ChatGPT Alternatives | llama.cpp | Integrated local provider | Configure a loopback or `host.docker.internal` OpenAI-compatible `llama-server`; HAI probes `/v1/models` before it can route or generate. |
| Testing Tools | Playwright | Integrated, opt-in | Verify named, allowlisted local routes without clicks, forms, downloads, retained state, or external origins. |
| WebAssembly Runtime | Wasmtime | Integrated, opt-in | Run reviewed capability-limited WASI helper modules only through HAI's approval-gated manifest runner. |
| Optimization Solvers | OR-Tools | Integrated profile | Optional internal CP-SAT schedule proposals with bounded inputs and no apply capability. |
| Monitoring Tool | Grafana | Reference only | Revisit only after real Prometheus data needs advanced visualization. |
| GraphRAG | LangChain, LlamaIndex, Cognee | Reference only | Revisit only for a measured retrieval or graph-provenance gap. |
| Vector Database & Vector Store | Qdrant | Reference only | Revisit only if pgvector proves insufficient with a migration and rollback plan. |
| Zapier Alternatives | Activepieces | Reference only | Do not add a second workflow control plane without a demonstrated connector gap. |
| LLM Tools | Mem0 | Reference only | HAI retains one memory/provenance owner. |
| Open Source Data Catalogs | OpenMetadata | Reference only | Reference governance patterns without deploying a second source catalog. |
| Zapier Alternatives | n8n | License review | Fair-code licensing and overlapping workflow ownership need an explicit decision. |
| Distributed File Storage | MinIO | Excluded | Archived upstream and AGPLv3 do not meet the current adoption bar. |

## Direct upstream additions after the collection pass

The collection pass is not the only valid discovery input. A project supplied
by the owner can enter the same review-first catalog when its upstream record
is inspected and it has a concrete HAI plane. Direct review does not bypass
the collection-screening, adapter, provenance, safety, or resource gates.

| Project | Plane | Disposition | Required first gate |
| --- | --- | --- | --- |
| Evidently | Verification and observability | Integrated, opt-in | Internal report runner for bounded synthetic/redacted fixtures; metadata-only output and no default egress. |
| Whylogs | Source-quality profiling | Reference only | Compact profile and constraint patterns only; the 2024 latest package release, overlap with Evidently, and default anonymous analytics require a measured gap and separate freshness/privacy review before any local adoption. |
| Guardrails AI | Verification and safety | Integrated, opt-in | Internal fixed-schema action-proposal validator; metadata-only output, no model call, Hub download, persistence, approval, or execution. |
| LM Evaluation Harness | Model evaluation | Integrated, opt-in | Fixed six-case synthetic local suite against one preconfigured local OpenAI-compatible model; aggregate metadata only and manual review required. |
| LiveKit Agents | Intake and controlled execution | Candidate | Explicit session-consent model, self-hosted/local service, configured providers, and a no-tool/no-contact default. |
| mistral.rs | Thinking and local inference | Integrated, opt-in | Loopback-only OpenAI-compatible `/v1` server, approved model and resource configuration, live probe, and disabled upstream agentic tools. |
| AG2 | Thinking and execution compatibility | Compatibility only | Fixed-schema bridge for an existing workload; no new parallel HAI runtime. |
| RAGFlow | Memory and source intake | Integrated, opt-in | Measured retrieval gap, named local data sources, provenance/deletion plan, capacity reservation, disabled code executor, and fixed-dataset local retrieval configuration. |
| SWE-agent | Coding-worker architecture | Reference only | The upstream recommends mini-SWE-agent, so HAI retains SWE-agent only as a historical comparison and does not add a legacy code-worker. |
| mini-SWE-agent | Controlled coding worker | Candidate | Pinned release, disposable copied worktree, no host shell/Docker socket/secrets/accounts, deny-by-default network, strict resource caps, diff-only output, selected deterministic tests, and human acceptance are required before any experimental adapter. |
| Promptfoo | Model safety regression | Integrated, opt-in | One reviewed local model endpoint, fixed synthetic suite, no external providers or telemetry, no production data, aggregate-only report, and human review before any routing or policy decision. |
| Serena | Coding-context retrieval | Integrated, opt-in | One owner-started loopback MCP endpoint pinned to a single project, a stable HAI project label, MCP handshake/tool inventory probe, and HAI's fixed `find_symbol` allowlist with source-body and hover data disabled. |
| Odoo | Business-system source ingestion | Integrated, opt-in | One operator-owned JSON-2 endpoint, read-only API key, optional database, and fixed HAI model/field allowlist. HAI uses only bounded `search_read`; all writes, payments, accounting changes, and generic RPC calls remain unavailable. |
| CloudQuery | Source-intake sync health | Integrated, opt-in | One local, operator-produced `cloudquery sync --summary-location` JSONL file under an explicit read-only mount. HAI does not run CloudQuery, inspect its configuration/credentials, or access source/destination records; it converts only bounded completed summary rows into provenance-linked review signals. |
| OpenSpec | Spec-driven software planning | Integrated, opt-in | One selected local project folder. HAI reads only active `openspec/changes` proposal/design/tasks/spec Markdown as reviewable planning bundles; it does not run OpenSpec, scan repository code, write files, or authorize coding execution. |

RAGFlow is deliberately not presented as HAI memory. Its parsed chunks and
citations are candidate evidence that must pass HAI's source-grounding,
freshness, conflict, and memory-update controls before influencing a fact,
task, or external action.

HAI's disabled-by-default bridge exposes only a local endpoint probe and fixed-
dataset retrieval. It cannot ingest or delete documents, invoke RAGFlow chat,
agents, MCP, or code execution, alter RAGFlow configuration, or create memory,
facts, workflows, or external actions automatically.

## Non-negotiable boundaries

1. A catalog candidate is not an installed integration.
2. No candidate can override model policy, paid-budget controls, source permissions, approval, audit, or emergency stop.
3. No external project receives arbitrary local shell, browser, credential, or network access from catalog registration.
4. A health check proves reachability, not safe task execution.
5. Real integration requires its own configuration, adapter, failure tests, owner approval policy, and rollback path.

## Back-office behavior

`GET /api/v1/brain-catalog/` returns the curated entries, their collection
provenance, activation boundary, status, and recommendation data. Planning can
surface a matching candidate, but tool routing marks it unavailable until the
specific adapter is present and reviewed.

The complete category pass, candidate shortlist, and exclusion rationale are in
[the OSS Insight screening ledger](ossinsight-screening-ledger.md).

## Implemented local inference boundary

`llama.cpp` is now a first-class local provider in both HAI model back-office
registries. Set `LLAMA_CPP_BASE_URL` and, when needed,
`LLAMA_CPP_MODEL_ID` to enable it. The endpoint may only use `localhost`, a
loopback IP, or `host.docker.internal` for the Windows-host/Docker deployment
case. A configured value alone is not enough: HAI marks it usable only after a
live `/v1/models` probe, retains the EUR 0 paid budget, and continues to apply
the router's existing validation, fallback, audit, and approval controls.

`Ollama`, `LM Studio`, `LocalAI`, and `vLLM` use separate, first-class profile
patterns. Set
`LOCALAI_BASE_URL` / `LOCALAI_MODEL_ID` or `VLLM_BASE_URL` / `VLLM_MODEL_ID`
to point at an operator-installed endpoint. Both profiles reject non-local
endpoints, stay inactive until `/v1/models` succeeds, use the bounded
OpenAI-compatible completion contract, and remain subject to HAI's EUR 0,
local-first, validation, audit, and approval policy. HAI neither installs the
servers nor downloads or selects their models.

## Implemented mistral.rs boundary

HAI now includes an opt-in `mistral-rs` local provider profile. Set
`MISTRAL_RS_BASE_URL` and `MISTRAL_RS_MODEL_ID` after starting an
operator-reviewed loopback or `host.docker.internal` server. HAI rejects LAN
and public endpoints, requires a live `/v1/models` probe, and invokes only
`/v1/chat/completions` for approved local generation through the existing EUR
0 routing, fallback, audit, and approval policy. It never starts
`mistralrs`, downloads a model, or calls its UI, agent, shell, web, file, MCP,
Skills, or code-execution APIs.

## Implemented Evidently boundary

HAI now includes an opt-in internal Evidently runner under the Compose
`evaluation` profile. It accepts only 1-25 bounded `synthetic` or `redacted`
fixtures with opaque IDs, performs a deterministic sensitive-data gate before
any local request, and invokes a local offline Evidently DataSummary report.
The bridge returns only aggregate report metadata and a digest; it persists no
fixture text and cannot call a model provider, export telemetry, change routing
or policy, mark work verified, update memory, or execute an action. A report is
review evidence, never an automatic completion claim.

## Implemented Guardrails AI boundary

HAI now includes an opt-in internal Guardrails AI runner under the Compose
`validation` profile. It accepts one bounded, already-redacted
`action_proposal` JSON document and validates only the fixed Pydantic schema.
The bridge returns validation metadata and a digest, never the proposal text.
It cannot invoke an LLM, fetch a Guardrails Hub validator, retry a model,
persist data, alter HAI policy or routing, verify completion, approve, or
execute any action. A valid schema result remains a review signal only.

## Implemented LM Evaluation Harness boundary

HAI now includes an opt-in internal LM Evaluation Harness runner under the
Compose `model-evaluation` profile. It accepts no model, endpoint, task,
prompt, fixture, or command from the API. The operator configures exactly one
local OpenAI-compatible model endpoint in environment settings; the runner
executes only HAI's shipped six-case synthetic `hai_synthetic_v1` suite and
returns aggregate exact-match metadata plus a digest. It never writes samples,
downloads public datasets or benchmarks, retains task rows or raw generations,
exports telemetry, or changes model routing, budget, policy, verification,
memory, workflows, approvals, or execution. A score is review evidence, never
proof that a model is suitable for a real task.

## Implemented metrics boundary

HAI now includes an opt-in Prometheus exposition endpoint. Set
`HAI_PROMETHEUS_ENABLED=true` and a separate `HAI_PROMETHEUS_TOKEN`; only then
does `/metrics` exist. It requires a bearer token and exports HTTP request
counts and latency using route templates rather than raw paths. It also exports
bounded outcome-monitor sweep latency/result, discovered collection/composition
scope pressure, expired-lease recovery, processing outcome, retry, and last
successful-sweep signals. The outcome-monitor dimensions are fixed and do not
contain owner, workspace, target, source, prompt, record, or failure-detail
values. Discovered scope counts are capped by the scheduler limit and are not
represented as an exact global backlog. A Prometheus collector, retention,
dashboards, and alert rules are still separately configured and local by
default.

## Implemented LiteLLM boundary

LiteLLM is an optional, operator-installed local gateway profile in both HAI
model back-office registries. Set `LITELLM_ENABLED=true`,
`LITELLM_BASE_URL`, `LITELLM_MODEL_ID`, and a separate `LITELLM_API_KEY`.
HAI accepts only `localhost`, loopback, or `host.docker.internal`, probes
`/v1/models` with the virtual key, and sends the key only to that configured
gateway. HAI does not infer the gateway's upstream providers or billing from a
successful probe: each LiteLLM generation remains approval-gated and the EUR 0
policy remains authoritative.

## Implemented pgvector boundary

HAI now has an opt-in local semantic retrieval path using pgvector inside its
existing automation database. Set `HAI_SEMANTIC_RETRIEVAL_ENABLED=true` with a
loopback or `host.docker.internal` OpenAI-compatible embedding endpoint and a
named model. HAI indexes already-ingested source extractions and editable
context memories as they are created or updated, keeping source ownership/sensitivity and memory
owner/project/archive filters in their respective database queries. Both paths
use their existing keyword search when the semantic path is disabled, empty, or
unavailable. It never sends source or memory text to an arbitrary cloud URL and
does not create embeddings until the operator enables the feature.

## Implemented OR-Tools boundary

HAI now has an opt-in, internal-only OR-Tools CP-SAT planning service. The
`optimization` Compose profile is not started by default and exposes no host
port. HAI passes only opaque job IDs and bounded integer scheduling constraints
to it, validates the full response against the request, and saves an
owner-scoped proposal audit record. The service has no capability to apply a
proposal, call a tool, access sources, read files, or change a calendar or
workflow. Any implementation of a chosen proposal must use HAI's separate
approval and verification paths.

## Implemented Temporal durability boundary

HAI now has an opt-in local Temporal profile for one named, owner-scoped
workflow: a governed check of due open loops. It stores only an opaque HAI run
ID, scheduled time, and bounded limit in Temporal; ownership and all workflow
context remain in HAI's database. The activity may create HAI follow-up
proposals through the existing claim-aware workflow service. It cannot execute
tools or external actions, and it cannot approve or complete work. The exact
setup, route surface, and isolation boundary are documented in
[Temporal durability](temporal-durability.md).

## Implemented Playwright verification boundary

HAI now has an opt-in `verification` Compose profile backed by a local
Playwright worker. The only callable operation is a named, read-only check of a
configured local URL. The HAI API supplies no arbitrary URL or browser action;
the worker blocks requests and redirects outside its local origin allowlist,
does not retain state, and returns only a sanitized path, title, and bounded
pass/fail summary. Each run is approval-gated and owner-scoped. It is useful for
confirming a route is reachable, not for using an account or automating a site.
