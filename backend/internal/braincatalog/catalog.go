// Package braincatalog owns HAI's curated view of external agent projects.
//
// It is deliberately a catalog, not a package manager: HAI must not download,
// install, or execute third-party agent code merely because it appears in an
// awesome-list. Entries become usable only through a reviewed adapter and the
// existing approval-gated runtime path.
package braincatalog

import "strings"

// Status describes HAI's adoption decision for an external project.
type Status string

const (
	StatusCandidate     Status = "candidate"
	StatusIntegrated    Status = "integrated_profile"
	StatusCompatibility Status = "compatibility_only"
	StatusReferenceOnly Status = "reference_only"
	StatusExcluded      Status = "excluded"
	StatusLicenseReview Status = "license_review"
)

// ControlMapping records how an upstream pattern is translated into an
// HAI-owned control. A recommendation therefore never delegates safety,
// policy, or execution authority to a third-party project.
type ControlMapping struct {
	SourcePattern string `json:"sourcePattern"`
	HAIControl    string `json:"haiControl"`
	Boundary      string `json:"boundary"`
}

// ImplementationBoundary is the concrete HAI-owned surface behind an
// integrated catalog profile. It is intentionally not an upstream deployment
// claim: the route can still be disabled, configuration-gated, or health-only.
// Keeping this evidence next to the catalog prevents an "integrated" label
// from drifting into a capability that has no local implementation boundary.
type ImplementationBoundary struct {
	Control    string `json:"control"`
	Route      string `json:"route"`
	SourcePath string `json:"sourcePath"`
	Scope      string `json:"scope"`
}

// Entry is a transparent, source-backed integration decision. Verification is
// a curation snapshot rather than a claim that a runtime has been installed.
type Entry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	UpstreamURL string `json:"upstreamUrl"`
	// RepositoryAliases are reviewed historic or transferred GitHub slugs for
	// this same upstream. They are discovery de-duplication hints only: an
	// alias never expands the profile's scope, changes its status, or starts a
	// runtime.
	RepositoryAliases    []string                `json:"repositoryAliases,omitempty"`
	SourceCatalogURL     string                  `json:"sourceCatalogUrl"`
	SourceCollection     string                  `json:"sourceCollection,omitempty"`
	Status               Status                  `json:"status"`
	Category             string                  `json:"category"`
	IntegrationMode      string                  `json:"integrationMode"`
	Capabilities         []string                `json:"capabilities"`
	RecommendedFor       []string                `json:"recommendedFor"`
	RequiresApproval     bool                    `json:"requiresApproval"`
	LocalFirstCompatible bool                    `json:"localFirstCompatible"`
	Activation           string                  `json:"activation"`
	Rationale            string                  `json:"rationale"`
	VerifiedAt           string                  `json:"verifiedAt"`
	VerificationNote     string                  `json:"verificationNote"`
	ControlMappings      []ControlMapping        `json:"controlMappings,omitempty"`
	Implementation       *ImplementationBoundary `json:"implementation,omitempty"`
}

// Recommendation makes a candidate visible to the planner without claiming it
// is installed or selected for execution.
type Recommendation struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Status           Status           `json:"status"`
	Role             string           `json:"role"`
	Rationale        string           `json:"rationale"`
	RequiresApproval bool             `json:"requiresApproval"`
	Activation       string           `json:"activation"`
	ControlMappings  []ControlMapping `json:"controlMappings,omitempty"`
}

// UpstreamReview is a point-in-time public metadata check for a catalog entry.
// It deliberately does not change HAI's adoption status: an upstream being
// available is neither an approval nor proof that its adapter is safe.
type UpstreamReview struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	UpstreamURL         string   `json:"upstreamUrl"`
	ResolvedRepository  string   `json:"resolvedRepository,omitempty"`
	ResolvedUpstreamURL string   `json:"resolvedUpstreamUrl,omitempty"`
	RepositoryMoved     bool     `json:"repositoryMoved"`
	CheckedAt           string   `json:"checkedAt"`
	Available           bool     `json:"available"`
	Archived            bool     `json:"archived"`
	License             string   `json:"license,omitempty"`
	DefaultBranch       string   `json:"defaultBranch,omitempty"`
	PushedAt            string   `json:"pushedAt,omitempty"`
	Message             string   `json:"message"`
	Disposition         Status   `json:"disposition"`
	Readiness           string   `json:"readiness"`
	ReadinessReason     string   `json:"readinessReason"`
	RequiredGates       []string `json:"requiredGates,omitempty"`
}

const sourceCatalogURL = "https://github.com/e2b-dev/awesome-ai-agents"

// verifiedAt is the most recent successful check of the OSS Insight collection
// index. Individual profile entries retain their own upstream verification
// dates because collection membership does not prove repository readiness.
const verifiedAt = "2026-07-21"

var discoverySources = []CatalogSource{
	{Name: "Awesome AI Agents", URL: sourceCatalogURL, Scope: "external agent projects"},
	{Name: "OSS Insight", URL: "https://ossinsight.io/collections", Scope: "curated repository collections"},
}

// CatalogSource records a discovery index. Discovery records are evidence for
// curation, not an installation, endorsement, or runtime trust decision.
type CatalogSource struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Scope string `json:"scope"`
}

// DiscoverySources returns a copy so API callers cannot modify the registry.
func DiscoverySources() []CatalogSource {
	return append([]CatalogSource(nil), discoverySources...)
}

var entries = []Entry{
	{
		ID: "source-linked-knowledge-graph", Name: "HAI source-linked knowledge graph", UpstreamURL: "https://github.com/microsoft/graphrag", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10134/repos/", SourceCollection: "Knowledge Graphs for AI",
		Status: StatusIntegrated, Category: "deterministic source-provenance graph", IntegrationMode: "read-only candidate graph and timeline view",
		Capabilities: []string{"entity co-occurrence view", "date candidate timeline", "source-linked inspection", "owner-scoped context"}, RecommendedFor: []string{"connected-source triage", "case timeline review", "evidence navigation", "project-context inspection"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "No external runtime is required. HAI reads only owner-visible source extractions and derives bounded candidate entities, co-occurrence links, and date hints. Sensitive extractions are excluded by default. The result is read-only and cannot update memory, support a claim, create a workflow, select a model, or trigger an action.",
		Rationale:  "Knowledge-graph repositories demonstrate useful navigation patterns, but a second graph database would duplicate HAI's source and memory authorities before a measured scale need exists. This native view provides provenance-first inspection without importing an agent or graph runtime.",
		VerifiedAt: "2026-07-20", VerificationNote: "HAI implements a deterministic source-extraction view only. Microsoft GraphRAG is retained as an upstream architecture reference; HAI does not vendor, install, configure, query, or execute GraphRAG code, models, storage, or prompts.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "entity and relationship graph", HAIControl: "owner-scoped source extractions with source references", Boundary: "co-occurrence is labelled candidate-only and never establishes a real-world relationship"},
			{SourcePattern: "temporal graph context", HAIControl: "candidate date timeline", Boundary: "relative dates remain unparsed and no event may create a reminder, workflow, or action"},
			{SourcePattern: "graph-enhanced retrieval", HAIControl: "grounded-answer evidence verification", Boundary: "graph output cannot become evidence, memory, claim support, or execution input"},
		},
	},
	{
		ID: "anythingllm", Name: "AnythingLLM", UpstreamURL: "https://github.com/Mintplex-Labs/anything-llm", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10108/repos/", SourceCollection: "RAG Frameworks",
		Status: StatusIntegrated, Category: "local RAG workspace evidence adapter", IntegrationMode: "bounded local vector-search bridge",
		Capabilities: []string{"document workspaces", "RAG retrieval", "agent workspace patterns", "local-model connections"}, RecommendedFor: []string{"approved document workspaces", "RAG adapter evaluation", "local research preparation"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Configure one operator-managed local endpoint, a dedicated API key, fixed workspace slugs, and explicit local-embedding confirmation. HAI calls only workspace vector search; it cannot open chat, send attachments, read history, ingest/delete documents, change settings, or execute AnythingLLM agents/tools. Returned chunks remain unverified candidate evidence.",
		Rationale:  "AnythingLLM can complement HAI's source-grounded research flow with bounded retrieval from curated local workspaces, while HAI keeps project memory, verification, provider policy, and approvals authoritative.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official upstream API source reviewed on 2026-07-20: the authenticated workspace vector-search endpoint returns chunk text and metadata without using chat/history endpoints. HAI implements only this disabled-by-default, allowlisted local bridge. No AnythingLLM workspace, connector, model, or agent is installed or configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "document workspace", HAIControl: "source registry, provenance links, and memory review", Boundary: "imports do not become HAI memory facts without source support or confirmation"},
			{SourcePattern: "workspace agent", HAIControl: "task planner and approval-gated runtime adapters", Boundary: "no workspace agent can self-authorize tools or external effects"},
			{SourcePattern: "workspace vector search", HAIControl: "grounded answer candidate-evidence selection", Boundary: "only explicitly configured local workspaces are queried; no chat, history, attachment, or mutation routes are called"},
		},
	},
	{
		ID: "github-mcp-server", Name: "GitHub MCP Server", UpstreamURL: "https://github.com/github/github-mcp-server", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10105/repos/", SourceCollection: "MCP Servers",
		Status: StatusIntegrated, Category: "scoped GitHub repository context", IntegrationMode: "disabled-by-default local read-only GitHub MCP-compatible context profile",
		Capabilities: []string{"repository inspection", "issue and pull-request operations", "GitHub tool schemas"}, RecommendedFor: []string{"repository context", "issue triage", "pull-request review"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Enable HAI's local FastMCP bridge with two separate local-only tokens and one owner identity. The native GitHub source connector remains read-only and supplies at most eight repository slugs with project and sync freshness. A separately installed official GitHub MCP server is still preflight-only and needs its own minimum-scope credential review. Write, merge, label, comment, or workflow actions remain separate HAI approvals.",
		Rationale:  "The maintained official GitHub MCP server is useful evidence for repository-context interoperability. HAI now exposes its existing owner-scoped GitHub source configuration through a narrowly bounded local FastMCP tool while retaining repository scope, credentials, source content, write policy, and final execution authority in HAI's connector and approval layers.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight MCP Servers repository list and GitHub metadata checked on 2026-07-19: active main branch, MIT licence. On 2026-07-21 HAI added a disabled-by-default local FastMCP GitHub repository-context tool backed only by its owner-scoped native GitHub source registry. It returns repository slug, project key, state, sync frequency, and last-sync time only; it does not install or start GitHub MCP, send GitHub credentials, call upstream tools, or disclose repository content.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "GitHub repository context", HAIControl: "owner-scoped GitHub source registry and local FastMCP bridge", Boundary: "only repository slug, project key, status, sync frequency, and last-sync time are returned; source content and credentials stay inside HAI"},
			{SourcePattern: "GitHub MCP tools", HAIControl: "MCP preflight plus GitHub connector scopes, audit events, and approval queue", Boundary: "catalog discovery never creates credentials or grants repository access"},
			{SourcePattern: "repository write operations", HAIControl: "risk policy and per-action confirmation", Boundary: "writes, comments, merges, and workflow dispatches stay approval-gated"},
		},
	},
	{
		ID: "playwright-mcp", Name: "Playwright MCP", UpstreamURL: "https://github.com/microsoft/playwright-mcp", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10105/repos/", SourceCollection: "MCP Servers",
		Status: StatusCandidate, Category: "controlled browser MCP automation", IntegrationMode: "review-first local inspection-only MCP context profile",
		Capabilities: []string{"browser tool schemas", "page inspection", "scripted browser actions"}, RecommendedFor: []string{"approved browser verification", "reproducible UI checks", "read-first web workflows"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review a local browser profile with explicit origin, download, upload, credential, storage, and action allowlists. Begin with read-only checks and deterministic test flows; external messages, posts, account changes, uploads, purchases, and deletion require a separate HAI approval.",
		Rationale:  "Playwright MCP can expose HAI's existing browser verification discipline through a standard tool boundary, without broadening browser autonomy or bypassing the current approval policy.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight MCP Servers repository list and GitHub metadata checked on 2026-07-19: active main branch, Apache-2.0 licence. HAI can preflight one operator-configured local endpoint through initialize and tools/list, accepting only a reviewed inspection-only browser inventory. HAI does not install or start Playwright MCP, connect a browser, send credentials, or call its tools.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "browser MCP tools", HAIControl: "browser origin and action allowlists", Boundary: "the tool cannot inherit logged-in accounts or external-action permission"},
			{SourcePattern: "browser state", HAIControl: "source evidence and verification records", Boundary: "browser observations do not become facts without HAI verification"},
		},
	},
	{
		ID: "google-genai-toolbox", Name: "MCP Toolbox", UpstreamURL: "https://github.com/googleapis/mcp-toolbox", RepositoryAliases: []string{"googleapis/genai-toolbox"}, SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10105/repos/", SourceCollection: "MCP Servers",
		Status: StatusIntegrated, Category: "database tool boundary", IntegrationMode: "integrated local MCP Toolbox preflight profile",
		Capabilities: []string{"local MCP readiness", "database tool inventory", "toolset scope review"}, RecommendedFor: []string{"approved database-tool review", "source-backed operational query design", "connector design"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Configure one separately started local MCP Toolbox Streamable HTTP endpoint under HAI's owner-only preflight. HAI performs only initialize and tools/list, never sends credentials or calls a database tool. A future execution adapter would still require a named read-only toolset, approved query templates, row/time limits, parameter validation, redacted audit logs, and a separate approval review.",
		Rationale:  "MCP Toolbox offers a relevant MCP design for reviewing a narrowly exposed database toolset, while HAI keeps connection ownership, provenance, query policy, write denial, and every tool call outside this inspection profile.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight MCP Servers repository list and GitHub metadata checked on 2026-07-21: the former googleapis/genai-toolbox slug now resolves to active googleapis/mcp-toolbox (main, Apache-2.0). HAI implements only its owner-authenticated local MCP handshake and tool inventory; no MCP Toolbox process, database connection, credential, or tool execution is configured by HAI.",
	},
	{
		ID: "qodo-pr-agent", Name: "Qodo PR-Agent", UpstreamURL: "https://github.com/qodo-ai/pr-agent", RepositoryAliases: []string{"Codium-ai/pr-agent", "The-PR-Agent/pr-agent"}, SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10136/repos/", SourceCollection: "AI Code Review",
		Status: StatusReferenceOnly, Category: "community-maintained legacy pull-request review framework", IntegrationMode: "review-pattern reference",
		Capabilities: []string{"pull-request analysis", "change summaries", "review suggestions", "test-gap detection"}, RecommendedFor: []string{"developer quality gates", "pull-request triage", "review preparation"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install or connect this upstream. Its repository redirects to the community-maintained legacy The-PR-Agent/pr-agent project. Any future use requires a separate maintenance, local-model, repository-scope, diff-redaction, retention, and no-publish/no-merge review.",
		Rationale:  "The project retains useful pull-request review patterns, but its community-maintained legacy status and CLI/Action/webhook publication paths make it unsuitable for direct adoption without an explicit operational review. HAI's bounded mini-SWE review proposal profile remains the only active coding-worker path.",
		VerifiedAt: "2026-07-21", VerificationNote: "Upstream rechecked on 2026-07-21: qodo-ai/pr-agent redirects to active The-PR-Agent/pr-agent, whose README identifies it as a community-maintained legacy project and whose current LICENSE is MIT. Its documented CLI, Action, and webhook paths can publish review output. HAI has no dependency, runner, credentials, repository access, or integration for it.",
	},
	{
		ID: "swe-agent", Name: "SWE-agent", UpstreamURL: "https://github.com/SWE-agent/SWE-agent", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10136/repos/", SourceCollection: "AI Code Review",
		Status: StatusReferenceOnly, Category: "superseded code-worker architecture", IntegrationMode: "historical architecture reference",
		Capabilities: []string{"issue-to-patch planning", "test-driven code changes", "workspace task loops"}, RecommendedFor: []string{"coding-agent architecture review", "trajectory and sandbox comparison"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install or connect SWE-agent. Its maintainers recommend mini-SWE-agent as the successor. Retain it only for design comparison; HAI will not create a legacy code-worker profile, mount a repository, grant a provider credential, or run an agent loop from this project.",
		Rationale:  "SWE-agent remains an informative code-agent design, but its own upstream now directs new users to mini-SWE-agent. HAI therefore keeps the older project as a reference rather than maintaining two overlapping coding-worker candidates.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official upstream reviewed on 2026-07-20: active main branch, MIT licence, and its README states that current development effort is on mini-SWE-agent and recommends mini-SWE-agent going forward. HAI has no SWE-agent worker, repository mount, provider credential, or executable integration.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "agent shell loop", HAIControl: "controlled runtime worker and workspace allowlist", Boundary: "no generic shell, secret, network, or host access"},
			{SourcePattern: "generated patch", HAIControl: "diff audit and deterministic tests", Boundary: "a patch never becomes a commit, push, or completion claim automatically"},
		},
	},
	{
		ID: "swe-rex", Name: "SWE-ReX", UpstreamURL: "https://github.com/SWE-agent/SWE-ReX", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10137/repos/", SourceCollection: "Agent Sandboxing",
		Status: StatusReferenceOnly, Category: "sandboxed shell-session framework", IntegrationMode: "execution-architecture reference",
		Capabilities: []string{"sandboxed shell sessions", "local and remote deployment abstractions", "command output and exit-code capture", "parallel execution patterns"}, RecommendedFor: []string{"controlled runtime architecture", "sandbox boundary review", "coding-worker containment"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install, start, or connect SWE-ReX directly. Its server exposes session creation, command execution, file read/write, upload, and close endpoints. Revisit only if HAI's existing runtime registry, disposable mini-SWE worker, and WASI runner have a measured sandboxing gap and a dedicated local deployment, command allowlist, workspace isolation, network policy, secret handling, audit, rollback, and emergency-stop design is approved.",
		Rationale:  "SWE-ReX is active and MIT licensed, but its general shell interface would otherwise create a parallel broad-command execution control plane. HAI retains one approval-gated runtime authority and records SWE-ReX only as a concrete local sandbox architecture reference.",
		VerifiedAt: "2026-07-21", VerificationNote: "Upstream rechecked on 2026-07-21: active main branch, MIT licence, Python >=3.10, and current server routes for create_session, run_in_session, execute, read_file, write_file, upload, and close. HAI has no SWE-ReX dependency, service, token, endpoint, session, shell command, file operation, or remote deployment configured.",
	},
	{
		ID: "swe-bench", Name: "SWE-bench", UpstreamURL: "https://github.com/SWE-bench/SWE-bench", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10136/repos/", SourceCollection: "AI Code Review / Agent Harness",
		Status: StatusExcluded, Category: "resource-intensive coding benchmark harness", IntegrationMode: "excluded benchmark harness",
		Capabilities: []string{"containerized patch evaluation", "real-world issue benchmark", "reproducible code-agent scoring"}, RecommendedFor: []string{"offline benchmark research", "coding-agent evaluation design"},
		RequiresApproval: true, LocalFirstCompatible: false,
		Activation: "Do not install SWE-bench, download its datasets, pull benchmark images, expose a Docker socket, mount a workspace, or start a cloud evaluation from HAI. Any future evaluation needs a separately approved capacity, source-provenance, image-isolation, local-only, cost, and retention plan.",
		Rationale:  "SWE-bench is useful benchmark research, but its multi-image Docker harness is inappropriate as a routine HAI brain or local runtime. HAI keeps its lighter mini-SWE disposable patch-proposal worker and independent verification controls for bounded local review.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Code Review and Agent Harness repository lists plus the official SWE-bench README were checked on 2026-07-21. The upstream is MIT licensed but documents Docker with approximately 120 GB free storage, 16 GB RAM, and 8 CPU cores. HAI has no SWE-bench package, dataset, image, Docker socket, cloud evaluation, or execution path configured.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "containerized benchmark evaluation", HAIControl: "separate capacity and isolation review", Boundary: "benchmark images, datasets, workspaces, Docker socket, and cloud execution are unavailable to HAI"},
			{SourcePattern: "patch success score", HAIControl: "HAI-native diff inspection and deterministic verification", Boundary: "a benchmark result cannot authorize a patch, completion, provider, or execution action"},
		},
	},
	{
		ID: "mini-swe-agent", Name: "mini-SWE-agent", UpstreamURL: "https://github.com/SWE-agent/mini-swe-agent", SourceCatalogURL: "https://github.com/SWE-agent/mini-swe-agent", SourceCollection: "SWE-agent successor / AI Code Review",
		Status: StatusIntegrated, Category: "minimal sandboxed code-worker", IntegrationMode: "disabled-by-default disposable patch-proposal worker",
		Capabilities: []string{"linear agent trajectory", "repository patch proposal", "Docker or Podman environments", "local model compatibility"}, RecommendedFor: []string{"contained local bug-fix experiments", "reproducible patch proposals", "coding-worker sandbox evaluation"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Enable the pinned `mini-swe` Compose profile only after placing a reviewed, sanitized source snapshot in the workspace allowlist and preloading an isolated local Ollama model. HAI accepts one owner-scoped workflow only after a high-risk review is approved and the workflow reaches ready. The runner copies the snapshot into temporary storage and returns a bounded diff/digest for human review. It cannot commit, push, create a pull request, access accounts, apply a patch, or execute outside the disposable sandbox.",
		Rationale:  "mini-SWE-agent is the maintained successor recommended by the SWE-agent project and has a smaller, linear execution model that HAI can contain in a review-first, local-only patch-proposal profile. The integration remains disabled until an operator supplies an approved source snapshot, a separate runner token, and a reviewed local model.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official upstream and CLI verified on 2026-07-20: MIT, active main branch, v2.4.5 released 2026-07-06. HAI pins that package in a disabled-by-default, read-only-snapshot Compose profile. The image build and CLI contract are verified; no local model, source snapshot, runner token, or production execution has been configured.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "bash or subprocess command", HAIControl: "isolated disposable worktree and resource-limited execution broker", Boundary: "no host shell, Docker socket, secret, account, or unrestricted network access"},
			{SourcePattern: "agent trajectory and generated patch", HAIControl: "audit event, diff inspection, selected deterministic tests, and human approval", Boundary: "the agent cannot commit, push, open a pull request, or claim task completion"},
		},
	},
	{
		ID: "openlit", Name: "OpenLIT", UpstreamURL: "https://github.com/openlit/openlit", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10135/repos/", SourceCollection: "AI Observability",
		Status: StatusIntegrated, Category: "local aggregate OTLP observability", IntegrationMode: "disabled-by-default local aggregate OTLP export bridge",
		Capabilities: []string{"LLM traces", "latency and token metrics", "tool observability", "OpenTelemetry export"}, RecommendedFor: []string{"model routing diagnostics", "tool failure analysis", "local performance evidence"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review and host a local OpenLIT-compatible OTLP collector, set HAI_OPENLIT_ENABLED=true and one loopback/private HAI_OPENLIT_OTLP_ENDPOINT, then invoke the owner-only aggregate snapshot export. Review collector retention, access, deletion, and local network controls separately. HAI will not install OpenLIT, use its SDK, instrument calls automatically, export prompt/source/model/token/workflow data, or contact a remote endpoint.",
		Rationale:  "OpenLIT now supplies an optional local aggregate trace viewer for a measured observability gap while HAI retains native audit records and sole routing, approval, verification, memory, workflow, and execution authority.",
		VerifiedAt: "2026-07-21", VerificationNote: "Official GitHub metadata rechecked on 2026-07-21: active main branch, Apache-2.0 licence, and a same-day upstream push. HAI implements only a disabled-by-default owner-triggered OTLP/HTTP JSON export to a local collector. It installs no collector and uses no OpenLIT SDK or automatic instrumentation.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "OpenLIT telemetry instrumentation", HAIControl: "fixed aggregate-only manual OTLP snapshot", Boundary: "no SDK, automatic instrumentation, caller-selected attributes, prompt, completion, source, file, token, model, workflow, or credential export"},
			{SourcePattern: "collector trace acceptance", HAIControl: "HAI audit, approval, verification, routing, and execution controls", Boundary: "a collector trace cannot authorize, verify, route, retain memory, spend budget, or execute work"},
		},
	},
	{
		ID: "agentops", Name: "AgentOps", UpstreamURL: "https://github.com/AgentOps-AI/agentops", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10135/repos/", SourceCollection: "AI Observability",
		Status: StatusReferenceOnly, Category: "agent observability platform", IntegrationMode: "architecture reference",
		Capabilities: []string{"agent tracing", "cost and latency observability", "session replay patterns"}, RecommendedFor: []string{"observability architecture review", "agent trace comparison"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install the SDK, configure an API key, export traces, or start a hosted or self-hosted AgentOps service from HAI. Reconsider only if HAI's existing audit ledger plus opt-in Prometheus, Langfuse, OpenLIT, and MLflow profiles leave a measured, source-redacted observability gap.",
		Rationale:  "AgentOps is active and MIT licensed, but introducing another trace authority would duplicate HAI's existing observability surfaces and create additional prompt, source, retention, and egress risk without a demonstrated gap.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Observability repository list and GitHub metadata checked on 2026-07-21: active main branch, archived=false, MIT licence, and latest push 2026-06-25. HAI has no AgentOps SDK, key, endpoint, collector, trace export, or telemetry integration configured.",
	},
	{
		ID: "langmem", Name: "LangMem", UpstreamURL: "https://github.com/langchain-ai/langmem", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10114/repos/", SourceCollection: "AI Agent Memory",
		Status: StatusReferenceOnly, Category: "memory-consolidation patterns", IntegrationMode: "architecture reference",
		Capabilities: []string{"memory extraction", "long-term memory patterns", "context management"}, RecommendedFor: []string{"memory-consolidation review", "context retrieval design", "preference revision patterns"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not introduce a parallel memory store. Revisit only for a measured gap in HAI's source-linked memory consolidation, with a provenance, correction, export, deletion, and rollback design that preserves HAI as the sole active memory authority.",
		Rationale:  "LangMem supplies useful memory-engineering patterns but must not replace HAI's editable, local-first, source-grounded memory plane.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Agent Memory repository list and GitHub metadata checked on 2026-07-19: active main branch, MIT licence; LangMem is not installed or connected.",
	},
	{
		ID: "pyrit", Name: "PyRIT", UpstreamURL: "https://github.com/Azure/PyRIT", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10138/repos/", SourceCollection: "AI Red Teaming",
		Status: StatusExcluded, Category: "AI red-team evaluation", IntegrationMode: "excluded upstream",
		Capabilities: []string{"adversarial prompt testing", "risk evaluation patterns"}, RecommendedFor: []string{"safety-test research only"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not integrate. Select a maintained safety-testing alternative only after a separate fixture, provider, egress, and no-write evaluation review.",
		Rationale:  "PyRIT is visible in the OSS Insight AI Red Teaming list but is archived upstream, so it does not meet HAI's active-candidate maintenance bar.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Red Teaming repository list and GitHub metadata checked on 2026-07-19: archived=true, MIT licence; excluded from activation.",
	},
	{
		ID: "phoenix", Name: "Arize Phoenix", UpstreamURL: "https://github.com/Arize-ai/phoenix", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10135/repos/", SourceCollection: "AI Observability",
		Status: StatusLicenseReview, Category: "LLM observability", IntegrationMode: "license-review reference",
		Capabilities: []string{"traces", "evaluation dashboards", "retrieval observability"}, RecommendedFor: []string{"observability comparison"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not integrate until the upstream licence files, local deployment terms, telemetry retention, data egress, redaction, and collector ownership are reviewed. A missing SPDX value is not treated as an open-source licence grant.",
		Rationale:  "Phoenix is maintained and relevant, but the current GitHub API metadata reports NOASSERTION. HAI holds it for explicit licence and data-handling review rather than assuming it is acceptable.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Observability repository list and GitHub metadata checked on 2026-07-19: active main branch, licence=NOASSERTION; no Phoenix deployment is configured by HAI.",
	},
	{
		ID: "taskweaver", Name: "TaskWeaver", UpstreamURL: "https://github.com/microsoft/TaskWeaver", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10137/repos/", SourceCollection: "Agent Sandboxing",
		Status: StatusExcluded, Category: "code-interpreter agent", IntegrationMode: "excluded upstream",
		Capabilities: []string{"code-interpreter patterns", "plugin orchestration"}, RecommendedFor: []string{"architecture research only"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not integrate. Reconsider only if a maintained successor and a complete sandbox, tool, data, and rollback design are independently reviewed.",
		Rationale:  "TaskWeaver is relevant to governed code execution but is archived upstream, which disqualifies it from HAI's active-candidate set.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Agent Sandboxing and Agent Harness repository lists and GitHub metadata checked on 2026-07-19: archived=true, MIT licence; excluded from activation.",
	},
	{
		ID: "presidio", Name: "Presidio", UpstreamURL: "https://github.com/data-privacy-stack/presidio", RepositoryAliases: []string{"microsoft/presidio"}, SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10116/repos/", SourceCollection: "AI Safety & Alignment",
		Status: StatusIntegrated, Category: "sensitive-data detection and redaction", IntegrationMode: "integrated opt-in local redaction adapter",
		Capabilities: []string{"PII detection", "redaction", "masking", "anonymisation"}, RecommendedFor: []string{"secret redaction", "source-import privacy checks", "safe audit previews"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "HAI ships a disabled-by-default local Analyzer bridge for bounded, manually submitted text and explicit language/entity allowlists. Before enabling it, review false positives, local model/language coverage, source retention, and capacity. The bridge returns metadata only; it cannot anonymize, delete source records, change approval status, or conceal original evidence from an authorised owner.",
		Rationale:  "HAI now exposes Presidio through a bounded local PII-detection bridge that strengthens its deterministic privacy boundary without introducing a second data authority.",
		VerifiedAt: "2026-07-20", VerificationNote: "OSS Insight AI Safety & Alignment listing and the current data-privacy-stack/presidio upstream were checked on 2026-07-20. The project has moved from the Microsoft GitHub namespace, is MIT licensed, and explicitly warns that automated detection is not complete. HAI has a disabled local Analyzer bridge but does not install or configure a Presidio service.",
	},
	{
		ID: "guardrails-ai", Name: "Guardrails AI", UpstreamURL: "https://github.com/guardrails-ai/guardrails", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10116/repos/", SourceCollection: "AI Safety & Alignment",
		Status: StatusIntegrated, Category: "structured-output validation", IntegrationMode: "integrated opt-in internal fixed-schema validation bridge",
		Capabilities: []string{"schema validation", "output validators", "retry signals", "structured extraction checks"}, RecommendedFor: []string{"structured extraction", "planning validation", "grounded-output review"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "HAI ships a disabled internal runner that validates one bounded redacted action_proposal JSON contract through Guardrails AI's Pydantic schema path. Enable it only with the local validation profile; no Hub validator download, LLM call, retry, persistence, execution, policy change, or approval is available.",
		Rationale:  "Guardrails AI complements HAI's deterministic schemas and verification statuses with a constrained review signal rather than replacing the safety policy or human approval gate.",
		VerifiedAt: "2026-07-20", VerificationNote: "OSS Insight AI Safety & Alignment listing and current Guardrails AI upstream were reviewed on 2026-07-20. HAI implements only an opt-in internal fixed-schema bridge; no local runner is enabled by default and proposal text is neither stored nor returned.",
	},
	{
		ID: "lm-eval-harness", Name: "LM Evaluation Harness", UpstreamURL: "https://github.com/EleutherAI/lm-evaluation-harness", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10119/repos/", SourceCollection: "AI Evaluation & Testing",
		Status: StatusIntegrated, Category: "offline model evaluation", IntegrationMode: "integrated opt-in internal fixed-suite local benchmark runner",
		Capabilities: []string{"benchmark suites", "few-shot evaluation", "repeatable model comparison", "result artifacts"}, RecommendedFor: []string{"local model comparison", "routing regression", "capability baselines"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "HAI ships a disabled local runner for one preconfigured local OpenAI-compatible model and a six-case synthetic suite. Enable the model-evaluation profile only after reviewing the named local endpoint, fixture provenance, resource limits, and no-production-data rule. Results can inform an operator review but cannot select a model, spend budget, or change HAI routing automatically.",
		Rationale:  "LM Evaluation Harness now adds reproducible local model evidence where HAI needs to compare capability rather than assume the cheapest provider is sufficient.",
		VerifiedAt: "2026-07-20", VerificationNote: "OSS Insight AI Evaluation & Testing listing and current LM Evaluation Harness upstream were reviewed on 2026-07-20. HAI implements only an opt-in six-case synthetic local bridge; no runner is enabled by default and no raw generations, task rows, or result artifacts are retained or returned.",
	},
	{
		ID: "openllmetry", Name: "OpenLLMetry", UpstreamURL: "https://github.com/traceloop/openllmetry", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10135/repos/", SourceCollection: "AI Observability",
		Status: StatusCandidate, Category: "LLM trace instrumentation", IntegrationMode: "reviewed local telemetry bridge",
		Capabilities: []string{"OpenTelemetry traces", "model-call instrumentation", "latency metrics", "cost and token signals"}, RecommendedFor: []string{"routing observability", "evaluation traces", "failure analysis"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review local collector ownership, attribute allowlist, secret and prompt redaction, retention, sampling, export disablement, and health checks. Telemetry is observational only: it cannot grant provider access, alter budgets, or approve execution.",
		Rationale:  "OpenLLMetry remains a trace-level observability candidate. HAI now has a durable native redacted generation ledger for provider/model, status, aggregate cost, exact-or-estimated tokens, latency, fallback, and audit evidence; a collector is justified only if a measured trace-level gap remains.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Observability repository listing and current GitHub metadata rechecked on 2026-07-21: active main branch, Apache-2.0 licence. HAI has no OpenLLMetry collector or instrumentation; its native generation ledger deliberately retains no prompts, completions, or raw provider payloads.",
	},
	{
		ID: "graphrag", Name: "Microsoft GraphRAG", UpstreamURL: "https://github.com/microsoft/graphrag", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10134/repos/", SourceCollection: "Knowledge Graphs for AI",
		Status: StatusReferenceOnly, Category: "graph-based retrieval patterns", IntegrationMode: "architecture reference",
		Capabilities: []string{"entity graph extraction", "community summaries", "graph retrieval"}, RecommendedFor: []string{"retrieval-gap analysis", "case timeline research", "entity-linking design"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not introduce a second index or memory authority. Revisit only after a measured source-linked retrieval gap and an approved design for extraction provenance, graph updates, deletion, export, and rollback.",
		Rationale:  "GraphRAG offers useful retrieval and evidence-linking patterns, but HAI keeps the existing source-linked memory plane authoritative until a demonstrated gap justifies a bounded adapter.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Knowledge Graphs for AI repository list and GitHub metadata checked on 2026-07-19; Microsoft GraphRAG is not installed or connected.",
	},
	{
		ID: "haystack", Name: "Haystack", UpstreamURL: "https://github.com/deepset-ai/haystack", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10108/repos/", SourceCollection: "RAG Frameworks",
		Status: StatusReferenceOnly, Category: "retrieval pipeline patterns", IntegrationMode: "architecture reference",
		Capabilities: []string{"document pipelines", "retrieval components", "evaluation patterns", "agent pipeline design"}, RecommendedFor: []string{"source-ingestion design", "retrieval-gap analysis", "document-processing review"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not create a parallel retrieval or memory system. Revisit only for a measured document-processing gap with a source, retention, evaluation, and rollback design that remains inside HAI's local provenance controls.",
		Rationale:  "Haystack provides mature retrieval-pipeline patterns, but HAI must preserve one controlled source, memory, verification, and execution plane.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight RAG Frameworks repository list and GitHub metadata checked on 2026-07-19; Haystack is not installed or connected.",
	},
	{
		ID: "fastmcp", Name: "FastMCP", UpstreamURL: "https://github.com/jlowin/fastmcp", RepositoryAliases: []string{"PrefectHQ/fastmcp"}, SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10105/repos/", SourceCollection: "MCP Servers",
		Status: StatusIntegrated, Category: "MCP tool-server authoring", IntegrationMode: "integrated local read-only HAI MCP bridge",
		Capabilities: []string{"authenticated MCP server", "fixed read-only HAI workflow tools", "typed tool schemas", "separate client and bridge tokens"}, RecommendedFor: []string{"reviewed local HAI operational context", "MCP capability design", "read-only agent situational awareness"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set one owner identity and two different local-only 32+ character tokens, then run the `mcp-bridge` Compose profile. The bridge binds only to 127.0.0.1 and exposes exactly two authenticated read-only tools. It must not be added to the generic no-auth MCP preflight list; any future write tool needs a separate HAI adapter and approval review.",
		Rationale:  "The local FastMCP bridge gives a reviewed agent client bounded situational awareness while HAI keeps all workflow mutation, source access, memory, approval, execution, and audit authority. It is intentionally not a generic tool registry or process launcher.",
		VerifiedAt: "2026-07-20", VerificationNote: "Current jlowin/fastmcp main revision and Apache-2.0 license checked on 2026-07-20. The isolated profile pins fastmcp 3.4.4 and implements no upstream tool execution, source access, or write capability by default.",
	},
	{
		ID: "vllm", Name: "vLLM", UpstreamURL: "https://github.com/vllm-project/vllm", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10109/repos/", SourceCollection: "LLM Inference Engines",
		Status: StatusIntegrated, Category: "local high-throughput model inference", IntegrationMode: "integrated loopback OpenAI-compatible provider profile",
		Capabilities: []string{"local model serving", "OpenAI-compatible API", "batched inference", "model capability discovery"}, RecommendedFor: []string{"local reasoning", "larger local models", "high-volume extraction"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review a loopback-only deployment with explicit GPU, model, quantization, context-window, retention, and resource limits. Reuse HAI's existing OpenAI-compatible provider probe and EUR 0 routing policy; HAI cannot select, send data to, or start vLLM until an operator configures and verifies the endpoint.",
		Rationale:  "HAI now implements a distinct vLLM provider profile for a measured local throughput or serving need while preserving explicit configuration, loopback-only reachability, live probing, and the existing model, budget, and approval policy.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight LLM Inference Engines repository list and GitHub metadata checked on 2026-07-19: active main branch, Apache-2.0 licence. HAI implements only the provider profile; no vLLM endpoint or model is configured by HAI.",
	},
	{
		ID: "sglang", Name: "SGLang", UpstreamURL: "https://github.com/sgl-project/sglang", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10109/repos/", SourceCollection: "LLM Inference Engines",
		Status: StatusIntegrated, Category: "local high-throughput model inference", IntegrationMode: "integrated loopback OpenAI-compatible provider profile",
		Capabilities: []string{"local model serving", "OpenAI-compatible API", "batched inference", "structured output serving"}, RecommendedFor: []string{"local reasoning", "larger local models", "high-volume extraction"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review a loopback-only deployment with explicit GPU, model, model licence, context-window, retention, and resource limits. Set SGLANG_BASE_URL and SGLANG_MODEL_ID only after that review. HAI probes only /v1/models and calls only /v1/chat/completions under its EUR 0 policy; it does not start SGLang, pull an image, choose a model, or inherit upstream tool surfaces.",
		Rationale:  "HAI now implements a distinct SGLang provider profile for an explicit local serving need while preserving local-first routing, the daily exact-model availability gate, budget controls, audit, and task approval gates.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight LLM Inference Engines repository list and current GitHub metadata checked on 2026-07-21: active main branch, Apache-2.0 licence, and same-day upstream push. SGLang documents OpenAI API compatibility; HAI implements only the loopback /v1/models and /v1/chat/completions provider profile. No SGLang server, model, UI, external endpoint, or built-in tool surface is configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "OpenAI-compatible server", HAIControl: "local provider probe, daily exact-model availability gate, and EUR 0 router", Boundary: "provider availability does not bypass model selection, budget, or task approval"},
			{SourcePattern: "operator-managed runtime or model update", HAIControl: "daily exact-model verification", Boundary: "HAI does not silently pull images, replace model weights, change model identifiers, or inherit upstream execution surfaces"},
		},
	},
	{
		ID: "deepeval", Name: "DeepEval", UpstreamURL: "https://github.com/confident-ai/deepeval", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10119/repos/", SourceCollection: "AI Evaluation & Testing",
		Status: StatusIntegrated, Category: "local synthetic source-grounding regression", IntegrationMode: "integrated opt-in isolated local evaluation runner",
		Capabilities: []string{"FaithfulnessMetric regression", "synthetic evidence-answer evaluation", "aggregate evaluator accuracy evidence"}, RecommendedFor: []string{"source-grounded answer regression", "local judge model review", "retrieval quality safeguards"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set HAI_DEEPEVAL_ENABLED=true, configure one reviewed local OpenAI-compatible model endpoint, and start the deepeval-evaluation Compose profile. HAI calls only a fixed three-case synthetic FaithfulnessMetric suite and returns aggregate evaluator accuracy. It cannot receive real sources, answers, prompts, metrics, provider choices, or commands, and the score cannot verify a task, change a route, or enable a provider.",
		Rationale:  "HAI now uses DeepEval for a distinct source-grounding regression beside Promptfoo, Garak, and DeepTeam. It tests whether a local judge distinguishes fixed faithful from unsupported synthetic answers while retaining HAI's verification, provider policy, and completion authority.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official DeepEval repository and PyPI metadata reviewed on 2026-07-20: Apache-2.0, current 4.1.1 release, Python >=3.9,<4.0, and documented FaithfulnessMetric support for checking an answer against retrieval context. HAI ships a disabled-by-default local runner with fixed synthetic fixtures; no endpoint, model, real HAI answer, connected source, or cloud account is configured by HAI by default.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "synthetic evidence and answer fixture", HAIControl: "isolated DeepEval FaithfulnessMetric runner", Boundary: "fixed three-case suite only; no real source, answer, prompt, caller metric, or model setting is accepted"},
			{SourcePattern: "evaluator score", HAIControl: "reviewable aggregate regression evidence", Boundary: "a score cannot mark a claim verified, alter routing, change policy, approve an action, or execute work"},
		},
	},
	{
		ID: "gitleaks", Name: "Gitleaks", UpstreamURL: "https://github.com/gitleaks/gitleaks", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10051/repos/", SourceCollection: "Security Tool",
		Status: StatusIntegrated, Category: "local aggregate secret scanner", IntegrationMode: "integrated opt-in redacted snapshot scanner",
		Capabilities: []string{"read-only named local snapshot scan", "aggregate rule and affected-file evidence", "redacted result digest"}, RecommendedFor: []string{"reviewed repository safety checks", "pre-execution source snapshot review", "credential-leak triage"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set a separate 16+ character local runner token, list one to eight reviewed snapshot names, copy only the reviewed snapshot under security-snapshots, and start the secret-scan Compose profile. An owner-admin may probe or scan a named snapshot. HAI never accepts a caller-selected path, source content, rule configuration, command, report destination, or secret value.",
		Rationale:  "A bounded local Gitleaks profile closes a distinct source-safety gap without granting a scanner access to arbitrary host paths, repositories, credentials, network services, or HAI execution authority.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight Security Tool listing and current gitleaks/gitleaks GitHub metadata checked on 2026-07-21: active MIT upstream, v8.30.1 release, and no archive marker. The isolated optional runner uses the documented Gitleaks directory scan with redacted JSON output, deletes its temporary report, and returns aggregate metadata only.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "reviewed named source snapshot", HAIControl: "read-only Gitleaks directory scan", Boundary: "only configured direct child snapshots under the read-only input mount; no caller path, Git history, network, source upload, or file write"},
			{SourcePattern: "redacted scanner findings", HAIControl: "aggregate rule counts, affected-file count, and result digest", Boundary: "matched text, secret values, paths, lines, commits, authors, raw reports, and source files are not returned, stored, or used as facts"},
			{SourcePattern: "potential secret finding", HAIControl: "owner review in the original workspace", Boundary: "a result cannot automatically block, approve, execute, verify completion, alter memory, or alter provider routing"},
		},
	},
	{
		ID: "gosec", Name: "Gosec", UpstreamURL: "https://github.com/securego/gosec", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10051/repos/", SourceCollection: "Security Tool",
		Status: StatusIntegrated, Category: "local aggregate Go security analysis", IntegrationMode: "integrated opt-in redacted vendored Go snapshot scanner",
		Capabilities: []string{"read-only named vendored Go snapshot scan", "aggregate severity and confidence evidence", "redacted result digest"}, RecommendedFor: []string{"reviewed Go repository security checks", "pre-execution source snapshot review", "manual secure-coding triage"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set a separate 16+ character local runner token, list one to eight reviewed snapshot names, copy only a reviewed Go source snapshot with go.mod and vendor/modules.txt under security-snapshots, and start the go-security-scan Compose profile. An owner-admin may probe or scan a named snapshot. HAI never accepts a caller-selected path, source, finding, rule, CWE, command, report destination, or remediation request.",
		Rationale:  "A bounded local Gosec profile adds a distinct static Go AST/SSA and taint-analysis signal next to HAI's secrets, SBOM, and vulnerability controls without granting a scanner arbitrary host paths, source export, module network access, credentials, write authority, or HAI execution authority.",
		VerifiedAt: "2026-07-22", VerificationNote: "OSS Insight Security Tool listing and current securego/gosec GitHub metadata checked on 2026-07-22: active Apache-2.0 upstream, v2.28.0 release, and no archive marker. Upstream documents AST/SSA scanning, taint analyzers, JSON output, and Go 1.25+; HAI's isolated optional runner requires vendored dependencies and disables module downloads and proxy egress before returning aggregate metadata only.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "reviewed named vendored Go source snapshot", HAIControl: "read-only offline Gosec package scan", Boundary: "only configured direct child snapshots under the read-only input mount with go.mod and vendor/modules.txt; no caller path, source upload, module download, proxy egress, or file write"},
			{SourcePattern: "local static-analysis report", HAIControl: "aggregate finding total, severity/confidence counts, and result digest", Boundary: "source, paths, findings, rules, CWEs, raw reports, and remediation details are not returned, stored, or used as facts"},
			{SourcePattern: "potential secure-coding finding", HAIControl: "owner review in the original workspace", Boundary: "a result cannot automatically alter source, block or approve work, execute remediation, verify completion, alter memory, or alter provider routing"},
		},
	},
	{
		ID: "trivy", Name: "Trivy", UpstreamURL: "https://github.com/aquasecurity/trivy", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10051/repos/", SourceCollection: "Security Tool",
		Status: StatusIntegrated, Category: "local aggregate configuration security review", IntegrationMode: "integrated opt-in offline configuration snapshot scanner",
		Capabilities: []string{"read-only named configuration snapshot scan", "aggregate configuration severity evidence", "redacted result digest"}, RecommendedFor: []string{"reviewed infrastructure configuration checks", "pre-execution configuration snapshot review", "manual misconfiguration triage"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set a separate 16+ character local runner token, list one to eight reviewed snapshot names, copy only a reviewed configuration snapshot under security-snapshots, and start the configuration-security-scan Compose profile. An owner-admin may probe or scan a named snapshot. HAI never accepts a caller-selected path, image, repository, cloud target, finding, policy, command, report destination, or remediation request.",
		Rationale:  "A bounded local Trivy profile adds aggregate offline configuration-security evidence without granting a scanner arbitrary host paths, image/repository/cloud access, policy updates, credentials, write authority, or HAI execution authority.",
		VerifiedAt: "2026-07-22", VerificationNote: "OSS Insight Security Tool listing and current aquasecurity/trivy GitHub metadata checked on 2026-07-22: active Apache-2.0 upstream, v0.72.0 release, and no archive marker. HAI deliberately uses only the offline configuration scanner, disables policy updates and proxy egress, and returns aggregate metadata only.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "reviewed named configuration snapshot", HAIControl: "read-only offline Trivy configuration scan", Boundary: "only configured direct child snapshots under the read-only input mount; no caller path, source upload, image, repository, cloud, policy update, proxy egress, or file write"},
			{SourcePattern: "local configuration security report", HAIControl: "aggregate finding total, severity counts, and result digest", Boundary: "source, paths, findings, rules, policy details, raw reports, and remediation details are not returned, stored, or used as facts"},
			{SourcePattern: "potential configuration issue", HAIControl: "owner review in the original workspace", Boundary: "a result cannot automatically alter source, block or approve work, execute remediation, verify completion, alter memory, or alter provider routing"},
		},
	},
	{
		ID: "syft", Name: "Syft", UpstreamURL: "https://github.com/anchore/syft", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10051/repos/", SourceCollection: "Security Tool",
		Status: StatusIntegrated, Category: "local aggregate software inventory", IntegrationMode: "integrated opt-in redacted snapshot SBOM inventory",
		Capabilities: []string{"read-only named local snapshot inventory", "aggregate package and ecosystem evidence", "redacted result digest"}, RecommendedFor: []string{"reviewed repository inventory", "dependency ecosystem review", "pre-execution source snapshot review"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set a separate 16+ character local runner token, list one to eight reviewed snapshot names, copy only the reviewed snapshot under security-snapshots, and start the sbom-inventory Compose profile. An owner-admin may probe or inventory a named snapshot. HAI never accepts a caller-selected path, source content, SBOM export, package query, command, or scanner configuration.",
		Rationale:  "A bounded local Syft profile gives HAI software-supply-chain context without granting an inventory runner access to arbitrary host paths, repositories, package metadata, credentials, network services, or HAI execution authority.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight Security Tool listing and current anchore/syft GitHub metadata checked on 2026-07-21: active Apache-2.0 upstream, v1.48.0 release, and no archive marker. The isolated optional runner uses the documented Syft directory inventory with in-memory Syft JSON parsing and returns aggregate metadata only.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "reviewed named source snapshot", HAIControl: "read-only Syft directory inventory", Boundary: "only configured direct child snapshots under the read-only input mount; no caller path, Git history, network, source upload, or file write"},
			{SourcePattern: "generated local SBOM", HAIControl: "aggregate package total, ecosystem counts, and result digest", Boundary: "SBOMs, package names, versions, licences, PURLs, paths, and source files are not returned, stored, or used as facts"},
			{SourcePattern: "software inventory evidence", HAIControl: "owner review in the original workspace", Boundary: "a result cannot automatically block, approve, execute, verify completion, alter memory, or alter provider routing"},
		},
	},
	{
		ID: "grype", Name: "Grype", UpstreamURL: "https://github.com/anchore/grype", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10051/repos/", SourceCollection: "Security Tool",
		Status: StatusIntegrated, Category: "local aggregate vulnerability evidence", IntegrationMode: "integrated opt-in offline snapshot vulnerability scanner",
		Capabilities: []string{"read-only named local snapshot scan", "aggregate severity and fix-availability evidence", "redacted result digest"}, RecommendedFor: []string{"reviewed repository vulnerability triage", "pre-execution source snapshot review", "manual dependency-risk review"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set a separate 16+ character local runner token, list one to eight reviewed snapshot names, copy only the reviewed snapshot under security-snapshots, manually provide a reviewed local advisory database under security-advisories, and start the vulnerability-scan Compose profile. An owner-admin may probe or scan a named snapshot. HAI never accepts a caller-selected path, package, version, CVE, advisory, raw report, command, scanner configuration, or remediation request.",
		Rationale:  "A bounded offline Grype profile closes the remaining aggregate vulnerability-evidence gap next to HAI's secret and SBOM controls without granting a scanner arbitrary host paths, repositories, credentials, network access, dependency-write authority, or HAI execution authority.",
		VerifiedAt: "2026-07-22", VerificationNote: "OSS Insight Security Tool listing and current anchore/grype GitHub metadata checked on 2026-07-22: active Apache-2.0 upstream, v0.116.0 release, and no archive marker. The isolated optional runner accepts only a configured named read-only snapshot, uses a separately mounted local advisory database with update checks disabled, clears proxies, and returns aggregate metadata only.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "reviewed named source snapshot", HAIControl: "read-only offline Grype directory scan", Boundary: "only configured direct child snapshots under the read-only input mount; no caller path, Git history, network, source upload, or file write"},
			{SourcePattern: "local vulnerability evidence", HAIControl: "aggregate severity counts, fix-availability count, and result digest", Boundary: "CVEs, package names, versions, advisories, paths, raw reports, source files, and remediation commands are not returned, stored, or used as facts"},
			{SourcePattern: "potential vulnerability finding", HAIControl: "owner review in the original workspace", Boundary: "a result cannot automatically alter dependencies, block or approve work, execute remediation, verify completion, alter memory, or alter provider routing"},
		},
	},
	{
		ID: "ollama", Name: "Ollama", UpstreamURL: "https://github.com/ollama/ollama", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10109/repos/", SourceCollection: "LLM Inference Engines",
		Status: StatusIntegrated, Category: "local model inference", IntegrationMode: "operator-configured loopback Ollama provider",
		Capabilities: []string{"local model discovery", "local generation", "model tags probe", "local-first routing"}, RecommendedFor: []string{"local reasoning", "classification", "extraction", "drafting"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set a loopback-only OLLAMA_BASE_URL and run HAI's persisted provider probe. HAI selects only a live local model under the EUR 0 policy and still requires the task's existing approval gate before consequential generation or execution.",
		Rationale:  "HAI already has a real local Ollama provider, tag probe, readiness persistence, and local-first route selection. The catalog makes that implemented boundary visible without installing Ollama or selecting a model automatically.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight LLM Inference Engines repository list and HAI's existing local-provider implementation checked on 2026-07-19.",
	},
	{
		ID: "browser-use", Name: "browser-use", UpstreamURL: "https://github.com/browser-use/browser-use", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10113/repos/", SourceCollection: "AI Browser Agents",
		Status: StatusCandidate, Category: "agentic browser execution", IntegrationMode: "reviewed local browser adapter",
		Capabilities: []string{"browser task planning", "tool-mediated browsing", "structured browser outcomes"}, RecommendedFor: []string{"browser workflow design", "approved research", "read-only verification"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review a local, named browser profile with origin, download, upload, credential, and action allowlists. Start with read-only verification; sending, posting, account changes, purchases, uploads, and destructive actions require separate HAI approvals.",
		Rationale:  "A relevant browser-agent candidate, but browser autonomy can cause irreversible external effects. HAI already owns allowlisted read-only local browser verification, so a browser-use adapter is justified only by a measured capability gap that does not duplicate that surface.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Browser Agents listing and current GitHub metadata rechecked on 2026-07-21: active main branch, MIT licence, and not archived. HAI has no browser-use runtime, browser profile, cookies, account session, or external-action integration configured.",
	},
	{
		ID: "nemo-guardrails", Name: "NVIDIA NeMo Guardrails", UpstreamURL: "https://github.com/NVIDIA-NeMo/Guardrails", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10116/repos/", SourceCollection: "AI Safety & Alignment",
		Status: StatusLicenseReview, Category: "LLM interaction guardrails", IntegrationMode: "license-review reference",
		Capabilities: []string{"input controls", "output controls", "topic and policy rails"}, RecommendedFor: []string{"draft validation", "high-risk output review", "policy testing"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not integrate until upstream licence terms are independently reviewed. A future local adapter would also need policy ownership, false-positive handling, data redaction, model routing, audit events, fail-closed behavior, and strict no-approval/no-execution boundaries.",
		Rationale:  "NeMo Guardrails remains relevant to interaction-safety research, but current GitHub metadata provides no SPDX licence assertion. HAI keeps its deterministic injection block, Guardrails AI schema validator, Garak regression, verification policy, and approvals authoritative rather than importing an unreviewed dependency.",
		VerifiedAt: "2026-07-21", VerificationNote: "Current NVIDIA-NeMo/Guardrails GitHub metadata rechecked on 2026-07-21: active develop branch, release v0.23.0 published 2026-07-01, licence=NOASSERTION. No NeMo service, package, policy file, model, or telemetry path is configured by HAI.",
	},
	{
		ID: "garak", Name: "garak", UpstreamURL: "https://github.com/NVIDIA/garak", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10138/repos/", SourceCollection: "AI Red Teaming",
		Status: StatusIntegrated, Category: "local synthetic prompt-injection regression", IntegrationMode: "integrated opt-in isolated local evaluation runner",
		Capabilities: []string{"prompt-injection probe", "local model vulnerability regression", "aggregate pass/fail evidence"}, RecommendedFor: []string{"local model safety regression", "pre-release prompt-injection review", "safety harness review"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set a reviewed local OpenAI-compatible model endpoint and start the `garak-evaluation` Compose profile. HAI runs exactly one shipped four-case synthetic PromptInject probe. It cannot inspect HAI, target a real agent, accept user test data, call a runtime, use a cloud model, retain raw reports, or change HAI policy. Any real-system red-team plan needs a separately approved, redacted, isolated evaluation design.",
		Rationale:  "The integration adds a distinct, broader scanner-derived prompt-injection regression signal while HAI retains all production workflow, data, provider, verification, approval, runtime, and audit authority.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official Garak repository and package metadata reviewed on 2026-07-20: active Apache-2.0 LLM vulnerability scanner; garak 0.15.1 supports selectable probes and OpenAI-compatible local endpoints. HAI pins 0.15.1 in an opt-in internal runner, fixes one four-case PromptInject probe, clears inherited proxy/provider credentials, deletes raw reports, and returns aggregate metadata only. No real HAI target, account, source, runtime, model route, or action is configured.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "PromptInject probe pass/fail aggregate", HAIControl: "verification and safety review evidence", Boundary: "synthetic aggregate evidence cannot mark production work verified or alter a routing, policy, approval, or execution decision"},
			{SourcePattern: "Garak JSONL reports and model generations", HAIControl: "temporary runner filesystem and source-privacy boundary", Boundary: "raw prompts, outputs, hit logs, HTML, and full reports are deleted before the runner responds and are never persisted or exported by HAI"},
		},
	},
	{
		ID: "whisper-cpp", Name: "whisper.cpp", UpstreamURL: "https://github.com/ggml-org/whisper.cpp", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10118/repos/", SourceCollection: "Multimodal AI",
		Status: StatusIntegrated, Category: "local speech transcription", IntegrationMode: "operator-configured local intake adapter",
		Capabilities: []string{"offline transcription", "audio-to-text extraction", "local model execution"}, RecommendedFor: []string{"voice-note intake", "meeting evidence", "accessibility transcription"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Enable the local-transcription Compose profile, place one reviewed GGML model in the local model folder, then create an owner-scoped local-only whisper-audio source with an explicit subfolder. HAI stores returned transcripts only through its existing source and memory verification path.",
		Rationale:  "Local speech-to-text can broaden safe intake without transmitting audio to a cloud service, but it requires explicit consent and evidence-quality controls.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Multimodal AI repository list checked on 2026-07-19; HAI includes a disabled-by-default local whisper.cpp runner that reads only an explicit selected folder and returns transcript metadata through the source review path.",
	},
	{
		ID: "docling", Name: "Docling", UpstreamURL: "https://github.com/docling-project/docling", SourceCatalogURL: "https://ossinsight.io/collections", SourceCollection: "capability-gap review",
		Status: StatusIntegrated, Category: "local structured document extraction", IntegrationMode: "operator-triggered owner-scoped local intake adapter",
		Capabilities: []string{"office-document extraction", "structured Markdown conversion", "local text intake"}, RecommendedFor: []string{"document evidence intake", "project document review", "selected-folder extraction"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Enable the local-document-extraction Compose profile, use a dedicated internal runner token, and create an owner-scoped local-only docling-documents source that names one subfolder of ./connected-sources. DOCX, PPTX, XLSX, HTML, Markdown, and text extraction are manual only. PDF extraction stays disabled unless the operator separately provides reviewed local Docling artifacts and explicitly enables it; HAI never downloads models or calls remote parsing services.",
		Rationale:  "Docling closes a concrete structured-document intake gap while keeping HAI's source registry, provenance, memory, verification, workflow, approval, and execution controls authoritative.",
		VerifiedAt: "2026-07-22", VerificationNote: "Direct upstream metadata and documentation reviewed on 2026-07-22: active MIT-licensed main branch and v2.114.0 released 2026-07-20. This project was selected after an OSS Insight collection capability-gap pass; it was not represented as a member of a current collection response. HAI pins an opt-in internal runner, disables remote services, OCR, and table processing, accepts only an explicit selected folder, and never downloads model artifacts. Extracted text remains source-linked uncertain evidence until independently reviewed.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "selected local documents", HAIControl: "owner-scoped local-only source registry", Boundary: "one registered relative subfolder only; no uploads, arbitrary paths, scheduled scans, browser capture, or original-file retention"},
			{SourcePattern: "document conversion output", HAIControl: "source provenance and verification workflow", Boundary: "extracted text is uncertain source evidence and cannot create facts, update memory, approve work, execute an action, or prove completion"},
			{SourcePattern: "Docling PDF models and remote services", HAIControl: "operator-managed local artifact boundary", Boundary: "PDF extraction is disabled by default; HAI never downloads artifacts or enables remote parsing, OCR, table processing, plugins, or telemetry"},
		},
	},
	{
		ID: "a2a", Name: "A2A Protocol", UpstreamURL: "https://github.com/a2aproject/A2A", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10139/repos/", SourceCollection: "A2A Protocol",
		Status: StatusIntegrated, Category: "agent interoperability", IntegrationMode: "integrated controlled-planning bridge; local by default with governed fixed-ngrok opt-in",
		Capabilities: []string{"authenticated task envelopes", "bounded Agent Card capability advertisement", "non-executable planning drafts"}, RecommendedFor: []string{"reviewed peer planning", "protocol translation", "multi-agent interoperability"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Enable only with `HAI_A2A_BRIDGE_ENABLED=true`, a named owner, a separate 32+ character peer token, and an exact `/api/v1/a2a` endpoint. Keep the endpoint loopback/private by default. Fixed-ngrok exposure additionally requires `HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED=true`, production mode, disabled local-login bypass, and an endpoint whose origin exactly matches `HAI_NGROK_URL`. HAI implements only A2A 1.0-shaped JSON-RPC `SendMessage` for bounded standalone planning drafts and requires `A2A-Version: 1.0`; no peer discovery, polling, streaming, push, file input, task persistence, source refresh, approval, or execution is available.",
		Rationale:  "The bounded A2A subset gives one reviewed token-bearing peer a useful planning interface while HAI retains all workflow, source, memory, provider, approval, execution, verification, and audit authority. The optional public transport is restricted to HAI's separately governed fixed-ngrok origin. It is intentionally not a remote-agent trust channel or runtime registry.",
		VerifiedAt: "2026-07-20", VerificationNote: "OSS Insight A2A Protocol listing and current Linux Foundation a2aproject/A2A upstream were reviewed on 2026-07-20. HAI implements a restricted A2A 1.0-shaped JSON-RPC `SendMessage` planning profile and Agent Card, with an explicit bearer token. It does not claim full task-lifecycle conformance or depend on an unstable broad agent runtime.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "Agent Card discovery", HAIControl: "fixed Agent Card with separate bearer authentication; local by default and fixed-ngrok only by explicit opt-in", Boundary: "no automatic peer discovery, tool discovery, or credential negotiation"},
			{SourcePattern: "agent task envelope", HAIControl: "side-effect-free HAI planning preview", Boundary: "the bridge cannot create tasks, refresh sources, persist attempts, request approval, execute tools, or return HAI context"},
			{SourcePattern: "agent collaboration", HAIControl: "HAI workflow, approval, verification, and audit systems", Boundary: "peer output cannot become an action or completion signal"},
		},
	},
	{
		ID: "tabby", Name: "Tabby", UpstreamURL: "https://github.com/TabbyML/tabby", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10112/repos/", SourceCollection: "AI Coding Assistants",
		Status: StatusCandidate, Category: "self-hosted coding assistance", IntegrationMode: "operator-hosted editor-assistance adapter",
		Capabilities: []string{"self-hosted completion", "code context", "local model integration"}, RecommendedFor: []string{"developer assistance", "local coding experiments"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review a local deployment, workspace scope, model provider, telemetry, repository privacy, and read-only-first integration. HAI will not grant editor, terminal, or Git write authority by catalog entry.",
		Rationale:  "A self-hosted coding-assistance candidate that can support local development workflows while preserving HAI's review and execution boundaries.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Coding Assistants repository list checked on 2026-07-19; no Tabby service is configured by HAI.",
	},
	{
		ID: "letta", Name: "Letta", UpstreamURL: "https://github.com/letta-ai/letta", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10114/repos/", SourceCollection: "AI Agent Memory",
		Status: StatusReferenceOnly, Category: "agent memory patterns", IntegrationMode: "architecture reference",
		Capabilities: []string{"agent memory", "stateful context", "memory tooling"}, RecommendedFor: []string{"memory design", "retrieval experiments"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not introduce a second memory store. Port only a measured, source-linked memory capability through HAI's existing local records, review, export, and deletion controls.",
		Rationale:  "Letta provides useful memory-system patterns, but HAI must keep one editable, provenance-aware memory authority.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Agent Memory repository list checked on 2026-07-19; Letta is not installed or connected.",
	},
	{
		ID: "comfyui", Name: "ComfyUI", UpstreamURL: "https://github.com/comfyanonymous/ComfyUI", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10111/repos/", SourceCollection: "AI Image Generation",
		Status: StatusReferenceOnly, Category: "local image generation workflows", IntegrationMode: "optional reviewed artifact service",
		Capabilities: []string{"node-based image workflows", "local asset generation"}, RecommendedFor: []string{"approved visual artifacts", "image workflow design"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Keep disabled unless an approved artifact workflow defines model provenance, content controls, local storage, GPU limits, and a human publication gate.",
		Rationale:  "A capable local visual-artifact reference, but it does not expand HAI's core decision or execution authority by itself.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Image Generation repository list checked on 2026-07-19; no ComfyUI service is configured by HAI.",
	},
	{
		ID: "daytona", Name: "Daytona", UpstreamURL: "https://github.com/daytonaio/daytona", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10137/repos/", SourceCollection: "Agent Sandboxing",
		Status: StatusExcluded, Category: "unmaintained external sandbox architecture", IntegrationMode: "excluded upstream",
		Capabilities: []string{"historical isolated-workspace patterns", "historical execution-sandbox patterns"}, RecommendedFor: []string{"upstream lifecycle caution"},
		RequiresApproval: true, LocalFirstCompatible: false,
		Activation: "Do not install, connect, or recommend. Daytona's public repository states that it is no longer maintained and that its core moved private in June 2026; its hosted service also requires an account/API key and is outside HAI's local-first execution boundary.",
		Rationale:  "A discontinued public upstream and account-based external sandbox must not be represented as an eligible HAI execution option. Preserve it only as a lifecycle and sandbox-boundary caution.",
		VerifiedAt: verifiedAt, VerificationNote: "Upstream public repository reviewed on 2026-07-20: no longer maintained, with core development private; no Daytona environment is configured by HAI.",
	},
	{
		ID: "langfuse", Name: "Langfuse", UpstreamURL: "https://github.com/langfuse/langfuse", SourceCatalogURL: "https://ossinsight.io/collections/llm-devtools", SourceCollection: "LLM DevTools",
		Status: StatusIntegrated, Category: "self-hosted LLM observability", IntegrationMode: "opt-in local aggregate-trace observability bridge",
		Capabilities: []string{"local health and readiness", "aggregate control-plane traces", "OTLP/HTTP JSON export"}, RecommendedFor: []string{"local operations visibility", "model-routing audit context", "agent trace review"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Host Langfuse locally, configure a project key pair and HAI_LANGFUSE_ENABLED=true, then use the owner-only probe before an explicit aggregate operational-snapshot export. Review local retention, trace redaction, and deletion controls separately. HAI will not export prompts, task data, source records, model payloads, tokens, files, or workflow records.",
		Rationale:  "HAI now has a bounded local Langfuse bridge for explicit aggregate operational trace evidence without replacing its audit ledger or handing Langfuse routing, approval, verification, memory, workflow, or execution authority.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official repository and self-hosting documentation reviewed on 2026-07-20: active MIT core, current self-host health/readiness endpoints, project key basic authentication, and OTLP/HTTP trace ingestion. HAI implements only a local health/readiness probe plus one owner-triggered aggregate-only OTLP/JSON span. No Langfuse service, credentials, trace export, prompt, dataset, score, evaluation, callout, or cloud endpoint is configured by default.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "observability trace", HAIControl: "fixed aggregate-only operational snapshot", Boundary: "no prompt, source, file, model payload, token, workflow record, or caller-selected data is exported"},
			{SourcePattern: "trace acceptance", HAIControl: "HAI audit, approval, verification, and routing controls", Boundary: "a Langfuse trace cannot authorize, verify, route, retain memory, or execute work"},
		},
	},
	{
		ID: "mlflow", Name: "MLflow", UpstreamURL: "https://github.com/mlflow/mlflow", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10135/repos/", SourceCollection: "AI Observability",
		Status: StatusIntegrated, Category: "local evaluation evidence", IntegrationMode: "integrated opt-in local fixed-metric evaluation bridge",
		Capabilities: []string{"fixed-experiment evaluation run projection", "allowlisted metric evidence", "local model-review context"}, RecommendedFor: []string{"model evaluation evidence", "model selection review", "local experiment inventory"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Configure an operator-managed loopback or private MLflow tracking endpoint with explicit experiment-ID and metric-key allowlists. HAI reads only bounded recent run metadata and those metrics; it cannot read prompts, params, tags, datasets, artifacts, models, traces, or credentials, and cannot create, update, delete, register, deploy, or route anything.",
		Rationale:  "MLflow adds an inspectable local evidence source for model and agent evaluation runs without making it a routing authority, prompt store, model registry, workflow engine, or provider gateway.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official MLflow REST documentation and repository reviewed on 2026-07-20: the active Apache-2.0 project documents experiment and run search endpoints. HAI implements only a disabled-by-default local read-only runs-search projection with fixed experiment and metric allowlists. No MLflow tracking server, experiment, run, model, prompt, artifact, or credential is configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "MLflow experiment and run metrics", HAIControl: "model intelligence review context and audit boundary", Boundary: "only configured local experiment IDs and metric keys are projected; prompts, params, tags, datasets, artifacts, models, and traces are never requested"},
			{SourcePattern: "model registry and deployment APIs", HAIControl: "HAI local-first provider policy and approval queue", Boundary: "the bridge has no mutation endpoint and cannot alter routing, budget, model availability, workflow state, or execution"},
		},
	},
	{
		ID: "promptflow", Name: "Prompt flow", UpstreamURL: "https://github.com/microsoft/promptflow", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10141/repos/", SourceCollection: "Agent Harness",
		Status: StatusReferenceOnly, Category: "LLM flow development toolkit", IntegrationMode: "architecture reference",
		Capabilities: []string{"prompt-flow design", "flow evaluation", "trace and deployment patterns"}, RecommendedFor: []string{"LLM application lifecycle review", "flow evaluation design"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install Prompt flow, create a provider connection, enable telemetry, or run a flow from HAI. Reconsider only for a measured lifecycle or evaluation requirement that cannot be met by HAI's native task/verification path and its isolated evaluation profiles.",
		Rationale:  "Prompt flow remains MIT licensed and available, but its flow, connection, telemetry, and deployment surfaces overlap HAI's policy, routing, evaluation, audit, and workflow authority. It must not become a competing execution or provider configuration plane.",
		VerifiedAt: "2026-07-21", VerificationNote: "GitHub metadata checked on 2026-07-21: active main branch, archived=false, MIT licence, and latest push 2026-07-09. Its current README still documents provider connections and telemetry configuration. HAI has no Prompt flow package, connection, provider key, telemetry, flow, deployment, or execution integration configured.",
	},
	{
		ID: "promptfoo", Name: "Promptfoo", UpstreamURL: "https://github.com/promptfoo/promptfoo", SourceCatalogURL: "https://ossinsight.io/collections/llm-devtools", SourceCollection: "LLM DevTools",
		Status: StatusIntegrated, Category: "LLM safety regression", IntegrationMode: "integrated opt-in internal fixed-suite local evaluation bridge",
		Capabilities: []string{"prompt regression testing", "provider comparison", "synthetic high-risk action regression"}, RecommendedFor: []string{"local model safety regression", "prompt-injection regression checks", "evaluation design"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Enable only the contained `safety-evaluation` profile after reviewing one local OpenAI-compatible endpoint and its model provenance. HAI invokes a fixed six-case synthetic suite; it accepts no caller-provided provider, model, endpoint, prompt, command, source, or data. Review aggregate evidence before any separate routing or policy decision.",
		Rationale:  "HAI implements a bounded Promptfoo bridge for repeatable local prompt-injection and high-risk-action regression evidence without turning Promptfoo into an agent, data store, policy engine, or production red-team service.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official repository and documentation reviewed on 2026-07-20: MIT, active main branch, v0.121.19, local CLI/library evaluation with explicit OpenAI-compatible chat endpoints and declarative assertions. HAI pins that version in an opt-in internal runner and returns aggregate metadata only; no Promptfoo runtime, provider, real prompt, source record, telemetry export, or safety claim is configured by default.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "prompt or red-team test", HAIControl: "fixed synthetic regression suite", Boundary: "callers cannot choose prompts, datasets, providers, commands, or real account context"},
			{SourcePattern: "evaluation pass or failure", HAIControl: "model review and audit evidence", Boundary: "a score cannot change routing, policy, verification, approval, memory, workflow, or execution"},
		},
	},
	{
		ID: "airbyte", Name: "Airbyte", UpstreamURL: "https://github.com/airbytehq/airbyte", SourceCatalogURL: "https://ossinsight.io/collections/data-integration", SourceCollection: "Data Integration",
		Status: StatusIntegrated, Category: "local source and connection inventory", IntegrationMode: "integrated opt-in local Airbyte inventory adapter",
		Capabilities: []string{"approved-workspace source inventory", "connection status and schedule inventory", "source-linked sync metadata"}, RecommendedFor: []string{"connected-source readiness", "account sync health", "connector governance"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Configure a local Airbyte base URL, dedicated API key, and approved workspace UUID allowlist, then create a local-only airbyte-inventory source. HAI issues fixed, one-page read requests for source and connection metadata only; it cannot read configuration, credentials, records, selected fields, or sync results, and cannot create, change, start, stop, or delete anything in Airbyte.",
		Rationale:  "HAI can now use Airbyte as a bounded account-sync inventory without turning it into a second credential store, connector authority, source-of-truth, or workflow engine.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official Airbyte repository and API reference reviewed on 2026-07-20: active open-source data-movement project with read endpoints for sources and connections. HAI ships a disabled-by-default local inventory adapter; no Airbyte service, workspace, key, data record, connector configuration, or sync job is configured or controlled by HAI by default.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "Airbyte source or connection inventory", HAIControl: "connected-source metadata and audit records", Boundary: "fixed allowlisted workspaces and one bounded metadata page; configurations, credentials, records, selected fields, and run results are excluded"},
			{SourcePattern: "Airbyte sync management", HAIControl: "approval-gated HAI workflows", Boundary: "the inventory adapter cannot create, modify, start, stop, delete, or schedule Airbyte resources"},
		},
	},
	{
		ID: "odoo", Name: "Odoo", UpstreamURL: "https://github.com/odoo/odoo", SourceCatalogURL: "https://ossinsight.io/collections/business-management", SourceCollection: "Business Management",
		Status: StatusIntegrated, Category: "business system bridge", IntegrationMode: "integrated opt-in Odoo JSON-2 read-only source adapter",
		Capabilities: []string{"business records", "projects", "contacts", "accounting-adjacent workflows"}, RecommendedFor: []string{"business context", "project operations", "account bridge design"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Configure one operator-owned Odoo JSON-2 endpoint, API key, optional database, and fixed read-model allowlist. HAI only calls search_read; any write, financial, customer, or accounting action remains a separate approval-gated workflow.",
		Rationale:  "HAI can ingest a bounded, source-linked read-only Odoo snapshot without replacing its decision, approval, audit, or memory planes.",
		VerifiedAt: verifiedAt, VerificationNote: "Odoo JSON-2 external API checked on 2026-07-20; HAI has an opt-in adapter but no Odoo instance or credentials configured.",
	},
	{
		ID: "continue", Name: "Continue", UpstreamURL: "https://github.com/continuedev/continue", SourceCatalogURL: sourceCatalogURL,
		Status: StatusExcluded, Category: "unmaintained coding assistant", IntegrationMode: "excluded upstream",
		Capabilities: []string{"source-controlled coding checks", "PR review", "local CLI"}, RecommendedFor: []string{"coding", "repository review", "verification"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install or connect Continue. Its upstream repository now states that it is read-only and no longer actively maintained; a future replacement requires a separate review.",
		Rationale:  "A final release and archived-for-development repository are not a suitable foundation for a new HAI coding adapter.",
		VerifiedAt: verifiedAt, VerificationNote: "Upstream README rechecked on 2026-07-21: it states the repository is read-only and no longer actively maintained, despite a final 2.0.0 release.",
	},
	{
		ID: "microsoft-jarvis", Name: "Microsoft JARVIS (HuggingGPT)", UpstreamURL: "https://github.com/microsoft/JARVIS", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10098/repos/", SourceCollection: "AI Agent Frameworks",
		Status: StatusExcluded, Category: "legacy multi-model research prototype", IntegrationMode: "excluded upstream",
		Capabilities: []string{"task decomposition", "expert-model selection", "multi-model execution patterns"}, RecommendedFor: []string{"historical orchestration research"},
		RequiresApproval: true, LocalFirstCompatible: false,
		Activation: "Do not install, connect, or recommend JARVIS. Retain only its task-planning and explicit model-selection ideas as historical research; HAI's own router, runtime registry, verification, approval, and audit controls remain the only operational authority.",
		Rationale:  "The documented runtime is an outdated research stack that depends on Ubuntu 16.04, Python 3.8, text-davinci-003, Hugging Face credentials, or very large model downloads. It neither meets HAI's Windows local-first baseline nor its zero-paid-default and controlled-execution boundaries.",
		VerifiedAt: verifiedAt, VerificationNote: "Upstream README rechecked on 2026-07-21: latest announced project update is EasyTool dated 2024-01-15; setup specifies Ubuntu 16.04, Python 3.8, text-davinci-003, and up to 284 GB of model storage. No JARVIS package, service, credential, workspace, or model endpoint is configured by HAI.",
	},
	{
		ID: "cline", Name: "Cline", UpstreamURL: "https://github.com/cline/cline", SourceCatalogURL: "https://ossinsight.io/collections/llm-devtools", SourceCollection: "LLM DevTools",
		Status: StatusCandidate, Category: "interactive coding agent", IntegrationMode: "operator-configured editor extension or local bridge",
		Capabilities: []string{"interactive coding assistance", "tool-mediated workspace work", "MCP-aware development workflows"}, RecommendedFor: []string{"coding", "repository review", "developer-controlled task execution"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Keep Cline outside HAI until a reviewed, workspace-confined adapter exists. Any proposed bridge must use an explicit model provider, tool and network allowlists, a review-first change flow, and HAI approval before write-capable work.",
		Rationale:  "Active Apache-2.0 LLM-devtool project with relevant developer workflows, but its tool-mediated workspace access is high-risk and must not inherit authority from a catalog recommendation.",
		VerifiedAt: verifiedAt, VerificationNote: "GitHub repository and Apache-2.0 licence metadata checked on 2026-07-19.",
	},
	{
		ID: "opencode", Name: "OpenCode", UpstreamURL: "https://github.com/anomalyco/opencode", SourceCatalogURL: "https://ossinsight.io/collections/model-context-protocol-mcp-client", SourceCollection: "Model Context Protocol (MCP) Client",
		Status: StatusCandidate, Category: "terminal coding agent", IntegrationMode: "operator-configured local CLI or confined bridge",
		Capabilities: []string{"interactive coding assistance", "terminal-mediated workspace work", "MCP-aware development workflows"}, RecommendedFor: []string{"coding", "repository review", "developer-controlled task execution"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Keep OpenCode outside HAI until a reviewed, workspace-confined adapter exists. Any proposed bridge must use an explicit model provider, tool and network allowlists, a review-first change flow, and HAI approval before write-capable work.",
		Rationale:  "Active MIT local CLI candidate from the MCP-client collection with useful developer workflows, but terminal and workspace access must remain independently reviewed and approval-gated.",
		VerifiedAt: verifiedAt, VerificationNote: "GitHub repository metadata checked on 2026-07-19; the upstream repository reports an active anomalyco/opencode project and MIT licence.",
	},
	{
		ID: "opencode-ai-legacy", Name: "OpenCode (opencode-ai legacy)", UpstreamURL: "https://github.com/opencode-ai/opencode", SourceCatalogURL: "https://ossinsight.io/collections/model-context-protocol-mcp-client", SourceCollection: "Model Context Protocol (MCP) Client",
		Status: StatusExcluded, Category: "archived terminal coding agent", IntegrationMode: "excluded upstream",
		Capabilities: []string{"terminal coding assistance", "repository editing", "agent configuration"}, RecommendedFor: []string{"historical comparison only"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install or connect this project. It is a distinct archived repository, not an alias or predecessor of anomalyco/opencode. It cannot receive a workspace, model provider, credential, MCP server, or runtime adapter through HAI.",
		Rationale:  "The archived opencode-ai repository shares a name with HAI's active OpenCode candidate but is not the same upstream. Recording it separately prevents a deceptive name match from becoming a false readiness signal or duplicated discovery candidate.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official GitHub repository reviewed on 2026-07-20: opencode-ai/opencode is archived by its owner as of 2025-09-18. It is a distinct Go implementation from the active MIT anomalyco/opencode project. HAI has no dependency, CLI, workspace, provider credential, MCP server, or runtime adapter configured for it.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "terminal agent and repository editing", HAIControl: "catalog identity review and controlled runtime policy", Boundary: "an archived same-name upstream cannot inherit another project's review status or execution path"},
		},
	},
	{
		ID: "openhands", Name: "OpenHands", UpstreamURL: "https://github.com/OpenHands/OpenHands", RepositoryAliases: []string{"All-Hands-AI/OpenHands"}, SourceCatalogURL: sourceCatalogURL,
		Status: StatusIntegrated, Category: "external coding-agent readiness adapter", IntegrationMode: "integrated health-only endpoint adapter",
		Capabilities: []string{"configured endpoint health probe", "coding-agent setup requirements", "operator-reviewed deployment boundary"}, RecommendedFor: []string{"coding-agent readiness review", "isolated workspace planning", "operator-controlled runtime health"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set OPENHANDS_BASE_URL and an explicit RUNTIME_LAB_ALLOWED_HOSTS entry for an operator-managed health route, then use Runtime Lab to probe it. This adapter only verifies configured endpoint reachability. HAI cannot start OpenHands agents, select a backend/model, read or mount a workspace, call tools, create automations, or execute a task through it.",
		Rationale:  "The health-only adapter provides an auditable readiness boundary for an operator-managed OpenHands deployment while HAI keeps workspace scope, network access, task transport, execution, verification, and approval authority. The upstream is currently a beta Agent Canvas control center and its agent source is moving into separate repositories, so a task-execution bridge would be an unstable second control plane rather than a safe incremental capability.",
		VerifiedAt: "2026-07-21", VerificationNote: "Official OpenHands repository and GitHub metadata rechecked on 2026-07-21: active main branch, not archived, but GitHub reports licence NOASSERTION. Its README identifies the project as beta Agent Canvas, says Agent and Agent Server source live in OpenHands/software-agent-sdk, and warns that unsandboxed installation gives the agent full filesystem access. HAI implements only a disabled-by-default, allowlisted GET health probe; no OpenHands service, workspace, agent, model, tool, credential, or automation is configured by HAI.",
	},
	{
		ID: "crewai", Name: "CrewAI", UpstreamURL: "https://github.com/crewAIInc/crewAI", SourceCatalogURL: sourceCatalogURL,
		Status: StatusIntegrated, Category: "multi-agent orchestration", IntegrationMode: "integrated opt-in local two-role planning runner",
		Capabilities: []string{"role-based agents", "task orchestration", "flows"}, RecommendedFor: []string{"planning", "research", "multi-step workflows"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Enable `HAI_CREWAI_ENABLED=true` and the separate `crewai-planning` Compose profile only after configuring one reviewed local OpenAI-compatible model endpoint. HAI sends a short task plus up to eight criteria to two fixed no-tool roles, accepts one bounded schema-checked planning artifact, and retains validation, audit, approval, and all execution authority.",
		Rationale:  "The runner uses CrewAI's role/task coordination for a constrained planner-plus-reviewer draft without adopting its tools, memory, delegation, telemetry, provider discovery, or workflow control plane.",
		VerifiedAt: "2026-07-21", VerificationNote: "Official CrewAI upstream reviewed on 2026-07-21: active main branch, MIT licensed, and release 1.15.5 dated 2026-07-20. HAI's profile pins that release, disables OpenTelemetry, fixes a local model endpoint, creates no tools or memory, and returns only a reviewable plan through an owner-authenticated API.",
	},
	{
		ID: "aider", Name: "Aider", UpstreamURL: "https://github.com/Aider-AI/aider", RepositoryAliases: []string{"paul-gauthier/aider"}, SourceCatalogURL: sourceCatalogURL,
		Status: StatusCandidate, Category: "interactive coding agent", IntegrationMode: "operator-configured workspace CLI adapter",
		Capabilities: []string{"repository editing", "git-aware code changes", "model-assisted coding"}, RecommendedFor: []string{"coding", "small repository changes"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Configure a confined workspace and an explicit model provider, then implement a read-only/review-first adapter before any write-enabled workflow is considered.",
		Rationale:  "Apache-2.0 project suitable for controlled code-assistance experiments; direct repository edits remain high-risk and are not enabled by this catalog.",
		VerifiedAt: verifiedAt, VerificationNote: "Upstream repository availability and maintenance activity checked on 2026-07-19.",
	},
	{
		ID: "e2b", Name: "E2B", UpstreamURL: "https://github.com/e2b-dev/E2B", SourceCatalogURL: sourceCatalogURL,
		Status: StatusReferenceOnly, Category: "remote execution sandbox", IntegrationMode: "external sandbox SDK/service",
		Capabilities: []string{"isolated code sandbox", "agent execution environment"}, RecommendedFor: []string{"sandbox design", "isolated execution"},
		RequiresApproval: true, LocalFirstCompatible: false,
		Activation: "Do not enable by default. A separate budget, data-egress, and API-key review is required before HAI may call an external E2B sandbox.",
		Rationale:  "Its SDK is active and useful as a sandbox reference, but a hosted sandbox conflicts with HAI's local-first and EUR 0 paid-default policy until explicitly approved.",
		VerifiedAt: verifiedAt, VerificationNote: "Upstream repository and release activity checked on 2026-07-19.",
	},
	{
		ID: "autogpt", Name: "AutoGPT", UpstreamURL: "https://github.com/Significant-Gravitas/AutoGPT", SourceCatalogURL: sourceCatalogURL,
		Status: StatusLicenseReview, Category: "agent platform", IntegrationMode: "separate platform deployment",
		Capabilities: []string{"agent workflows", "continuous agents", "platform UI"}, RecommendedFor: []string{"workflow patterns"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not vendor or integrate platform code until its per-directory licensing, hosting, and security model are reviewed.",
		Rationale:  "The repository is active, but it contains differently licensed areas; HAI will not import code under an unreviewed licensing assumption.",
		VerifiedAt: verifiedAt, VerificationNote: "Upstream repository and licensing notice checked on 2026-07-19.",
	},
	{
		ID: "autogen", Name: "AutoGen", UpstreamURL: "https://github.com/microsoft/autogen", SourceCatalogURL: sourceCatalogURL,
		Status: StatusCompatibility, Category: "legacy multi-agent compatibility", IntegrationMode: "integrated transient migration-preview bridge",
		Capabilities: []string{"event-driven agent messaging", "team and delegation patterns", "MCP workbench compatibility", "structured task events"}, RecommendedFor: []string{"existing AutoGen workloads", "MCP compatibility", "migration planning"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Use HAI's owner-authenticated /api/v1/autogen-compat/preview endpoint with a 1-100 event, redacted migration sample. It normalizes only fixed event types into transient review signals. HAI does not install or execute AutoGen project code; any actual runtime bridge still needs a separate approved adapter review.",
		Rationale:  "AutoGen is maintenance mode but documents useful interoperability patterns. HAI now maps a bounded legacy event export into native review controls and open loops without creating a second runtime, persistence path, or authority channel.",
		VerifiedAt: "2026-07-21", VerificationNote: "GitHub metadata rechecked on 2026-07-21: active main branch, archived=false, licence=CC-BY-4.0. HAI's compatibility adapter has no AutoGen dependency and cannot invoke agents, models, MCP, tools, workflows, persistence, or execution.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "event-driven agent messages", HAIControl: "task events, workflow state, and immutable audit records", Boundary: "HAI owns task lifecycle and completion decisions"},
			{SourcePattern: "agent teams and delegation", HAIControl: "planner recommendations and approval-gated workflow assignments", Boundary: "no AutoGen agent can self-authorize an action"},
			{SourcePattern: "MCP Workbench", HAIControl: "agent runtime registry with trusted-server, tool, folder, and network allowlists", Boundary: "MCP tools require a reviewed adapter and the existing risk gate"},
			{SourcePattern: "code execution", HAIControl: "controlled runtime executor with workspace and approval constraints", Boundary: "no generic executor is exposed through this catalog"},
		},
	},
	{
		ID: "microsoft-agent-framework", Name: "Microsoft Agent Framework", UpstreamURL: "https://github.com/microsoft/agent-framework", SourceCatalogURL: "https://github.com/microsoft/autogen",
		SourceCollection: "Official AutoGen successor",
		Status:           StatusIntegrated, Category: "multi-agent workflow orchestration", IntegrationMode: "integrated opt-in local sequential planning runner plus transient migration-plan translator",
		Capabilities: []string{"local OpenAI-compatible planner/reviewer draft", "human-in-the-loop orchestration patterns", "checkpointing patterns", "A2A and MCP interoperability"}, RecommendedFor: []string{"AutoGen migration", "reviewed multi-agent planning", "agent interoperability planning"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Use the owner-authenticated /api/v1/autogen-compat/migration-plan endpoint with a 1-100-event redacted sample for a non-executable migration plan. Separately, enable HAI_AGENT_FRAMEWORK_ENABLED=true and the `agent-framework-planning` local Compose profile only after configuring one reviewed local OpenAI-compatible model endpoint. The runner receives a short task plus up to eight criteria, runs two fixed no-tool local roles, and returns one bounded review-only JSON draft. HAI keeps provider routing, approval, budget, source controls, audit records, emergency stop, persistence, and completion verification; no framework-owned tool execution, cloud hosting, checkpoint, peer discovery, or state authority is permitted.",
		Rationale:  "Microsoft positions Agent Framework as AutoGen's supported successor. Its current OpenAI-compatible client supports local Ollama, LM Studio, and vLLM endpoints, so HAI can use a tightly scoped local planner/reviewer without introducing a second control plane or opening tools, memory, MCP, A2A, workflow hosting, or execution.",
		VerifiedAt: "2026-07-21", VerificationNote: "Official AutoGen and Agent Framework upstream materials rechecked on 2026-07-21: AutoGen is maintenance mode; Agent Framework is MIT-licensed, production-stable, and supports local OpenAI-compatible endpoints. HAI pins Agent Framework core 1.11.0 and compatible OpenAI client 1.10.1 in an opt-in isolated runner with telemetry disabled and no tool/state authority.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "workflow checkpointing and restart", HAIControl: "workflow state machine, durable follow-up records, and verified completion", Boundary: "HAI owns state transitions and does not trust upstream completion signals"},
			{SourcePattern: "human-in-the-loop orchestration", HAIControl: "approval queue and autonomy policy", Boundary: "a framework callback cannot approve or execute a protected action"},
			{SourcePattern: "A2A and MCP interoperability", HAIControl: "reviewed adapters with named peers and local MCP preflight", Boundary: "no implicit peer discovery, process launch, or tool activation"},
			{SourcePattern: "provider middleware", HAIControl: "local-first LLM router with EUR 0 paid default", Boundary: "framework provider settings cannot bypass HAI routing or budget policy"},
		},
	},
	{
		ID: "metagpt", Name: "MetaGPT", UpstreamURL: "https://github.com/FoundationAgents/MetaGPT", SourceCatalogURL: sourceCatalogURL,
		Status: StatusExcluded, Category: "multi-agent software workflow", IntegrationMode: "reference only",
		Capabilities: []string{"role-based software workflow"}, RecommendedFor: []string{"architecture reference"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "Keep as a reference only until a new upstream review establishes current maintenance and an integration need.",
		Rationale:  "The repository remains available but its latest release and substantive push activity are materially older than the active candidates.",
		VerifiedAt: verifiedAt, VerificationNote: "Upstream repository activity checked on 2026-07-19.",
	},
	{
		ID: "litellm", Name: "LiteLLM", UpstreamURL: "https://github.com/BerriAI/litellm", SourceCatalogURL: "https://ossinsight.io/collections/ai-gateways", SourceCollection: "ai-gateways",
		Status: StatusIntegrated, Category: "self-hosted LLM gateway", IntegrationMode: "operator-hosted loopback-only gateway profile",
		Capabilities: []string{"OpenAI-compatible provider gateway", "local and cloud provider routing", "quota and spend telemetry", "model fallback"}, RecommendedFor: []string{"provider normalization", "local-first model routing", "quota observability"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set LITELLM_ENABLED=true, a separate LITELLM_API_KEY, LITELLM_MODEL_ID, and a loopback or host.docker.internal LITELLM_BASE_URL. HAI probes /v1/models with the key, rejects remote endpoints, and requires manual approval for generation.",
		Rationale:  "HAI now has a guarded local LiteLLM profile, but the proxy's upstream billing cannot be inferred from its endpoint. HAI therefore retains its EUR 0 policy, approval, audit, and model-selection controls rather than trusting the gateway.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight ai-gateways listing and upstream self-hosted proxy documentation checked on 2026-07-19.",
	},
	{
		ID: "pgvector", Name: "pgvector", UpstreamURL: "https://github.com/pgvector/pgvector", SourceCatalogURL: "https://ossinsight.io/collections/vector-database--vector-store", SourceCollection: "Vector Database & Vector Store",
		Status: StatusIntegrated, Category: "local semantic retrieval", IntegrationMode: "opt-in local pgvector retrieval adapter",
		Capabilities: []string{"vector similarity search", "embedding storage in PostgreSQL", "hybrid memory retrieval"}, RecommendedFor: []string{"semantic memory", "connected-source retrieval", "local evidence search"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set HAI_SEMANTIC_RETRIEVAL_ENABLED=true, a loopback/host.docker.internal HAI_EMBEDDING_BASE_URL, and HAI_EMBEDDING_MODEL. HAI creates the extension/table, indexes only cached source extractions, and falls back to keyword search whenever local semantic retrieval is unavailable.",
		Rationale:  "HAI now uses pgvector in its existing local PostgreSQL ownership boundary, without introducing a second memory store or a cloud embedding dependency. Owner, project, archive, and sensitivity filters remain in the SQL retrieval query.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Vector Database & Vector Store listing plus upstream pgvector 0.8.5 PostgreSQL 17 image and exact-search documentation checked on 2026-07-19.",
	},
	{
		ID: "temporal", Name: "Temporal", UpstreamURL: "https://github.com/temporalio/temporal", SourceCatalogURL: "https://ossinsight.io/collections/workflow-scheduler", SourceCollection: "Workflow Scheduler",
		Status: StatusIntegrated, Category: "durable workflow execution", IntegrationMode: "opt-in local service and narrow governed Go worker",
		Capabilities: []string{"durable workflow state", "retry handling", "scheduled work", "worker visibility"}, RecommendedFor: []string{"follow-ups", "long-running workflows", "bounded retries"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Enable the local durability Compose profile and HAI_TEMPORAL_ENABLED. The one registered worker can only run governed follow-up proposal checks; HAI remains authoritative for approval and completion decisions.",
		Rationale:  "Temporal is wired as a local restart-safe scheduling layer for one HAI-owned workflow. It is infrastructure, not an autonomous decision-maker or policy bypass.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Workflow Scheduler listing and upstream MIT-licensed release activity checked on 2026-07-19.",
	},
	{
		ID: "prefect", Name: "Prefect", UpstreamURL: "https://github.com/PrefectHQ/prefect", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10123/repos/", SourceCollection: "AI Workflow Orchestration",
		Status: StatusReferenceOnly, Category: "data workflow orchestrator", IntegrationMode: "architecture reference",
		Capabilities: []string{"scheduled flows", "retry orchestration", "work pools", "pipeline visibility"}, RecommendedFor: []string{"workflow orchestration comparison", "background-job architecture review"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install Prefect, start its server, create a work pool, configure a cloud account, or register a deployment from HAI. Reconsider only for a measured data-pipeline gap that cannot be met by HAI's bounded Temporal worker and existing governed workflow state machine.",
		Rationale:  "Prefect is active and Apache-2.0 licensed, but it would create a parallel scheduler, deployment registry, and workflow-control surface. HAI keeps Temporal as its single local durability layer until an explicit scale requirement proves otherwise.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Workflow Orchestration listing and GitHub metadata checked on 2026-07-21: active main branch, archived=false, Apache-2.0 licence, and latest push 2026-07-21. HAI has no Prefect package, server, deployment, work pool, cloud account, credential, or worker configured.",
	},
	{
		ID: "dagster", Name: "Dagster", UpstreamURL: "https://github.com/dagster-io/dagster", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10123/repos/", SourceCollection: "AI Workflow Orchestration",
		Status: StatusReferenceOnly, Category: "data asset orchestrator", IntegrationMode: "architecture reference",
		Capabilities: []string{"asset lineage", "data-pipeline orchestration", "observability", "data quality patterns"}, RecommendedFor: []string{"data asset architecture review", "lineage and scheduling comparison"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install Dagster, start a webserver or daemon, register an asset, configure an integration, or expose HAI sources to it. Reconsider only with a separately approved data-asset lineage need that cannot be represented by HAI's source provenance, audit, and Temporal-backed workflow controls.",
		Rationale:  "Dagster is active and Apache-2.0 licensed, but its asset catalog, daemon, and orchestration layer would overlap HAI's existing source provenance, task state, audit, and durability controls before a demonstrated data-platform need exists.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Workflow Orchestration listing and GitHub metadata checked on 2026-07-21: active master branch, archived=false, Apache-2.0 licence, and latest push 2026-07-21. HAI has no Dagster package, daemon, webserver, asset catalog, integration, source exposure, or credential configured.",
	},
	{
		ID: "prometheus", Name: "Prometheus", UpstreamURL: "https://github.com/prometheus/prometheus", SourceCatalogURL: "https://ossinsight.io/collections/monitoring-tool", SourceCollection: "Monitoring Tool",
		Status: StatusIntegrated, Category: "operational observability", IntegrationMode: "opt-in authenticated Prometheus exposition endpoint",
		Capabilities: []string{"service metrics", "health alert rules", "time-series queries", "local monitoring"}, RecommendedFor: []string{"runtime health", "queue metrics", "budget and throughput monitoring"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "Set HAI_PROMETHEUS_ENABLED=true and a separate HAI_PROMETHEUS_TOKEN, then configure a local collector to scrape /metrics with a bearer token. The exporter has no source-content labels and is disabled unless explicitly enabled.",
		Rationale:  "HAI now exposes a small authenticated Prometheus surface for HTTP request counts and latency. A collector remains operator-configured; Prometheus does not replace HAI's action-oriented system-status view.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Monitoring Tool listing and upstream Apache-2.0 release activity checked on 2026-07-19.",
	},
	{
		ID: "grafana", Name: "Grafana", UpstreamURL: "https://github.com/grafana/grafana", SourceCatalogURL: "https://ossinsight.io/collections/monitoring-tool", SourceCollection: "Monitoring Tool",
		Status: StatusReferenceOnly, Category: "observability visualization", IntegrationMode: "optional local dashboard",
		Capabilities: []string{"metrics visualization", "alerts", "operational dashboards"}, RecommendedFor: []string{"advanced observability", "operator diagnostics"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "Do not add Grafana until Prometheus metrics exist and the HAI system-status views cannot meet an identified advanced-observability need.",
		Rationale:  "Grafana is capable but would duplicate HAI's control-room surface unless it is justified by real metrics and advanced operator needs.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Monitoring Tool listing checked on 2026-07-19.",
	},
	{
		ID: "mcp-inspector", Name: "MCP Inspector", UpstreamURL: "https://github.com/modelcontextprotocol/inspector", SourceCatalogURL: "https://ossinsight.io/collections/model-context-protocol-mcp-client", SourceCollection: "Model Context Protocol (MCP) Client",
		Status: StatusIntegrated, Category: "MCP pre-activation validation", IntegrationMode: "HAI-owned local-only Streamable HTTP preflight",
		Capabilities: []string{"MCP handshake", "bounded tool inventory", "manual connection testing"}, RecommendedFor: []string{"MCP adapter review", "tool allowlist verification", "runtime health diagnostics"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set HAI_MCP_PREFLIGHT_ENABLED=true and configure reviewed localhost, loopback, or host.docker.internal Streamable HTTP endpoints. An admin may run initialize plus tools/list; HAI never starts a process, accepts credentials, or calls a tool.",
		Rationale:  "The upstream Inspector is a capable developer tool, but its proxy can start processes and connect broadly. HAI adopts only the useful pre-activation protocol check behind a tighter local-only boundary.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight MCP client listing and upstream Inspector architecture/security guidance checked on 2026-07-19.",
	},
	{
		ID: "langchain", Name: "LangChain", UpstreamURL: "https://github.com/langchain-ai/langchain", SourceCatalogURL: "https://ossinsight.io/collections/ai-agent-frameworks", SourceCollection: "AI Agent Frameworks and GraphRAG",
		Status: StatusReferenceOnly, Category: "reasoning and retrieval patterns", IntegrationMode: "architecture reference",
		Capabilities: []string{"tool calling patterns", "retrieval chains", "agent orchestration"}, RecommendedFor: []string{"adapter design", "retrieval design"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "Do not add as a parallel agent stack. Port only a justified capability through HAI-native Go interfaces after a concrete gap is documented.",
		Rationale:  "It is a broad ecosystem, but importing it would duplicate HAI planning, routing, memory, and tool controls without a clear operational gain.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Agent Frameworks and GraphRAG listings checked on 2026-07-19.",
	},
	{
		ID: "llamaindex", Name: "LlamaIndex", UpstreamURL: "https://github.com/run-llama/llama_index", SourceCatalogURL: "https://ossinsight.io/collections/graphrag---knowledge-graph-based-rag", SourceCollection: "GraphRAG - Knowledge Graph based RAG",
		Status: StatusReferenceOnly, Category: "retrieval and indexing patterns", IntegrationMode: "architecture reference",
		Capabilities: []string{"document indexing", "retrieval pipelines", "source-grounded context"}, RecommendedFor: []string{"connected-source ingestion", "retrieval evaluation"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "Keep as a reference until a source-retrieval gap cannot be met by HAI's native extraction, full-text, and planned pgvector path.",
		Rationale:  "Its retrieval patterns are useful, but another primary indexing framework would create duplicate memory ownership and harder provenance controls.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight GraphRAG listing checked on 2026-07-19.",
	},
	{
		ID: "cognee", Name: "Cognee", UpstreamURL: "https://github.com/topoteretes/cognee", SourceCatalogURL: "https://ossinsight.io/collections/graphrag---knowledge-graph-based-rag", SourceCollection: "GraphRAG - Knowledge Graph based RAG",
		Status: StatusReferenceOnly, Category: "knowledge graph memory", IntegrationMode: "architecture reference",
		Capabilities: []string{"knowledge graph enrichment", "semantic memory", "entity relationships"}, RecommendedFor: []string{"evidence graph design", "entity linking"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "Use only as a design reference until HAI has a verified graph-query need, source provenance model, and retention plan.",
		Rationale:  "Graph memory may help later, but adding a second knowledge system before the current memory and source layers are fully operational would increase inconsistency risk.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight GraphRAG listing checked on 2026-07-19.",
	},
	{
		ID: "qdrant", Name: "Qdrant", UpstreamURL: "https://github.com/qdrant/qdrant", SourceCatalogURL: "https://ossinsight.io/collections/vector-database--vector-store", SourceCollection: "Vector Database & Vector Store",
		Status: StatusReferenceOnly, Category: "dedicated vector database", IntegrationMode: "alternative local service",
		Capabilities: []string{"vector search", "collection management", "payload filtering"}, RecommendedFor: []string{"future high-volume semantic retrieval"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not add unless pgvector has a measured scale or capability limit. Any migration requires a provenance-preserving export, retention plan, and rollback evidence.",
		Rationale:  "Qdrant is a credible dedicated option, but is intentionally deferred to avoid two active vector stores before HAI has a demonstrated need.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Vector Database & Vector Store listing checked on 2026-07-19.",
	},
	{
		ID: "llama-cpp", Name: "llama.cpp", UpstreamURL: "https://github.com/ggml-org/llama.cpp", SourceCatalogURL: "https://ossinsight.io/collections/chatgpt-alternatives", SourceCollection: "ChatGPT Alternatives",
		Status: StatusIntegrated, Category: "local model inference", IntegrationMode: "operator-configured loopback OpenAI-compatible model server",
		Capabilities: []string{"local GGUF inference", "OpenAI-compatible server", "CPU and GPU deployment", "offline model serving"}, RecommendedFor: []string{"local-first LLM routing", "low-VRAM inference", "offline fallback"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Install and start llama.cpp outside HAI on a loopback-only endpoint, record model provenance and hardware limits, then set LLAMA_CPP_BASE_URL and LLAMA_CPP_MODEL_ID. HAI rejects non-local endpoints and requires a live /v1/models probe before routing or generation.",
		Rationale:  "HAI now owns a first-class, local-only llama.cpp provider profile in both model services. The upstream server remains operator-installed and is not active until its configured endpoint passes a live probe.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight ChatGPT Alternatives listing plus upstream MIT license and current release activity checked on 2026-07-19.",
	},
	{
		ID: "searxng", Name: "SearXNG", UpstreamURL: "https://github.com/searxng/searxng", SourceCatalogURL: "https://ossinsight.io/collections/search-engine", SourceCollection: "Search Engine",
		Status: StatusIntegrated, Category: "local public-source discovery", IntegrationMode: "operator-configured local JSON search adapter",
		Capabilities: []string{"self-hosted metasearch", "JSON source candidates", "privacy-oriented discovery", "search-engine aggregation"}, RecommendedFor: []string{"current public research", "source discovery", "grounded-answer evidence selection"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set a unique HAI_SEARXNG_SECRET, set HAI_SEARXNG_ENABLED=true, and start HAI's optional research-discovery Compose profile. The profile has no host port: only HAI's backend can reach it over an internal network, while SearXNG alone has outbound search-engine access. HAI sends bounded queries only, returns candidate sources, does not fetch pages, and does not treat snippets as verified evidence.",
		Rationale:  "HAI now has a constrained local discovery adapter and an opt-in, internal-only Compose profile for the gap between a research question and source selection. It remains disabled by default because its configured search engines receive the query, and AGPL-3.0 deployment terms must be reviewed independently.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Search Engine collection, upstream repository activity, AGPL-3.0 license, official Docker installation guidance, and the local HAI research-discovery profile checked on 2026-07-21.",
	},
	{
		ID: "playwright", Name: "Playwright", UpstreamURL: "https://github.com/microsoft/playwright", SourceCatalogURL: "https://ossinsight.io/collections/testing-tools", SourceCollection: "Testing Tools",
		Status: StatusIntegrated, Category: "controlled browser verification", IntegrationMode: "opt-in named local read-only verification worker",
		Capabilities: []string{"browser automation", "deterministic web verification", "trace artifacts", "cross-browser testing"}, RecommendedFor: []string{"web workflow verification", "regression checks", "approved browser tasks"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Use only through a reviewed adapter with named approved flows, origin allowlists, no secret capture, bounded downloads, and trace retention controls. A browser test cannot send, publish, purchase, or change accounts without the normal HAI approval gate.",
		Rationale:  "Playwright is a maintained, Apache-2.0 local testing framework that can verify an approved browser workflow. It is not a general web-execution permission.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Testing Tools listing and upstream Apache-2.0 license/current releases checked on 2026-07-19.",
	},
	{
		ID: "wasmtime", Name: "Wasmtime", UpstreamURL: "https://github.com/bytecodealliance/wasmtime", SourceCatalogURL: "https://ossinsight.io/collections/webassembly-runtime", SourceCollection: "WebAssembly Runtime",
		Status: StatusIntegrated, Category: "bounded local WASM execution", IntegrationMode: "opt-in content-addressed local WASI runner",
		Capabilities: []string{"WASM runtime", "WASI capability controls", "resource limits", "portable local execution"}, RecommendedFor: []string{"deterministic transforms", "untrusted plugin experiments", "bounded local helpers"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Enable the wasi Compose profile only after adding reviewed .wasm modules and their SHA-256 hashes to HAI_WASI_MODULES. The runner has no inherited network, preopened directories, environment, or arguments and is capped at 256 MiB, 0.5 CPU, and five seconds. Each run remains approval-gated.",
		Rationale:  "Wasmtime is a maintained Apache-2.0 runtime with Windows distributions and configurable resource controls, but sandboxing still depends on HAI's explicit capability policy and adapter implementation.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight WebAssembly Runtime listing and upstream Apache-2.0/current release documentation checked on 2026-07-19.",
	},
	{
		ID: "ortools", Name: "OR-Tools", UpstreamURL: "https://github.com/google/or-tools", SourceCatalogURL: "https://ossinsight.io/collections/optimization-solvers", SourceCollection: "Optimization Solvers",
		Status: StatusIntegrated, Category: "deterministic planning optimisation", IntegrationMode: "opt-in internal CP-SAT proposal service",
		Capabilities: []string{"constraint solving", "bounded schedule proposals", "no-overlap planning", "infeasibility evidence"}, RecommendedFor: []string{"task sequencing", "calendar suggestions", "field-job planning"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "Set HAI_PLANNING_OPTIMIZER_ENABLED=true and run the Compose optimization profile. The internal service accepts only opaque job IDs, minute windows, durations, priorities, and optional fixed starts; it returns a schedule proposal and deferred work. It has no workflow, calendar, filesystem, tool, or external-network apply endpoint.",
		Rationale:  "HAI now uses OR-Tools in a narrow local CP-SAT service that complements LLM planning with deterministic constraints. Results remain proposals; external or workflow changes still require the existing HAI planning, verification, and approval paths.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Optimization Solvers listing and upstream OR-Tools Apache-2.0 v9.15 release, CP-SAT documentation, and current Python package checked on 2026-07-19.",
	},
	{
		ID: "activepieces", Name: "Activepieces", UpstreamURL: "https://github.com/activepieces/activepieces", SourceCatalogURL: "https://ossinsight.io/collections/zapier-alternatives", SourceCollection: "Zapier Alternatives",
		Status: StatusReferenceOnly, Category: "workflow connector platform", IntegrationMode: "operator-hosted platform reference",
		Capabilities: []string{"workflow connectors", "event triggers", "MCP ecosystem", "approval-aware automation patterns"}, RecommendedFor: []string{"connector design", "workflow template research"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Keep as a reference until HAI has a specific connector gap that justifies a reviewed, narrowly scoped adapter. Do not deploy a second autonomous workflow control plane by default.",
		Rationale:  "The community edition is MIT and actively maintained, but a broad automation platform would duplicate HAI's workflow, secrets, approval, and audit responsibilities without a demonstrated gap.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Zapier Alternatives listing and upstream community/enterprise licensing split checked on 2026-07-19.",
	},
	{
		ID: "n8n", Name: "n8n", UpstreamURL: "https://github.com/n8n-io/n8n", SourceCatalogURL: "https://ossinsight.io/collections/zapier-alternatives", SourceCollection: "Zapier Alternatives",
		Status: StatusLicenseReview, Category: "workflow automation platform", IntegrationMode: "separate platform deployment",
		Capabilities: []string{"workflow automation", "integrations", "visual workflows", "self-hosting"}, RecommendedFor: []string{"connector landscape", "workflow pattern research"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not integrate or vendor n8n until the Sustainable Use License, enterprise-file restrictions, secret handling, and overlap with HAI workflow ownership are reviewed for the intended deployment.",
		Rationale:  "n8n is capable and currently maintained, but its fair-code licensing and overlapping automation control plane require an explicit legal and architecture decision before adoption.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Zapier Alternatives listing and upstream Sustainable Use License restrictions checked on 2026-07-19.",
	},
	{
		ID: "mem0", Name: "Mem0", UpstreamURL: "https://github.com/mem0ai/mem0", SourceCatalogURL: "https://ossinsight.io/collections/llm-tools", SourceCollection: "LLM Tools",
		Status: StatusReferenceOnly, Category: "agent memory patterns", IntegrationMode: "architecture reference",
		Capabilities: []string{"long-term memory", "memory consolidation", "retrieval filters", "memory lifecycle"}, RecommendedFor: []string{"memory evaluation", "consolidation design"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "Do not add as a second memory authority. Port only a measured memory capability through HAI's existing source-link, correction, retention, and deletion model after a native gap is demonstrated.",
		Rationale:  "Mem0 is an active Apache-2.0 memory project, but adopting it wholesale would split ownership of provenance, corrections, and personal-data retention.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight LLM Tools listing and upstream Apache-2.0/current release activity checked on 2026-07-19.",
	},
	{
		ID: "openmetadata", Name: "OpenMetadata", UpstreamURL: "https://github.com/open-metadata/OpenMetadata", SourceCatalogURL: "https://ossinsight.io/collections/open-source-data-catalogs", SourceCollection: "Open Source Data Catalogs",
		Status: StatusReferenceOnly, Category: "source-governance and lineage patterns", IntegrationMode: "architecture reference",
		Capabilities: []string{"metadata catalog", "data lineage", "governance", "source discovery"}, RecommendedFor: []string{"connected-source provenance", "data-quality governance"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "Use as a reference until HAI's connected-source estate has a demonstrated enterprise-scale metadata governance gap. Keep HAI's source registry and audit model authoritative for local personal data.",
		Rationale:  "OpenMetadata is actively maintained and Apache-2.0, but is a large independent control plane whose deployment would exceed HAI's current local-first scope.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Open Source Data Catalogs listing and upstream Apache-2.0/current release activity checked on 2026-07-19.",
	},
	{
		ID: "pydantic-ai", Name: "PydanticAI", UpstreamURL: "https://github.com/pydantic/pydantic-ai", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10098/repos/", SourceCollection: "AI Agent Frameworks",
		Status: StatusIntegrated, Category: "typed local planning and structured-output boundary", IntegrationMode: "integrated opt-in local structured-proposal runner",
		Capabilities: []string{"typed model outputs", "schema-first agent plans", "tool result validation", "dependency injection patterns"}, RecommendedFor: []string{"structured planning", "schema-constrained extraction", "validated agent proposals"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Enable only the typed-planning Compose profile with one operator-reviewed loopback OpenAI-compatible model. HAI sends a short task request and optional success criteria to a fixed Pydantic schema. The runner has no tools, MCP, web, file, source, memory, persistence, retry, provider-selection, approval, or execution capability; its draft remains subject to HAI validation and policy.",
		Rationale:  "The integrated local PydanticAI runner adds a constrained model-assisted planning draft without replacing HAI's deterministic planner, verifier, provider router, memory, audit, or approval control plane.",
		VerifiedAt: "2026-07-20", VerificationNote: "Upstream main and v2.13.0 release checked on 2026-07-20: MIT licence and maintained Python package. HAI pins pydantic-ai-slim[openai] 2.13.0 in an optional internal runner and exposes only one local schema-validated proposal endpoint.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "typed agent output", HAIControl: "HAI-owned schemas and verification status", Boundary: "model output remains a draft until HAI validates it"},
			{SourcePattern: "tool-capable agent", HAIControl: "runtime allowlists and approval queue", Boundary: "the adapter cannot select tools or produce side effects"},
		},
	},
	{
		ID: "localai", Name: "LocalAI", UpstreamURL: "https://github.com/mudler/LocalAI", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10109/repos/", SourceCollection: "LLM Inference Engines",
		Status: StatusIntegrated, Category: "local multimodal OpenAI-compatible inference", IntegrationMode: "integrated loopback OpenAI-compatible provider profile",
		Capabilities: []string{"local OpenAI-compatible API", "local model hosting", "multimodal serving", "CPU-capable inference"}, RecommendedFor: []string{"local model fallback", "OpenAI-compatible local endpoint", "offline multimodal preparation"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review a loopback-only LocalAI endpoint with explicit model provenance, resource limits, model allowlists, no public bind, provider health checks, and HAI's existing EUR 0 budget policy. HAI must not auto-download models, expose the endpoint, or route sensitive data before configuration review.",
		Rationale:  "HAI now implements the LocalAI provider contract alongside Ollama and llama.cpp while preserving explicit configuration, loopback-only reachability, a live probe, and local-first routing policy.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight LLM Inference Engines listing and GitHub metadata checked on 2026-07-19: active master branch, MIT licence. HAI implements only the provider profile; no LocalAI service or model is installed or configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "OpenAI-compatible server", HAIControl: "LLM router provider policy and loopback probe", Boundary: "the server cannot enable paid fallback or public egress"},
			{SourcePattern: "model catalogue", HAIControl: "operator-approved model provenance", Boundary: "HAI never downloads or selects a model implicitly"},
		},
	},
	{
		ID: "cloudquery", Name: "CloudQuery", UpstreamURL: "https://github.com/cloudquery/cloudquery", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10056/repos/", SourceCollection: "Data Integration",
		Status: StatusIntegrated, Category: "local read-only sync-summary intake", IntegrationMode: "integrated fixed-path local CloudQuery sync-summary adapter",
		Capabilities: []string{"incremental sync summaries", "source inventory signals", "cursor-safe local intake", "provenance-linked operational review"}, RecommendedFor: []string{"approved source ingestion", "sync health review", "account inventory signals"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Run CloudQuery separately with its own reviewed configuration and emit a local JSONL summary. Then enable HAI_CLOUDQUERY_SUMMARY_ENABLED, mount exactly one summary directory read-only, and register a local-only CloudQuery sync-summary source. HAI reads only completed newline-terminated summary rows from that fixed path; it never starts CloudQuery, reads its config/credentials, or accesses source/destination records.",
		Rationale:  "HAI can now turn bounded, operator-produced CloudQuery run summaries into source-linked sync health signals without creating a parallel connector, credential, destination, or data authority.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Data Integration listing and CloudQuery CLI source checked on 2026-07-20: active main branch, MPL-2.0 licence. HAI implements only a disabled-by-default local JSONL summary reader; no CloudQuery process, configuration, credentials, plugin, raw source data, or destination is installed or accessed by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "CloudQuery sync JSONL summary", HAIControl: "fixed local path, size/line limits, incremental cursor, and source audit", Boundary: "HAI never runs CloudQuery or reads config, credentials, raw source data, plugin output, or destination contents"},
			{SourcePattern: "sync health signal", HAIControl: "provenance, workflow review, memory review, and deletion controls", Boundary: "a summary does not become a fact, broad account inventory, or autonomous action without normal HAI processing"},
		},
	},
	{
		ID: "opik", Name: "Opik", UpstreamURL: "https://github.com/comet-ml/opik", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10135/repos/", SourceCollection: "AI Observability",
		Status: StatusCandidate, Category: "local trace and evaluation observability", IntegrationMode: "reviewed local telemetry adapter",
		Capabilities: []string{"LLM traces", "agent evaluation", "experiment comparison", "quality monitoring"}, RecommendedFor: []string{"local evaluation evidence", "trace review", "agent quality diagnostics"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review a local-only telemetry deployment with trace redaction, short retention, non-production fixtures first, explicit provider egress control, and an export/delete path. It cannot become HAI's audit authority or receive secrets, full personal documents, or unredacted credentials.",
		Rationale:  "Opik is a maintained Apache-2.0 local observability candidate that can complement HAI's audit records with evaluation evidence when Langfuse or OpenLLMetry do not meet a demonstrated review need.",
		VerifiedAt: "2026-07-21", VerificationNote: "Official GitHub metadata rechecked on 2026-07-21: active main branch, Apache-2.0 licence, and a same-day upstream push. No Opik service or telemetry export is configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "LLM trace", HAIControl: "redaction and audit-event policy", Boundary: "observability data cannot override HAI verification or approval decisions"},
			{SourcePattern: "evaluation dashboard", HAIControl: "source-backed metric definitions", Boundary: "metrics remain advisory and must identify their scope and freshness"},
		},
	},
	{
		ID: "deepteam", Name: "DeepTeam", UpstreamURL: "https://github.com/confident-ai/deepteam", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10138/repos/", SourceCollection: "AI Red Teaming",
		Status: StatusIntegrated, Category: "local synthetic agentic safety regression", IntegrationMode: "integrated opt-in isolated local evaluation runner",
		Capabilities: []string{"agentic vulnerability simulation", "prompt-leakage regression", "excessive-agency regression", "aggregate safety evidence"}, RecommendedFor: []string{"local model safety regression", "synthetic refusal-boundary review", "agentic safety harness review"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Set a reviewed local OpenAI-compatible model endpoint and start the `deepteam-evaluation` Compose profile. HAI runs exactly one shipped synthetic target with two fixed vulnerability types and one bounded attack method. It cannot inspect HAI, target a real agent, accept user test data, call a runtime, use a cloud model, persist raw cases, upload an assessment, or change HAI policy. Any real-system red-team plan needs a separately approved, redacted, isolated evaluation design.",
		Rationale:  "The integration provides a narrow local regression harness for agentic safety patterns while HAI retains all production workflow, data, provider, verification, approval, runtime, and audit authority.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official DeepTeam repository and package metadata reviewed on 2026-07-20: Apache-2.0, deepteam 1.0.7 supports local model callbacks, vulnerability/attack selection, and optional assessment upload. HAI pins 1.0.7 in an opt-in internal runner, calls RedTeamer with upload disabled, clears proxy settings, and returns fixed-suite aggregate metadata only. No real HAI target, account, source, runtime, model route, or action is configured.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "agentic attack simulation and risk score", HAIControl: "verification and safety review evidence", Boundary: "synthetic aggregate evidence cannot mark production work verified or alter a routing, policy, approval, or execution decision"},
			{SourcePattern: "assessment upload and generated attacks", HAIControl: "local runner containment and source privacy boundary", Boundary: "upstream assessment upload is disabled; raw attacks and generations are not returned, persisted, or exported"},
		},
	},
	{
		ID: "openspec", Name: "OpenSpec", UpstreamURL: "https://github.com/Fission-AI/OpenSpec", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10112/repos/", SourceCollection: "AI Coding Assistants",
		Status: StatusIntegrated, Category: "local read-only spec-driven planning intake", IntegrationMode: "integrated local OpenSpec change-artifact reader",
		Capabilities: []string{"change specifications", "acceptance criteria", "implementation plans", "reviewable task bundles"}, RecommendedFor: []string{"software task planning", "acceptance criteria", "reviewable coding proposals"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Create a local-only `openspec-artifacts` connected source for one selected project folder under CONNECTED_SOURCE_LOCAL_ROOT. HAI reads only active Markdown artifacts below `openspec/changes` (proposal, design, tasks, and specs) and groups them into one source-linked planning bundle per change. It does not install or run OpenSpec, inspect code outside that tree, write a repository, or authorize code edits, commits, branches, pulls, or runtime execution.",
		Rationale:  "HAI can use reviewable spec artifacts to improve task criteria and context without introducing another coding agent, source authority, or execution path.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Coding Assistants listing and OpenSpec upstream README checked on 2026-07-20: active main branch, MIT licence, artifact-guided proposal/design/tasks/spec workflow. HAI implements only a disabled-until-connected local artifact reader; no OpenSpec package, command, repository hook, or filesystem writer is installed or invoked by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "OpenSpec change artifacts", HAIControl: "selected-folder allowlist, source provenance, task criteria, and review queue", Boundary: "HAI reads no code outside active openspec/changes artifacts and the artifacts are not permission to edit code"},
			{SourcePattern: "repository workflow", HAIControl: "workspace allowlist and approval policy", Boundary: "no OpenSpec command, commit, pull request, runtime execution, or network action is implicit"},
		},
	},
	{
		ID: "claude-code-project-instructions", Name: "Claude Code project instructions", UpstreamURL: "https://github.com/anthropics/claude-code", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10124/repos/", SourceCollection: "Agent Skills & AGENTS.md",
		Status: StatusIntegrated, Category: "local read-only project agent guidance", IntegrationMode: "integrated untrusted project-instructions source reader",
		Capabilities: []string{"project-local agent guidance", "AGENTS.md intake", "CLAUDE.md intake", "source-linked planning context"}, RecommendedFor: []string{"reviewable software task planning", "project conventions", "workspace-scoped agent guidance"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Create a local-only `project-instructions` connected source for one selected folder under CONNECTED_SOURCE_LOCAL_ROOT. HAI reads only root AGENTS.md and CLAUDE.md as untrusted source records with file provenance. An operator must explicitly review and attach the content as planning context; HAI never runs commands from it, injects it into a model automatically, or lets it override policy, approvals, tool allowlists, workspace boundaries, or execution controls.",
		Rationale:  "Project-local instructions are useful context for consistent coding and review work, but they are not trusted authority. This narrow reader adds provenance and explicit review without adopting a second agent harness or allowing repository files to self-authorize HAI behavior.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight Agent Skills & AGENTS.md listing and GitHub metadata checked on 2026-07-21: anthropics/claude-code is active on main and not archived; GitHub reported no SPDX licence value in that metadata response. HAI does not install, invoke, configure, or send data to Claude Code. It implements only a local reader for the documented project-guidance file pattern.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "AGENTS.md or CLAUDE.md project guidance", HAIControl: "allowlisted local connected source with provenance", Boundary: "content is marked untrusted and is never automatic model input or execution authority"},
			{SourcePattern: "agent instruction", HAIControl: "HAI policy, approval, tool, and workspace controls", Boundary: "project text cannot relax policy, grants, runtime isolation, or emergency stop"},
		},
	},
	{
		ID: "fabric-patterns", Name: "Fabric prompt patterns", UpstreamURL: "https://github.com/danielmiessler/Fabric", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10124/repos/", SourceCollection: "Agent Skills & AGENTS.md",
		Status: StatusIntegrated, Category: "local read-only prompt-pattern review", IntegrationMode: "integrated untrusted Fabric pattern source reader",
		Capabilities: []string{"prompt-pattern library intake", "system.md provenance", "bounded local pattern review", "manual planning-context candidates"}, RecommendedFor: []string{"reviewable prompt patterns", "structured drafting patterns", "explicitly selected planning guidance"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Install or copy reviewed Fabric pattern folders locally, then create a local-only `fabric-patterns` connected source pointed at that patterns folder under CONNECTED_SOURCE_LOCAL_ROOT. HAI reads only immediate-child `system.md` files, at most 24 files of 48 KiB each, as untrusted source records with file provenance. It does not install or run Fabric, invoke a provider, automatically attach a pattern to a model, execute a pattern, or let a pattern override HAI policy, evidence, approvals, routing, tool allowlists, workspace controls, or emergency stop.",
		Rationale:  "Fabric provides a maintained, reusable pattern library, but its prompts are external instructions rather than HAI authority. A narrow local reader preserves useful operator-reviewed material without adding Fabric's CLI, provider configuration, plugins, or execution path as a parallel control plane.",
		VerifiedAt: "2026-07-22", VerificationNote: "OSS Insight Agent Skills & AGENTS.md listing and GitHub metadata checked on 2026-07-22: danielmiessler/Fabric is active on main, not archived, MIT licensed, and had an upstream push on 2026-07-16. HAI implements only a bounded local reader for manually installed pattern system.md files; no Fabric dependency, CLI, provider, plugin, updater, telemetry, command, or network call is installed or invoked.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "Fabric pattern system.md", HAIControl: "allowlisted local source extraction with provenance", Boundary: "pattern text is marked untrusted, excluded from normal automatic task context, and cannot authorize an action or support a factual claim"},
			{SourcePattern: "prompt-pattern library", HAIControl: "explicit operator review before a future attachment path", Boundary: "no upstream CLI, model/provider configuration, plugin, command, tool, or network surface is inherited"},
		},
	},
	{
		ID: "pipecat", Name: "Pipecat", UpstreamURL: "https://github.com/pipecat-ai/pipecat", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10118/repos/", SourceCollection: "Multimodal AI",
		Status: StatusCandidate, Category: "local voice and multimodal intake", IntegrationMode: "reviewed local input pipeline adapter",
		Capabilities: []string{"voice pipelines", "multimodal events", "turn detection patterns", "local transport options"}, RecommendedFor: []string{"approved voice capture", "multimodal intake", "ambient input prototypes"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review an opt-in local microphone or file-import adapter with explicit capture indicator, per-source consent, local retention, transcription provenance, redaction, pause controls, and no always-on recording default. It cannot invoke tools or contacts from a spoken instruction without HAI's standard approval path.",
		Rationale:  "Pipecat is a maintained BSD-2-Clause framework that can inform a consentful local voice-intake path, while HAI preserves the user-controlled ambient, memory, execution, and safety boundaries.",
		VerifiedAt: "2026-07-21", VerificationNote: "Official GitHub metadata rechecked on 2026-07-21: active main branch, BSD-2-Clause licence, and a same-day upstream push. No Pipecat pipeline or audio capture is enabled by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "voice event", HAIControl: "source intake permission and provenance", Boundary: "audio is never captured or retained by default"},
			{SourcePattern: "multimodal agent turn", HAIControl: "planner and approval-gated runtime", Boundary: "input interpretation cannot self-authorize action"},
		},
	},
	{
		ID: "llm-guard", Name: "LLM Guard", UpstreamURL: "https://github.com/protectai/llm-guard", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10116/repos/", SourceCollection: "AI Safety & Alignment",
		Status: StatusExcluded, Category: "LLM security toolkit", IntegrationMode: "not adopted",
		Capabilities: []string{"prompt filtering", "output filtering", "security scanning"}, RecommendedFor: []string{"safety pattern research"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not integrate. Reassess only if a maintained successor, clear data-handling model, and a demonstrated gap beyond HAI's existing redaction and validation controls are recorded.",
		Rationale:  "The current upstream is archived. HAI will not add an archived safety dependency to a control plane that must remain maintainable.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Safety & Alignment listing and GitHub metadata checked on 2026-07-19: repository reports archived=true despite an MIT licence; no LLM Guard package is installed by HAI.",
	},
	{
		ID: "openai-evals", Name: "OpenAI Evals", UpstreamURL: "https://github.com/openai/evals", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10119/repos/", SourceCollection: "AI Evaluation & Testing",
		Status: StatusLicenseReview, Category: "LLM evaluation framework", IntegrationMode: "licence-review reference",
		Capabilities: []string{"evaluation framework", "benchmark registry", "model quality patterns"}, RecommendedFor: []string{"evaluation design", "benchmark research"},
		RequiresApproval: true, LocalFirstCompatible: false,
		Activation: "Do not integrate until the missing SPDX licence signal, current dependency/maintenance model, provider egress, test-data handling, and HAI evaluation overlap are explicitly reviewed.",
		Rationale:  "The repository remains active, but its GitHub metadata does not currently provide an SPDX licence assertion. HAI holds it rather than treating its popularity as deployment approval.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Evaluation & Testing listing and GitHub metadata checked on 2026-07-19: active main branch, licence reported as NOASSERTION; no OpenAI Evals package or provider access is configured by HAI.",
	},
	{
		ID: "agentbench", Name: "AgentBench", UpstreamURL: "https://github.com/THUDM/AgentBench", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10141/repos/", SourceCollection: "Agent Harness",
		Status: StatusReferenceOnly, Category: "agent benchmark reference", IntegrationMode: "evaluation architecture reference",
		Capabilities: []string{"agent benchmark tasks", "agent evaluation taxonomy", "completion assessment patterns"}, RecommendedFor: []string{"benchmark design", "agent quality research"},
		RequiresApproval: false, LocalFirstCompatible: true,
		Activation: "Use as a reference for HAI-native, redacted evaluation fixtures only. Do not import its task environments, external services, or benchmark claims as HAI production evidence without a dedicated reproduction plan.",
		Rationale:  "AgentBench is maintained and Apache-2.0, but HAI needs task-specific, source-controlled evaluation fixtures rather than a second benchmark runtime.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Agent Harness listing and GitHub metadata checked on 2026-07-19: active main branch, Apache-2.0 licence; no AgentBench task environment is installed by HAI.",
	},
	{
		ID: "omniparser", Name: "OmniParser", UpstreamURL: "https://github.com/microsoft/OmniParser", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10113/repos/", SourceCollection: "AI Browser Agents",
		Status: StatusLicenseReview, Category: "screen parsing for GUI agents", IntegrationMode: "licence-review reference",
		Capabilities: []string{"screen parsing", "visual element detection", "GUI grounding patterns"}, RecommendedFor: []string{"screen-understanding research", "browser verification design"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not integrate until its CC-BY-4.0 distribution implications, screenshot privacy, model weights, local hardware requirements, output retention, and interaction with HAI's browser allowlists are reviewed.",
		Rationale:  "The project is active, but screen capture is sensitive and the reported licence needs an explicit product and data-handling review before it can influence HAI browser workflows.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight AI Browser Agents listing and GitHub metadata checked on 2026-07-19: active master branch, CC-BY-4.0 licence; no OmniParser model, screenshot capture, or GUI agent is installed by HAI.",
	},
	{
		ID: "mcp-servers", Name: "MCP Servers Reference", UpstreamURL: "https://github.com/modelcontextprotocol/servers", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10105/repos/", SourceCollection: "MCP Servers",
		Status: StatusLicenseReview, Category: "MCP server reference collection", IntegrationMode: "licence-review reference",
		Capabilities: []string{"MCP server examples", "tool schema patterns", "connector reference"}, RecommendedFor: []string{"MCP adapter design", "tool boundary research"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not adopt the collection as a tool bundle. Each server needs its own repository, licence, credential, network, tool allowlist, preflight, audit, rollback, and approval review before any local adapter is considered.",
		Rationale:  "The repository is active but reports no SPDX licence through GitHub metadata and contains heterogeneous server examples; a collection cannot inherit a single trust decision.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight MCP Servers listing and GitHub metadata checked on 2026-07-19: active main branch, licence reported as NOASSERTION; no MCP Servers example or tool has been installed or enabled by HAI.",
	},
	{
		ID: "evidently", Name: "Evidently", UpstreamURL: "https://github.com/evidentlyai/evidently", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10135/repos/", SourceCollection: "AI Observability",
		Status: StatusIntegrated, Category: "local AI quality evaluation and monitoring", IntegrationMode: "integrated opt-in internal report-only evaluation bridge",
		Capabilities: []string{"LLM evaluation", "RAG evaluation", "data-quality checks", "drift detection", "pass/fail test suites"}, RecommendedFor: []string{"source-grounded answer regression", "retrieval evaluation", "routing quality review", "input-quality monitoring"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "HAI includes a disabled internal report runner. Enable only the local evaluation Compose profile after reviewing synthetic/redacted fixture provenance, capacity, retention, and result review. The bridge rejects detected personal data and secrets, returns metadata only, and cannot mark an answer verified, change routing or policy, enable a provider, or execute an action.",
		Rationale:  "Evidently now contributes a contained local quality-evidence path without displacing HAI's source grounding, deterministic validators, approval gates, or audit authority.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official repository reviewed on 2026-07-20: Apache-2.0, with offline reports, test suites, and optional self-hosted monitoring. HAI ships only an opt-in internal DataSummary bridge for bounded synthetic/redacted fixtures; no service is enabled, no fixture is persisted, and no telemetry export is configured.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "evaluation report or test suite", HAIControl: "verification evidence and review queue", Boundary: "a score cannot claim completion or change policy automatically"},
			{SourcePattern: "monitoring dashboard", HAIControl: "local observability and retention policy", Boundary: "no prompt, source, or telemetry egress is implicit"},
		},
	},
	{
		ID: "whylogs", Name: "Whylogs", UpstreamURL: "https://github.com/whylabs/whylogs", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10135/repos/", SourceCollection: "AI Observability",
		Status: StatusReferenceOnly, Category: "data-quality profiling patterns", IntegrationMode: "freshness-held architecture reference",
		Capabilities: []string{"compact data profiles", "data constraints", "drift detection", "mergeable summaries"}, RecommendedFor: []string{"source-quality design", "local data-quality review", "profile-retention patterns"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install or connect Whylogs. Revisit only if a measured source-quality gap remains after HAI's bounded Evidently path, and only with a local-only profile store, an explicit source allowlist, retention/deletion policy, no external writer, disabled anonymous analytics, and a privacy review. Profiles remain diagnostic evidence and cannot verify facts, alter routing, update memory, or authorize actions.",
		Rationale:  "Whylogs supplies useful compact, mergeable profiling and constraint patterns, but its most recent public package release predates HAI's current maintenance bar and its scope overlaps the existing report-only Evidently evaluation bridge.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official upstream and package registry reviewed on 2026-07-20: Apache-2.0, mainline branch, latest PyPI release whylogs 1.6.4 on 2024-12-03. The upstream documents anonymous environment analytics enabled by default and an opt-out. HAI has no Whylogs dependency, profile, source access, service, or telemetry export configured.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "dataset profile or constraint", HAIControl: "source-quality verification evidence and review queue", Boundary: "profile output cannot establish source authority or completion automatically"},
			{SourcePattern: "usage analytics or external writer", HAIControl: "local telemetry and egress policy", Boundary: "no analytics, profile upload, or external destination is configured by HAI"},
		},
	},
	{
		ID: "livekit-agents", Name: "LiveKit Agents", UpstreamURL: "https://github.com/livekit/agents", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10118/repos/", SourceCollection: "Multimodal AI",
		Status: StatusCandidate, Category: "opt-in realtime voice and multimodal intake", IntegrationMode: "reviewed, operator-hosted realtime intake bridge",
		Capabilities: []string{"realtime voice sessions", "multimodal conversation", "MCP tool compatibility", "agent testing", "job scheduling"}, RecommendedFor: []string{"opt-in voice assistant", "accessibility intake", "real-time local interaction prototypes"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review an opt-in local or self-hosted LiveKit deployment with a visible capture state, per-session consent, explicit STT/LLM/TTS providers, retained-transcript controls, a named room allowlist, and HAI's existing tool and approval gates. It must not activate a microphone, make calls, contact anyone, or invoke MCP tools without separate HAI authorization.",
		Rationale:  "LiveKit Agents is a maintained Apache-2.0 framework for real-time multimodal interaction and can eventually provide a consentful voice front door, while HAI keeps task creation, memory, provider routing, execution, and external effects under its own controls.",
		VerifiedAt: "2026-07-21", VerificationNote: "Official GitHub metadata rechecked on 2026-07-21: active main branch, Apache-2.0 licence, and a same-day upstream push. Production still requires an explicit LiveKit URL, API key, secret, room allowlist, and consent design. No LiveKit service, room, capture device, or credentials are configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "realtime voice session", HAIControl: "source-intake consent, provenance, and pause controls", Boundary: "audio is not captured or retained by default"},
			{SourcePattern: "function or MCP tool", HAIControl: "runtime registry and approval-gated action policy", Boundary: "a spoken instruction cannot self-authorize a tool or external effect"},
		},
	},
	{
		ID: "mistral-rs", Name: "mistral.rs", UpstreamURL: "https://github.com/ericlbuehler/mistral.rs", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10109/repos/", SourceCollection: "LLM Inference Engines",
		Status: StatusIntegrated, Category: "local multimodal model serving", IntegrationMode: "integrated loopback OpenAI-compatible provider profile",
		Capabilities: []string{"local inference", "OpenAI-compatible serving", "Anthropic-compatible serving", "multimodal inputs", "hardware-aware tuning"}, RecommendedFor: []string{"local-first model experiments", "multimodal intake evaluation", "OpenAI-compatible provider compatibility"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Review one loopback-only local server with an approved model, model licence, hardware and resource limit, context window, request retention setting, file-input policy, and disabled built-in agentic tools. Set MISTRAL_RS_BASE_URL and MISTRAL_RS_MODEL_ID only after that review. HAI calls only /v1/models and /v1/chat/completions through its existing local provider probe and EUR 0 routing policy; it never starts the server or selects a model automatically.",
		Rationale:  "HAI now implements a distinct mistral.rs loopback provider profile for an explicit local inference need while retaining live probing, local-first routing, budget controls, audit, and approval gates. Its upstream agentic surfaces are not integrated.",
		VerifiedAt: verifiedAt, VerificationNote: "Official repository reviewed on 2026-07-20: MIT, active master branch, OpenAI-compatible /v1 and Anthropic-compatible Messages endpoints plus optional agentic tools. HAI implements only the loopback /v1/models and /v1/chat/completions provider profile; no mistral.rs server, model, UI, file endpoint, MCP, Skills, or built-in tool surface is configured.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "OpenAI-compatible server", HAIControl: "local provider probe and EUR 0 router", Boundary: "provider availability does not bypass model selection, budget, or task approval"},
			{SourcePattern: "agentic shell, web, or code tool", HAIControl: "controlled runtime executor", Boundary: "upstream built-in tools remain disabled and are never inherited by HAI"},
		},
	},
	{
		ID: "ag2", Name: "AG2", UpstreamURL: "https://github.com/ag2ai/ag2", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10104/repos/", SourceCollection: "AI Agent Frameworks",
		Status: StatusCompatibility, Category: "multi-agent framework compatibility", IntegrationMode: "operator-hosted compatibility bridge or migration reference",
		Capabilities: []string{"agent collaboration", "human-in-the-loop workflows", "tool-use patterns", "structured outputs", "multi-agent orchestration"}, RecommendedFor: []string{"existing AG2 workload review", "AutoGen-era migration analysis", "multi-agent interoperability research"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not make AG2 a second HAI runtime. Review only a narrow, local bridge for an existing AG2 workload with a fixed task schema, model allowlist, disabled code execution by default, workspace and network constraints, and HAI-owned audit and approval enforcement. New HAI work continues to use native workflow controls or separately reviewed successor profiles.",
		Rationale:  "AG2 remains an actively maintained, Apache-2.0 AutoGen-derived framework with useful human-in-the-loop and multi-agent patterns. It overlaps HAI's orchestration layer, so its correct role is compatibility and migration review, not a parallel autonomous control plane.",
		VerifiedAt: verifiedAt, VerificationNote: "Official repository reviewed on 2026-07-19: Apache-2.0, active main branch, now uses the ag2 package and documents multi-agent, tool, and code-execution patterns. No AG2 package, agent, model key, or code executor is installed or configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "agent cooperation and handoff", HAIControl: "workflow state, assignments, and approval queue", Boundary: "HAI owns lifecycle, approval, and completion state"},
			{SourcePattern: "code execution or registered tool", HAIControl: "controlled runtime executor and tool allowlist", Boundary: "no AG2 agent receives generic host, secret, or network authority"},
		},
	},
	{
		ID: "omega-memory", Name: "Omega Memory", UpstreamURL: "https://github.com/omega-memory/omega-memory", SourceCatalogURL: "https://ossinsight.io/collections/ai-agent-frameworks", SourceCollection: "AI Agent Frameworks",
		Status: StatusReferenceOnly, Category: "local-first cross-model memory patterns", IntegrationMode: "native memory-health reference",
		Capabilities: []string{"local-first memory storage", "on-device semantic retrieval", "cross-model memory patterns", "MCP memory client patterns"}, RecommendedFor: []string{"memory consolidation review", "local memory portability research", "cross-agent memory-boundary design"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install, start, connect, import, or enable Omega Memory or its MCP/client hooks. HAI remains the only memory, provenance, retention, and deletion authority. Any future migration must begin with an owner-selected export, a local-only schema and retention review, duplicate/provenance reconciliation, a dry run, explicit acceptance, and rollback evidence.",
		Rationale:  "Omega Memory offers useful local-first consolidation and cross-model memory patterns, but adding another active memory store would split ownership, correction, provenance, retention, and verification. HAI instead adds an owner-scoped read-only memory-health endpoint that identifies stale, ungrounded, dormant, and possible duplicate records for manual review.",
		VerifiedAt: "2026-07-21", VerificationNote: "Official upstream and license reviewed on 2026-07-21: Apache-2.0, active main branch, local SQLite/on-device embedding design, and MCP client integration patterns. HAI does not install Omega packages, databases, model files, MCP servers, hooks, or source ingestion.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "cross-model memory store", HAIControl: "owner-scoped ContextMemory plus canonical source and verification layers", Boundary: "no second persistent memory authority or automatic memory import is created"},
			{SourcePattern: "memory consolidation", HAIControl: "read-only memory-health and candidate review", Boundary: "suggestions cannot archive, merge, delete, change provenance, or verify a memory"},
			{SourcePattern: "MCP memory client", HAIControl: "reviewed local MCP bridge and source/memory access policy", Boundary: "no MCP hook receives memory content or write authority by default"},
		},
	},
	{
		ID: "ragflow", Name: "RAGFlow", UpstreamURL: "https://github.com/infiniflow/ragflow", SourceCatalogURL: "https://github.com/infiniflow/ragflow", SourceCollection: "user-provided RAG candidate",
		Status: StatusIntegrated, Category: "source-linked document retrieval and parsing", IntegrationMode: "integrated opt-in local document retrieval bridge",
		Capabilities: []string{"document parsing", "retrieval and reranking", "grounded citations", "chunk inspection", "multimodal document intake"}, RecommendedFor: []string{"document-heavy research", "evidence-linked retrieval evaluation", "complex PDF and office-document parsing"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "First measure a real document parsing or retrieval gap against HAI's existing source-ingestion path. Then review a separately deployed local instance with a named source-folder allowlist, explicit connector scopes, local model endpoints, retention/deletion/export controls, citation and chunk provenance, CPU/RAM/disk limits, and every code-execution feature disabled. Imported text remains an external retrieval index: it cannot become HAI memory, create facts, send data, or call tools without HAI verification and approval.",
		Rationale:  "RAGFlow is a current Apache-2.0, self-hostable RAG engine with document parsing, reranking, citation, and multimodal ingestion capabilities. It can strengthen document-heavy retrieval after a measured gap review, but it is too broad to become a competing source, memory, workflow, or agent control plane.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official repository reviewed on 2026-07-20: Apache-2.0, active main branch, current v0.26.4 deployment guidance, with cited retrieval and document-parsing capabilities. Its self-hosting guidance requires at least 4 CPU cores, 16 GB RAM, 50 GB disk, Docker Compose, and gVisor only for its optional code executor. HAI includes a disabled-by-default local retrieval bridge; no RAGFlow service, dataset, connector, model, or code executor is deployed or configured by HAI by default.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "document parsing and chunks", HAIControl: "source registry, provenance, and correction workflow", Boundary: "parsed material is not trusted memory or a verified claim by default"},
			{SourcePattern: "retrieval citation", HAIControl: "grounded-answer claim verification", Boundary: "a cited chunk must still be checked for support, freshness, and conflicts"},
			{SourcePattern: "agent or code-executor component", HAIControl: "approval-gated runtime registry", Boundary: "RAGFlow agent and executor features remain disabled outside a separately reviewed adapter"},
		},
	},
	{
		ID: "serena", Name: "Serena", UpstreamURL: "https://github.com/oraios/serena", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10106/repos/", SourceCollection: "Coding Agents",
		Status: StatusIntegrated, Category: "read-only semantic code context", IntegrationMode: "integrated opt-in local MCP symbol bridge",
		Capabilities: []string{"symbol-level code retrieval", "reference lookup", "language-server diagnostics", "semantic repository context"}, RecommendedFor: []string{"large repository inspection", "source-grounded code planning", "cross-file impact review", "semantic code retrieval"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "HAI ships a disabled-by-default, loopback-only Serena bridge. The owner must self-start one project-pinned HTTP endpoint, set its non-path project label, and approve an MCP probe before a user may request bounded find_symbol metadata. HAI never starts the process, activates a project, supplies credentials, or forwards generic MCP calls. Source body and hover information are disabled; editing, shell commands, files, memories, project management, diagnostics, JetBrains integration, external-project lookup, and automatic language-server installation remain unavailable. Any later write path must use HAI's separate controlled runtime and approval flow.",
		Rationale:  "The bounded bridge improves large-repository inspection through symbol-aware retrieval without creating a second coding agent or execution authority. It exposes one local read-only Serena capability while keeping all other upstream tools outside HAI's trust boundary.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official repository and Serena tool/security documentation reviewed on 2026-07-20: active main branch, MIT, latest v1.6.0 released 2026-07-16. HAI now implements a disabled local Streamable HTTP bridge that invokes only find_symbol with source body and hover data disabled. No Serena process, language server, repository mount, endpoint, or project is configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "symbol retrieval and diagnostics", HAIControl: "repository source links and code-review evidence", Boundary: "retrieved source context is read-only evidence, not an approved code change"},
			{SourcePattern: "symbolic editing, shell, and memory tools", HAIControl: "controlled runtime executor and HAI memory plane", Boundary: "those upstream tools remain disabled and cannot inherit workspace, secret, or host authority"},
		},
	},
	{
		ID: "ufo", Name: "Microsoft UFO", UpstreamURL: "https://github.com/microsoft/UFO", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10113/repos/", SourceCollection: "AI Browser Agents",
		Status: StatusReferenceOnly, Category: "Windows and multi-device agent architecture", IntegrationMode: "high-risk host-automation architecture reference",
		Capabilities: []string{"Windows UI automation", "device capability matching", "DAG orchestration", "execution recovery patterns"}, RecommendedFor: []string{"Windows automation safety research", "device capability registry design", "controlled execution architecture"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install or connect UFO as an HAI runtime. Revisit only after a separate Windows execution safety review defines an isolated user session, visible operator control, per-application allowlist, no-secret boundary, screen-capture retention policy, deterministic rollback, emergency stop, and per-action approval. Multi-device registration, GUI clicking, Win32/UIA/COM access, API keys, and model routing are all out of scope for this reference profile.",
		Rationale:  "UFO documents useful capability-matching and recovery patterns, but its Windows desktop and multi-device execution scope would bypass HAI's current controlled-runtime safety boundary if adopted directly.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official repository reviewed on 2026-07-20: active main branch; UFO2 exposes Windows UIA, Win32, and WinCOM control, while UFO3 adds multi-device orchestration and requires explicit LLM configuration. No UFO process, device agent, screen capture, UI automation, or provider credential is configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "device capability matching and DAG orchestration", HAIControl: "HAI runtime registry and workflow state", Boundary: "HAI does not auto-register devices or dispatch work to a host agent"},
			{SourcePattern: "Windows GUI and native-control automation", HAIControl: "approval-gated controlled runtime", Boundary: "no UIA, Win32, WinCOM, screenshot, or click authority is inherited"},
		},
	},
	{
		ID: "goose", Name: "Goose", UpstreamURL: "https://github.com/aaif-goose/goose", RepositoryAliases: []string{"block/goose"}, SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10106/repos/", SourceCollection: "Coding Agents",
		Status: StatusReferenceOnly, Category: "general-purpose local agent architecture", IntegrationMode: "second-control-plane reference",
		Capabilities: []string{"desktop and CLI agent patterns", "MCP extension patterns", "provider compatibility", "workflow recipes"}, RecommendedFor: []string{"local agent boundary research", "MCP extension review", "provider interoperability comparison"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not embed, install, or run Goose from HAI. Its provider, extension, desktop, CLI, API, recipe, filesystem, and execution surfaces are a separate general-purpose agent control plane. Revisit only for a narrow, fixed-schema interoperability case that preserves HAI-owned provider policy, tool allowlists, approvals, audit events, workspace limits, and emergency stop.",
		Rationale:  "Goose is an active extensible local agent with broad provider and MCP support, but its general-purpose execution model overlaps HAI's planner, runtime registry, router, and governance layers. It is valuable as a comparison source, not a runtime dependency.",
		VerifiedAt: "2026-07-20", VerificationNote: "Official repository reviewed on 2026-07-20: active main branch, Apache-2.0, latest v1.43.0 released 2026-07-14; it provides a Windows desktop app, CLI, API, provider connections, and MCP extensions. No Goose binary, extension, provider account, workspace, or API is configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "provider and MCP extension ecosystem", HAIControl: "HAI provider router and reviewed runtime registry", Boundary: "no provider key, extension, or tool trust is inherited"},
			{SourcePattern: "desktop, CLI, API, and workflow execution", HAIControl: "HAI workflow engine and approval queue", Boundary: "a general-purpose upstream agent cannot create a parallel execution path"},
		},
	},
	{
		ID: "agno", Name: "Agno", UpstreamURL: "https://github.com/agno-agi/agno", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10098/repos/", SourceCollection: "AI Agent Frameworks",
		Status: StatusReferenceOnly, Category: "broad agent-platform architecture", IntegrationMode: "second-control-plane reference",
		Capabilities: []string{"agent platform patterns", "multi-agent composition", "developer tooling"}, RecommendedFor: []string{"agent-control-plane comparison", "local-first orchestration research"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install, start, or connect Agno as an HAI runtime. Revisit only for a measured local-only capability gap with a fixed schema, named provider, no-tool default, workspace and network limits, audit trail, approval gate, and emergency stop. HAI must retain workflow state, routing, source, memory, verification, and execution authority.",
		Rationale:  "Agno is an active Apache-2.0 agent-platform project, but its broad platform scope overlaps HAI's existing workflow, provider, approval, audit, and controlled-runtime layers. It is useful for architecture comparison, not a second autonomous control plane.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Agent Frameworks listing and GitHub metadata checked on 2026-07-21: active main branch, archived=false, Apache-2.0. No Agno package, service, model, credential, tool, workspace, or agent is installed or configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "agent platform and multi-agent composition", HAIControl: "HAI workflow engine and approval queue", Boundary: "no upstream agent may create a parallel lifecycle or self-authorize work"},
			{SourcePattern: "developer tool integration", HAIControl: "reviewed runtime registry and tool allowlist", Boundary: "no host, secret, browser, filesystem, or network authority is inherited"},
		},
	},
	{
		ID: "voltagent", Name: "VoltAgent", UpstreamURL: "https://github.com/VoltAgent/voltagent", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10098/repos/", SourceCollection: "AI Agent Frameworks",
		Status: StatusReferenceOnly, Category: "TypeScript agent and observability architecture", IntegrationMode: "second-control-plane reference",
		Capabilities: []string{"agent framework patterns", "MCP integration patterns", "multi-agent collaboration", "observability patterns"}, RecommendedFor: []string{"TypeScript agent architecture comparison", "MCP and trace-boundary research"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install, start, or connect VoltAgent. Revisit only after a measured gap remains beyond HAI's reviewed MCP preflight, provider routing, audit records, and observability candidates. Any bridge must be local, fixed-schema, tool-denied by default, and preserve HAI-owned approval, redaction, retention, and emergency-stop controls.",
		Rationale:  "VoltAgent is an active MIT TypeScript agent platform, but its agent, MCP, and observability surfaces overlap HAI's controlled runtime and telemetry roadmap. A second TypeScript control plane or trace store has no demonstrated justification.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Agent Frameworks listing and GitHub metadata checked on 2026-07-21: active main branch, archived=false, MIT. No VoltAgent package, service, trace store, provider, MCP server, credential, or agent is installed or configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "MCP and agent tool integration", HAIControl: "MCP preflight and reviewed runtime adapters", Boundary: "unreviewed tools cannot be enumerated for execution or inherit credentials"},
			{SourcePattern: "agent observability", HAIControl: "HAI audit and redacted generation evidence", Boundary: "no prompt, secret, source, or action data is exported to a second telemetry authority"},
		},
	},
	{
		ID: "openai-agents-python", Name: "OpenAI Agents SDK", UpstreamURL: "https://github.com/openai/openai-agents-python", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10098/repos/", SourceCollection: "AI Agent Frameworks",
		Status: StatusReferenceOnly, Category: "multi-agent workflow architecture", IntegrationMode: "provider and control-plane reference",
		Capabilities: []string{"multi-agent workflow patterns", "handoff patterns", "agent harness architecture"}, RecommendedFor: []string{"agent handoff comparison", "provider-boundary research"},
		RequiresApproval: true, LocalFirstCompatible: false,
		Activation: "Do not install or invoke the OpenAI Agents SDK from HAI. Any future compatibility work must prove a local-first or explicitly approved provider boundary, retain HAI routing and EUR 0 policy ownership, forbid implicit cloud calls, and use a fixed non-executing schema before any runtime or tool integration is considered.",
		Rationale:  "The SDK is active and MIT licensed, but a generic agent SDK bridge would add a second agent loop and provider/billing surface while HAI defaults to local models and paid usage disabled. Its handoff patterns remain useful as reference only.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Agent Frameworks listing and GitHub metadata checked on 2026-07-21: active main branch, archived=false, MIT. No SDK dependency, OpenAI credential, model call, agent loop, tool, or handoff runtime is configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "agent handoff", HAIControl: "workflow assignment and review state", Boundary: "a handoff cannot bypass task ownership, risk, approval, or completion verification"},
			{SourcePattern: "model provider call", HAIControl: "local-first LLM router and paid-budget policy", Boundary: "no provider credential or cloud call is inherited"},
		},
	},
	{
		ID: "langroid", Name: "Langroid", UpstreamURL: "https://github.com/langroid/langroid", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10098/repos/", SourceCollection: "AI Agent Frameworks",
		Status: StatusReferenceOnly, Category: "local-model multi-agent architecture", IntegrationMode: "architecture reference",
		Capabilities: []string{"local-model patterns", "multi-agent programming", "retrieval patterns", "tool-call patterns"}, RecommendedFor: []string{"local multi-agent comparison", "retrieval design research"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not install, start, or connect Langroid as an HAI runtime. Revisit only if a concrete local-model multi-agent or retrieval gap remains after HAI's PydanticAI, CrewAI, AutoGen/AG2 compatibility, RAGFlow, and native planning paths are measured. Any bridge must be bounded, no-tool by default, and HAI-governed.",
		Rationale:  "Langroid remains active and MIT licensed with local-model and multi-agent patterns, but it duplicates existing reviewed planning, compatibility, and retrieval options. No additional runtime authority is warranted without a measured gap.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Agent Frameworks listing and GitHub metadata checked on 2026-07-21: active main branch, archived=false, MIT. No Langroid package, local model, retrieval index, tool, source connection, or agent is installed or configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "local-model and multi-agent loop", HAIControl: "HAI local router and workflow planner", Boundary: "the upstream cannot select providers, change budget, or create autonomous tasks"},
			{SourcePattern: "retrieval or tool call", HAIControl: "source provenance and reviewed runtime adapters", Boundary: "no source, memory, or tool side effect is accepted without HAI verification and approval"},
		},
	},
	{
		ID: "camel", Name: "CAMEL", UpstreamURL: "https://github.com/camel-ai/camel", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10098/repos/", SourceCollection: "AI Agent Frameworks",
		Status: StatusReferenceOnly, Category: "multi-agent research architecture", IntegrationMode: "research reference",
		Capabilities: []string{"agent-society patterns", "multi-agent coordination research", "agent evaluation patterns"}, RecommendedFor: []string{"multi-agent research comparison", "agent-safety design review"},
		RequiresApproval: true, LocalFirstCompatible: false,
		Activation: "Do not install, start, or connect CAMEL as an HAI runtime. Revisit only with a concrete, bounded research or evaluation need, synthetic/redacted fixtures, no external accounts, no tools, no execution, and a separate approval and privacy review.",
		Rationale:  "CAMEL is an active Apache-2.0 research-oriented multi-agent framework. Its broad agent-society focus is not a justified substitute for HAI's operational control plane, but its patterns can inform future evaluation design.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Agent Frameworks listing and GitHub metadata checked on 2026-07-21: active master branch, archived=false, Apache-2.0. No CAMEL package, agent, model, research dataset, tool, or runtime is installed or configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "multi-agent coordination research", HAIControl: "reviewed workflow and evaluation design", Boundary: "research patterns cannot authorize autonomous execution or external contact"},
		},
	},
	{
		ID: "mastra", Name: "Mastra", UpstreamURL: "https://github.com/mastra-ai/mastra", SourceCatalogURL: "https://api.ossinsight.io/v1/collections/10098/repos/", SourceCollection: "AI Agent Frameworks",
		Status: StatusLicenseReview, Category: "TypeScript agent framework", IntegrationMode: "licence-review reference",
		Capabilities: []string{"agent framework patterns", "workflow patterns", "tool integration patterns"}, RecommendedFor: []string{"agent framework comparison", "TypeScript workflow research"},
		RequiresApproval: true, LocalFirstCompatible: false,
		Activation: "Do not install, start, or connect Mastra. The public GitHub metadata did not provide a usable SPDX licence assertion; independently review the applicable terms, provider/data handling, and overlap with HAI before considering any local adapter.",
		Rationale:  "Mastra is active, but its unverified licence metadata and broad framework overlap block adoption. HAI records it for transparent review rather than assuming it is safe to use.",
		VerifiedAt: "2026-07-21", VerificationNote: "OSS Insight AI Agent Frameworks listing and GitHub metadata checked on 2026-07-21: active main branch, archived=false, licence=NOASSERTION. No Mastra package, service, provider, credential, tool, workflow, or agent is installed or configured by HAI.",
		ControlMappings: []ControlMapping{
			{SourcePattern: "agent framework and tool integration", HAIControl: "catalog licence and adapter review", Boundary: "no runtime activation occurs before terms and safety boundaries are independently approved"},
		},
	},
	{
		ID: "minio", Name: "MinIO", UpstreamURL: "https://github.com/minio/minio", SourceCatalogURL: "https://ossinsight.io/collections/distributed-file-storage", SourceCollection: "Distributed File Storage",
		Status: StatusExcluded, Category: "object storage", IntegrationMode: "not adopted",
		Capabilities: []string{"S3-compatible object storage", "local artifact storage"}, RecommendedFor: []string{"storage architecture reference"},
		RequiresApproval: true, LocalFirstCompatible: true,
		Activation: "Do not add to HAI. Reassess object storage only after an attachment-volume need is measured and a maintained, licence-compatible option is selected.",
		Rationale:  "The upstream repository is archived and AGPLv3, so it does not meet HAI's current maintenance and licensing adoption bar.",
		VerifiedAt: verifiedAt, VerificationNote: "OSS Insight Distributed File Storage listing and upstream archive/licensing status checked on 2026-07-19.",
	},
}

// Entries returns a deep copy so callers cannot mutate the registry.
func Entries() []Entry {
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, copyEntry(entry))
	}
	return out
}

func EntryByID(id string) (Entry, bool) {
	for _, entry := range entries {
		if entry.ID == strings.ToLower(strings.TrimSpace(id)) {
			return copyEntry(entry), true
		}
	}
	return Entry{}, false
}

func copyEntry(entry Entry) Entry {
	entry.RepositoryAliases = append([]string(nil), entry.RepositoryAliases...)
	entry.Capabilities = append([]string(nil), entry.Capabilities...)
	entry.RecommendedFor = append([]string(nil), entry.RecommendedFor...)
	entry.ControlMappings = append([]ControlMapping(nil), entry.ControlMappings...)
	if boundary, ok := IntegratedImplementationBoundary(entry.ID); ok {
		copy := boundary
		entry.Implementation = &copy
	} else {
		entry.Implementation = nil
	}
	return entry
}

// Recommend maps a task to relevant external capabilities. It never marks a
// candidate as configured, selected for execution, or safe to invoke.
func Recommend(taskType, request string) []Recommendation {
	text := strings.ToLower(taskType + " " + request)
	ids := []string{}
	if containsAny(text, "code", "coding", "repository", "repo", "pull request", "test", "build", "bug", "commit") {
		ids = append(ids, "cline", "opencode", "aider", "openhands", "qodo-pr-agent", "mini-swe-agent")
	}
	if containsAny(text, "serena", "semantic code", "symbol retrieval", "symbolic code", "cross-file impact", "language server diagnostics") {
		ids = append(ids, "serena")
	}
	if containsAny(text, "plan", "research", "workflow", "delegate", "multi-agent", "orchestr") {
		ids = append(ids, "crewai", "microsoft-agent-framework")
	}
	if containsAny(text, "ag2", "ag2 migration", "ag2 workflow") {
		ids = append(ids, "ag2")
	}
	if containsAny(text, "typed plan", "typed output", "structured plan", "structured extraction", "schema first", "plan schema", "pydantic ai", "pydanticai") {
		ids = append(ids, "pydantic-ai")
	}
	if containsAny(text, "sandbox", "isolate", "untrusted code") {
		ids = append(ids, "e2b")
	}
	if containsAny(text, "autogen", "agentchat", "magentic", "mcp workbench", "autogen migration") {
		ids = append(ids, "autogen")
	}
	if containsAny(text, "microsoft agent framework", "agent framework", "autogen successor", "agent framework migration") {
		ids = append(ids, "microsoft-agent-framework")
	}
	if containsAny(text, "provider", "model gateway", "quota", "token cost", "model routing", "litellm") {
		ids = append(ids, "litellm")
	}
	if containsAny(text, "local model", "local inference", "gguf", "llama.cpp", "llama cpp", "offline model", "ollama") {
		ids = append(ids, "ollama", "llama-cpp")
	}
	if containsAny(text, "localai", "local ai", "openai compatible local", "multimodal local model") {
		ids = append(ids, "localai")
	}
	if containsAny(text, "vllm", "high throughput", "batched inference", "serve a model") {
		ids = append(ids, "vllm")
	}
	if containsAny(text, "sglang", "structured output serving", "speculative decoding") {
		ids = append(ids, "sglang")
	}
	if containsAny(text, "mistral.rs", "mistral rs", "anthropic compatible local", "local multimodal model", "local multimodal inference") {
		ids = append(ids, "mistral-rs")
	}
	if containsAny(text, "semantic memory", "embedding", "vector search", "pgvector") {
		ids = append(ids, "pgvector")
	}
	if containsAny(text, "ragflow", "document retrieval", "document parsing", "complex pdf", "evidence retrieval", "reranking", "re-ranking") {
		ids = append(ids, "ragflow")
	}
	if containsAny(text, "odoo", "crm lead", "crm leads", "sales order", "sales orders", "business system", "business records", "accounting-adjacent", "project task", "project tasks") {
		ids = append(ids, "odoo")
	}
	if containsAny(text, "source inventory", "inventory source", "inventory a source", "inventory sources", "source ingestion", "incremental connector", "cloudquery", "read first connector", "account inventory") {
		ids = append(ids, "cloudquery")
	}
	if containsAny(text, "durable workflow", "scheduled", "follow-up", "follow up", "retry", "temporal") {
		ids = append(ids, "temporal")
	}
	if containsAny(text, "prefect", "dagster", "data workflow orchestrator", "data asset orchestrator") {
		ids = append(ids, "prefect", "dagster")
	}
	if containsAny(text, "observability", "monitoring", "metrics", "prometheus") {
		ids = append(ids, "prometheus")
	}
	if containsAny(text, "mcp inspect", "mcp health", "mcp server", "mcp inspector") {
		ids = append(ids, "mcp-inspector")
	}
	if containsAny(text, "mcp server", "create a tool server", "publish a tool", "fastmcp", "github mcp", "playwright mcp", "database mcp") {
		ids = append(ids, "fastmcp")
		if containsAny(text, "github mcp", "repository", "repo", "pull request", "issue") {
			ids = append(ids, "github-mcp-server")
		}
		if containsAny(text, "playwright mcp", "browser") {
			ids = append(ids, "playwright-mcp")
		}
		if containsAny(text, "database mcp", "database", "sql", "query") {
			ids = append(ids, "google-genai-toolbox")
		}
	}
	if containsAny(text, "browser verification", "browser test", "browser flow", "web flow", "playwright", "ui regression") {
		ids = append(ids, "playwright", "playwright-mcp")
	}
	if containsAny(text, "browser agent", "browser-use", "browser use", "web research", "browse website") {
		ids = append(ids, "browser-use", "playwright")
	}
	if containsAny(text, "microsoft ufo", "ufo windows", "windows ui automation", "desktop agentos", "multi-device agent") {
		ids = append(ids, "ufo")
	}
	if containsAny(text, "aaif goose", "goose agent", "goose mcp", "goose workflow") {
		ids = append(ids, "goose")
	}
	if containsAny(text, "guardrail", "prompt injection", "llm safety", "red team", "red-team", "jailbreak", "safety evaluation") {
		ids = append(ids, "nemo-guardrails", "garak", "promptfoo")
	}
	if containsAny(text, "gitleaks", "secret scan", "scan repository secrets", "scan repo secrets", "credential leak", "credential leaks") {
		ids = append(ids, "gitleaks")
	}
	if containsAny(text, "gosec", "go security scan", "go static security", "go source security", "go taint analysis", "secure go code") {
		ids = append(ids, "gosec")
	}
	if containsAny(text, "trivy", "configuration scan", "misconfiguration scan", "infrastructure configuration", "dockerfile security", "compose security", "terraform security", "iac security") {
		ids = append(ids, "trivy")
	}
	if containsAny(text, "grype", "vulnerability scan", "vulnerability severity", "dependency vulnerability", "vulnerabilities") {
		ids = append(ids, "grype")
	}
	if containsAny(text, "syft", "sbom", "software bill of materials", "dependency inventory", "package inventory", "dependency ecosystem") {
		ids = append(ids, "syft")
	}
	if containsAny(text, "deepteam", "red team regression", "agent red team") {
		ids = append(ids, "deepteam")
	}
	if containsAny(text, "pii", "personal data", "sensitive data", "secret redaction", "redact", "redaction", "anonymize", "anonymise", "presidio") {
		ids = append(ids, "presidio")
	}
	if containsAny(text, "schema validation", "structured output validation", "validate structured output", "output validator", "guardrails ai", "guardrails-ai") {
		ids = append(ids, "guardrails-ai")
	}
	if containsAny(text, "evaluate", "evaluation", "quality regression", "retrieval evaluation", "deepeval") {
		ids = append(ids, "deepeval", "evidently")
	}
	if containsAny(text, "opik", "evaluation traces", "experiment comparison") {
		ids = append(ids, "opik")
	}
	if containsAny(text, "model benchmark", "benchmark model", "benchmark a local model", "offline model evaluation", "lm evaluation", "lm-eval", "lm evaluation harness") {
		ids = append(ids, "lm-eval-harness")
	}
	if containsAny(text, "trace instrumentation", "trace telemetry", "open telemetry", "opentelemetry", "openllmetry", "model traces") {
		ids = append(ids, "openllmetry", "openlit", "phoenix")
	}
	if containsAny(text, "agentops", "agent ops", "promptflow", "prompt flow") {
		ids = append(ids, "agentops", "promptflow")
	}
	if containsAny(text, "voice note", "audio", "transcribe", "transcription", "speech to text", "speech-to-text") {
		ids = append(ids, "whisper-cpp")
	}
	if containsAny(text, "docling", "extract document", "document extraction", "office document", "docx", "pptx", "xlsx", "document evidence", "parse document") {
		ids = append(ids, "docling")
	}
	if containsAny(text, "voice pipeline", "multimodal intake", "pipecat", "ambient voice") {
		ids = append(ids, "pipecat", "livekit-agents")
	}
	if containsAny(text, "livekit", "realtime voice", "real-time voice", "voice session", "voice assistant") {
		ids = append(ids, "livekit-agents")
	}
	if containsAny(text, "agent to agent", "agent-to-agent", "a2a protocol", "a2a") {
		ids = append(ids, "a2a")
	}
	if containsAny(text, "tabby", "self-hosted coding assistant", "code completion") {
		ids = append(ids, "tabby")
	}
	if containsAny(text, "openspec", "spec driven", "specification", "acceptance criteria", "change plan") {
		ids = append(ids, "openspec")
	}
	if containsAny(text, "agents.md", "claude.md", "project instructions", "project guidance", "repository guidance") {
		ids = append(ids, "claude-code-project-instructions")
	}
	if containsAny(text, "fabric pattern", "fabric prompt", "prompt pattern", "prompt-pattern", "pattern library") {
		ids = append(ids, "fabric-patterns")
	}
	if containsAny(text, "letta", "agent memory", "memory consolidation", "long term memory", "long-term memory", "langmem", "omega memory", "cross model memory") {
		ids = append(ids, "letta", "langmem", "omega-memory")
	}
	if containsAny(text, "comfyui", "image generation", "generate image") {
		ids = append(ids, "comfyui")
	}
	if containsAny(text, "wasm", "webassembly", "wasi", "bounded helper") {
		ids = append(ids, "wasmtime")
	}
	if containsAny(text, "schedule optimization", "route optimization", "resource assignment", "constraint solver", "or-tools", "ortools") {
		ids = append(ids, "ortools")
	}
	if containsAny(text, "activepieces", "connector platform", "automation platform", "n8n", "mem0", "data lineage", "openmetadata", "open metadata") {
		if containsAny(text, "activepieces", "connector platform", "automation platform") {
			ids = append(ids, "activepieces")
		}
		if containsAny(text, "n8n") {
			ids = append(ids, "n8n")
		}
		if containsAny(text, "mem0") {
			ids = append(ids, "mem0")
		}
		if containsAny(text, "data lineage", "openmetadata", "open metadata") {
			ids = append(ids, "openmetadata")
		}
	}
	if containsAny(text, "daytona", "managed sandbox", "workspace sandbox") {
		ids = append(ids, "daytona")
	}
	if containsAny(text, "graphrag", "knowledge graph", "entity linking", "langchain", "llamaindex", "llama index", "cognee", "haystack", "document pipeline", "anythingllm", "anything llm", "rag workspace", "document workspace") {
		ids = append(ids, "langchain", "llamaindex", "cognee", "graphrag", "haystack")
		if containsAny(text, "anythingllm", "anything llm", "rag workspace", "document workspace") {
			ids = append(ids, "anythingllm")
		}
	}
	if containsAny(text, "qdrant", "dedicated vector database") {
		ids = append(ids, "qdrant")
	}
	seen := map[string]bool{}
	out := []Recommendation{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		entry, ok := EntryByID(id)
		if !ok {
			continue
		}
		role := "optional capability"
		if entry.Status == StatusCompatibility {
			role = "legacy compatibility only"
		} else if entry.Status == StatusIntegrated {
			role = "integrated profile; operator configuration and live probe required"
		} else if entry.Status != StatusCandidate {
			role = "reference or review only"
		}
		out = append(out, Recommendation{
			ID: id, Name: entry.Name, Status: entry.Status, Role: role,
			Rationale: entry.Rationale, RequiresApproval: entry.RequiresApproval, Activation: entry.Activation,
			ControlMappings: append([]ControlMapping(nil), entry.ControlMappings...),
		})
	}
	return out
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}
