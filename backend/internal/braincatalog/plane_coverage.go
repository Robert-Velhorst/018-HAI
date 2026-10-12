package braincatalog

// CapabilityPlane is an HAI-owned architectural layer. It describes where a
// reviewed project could contribute after a separate adapter review; it does
// not describe installed software or grant the project execution authority.
type CapabilityPlane string

const (
	PlaneThinking      CapabilityPlane = "thinking"
	PlaneMemory        CapabilityPlane = "memory"
	PlaneIntake        CapabilityPlane = "intake"
	PlaneOperations    CapabilityPlane = "operations"
	PlaneExecution     CapabilityPlane = "execution"
	PlaneVerification  CapabilityPlane = "verification"
	PlaneGovernance    CapabilityPlane = "governance"
	PlaneObservability CapabilityPlane = "observability"
)

type CapabilityPlaneEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status Status `json:"status"`
}

type CapabilityPlaneCoverage struct {
	Plane       CapabilityPlane        `json:"plane"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Integrated  int                    `json:"integrated"`
	Candidates  int                    `json:"candidates"`
	Held        int                    `json:"held"`
	Entries     []CapabilityPlaneEntry `json:"entries"`
}

type capabilityPlaneDefinition struct {
	name        string
	description string
	entryIDs    []string
}

var capabilityPlaneOrder = []CapabilityPlane{
	PlaneThinking,
	PlaneMemory,
	PlaneIntake,
	PlaneOperations,
	PlaneExecution,
	PlaneVerification,
	PlaneGovernance,
	PlaneObservability,
}

var capabilityPlaneDefinitions = map[CapabilityPlane]capabilityPlaneDefinition{
	PlaneThinking: {
		name: "Thinking and planning", description: "Local reasoning, model routing, research, and deterministic planning proposals.",
		entryIDs: []string{"ag2", "agno", "anythingllm", "autogen", "autogpt", "camel", "crewai", "goose", "langchain", "langroid", "letta", "litellm", "llama-cpp", "lm-eval-harness", "localai", "mastra", "metagpt", "microsoft-agent-framework", "microsoft-jarvis", "mistral-rs", "ollama", "openai-agents-python", "ortools", "pydantic-ai", "searxng", "sglang", "vllm", "voltagent"},
	},
	PlaneMemory: {
		name: "Memory and knowledge", description: "Source-linked retrieval, workspace context, and durable knowledge patterns.",
		entryIDs: []string{"anythingllm", "chatgpt-codex-mcp-daemon", "cognee", "graphrag", "haystack", "langchain", "langmem", "letta", "llamaindex", "mem0", "omega-memory", "pgvector", "qdrant", "ragflow", "source-linked-knowledge-graph"},
	},
	PlaneIntake: {
		name: "Source intake", description: "Read-first connector, document, search, and transcription capability candidates.",
		entryIDs: []string{"airbyte", "chatgpt-codex-mcp-daemon", "claude-code-project-instructions", "cloudquery", "docling", "fabric-patterns", "google-genai-toolbox", "livekit-agents", "omniparser", "pipecat", "ragflow", "searxng", "whisper-cpp"},
	},
	PlaneOperations: {
		name: "Operations", description: "Durable workflows, business-system bridges, and controlled operational data flows.",
		entryIDs: []string{"activepieces", "airbyte", "cloudquery", "dagster", "minio", "n8n", "odoo", "openmetadata", "prefect", "temporal"},
	},
	PlaneExecution: {
		name: "Controlled execution", description: "Scoped browser, MCP, workspace, CLI, and WASI execution patterns behind HAI controls.",
		entryIDs: []string{"a2a", "ag2", "aider", "autogen", "autogpt", "browser-use", "cline", "comfyui", "continue", "crewai", "daytona", "e2b", "fastmcp", "github-mcp-server", "goose", "google-genai-toolbox", "livekit-agents", "mcp-inspector", "mcp-servers", "metagpt", "mini-swe-agent", "microsoft-agent-framework", "opencode", "opencode-ai-legacy", "openhands", "openspec", "pipecat", "playwright", "playwright-mcp", "pydantic-ai", "serena", "swe-agent", "swe-rex", "tabby", "taskweaver", "ufo", "wasmtime"},
	},
	PlaneVerification: {
		name: "Verification and safety", description: "Evaluation, redaction, guardrails, code review, and adversarial testing before completion or action.",
		entryIDs: []string{"agentbench", "browser-use", "deepeval", "deepteam", "evidently", "garak", "gitleaks", "gosec", "grype", "guardrails-ai", "langfuse", "llm-guard", "lm-eval-harness", "nemo-guardrails", "openai-evals", "opik", "presidio", "promptfoo", "pyrit", "qodo-pr-agent", "source-linked-knowledge-graph", "swe-bench", "syft", "trivy"},
	},
	PlaneGovernance: {
		name: "Governance and boundaries", description: "Approval, protocol, policy, and data-boundary patterns that remain HAI-owned.",
		entryIDs: []string{"a2a", "autogen", "fastmcp", "guardrails-ai", "llm-guard", "mcp-inspector", "mcp-servers", "microsoft-agent-framework", "openmetadata", "presidio", "pydantic-ai"},
	},
	PlaneObservability: {
		name: "Observability", description: "Local metrics, traces, evaluations, and diagnostics for inspectable agent behavior.",
		entryIDs: []string{"agentops", "evidently", "grafana", "langfuse", "mlflow", "openlit", "openllmetry", "opik", "phoenix", "prometheus", "promptflow", "whylogs"},
	},
}

// CapabilityPlaneCoverageReport makes the catalog's architectural fit
// inspectable without creating a second runtime or changing any profile state.
func CapabilityPlaneCoverageReport() []CapabilityPlaneCoverage {
	coverage := make([]CapabilityPlaneCoverage, 0, len(capabilityPlaneOrder))
	for _, plane := range capabilityPlaneOrder {
		definition := capabilityPlaneDefinitions[plane]
		item := CapabilityPlaneCoverage{Plane: plane, Name: definition.name, Description: definition.description}
		for _, id := range definition.entryIDs {
			entry, ok := EntryByID(id)
			if !ok {
				continue
			}
			item.Entries = append(item.Entries, CapabilityPlaneEntry{ID: entry.ID, Name: entry.Name, Status: entry.Status})
			switch entry.Status {
			case StatusIntegrated:
				item.Integrated++
			case StatusCandidate, StatusCompatibility:
				item.Candidates++
			default:
				item.Held++
			}
		}
		coverage = append(coverage, item)
	}
	return coverage
}
