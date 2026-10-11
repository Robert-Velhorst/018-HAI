# OSS Insight Collection Screening Ledger

## Scope and decision rule

This is the complete collection-level screening pass over the 138 collections
returned by the OSS Insight public API on 2026-07-19. The website grid still
reported 102 categories on that date, so the authenticated HAI catalog keeps
the API-only AI collection provenance separately instead of silently treating
the older website pagination as complete. It is deliberately not a bulk-import list:
the collections contain hundreds of repositories, many of which overlap with
HAI, introduce a second control plane, require a separate security review, or
have no role in a local-first personal operations system.

An entry is added to the authenticated HAI brain catalog only when it has a
clear control-plane role and a bounded activation path. A catalog profile is
never an installed dependency, a grant of credentials, or permission to run
code. The source catalog and the complete 138-category decision record are
recorded at `GET /api/v1/brain-catalog/`; the back office renders the latter
only in its expandable collection-coverage section.

## Shortlisted projects

| Project | Collection | Disposition | HAI role | Activation boundary |
| --- | --- | --- | --- | --- |
| LiteLLM | ai-gateways | Integrated profile | Keyed local provider gateway | Loopback-only profile; manual approval required because proxy upstream billing is not inferable. |
| llama.cpp | ChatGPT Alternatives | Integrated, opt-in | Local GGUF inference | Loopback endpoint, model provenance, and live health review required before HAI can route or generate. |
| pgvector | Vector Database & Vector Store | Integrated, opt-in | Local semantic retrieval in existing Postgres | Pinned Postgres 17 pgvector image; local-only embedding endpoint; source sensitivity and memory owner/project SQL filters; keyword fallback. |
| Temporal | Workflow Scheduler | Integrated, opt-in | Durable governed follow-up checks | One local worker runs a named proposal-only workflow; HAI owns approvals and completion. |
| Prometheus | Monitoring Tool | Integrated profile | Authenticated HTTP request telemetry | Opt-in token-protected exporter; local collector and retention configuration remain operator-managed. |
| MCP Inspector | Model Context Protocol (MCP) Client | Integrated profile | Local-only MCP preflight | HAI-owned initialize + tools/list review of configured local Streamable HTTP servers; no process spawn or tool execution. |
| Playwright | Testing Tools | Integrated, opt-in | Read-only local browser verification | Named local routes, origin allowlist, no secret capture, no interaction API, and approval gates. |
| Wasmtime | WebAssembly Runtime | Integrated, opt-in | Bounded WASI helper execution | Reviewed content-addressed modules only; no inherited network, filesystem, environment, or arguments; strict resource caps and approval gate. |
| OR-Tools | Optimization Solvers | Integrated, opt-in | Internal deterministic CP-SAT schedule proposals | Bounded opaque task inputs only; returns audited suggestions and deferred work without workflow, calendar, filesystem, tool, or external-network apply capability. |
| Ollama | LLM Inference Engines | Integrated, opt-in | Local model routing and live probe | Existing loopback provider, model-tag probe, persisted readiness, EUR 0 policy, and task approval gates remain authoritative. |
| FastMCP | MCP Servers | Integrated, opt-in | Authenticated local HAI read-context bridge | The `mcp-bridge` profile publishes exactly two bounded read-only workflow tools over loopback using distinct client and backend tokens. HAI retains sources, memory, approvals, execution, and audit; any write tool needs a separate review. |
| vLLM | LLM Inference Engines | Integrated, opt-in | Local high-throughput model serving | A loopback endpoint, explicit GPU/model limits, existing HAI provider probe, and EUR 0 routing policy are required. |
| DeepEval | AI Evaluation & Testing | Integrated, opt-in | Fixed local source-grounding regression | The isolated `deepeval-evaluation` runner pins DeepEval 4.1.1 and evaluates exactly three shipped synthetic evidence/answer pairs with `FaithfulnessMetric` through one configured local judge. It returns aggregate evaluator accuracy only and cannot receive real HAI sources, answers, prompts, metrics, or actions. |
| Microsoft Presidio | AI Safety & Alignment | Integrated, opt-in | Local sensitive-data detection and redaction | Explicit recognisers, confidence thresholds, false-positive review, source retention, and audit events are required; redaction cannot delete or hide authorised source evidence. |
| Guardrails AI | AI Safety & Alignment | Integrated, opt-in | Fixed-schema structured-proposal validation | The opt-in internal runner validates one bounded redacted `action_proposal` JSON document with no model call, Hub download, persistence, approval, or execution; a valid result remains review evidence only. |
| LM Evaluation Harness | AI Evaluation & Testing | Integrated, opt-in | Fixed six-case synthetic local model benchmark runner | The opt-in runner evaluates only one operator-configured local OpenAI-compatible model against HAI's shipped synthetic suite, returns aggregate metadata only, and cannot alter routing automatically. |
| OpenLLMetry | AI Observability | Candidate | Local LLM trace instrumentation | Local collector ownership, attribute allowlists, secret/prompt redaction, retention, sampling, export disablement, and health checks are required; telemetry cannot approve execution or spend. |
| Whylogs | AI Observability | Reference only | Compact data-quality profiling and constraint patterns | The latest public package release is from 2024 and overlaps the bounded Evidently evaluator. Do not install it; any future local-only review must disable documented anonymous analytics, preserve source/retention controls, and prevent profile output from changing facts, memory, routing, verification, or actions. |
| Langfuse | LLM DevTools | Integrated, opt-in | Local aggregate control-plane observability | The owner-only local bridge probes health/readiness and can explicitly export one fixed aggregate OTLP trace; trace redaction, retention, service credentials, and data-egress controls still require an operator review. |
| Promptfoo | LLM DevTools | Integrated, opt-in | Fixed synthetic local safety regression | The opt-in `safety-evaluation` runner uses one reviewed local model endpoint and a shipped six-case suite. It returns aggregate metadata only; no provider, prompt, source, test data, routing, policy, or action is caller-controlled. |
| Airbyte | Data Integration | Integrated, opt-in | Local source and connection inventory | The local-only `airbyte-inventory` connector reads a single bounded metadata page for fixed allowlisted workspaces. It excludes credentials, connector configuration, selected fields, source records, sync results, and every Airbyte mutation or sync-control action. |
| Odoo | Business Management | Integrated, opt-in | Read-only Odoo JSON-2 source adapter | HAI can call only `search_read` for fixed operator-approved models and fields from one named instance. Any write, customer, financial, or accounting action remains independently approval-gated. |
| browser-use | AI Browser Agents | Candidate | Reviewed browser-agent adapter | Named browser profile, origin/download/upload/credential allowlists, read-only-first validation, and separate high-risk action approvals are required. |
| NVIDIA NeMo Guardrails | AI Safety & Alignment | Candidate | Local dialog-guardrail adapter | Policy ownership, redacted fixtures, false-positive review, audit records, and no-write evaluation boundaries are mandatory. |
| garak | AI Red Teaming | Integrated, opt-in | Fixed local prompt-injection regression | The isolated `garak-evaluation` runner pins Garak 0.15.1, calls one configured local model with one four-case synthetic PromptInject probe, returns aggregate metadata only, clears inherited provider credentials/proxies, and deletes raw reports. It cannot target HAI or change its controls. |
| whisper.cpp | Multimodal AI | Integrated, opt-in | Local speech-to-text intake | The opt-in local-transcription runner reads only an explicit owner-selected subfolder with a manually provisioned local model, produces source-linked uncertain transcripts, and has no microphone, cloud upload, automatic scan, raw-audio retention, or action path. |
| A2A Protocol | A2A Protocol | Integrated, opt-in | Controlled-planning bridge; local by default, governed fixed-ngrok optional | The A2A 1.0-shaped Agent Card and authenticated `SendMessage` profile produce a bounded side-effect-free planning artifact for one named owner. Public transport is restricted to an explicitly enabled production fixed-ngrok origin with local-login bypass disabled. It requires `A2A-Version: 1.0` and accepts only standalone `ROLE_USER` text. It is not a full task-lifecycle server: no polling, task persistence, peer discovery, outbound remote-agent invocation, source refresh, approval, execution, streaming, file input, or HAI context disclosure. |
| Tabby | AI Coding Assistants | Candidate | Self-hosted coding assistance | Local deployment, model/privacy review, workspace scope, and read-only-first review are required. |
| Cline | LLM DevTools | Candidate | Review-first interactive coding assistance | Explicit model provider, workspace, tool, network, audit, and approval boundary required before any HAI bridge. |
| OpenCode | Model Context Protocol (MCP) Client | Candidate | Review-first terminal coding assistance | Explicit model provider, workspace, tool, network, audit, and approval boundary required before any HAI bridge. |
| OpenCode (opencode-ai legacy) | Model Context Protocol (MCP) Client | Excluded | Archived same-name terminal agent | This distinct archived upstream cannot inherit the active anomalyco/opencode candidate's review status, workspace, provider, credential, MCP, or runtime permissions. |
| Continue, OpenHands, CrewAI, Aider | AI Agent Frameworks / LLM DevTools / MCP | Candidate | Reviewed coding and orchestration profiles | Existing catalog controls apply; no generic agent-execution endpoint. |
| AutoGen | AI Agent Frameworks | Compatibility only | Migration and protocol translation | Dedicated bridge and approval required; no new foundation work. |
| Activepieces | Zapier Alternatives | Reference only | Connector and workflow-pattern research | No second automation control plane by default. |
| Mem0 | LLM Tools | Reference only | Memory-consolidation reference | HAI remains the sole memory/provenance authority. |
| Letta, ComfyUI | Agent Memory / Image Generation | Reference only | Design and workflow references | HAI does not create a second memory authority or autonomous publication workflow without an explicit architecture decision. |
| Daytona | Agent Sandboxing | Excluded | Public upstream no longer maintained | The public repository states that core development moved private in June 2026. HAI must not install, connect, or recommend a discontinued, account/API-key based external sandbox. |
| OpenMetadata | Open Source Data Catalogs | Reference only | Data lineage and governance reference | Too large for current local-first source registry. |
| LangChain, LlamaIndex, Cognee, Microsoft GraphRAG, Haystack, Qdrant, Grafana | Agent / GraphRAG / Vector / Monitoring | Reference only | Pattern or future scale option | Revisit only after a measured native gap; HAI keeps one source-linked memory and retrieval authority. |
| n8n | Zapier Alternatives | License review | Workflow-platform comparison | Sustainable Use License and architecture overlap need a decision first. |
| MinIO | Distributed File Storage | Excluded | Storage reference only | Archived upstream and AGPLv3 do not meet the current adoption bar. |

## Upstream verification snapshot

On 2026-07-19, the GitHub repository API was checked for the active profiles
and highest-value candidates: LiteLLM, llama.cpp, pgvector, Temporal,
Prometheus, MCP Inspector, Playwright, Wasmtime, OR-Tools, Continue, Cline, OpenCode,
OpenHands, CrewAI, Aider, AutoGen, AutoGPT, Mem0, OpenMetadata, and n8n. All
20 repositories reported `archived=false` at that time. This is a maintenance
signal, not an adoption grant: HAI still requires the per-profile configuration,
health, approval, audit, rollback, license, and data-egress gates described
above.

GitHub's API reported `NOASSERTION` for several repositories where a licence
could not be reliably represented as an SPDX field. HAI treats that result as
"review the upstream licence files" rather than assuming an open-source
licence. In particular, it does not relax the existing AutoGPT and n8n licence
review states.

The 2026-07-19 follow-up check covered Ollama, browser-use, NVIDIA NeMo
Guardrails, garak, whisper.cpp, A2A, Tabby, Letta, ComfyUI, Daytona, FastMCP,
vLLM, DeepEval, Microsoft Presidio, Guardrails AI, LM Evaluation Harness,
OpenLLMetry, Microsoft GraphRAG, and Haystack. All 19 reported `archived=false`.
The check confirmed
Apache-2.0 metadata for FastMCP, vLLM, DeepEval, garak, A2A, and Letta; MIT for
Ollama, browser-use, and whisper.cpp; GPL-3.0 for ComfyUI; and `NOASSERTION`
for NeMo Guardrails, Tabby, and Daytona. A separate metadata check found the
LLM Guard project archived, so it is not admitted as a profile. A 2026-07-20
upstream review then found that Daytona's public repository is no longer
maintained and its core moved private; Daytona is therefore excluded rather
than retained as a sandbox reference. HAI keeps every new runtime-capable
project review-first, and holds licence-sensitive or external-sandbox
candidates as references only where their upstream remains suitable.

The same-day expansion also checked the next operationally relevant OSS Insight
repositories. GitHub MCP Server, Playwright MCP, Gen AI Toolbox, Qodo PR-Agent,
SWE-agent, OpenLIT, and LangMem reported `archived=false`; their GitHub API
licences were MIT, Apache-2.0, Apache-2.0, MIT, MIT, Apache-2.0, and MIT
respectively. They are catalogued only as review-first candidates or a memory
reference. A 2026 follow-up found that SWE-agent now recommends its maintained
mini-SWE-agent successor: HAI holds SWE-agent as a reference and makes
mini-SWE-agent the only review-first disposable-worktree candidate. No MCP
service, token, browser profile, database connection, code worker, collector,
or memory store was installed by this screening. Arize Phoenix reported
`NOASSERTION`, so it remains under licence review. PyRIT and TaskWeaver
reported `archived=true`, so HAI records them as excluded rather than making
them selectable capabilities.

AnythingLLM was separately confirmed in the same OSS Insight RAG Frameworks
repository list. Its GitHub metadata reported `archived=false`, an active
`master` branch, and MIT on 2026-07-19. On 2026-07-20, HAI added a
disabled-by-default local vector-search bridge for a fixed workspace allowlist.
It remains neither a parallel HAI memory/source/verification authority nor an
AnythingLLM deployment: no HAI process installs or starts it, and the bridge
cannot call chat, history, agent, tool, attachment, or mutation endpoints.

On 2026-07-20, the current FastMCP upstream was checked directly at
`jlowin/fastmcp`: the current package was 3.4.4 and the repository carries an
Apache-2.0 licence. HAI's `mcp-bridge` profile is therefore an explicit,
loopback-only, read-only integration, not a generic MCP executor. The same
review found CloudQuery maintained, but its current public upstream did not
contain a GitHub source plugin. On 2026-07-20, HAI added a disabled-by-default
local JSONL summary reader instead of claiming a CloudQuery GitHub connector:
it consumes only completed records emitted by an operator-run `cloudquery sync
--summary-location` command. HAI never starts CloudQuery, reads its
configuration or credentials, or accesses raw source/destination data.

The same 2026-07-20 review confirmed the current A2A project at
`a2aproject/A2A`, hosted by the Linux Foundation and Apache-2.0 licensed. HAI
implements only a local, bearer-authenticated, A2A 1.0-shaped `SendMessage`
planning profile and a minimal Agent Card. It requires `A2A-Version: 1.0` and
does not claim full A2A task-lifecycle conformance: peer discovery, remote
agents, task persistence/polling, streaming, push notifications, file input,
and the protocol as a path around HAI's workflow, approval, execution,
verification, or audit controls remain unavailable.

## Complete collection screen

Each listed collection was classified by its suitability for HAI's thinking,
memory, operations, execution, verification, or governance planes. "No direct
adoption" means the collection is either out of scope, already covered by the
existing stack, too broad to introduce without a demonstrated gap, or requires
a separate project-level review.

| Page | Collections screened | Outcome |
| --- | --- | --- |
| 1 | ai-gateways; Mocking and Stubbing tools; Documentation Generator; Anomaly Detection Software; BaaS; Model Context Protocol (MCP) Client; AI Agent Frameworks; AI Training Observability; Project Management; GraphRAG - Knowledge Graph based RAG; Vector Database & Vector Store; 3D Physics Engines | Added gateway, MCP, agent, retrieval profiles; project-management/GraphRAG remain reference patterns; test tooling is covered by the existing verification path; the rest has no direct adoption. |
| 2 | Browser Extension Frameworks; Go Logging Libraries; Go Web Frameworks; Relational Database; WebRTC; LLM DevTools; Reactive Monolith Frameworks; Open Source Data Catalogs; ML in Rust; Programming Language of China; Web Scanner; Cloud Financial Management and Resource Optimization | Added coding-tool and data-governance references. Browser extensions, web scanners, and framework replacements are not adopted; financial-optimisation tools need a separate finance scope. |
| 3 | Networking for Games; Stable Diffusion Ecosystem; ChatGPT Apps; Vector Search Engine; LLM Tools; ChatGPT Alternatives; Zapier Alternatives; Cpp CLI Parsing; Business Management; Ansible DevTools; Approximate Nearest Neighbor Library; Optimization Solvers | Added llama.cpp, OR-Tools, workflow-platform dispositions, and memory reference. Image, business-suite, and infrastructure tooling remain separate optional projects. |
| 4 | X as Code; Robotics; Virtual Reality; javascript ORM; Javascript Build Tool; Kubernetes Tooling; Serverless Framework; Slack Alternative; iOS Framework; Key Value Database; MLOps Tools; Workflow Scheduler | Added Temporal. Infrastructure-as-code, Kubernetes, serverless, chat, mobile, and robotics are not HAI brain dependencies; Redis remains existing infrastructure rather than a new brain store. |
| 5 | Data Integration; Password Manager; Monitoring Tool; Configuration Management Tools; Golang ORM; Security Tool; Open Source Forum Software; Computer Science Courses; UI Framework and UIkit; Terminal; TUI Framework; Modern Data Stack | Added Prometheus/Grafana dispositions and an opt-in Airbyte source/connection inventory adapter. Password/security tools are not imported due to credential and attack-surface concerns; data integration remains bounded by HAI connector controls. |
| 6 | Go Database; Rust Database; Segment Alternative; API tool for developer; Hyperledger Fabric; Hyperledger Besu; Hyperledger Foundation; WYSIWYG Editor; PaaS; Diagram as Code; Identity Server; Message and Streaming | No direct adoption. HAI already has Postgres, bundled identity, and message infrastructure. Blockchain, PaaS, and editor frameworks do not power the HAI brain. |
| 7 | Web3; Finance; Cross Platform GUI Tool; Remote Desktop Tool; Testing Tools; WebAssembly Runtime; Distributed File Storage; Programming Language; Javascript Charting; CICD; React Framework; APM Tool | Added Playwright and Wasmtime. Remote desktop, Web3, finance, and CI/CD execution are deliberately out of scope; MinIO is excluded; APM remains Prometheus-first. |
| 8 | Chaos Engineering; Search Engine; Text Editor; Javascript Game Engine; Game Engine; Headless CMS; Artificial Intelligence; Github Alternative; Graph Database; Time Series Database; Business Intelligence; Javascript Framework | No direct adoption. Search/graph/time-series options stay deferred behind pgvector/Prometheus; AI libraries require a concrete model-serving or evaluation gap; development/UI ecosystems do not replace HAI's stack. |
| 9 | Web Framework; Low Code Development Tool; Google Analytics Alternative; CSS Framework; Open Source Database; Static Site Generator | No direct adoption. Low-code offerings overlap with HAI workflow ownership; analytics/CSS/framework/database alternatives cannot be introduced without a measured migration case. |
| API-only AI snapshot A | MCP Servers; Coding Agents; Vibe Coding Tools; RAG Frameworks; LLM Inference Engines; LLM Fine-Tuning Tools; AI Image Generation; AI Coding Assistants; AI Browser Agents; AI Agent Memory; LLM Gateway & Proxy; AI Safety & Alignment | Added FastMCP review, local inference, coding, browser, safety, and memory dispositions. Fine-tuning and unbounded image-generation workflows are not adopted. |
| API-only AI snapshot B | Vector Databases; Multimodal AI; AI Evaluation & Testing; Model Compression; AI Video Generation; AI Workflow Orchestration; Agent Skills & AGENTS.md; AI Infrastructure; Edge AI; AI Governance; Google ADK; Neuro-Symbolic AI | Added local transcription, DeepEval review, and protocol references. HAI retains one retrieval, workflow, policy, and control plane. |
| API-only AI snapshot C | AI FinOps; Synthetic Data; AI Quantitative Finance; AI Agent Marketplace; Knowledge Graphs for AI; AI Observability; AI Code Review; Agent Sandboxing; AI Red Teaming; A2A Protocol; Google ADK Python; Agent Harness | Added FinOps, observability, code-review, sandbox, red-team, and interoperability decisions. Finance, marketplaces, and uncontrolled remote peers are not adopted. |

## Adoption gates

1. Upstream availability, license, maintenance, and project fit are checked before a catalog entry is added.
2. A candidate must have a concrete adapter contract, owner, local configuration, health check, audit event, failure behaviour, and rollback path before it can do work.
3. Recommendation is not activation: task planning exposes the option, while tool routing reports it as unavailable until its reviewed adapter exists.
4. Runtime-capable candidates stay behind workspace, network, tool, and approval allowlists. Browser automation, local execution, sending, publishing, deletion, credential use, and financial actions never inherit permission from the catalog.
5. HAI does not adopt another product as a second memory, automation, source, or policy authority without an explicit architecture decision.

## Re-screen triggers

Revisit an entry when its license or maintenance changes, a real HAI metric demonstrates a capability gap, or a proposed adapter has a complete safety and rollback design. Re-run this collection-level screen whenever the public API collection count or repository rankings change, before treating newly surfaced OSS Insight projects as adoption candidates.

## Bounded discovery queue

The Brain Catalog's admin-only OSS Insight discovery action first reads the
complete public collection index and compares it with HAI's 138-category
screening snapshot. It provides two deliberately bounded repository-name scans:

1. `candidate` reads only categories classified as `review_candidate`.
2. `reviewable` reads those categories plus `represented_in_catalog` categories,
   so HAI can surface complementary or replacement upstreams for capabilities
   it already profiles.

Neither scan queries `reference_only` or `not_adopted` categories. The result
removes already catalogued upstream repositories, caps the returned shortlist,
records each discovery's collection disposition, keeps separate short-lived
caches for the two scopes, and exposes source links, collection rationale,
missing categories, and unavailable category reads. The complete remote pass
has one total deadline, so a slow source cannot become an unbounded dashboard
operation.

The catalogue also maintains a small, explicit list of source-reviewed GitHub
repository aliases for upstream transfers. A known historic or renamed slug is
suppressed only when it is recorded against the same HAI profile; it does not
change that profile's status, activation requirements, permissions, or runtime
state. Similar names and unreviewed forks remain discoveries, avoiding both
duplicate reviews and unsafe automatic equivalence.

Discovery is deliberately non-mutating. It does not clone code, download
packages, add a catalog record, create credentials, configure a runtime, call a
tool, or execute a repository. An owner can only turn one discovery into a
manual HAI pursuit. That pursuit requires separate upstream, licence, local
deployment, health, audit, rollback, data-egress, and approval review before an
adapter can be implemented.

The discovery revalidation endpoint accepts the same source scope and rejects
any repository that is absent from that fresh scoped report. This prevents the
metadata checker from being repurposed as an arbitrary GitHub request proxy.

## Metadata readiness assessment

When an owner rechecks a fixed catalog entry or a repository returned by the
bounded discovery queue, HAI reports a readiness state alongside the raw GitHub
metadata. The state is a review-ordering aid, not a change to the catalog and
never an authorisation to install or run code:

| Readiness | Meaning |
| --- | --- |
| `review_now` | The upstream is available, not archived, and reports a licence that does not trigger HAI's current licence-hold rules. A human may start a narrow adapter review. |
| `license_review` | The project is already held for licensing, reports no SPDX licence / `NOASSERTION`, or reports a licence requiring an explicit architecture and legal review. |
| `archived` | The upstream reports `archived=true`; active adoption work must stop unless a human records an exception. |
| `upstream_unavailable` | GitHub did not confirm the fixed repository. HAI cannot prioritize an adapter review from missing metadata. |
| `reference_only` / `not_adopted` | The existing collection/catalog disposition remains authoritative. Metadata does not reopen a reference-only or excluded project. |
| `profile_review` | An integrated HAI profile still needs its own local configuration and live readiness check. |

Every readiness response repeats the required adapter gates: owner approval,
local deployment and data-egress design, health/audit/rollback/no-op validation,
and existing approval policy for consequential actions. These gates remain in
force even for `review_now` candidates.

## Capability recommendation

The Brain Catalog also provides a read-only capability recommendation endpoint
for task planning. It ranks already reviewed catalog entries against a specific
need using the entry name, category, capabilities, and intended use. Entries
held for licensing, excluded from adoption, or retained only as references are
not recommended.

The result explains its match, readiness, and the next review step. It neither
queries an upstream repository nor changes catalog, runtime, provider, or task
state. In particular, a recommendation never installs a package, configures a
service, creates credentials, or executes a tool. An integrated profile still
requires its own local health and configuration checks; a candidate requires a
manual adapter review under the adoption gates above.

Task planning consumes the same ranked matches as planning context and returns
them with the tool-routing decision. This makes a relevant reviewed capability
visible to the operator without selecting it as a runnable tool. Candidate
matches remain skipped until an adapter has been reviewed and configured;
compatibility profiles remain blocked behind an explicit bridge and approval;
and integrated profiles remain skipped until local configuration and live
health evidence are present.

Every ranked match carries the reviewed upstream URL, its OSS Insight source
record, source collection, verification date, and verification note. The
operator can therefore inspect the evidence from either the Brain Catalog or a
task plan before starting any separate adapter review.

## Adoption roadmap

`GET /api/v1/brain-catalog/adoption-plan` returns the read-only implementation
queue for integrated profiles, review-first candidates, and compatibility
bridges. It ranks work using local-first suitability and HAI capability-plane
coverage so an unserved plane is visible before another overlapping tool is
considered. Held, excluded, licence-review, and reference-only entries are not
included.

The endpoint never calls an upstream, installs a package, creates credentials,
changes provider or runtime configuration, approves work, or executes a tool.
Each item repeats its required gates and its recommended next review action;
the existing catalog inspector remains the path to inspect provenance and open
a manual adapter-review pursuit.

Task planning uses the same roadmap as context. A requested capability is
ranked by direct task relevance first; roadmap priority breaks ties and carries
the capability-plane and gate rationale into the plan. This does not select,
configure, or execute an external tool. The task router continues to mark
catalog candidates as unavailable until their separate adapter review is
approved and a live adapter reports healthy.

The matching vocabulary applies a small deterministic expansion for common
operational terms, for example `LLM` to `model` and `inference`, or `PII` to
`sensitive` and `redaction`. Singular and plural variants are normalized. The
expanded terms are returned to the operator, so this matching aid is
inspectable and does not represent an unlogged model inference or an upstream
search. Specific terms written by the operator rank above generic or expanded
terms, preventing a broad phrase such as "local model" from displacing an
explicit objective such as "benchmark".

## HAI capability planes

The authenticated back office maps every reviewed catalog entry to one or more
HAI-owned planes: thinking and planning, memory and knowledge, source intake,
operations, controlled execution, verification and safety, governance and
boundaries, or observability. The map is deliberately architectural rather
than an installation inventory. It shows where a project may contribute after
review, while its catalog status still determines whether it is integrated,
review-first, held, reference-only, or excluded.

The plane mapping has a test that rejects an unclassified catalog entry or a
plane reference to a missing entry. This keeps future OSS Insight additions
visible in an operational layer instead of turning the catalog into an
unstructured list of repositories.

## Live candidate-gap pass

On 2026-07-19, HAI compared the repository names returned by every
`review_candidate` OSS Insight collection with the reviewed catalog, then
checked GitHub metadata for the capability gaps below. The list records a
decision, not a package installation or an endorsement of every upstream in a
high-ranking collection.

| Repository | Capability gap | Decision | Boundary |
| --- | --- | --- | --- |
| `pydantic/pydantic-ai` | Typed planning and schema-constrained output | Integrated, opt-in local proposal runner | `pydantic-ai-slim[openai]` 2.13.0 runs only behind a fixed local schema, loopback-model allowlist, bounded request/response limits, and no-tool/no-persistence boundary. HAI retains schemas, validation, provider policy, audit, and approvals. |
| `qodo-ai/pr-agent` -> `The-PR-Agent/pr-agent` | Pull-request review patterns | Licence review | The repository redirects to a community-maintained legacy AGPL-3.0 project with CLI, Action, and webhook publishing paths. HAI installs nothing; any use requires explicit licence, model-egress, repository-scope, redaction, retention, and no-publish review. |
| `mudler/LocalAI` | Alternative local OpenAI-compatible model serving | Review-first candidate | Loopback only, approved model provenance, no automatic model download or paid routing. |
| `cloudquery/cloudquery` | Read-first incremental source inventory | Integrated, opt-in local summary reader | Fixed local JSONL summary path only, bounded completed rows and incremental cursoring; CloudQuery credentials/configuration, raw source data, destinations, and process execution remain outside HAI. |
| `comet-ml/opik` | Local trace and evaluation evidence | Review-first candidate | Redacted local traces, retention/export controls, no audit-authority replacement. |
| `confident-ai/deepteam` | No-write agent red-team regression | Integrated, opt-in synthetic local runner | Pinned DeepTeam 1.0.7 evaluates one deterministic synthetic HAI refusal target against two fixed vulnerability types and one bounded prompt-injection method. Assessment upload is disabled; HAI accepts only aggregate metadata. It never targets real HAI behavior, sources, accounts, runtimes, or actions. |
| `confident-ai/deepeval` | Source-grounded answer evaluation | Integrated, opt-in synthetic local runner | Pinned DeepEval 4.1.1 evaluates only three shipped synthetic evidence/answer fixtures through `FaithfulnessMetric` and one configured local judge. The runner returns aggregate metadata only; it cannot receive real HAI answers, sources, prompts, metrics, routing, policy, verification, or action data. |
| `Fission-AI/OpenSpec` | Spec-first coding plans | Integrated, opt-in local artifact reader | Selected local `openspec/changes` artifacts only; groups proposal/design/tasks/spec Markdown into reviewable source bundles and never invokes OpenSpec, reads repository code outside that tree, or edits/commits/branches/opens pulls. |
| `pipecat-ai/pipecat` | Consentful local voice or multimodal intake | Review-first candidate | Opt-in capture, provenance, pause, retention, and no action from speech without HAI approval. |
| `protectai/llm-guard` | LLM security filtering | Excluded | GitHub metadata reports the upstream as archived. |
| `openai/evals` | LLM evaluation framework | Licence review | GitHub metadata reports `NOASSERTION`; provider and dependency review is also required. |
| `THUDM/AgentBench` | Agent benchmark taxonomy | Reference only | Use only to inform HAI-native, redacted evaluation fixtures. |
| `microsoft/OmniParser` | GUI screen-understanding patterns | Licence review | CC-BY-4.0, screenshot privacy, weights, and retention require separate review. |
| `modelcontextprotocol/servers` | MCP examples collection | Licence review | The collection is not a trust bundle; each server needs an individual review. |

The live comparison also found many overlapping coding agents, hosted
workspaces, duplicate RAG systems, broad browser agents, and business suites.
HAI keeps those as unreviewed discovery records or category-level non-adoption
decisions unless a measured product gap justifies a narrow adapter design.
