package braincatalog

import "strconv"

// CollectionDisposition is the explicit outcome of screening one OSS Insight
// collection. It is intentionally broader than Entry.Status: a collection is
// a discovery category, not a component HAI may execute.
type CollectionDisposition string

const (
	CollectionRepresented CollectionDisposition = "represented_in_catalog"
	CollectionCandidate   CollectionDisposition = "review_candidate"
	CollectionReference   CollectionDisposition = "reference_only"
	CollectionDeferred    CollectionDisposition = "not_adopted"
)

// CollectionScreening makes the complete OSS Insight collection pass
// inspectable in the authenticated back office. A screening result never
// changes a runtime, connector, credential, or approval state.
type CollectionScreening struct {
	Collection      string                `json:"collection"`
	Page            int                   `json:"page"`
	Disposition     CollectionDisposition `json:"disposition"`
	RelatedEntryIDs []string              `json:"relatedEntryIds,omitempty"`
	Rationale       string                `json:"rationale"`
	SourceURL       string                `json:"sourceUrl"`
}

type collectionDecision struct {
	disposition CollectionDisposition
	entries     []string
	rationale   string
}

var ossInsightCollectionPages = [][]string{
	{"ai-gateways", "Mocking and Stubbing tools", "Documentation Generator", "Anomaly Detection Software", "BaaS", "Model Context Protocol (MCP) Client", "AI Agent Frameworks", "AI Training Observability", "Project Management", "GraphRAG - Knowledge Graph based RAG", "Vector Database & Vector Store", "3D Physics Engines"},
	{"Browser Extension Frameworks", "Go Logging Libraries", "Go Web Frameworks", "Relational Database", "WebRTC", "LLM DevTools", "Reactive Monolith Frameworks", "Open Source Data Catalogs", "ML in Rust", "Programming Language of China", "Web Scanner", "Cloud Financial Management and Resource Optimization"},
	{"Networking for Games", "Stable Diffusion Ecosystem", "ChatGPT Apps", "Vector Search Engine", "LLM Tools", "ChatGPT Alternatives", "Zapier Alternatives", "Cpp CLI Parsing", "Business Management", "Ansible DevTools", "Approximate Nearest Neighbor Library", "Optimization Solvers"},
	{"X as Code", "Robotics", "Virtual Reality", "javascript ORM", "Javascript Build Tool", "Kubernetes Tooling", "Serverless Framework", "Slack Alternative", "iOS Framework", "Key Value Database", "MLOps Tools", "Workflow Scheduler"},
	{"Data Integration", "Password Manager", "Monitoring Tool", "Configuration Management Tools", "Golang ORM", "Security Tool", "Open Source Forum Software", "Computer Science Courses", "UI Framework and UIkit", "Terminal", "TUI Framework", "Modern Data Stack"},
	{"Go Database", "Rust Database", "Segment Alternative", "API tool for developer", "Hyperledger Fabric", "Hyperledger Besu", "Hyperledger Foundation", "WYSIWYG Editor", "PaaS", "Diagram as Code", "Identity Server", "Message and Streaming"},
	{"Web3", "Finance", "Cross Platform GUI Tool", "Remote Desktop Tool", "Testing Tools", "WebAssembly Runtime", "Distributed File Storage", "Programming Language", "Javascript Charting", "CICD", "React Framework", "APM Tool"},
	{"Chaos Engineering", "Search Engine", "Text Editor", "Javascript Game Engine", "Game Engine", "Headless CMS", "Artificial Intelligence", "Github Alternative", "Graph Database", "Time Series Database", "Business Intelligence", "Javascript Framework"},
	{"Web Framework", "Low Code Development Tool", "Google Analytics Alternative", "CSS Framework", "Open Source Database", "Static Site Generator"},
	{"MCP Servers", "Coding Agents", "Vibe Coding Tools", "RAG Frameworks", "LLM Inference Engines", "LLM Fine-Tuning Tools", "AI Image Generation", "AI Coding Assistants", "AI Browser Agents", "AI Agent Memory", "LLM Gateway & Proxy", "AI Safety & Alignment"},
	{"Vector Databases", "Multimodal AI", "AI Evaluation & Testing", "Model Compression", "AI Video Generation", "AI Workflow Orchestration", "Agent Skills & AGENTS.md", "AI Infrastructure", "Edge AI", "AI Governance", "Google ADK", "Neuro-Symbolic AI"},
	{"AI FinOps", "Synthetic Data", "AI Quantitative Finance", "AI Agent Marketplace", "Knowledge Graphs for AI", "AI Observability", "AI Code Review", "Agent Sandboxing", "AI Red Teaming", "A2A Protocol", "Google ADK Python", "Agent Harness"},
}

// OSS Insight's public collection API contains newer AI-specific collections
// that are not yet visible in its 102-category web grid. These IDs make the
// provenance link point at the documented repository-list API instead of an
// inaccurate page number in the older web pagination.
var ossInsightCollectionIDs = map[string]string{
	"MCP Servers": "10105", "Coding Agents": "10106", "Vibe Coding Tools": "10107", "RAG Frameworks": "10108",
	"LLM Inference Engines": "10109", "LLM Fine-Tuning Tools": "10110", "AI Image Generation": "10111", "AI Coding Assistants": "10112",
	"AI Browser Agents": "10113", "AI Agent Memory": "10114", "LLM Gateway & Proxy": "10115", "AI Safety & Alignment": "10116",
	"Vector Databases": "10117", "Multimodal AI": "10118", "AI Evaluation & Testing": "10119", "Model Compression": "10121",
	"AI Video Generation": "10122", "AI Workflow Orchestration": "10123", "Agent Skills & AGENTS.md": "10124", "AI Infrastructure": "10125",
	"Edge AI": "10126", "AI Governance": "10127", "Google ADK": "10128", "Neuro-Symbolic AI": "10129",
	"AI FinOps": "10130", "Synthetic Data": "10131", "AI Quantitative Finance": "10132", "AI Agent Marketplace": "10133",
	"Knowledge Graphs for AI": "10134", "AI Observability": "10135", "AI Code Review": "10136", "Agent Sandboxing": "10137",
	"AI Red Teaming": "10138", "A2A Protocol": "10139", "Google ADK Python": "10140", "Agent Harness": "10141",
}

var ossInsightCollectionDecisions = map[string]collectionDecision{
	"ai-gateways":                          {CollectionRepresented, []string{"litellm"}, "A local-first provider gateway profile is already registered; every configured upstream still needs its own budget and approval review."},
	"Model Context Protocol (MCP) Client":  {CollectionRepresented, []string{"mcp-inspector", "opencode", "opencode-ai-legacy"}, "HAI has a local preflight profile and a separate review-first terminal-agent candidate; the distinct archived opencode-ai project is excluded. No MCP tool is enabled by discovery."},
	"AI Agent Frameworks":                  {CollectionCandidate, []string{"openhands", "crewai", "autogen", "ag2", "pydantic-ai", "microsoft-jarvis"}, "OpenHands has an integrated health-only readiness adapter; broader agent-framework execution remains review-first, and HAI retains policy, audit, and final execution authority. Microsoft JARVIS is recorded as an excluded historical research prototype, not an eligible runtime."},
	"GraphRAG - Knowledge Graph based RAG": {CollectionRepresented, []string{"source-linked-knowledge-graph", "langchain", "llamaindex", "cognee", "graphrag"}, "HAI now provides a deterministic, source-linked candidate graph and timeline view. It remains a review surface only: upstream graph runtimes are not installed, and graph output cannot become memory, claim support, workflow input, or action authority."},
	"Vector Database & Vector Store":       {CollectionRepresented, []string{"pgvector", "qdrant"}, "The local Postgres pgvector profile is preferred; a second active vector database is held as a reference."},
	"LLM DevTools":                         {CollectionCandidate, []string{"cline", "aider", "langfuse", "promptflow", "promptfoo"}, "Coding and LLM-quality tools require workspace confinement, trace redaction, provider selection, and an explicit adapter review. Prompt flow remains a reference because its flow, connection, telemetry, and deployment surfaces would otherwise duplicate HAI's control plane."},
	"Open Source Data Catalogs":            {CollectionReference, []string{"openmetadata"}, "Data-lineage patterns are retained for future scale; a second metadata authority is not adopted."},
	"ChatGPT Apps":                         {CollectionReference, []string{"autogpt"}, "Chat applications are not a substitute for HAI's controlled task and approval plane."},
	"Vector Search Engine":                 {CollectionReference, []string{"qdrant"}, "HAI avoids another active vector service while the existing local retrieval profile meets the demonstrated need."},
	"LLM Tools":                            {CollectionReference, []string{"langchain", "mem0", "promptfoo"}, "Useful implementation patterns remain visible without splitting HAI's memory, verification, or routing authority."},
	"ChatGPT Alternatives":                 {CollectionRepresented, []string{"llama-cpp"}, "The local inference profile supports operator-configured loopback model servers and remains disabled until live readiness is proven."},
	"Zapier Alternatives":                  {CollectionReference, []string{"activepieces", "n8n"}, "HAI does not silently introduce a second workflow control plane; licensing and ownership are separate reviews."},
	"Business Management":                  {CollectionRepresented, []string{"odoo"}, "HAI has an opt-in, read-only Odoo JSON-2 source adapter with fixed model and field allowlists. It does not import or replace an ERP, write back, or authorize financial/customer actions."},
	"Optimization Solvers":                 {CollectionRepresented, []string{"ortools"}, "The internal profile returns audited proposals only; it cannot apply schedule, calendar, filesystem, or network changes."},
	"Workflow Scheduler":                   {CollectionRepresented, []string{"temporal"}, "A durable local workflow profile is available only through a named, governed worker and existing approvals."},
	"Data Integration":                     {CollectionRepresented, []string{"airbyte", "cloudquery"}, "HAI has disabled-by-default local inventory adapters: Airbyte reads a fixed one-page source/connection inventory from allowlisted workspaces only, while CloudQuery reads a local sync summary. Both leave credentials, configuration, raw source data, destinations, and execution outside HAI's control."},
	"Security Tool":                        {CollectionRepresented, []string{"gitleaks", "gosec", "trivy", "syft", "grype"}, "HAI has owner-triggered, disabled-by-default Gitleaks secret-scan, Gosec Go source-analysis, Trivy offline configuration-review, Syft SBOM-inventory, and offline Grype vulnerability-evidence profiles for named read-only source snapshots. They return aggregate redacted evidence only; no scanner output authorizes an action, dependency change, or source fact."},
	"Monitoring Tool":                      {CollectionRepresented, []string{"prometheus", "langfuse"}, "Operational metrics are Prometheus-first; LLM trace/evaluation is a separate, opt-in review candidate."},
	"Testing Tools":                        {CollectionRepresented, []string{"playwright"}, "The registered profile is read-only verification with an origin allowlist and no secret capture."},
	"WebAssembly Runtime":                  {CollectionRepresented, []string{"wasmtime"}, "Only approved content-addressed WASI modules can use the bounded local runner."},
	"Search Engine":                        {CollectionRepresented, []string{"searxng"}, "The local search profile is discovery-only: it never fetches pages, stores facts, or upgrades evidence verification."},
	"APM Tool":                             {CollectionReference, []string{"prometheus"}, "Prometheus remains the current local observability profile; broader APM is deferred pending a measured operational gap."},
	"Distributed File Storage":             {CollectionDeferred, []string{"minio"}, "No storage platform is adopted. The previous MinIO candidate remains excluded under its recorded upstream and licence review."},
	"Low Code Development Tool":            {CollectionDeferred, []string{"n8n"}, "Low-code platforms overlap HAI's workflow authority and remain held behind licensing and architecture review."},
	"Open Source Database":                 {CollectionDeferred, []string{"pgvector", "qdrant"}, "HAI retains its existing Postgres base; a database migration requires a demonstrated reliability or scale case."},
	"MCP Servers":                          {CollectionCandidate, []string{"mcp-inspector", "fastmcp", "github-mcp-server", "playwright-mcp", "google-genai-toolbox", "mcp-servers"}, "MCP server discovery is limited to preflight and allowlist review; local tool servers, GitHub, browser, and database bridges remain review-first options, and a catalog record never launches a server, creates credentials, or calls a tool."},
	"Coding Agents":                        {CollectionCandidate, []string{"opencode", "cline", "aider", "openhands", "serena", "goose"}, "OpenHands has an integrated endpoint-health adapter only. Write-capable coding agents remain workspace-confined, review-first candidates with explicit model, tool, and network boundaries. Serena has an integrated, opt-in read-only semantic-symbol bridge; its remaining tools are unavailable to HAI. Goose remains a reference because it would otherwise create a competing general-purpose agent control plane."},
	"Vibe Coding Tools":                    {CollectionReference, []string{"cline", "opencode"}, "HAI retains its governed task and change-verification path instead of adopting a separate application-building control plane."},
	"RAG Frameworks":                       {CollectionCandidate, []string{"langchain", "llamaindex", "mem0", "letta", "langmem", "haystack", "anythingllm"}, "AnythingLLM has a bounded local vector-search evidence bridge; HAI keeps one source-linked memory plane and does not create a second memory authority."},
	"LLM Inference Engines":                {CollectionRepresented, []string{"ollama", "llama-cpp", "vllm", "sglang", "localai", "mistral-rs"}, "HAI has loopback-only provider profiles for Ollama, llama.cpp, LocalAI, vLLM, SGLang, and mistral.rs. Endpoint locality and model presence are not proof of local inference or unmetered billing; Model Intelligence stays blocked until an exact operator attestation, daily maintenance, and live probe pass. Upstream agentic surfaces are not inherited."},
	"LLM Fine-Tuning Tools":                {CollectionDeferred, nil, "Training and fine-tuning add compute, model provenance, evaluation, and data-governance obligations that are outside HAI's current execution plane."},
	"AI Image Generation":                  {CollectionReference, []string{"comfyui"}, "Image generation is a potential approved artifact workflow, not an autonomous public-publishing capability."},
	"AI Coding Assistants":                 {CollectionRepresented, []string{"tabby", "continue", "openspec"}, "HAI has a local OpenSpec artifact reader only. It never installs or runs OpenSpec, reads code outside active change artifacts, or grants source or write authority to a coding assistant."},
	"AI Browser Agents":                    {CollectionCandidate, []string{"browser-use", "playwright", "omniparser", "ufo"}, "Browser and desktop autonomy are high-risk. HAI uses an allowlisted, approval-gated verification path; UFO remains an execution-architecture reference rather than a connected Windows device agent."},
	"AI Agent Memory":                      {CollectionReference, []string{"mem0", "letta", "langmem"}, "Useful memory patterns are retained, while HAI keeps its editable, source-linked local memory plane as the only active authority."},
	"LLM Gateway & Proxy":                  {CollectionRepresented, []string{"litellm"}, "Provider normalization stays behind HAI's local-first routing, paid-budget, and approval controls."},
	"AI Safety & Alignment":                {CollectionCandidate, []string{"nemo-guardrails", "guardrails-ai", "presidio", "garak", "llm-guard"}, "Safety tools may strengthen validation and redaction, but require a data-handling and false-positive review before they influence actions; archived LLM Guard remains excluded."},
	"Vector Databases":                     {CollectionRepresented, []string{"pgvector", "qdrant"}, "HAI prefers its existing Postgres retrieval boundary and holds alternate vector services as references until scale evidence justifies them."},
	"Multimodal AI":                        {CollectionCandidate, []string{"whisper-cpp", "pipecat", "livekit-agents"}, "Local speech transcription, realtime voice, or multimodal intake can enrich approved work, but audio capture, retention, source attribution, and explicit session consent must be reviewed first."},
	"AI Evaluation & Testing":              {CollectionRepresented, []string{"promptfoo", "langfuse", "garak", "deepeval", "lm-eval-harness", "openllmetry", "opik", "openai-evals"}, "HAI has isolated fixed synthetic profiles for Promptfoo, Garak, DeepEval, and LM Evaluation Harness. Real prompts, provider credentials, and production evidence remain outside those profiles; OpenLLMetry and Opik remain review candidates, while OpenAI Evals remains held for licence review."},
	"Model Compression":                    {CollectionReference, []string{"llama-cpp"}, "Model quantization is treated as an operator model-provenance concern, not an autonomous optimization action."},
	"AI Video Generation":                  {CollectionDeferred, nil, "Video generation is not part of the current command and execution plane and would require separate resource and publication controls."},
	"AI Workflow Orchestration":            {CollectionRepresented, []string{"temporal", "n8n", "prefect", "dagster"}, "HAI uses a bounded Temporal worker. Prefect and Dagster remain explicit architecture references so their schedulers, deployments, catalogs, daemons, and integrations do not become parallel workflow control planes."},
	"Agent Skills & AGENTS.md":             {CollectionRepresented, []string{"claude-code-project-instructions", "opencode", "mcp-inspector"}, "HAI reads only root AGENTS.md and CLAUDE.md from a selected local project as untrusted, provenance-linked planning context. It owns task policy, tool allowlists, execution approval, and never automatically passes instruction text to an agent."},
	"AI Infrastructure":                    {CollectionReference, []string{"litellm", "prometheus"}, "Infrastructure patterns are useful only when they preserve HAI's small local control plane and do not introduce unreviewed cloud spend."},
	"Edge AI":                              {CollectionRepresented, []string{"ollama", "llama-cpp"}, "Local model serving remains the preferred path; hardware selection and model provenance require an explicit operator configuration."},
	"AI Governance":                        {CollectionReference, []string{"nemo-guardrails", "guardrails-ai", "presidio"}, "Governance frameworks can inform policy checks but do not replace HAI's approvals, audit records, or deterministic controls."},
	"Google ADK":                           {CollectionReference, []string{"a2a"}, "Google ADK is retained only as an interoperability reference; HAI does not create a second agent runtime foundation."},
	"Neuro-Symbolic AI":                    {CollectionDeferred, nil, "No demonstrated HAI capability gap justifies adopting a research-oriented reasoning stack at this time."},
	"AI FinOps":                            {CollectionRepresented, []string{"litellm", "langfuse", "openllmetry"}, "HAI's EUR 0 paid default and local-first router remain authoritative; external telemetry must not approve spend."},
	"Synthetic Data":                       {CollectionDeferred, nil, "Synthetic-data generation is not adopted until a concrete test-data need, privacy review, and retention plan exist."},
	"AI Quantitative Finance":              {CollectionDeferred, nil, "Financial modeling and trading frameworks are intentionally outside autonomous execution and require a separate regulated-use review."},
	"AI Agent Marketplace":                 {CollectionReference, []string{"autogen", "crewai"}, "Marketplace projects are discovery material only; HAI will not import third-party agents or their implicit permissions."},
	"Knowledge Graphs for AI":              {CollectionRepresented, []string{"source-linked-knowledge-graph", "cognee", "llamaindex", "graphrag"}, "HAI provides a bounded, source-linked candidate graph and timeline view over existing owner-scoped extractions. It does not add a second graph database, execute an upstream graph runtime, or elevate extracted strings to facts."},
	"AI Observability":                     {CollectionCandidate, []string{"agentops", "langfuse", "openllmetry", "openlit", "phoenix", "opik", "evidently", "whylogs"}, "Trace and evaluation observability is a strong candidate, subject to local hosting, redaction, retention, egress, and licence review. AgentOps remains a reference because it would duplicate HAI's audit plus optional Prometheus, Langfuse, OpenLIT, and MLflow surfaces. Whylogs is held as a stale-release profiling reference rather than a new telemetry authority."},
	"AI Code Review":                       {CollectionCandidate, []string{"promptfoo", "qodo-pr-agent", "swe-agent", "swe-bench"}, "Code review integrations remain proposal-only until repository scope, test commands, sandboxing, model egress, and write boundaries are explicitly approved. SWE-bench remains excluded as a heavyweight benchmark harness rather than a HAI runtime."},
	"Agent Sandboxing":                     {CollectionReference, []string{"e2b", "daytona", "swe-rex", "taskweaver"}, "E2B remains an external-service reference only. SWE-ReX is an active local sandbox architecture reference but its arbitrary shell and file API is not a second HAI execution path. Daytona is excluded because its public upstream is unmaintained and core moved private; archived TaskWeaver is also excluded. No collection item authorizes a broad execution environment."},
	"AI Red Teaming":                       {CollectionCandidate, []string{"promptfoo", "garak", "pyrit", "deepteam"}, "Safety testing is valuable but must use redacted fixtures, provider restrictions, and a no-write evaluation boundary; archived PyRIT remains excluded."},
	"A2A Protocol":                         {CollectionCandidate, []string{"a2a", "autogen"}, "Agent-to-agent interoperability needs a narrow, signed task envelope; discovery does not authorize remote peers or tools."},
	"Google ADK Python":                    {CollectionReference, []string{"a2a"}, "The SDK is retained as protocol reference only while HAI preserves its own planner, policy, and execution control plane."},
	"Agent Harness":                        {CollectionCandidate, []string{"autogen", "crewai", "letta", "swe-agent", "swe-bench", "taskweaver", "agentbench"}, "Harness patterns are candidates for HAI-native adapters only; no external agent may self-authorize tools, providers, or side effects, and archived upstreams remain excluded. SWE-bench remains a capacity-gated benchmark exclusion, not a worker profile."},
}

const defaultCollectionRationale = "No direct HAI adoption: this category is outside the current local-first control plane, already covered by the stack, or needs a demonstrated product gap before project-level review."

// CollectionScreenings returns a copy of the complete 138-category API snapshot.
func CollectionScreenings() []CollectionScreening {
	result := make([]CollectionScreening, 0, 138)
	for pageIndex, collections := range ossInsightCollectionPages {
		page := pageIndex + 1
		for _, collection := range collections {
			decision, ok := ossInsightCollectionDecisions[collection]
			if !ok {
				decision = collectionDecision{disposition: CollectionDeferred, rationale: defaultCollectionRationale}
			}
			sourceURL := "https://ossinsight.io/collections?page=" + strconv.Itoa(page)
			if collectionID := ossInsightCollectionIDs[collection]; collectionID != "" {
				sourceURL = "https://api.ossinsight.io/v1/collections/" + collectionID + "/repos/"
			}
			result = append(result, CollectionScreening{
				Collection: collection, Page: page, Disposition: decision.disposition,
				RelatedEntryIDs: append([]string(nil), decision.entries...), Rationale: decision.rationale,
				SourceURL: sourceURL,
			})
		}
	}
	return result
}

// CollectionScreeningSummary keeps the UI honest about how much of the
// discovery source was screened and how many categories have a tangible HAI
// catalog relationship.
type CollectionScreeningSummary struct {
	Total       int                   `json:"total"`
	Represented int                   `json:"represented"`
	Candidates  int                   `json:"candidates"`
	Reference   int                   `json:"reference"`
	Deferred    int                   `json:"deferred"`
	Entries     []CollectionScreening `json:"entries"`
}

func OSSInsightScreening() CollectionScreeningSummary {
	entries := CollectionScreenings()
	summary := CollectionScreeningSummary{Total: len(entries), Entries: entries}
	for _, entry := range entries {
		switch entry.Disposition {
		case CollectionRepresented:
			summary.Represented++
		case CollectionCandidate:
			summary.Candidates++
		case CollectionReference:
			summary.Reference++
		default:
			summary.Deferred++
		}
	}
	return summary
}
