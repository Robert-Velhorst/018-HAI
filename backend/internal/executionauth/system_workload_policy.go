package executionauth

import "fmt"

// systemWorkloadPolicy is server-owned classification for a built-in actor.
// System callers may describe an effect, but they cannot choose the authority,
// autonomy, or risk classification used to authorize it.
type systemWorkloadPolicy struct {
	id                string
	actorIdentity     string
	action            string
	stage             Stage
	resourceType      string
	toolID            string
	runtimeID         string
	requireRuntimeID  bool
	requiredAuthority int
	requestedAutonomy int
	risk              RiskLevel
	reversible        bool
	maximumCostEUR    float64
}

var builtInSystemWorkloadPolicies = []systemWorkloadPolicy{
	{
		id: "phase2-local-safe-worker-v1", actorIdentity: "system:phase2-safe-worker",
		action: "executionbroker.local-safe-worker.write", stage: StageExecution,
		resourceType: "executionbroker.final-effect", toolID: "phase2-local-safe-worker",
		runtimeID: "hai-local-safe-worker", requiredAuthority: 1, requestedAutonomy: 8,
		risk: RiskLow, reversible: true, maximumCostEUR: 0,
	},
	{
		id: "task-agent-runtime-v1", actorIdentity: "hai-task-engine",
		action: AgentRuntimeExecuteAction, stage: StageExecution,
		resourceType: AgentRuntimeResourceType, toolID: "automation-agent-runtime",
		requireRuntimeID: true, requiredAuthority: 6, requestedAutonomy: 6,
		risk: RiskHigh, reversible: false, maximumCostEUR: 0,
	},
	{
		id: "task-automation-api-read-autonomous-v1", actorIdentity: "hai-task-engine",
		action: "automation.api.read", stage: StageDataAccess,
		resourceType: "automation", toolID: "automation-api-client",
		requiredAuthority: 8, requestedAutonomy: 8,
		risk: RiskLow, reversible: true, maximumCostEUR: 0,
	},
	{
		id: "task-automation-api-read-approved-v1", actorIdentity: "hai-task-engine",
		action: "automation.api.read", stage: StageDataAccess,
		resourceType: "automation", toolID: "automation-api-client",
		requiredAuthority: 6, requestedAutonomy: 6,
		risk: RiskLow, reversible: true, maximumCostEUR: 0,
	},
	{
		id: "task-automation-api-mutate-v1", actorIdentity: "hai-task-engine",
		action: "automation.api.mutate", stage: StageCommitment,
		resourceType: "automation", toolID: "automation-api-client",
		requiredAuthority: 6, requestedAutonomy: 6,
		risk: RiskHigh, reversible: false, maximumCostEUR: 0,
	},
	{
		id: "task-automation-script-v1", actorIdentity: "hai-task-engine",
		action: "automation.script.execute", stage: StageExecution,
		resourceType: "automation", toolID: "automation-script-runner",
		requiredAuthority: 6, requestedAutonomy: 6,
		risk: RiskHigh, reversible: false, maximumCostEUR: 0,
	},
	{
		id: "task-automation-docker-v1", actorIdentity: "hai-task-engine",
		action: "automation.docker.start", stage: StageExecution,
		resourceType: "automation", toolID: "automation-docker-client",
		requiredAuthority: 6, requestedAutonomy: 6,
		risk: RiskHigh, reversible: true, maximumCostEUR: 0,
	},
	{
		// Free, unpaid cloud inference for the task engine. Prompt content
		// leaves the machine, so this is data access rather than tool use, is
		// classified as irreversible, and needs a higher authority than the
		// local profile. Zero cost is part of the contract: a priced or paid
		// model arrives as expenditure and matches nothing here.
		id: "task-engine-free-cloud-llm-generate-v1", actorIdentity: "hai:task-engine",
		action: "llm.generate", stage: StageDataAccess,
		resourceType: "llm-model", requireRuntimeID: true,
		requiredAuthority: 6, requestedAutonomy: 6,
		risk: RiskMedium, reversible: false, maximumCostEUR: 0,
	},
	{
		// Local, free, reversible inference for the task engine, including the
		// bounded model-directed read-only MCP tool loop. Only the local-safe
		// classification is registered: a cloud, paid, or approval-gated model
		// arrives with a different stage, authority and risk, matches nothing
		// here, and stays denied.
		id: "task-engine-local-llm-generate-v1", actorIdentity: "hai:task-engine",
		action: "llm.generate", stage: StageToolUse,
		resourceType: "llm-model", requireRuntimeID: true,
		requiredAuthority: 4, requestedAutonomy: 8,
		risk: RiskLow, reversible: true, maximumCostEUR: 0,
	},
	{
		id: "local-model-maintenance-v1", actorIdentity: "hai:model-maintenance",
		action: "llm.model.pull", stage: StageToolUse,
		resourceType: "llm-model", requireRuntimeID: true,
		requiredAuthority: 4, requestedAutonomy: 8,
		risk: RiskLow, reversible: true, maximumCostEUR: 0,
	},
}

func evaluateSystemWorkload(request Request) (SystemWorkloadEvidence, error) {
	evidence := SystemWorkloadEvidence{ActorIdentity: request.ActorIdentity}
	var policy *systemWorkloadPolicy
	actorRegistered := false
	effectRegistered := false
	for index := range builtInSystemWorkloadPolicies {
		candidate := &builtInSystemWorkloadPolicies[index]
		if candidate.actorIdentity != request.ActorIdentity {
			continue
		}
		actorRegistered = true
		if request.Action != candidate.action || request.Stage != candidate.stage ||
			request.ResourceType != candidate.resourceType ||
			(candidate.toolID != "" && request.ToolID != candidate.toolID) ||
			(candidate.runtimeID != "" && request.RuntimeID != candidate.runtimeID) ||
			(candidate.requireRuntimeID && request.RuntimeID == "") {
			continue
		}
		effectRegistered = true
		if request.RequiredAuthority != candidate.requiredAuthority ||
			request.RequestedAutonomy != candidate.requestedAutonomy ||
			request.Risk != candidate.risk || request.Reversible != candidate.reversible ||
			request.EstimatedCostEUR > candidate.maximumCostEUR {
			continue
		}
		policy = candidate
		break
	}
	if !actorRegistered {
		return evidence, fmt.Errorf("system actor is not registered as an executable workload")
	}
	if !effectRegistered {
		return evidence, fmt.Errorf("system workload effect does not match its registered operation contract")
	}
	if policy == nil {
		return evidence, fmt.Errorf("system workload classification differs from its server-owned policy")
	}
	evidence.PolicyID = policy.id
	if policy.id == "local-model-maintenance-v1" && request.ToolID != request.RuntimeID {
		return evidence, fmt.Errorf("model maintenance provider binding does not match its registered runtime")
	}
	if (policy.id == "task-engine-local-llm-generate-v1" ||
		policy.id == "task-engine-free-cloud-llm-generate-v1") && request.ToolID != request.RuntimeID {
		return evidence, fmt.Errorf("model generation provider binding does not match its registered runtime")
	}
	evidence.Matched = true
	return evidence, nil
}
