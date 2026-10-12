package braincatalog

// integratedImplementationBoundaries is an intentionally explicit registry.
// The source paths are repository-root-relative and are verified by tests. A
// profile may be opt-in or health-only, but every StatusIntegrated entry must
// resolve to one HAI-owned route/control and one concrete source boundary.
var integratedImplementationBoundaries = map[string]ImplementationBoundary{
	"a2a":                              {"A2A compatibility bridge", "/api/v1/a2a", "backend/internal/a2abridge/service.go", "bounded local planning previews only; no peer task execution"},
	"airbyte":                          {"connected-source inventory", "/api/v1/sources", "backend/internal/source/airbyte_inventory.go", "one-page read-only metadata inventory for allowlisted workspaces"},
	"anythingllm":                      {"local retrieval adapter", "/api/v1/anythingllm", "backend/internal/anythingllm/service.go", "fixed-workspace candidate retrieval only"},
	"claude-code-project-instructions": {"project-guidance intake", "/api/v1/sources", "backend/internal/source/agent_instructions.go", "root AGENTS.md and CLAUDE.md as untrusted read-only planning context"},
	"cloudquery":                       {"local summary intake", "/api/v1/sources", "backend/internal/source/cloudquery_summary.go", "read-only operator-produced JSONL sync summaries"},
	"chatgpt-codex-mcp-daemon":         {"conversation-history task context", "/api/v1/task", "backend/internal/chatgptlogs/service.go", "model-directed loop over nine reviewed read-only tools; bounded untrusted context and no process launch"},
	"crewai":                           {"local planning review", "/api/v1/crewai", "backend/internal/crewai/service.go", "fixed no-tool planner/reviewer draft only"},
	"fabric-patterns":                  {"prompt-pattern intake", "/api/v1/sources", "backend/internal/source/fabric_patterns.go", "immediate-child local system.md files as untrusted manual-review records"},
	"deepeval":                         {"local evaluation bridge", "/api/v1/deepeval", "backend/internal/deepeval/service.go", "fixed synthetic evaluation evidence only"},
	"deepteam":                         {"local red-team bridge", "/api/v1/deepteam", "backend/internal/deepteam/service.go", "fixed synthetic safety evidence only"},
	"docling":                          {"local document extraction", "/api/v1/sources/:id/extract-documents", "backend/internal/docling/service.go", "operator-approved selected-folder extraction only"},
	"evidently":                        {"local quality report", "/api/v1/evidently", "backend/internal/evidently/service.go", "bounded redacted data-quality evidence only"},
	"fastmcp":                          {"local MCP bridge", "/api/v1/mcp-agent", "backend/internal/mcpbridge/service.go", "authenticated fixed read-only HAI tools only"},
	"garak":                            {"local safety evaluation", "/api/v1/garak", "backend/internal/garak/service.go", "fixed synthetic red-team evidence only"},
	"github-mcp-server":                {"GitHub MCP-compatible context", "/api/v1/mcp-agent/github-repositories", "backend/internal/mcpbridge/service.go", "owner-scoped repository metadata only; no GitHub writes"},
	"gitleaks":                         {"secret-scan review", "/api/v1/gitleaks", "backend/internal/gitleaks/service.go", "approved snapshot scan metadata only"},
	"gosec":                            {"Go source-security review", "/api/v1/gosec", "backend/internal/gosec/service.go", "approved vendored Go snapshot aggregate metadata only"},
	"trivy":                            {"configuration-security review", "/api/v1/trivy", "backend/internal/trivy/service.go", "approved offline configuration snapshot aggregate metadata only"},
	"grype":                            {"vulnerability-scan review", "/api/v1/grype", "backend/internal/grype/service.go", "approved offline snapshot vulnerability metadata only"},
	"google-genai-toolbox":             {"MCP tool preflight", "/api/v1/mcp-preflight", "backend/internal/mcppreflight/service.go", "initialize and tools/list only; no database call"},
	"guardrails-ai":                    {"structured-output validation", "/api/v1/guardrails", "backend/internal/guardrails/service.go", "fixed redacted schema check only"},
	"langfuse":                         {"local observability bridge", "/api/v1/langfuse", "backend/internal/langfuse/service.go", "bounded local diagnostics only"},
	"litellm":                          {"local provider gateway", "/api/v1/llm", "backend/internal/llm/policy.go", "explicit local gateway profile under canonical routing policy"},
	"llama-cpp":                        {"local provider profile", "/api/v1/llm", "backend/internal/llm/policy.go", "operator-managed loopback OpenAI-compatible endpoint only"},
	"lm-eval-harness":                  {"local benchmark bridge", "/api/v1/lm-eval", "backend/internal/lmeval/service.go", "fixed synthetic suite only"},
	"localai":                          {"local provider profile", "/api/v1/llm", "backend/internal/llm/policy.go", "operator-managed loopback OpenAI-compatible endpoint only"},
	"mcp-inspector":                    {"MCP readiness preflight", "/api/v1/mcp-preflight", "backend/internal/mcppreflight/service.go", "handshake and tool inventory only"},
	"mini-swe-agent":                   {"sandbox patch proposal", "/api/v1/mini-swe", "backend/internal/miniswe/service.go", "disposable snapshot diff proposal only"},
	"mistral-rs":                       {"local provider profile", "/api/v1/llm", "backend/internal/llm/policy.go", "operator-managed loopback OpenAI-compatible endpoint only"},
	"microsoft-agent-framework":        {"local planning review", "/api/v1/agent-framework", "backend/internal/agentframework/service.go", "fixed local planner/reviewer draft only; no tools, sources, memory, or execution"},
	"mlflow":                           {"local evaluation evidence", "/api/v1/mlflow", "backend/internal/mlflow/service.go", "allowlisted recent run metrics only"},
	"odoo":                             {"read-only business intake", "/api/v1/sources", "backend/internal/source/odoo_json2.go", "fixed-model search_read source ingestion only"},
	"ollama":                           {"local provider and updater", "/api/v1/llm", "backend/internal/llm/maintenance.go", "configured tag pull and post-verification under daily maintenance"},
	"openhands":                        {"runtime health profile", "/api/v1/runtime-lab", "backend/internal/runtimelab/remote_runtime.go", "allowlisted health probe only; task execution refused"},
	"openlit":                          {"aggregate telemetry export", "/api/v1/openlit", "backend/internal/openlit/service.go", "owner-triggered aggregate local OTLP snapshot only"},
	"openspec":                         {"planning-artifact intake", "/api/v1/sources", "backend/internal/source/openspec_artifacts.go", "active Markdown planning artifacts only"},
	"ortools":                          {"deterministic optimization", "/api/v1/planning-optimizer", "backend/internal/planningoptimizer/service.go", "bounded proposal-only schedule/resource solver"},
	"pgvector":                         {"semantic retrieval", "/api/v1/memory", "backend/internal/semantic/service.go", "owner-scoped Postgres similarity retrieval only"},
	"playwright":                       {"browser verification", "/api/v1/browser-verification", "backend/internal/browserverify/service.go", "configured browser verification evidence only"},
	"presidio":                         {"local PII detection", "/api/v1/presidio", "backend/internal/presidio/service.go", "bounded metadata-only analyzer result"},
	"prometheus":                       {"metrics exporter", "/metrics", "backend/internal/metrics/exporter.go", "opt-in bearer-protected service metrics only"},
	"promptfoo":                        {"local evaluation bridge", "/api/v1/promptfoo", "backend/internal/promptfoo/service.go", "fixed synthetic prompt evaluation only"},
	"pydantic-ai":                      {"typed planning review", "/api/v1/pydantic-ai", "backend/internal/pydanticai/service.go", "fixed no-tool typed draft only"},
	"ragflow":                          {"local retrieval bridge", "/api/v1/ragflow", "backend/internal/ragflow/service.go", "allowlisted dataset candidate retrieval only"},
	"searxng":                          {"local source discovery", "/api/v1/research", "backend/internal/research/service.go", "bounded unverified public-source candidates only"},
	"serena":                           {"semantic code context", "/api/v1/serena", "backend/internal/serena/service.go", "one local read-only symbol lookup only"},
	"sglang":                           {"local provider profile", "/api/v1/llm", "backend/internal/llm/policy.go", "operator-managed loopback OpenAI-compatible endpoint only"},
	"source-linked-knowledge-graph":    {"candidate graph and timeline", "/api/v1/sources/knowledge-graph", "backend/internal/source/service.go", "read-only owner-scoped entity/date candidates only"},
	"syft":                             {"software inventory review", "/api/v1/syft", "backend/internal/syft/service.go", "approved snapshot SBOM metadata only"},
	"temporal":                         {"workflow durability bridge", "/api/v1/temporal", "backend/internal/temporalbridge/service.go", "HAI-owned workflow state remains authoritative"},
	"vllm":                             {"local provider profile", "/api/v1/llm", "backend/internal/llm/policy.go", "operator-managed loopback OpenAI-compatible endpoint only"},
	"wasmtime":                         {"WASI helper runtime", "/api/v1/wasi", "backend/internal/wasiexec/service.go", "reviewed module execution with explicit capability limits"},
	"whisper-cpp":                      {"local transcription", "/api/v1/whispercpp", "backend/internal/whispercpp/service.go", "operator-approved local audio transcription only"},
}

// IntegratedImplementationBoundary returns a copy so a caller cannot mutate
// the catalog's declared implementation evidence.
func IntegratedImplementationBoundary(id string) (ImplementationBoundary, bool) {
	boundary, ok := integratedImplementationBoundaries[id]
	return boundary, ok
}
