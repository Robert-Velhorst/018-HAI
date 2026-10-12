package executionauth

import "testing"

func TestTaskEngineSystemWorkloadProfilesMatchExactOperationContract(t *testing.T) {
	tests := []struct {
		name     string
		request  Request
		policyID string
	}{
		{
			name: "autonomous read",
			request: Request{ActorIdentity: "hai-task-engine", Action: "automation.api.read", Stage: StageDataAccess,
				ResourceType: "automation", ToolID: "automation-api-client", RequiredAuthority: 8,
				RequestedAutonomy: 8, Risk: RiskLow, Reversible: true},
			policyID: "task-automation-api-read-autonomous-v1",
		},
		{
			name: "case approved read",
			request: Request{ActorIdentity: "hai-task-engine", Action: "automation.api.read", Stage: StageDataAccess,
				ResourceType: "automation", ToolID: "automation-api-client", RequiredAuthority: 6,
				RequestedAutonomy: 6, Risk: RiskLow, Reversible: true},
			policyID: "task-automation-api-read-approved-v1",
		},
		{
			name: "case approved script",
			request: Request{ActorIdentity: "hai-task-engine", Action: "automation.script.execute", Stage: StageExecution,
				ResourceType: "automation", ToolID: "automation-script-runner", RequiredAuthority: 6,
				RequestedAutonomy: 6, Risk: RiskHigh, Reversible: false},
			policyID: "task-automation-script-v1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := evaluateSystemWorkload(test.request)
			if err != nil {
				t.Fatalf("evaluateSystemWorkload: %v", err)
			}
			if !evidence.Matched || evidence.PolicyID != test.policyID {
				t.Fatalf("evidence = %#v, want matched policy %q", evidence, test.policyID)
			}
		})
	}
}

func TestTaskEngineSystemWorkloadRejectsUnknownEffectAndSelfReclassification(t *testing.T) {
	unknown := Request{ActorIdentity: "hai-task-engine", Action: "automation.unknown", Stage: StageExecution,
		ResourceType: "automation", ToolID: "automation-api-client", RequiredAuthority: 6,
		RequestedAutonomy: 6, Risk: RiskLow, Reversible: true}
	if _, err := evaluateSystemWorkload(unknown); err == nil || err.Error() != "system workload effect does not match its registered operation contract" {
		t.Fatalf("unknown effect error = %v", err)
	}

	reclassified := Request{ActorIdentity: "hai-task-engine", Action: "automation.api.read", Stage: StageDataAccess,
		ResourceType: "automation", ToolID: "automation-api-client", RequiredAuthority: 6,
		RequestedAutonomy: 7, Risk: RiskLow, Reversible: true}
	if _, err := evaluateSystemWorkload(reclassified); err == nil || err.Error() != "system workload classification differs from its server-owned policy" {
		t.Fatalf("reclassified effect error = %v", err)
	}
}

func TestTaskEngineLocalModelGenerationIsRegisteredOnlyForLocalFreeInference(t *testing.T) {
	local := Request{ActorIdentity: "hai:task-engine", Action: "llm.generate", Stage: StageToolUse,
		ResourceType: "llm-model", ToolID: "ollama", RuntimeID: "ollama", RequiredAuthority: 4,
		RequestedAutonomy: 8, Risk: RiskLow, Reversible: true}
	evidence, err := evaluateSystemWorkload(local)
	if err != nil {
		t.Fatalf("local inference must be registered: %v", err)
	}
	if !evidence.Matched || evidence.PolicyID != "task-engine-local-llm-generate-v1" {
		t.Fatalf("evidence = %#v", evidence)
	}

	// Cloud egress has its own registered profile; what matters here is that it
	// never borrows the local one, whose lower authority assumes nothing leaves
	// the machine.
	cloud := local
	cloud.Stage = StageDataAccess
	cloud.Risk = RiskMedium
	cloud.Reversible = false
	cloud.RequiredAuthority = 6
	cloud.RequestedAutonomy = 6
	cloudEvidence, err := evaluateSystemWorkload(cloud)
	if err == nil && cloudEvidence.PolicyID == "task-engine-local-llm-generate-v1" {
		t.Fatal("cloud model generation must not match the local-safe policy")
	}

	paid := local
	paid.Stage = StageExpenditure
	paid.Risk = RiskHigh
	paid.Reversible = false
	paid.RequiredAuthority = 8
	paid.RequestedAutonomy = 6
	paid.EstimatedCostEUR = 0.01
	if _, err := evaluateSystemWorkload(paid); err == nil {
		t.Fatal("paid model generation must not match the local-safe policy")
	}

	free := local
	free.EstimatedCostEUR = 0.01
	if _, err := evaluateSystemWorkload(free); err == nil {
		t.Fatal("a priced local call must not match a zero-cost policy")
	}

	unbound := local
	unbound.RuntimeID = ""
	if _, err := evaluateSystemWorkload(unbound); err == nil {
		t.Fatal("model generation without a runtime binding must be denied")
	}

	mismatched := local
	mismatched.RuntimeID = "lm-studio"
	if _, err := evaluateSystemWorkload(mismatched); err == nil ||
		err.Error() != "model generation provider binding does not match its registered runtime" {
		t.Fatalf("mismatched provider binding error = %v", err)
	}
}

func TestTaskEngineFreeCloudGenerationIsRegisteredButPaidEgressIsNot(t *testing.T) {
	free := Request{ActorIdentity: "hai:task-engine", Action: "llm.generate", Stage: StageDataAccess,
		ResourceType: "llm-model", ToolID: "openrouter", RuntimeID: "openrouter", RequiredAuthority: 6,
		RequestedAutonomy: 6, Risk: RiskMedium, Reversible: false}
	evidence, err := evaluateSystemWorkload(free)
	if err != nil {
		t.Fatalf("free cloud inference must be registered: %v", err)
	}
	if !evidence.Matched || evidence.PolicyID != "task-engine-free-cloud-llm-generate-v1" {
		t.Fatalf("evidence = %#v", evidence)
	}

	priced := free
	priced.EstimatedCostEUR = 0.01
	if _, err := evaluateSystemWorkload(priced); err == nil {
		t.Fatal("a priced cloud call must not match the free-cloud policy")
	}

	paid := free
	paid.Stage = StageExpenditure
	paid.Risk = RiskHigh
	paid.RequiredAuthority = 8
	if _, err := evaluateSystemWorkload(paid); err == nil {
		t.Fatal("paid cloud generation must not match any registered policy")
	}

	escalated := free
	escalated.RequestedAutonomy = 8
	if _, err := evaluateSystemWorkload(escalated); err == nil {
		t.Fatal("cloud egress must not run at the local profile autonomy")
	}

	mismatched := free
	mismatched.RuntimeID = "openai-codex"
	if _, err := evaluateSystemWorkload(mismatched); err == nil ||
		err.Error() != "model generation provider binding does not match its registered runtime" {
		t.Fatalf("mismatched provider binding error = %v", err)
	}
}
